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
}

// NewService builds a dispatch Service.
func NewService(q *sqlc.Queries, store *state.Store, base policy.Config, now func() int64) *Service {
	return &Service{Q: q, Store: store, Base: base, Now: now, sess: session.NewStore()}
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
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

func parseUsage(body string) (int64, int64) {
	var u usage
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		return 0, 0
	}
	return u.Usage.InputTokens, u.Usage.OutputTokens
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
	trig := fallback.Decide(fallback.DecideInput{
		Model: model, BodyText: bodyText, EstCostUsd: est, PoolEmpty: len(order) == 0,
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
		orch := &Orchestrator{Store: s.Store, Cfg: breaker, CooldownMin: cfg.SlotCooldownMinMs, CooldownMax: cfg.SlotCooldownMaxMs, MaxAttempts: maxFailover, OnBan: s.recordBan}
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

func (s *Service) buildCandidates(ctx context.Context, ownerID, model string, cfg policy.Config) ([]string, Resolver) {
	nodes, err := s.Q.ListNodesByOwner(ctx, ownerID)
	if err != nil || len(nodes) == 0 {
		nodes, _ = s.Q.ListNodes(ctx)
	}
	refs := map[string]NodeRef{}
	type cand struct {
		key    string
		weight int
	}
	var cands []cand
	nowMs := s.Now()
	isOpus := strings.Contains(strings.ToLower(model), "opus")
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
			// Apply or clear warmup cap.
			if inWarmup {
				s.Store.SetWarmupCap(key, cfg.WarmupMaxConcurrent)
			} else {
				s.Store.SetWarmupCap(key, 0)
			}
			cands = append(cands, cand{key: key, weight: int(na.Weight)})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].weight > cands[j].weight })
	order := make([]string, len(cands))
	for i, c := range cands {
		order[i] = c.key
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

func (s *Service) viaChannel(ctx context.Context, ownerID, model string, body []byte, ch sqlc.FallbackChannel, reason string, latencyMs int64) Outcome {
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
		in, out := parseUsage(res.Body)
		cost = billing.CostUsd(model, in, out, 0, 0)
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
	in, out := parseUsage(res.Body)
	cost := billing.CostUsd(model, in, out, 0, 0)
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

// parseUsageSSE extracts input/output token counts from an Anthropic SSE body.
// input_tokens appears in message_start; output_tokens last value is in message_delta.
func parseUsageSSE(body string) (in, out int64) {
	reIn := regexp.MustCompile(`"input_tokens":\s*(\d+)`)
	reOut := regexp.MustCompile(`"output_tokens":\s*(\d+)`)

	if m := reIn.FindStringSubmatch(body); len(m) == 2 {
		fmt.Sscanf(m[1], "%d", &in)
	}
	// use last match for output_tokens
	all := reOut.FindAllStringSubmatch(body, -1)
	if len(all) > 0 {
		fmt.Sscanf(all[len(all)-1][1], "%d", &out)
	}
	return in, out
}

// logStream logs a completed streaming dispatch with TTFB and token counts.
func (s *Service) logStream(ctx context.Context, ownerID, model, key string, status int, in, out, latencyMs, ttfbMs int64) {
	httpStatus := "ok"
	if status < 200 || status >= 300 {
		httpStatus = "error"
	}
	cost := billing.CostUsd(model, in, out, 0, 0)
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
		// not committed → failed before first byte → record error + failover
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
		_ = st.Body.Close()
		return Outcome{}, false, "" // bad status before first byte → settle(false) via defer, failover
	}
	// commit: stream to client
	start := time.Now()
	CopyForwardableHeaders(w.Header(), st.Header)
	w.WriteHeader(st.Status)
	ttfb, sseBody := flushCopyCapture(w, st.Body, start)
	_ = st.Body.Close()
	settle(true)
	in, out := parseUsageSSE(sseBody)
	total := time.Since(start).Milliseconds()
	s.logStream(ctx, ownerID, model, key, st.Status, in, out, total, ttfb)
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

// streamChannel attempts a fallback channel stream. committed=true means we wrote to the client.
func (s *Service) streamChannel(ctx context.Context, w http.ResponseWriter, ch sqlc.FallbackChannel, body []byte, ownerID, model, reason string) (Outcome, bool) {
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
	in, out := parseUsageSSE(sseBody)
	cost := billing.CostUsd(model, in, out, 0, 0)
	_ = s.Q.UpsertFallbackSpend(ctx, sqlc.UpsertFallbackSpendParams{ChannelID: ch.ID, Day: todayDayStr(), Requests: 1, EstCostUsd: cost, BalanceObserved: 0})
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		Status: "ok", HttpStatus: int32(st.Status), FallbackReason: reason,
		LatencyMs: time.Since(start).Milliseconds(), TtfbMs: ttfb,
		TokensIn: in, TokensOut: out, Stream: true, CostUsd: cost,
	})
	return Outcome{Status: st.Status, Target: "fallback:" + ch.ID, Reason: reason}, true
}
