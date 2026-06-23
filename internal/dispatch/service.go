package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/events"
	"github.com/qwwqq1000-arch/tower/internal/fallback"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/session"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// Service assembles policy + selection + proxy + orchestration into one call.
type Service struct {
	Q     *sqlc.Queries
	Store *state.Store
	Base  policy.Config
	Now   func() int64
	sess  *session.Store

	// Cipher is the master-key AES-GCM cipher threaded in from the runtime
	// (vault-crypto-1). It is used to decrypt channel/account secrets just
	// before use (vault-crypto-3). May be nil in tests that don't touch secrets.
	Cipher *crypto.Cipher

	// scaledUp tracks owners for which reserve accounts were last activated,
	// to deduplicate scale_up / scale_down events.
	scaledUpMu sync.Mutex
	scaledUp   map[string]bool
}

// NewService builds a dispatch Service. cipher is the runtime master-key cipher
// (vault-crypto-1) used to decrypt secrets at use; it may be nil.
func NewService(q *sqlc.Queries, store *state.Store, base policy.Config, now func() int64, cipher *crypto.Cipher) *Service {
	return &Service{Q: q, Store: store, Base: base, Now: now, sess: session.NewStore(), Cipher: cipher, scaledUp: make(map[string]bool)}
}

// matchesAny reports whether body contains any of kws (case-insensitive).
func matchesAny(body string, kws []string) bool {
	lower := strings.ToLower(body)
	for _, kw := range kws {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// Outcome is the result of a dispatch.
type Outcome struct {
	Status int
	Body   string
	Target string
	Reason string
}

type usage struct {
	Usage struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheCreation            struct {
			Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
			Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
		} `json:"cache_creation"`
	} `json:"usage"`
}

// parseUsage extracts (in, out, cacheRead, cache5m, cache1h) from a JSON response body.
// If the split ephemeral fields are present (either >0), they are used as cache5m/cache1h;
// otherwise the aggregate cache_creation_input_tokens is treated as cache5m (cache1h=0).
func parseUsage(body string) (in, out, cacheRead, cache5m, cache1h int64) {
	var u usage
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		return 0, 0, 0, 0, 0
	}
	in = u.Usage.InputTokens
	out = u.Usage.OutputTokens
	cacheRead = u.Usage.CacheReadInputTokens
	if u.Usage.CacheCreation.Ephemeral5mInputTokens > 0 || u.Usage.CacheCreation.Ephemeral1hInputTokens > 0 {
		cache5m = u.Usage.CacheCreation.Ephemeral5mInputTokens
		cache1h = u.Usage.CacheCreation.Ephemeral1hInputTokens
	} else {
		cache5m = u.Usage.CacheCreationInputTokens
		cache1h = 0
	}
	return in, out, cacheRead, cache5m, cache1h
}

// Dispatch routes one request: fallback decision → our nodes (failover) →
// fallback backstop, logging and cost-rolling the outcome.
func (s *Service) Dispatch(ctx context.Context, ownerID, model, bodyText string, body []byte) Outcome {
	start := time.Now()

	cfg := s.resolveConfig(ctx, ownerID)
	breaker := state.BreakerCfg{
		PersistStreak: cfg.BanPersistStreak, PermStreak: cfg.PermanentBanStreak,
		BaseMs: cfg.CooldownBaseMs, MaxMs: cfg.CooldownMaxMs, Mult: cfg.CooldownMult,
	}

	order, resolver, keyOwner := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID, model)

	conv := session.ConvID(body)
	nowMs := s.Now()
	if cfg.AffinityTTLSec > 0 {
		order = s.applyAffinity(conv, order, nowMs)
	}

	est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
	probeText := lastUserText(body)
	var chPriceThreshold float64
	if len(channels) > 0 {
		chPriceThreshold = channels[0].PriceThreshold
	}
	trig := fallback.Decide(fallback.DecideInput{
		Model: model, BodyText: bodyText, ProbeText: probeText, EstCostUsd: est, PoolEmpty: len(order) == 0,
		Keywords: cfg.FallbackKeywords, FallbackModels: cfg.FallbackModels,
		PriceThresholdUsd: fallback.EffectivePriceThreshold(chPriceThreshold, cfg.FallbackPriceThresholdUsd),
		ProbeEnabled: cfg.FallbackProbeEnabled,
	})

	// Fallback-first when triggered (and channels exist).
	if cfg.FallbackEnabled && trig != fallback.None && trig != fallback.Exhausted && len(channels) > 0 {
		return s.viaChannels(ctx, ownerID, model, body, channels, string(trig), time.Since(start).Milliseconds(), cfg)
	}

	// PRE-FLIGHT: session exile check — route exiled conversations to fallback.
	if (cfg.SessionErrorThreshold > 0 || cfg.ResponseExileEnabled) && s.sess.Exiled(conv, nowMs) && cfg.FallbackEnabled && len(channels) > 0 {
		return s.viaChannels(ctx, ownerID, model, body, channels, "session", time.Since(start).Milliseconds(), cfg)
	}

	// Our nodes.
	if len(order) > 0 {
		maxFailover := cfg.MaxFailover
		if maxFailover <= 0 {
			maxFailover = 50
		}
		orch := &Orchestrator{Store: s.Store, Cfg: breaker, CooldownMin: cfg.SlotCooldownMinMs, CooldownMax: cfg.SlotCooldownMaxMs, MaxAttempts: maxFailover,
			OnBan:     func(key string, status int) { s.recordBan(ctx, acctOwnerOf(keyOwner, key, ownerID), key, status) },
			OnRecover: func(key string) { s.recordRecover(ctx, key) },
			OnAttempt: func(key string, status int, ok bool, banned bool) {
				if !ok {
					s.logAttemptErr(ctx, ownerID, model, key, status)
					s.recordRetry(ctx, acctOwnerOf(keyOwner, key, ownerID), model, key, status, banned)
					s.maybeCooldown(ctx, acctOwnerOf(keyOwner, key, ownerID), key, status, cfg)
				}
			},
			IsCooldownSignal: func(status int) bool { return isCooldownSignal(status, cfg) },
		}
		np := &NodeProxy{Body: body, Resolve: resolver, BanSignals: cfg.BanSignals, BanKeywords: cfg.BanKeywords, IdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second}
		res, winKey, ok := orch.Dispatch(ctx, model, order, np)
		if ok {
			// Response exile: if the response body contains a safety-refusal keyword,
			// exile this conversation and re-serve via fallback if possible.
			if cfg.ResponseExileEnabled && matchesAny(res.Body, cfg.ResponseExileKeywords) {
				if justExiled := s.sess.ForceExile(conv, int64(cfg.SessionCooldownSec)*1000, nowMs); justExiled {
					_ = events.Record(ctx, s.Q, nowMs, events.Event{Type: "session_exile", Target: "cyber", OwnerID: ownerID})
				}
				if len(channels) > 0 {
					return s.viaChannels(ctx, ownerID, model, body, channels, "cyber", time.Since(start).Milliseconds(), cfg)
				}
				// No fallback channel — log and return the original response.
				s.logOK(ctx, ownerID, model, res, winKey, time.Since(start).Milliseconds(), "cyber")
				return Outcome{Status: res.Status, Body: res.Body, Target: winKey, Reason: "cyber"}
			}
			s.sess.RecordSuccess(conv)
			if cfg.AffinityTTLSec > 0 {
				s.sess.SetAffinity(conv, winKey, int64(cfg.AffinityTTLSec)*1000, nowMs)
			}
			s.logOK(ctx, ownerID, model, res, winKey, time.Since(start).Milliseconds(), "")
			return Outcome{Status: res.Status, Body: res.Body, Target: winKey, Reason: ""}
		}
		// our pool failed → record error for session tracking
		if justExiled := s.sess.RecordError(conv, int64(cfg.SessionErrorThreshold), int64(cfg.SessionCooldownSec)*1000, nowMs); justExiled {
			_ = events.Record(ctx, s.Q, nowMs, events.Event{Type: "session_exile", Target: "session", OwnerID: ownerID})
		}
		// fallback backstop
		if cfg.FallbackEnabled && len(channels) > 0 {
			return s.viaChannels(ctx, ownerID, model, body, channels, "exhausted", time.Since(start).Milliseconds(), cfg)
		}
		s.logErr(ctx, ownerID, model, res.Status, 0, "")
		return Outcome{Status: 503, Body: `{"error":"all accounts exhausted"}`, Target: "node", Reason: ""}
	}

	// No nodes at all → fallback if possible.
	if cfg.FallbackEnabled && len(channels) > 0 {
		return s.viaChannels(ctx, ownerID, model, body, channels, "no_nodes", time.Since(start).Milliseconds(), cfg)
	}
	s.logErr(ctx, ownerID, model, 503, 0, "no_nodes")
	return Outcome{Status: 503, Body: `{"error":"no nodes available"}`, Target: "none", Reason: ""}
}

// applyAffinity moves the conversation's sticky account (if any, and present in
// order) to the front, so repeat requests reuse the same account for prompt
// caching. Enforces policy.AffinityTTLSec.
func (s *Service) applyAffinity(conv string, order []string, now int64) []string {
	if conv == "" || len(order) < 2 {
		return order
	}
	key, ok := s.sess.Affinity(conv, now)
	if !ok {
		return order
	}
	idx := -1
	for i, k := range order {
		if k == key {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return order // not found, or already first
	}
	reordered := make([]string, 0, len(order))
	reordered = append(reordered, key)
	reordered = append(reordered, order[:idx]...)
	reordered = append(reordered, order[idx+1:]...)
	return reordered
}

// resolveConfig resolves the effective 封控 policy for the given dispatch owner by
// applying the global layer first, then the owner's (tenant) layer over it, so a
// per-tenant override wins over the global default. ownerID=="" (admin/unowned
// dispatch key) has no tenant layer and resolves to global-over-base.
func (s *Service) resolveConfig(ctx context.Context, ownerID string) policy.Config {
	rows, err := s.Q.ListPolicies(ctx)
	if err != nil {
		return s.Base
	}
	var globalPatch, tenantPatch *policy.Patch
	for _, r := range rows {
		switch {
		case r.ScopeType == "global":
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil {
				globalPatch = &p
			}
		case ownerID != "" && r.ScopeType == "owner" && r.ScopeID == ownerID:
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil {
				tenantPatch = &p
			}
		}
	}
	patches := make([]policy.Patch, 0, 2)
	if globalPatch != nil {
		patches = append(patches, *globalPatch)
	}
	if tenantPatch != nil {
		patches = append(patches, *tenantPatch)
	}
	return policy.Resolve(s.Base, patches...)
}

// slotActiveNow reports whether the given [startMin, endMin) window (minute-of-day,
// Beijing time) is active at the instant represented by nowMs (Unix ms).
// If start == end or the window is [0, 1440) it is treated as always-active.
// Overnight windows (start > end) are active when cur >= start OR cur < end.
func slotActiveNow(startMin, endMin int, nowMs int64) bool {
	if startMin == endMin || (startMin == 0 && endMin == 1440) {
		return true // always-active
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	t := time.Unix(nowMs/1000, (nowMs%1000)*int64(time.Millisecond)).In(loc)
	cur := t.Hour()*60 + t.Minute()
	if startMin <= endMin {
		return cur >= startMin && cur < endMin
	}
	// overnight: e.g. 22:00 → 06:00
	return cur >= startMin || cur < endMin
}

func (s *Service) buildCandidates(ctx context.Context, ownerID, model string, cfg policy.Config) ([]string, Resolver, map[string]string) {
	nodes, _ := s.Q.ListNodes(ctx)
	// Build account-owner map for strict tenant isolation.
	acctOwner := map[string]string{}
	if ownerRows, aerr := s.Q.ListAccountOwners(ctx); aerr == nil {
		for _, row := range ownerRows {
			acctOwner[row.ID] = row.OwnerID
		}
	}
	// keyOwner maps dispatch key (nodeID:profileID) → the account's ownerID.
	// Used to attribute ban/retry/cooldown events to the banned account's owner
	// rather than the dispatch caller's owner (events-audit-3).
	keyOwner := map[string]string{}
	refs := map[string]NodeRef{}
	type cand struct {
		key     string
		weight  int
		reserve bool
	}
	var cands []cand
	nowMs := s.Now()
	isOpus := strings.Contains(strings.ToLower(model), "opus")

	// Load slots once per call for slot-window filtering.
	type slotEntry struct{ startMin, endMin int; enabled bool }
	slotMap := map[string]slotEntry{}
	if slotRows, serr := s.Q.ListSlots(ctx); serr == nil {
		for _, sl := range slotRows {
			slotMap[sl.ID] = slotEntry{startMin: int(sl.StartMin), endMin: int(sl.EndMin), enabled: sl.Enabled}
		}
	}

	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		accs, err := s.Q.ListNodeAccountsByNode(ctx, n.ID)
		if err != nil {
			continue
		}
		for _, na := range accs {
			if !na.Enabled {
				continue
			}
			// Strict per-account tenant isolation: skip accounts not owned by the requesting tenant.
			// ownerID=="" means admin/unowned dispatch key → include all accounts.
			if ownerID != "" && acctOwner[na.AccountID] != ownerID {
				continue
			}
			// Slot-window filter: skip only when slot exists, is enabled, and window is inactive.
			if na.SlotID != "" {
				if sl, ok := slotMap[na.SlotID]; ok && sl.enabled {
					if !slotActiveNow(sl.startMin, sl.endMin, nowMs) {
						continue
					}
				}
				// Unknown slot_id or disabled slot → treat as always-active (don't skip).
			}
			// Determine warmup state for this account.
			var onboardedAt int64
			if acc, aerr := s.Q.GetAccount(ctx, na.AccountID); aerr == nil {
				onboardedAt = acc.OnboardedAt
			}
			inWarmup := cfg.WarmupHours > 0 && onboardedAt > 0 &&
				(nowMs-onboardedAt) < int64(cfg.WarmupHours)*3_600_000
			// Skip opus candidates that are still warming up (if block is enabled).
			if inWarmup && cfg.WarmupBlockOpus && isOpus {
				continue
			}
			key := n.ID + ":" + na.ProfileID
			// Record the account owner for ban-event attribution (events-audit-3).
			keyOwner[key] = acctOwner[na.AccountID]
			// Decrypt the node api_key transparently (vault-crypto-3): ciphertext
			// rows decrypt, legacy plaintext rows pass through unchanged.
			refs[key] = NodeRef{BaseURL: n.BaseUrl, APIKey: s.Cipher.DecryptOrPlaintext(n.ApiKey), ProfileID: na.ProfileID, Kind: n.Kind}
			s.Store.Ensure(key, cfg.MaxConcurrent)
			s.Store.SetCapacity(key, cfg.MaxConcurrent)
			// Apply or clear warmup cap.
			if inWarmup {
				s.Store.SetWarmupCap(key, cfg.WarmupMaxConcurrent)
			} else {
				s.Store.SetWarmupCap(key, 0)
			}
			cands = append(cands, cand{key: key, weight: int(na.Weight), reserve: na.Role == "reserve"})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].weight > cands[j].weight })

	if !cfg.ElasticEnabled {
		// Non-elastic path: all accounts, weight-desc (unchanged behaviour).
		order := make([]string, len(cands))
		for i, c := range cands {
			order[i] = c.key
		}
		resolver := func(key string) (NodeRef, bool) { r, ok := refs[key]; return r, ok }
		return order, resolver, keyOwner
	}

	// Elastic path: partition by count — first N (weight-desc) are baseline; the rest are reserve.
	n := cfg.ElasticBaselineCount
	if n < 1 {
		n = 1
	}
	if n > len(cands) {
		n = len(cands)
	}
	baseline := make([]string, n)
	for i := 0; i < n; i++ {
		baseline[i] = cands[i].key
	}
	var reserve []string
	for i := n; i < len(cands); i++ {
		reserve = append(reserve, cands[i].key)
	}

	// Compute baseline utilisation from the store snapshot.
	snap := s.Store.Snapshot(nowMs)
	baselineSet := make(map[string]bool, len(baseline))
	for _, k := range baseline {
		baselineSet[k] = true
	}
	var totalInflight, totalCapacity int
	for _, sn := range snap {
		if baselineSet[sn.Key] {
			totalInflight += sn.Inflight
			totalCapacity += sn.Inflight + sn.Available
		}
	}
	var util float64
	if totalCapacity == 0 {
		util = 1.0
	} else {
		util = float64(totalInflight) / float64(totalCapacity)
	}

	scaleUp := cfg.ElasticScaleUpUtil
	scaleDown := cfg.ElasticScaleDownUtil
	if scaleDown <= 0 || scaleDown >= scaleUp {
		scaleDown = scaleUp // no hysteresis if misconfigured
	}

	s.scaledUpMu.Lock()
	wasScaled := s.scaledUp[ownerID]
	shouldScale := wasScaled
	if !wasScaled && util >= scaleUp {
		shouldScale = true
	} else if wasScaled && util <= scaleDown {
		shouldScale = false
	}
	if shouldScale {
		s.scaledUp[ownerID] = true
	} else {
		delete(s.scaledUp, ownerID)
	}
	s.scaledUpMu.Unlock()

	var order []string
	if shouldScale && len(reserve) > 0 {
		nRes := len(reserve)
		if cfg.ElasticMaxReserve > 0 && nRes > cfg.ElasticMaxReserve {
			nRes = cfg.ElasticMaxReserve
		}
		order = append(append([]string{}, baseline...), reserve[:nRes]...)
	} else {
		order = baseline
	}

	// Deduplicated scale_up / scale_down events (recorded after unlocking).
	if shouldScale && !wasScaled {
		_ = events.Record(ctx, s.Q, nowMs, events.Event{
			Type:    "scale_up",
			Target:  fmt.Sprintf("reserves=%d", len(order)-len(baseline)),
			OwnerID: ownerID,
		})
	} else if !shouldScale && wasScaled {
		_ = events.Record(ctx, s.Q, nowMs, events.Event{
			Type:    "scale_down",
			Target:  "",
			OwnerID: ownerID,
		})
	}

	resolver := func(key string) (NodeRef, bool) { r, ok := refs[key]; return r, ok }
	return order, resolver, keyOwner
}

// channelAboveBalanceAlert reports whether a fallback channel has a sufficient
// observed balance to be eligible for routing. A channel is excluded when:
//   - balance_alert_usd > 0 (a threshold is configured), AND
//   - balance_usd < balance_alert_usd (the last observed balance is below it).
//
// When no alert threshold is configured (balance_alert_usd == 0) the channel is
// always considered routable regardless of the observed balance. This ensures
// that channels without balance monitoring are never silently excluded.
func channelAboveBalanceAlert(ch sqlc.FallbackChannel) bool {
	if ch.BalanceAlertUsd <= 0 {
		return true // no threshold configured → always routable
	}
	return ch.BalanceUsd >= ch.BalanceAlertUsd
}

// channelAllowsModel reports whether model is permitted by a channel's allowlist.
// The allowlist is a comma- or space-separated list of model-family keywords
// (e.g. "opus", "haiku", "sonnet").  An empty allowlist permits all models.
// Matching is case-insensitive substring: a keyword "haiku" matches any model
// whose name contains "haiku".
func channelAllowsModel(allowlist, model string) bool {
	if allowlist == "" {
		return true
	}
	m := strings.ToLower(model)
	// Split on commas or spaces.
	for _, raw := range strings.FieldsFunc(allowlist, func(r rune) bool { return r == ',' || r == ' ' }) {
		kw := strings.ToLower(strings.TrimSpace(raw))
		if kw == "" {
			continue
		}
		if strings.Contains(m, kw) {
			return true
		}
	}
	return false
}

func (s *Service) enabledChannels(ctx context.Context, ownerID string, model string) []sqlc.FallbackChannel {
	chs, err := s.Q.ListEnabledFallbackChannels(ctx)
	if err != nil {
		return nil
	}
	out := chs[:0]
	for _, c := range chs {
		if c.OwnerID != ownerID { // strict owner scoping: admin(owner="") uses owner="" channels; tenant uses own
			continue
		}
		if !channelAllowsModel(c.ModelAllowlist, model) {
			continue
		}
		// Balance is alert-only and NEVER removes a channel from routing: a stale or
		// transiently-zero balance reading must not exclude the only fallback channel
		// and turn a recoverable node failure into a 503 (fallback-5 revisited).
		out = append(out, c)
	}
	return out
}

// fbSlotKey returns the store key for a fallback channel's concurrency slot.
func fbSlotKey(id string) string { return "fb:" + id }

// viaChannel forwards one non-streaming request through a single fallback
// channel. served=false means the channel was at MaxConcurrent capacity and the
// caller should try the next channel; served=true means this channel handled the
// request (success or a real error response).
func (s *Service) viaChannel(ctx context.Context, ownerID, model string, body []byte, ch sqlc.FallbackChannel, reason string, latencyMs int64, cfg policy.Config) (Outcome, bool) {
	cap := int(ch.MaxConcurrent)
	if cap <= 0 {
		cap = 1000
	}
	key := fbSlotKey(ch.ID)
	s.Store.Ensure(key, cap)
	bk := state.BreakerCfg{PersistStreak: 1 << 30, BaseMs: 0, MaxMs: 0, Mult: 1}
	// TryDispatch returns false when the channel's slot set is full (MaxConcurrent
	// reached). Reject with backpressure (503) rather than forwarding anyway, so
	// the concurrency cap is actually enforced (fallback-2). Return the 503 Outcome
	// before any Q persistence (mirrors streamChannel's reject path): the caller
	// receiving this Outcome is responsible for its own logging, and keeping this
	// path Q-free lets it be unit-tested without a database.
	if !s.Store.TryDispatch(key, model, bk) {
		return Outcome{Status: 503, Body: `{"error":"fallback channel at capacity"}`, Target: "fallback:" + ch.ID, Reason: reason}, false
	}
	defer s.Store.Complete(key, int64(ch.CooldownMs), int64(ch.CooldownMs))

	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "fallback", Target: ch.ID, OwnerID: ownerID, Detail: map[string]any{"reason": reason, "channelId": ch.ID, "channelName": ch.Name}})

	// Decrypt the channel api_key transparently before forwarding (vault-crypto-3).
	cp := &ChannelProxy{Body: body, Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: s.Cipher.DecryptOrPlaintext(ch.ApiKey)}, IdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second}
	res, err := cp.Send(ctx, ch.ID)
	if err != nil {
		// Channel unreachable — report served=false so viaChannels fails over to the
		// next channel (matches the streaming path). Q-free, like the capacity reject.
		return Outcome{Status: 502, Body: `{"error":"fallback channel error"}`, Target: "fallback:" + ch.ID, Reason: reason}, false
	}
	status := "ok"
	if res.Status < 200 || res.Status >= 300 {
		status = "error"
	}
	var cost float64
	var in, out int64
	if status == "ok" {
		var cacheRead, cache5m, cache1h int64
		in, out, cacheRead, cache5m, cache1h = parseUsage(res.Body)
		cost = billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
		// Record the channel's last-observed balance so the spend row reflects
		// the balance at dispatch time (fallback-5: write observed balance).
		_ = s.Q.UpsertFallbackSpend(ctx, sqlc.UpsertFallbackSpendParams{ChannelID: ch.ID, Day: todayDayStr(), Requests: 1, EstCostUsd: cost, BalanceObserved: ch.BalanceUsd})
	}
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		ProfileID: "", Status: status, HttpStatus: int32(res.Status), LatencyMs: latencyMs, FallbackReason: reason,
		TokensIn: in, TokensOut: out, Stream: false, CostUsd: cost,
	})
	// served=false on an error status (>=400) so viaChannels fails over to the next
	// channel; a clean <400 response is returned as-is.
	return Outcome{Status: res.Status, Body: res.Body, Target: "fallback:" + ch.ID, Reason: reason}, res.Status < 400
}

// viaChannels forwards a non-streaming request through the owner's fallback
// channels in priority order, skipping any that are at MaxConcurrent capacity,
// and returns the first that serves it. Only when EVERY channel is at capacity
// does it return the last 503 (fallback-1: failover to the next fallback channel
// instead of 503-ing when channels[0] is full).
func (s *Service) viaChannels(ctx context.Context, ownerID, model string, body []byte, channels []sqlc.FallbackChannel, reason string, latencyMs int64, cfg policy.Config) Outcome {
	var lastFail Outcome
	for _, ch := range channels {
		out, served := s.viaChannel(ctx, ownerID, model, body, ch, reason, latencyMs, cfg)
		if served {
			return out
		}
		lastFail = out // at capacity or errored — remember it and try the next channel
	}
	return lastFail
}

// streamChannels forwards a streaming request through the owner's fallback
// channels in priority order, skipping any that are at capacity or unreachable
// (committed=false ⟹ nothing written to w yet, safe to try the next), and returns
// committed=true for the first that serves the stream (fallback-1).
func (s *Service) streamChannels(ctx context.Context, w http.ResponseWriter, channels []sqlc.FallbackChannel, body []byte, ownerID, model, reason string, cfg policy.Config) (Outcome, bool) {
	for _, ch := range channels {
		if out, committed := s.streamChannel(ctx, w, ch, body, ownerID, model, reason, cfg); committed {
			return out, true
		}
	}
	return Outcome{}, false
}

func (s *Service) logOK(ctx context.Context, ownerID, model string, res ProxyResult, key string, latencyMs int64, reason string) {
	in, out, cacheRead, cache5m, cache1h := parseUsage(res.Body)
	cost := billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
	if reason == "" && !billing.KnownModel(model) {
		reason = "unknown-model-pricing"
	}
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: "ok", HttpStatus: int32(res.Status), LatencyMs: latencyMs, TokensIn: in, TokensOut: out,
		FallbackReason: reason, TtfbMs: latencyMs, Stream: false, CostUsd: cost,
	})
	if in > 0 || out > 0 {
		_ = s.Q.AddCostRollup(ctx, sqlc.AddCostRollupParams{
			ScopeType: "owner", ScopeID: ownerID, Day: todayDayStr(), Model: model,
			Requests: 1, TokensIn: in, TokensOut: out, CostUsd: cost,
		})
	}
}

func (s *Service) logErr(ctx context.Context, ownerID, model string, status int, latencyMs int64, reason string) {
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "node", ProfileID: "",
		Status: "error", HttpStatus: int32(status), LatencyMs: latencyMs, FallbackReason: reason,
		Stream: false, CostUsd: 0,
	})
}

// logAttemptErr logs a single per-attempt failure (non-2xx or banned) without
// overwriting the final-outcome row written by logOK / logErr. Latency is 0
// because we have no settled TTFB for a failed attempt.
func (s *Service) logAttemptErr(ctx context.Context, ownerID, model, key string, status int) {
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: "error", HttpStatus: int32(status), LatencyMs: 0, FallbackReason: "",
		Stream: false, CostUsd: 0,
	})
}

// flushCopyCapture streams src→dst flushing each chunk; returns ttfb (ms to
// first byte, from start) and a bounded copy of the body (for token parsing).
func flushCopyCapture(dst http.ResponseWriter, src io.Reader, start time.Time) (ttfbMs int64, body string) {
	fl, _ := dst.(http.Flusher)
	buf := make([]byte, 16*1024)
	var sb strings.Builder
	const bodyCap = 512 * 1024
	first := true
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if first {
				ttfbMs = time.Since(start).Milliseconds()
				first = false
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			if sb.Len() < bodyCap {
				sb.Write(buf[:n])
			}
		}
		if err != nil {
			break
		}
	}
	return ttfbMs, sb.String()
}

// parseUsageSSE extracts (in, out, cacheRead, cache5m, cache1h) from an Anthropic SSE body.
// input_tokens appears in message_start; output_tokens last value is in message_delta.
// If the split ephemeral fields appear (either >0), cache5m/cache1h are set from those;
// otherwise aggregate cache_creation_input_tokens is treated as cache5m (cache1h=0).
func parseUsageSSE(body string) (in, out, cacheRead, cache5m, cache1h int64) {
	reIn := regexp.MustCompile(`"input_tokens":\s*(\d+)`)
	reOut := regexp.MustCompile(`"output_tokens":\s*(\d+)`)
	reCacheRead := regexp.MustCompile(`"cache_read_input_tokens":\s*(\d+)`)
	reEph5m := regexp.MustCompile(`"ephemeral_5m_input_tokens":\s*(\d+)`)
	reEph1h := regexp.MustCompile(`"ephemeral_1h_input_tokens":\s*(\d+)`)
	reCacheCreate := regexp.MustCompile(`"cache_creation_input_tokens":\s*(\d+)`)

	if m := reIn.FindStringSubmatch(body); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &in)
	}
	// use last match for output_tokens
	all := reOut.FindAllStringSubmatch(body, -1)
	if len(all) > 0 {
		fmt.Sscanf(all[len(all)-1][1], "%d", &out)
	}
	if m := reCacheRead.FindStringSubmatch(body); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &cacheRead)
	}
	var eph5m, eph1h int64
	if m := reEph5m.FindStringSubmatch(body); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &eph5m)
	}
	if m := reEph1h.FindStringSubmatch(body); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &eph1h)
	}
	if eph5m > 0 || eph1h > 0 {
		cache5m = eph5m
		cache1h = eph1h
	} else {
		var agg int64
		if m := reCacheCreate.FindStringSubmatch(body); len(m) == 2 {
			fmt.Sscanf(m[1], "%d", &agg)
		}
		cache5m = agg
		cache1h = 0
	}
	return in, out, cacheRead, cache5m, cache1h
}

// logStream logs a completed streaming dispatch with TTFB and token counts.
func (s *Service) logStream(ctx context.Context, ownerID, model, key string, status int, in, out, cacheRead, cache5m, cache1h, latencyMs, ttfbMs int64) {
	httpStatus := "ok"
	if status < 200 || status >= 300 {
		httpStatus = "error"
	}
	cost := billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
	streamReason := ""
	if !billing.KnownModel(model) {
		streamReason = "unknown-model-pricing"
	}
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: httpStatus, HttpStatus: int32(status), LatencyMs: latencyMs,
		TokensIn: in, TokensOut: out, TtfbMs: ttfbMs, Stream: true, CostUsd: cost,
		FallbackReason: streamReason,
	})
	if in > 0 || out > 0 {
		_ = s.Q.AddCostRollup(ctx, sqlc.AddCostRollupParams{
			ScopeType: "owner", ScopeID: ownerID, Day: todayDayStr(), Model: model,
			Requests: 1, TokensIn: in, TokensOut: out, CostUsd: cost,
		})
	}
}

// flushCopy streams src→dst, flushing after each chunk so SSE reaches the client live.
func flushCopy(dst http.ResponseWriter, src io.Reader) {
	fl, _ := dst.(http.Flusher)
	buf := make([]byte, 16*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// DispatchStream routes a streaming request: it may fail over to another account
// before the first byte; once streaming to the client starts, it commits.
func (s *Service) DispatchStream(ctx context.Context, w http.ResponseWriter, ownerID, model string, body []byte) Outcome {
	cfg := s.resolveConfig(ctx, ownerID)
	breaker := state.BreakerCfg{
		PersistStreak: cfg.BanPersistStreak, PermStreak: cfg.PermanentBanStreak,
		BaseMs: cfg.CooldownBaseMs, MaxMs: cfg.CooldownMaxMs, Mult: cfg.CooldownMult,
	}
	order, resolver, keyOwner := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID, model)

	conv := session.ConvID(body)
	nowMs := s.Now()
	if cfg.AffinityTTLSec > 0 {
		order = s.applyAffinity(conv, order, nowMs)
	}

	// Probe/keyword/model fallback decision — same logic as non-streaming Dispatch.
	est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
	probeText := lastUserText(body)
	var chPriceThresholdS float64
	if len(channels) > 0 {
		chPriceThresholdS = channels[0].PriceThreshold
	}
	trig := fallback.Decide(fallback.DecideInput{
		Model: model, BodyText: string(body), ProbeText: probeText, EstCostUsd: est, PoolEmpty: len(order) == 0,
		Keywords: cfg.FallbackKeywords, FallbackModels: cfg.FallbackModels,
		PriceThresholdUsd: fallback.EffectivePriceThreshold(chPriceThresholdS, cfg.FallbackPriceThresholdUsd),
		ProbeEnabled: cfg.FallbackProbeEnabled,
	})
	if cfg.FallbackEnabled && trig != fallback.None && trig != fallback.Exhausted && len(channels) > 0 {
		if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, string(trig), cfg); committed {
			return out
		}
	}

	// PRE-FLIGHT: session exile check — route exiled conversations to fallback.
	if (cfg.SessionErrorThreshold > 0 || cfg.ResponseExileEnabled) && s.sess.Exiled(conv, nowMs) && cfg.FallbackEnabled && len(channels) > 0 {
		if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, "session", cfg); committed {
			return out
		}
	}

	maxFailover := cfg.MaxFailover
	if maxFailover <= 0 {
		maxFailover = 50
	}
	attempts := 0
	for _, key := range order {
		if attempts >= maxFailover {
			break
		}
		attempts++
		ok, trial := s.Store.TryDispatchTrial(key, model, breaker)
		if !ok {
			continue
		}
		out, committed, sseBody := s.streamOneWithBody(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model, keyOwner)
		if committed {
			// Response exile: scan body for safety-refusal keywords.
			if cfg.ResponseExileEnabled && matchesAny(sseBody, cfg.ResponseExileKeywords) {
				if justExiled := s.sess.ForceExile(conv, int64(cfg.SessionCooldownSec)*1000, nowMs); justExiled {
					_ = events.Record(ctx, s.Q, nowMs, events.Event{Type: "session_exile", Target: "cyber", OwnerID: ownerID})
				}
				// Cannot re-serve mid-stream — exile only affects future requests.
			} else if out.Status >= 300 {
				// Committed stream carried an upstream error (e.g. event:error with a
				// 200 header). Can't fail over now, but count it as a session error so
				// the conversation exiles to fallback on subsequent requests.
				if justExiled := s.sess.RecordError(conv, int64(cfg.SessionErrorThreshold), int64(cfg.SessionCooldownSec)*1000, nowMs); justExiled {
					_ = events.Record(ctx, s.Q, nowMs, events.Event{Type: "session_exile", Target: "session", OwnerID: ownerID})
				}
			} else {
				s.sess.RecordSuccess(conv)
				if cfg.AffinityTTLSec > 0 {
					s.sess.SetAffinity(conv, key, int64(cfg.AffinityTTLSec)*1000, nowMs)
				}
			}
			return out
		}
		// not committed → failed before first byte → log per-attempt error + failover
		s.logAttemptErr(ctx, ownerID, model, key, out.Status)
		s.recordRetry(ctx, acctOwnerOf(keyOwner, key, ownerID), model, key, out.Status, ClassifyBanned(out.Status, "", cfg.BanSignals, nil))
		s.maybeCooldown(ctx, acctOwnerOf(keyOwner, key, ownerID), key, out.Status, cfg)
		if justExiled := s.sess.RecordError(conv, int64(cfg.SessionErrorThreshold), int64(cfg.SessionCooldownSec)*1000, nowMs); justExiled {
			_ = events.Record(ctx, s.Q, nowMs, events.Event{Type: "session_exile", Target: "session", OwnerID: ownerID})
		}
	}

	// exhausted → fallback channel stream, else 503
	if cfg.FallbackEnabled && len(channels) > 0 {
		exReason := "exhausted"
		if len(order) == 0 {
			exReason = "no_nodes"
		}
		if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, exReason, cfg); committed {
			return out
		}
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":"no nodes available"}`))
	s.logErr(ctx, ownerID, model, 503, 0, "")
	return Outcome{Status: 503, Target: "none"}
}

// streamOneWithBody is like streamOne but also returns the captured SSE body.
// This allows callers to inspect the response for exile keywords without
// duplicating any of the settle invariants.
func (s *Service) streamOneWithBody(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string, keyOwner map[string]string) (Outcome, bool, string) {
	out, committed, sseBody := s.streamOneInternal(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model, keyOwner)
	return out, committed, sseBody
}

// streamOneInternal is the actual implementation; streamOneWithBody is a thin wrapper.
func (s *Service) streamOneInternal(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string, keyOwner map[string]string) (Outcome, bool, string) {
	settled := false
	sendReturned := false
	lastStatus := 0
	lastBanned := false
	settle := func(success bool) {
		if settled {
			return
		}
		settled = true
		s.Store.Complete(key, cfg.SlotCooldownMinMs, cfg.SlotCooldownMaxMs)
		if !success && !sendReturned {
			return // panic before upstream responded → release slot only, no ban
		}
		if trial {
			if !success && isCooldownSignal(lastStatus, cfg) {
				// A transient cooldown signal (e.g. 429) during recovery must not
				// reopen/escalate the breaker; the error-cooldown owns the backoff.
				s.Store.OnTrialCooldown(key)
			} else {
				s.Store.OnTrialResult(key, breaker, success, lastBanned)
				if success {
					// Half-open trial succeeded: close the open ban episode so
					// recovered_at/survival_ms are populated (events-audit-2).
					s.recordRecover(ctx, key)
				} else if lastBanned {
					s.recordBan(ctx, acctOwnerOf(keyOwner, key, ownerID), key, lastStatus)
				}
			}
		} else if success {
			s.Store.OnSuccess(key)
		} else if lastBanned {
			// Only classified ban signals open the breaker; transient failures
			// (502/429/network) fail over without banning.
			if s.Store.OnBanSignal(key, breaker) {
				s.recordBan(ctx, acctOwnerOf(keyOwner, key, ownerID), key, lastStatus)
			}
		}
	}
	defer func() { settle(false) }()

	np := &NodeProxy{Body: body, Resolve: resolver, BanSignals: cfg.BanSignals, BanKeywords: cfg.BanKeywords, IdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second}
	st, err := np.OpenStream(ctx, key)
	sendReturned = true
	if err != nil {
		return Outcome{}, false, "" // connection error → failover
	}
	lastStatus = st.Status
	lastBanned = st.Banned
	if st.Banned || st.Status < 200 || st.Status >= 300 {
		httpStatus := st.Status
		_ = st.Body.Close()
		return Outcome{Status: httpStatus, Target: key}, false, "" // bad status before first byte → settle(false) via defer, failover
	}
	// commit: stream to client
	start := time.Now()
	CopyForwardableHeaders(w.Header(), st.Header)
	w.WriteHeader(st.Status)
	ttfb, sseBody := flushCopyCapture(w, st.Body, start)
	_ = st.Body.Close()
	// Claude can return a 200 header and then an `event: error` (e.g.
	// overloaded_error) inside the SSE body. We've already committed (cannot fail
	// over mid-stream), but it must be accounted as an ERROR, not 200 ok — and fed
	// into the session-error counter so the conversation exiles to fallback next.
	streamErr := sseHasError(sseBody)
	// A committed stream whose error body matches a ban signal/keyword (e.g.
	// authentication_error) is a real ban — open the breaker (the HTTP header was
	// 200 so this is the only way streamed keyword/auth bans are detected).
	if streamErr && ClassifyBanned(st.Status, sseBody, cfg.BanSignals, cfg.BanKeywords) {
		lastBanned = true
	}
	settle(!streamErr)
	in, out, cacheRead, cache5m, cache1h := parseUsageSSE(sseBody)
	total := time.Since(start).Milliseconds()
	effStatus := st.Status
	if streamErr {
		effStatus = 500
	}
	s.logStream(ctx, ownerID, model, key, effStatus, in, out, cacheRead, cache5m, cache1h, total, ttfb)
	return Outcome{Status: effStatus, Target: key}, true, sseBody
}

// sseHasError reports whether a committed SSE body carries an upstream error
// event (Claude emits `event: error` with a 200 header for overloaded/api errors,
// e.g. {"type":"error","error":{"type":"overloaded_error"}}).
func sseHasError(body string) bool {
	return strings.Contains(body, "event: error") ||
		strings.Contains(body, `"type": "error"`) ||
		strings.Contains(body, `"type":"error"`)
}

// recordBan records a ban episode and event for the given key (node:profile).
// recordBan records a ban-control event (ban_detected, or ban_permanent when the
// account was permanently banned) with the triggering account, streak, and
// HTTP status, plus a durable ban_episode. ownerID/status give the event context.
//
// permanent and streak are read in a single BanInfo call (ban-classify-6) so
// the event type and detail are always consistent with each other.
// Errors from both writes are logged rather than silently discarded (events-audit-5).
func (s *Service) recordBan(ctx context.Context, ownerID, key string, status int) {
	node, profile, found := splitKey(key)
	if !found {
		return
	}
	// Capture now once so the dispatch event and ban episode share the same
	// timestamp (two s.Now() calls could skew the values on a slow path).
	nowMs := s.Now()
	// Read permanent + streak atomically under a single store lock to avoid
	// the race where IsPermanent and BanStreak each acquire the lock separately
	// and can observe different breaker generations (ban-classify-6).
	permanent, streak := s.Store.BanInfo(key)
	typ := "ban_detected"
	if permanent {
		typ = "ban_permanent"
	}
	detail := map[string]any{"account": key, "status": status, "streak": streak, "permanent": permanent}
	if err := events.Record(ctx, s.Q, nowMs, events.Event{Type: typ, Target: key, OwnerID: ownerID, Detail: detail}); err != nil {
		log.Printf("recordBan: insert event key=%s: %v", key, err)
	}
	db, _ := json.Marshal(detail)
	if err := s.Q.InsertBanEpisode(ctx, sqlc.InsertBanEpisodeParams{NodeID: node, ProfileID: profile, BannedAt: nowMs, Detail: db}); err != nil {
		log.Printf("recordBan: insert episode key=%s: %v", key, err)
	}
}

// recordRecover closes any open ban episodes for the given key (node:profile)
// by calling RecoverBanEpisode, so recovered_at and survival_ms are populated.
// Called on half-open trial success and on manual recovery.
func (s *Service) recordRecover(ctx context.Context, key string) {
	node, profile, found := splitKey(key)
	if !found {
		return
	}
	_ = s.Q.RecoverBanEpisode(ctx, sqlc.RecoverBanEpisodeParams{
		NodeID:      node,
		ProfileID:   profile,
		RecoveredAt: s.Now(),
	})
}

// isCooldownSignal reports whether status matches a configured CooldownSignal (and
// the feature is enabled). Shared by maybeCooldown and the trial-settle paths.
func isCooldownSignal(status int, cfg policy.Config) bool {
	if cfg.CooldownSignalSec <= 0 || len(cfg.CooldownSignals) == 0 {
		return false
	}
	for _, c := range cfg.CooldownSignals {
		if status == c {
			return true
		}
	}
	return false
}

// maybeCooldown puts an account into a temporary cooldown when the response status
// matches a configured CooldownSignal (e.g. 429). This is NOT a ban: the account
// auto-recovers after CooldownSignalSec and never escalates to permanent.
func (s *Service) maybeCooldown(ctx context.Context, ownerID, key string, status int, cfg policy.Config) {
	if !isCooldownSignal(status, cfg) {
		return
	}
	until := s.Now() + int64(cfg.CooldownSignalSec)*1000
	s.Store.SetCooldown(key, cfg.MaxConcurrent, until)
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{
		Type: "cooldown", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "status": status, "seconds": cfg.CooldownSignalSec},
	})
}

// recordRetry records a failover event: an attempt to account `key` failed
// (status / banned), so dispatch moves on to the next candidate.
func (s *Service) recordRetry(ctx context.Context, ownerID, model, key string, status int, banned bool) {
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{
		Type: "retry", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "model": model, "status": status, "banned": banned},
	})
}

func splitKey(key string) (node, profile string, ok bool) {
	i := strings.LastIndex(key, ":")
	if i < 0 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

// acctOwnerOf returns the ownerID of the account associated with the dispatch
// key (nodeID:profileID). If the key is not in the keyOwner map (e.g. the
// account was added after buildCandidates ran), it falls back to the dispatch
// caller's ownerID so events-audit-3 attribution degrades gracefully.
func acctOwnerOf(keyOwner map[string]string, key, callerOwnerID string) string {
	if owner, ok := keyOwner[key]; ok && owner != "" {
		return owner
	}
	return callerOwnerID
}

func todayDayStr() string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// lastUserText extracts the text content of the last user message from a raw
// Anthropic-format request body. Content may be a plain string or an array of
// content blocks; in the latter case the "text" fields are concatenated.
// Returns "" on any parse failure or when no user message is present.
func lastUserText(body []byte) string {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	// Walk backwards to find the last user message.
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := req.Messages[i]
		if msg.Role != "user" {
			continue
		}
		// Try plain string first.
		var s string
		if err := json.Unmarshal(msg.Content, &s); err == nil {
			return s
		}
		// Try array of content blocks.
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			var sb strings.Builder
			for _, b := range blocks {
				if b.Type == "text" {
					sb.WriteString(b.Text)
				}
			}
			return sb.String()
		}
		return ""
	}
	return ""
}

// streamChannel attempts a fallback channel stream. committed=true means we wrote to the client.
func (s *Service) streamChannel(ctx context.Context, w http.ResponseWriter, ch sqlc.FallbackChannel, body []byte, ownerID, model, reason string, cfg policy.Config) (Outcome, bool) {
	cap := int(ch.MaxConcurrent)
	if cap <= 0 {
		cap = 1000
	}
	key := fbSlotKey(ch.ID)
	s.Store.Ensure(key, cap)
	bk := state.BreakerCfg{PersistStreak: 1 << 30, BaseMs: 0, MaxMs: 0, Mult: 1}
	// Slot set full (MaxConcurrent reached): do not forward. Return committed=false
	// so the caller falls through to the next attempt / 503 (fallback-2).
	if !s.Store.TryDispatch(key, model, bk) {
		return Outcome{}, false
	}
	defer s.Store.Complete(key, int64(ch.CooldownMs), int64(ch.CooldownMs))

	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "fallback", Target: ch.ID, OwnerID: ownerID, Detail: map[string]any{"reason": reason, "channelId": ch.ID, "channelName": ch.Name}})

	// Decrypt the channel api_key transparently before forwarding (vault-crypto-3).
	cp := &ChannelProxy{Body: body, Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: s.Cipher.DecryptOrPlaintext(ch.ApiKey)}, IdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second}
	st, err := cp.OpenStream(ctx, ch.ID)
	if err != nil {
		return Outcome{}, false
	}
	if st.Status >= 400 {
		_ = st.Body.Close()
		return Outcome{}, false
	}
	CopyForwardableHeaders(w.Header(), st.Header)
	w.WriteHeader(st.Status)
	start := time.Now()
	ttfb, sseBody := flushCopyCapture(w, st.Body, start)
	_ = st.Body.Close()
	in, out, cacheRead, cache5m, cache1h := parseUsageSSE(sseBody)
	cost := billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
	// Record the channel's last-observed balance so the spend row reflects
	// the balance at dispatch time (fallback-5: write observed balance).
	_ = s.Q.UpsertFallbackSpend(ctx, sqlc.UpsertFallbackSpendParams{ChannelID: ch.ID, Day: todayDayStr(), Requests: 1, EstCostUsd: cost, BalanceObserved: ch.BalanceUsd})
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		Status: "ok", HttpStatus: int32(st.Status), FallbackReason: reason,
		LatencyMs: time.Since(start).Milliseconds(), TtfbMs: ttfb,
		TokensIn: in, TokensOut: out, Stream: true, CostUsd: cost,
	})
	return Outcome{Status: st.Status, Target: "fallback:" + ch.ID, Reason: reason}, true
}
