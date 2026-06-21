package dispatch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"

	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/events"
	"github.com/qwwqq1000-arch/tower/internal/fallback"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// Service assembles policy + selection + proxy + orchestration into one call.
type Service struct {
	Q     *sqlc.Queries
	Store *state.Store
	Base  policy.Config
	Now   func() int64
}

// NewService builds a dispatch Service.
func NewService(q *sqlc.Queries, store *state.Store, base policy.Config, now func() int64) *Service {
	return &Service{Q: q, Store: store, Base: base, Now: now}
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
	cfg := s.resolveConfig(ctx)
	breaker := state.BreakerCfg{
		PersistStreak: cfg.BanPersistStreak, BaseMs: cfg.CooldownBaseMs,
		MaxMs: cfg.CooldownMaxMs, Mult: cfg.CooldownMult,
	}

	order, resolver := s.buildCandidates(ctx, ownerID, cfg.MaxConcurrent)
	channels := s.enabledChannels(ctx, ownerID)

	est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
	trig := fallback.Decide(fallback.DecideInput{
		Model: model, BodyText: bodyText, EstCostUsd: est, PoolEmpty: len(order) == 0,
		Keywords: nil, FallbackModels: nil, PriceThresholdUsd: cfg.FallbackPriceThresholdUsd,
		ProbeEnabled: false,
	})

	// Fallback-first when triggered (and channels exist).
	if cfg.FallbackEnabled && trig != fallback.None && trig != fallback.Exhausted && len(channels) > 0 {
		return s.viaChannel(ctx, ownerID, model, body, channels[0], string(trig))
	}

	// Our nodes.
	if len(order) > 0 {
		orch := &Orchestrator{Store: s.Store, Cfg: breaker, CooldownMin: cfg.SlotCooldownMinMs, CooldownMax: cfg.SlotCooldownMaxMs, MaxAttempts: 50}
		np := &NodeProxy{Body: body, Resolve: resolver, BanSignals: cfg.BanSignals, BanKeywords: cfg.BanKeywords}
		res, ok := orch.Dispatch(ctx, model, order, np)
		if ok {
			s.logOK(ctx, ownerID, model, res, "")
			return Outcome{Status: res.Status, Body: res.Body, Target: "node", Reason: ""}
		}
		// our pool failed → fallback backstop
		if cfg.FallbackEnabled && len(channels) > 0 {
			return s.viaChannel(ctx, ownerID, model, body, channels[0], "exhausted")
		}
		s.logErr(ctx, ownerID, model, res.Status, "")
		return Outcome{Status: 503, Body: `{"error":"all accounts exhausted"}`, Target: "node", Reason: ""}
	}

	// No nodes at all → fallback if possible.
	if cfg.FallbackEnabled && len(channels) > 0 {
		return s.viaChannel(ctx, ownerID, model, body, channels[0], "exhausted")
	}
	s.logErr(ctx, ownerID, model, 503, "no_nodes")
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

func (s *Service) buildCandidates(ctx context.Context, ownerID string, capacity int) ([]string, Resolver) {
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
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		accs, err := s.Q.ListNodeAccountsByNode(ctx, n.ID)
		if err != nil {
			continue
		}
		for _, a := range accs {
			if !a.Enabled {
				continue
			}
			key := n.ID + ":" + a.ProfileID
			refs[key] = NodeRef{BaseURL: n.BaseUrl, APIKey: n.ApiKey, ProfileID: a.ProfileID}
			s.Store.Ensure(key, capacity)
			cands = append(cands, cand{key: key, weight: int(a.Weight)})
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

func (s *Service) viaChannel(ctx context.Context, ownerID, model string, body []byte, ch sqlc.FallbackChannel, reason string) Outcome {
	cp := &ChannelProxy{Body: body, Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: ch.ApiKey}}
	res, err := cp.Send(ctx, ch.ID)
	if err != nil {
		s.logErr(ctx, ownerID, model, 502, reason)
		return Outcome{Status: 502, Body: `{"error":"fallback channel error"}`, Target: "fallback", Reason: reason}
	}
	status := "ok"
	if res.Status < 200 || res.Status >= 300 {
		status = "error"
	}
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		ProfileID: "", Status: status, HttpStatus: int32(res.Status), FallbackReason: reason,
	})
	return Outcome{Status: res.Status, Body: res.Body, Target: "fallback:" + ch.ID, Reason: reason}
}

func (s *Service) logOK(ctx context.Context, ownerID, model string, res ProxyResult, reason string) {
	in, out := parseUsage(res.Body)
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "node", ProfileID: "",
		Status: "ok", HttpStatus: int32(res.Status), TokensIn: in, TokensOut: out,
		FallbackReason: reason,
	})
	if in > 0 || out > 0 {
		_ = s.Q.AddCostRollup(ctx, sqlc.AddCostRollupParams{
			ScopeType: "owner", ScopeID: ownerID, Day: "", Model: model,
			Requests: 1, TokensIn: in, TokensOut: out, CostUsd: billing.CostUsd(model, in, out, 0, 0),
		})
	}
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "dispatch_ok", Target: "node", OwnerID: ownerID})
}

func (s *Service) logErr(ctx context.Context, ownerID, model string, status int, reason string) {
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "node", ProfileID: "",
		Status: "error", HttpStatus: int32(status), FallbackReason: reason,
	})
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
	order, resolver := s.buildCandidates(ctx, ownerID, cfg.MaxConcurrent)

	attempts := 0
	for _, key := range order {
		if attempts >= 50 {
			break
		}
		attempts++
		ok, trial := s.Store.TryDispatchTrial(key, model, breaker)
		if !ok {
			continue
		}
		out, committed := s.streamOne(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model)
		if committed {
			return out
		}
		// not committed → failed before first byte → failover
	}

	// exhausted → fallback channel stream, else 503
	channels := s.enabledChannels(ctx, ownerID)
	if cfg.FallbackEnabled && len(channels) > 0 {
		if out, committed := s.streamChannel(ctx, w, channels[0], body, ownerID, model, "exhausted"); committed {
			return out
		}
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":"no nodes available"}`))
	s.logErr(ctx, ownerID, model, 503, "")
	return Outcome{Status: 503, Target: "none"}
}

// streamOne attempts one account. committed=true means we wrote to the client
// (success) and the caller must not fail over.
func (s *Service) streamOne(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string) (Outcome, bool) {
	settled := false
	settle := func(success bool) {
		if settled {
			return
		}
		settled = true
		s.Store.Complete(key, cfg.SlotCooldownMinMs, cfg.SlotCooldownMaxMs)
		if trial {
			s.Store.OnTrialResult(key, breaker, success)
		} else if success {
			s.Store.OnSuccess(key)
		} else {
			s.Store.OnBanSignal(key, breaker)
		}
	}
	defer func() { settle(false) }()

	np := &NodeProxy{Body: body, Resolve: resolver, BanSignals: cfg.BanSignals}
	st, err := np.OpenStream(ctx, key)
	if err != nil {
		return Outcome{}, false // connection error → failover
	}
	if st.Banned || st.Status >= 400 {
		_ = st.Body.Close()
		return Outcome{}, false // bad status before first byte → settle(false) via defer, failover
	}
	// commit: stream to client
	CopyForwardableHeaders(w.Header(), st.Header)
	w.WriteHeader(st.Status)
	flushCopy(w, st.Body)
	_ = st.Body.Close()
	settle(true)
	s.logOK(ctx, ownerID, model, ProxyResult{Status: st.Status}, "")
	return Outcome{Status: st.Status, Target: "node"}, true
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
	flushCopy(w, st.Body)
	_ = st.Body.Close()
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		Status: "ok", HttpStatus: int32(st.Status), FallbackReason: reason,
	})
	return Outcome{Status: st.Status, Target: "fallback:" + ch.ID, Reason: reason}, true
}
