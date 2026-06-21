package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/billing"
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

	// scaledUp tracks owners for which reserve accounts were last activated,
	// to deduplicate scale_up / scale_down events.
	scaledUpMu sync.Mutex
	scaledUp   map[string]bool
}

// NewService builds a dispatch Service.
func NewService(q *sqlc.Queries, store *state.Store, base policy.Config, now func() int64) *Service {
	return &Service{Q: q, Store: store, Base: base, Now: now, sess: session.NewStore(), scaledUp: make(map[string]bool)}
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

	cfg := s.resolveConfig(ctx)
	breaker := state.BreakerCfg{
		PersistStreak: cfg.BanPersistStreak, BaseMs: cfg.CooldownBaseMs,
		MaxMs: cfg.CooldownMaxMs, Mult: cfg.CooldownMult,
	}

	order, resolver := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID)

	conv := session.ConvID(body)
	nowMs := s.Now()

	est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
	probeText := lastUserText(body)
	trig := fallback.Decide(fallback.DecideInput{
		Model: model, BodyText: bodyText, ProbeText: probeText, EstCostUsd: est, PoolEmpty: len(order) == 0,
		Keywords: cfg.FallbackKeywords, FallbackModels: cfg.FallbackModels, PriceThresholdUsd: cfg.FallbackPriceThresholdUsd,
		ProbeEnabled: cfg.FallbackProbeEnabled,
	})

	// Fallback-first when triggered (and channels exist).
	if cfg.FallbackEnabled && trig != fallback.None && trig != fallback.Exhausted && len(channels) > 0 {
		return s.viaChannel(ctx, ownerID, model, body, channels[0], string(trig), time.Since(start).Milliseconds())
	}

	// PRE-FLIGHT: session exile check — route exiled conversations to fallback.
	if cfg.SessionErrorThreshold > 0 && s.sess.Exiled(conv, nowMs) && cfg.FallbackEnabled && len(channels) > 0 {
		return s.viaChannel(ctx, ownerID, model, body, channels[0], "session", time.Since(start).Milliseconds())
	}

	// Our nodes.
	if len(order) > 0 {
		maxFailover := cfg.MaxFailover
		if maxFailover <= 0 {
			maxFailover = 50
		}
		orch := &Orchestrator{Store: s.Store, Cfg: breaker, CooldownMin: cfg.SlotCooldownMinMs, CooldownMax: cfg.SlotCooldownMaxMs, MaxAttempts: maxFailover, OnBan: s.recordBan,
			OnAttempt: func(key string, status int, ok bool, banned bool) {
				if !ok {
					s.logAttemptErr(ctx, ownerID, model, key, status)
				}
			},
		}
		np := &NodeProxy{Body: body, Resolve: resolver, BanSignals: cfg.BanSignals, BanKeywords: cfg.BanKeywords}
		res, winKey, ok := orch.Dispatch(ctx, model, order, np)
		if ok {
			// Response exile: if the response body contains a safety-refusal keyword,
			// exile this conversation and re-serve via fallback if possible.
			if cfg.ResponseExileEnabled && matchesAny(res.Body, cfg.ResponseExileKeywords) {
				s.sess.ForceExile(conv, int64(cfg.SessionCooldownSec)*1000, nowMs)
				if len(channels) > 0 {
					return s.viaChannel(ctx, ownerID, model, body, channels[0], "cyber", time.Since(start).Milliseconds())
				}
				// No fallback channel — log and return the original response.
				s.logOK(ctx, ownerID, model, res, winKey, time.Since(start).Milliseconds(), "cyber")
				return Outcome{Status: res.Status, Body: res.Body, Target: winKey, Reason: "cyber"}
			}
			s.sess.RecordSuccess(conv)
			s.logOK(ctx, ownerID, model, res, winKey, time.Since(start).Milliseconds(), "")
			return Outcome{Status: res.Status, Body: res.Body, Target: winKey, Reason: ""}
		}
		// our pool failed → record error for session tracking
		s.sess.RecordError(conv, int64(cfg.SessionErrorThreshold), int64(cfg.SessionCooldownSec)*1000, nowMs)
		// fallback backstop
		if cfg.FallbackEnabled && len(channels) > 0 {
			return s.viaChannel(ctx, ownerID, model, body, channels[0], "exhausted", time.Since(start).Milliseconds())
		}
		s.logErr(ctx, ownerID, model, res.Status, 0, "")
		return Outcome{Status: 503, Body: `{"error":"all accounts exhausted"}`, Target: "node", Reason: ""}
	}

	// No nodes at all → fallback if possible.
	if cfg.FallbackEnabled && len(channels) > 0 {
		return s.viaChannel(ctx, ownerID, model, body, channels[0], "exhausted", time.Since(start).Milliseconds())
	}
	s.logErr(ctx, ownerID, model, 503, 0, "no_nodes")
	return Outcome{Status: 503, Body: `{"error":"no nodes available"}`, Target: "none", Reason: ""}
}

func (s *Service) resolveConfig(ctx context.Context) policy.Config {
	rows, err := s.Q.ListPolicies(ctx)
	if err != nil {
		return s.Base
	}
	var patches []policy.Patch
	for _, r := range rows {
		if r.ScopeType == "global" {
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil {
				patches = append(patches, p)
			}
		}
	}
	return policy.Resolve(s.Base, patches...)
}

// pickElastic selects the ordered candidate list given the elastic configuration.
// It is a pure function so it can be table-tested independently.
// baseline and reserve are already sorted weight-desc.
// util is baseline inflight / baseline capacity (1.0 when capacity == 0).
// Returns the ordered slice to use.
func pickElastic(baseline, reserve []string, util, threshold float64, maxReserve int) []string {
	if util >= threshold {
		n := len(reserve)
		if maxReserve > 0 && n > maxReserve {
			n = maxReserve
		}
		out := make([]string, 0, len(baseline)+n)
		out = append(out, baseline...)
		out = append(out, reserve[:n]...)
		return out
	}
	return baseline
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

func (s *Service) buildCandidates(ctx context.Context, ownerID, model string, cfg policy.Config) ([]string, Resolver) {
	nodes, err := s.Q.ListNodesByOwner(ctx, ownerID)
	if err != nil || len(nodes) == 0 {
		nodes, _ = s.Q.ListNodes(ctx)
	}
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
			refs[key] = NodeRef{BaseURL: n.BaseUrl, APIKey: n.ApiKey, ProfileID: na.ProfileID}
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
		return order, resolver
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

	threshold := cfg.ElasticScaleUpUtil
	order := pickElastic(baseline, reserve, util, threshold, cfg.ElasticMaxReserve)

	// Deduplicated scale_up / scale_down events.
	nReserve := len(order) - len(baseline)
	if nReserve < 0 {
		nReserve = 0
	}
	s.scaledUpMu.Lock()
	wasScaled := s.scaledUp[ownerID]
	if nReserve > 0 && !wasScaled {
		s.scaledUp[ownerID] = true
		s.scaledUpMu.Unlock()
		_ = events.Record(ctx, s.Q, nowMs, events.Event{
			Type:    "scale_up",
			Target:  fmt.Sprintf("reserves=%d", nReserve),
			OwnerID: ownerID,
		})
	} else if nReserve == 0 && wasScaled {
		delete(s.scaledUp, ownerID)
		s.scaledUpMu.Unlock()
		_ = events.Record(ctx, s.Q, nowMs, events.Event{
			Type:    "scale_down",
			Target:  "reserves=0",
			OwnerID: ownerID,
		})
	} else {
		s.scaledUpMu.Unlock()
	}

	resolver := func(key string) (NodeRef, bool) { r, ok := refs[key]; return r, ok }
	return order, resolver
}

func (s *Service) enabledChannels(ctx context.Context, _ string) []sqlc.FallbackChannel {
	chs, err := s.Q.ListEnabledFallbackChannels(ctx)
	if err != nil {
		return nil
	}
	return chs
}

// fbSlotKey returns the store key for a fallback channel's concurrency slot.
func fbSlotKey(id string) string { return "fb:" + id }

func (s *Service) viaChannel(ctx context.Context, ownerID, model string, body []byte, ch sqlc.FallbackChannel, reason string, latencyMs int64) Outcome {
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "fallback", Target: reason, OwnerID: ownerID})
	cap := int(ch.MaxConcurrent)
	if cap <= 0 {
		cap = 1000
	}
	key := fbSlotKey(ch.ID)
	s.Store.Ensure(key, cap)
	bk := state.BreakerCfg{PersistStreak: 1 << 30, BaseMs: 0, MaxMs: 0, Mult: 1}
	occupied := s.Store.TryDispatch(key, model, bk)
	if occupied {
		defer s.Store.Complete(key, int64(ch.CooldownMs), int64(ch.CooldownMs))
	}

	cp := &ChannelProxy{Body: body, Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: ch.ApiKey}}
	res, err := cp.Send(ctx, ch.ID)
	if err != nil {
		s.logErr(ctx, ownerID, model, 502, latencyMs, reason)
		return Outcome{Status: 502, Body: `{"error":"fallback channel error"}`, Target: "fallback", Reason: reason}
	}
	status := "ok"
	if res.Status < 200 || res.Status >= 300 {
		status = "error"
	}
	var cost float64
	if status == "ok" {
		in, out, cacheRead, cache5m, cache1h := parseUsage(res.Body)
		cost = billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
		_ = s.Q.UpsertFallbackSpend(ctx, sqlc.UpsertFallbackSpendParams{ChannelID: ch.ID, Day: todayDayStr(), Requests: 1, EstCostUsd: cost, BalanceObserved: 0})
	}
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		ProfileID: "", Status: status, HttpStatus: int32(res.Status), LatencyMs: latencyMs, FallbackReason: reason,
		Stream: false, CostUsd: cost,
	})
	return Outcome{Status: res.Status, Body: res.Body, Target: "fallback:" + ch.ID, Reason: reason}
}

func (s *Service) logOK(ctx context.Context, ownerID, model string, res ProxyResult, key string, latencyMs int64, reason string) {
	in, out, cacheRead, cache5m, cache1h := parseUsage(res.Body)
	cost := billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: "ok", HttpStatus: int32(res.Status), LatencyMs: latencyMs, TokensIn: in, TokensOut: out,
		FallbackReason: reason, TtfbMs: latencyMs, Stream: false, CostUsd: cost,
	})
	if in > 0 || out > 0 {
		_ = s.Q.AddCostRollup(ctx, sqlc.AddCostRollupParams{
			ScopeType: "owner", ScopeID: ownerID, Day: "", Model: model,
			Requests: 1, TokensIn: in, TokensOut: out, CostUsd: cost,
		})
	}
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "dispatch_ok", Target: key, OwnerID: ownerID})
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
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: httpStatus, HttpStatus: int32(status), LatencyMs: latencyMs,
		TokensIn: in, TokensOut: out, TtfbMs: ttfbMs, Stream: true, CostUsd: cost,
	})
	if in > 0 || out > 0 {
		_ = s.Q.AddCostRollup(ctx, sqlc.AddCostRollupParams{
			ScopeType: "owner", ScopeID: ownerID, Day: "", Model: model,
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
	cfg := s.resolveConfig(ctx)
	breaker := state.BreakerCfg{
		PersistStreak: cfg.BanPersistStreak, BaseMs: cfg.CooldownBaseMs,
		MaxMs: cfg.CooldownMaxMs, Mult: cfg.CooldownMult,
	}
	order, resolver := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID)

	conv := session.ConvID(body)
	nowMs := s.Now()

	// Probe/keyword/model fallback decision — same logic as non-streaming Dispatch.
	est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
	probeText := lastUserText(body)
	trig := fallback.Decide(fallback.DecideInput{
		Model: model, BodyText: string(body), ProbeText: probeText, EstCostUsd: est, PoolEmpty: len(order) == 0,
		Keywords: cfg.FallbackKeywords, FallbackModels: cfg.FallbackModels, PriceThresholdUsd: cfg.FallbackPriceThresholdUsd,
		ProbeEnabled: cfg.FallbackProbeEnabled,
	})
	if cfg.FallbackEnabled && trig != fallback.None && trig != fallback.Exhausted && len(channels) > 0 {
		if out, committed := s.streamChannel(ctx, w, channels[0], body, ownerID, model, string(trig)); committed {
			return out
		}
	}

	// PRE-FLIGHT: session exile check — route exiled conversations to fallback.
	if cfg.SessionErrorThreshold > 0 && s.sess.Exiled(conv, nowMs) && cfg.FallbackEnabled && len(channels) > 0 {
		if out, committed := s.streamChannel(ctx, w, channels[0], body, ownerID, model, "session"); committed {
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
		out, committed, sseBody := s.streamOneWithBody(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model)
		if committed {
			// Response exile: scan body for safety-refusal keywords.
			if cfg.ResponseExileEnabled && matchesAny(sseBody, cfg.ResponseExileKeywords) {
				s.sess.ForceExile(conv, int64(cfg.SessionCooldownSec)*1000, nowMs)
				// Cannot re-serve mid-stream — exile only affects future requests.
			} else {
				s.sess.RecordSuccess(conv)
			}
			return out
		}
		// not committed → failed before first byte → log per-attempt error + failover
		s.logAttemptErr(ctx, ownerID, model, key, out.Status)
		s.sess.RecordError(conv, int64(cfg.SessionErrorThreshold), int64(cfg.SessionCooldownSec)*1000, nowMs)
	}

	// exhausted → fallback channel stream, else 503
	if cfg.FallbackEnabled && len(channels) > 0 {
		if out, committed := s.streamChannel(ctx, w, channels[0], body, ownerID, model, "exhausted"); committed {
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
func (s *Service) streamOneWithBody(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string) (Outcome, bool, string) {
	out, committed, sseBody := s.streamOneInternal(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model)
	return out, committed, sseBody
}

// streamOne attempts one account. committed=true means we wrote to the client
// (success) and the caller must not fail over.
func (s *Service) streamOne(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string) (Outcome, bool) {
	out, committed, _ := s.streamOneInternal(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model)
	return out, committed
}

// streamOneInternal is the actual implementation; streamOne and streamOneWithBody are thin wrappers.
func (s *Service) streamOneInternal(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string) (Outcome, bool, string) {
	settled := false
	sendReturned := false
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
			s.Store.OnTrialResult(key, breaker, success)
			if !success {
				s.recordBan(key)
			}
		} else if success {
			s.Store.OnSuccess(key)
		} else {
			if s.Store.OnBanSignal(key, breaker) {
				s.recordBan(key)
			}
		}
	}
	defer func() { settle(false) }()

	np := &NodeProxy{Body: body, Resolve: resolver, BanSignals: cfg.BanSignals}
	st, err := np.OpenStream(ctx, key)
	sendReturned = true
	if err != nil {
		return Outcome{}, false, "" // connection error → failover
	}
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
	settle(true)
	in, out, cacheRead, cache5m, cache1h := parseUsageSSE(sseBody)
	total := time.Since(start).Milliseconds()
	s.logStream(ctx, ownerID, model, key, st.Status, in, out, cacheRead, cache5m, cache1h, total, ttfb)
	return Outcome{Status: st.Status, Target: key}, true, sseBody
}

// recordBan records a ban episode and event for the given key (node:profile).
func (s *Service) recordBan(key string) {
	node, profile, found := splitKey(key)
	if !found {
		return
	}
	ctx := context.Background()
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "ban", Target: key})
	_ = s.Q.InsertBanEpisode(ctx, sqlc.InsertBanEpisodeParams{NodeID: node, ProfileID: profile, BannedAt: s.Now(), Detail: []byte("{}")})
}

func splitKey(key string) (node, profile string, ok bool) {
	i := strings.LastIndex(key, ":")
	if i < 0 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
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
func (s *Service) streamChannel(ctx context.Context, w http.ResponseWriter, ch sqlc.FallbackChannel, body []byte, ownerID, model, reason string) (Outcome, bool) {
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "fallback", Target: reason, OwnerID: ownerID})
	cap := int(ch.MaxConcurrent)
	if cap <= 0 {
		cap = 1000
	}
	key := fbSlotKey(ch.ID)
	s.Store.Ensure(key, cap)
	bk := state.BreakerCfg{PersistStreak: 1 << 30, BaseMs: 0, MaxMs: 0, Mult: 1}
	occupied := s.Store.TryDispatch(key, model, bk)
	if occupied {
		defer s.Store.Complete(key, int64(ch.CooldownMs), int64(ch.CooldownMs))
	}

	cp := &ChannelProxy{Body: body, Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: ch.ApiKey}}
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
	_ = s.Q.UpsertFallbackSpend(ctx, sqlc.UpsertFallbackSpendParams{ChannelID: ch.ID, Day: todayDayStr(), Requests: 1, EstCostUsd: cost, BalanceObserved: 0})
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		Status: "ok", HttpStatus: int32(st.Status), FallbackReason: reason,
		LatencyMs: time.Since(start).Milliseconds(), TtfbMs: ttfb,
		TokensIn: in, TokensOut: out, Stream: true, CostUsd: cost,
	})
	return Outcome{Status: st.Status, Target: "fallback:" + ch.ID, Reason: reason}, true
}
