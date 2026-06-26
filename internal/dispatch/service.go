package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/cpaclient"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/events"
	"github.com/qwwqq1000-arch/tower/internal/fallback"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/session"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

const (
	spendFallbackRecoveryMs int64 = 18_000_000 // 5h fallback when quota classify unavailable
)

// spendThresholdEntry holds the persisted per-account spending threshold T and
// the calendar day (nowMs/86400000) it was anchored on.
type spendThresholdEntry struct {
	threshold float64
	day       int64 // calendar day bucket (nowMs / 86400000)
}

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

	// policyVer is a monotonic version counter bumped on every policy write.
	// policyCache holds per-(ownerID,accountID) cached resolved configs keyed
	// by ownerID+"|"+accountID; entries are invalidated when policyVer changes.
	policyVer   atomic.Int64
	policyCache sync.Map // key: string → cachedPolicyCfg

	// keyAccount maps a dispatch key (nodeID:profileID) → the business accountID
	// (na.AccountID, matching policies-table scope_id for account scope). Populated
	// in buildCandidates and read in recordSpend so per-account policy overrides are
	// reachable at points where only the key is known. key→accountId is stable, so
	// a shared lock-free map is safe.
	keyAccount sync.Map // key: string (nodeID:profileID) → string (accountID)

	// quotaInFlight guards against multiple concurrent quota-classify goroutines for
	// the same account key (e.g. burst of 429s). Only one goroutine runs at a time.
	quotaInFlight sync.Map // key: string → struct{}

	// spendThreshold holds the in-memory per-account cumulative spend threshold T for
	// the raising-threshold model. Persisted to account_spend_threshold on change.
	// Value type: spendThresholdEntry.
	spendThreshold sync.Map // key: string (dispatch key) → spendThresholdEntry

	// spendLimitedAt tracks the threshold at which the account was last limited, to
	// detect recovery and raise the bar exactly once per cycle. Value type: float64.
	spendLimitedAt sync.Map // key: string → float64
}

// NewService builds a dispatch Service. cipher is the runtime master-key cipher
// (vault-crypto-1) used to decrypt secrets at use; it may be nil.
func NewService(q *sqlc.Queries, store *state.Store, base policy.Config, now func() int64, cipher *crypto.Cipher) *Service {
	return &Service{Q: q, Store: store, Base: base, Now: now, sess: session.NewStore(), Cipher: cipher, scaledUp: make(map[string]bool)}
}

// RestoreSpendThresholds loads persisted spend thresholds from DB into the in-memory map.
// Called once at startup. todaySpend is ephemeral (resets to 0 on restart — intended).
// If the stored day matches today, the threshold is restored so the bar is not re-anchored
// to T₀ mid-cycle. If the day has changed, the next recordSpend will re-anchor naturally.
func (s *Service) RestoreSpendThresholds(ctx context.Context) {
	if s.Q == nil {
		return
	}
	today := s.Now() / 86_400_000
	rows, err := s.Q.ListAccountSpendThresholds(ctx)
	if err != nil {
		log.Printf("RestoreSpendThresholds: %v", err)
		return
	}
	for _, r := range rows {
		s.spendThreshold.Store(r.Key, spendThresholdEntry{threshold: r.Threshold, day: r.Day})
		_ = today // day-mismatch is handled lazily in loadSpendThreshold
	}
	if len(rows) > 0 {
		log.Printf("restored %d spend threshold(s) from DB", len(rows))
	}
}

// effectiveCap returns the maximum concurrency for a single account, accounting
// for the SerialQueue feature. When serial=true the cap is forced to 1 regardless
// of maxc; otherwise maxc is returned unchanged. This is the dispatch-layer
// equivalent of min(cfg.MaxConcurrent, 1) and is the core of serial-queue
// behaviour (disguise-phase4).
//
// SerialQueueEnabled forces cap=1 AND enables bounded slot-wait (SerialQueueWaitMs):
// when the single slot is busy the dispatcher waits up to SerialQueueWaitMs ms for
// it to free before failing over. Both behaviours are now implemented.
func effectiveCap(serial bool, maxc int) int {
	if serial {
		return 1
	}
	return maxc
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

// terminalError reports whether a response is a deterministic terminal failure
// that must be returned immediately to the client without retrying other accounts
// or falling over to the fallback channel. Matches when status==400 AND the body
// contains any TerminalErrorKeywords substring (case-insensitive). Empty keywords
// → feature off → never terminal.
func terminalError(status int, body string, cfg policy.Config) bool {
	if status != 400 {
		return false
	}
	if len(cfg.TerminalErrorKeywords) == 0 {
		return false
	}
	lb := strings.ToLower(body)
	for _, kw := range cfg.TerminalErrorKeywords {
		if kw != "" && strings.Contains(lb, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// directFallbackMatch reports whether a failed attempt's response should route
// straight to fallback (skipping remaining account attempts): status is in the
// configured codes AND the body contains a configured keyword. Empty codes or
// empty keywords → never matches (feature off).
func directFallbackMatch(status int, body string, cfg policy.Config) bool {
	if len(cfg.DirectFallbackStatusCodes) == 0 || len(cfg.DirectFallbackKeywords) == 0 {
		return false
	}
	hit := false
	for _, c := range cfg.DirectFallbackStatusCodes {
		if c == status {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	lb := strings.ToLower(body)
	for _, kw := range cfg.DirectFallbackKeywords {
		if kw != "" && strings.Contains(lb, strings.ToLower(kw)) {
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

// reqMaxTokens extracts the requested output cap (max_tokens) from a Messages
// request body, or 0 when absent/unparseable.
func reqMaxTokens(body []byte) int {
	var p struct {
		MaxTokens int `json:"max_tokens"`
	}
	_ = json.Unmarshal(body, &p)
	return p.MaxTokens
}

// maxTokensError builds the 400 body for an over-limit request.
func maxTokensError(req, limit int, model string) string {
	return fmt.Sprintf(`{"error":{"type":"invalid_request_error","message":"max_tokens %d exceeds the %d output-token limit for %s"}}`, req, limit, model)
}

var limitResetRe = regexp.MustCompile(`(?i)resets?\s+(\d{1,2}):(\d{2})\s*(am|pm)\s*\(?\s*utc\s*\)?`)

// limitResetDateRe matches the 7-day quota wording that carries a date, e.g.
// "resets Jun 28 12:50pm (UTC)" — parsed before the time-only regex (which would
// otherwise grab "12:50pm" and resolve it to today).
var limitResetDateRe = regexp.MustCompile(`(?i)resets?\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})\s+(\d{1,2}):(\d{2})\s*(am|pm)\s*\(?\s*utc\s*\)?`)

var monthAbbr = map[string]time.Month{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// parseLimitReset detects a usage-EXHAUSTION response and returns the reset deadline
// (account-limit-reactive). Detection is error + KEYWORD: the body must contain one of
// the configured QuotaLimitKeywords (precise phrases like "hit your limit"). A bare
// error or a transient `rate_limit_error` must NOT limit the account — that previously
// false-limited accounts. The new-meridian node wraps the claude.ai limit as a 500
// api_error "...You've hit your limit · resets H:MMam/pm (UTC)"; the reset lives only in
// that text, so we parse the wall-clock UTC time to the next occurrence (else 1h).
// Returns (false, 0) when no keyword matches.
func parseLimitReset(status int, body string, now int64, keywords []string, codes []int, defaultResetMs int64) (bool, int64) {
	// Gate by status code first so the body scan does not run on every response — only
	// the codes a quota limit actually arrives on (429/500). Empty codes = scan all.
	if len(codes) > 0 {
		ok := false
		for _, c := range codes {
			if c == status {
				ok = true
				break
			}
		}
		if !ok {
			return false, 0
		}
	}
	lb := strings.ToLower(body)
	matched := false
	for _, kw := range keywords {
		if kw != "" && strings.Contains(lb, strings.ToLower(kw)) {
			matched = true
			break
		}
	}
	if !matched {
		return false, 0
	}
	// Date+time reset (7-day quota: "resets Jun 28 12:50pm (UTC)") — try first, since
	// it also contains a H:MM the time-only regex would otherwise misread as today.
	if m := limitResetDateRe.FindStringSubmatch(body); m != nil {
		mon := monthAbbr[strings.ToLower(m[1])]
		day, _ := strconv.Atoi(m[2])
		hh, _ := strconv.Atoi(m[3])
		mm, _ := strconv.Atoi(m[4])
		if strings.EqualFold(m[5], "pm") && hh != 12 {
			hh += 12
		} else if strings.EqualFold(m[5], "am") && hh == 12 {
			hh = 0
		}
		t := time.UnixMilli(now).UTC()
		reset := time.Date(t.Year(), mon, day, hh, mm, 0, 0, time.UTC)
		if reset.Before(t) {
			reset = reset.AddDate(1, 0, 0) // month/day already passed this year → next year
		}
		return true, reset.UnixMilli()
	}
	// Explicit wall-clock reset (subscription format) → resolve to next occurrence.
	if m := limitResetRe.FindStringSubmatch(body); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		if strings.EqualFold(m[3], "pm") && hh != 12 {
			hh += 12
		} else if strings.EqualFold(m[3], "am") && hh == 12 {
			hh = 0
		}
		t := time.UnixMilli(now).UTC()
		reset := time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, time.UTC)
		if !reset.After(t) {
			reset = reset.Add(24 * time.Hour) // wall-clock already passed today → next day
		}
		return true, reset.UnixMilli()
	}
	// Keyword matched but no explicit reset time (e.g. CPA "cooling down") → default
	// (QuotaLimitDefaultResetMs, typically 5 min). These are transient cooldowns that
	// recover fast, so a long block would over-rotate; the account re-limits on the
	// next attempt if still limited (self-correcting, no polling) and recovers as soon
	// as it stops returning it.
	return true, now + defaultResetMs
}

const maxDetailBodyBytes = 64 * 1024

// redactedHeaderKeys carry the dispatch key / cookies and are masked before the
// request detail is stored — secrets must never reach the log-detail view.
var redactedHeaderKeys = map[string]bool{
	"Authorization": true, "X-Api-Key": true, "Cookie": true, "Proxy-Authorization": true,
}

// maskSecret fully redacts a secret-bearing header value for the log-detail view.
// It reveals NO characters — even a short prefix of a tenant's own dispatch key is
// needless exposure (security-audit MED-2).
func maskSecret(string) string {
	return "****"
}

// redactHeadersJSON serializes the client headers for the log-detail view with
// secret-bearing values masked (logs-detail-1).
func redactHeadersJSON(h http.Header) string {
	out := map[string]string{}
	for k, vs := range h {
		v := strings.Join(vs, ", ")
		if redactedHeaderKeys[http.CanonicalHeaderKey(k)] {
			v = maskSecret(v)
		}
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// writeRequestDetail stores the request's body (capped) + redacted headers once,
// keyed by the ctx request id, for the log "view request" feature (logs-detail-1).
func (s *Service) writeRequestDetail(ctx context.Context, ownerID string, body []byte) {
	rid := requestIDFrom(ctx)
	if rid == "" {
		return
	}
	bodyStr := string(body)
	if len(bodyStr) > maxDetailBodyBytes {
		bodyStr = bodyStr[:maxDetailBodyBytes] + "\n…[truncated]"
	}
	_ = s.Q.UpsertDispatchLogDetail(ctx, sqlc.UpsertDispatchLogDetailParams{
		RequestID: rid, OwnerID: ownerID, Ts: s.Now(),
		ReqBody: bodyStr, ReqHeaders: redactHeadersJSON(clientHeadersFrom(ctx)),
	})
}

// appendDetailResponse appends one response segment (capped) to the ctx request's
// detail and updates the latest status. The underlying query APPENDS, so a failed-over
// request accumulates every attempt's error plus the final outcome (logs-detail-3).
func (s *Service) appendDetailResponse(ctx context.Context, status int, segment string) {
	rid := requestIDFrom(ctx)
	if rid == "" {
		return
	}
	if len(segment) > maxDetailBodyBytes {
		segment = segment[:maxDetailBodyBytes] + "\n…[truncated]"
	}
	_ = s.Q.UpdateDispatchLogDetailResponse(ctx, sqlc.UpdateDispatchLogDetailResponseParams{
		RequestID: rid, RespStatus: int32(status), RespBody: segment,
	})
}

// UpdateRequestDetailResponse records the FINAL response status + body (capped) for the
// ctx request, so the log-detail modal can show WHY a request ended as it did — the
// actual response/error (logs-detail-2). Called by the HTTP handler after
// Dispatch/DispatchStream returns. No-op if no request id or the detail expired.
func (s *Service) UpdateRequestDetailResponse(ctx context.Context, status int, body string) {
	s.appendDetailResponse(ctx, status, fmt.Sprintf("── 响应 → HTTP %d ──\n%s", status, body))
}


// applyReactiveLimit rotates an account out of dispatch until the reset time parsed
// from a usage-limit RESPONSE (account-limit-reactive), and records the event. The
// account auto-recovers when the limit expires — no polling. This is the primary
// rotation signal; the periodic quota poll no longer rotates accounts.
func (s *Service) applyReactiveLimit(ctx context.Context, ownerID, key string, resetMs int64) {
	s.Store.SetLimited(key, s.Base.MaxConcurrent, map[string]int64{"all": resetMs})
	s.persistLimit(ctx, key, resetMs, "reactive")
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{
		Type: "quota_limited", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "resetsAt": resetMs, "source": "response"},
	})
}

// persistLimit persists a quota-limit state to the DB so it survives restart.
// Best-effort: DB errors are logged and never bubble up to the caller.
func (s *Service) persistLimit(ctx context.Context, key string, untilMs int64, reason string) {
	if err := s.Q.UpsertAccountLimitState(ctx, sqlc.UpsertAccountLimitStateParams{
		Key: key, LimitedUntil: untilMs, LimitReason: reason, UpdatedAt: s.Now(),
	}); err != nil {
		log.Printf("persistLimit %s: %v", key, err)
	}
}

// clearPersistedLimit removes a key's persisted limit row (best-effort).
func (s *Service) clearPersistedLimit(ctx context.Context, key string) {
	if err := s.Q.DeleteAccountLimitState(ctx, key); err != nil {
		log.Printf("clearPersistedLimit %s: %v", key, err)
	}
}

// overCap reports whether spend sum has reached or exceeded the configured cap.
// capUsd <= 0 means disabled; returns false in that case (no cap hit possible).
func overCap(sum, capUsd float64) bool {
	return capUsd > 0 && sum >= capUsd
}

// loadSpendThreshold returns the current threshold T for key. If absent or the
// stored day differs from today (day = nowMs/86400000), it re-anchors to the
// initial draw (salt "thr:0") and returns the new T. Persists on change (best-effort).
func (s *Service) loadSpendThreshold(ctx context.Context, key string, cfg policy.Config, now int64, spendBasis float64) float64 {
	today := now / 86_400_000
	if v, ok := s.spendThreshold.Load(key); ok {
		e := v.(spendThresholdEntry)
		if e.day == today {
			return e.threshold
		}
	}
	// First time or day changed → anchor at the spend-so-far (spendBasis, the today-spend
	// BEFORE the in-flight cost) + a fresh [min,max] budget, so the threshold always sits
	// one draw ABOVE current spend. Re-anchoring mid-day (or after a config change) then
	// gives the account a real budget instead of tripping instantly because cumulative
	// spend already passed a smaller absolute value. At day start spendBasis≈0 → T≈draw.
	t := spendBasis + cfg.SpendCap5hUsd.Resolve(key, "thr:0")
	s.spendThreshold.Store(key, spendThresholdEntry{threshold: t, day: today})
	s.persistSpendThreshold(ctx, key, t, today, now)
	s.spendLimitedAt.Delete(key) // clear stale limitedAt marker on day reset
	return t
}

// raiseSpendThreshold sets the next threshold bar to today's spend + a fresh [min,max]
// draw and persists it. Returns the new threshold. Anchoring at current spend (rather
// than currentT) makes each post-recovery budget a real [min,max] above where the
// account actually is.
func (s *Service) raiseSpendThreshold(ctx context.Context, key string, cfg policy.Config, currentT float64, now int64, spendBasis float64) float64 {
	today := now / 86_400_000
	next := cfg.SpendCap5hUsd.Resolve(key, fmt.Sprintf("thr:%d", int64(currentT)))
	// Anchor the next bar at the spend-so-far (spendBasis) + a fresh [min,max] budget, so
	// after a recovery the account always gets a real budget above where it actually is,
	// never re-tripping instantly when cumulative spend already overshot the old bar.
	newT := spendBasis + next
	s.spendThreshold.Store(key, spendThresholdEntry{threshold: newT, day: today})
	s.persistSpendThreshold(ctx, key, newT, today, now)
	return newT
}

// persistSpendThreshold upserts the threshold row (best-effort, no-op if Q is nil).
func (s *Service) persistSpendThreshold(ctx context.Context, key string, threshold float64, day, now int64) {
	if s.Q == nil {
		return
	}
	if err := s.Q.UpsertAccountSpendThreshold(ctx, sqlc.UpsertAccountSpendThresholdParams{
		Key:       key,
		Threshold: threshold,
		Day:       day,
		UpdatedAt: now,
	}); err != nil {
		log.Printf("persistSpendThreshold %s: %v", key, err)
	}
}

// recordSpend accumulates spend for key and, if the cumulative today-spend reaches
// the raising threshold T, rotates the account out of dispatch via SetLimited.
// Recovery (quota resets_at) raises T by one cap-draw so the next stop is higher.
// Best-effort: panics are caught so spend tracking never fails the request.
func (s *Service) recordSpend(ctx context.Context, ownerID, key string, cost float64) {
	defer func() { recover() }() //nolint:errcheck
	// Resolve per-account spend-cap config.
	accountID := ""
	if v, ok := s.keyAccount.Load(key); ok {
		accountID, _ = v.(string)
	}
	cfg := s.resolveConfig(ctx, ownerID, accountID)
	if !cfg.SpendCap5hEnabled {
		return // zero overhead when cap is off (default)
	}
	now := s.Now()
	// Capture today's spend BEFORE adding this cost — that is the basis a fresh threshold
	// anchors on (current spend + a [min,max] budget), so the in-flight cost doesn't get
	// absorbed into the new budget.
	spendBasis := s.Store.SpendToday(key, now)
	s.Store.AddSpend(key, cost, now)

	// Load current threshold T (re-anchors at spendBasis + draw if day changed/first time).
	T := s.loadSpendThreshold(ctx, key, cfg, now, spendBasis)

	// Check whether this account is currently limited.
	isLimited := s.Store.IsLimited(key, now)

	// If currently limited → skip re-firing. But check for threshold raise on recovery.
	if isLimited {
		return
	}

	// Not currently limited. Check if we need to raise the bar (just recovered).
	if latV, ok := s.spendLimitedAt.Load(key); ok {
		latT := latV.(float64)
		if latT == T {
			// Recovered from the latT cycle → raise T to spendBasis + a fresh budget.
			T = s.raiseSpendThreshold(ctx, key, cfg, T, now, spendBasis)
			s.spendLimitedAt.Delete(key)
		}
	}

	// Check today spend against current threshold.
	todaySpend := s.Store.SpendToday(key, now)
	if !overCap(todaySpend, T) {
		return // under threshold — nothing to do
	}

	// todaySpend >= T: limit the account.
	s.spendLimitedAt.Store(key, T) // remember which T triggered this

	fetchCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	reason, recovMs, src := s.spendLimitRecovery(fetchCtx, key, cfg, now)
	cancel()
	if reason == "" {
		// Quota couldn't be fetched → fixed +5h fallback (last resort only).
		reason, recovMs, src = "5h", now+spendFallbackRecoveryMs, "spend5h_fixedfallback"
	}
	// recovMs is the account's REAL 5h-window reset (design: spend-cap recovery aligns to
	// the live quota's 5h reset, not a fixed +5h), or a genuinely-full window's reset, or
	// the fixed fallback only when the quota couldn't be fetched at all.
	s.Store.SetLimitedReason(key, s.Base.MaxConcurrent, recovMs, reason)
	s.persistLimit(ctx, key, recovMs, reason)
	_ = events.Record(ctx, s.Q, now, events.Event{
		Type: "quota_limited", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "resetsAt": recovMs, "source": src},
	})
}

// insertLog stamps the ctx request id onto the row so every log row of a request
// links to its stored detail, then inserts it.
func (s *Service) insertLog(ctx context.Context, p sqlc.InsertDispatchLogParams) {
	p.RequestID = requestIDFrom(ctx)
	_ = s.Q.InsertDispatchLog(ctx, p)
}

// Dispatch routes one request: fallback decision → our nodes (failover) →
// fallback backstop, logging and cost-rolling the outcome.
func (s *Service) Dispatch(ctx context.Context, ownerID, model, bodyText string, body []byte) Outcome {
	start := time.Now()
	s.writeRequestDetail(ctx, ownerID, body)

	cfg := s.resolveConfig(ctx, ownerID, "")
	// Per-model max_tokens ceiling: reject an over-limit request 400 BEFORE any
	// node/fallback attempt — it fails on every upstream, so retrying wastes
	// attempts (limits-1).
	if limit := cfg.MaxTokensFor(model); limit > 0 {
		if req := reqMaxTokens(body); req > limit {
			s.logErr(ctx, ownerID, model, 400, time.Since(start).Milliseconds(), "max_tokens_exceeded")
			return Outcome{Status: 400, Body: maxTokensError(req, limit, model), Target: "none", Reason: "max_tokens_exceeded"}
		}
	}
	breaker := state.BreakerCfg{
		PersistStreak: cfg.BanPersistStreak, PermStreak: cfg.PermanentBanStreak,
		BaseMs: cfg.CooldownBaseMs, MaxMs: cfg.CooldownMaxMs, Mult: cfg.CooldownMult,
	}

	order, allKeys, resolver, keyOwner, keyCfg := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID, model)

	// Envelope gate (CCEnvelopeEnabled): check whether the request carries the
	// expected three-piece envelope.  Missing pieces → fallback or inject.
	if cfg.CCEnvelopeEnabled {
		if miss := missingEnvelopePieces(cfg, body, requestQueryFrom(ctx), clientHeadersFrom(ctx)); len(miss) > 0 {
			action := cfg.CCEnvelopeAction
			if action == "" {
				action = "fallback"
			}
			if action == "fallback" {
				// Mirror the exhausted-pool fallback path — same call, reason "envelope".
				if cfg.FallbackEnabled && len(channels) > 0 {
					return s.viaChannels(ctx, ownerID, model, body, channels, "envelope", time.Since(start).Milliseconds(), cfg)
				}
				s.logErr(ctx, ownerID, model, 503, 0, "envelope_no_channel")
				return Outcome{Status: 503, Body: `{"error":"envelope incomplete, no fallback channel"}`, Target: "none", Reason: "envelope_no_channel"}
			}
			// "complete": stash the inject-set so proxy.Send can apply it.
			ctx = withEnvelopeInject(ctx, miss)
		}
	}

	conv := session.ConvID(body)
	nowMs := s.Now()

	// BodyPad (disguise-phase4): inject padding into metadata.pad before dispatch.
	// Guard: only active when explicitly enabled AND BodyPadBytes resolves to > 0.
	// Default BodyPadEnabled=false + BodyPadBytes={0,0} → this block never executes.
	// padBody is always safe: any error returns the original body unchanged.
	if cfg.BodyPadEnabled {
		n := int(cfg.BodyPadBytes.Resolve(conv, "bodypad"))
		body = padBody(body, n, conv)
	}

	// TIER 1: keyword + model — highest priority, before affinity.
	if cfg.FallbackEnabled && len(channels) > 0 {
		if fallback.MatchesKeyword(bodyText, cfg.FallbackKeywords) {
			return s.viaChannels(ctx, ownerID, model, body, channels, "keyword", time.Since(start).Milliseconds(), cfg)
		}
		if fallback.MatchesModel(model, cfg.FallbackModels) {
			return s.viaChannels(ctx, ownerID, model, body, channels, "model", time.Since(start).Milliseconds(), cfg)
		}
	}

	// TIER 2: affinity pinning.
	// Resolve affinity pin against allKeys (full candidate set) NOT just the elastic
	// baseline — so a reserve account that was pinned in a prior turn is reachable
	// even when elastic has it off the active working set (affinity > elastic, Fix 2).
	_, pinActive := s.sess.Affinity(conv, nowMs)
	pinned := false
	affinityWaitKey := ""
	if cfg.AffinityTTLSec > 0 && pinActive {
		pinnedOrder := s.pinToAffinity(conv, allKeys, nowMs)
		if len(pinnedOrder) == 1 {
			// Found the pinned account in the full set → force it as the sole candidate.
			order = pinnedOrder
			affinityWaitKey = pinnedOrder[0]
		} else {
			// Pinned account absent anywhere → empty list → falls through to exhausted fallback.
			order = nil
		}
		pinned = true
	}

	// TIER 3: soft rules (probe, price) + session-exile — only when NOT pinned.
	if !pinned {
		est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
		probeText := lastUserText(body)
		var chPriceThreshold float64
		if len(channels) > 0 {
			chPriceThreshold = channels[0].PriceThreshold
		}
		softTrig := fallback.DecideSoft(fallback.DecideInput{
			Model: model, BodyText: bodyText, ProbeText: probeText, EstCostUsd: est, PoolEmpty: len(order) == 0,
			Keywords: cfg.FallbackKeywords, FallbackModels: cfg.FallbackModels,
			PriceThresholdUsd: fallback.EffectivePriceThreshold(chPriceThreshold, cfg.FallbackPriceThresholdUsd),
			ProbeEnabled: cfg.FallbackProbeEnabled,
		})
		if cfg.FallbackEnabled && softTrig != fallback.None && softTrig != fallback.Exhausted && len(channels) > 0 {
			return s.viaChannels(ctx, ownerID, model, body, channels, string(softTrig), time.Since(start).Milliseconds(), cfg)
		}
		if (cfg.SessionErrorThreshold > 0 || cfg.ResponseExileEnabled) && s.sess.Exiled(conv, nowMs) && cfg.FallbackEnabled && len(channels) > 0 {
			return s.viaChannels(ctx, ownerID, model, body, channels, "session", time.Since(start).Milliseconds(), cfg)
		}
	}

	// Our nodes.
	if len(order) > 0 {
		maxFailover := cfg.MaxFailover
		if maxFailover <= 0 {
			maxFailover = 50
		}
		// Build per-key serial-wait maps from per-account config (SerialQueueEnabled + WaitMs).
		// Zero overhead when all accounts have SerialQueueEnabled=false (default).
		serialWaitKeys := map[string]bool{}
		serialWaitMs := map[string]int64{}
		for _, k := range order {
			if ac, ok := keyCfg[k]; ok && ac.SerialQueueEnabled && ac.SerialQueueWaitMs > 0 {
				serialWaitKeys[k] = true
				serialWaitMs[k] = int64(ac.SerialQueueWaitMs)
			}
		}
		// Affinity wait: bounded slot-wait for the pinned account before failover.
		if affinityWaitKey != "" && cfg.AffinityWaitMs > 0 {
			serialWaitKeys[affinityWaitKey] = true
			if int64(cfg.AffinityWaitMs) > serialWaitMs[affinityWaitKey] {
				serialWaitMs[affinityWaitKey] = int64(cfg.AffinityWaitMs)
			}
		}
		humanDist := "uniform"
		if cfg.HumanDelayEnabled {
			humanDist = cfg.HumanDelayDist
		}
		orch := &Orchestrator{Store: s.Store, Cfg: breaker, CooldownMin: cfg.SlotCooldownMinMs, CooldownMax: cfg.SlotCooldownMaxMs, CooldownDist: humanDist, CooldownP50: cfg.HumanDelayP50Ms, CooldownP95: cfg.HumanDelayP95Ms, MaxAttempts: maxFailover,
			OnBan:     func(key string, status int) { s.recordBan(ctx, acctOwnerOf(keyOwner, key, ownerID), key, status) },
			OnRecover: func(key string) { s.recordRecover(ctx, key) },
			OnAttempt: func(key string, res ProxyResult, ok bool) {
				if !ok {
					s.logAttempt(ctx, ownerID, model, key, res.Status, false, res.Body) // own row + own detail (retry-log-clarity)
					s.recordRetry(ctx, acctOwnerOf(keyOwner, key, ownerID), model, key, res.Status, res.Banned)
					s.maybeCooldown(ctx, acctOwnerOf(keyOwner, key, ownerID), key, res.Status, cfg)
					// Reactive quota rotation: if the response is a usage-limit error,
					// rotate the account out until the reset time parsed from it.
					if limited, resetMs := parseLimitReset(res.Status, res.Body, s.Now(), cfg.QuotaLimitKeywords, cfg.QuotaLimitStatusCodes, cfg.QuotaLimitDefaultResetMs); limited {
						s.applyReactiveLimit(ctx, acctOwnerOf(keyOwner, key, ownerID), key, resetMs)
					}
				}
			},
			IsCooldownSignal:    func(status int) bool { return isCooldownSignal(status, cfg) },
			NowMs:               s.Now,
			SerialWaitKeys:      serialWaitKeys,
			SerialWaitMs:        serialWaitMs,
			DirectFallback:      func(res ProxyResult) bool { return directFallbackMatch(res.Status, res.Body, cfg) },
			Terminal:            func(res ProxyResult) bool { return terminalError(res.Status, res.Body, cfg) },
			RetryDelayMs:        cfg.RetryDelayMs,
			RetrySameAccountMax: cfg.RetrySameAccountMax,
		}
		np := &NodeProxy{Body: body, Resolve: resolver, BanSignals: cfg.BanSignals, BanKeywords: cfg.BanKeywords, IdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second, UpstreamTimeoutSec: cfg.UpstreamTimeoutSec}
		res, winKey, ok := orch.Dispatch(ctx, model, order, np)
		if ok {
			// Response exile: if the response body contains a safety-refusal keyword,
			// exile this conversation and re-serve via fallback if possible.
			if cfg.ResponseExileEnabled && matchesAny(res.Body, cfg.ResponseExileKeywords) {
				if justExiled := s.sess.ForceExile(conv, int64(cfg.SessionCooldownSec)*1000, nowMs); justExiled {
					_ = events.Record(ctx, s.Q, nowMs, events.Event{Type: "session_exile", Target: "cyber", OwnerID: ownerID})
				}
				// The node account (winKey) DID serve a real upstream request here even
				// though we re-route the client to fallback — count it toward the rate
				// governor windows, matching the no-channel branch below. Without this
				// the rate-governor undercounts when ResponseExile + RateGov overlap.
				if cfg.RateGovEnabled {
					s.Store.RecordReq(winKey)
				}
				if len(channels) > 0 {
					return s.viaChannels(ctx, ownerID, model, body, channels, "cyber", time.Since(start).Milliseconds(), cfg)
				}
				// No fallback channel — log and return the original response.
				s.logOK(ctx, ownerID, model, res, winKey, time.Since(start).Milliseconds(), "cyber", pinned)
				return Outcome{Status: res.Status, Body: res.Body, Target: winKey, Reason: "cyber"}
			}
			s.sess.RecordSuccess(conv)
			if cfg.AffinityTTLSec > 0 {
				s.sess.SetAffinity(conv, winKey, int64(cfg.AffinityTTLSec)*1000, nowMs)
			}
			if cfg.ModelPinEnabled && cfg.ModelPinMode == "sticky" {
				if wasNew := s.Store.RecordModel(winKey, model, int64(cfg.AffinityTTLSec)*1000); wasNew && cfg.ModelElasticEnabled {
					s.recordModelPin(ctx, acctOwnerOf(keyOwner, winKey, ownerID), winKey, model)
				}
			}
			if cfg.RateGovEnabled {
				s.Store.RecordReq(winKey)
			}
			scfg := keyCfg[winKey]
			if scfg.SessionSimEnabled {
				target := int(scfg.SessionBurstCount.Resolve(winKey, "burst"))
				s.Store.BurstTick(winKey)
				if s.Store.BurstShouldPause(winKey, target) {
					pause := scfg.SessionPauseMs.Resolve(winKey, "pause")
					s.Store.SetLimited(winKey, scfg.MaxConcurrent, map[string]int64{"all": nowMs + pause})
					s.Store.BurstReset(winKey)
				}
			}
			s.logOK(ctx, ownerID, model, res, winKey, time.Since(start).Milliseconds(), "", pinned)
			return Outcome{Status: res.Status, Body: res.Body, Target: winKey, Reason: ""}
		}
		// Terminal 400: deterministic invalid_request_error — return immediately to client,
		// no retry, no fallback. The error will be identical on every account and fallback.
		if terminalError(res.Status, res.Body, cfg) {
			s.logErr(ctx, ownerID, model, res.Status, time.Since(start).Milliseconds(), "terminal_error")
			return Outcome{Status: res.Status, Body: res.Body, Target: "node", Reason: "terminal_error"}
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

// pinToAffinity enforces STRICT session affinity (account-affinity-A): once a
// conversation is pinned to an account it is served ONLY by that account — never a
// second node account. The pinned account becomes the sole node candidate; if it is
// set but absent from the candidate list (gone/filtered/rotated out), the node list
// is emptied so the request falls through to fallback (daodun) instead of hopping to
// another node account — which would break thinking-block signatures and spread one
// conversation across multiple accounts (a ban pattern). A conversation with no pin
// yet (first turn or expired TTL) keeps the full list for normal load-balanced
// selection, then SetAffinity pins the winner. Enforces policy.AffinityTTLSec.
func (s *Service) pinToAffinity(conv string, order []string, now int64) []string {
	if conv == "" {
		return order
	}
	key, ok := s.sess.Affinity(conv, now)
	if !ok {
		return order // no pin yet → normal selection
	}
	for _, k := range order {
		if k == key {
			return []string{key} // pinned account present → sole candidate
		}
	}
	return nil // pinned account unavailable → force fallback, never hop to another node account
}

// cachedPolicyCfg is a version-stamped resolved policy config entry.
type cachedPolicyCfg struct {
	ver int64
	cfg policy.Config
}

// BumpPolicyVersion invalidates all cached resolveConfig entries by advancing
// the version counter. Must be called after any policy write (upsert/delete).
func (s *Service) BumpPolicyVersion() { s.policyVer.Add(1) }

// resolveConfig resolves the effective 封控 policy for the given dispatch owner by
// applying the global layer first, then the owner's (tenant) layer, then the
// account layer over it, so later layers win. ownerID=="" (admin/unowned dispatch
// key) has no tenant layer. accountID=="" means no account layer is applied.
//
// Results are cached per (ownerID, accountID) and invalidated when BumpPolicyVersion
// is called, eliminating the per-request ListPolicies full-table scan on the hot path.
// Reads are lock-free (sync.Map + atomic.Int64); concurrent rebuilds after a bump are
// idempotent and safe.
func (s *Service) resolveConfig(ctx context.Context, ownerID, accountID string) policy.Config {
	ver := s.policyVer.Load()
	key := ownerID + "|" + accountID
	if v, ok := s.policyCache.Load(key); ok {
		if entry := v.(cachedPolicyCfg); entry.ver == ver {
			return entry.cfg
		}
	}

	rows, err := s.Q.ListPolicies(ctx)
	if err != nil {
		return s.Base
	}
	var gp, op, ap *policy.Patch
	for _, r := range rows {
		switch {
		case r.ScopeType == "global":
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil {
				gp = &p
			}
		case ownerID != "" && r.ScopeType == "owner" && r.ScopeID == ownerID:
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil {
				op = &p
			}
		case accountID != "" && r.ScopeType == "account" && r.ScopeID == accountID:
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil {
				ap = &p
			}
		}
	}
	patches := make([]policy.Patch, 0, 3)
	if gp != nil {
		patches = append(patches, *gp)
	}
	if op != nil {
		patches = append(patches, *op)
	}
	if ap != nil {
		patches = append(patches, *ap)
	}
	cfg := policy.Resolve(s.Base, patches...)
	s.policyCache.Store(key, cachedPolicyCfg{ver: ver, cfg: cfg})
	return cfg
}

// ResolveConfigForOwner returns the effective policy.Config for the given owner
// (global + owner-scope patches applied, no per-account patch). Exposed for callers
// outside the dispatch package (e.g. the status handler).
func (s *Service) ResolveConfigForOwner(ctx context.Context, ownerID string) policy.Config {
	return s.resolveConfig(ctx, ownerID, "")
}

// slotActiveNow reports whether the given [startMin, endMin) window (minute-of-day,
// in tzName timezone) is active at the instant represented by nowMs (Unix ms).
// If start == end or the window is [0, 1440) it is treated as always-active.
// Overnight windows (start > end) are active when cur >= start OR cur < end.
func slotActiveNow(startMin, endMin int, nowMs int64, tzName string) bool {
	if startMin == endMin || (startMin == 0 && endMin == 1440) {
		return true // always-active
	}
	loc, err := time.LoadLocation(tzName)
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

// orderIdleFirst sorts keys by current inflight ascending (least-busy first) with
// a random tiebreak so equally-idle accounts share new requests evenly, preventing
// the deterministic-order concentration where order[0] gets all traffic. Stable
// fallback to weight via the caller's pre-sort.
func orderIdleFirst(keys []string, inflightOf map[string]int) {
	rand.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	sort.SliceStable(keys, func(i, j int) bool { return inflightOf[keys[i]] < inflightOf[keys[j]] })
}

// buildCandidates returns:
//   - order: the elastic-filtered dispatch order (baseline only, or baseline+reserve when scaled up).
//   - allKeys: ALL eligible candidate keys in weight-desc order, BEFORE elastic partitioning.
//     Affinity resolution MUST use allKeys so a pinned reserve account is reachable even when
//     elastic is active and the baseline is the working set (affinity > elastic, Fix 2).
//   - resolver, keyOwner, keyCfg: unchanged.
// nodeCreatedMs returns a node's creation time in ms (0 if unset). Used as the elastic
// baseline/reserve seniority tiebreak so a newly-added node's accounts sort to the BACK
// (reserve) rather than jumping into the active baseline and displacing serving accounts.
func nodeCreatedMs(n sqlc.Node) int64 {
	if n.CreatedAt.Valid {
		return n.CreatedAt.Time.UnixMilli()
	}
	return 0
}

func (s *Service) buildCandidates(ctx context.Context, ownerID, model string, cfg policy.Config) (order []string, allKeys []string, resolver Resolver, keyOwner map[string]string, keyCfg map[string]policy.Config) {
	nodes, _ := s.Q.ListNodes(ctx)
	// keyCfg holds the per-account resolved config for each dispatch key.
	// Used by callers to populate per-key SerialWaitKeys/SerialWaitMs on the Orchestrator.
	keyCfg = map[string]policy.Config{}
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
	keyOwner = map[string]string{}
	refs := map[string]NodeRef{}
	type cand struct {
		key       string
		weight    int
		reserve   bool
		createdAt int64
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

	// Quiet hours: compute once whether we are in a quiet window.
	// Used inside the account loop to cap concurrency and RPM.
	inQuiet := false
	if cfg.QuietHoursEnabled && len(cfg.QuietHoursWindows) > 0 {
		loc, _ := time.LoadLocation(cfg.QuietHoursTZ)
		if loc == nil {
			loc = time.UTC
		}
		t := time.UnixMilli(nowMs).In(loc)
		curMin := t.Hour()*60 + t.Minute()
		inQuiet = policy.InAnyWindow(curMin, cfg.QuietHoursWindows)
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
					if !slotActiveNow(sl.startMin, sl.endMin, nowMs, cfg.QuietHoursTZ) {
						continue
					}
				}
				// Unknown slot_id or disabled slot → treat as always-active (don't skip).
			}
			// Resolve the per-account effective config for THIS candidate. With no
			// account-scope policy row this returns exactly cfg, so the change is
			// behavior-neutral unless a per-account override was set (version-cached,
			// so cheap after warmup). Used for this candidate's per-account knobs:
			// warmup, serial-queue/concurrency, quiet-hours RPM/concurrency,
			// rate-governor RPM/RPH/RPD, and model-pin.
			acfg := s.resolveConfig(ctx, ownerID, na.AccountID)
			// Determine warmup state for this account (per-account warmup window).
			var onboardedAt int64
			if acc, aerr := s.Q.GetAccount(ctx, na.AccountID); aerr == nil {
				onboardedAt = acc.OnboardedAt
			}
			inWarmup := acfg.WarmupHours > 0 && onboardedAt > 0 &&
				(nowMs-onboardedAt) < int64(acfg.WarmupHours)*3_600_000
			// Skip opus candidates that are still warming up (if block is enabled).
			if inWarmup && acfg.WarmupBlockOpus && isOpus {
				continue
			}
			key := n.ID + ":" + na.ProfileID
			// Capture per-account config for this key (used by callers to wire SerialWaitKeys/Ms).
			keyCfg[key] = acfg
			// Record the account owner for ban-event attribution (events-audit-3).
			keyOwner[key] = acctOwner[na.AccountID]
			// Record the business accountID for this key so per-account policy
			// overrides are reachable in recordSpend (where only the key is known).
			s.keyAccount.Store(key, na.AccountID)
			// Decrypt the node api_key transparently (vault-crypto-3): ciphertext
			// rows decrypt, legacy plaintext rows pass through unchanged.
			refs[key] = NodeRef{BaseURL: n.BaseUrl, APIKey: s.Cipher.DecryptOrPlaintext(n.ApiKey), ProfileID: na.ProfileID, Kind: n.Kind, Passthrough: n.Passthrough}
			// baseCap: serial-queue cap (1 when enabled, MaxConcurrent otherwise).
			// Quiet hours further reduces this to min(baseCap, QuietHoursConcurrency).
			// Per-account: serial-queue + concurrency knobs resolve from acfg so a
			// per-account override applies to this candidate.
			baseCap := effectiveCap(acfg.SerialQueueEnabled, acfg.MaxConcurrent)
			s.Store.Ensure(key, baseCap)
			cap := baseCap
			if inQuiet && acfg.QuietHoursConcurrency > 0 && acfg.QuietHoursConcurrency < cap {
				cap = acfg.QuietHoursConcurrency
			}
			s.Store.SetCapacity(key, cap)
			// Apply or clear warmup cap (per-account warmup concurrency).
			if inWarmup {
				s.Store.SetWarmupCap(key, acfg.WarmupMaxConcurrent)
			} else {
				s.Store.SetWarmupCap(key, 0)
			}
			// Rate governor: skip account if any rate window is exceeded.
			// Quiet hours adds an additional RPM cap even when RateGovEnabled=false.
			// All rate/quiet magnitude knobs resolve per-account from acfg.
			if acfg.RateGovEnabled || inQuiet {
				// Start with no RPM limit; apply rate-gov limit if enabled.
				var rpm int64
				hasRPMLimit := false
				if acfg.RateGovEnabled {
					rpm = acfg.RateRPM.Resolve(key, "rpm")
					hasRPMLimit = true
				}
				// Overlay quiet-hours RPM cap (takes min).
				if inQuiet {
					qrpm := acfg.QuietHoursRPM.Resolve(key, "qrpm")
					if !hasRPMLimit || qrpm < rpm {
						rpm = qrpm
					}
					hasRPMLimit = true
				}
				// rpm<=0 means "no limit" (matches the UI "0 = 不限"); only enforce when >0.
				if hasRPMLimit && rpm > 0 && int64(s.Store.ReqsInWindow(key, 60000)) >= rpm {
					continue
				}
				// RPH / RPD only when full rate gov is enabled.
				if acfg.RateGovEnabled {
					rph := acfg.RateRPH.Resolve(key, "rph")
					rpd := acfg.RateRPD.Resolve(key, "rpd")
					// rph/rpd<=0 means "no limit" (0 = 不限); only enforce when >0.
					if (rph > 0 && int64(s.Store.ReqsInWindow(key, 3600000)) >= rph) ||
						(rpd > 0 && int64(s.Store.ReqsInWindow(key, 86400000)) >= rpd) {
						continue
					}
				}
			}
			// ModelPin filter. "fixed" is a hard per-account restriction — always applied.
			// "sticky" is applied here in LEGACY mode, but DEFERRED to the model-aware
			// elastic selection below when ModelElasticEnabled+ElasticEnabled are on, so the
			// elastic baseline stays model-agnostic (model-aware-elastic) instead of being
			// shrunk by the model filter (which used to pull reserve accounts into rotation).
			if acfg.ModelPinEnabled {
				switch acfg.ModelPinMode {
				case "fixed":
					if acfg.ModelPinTarget != "" && !strings.Contains(strings.ToLower(model), strings.ToLower(acfg.ModelPinTarget)) {
						continue
					}
				case "sticky":
					if !(cfg.ModelElasticEnabled && cfg.ElasticEnabled) {
						ttl := int64(acfg.AffinityTTLSec) * 1000
						if pm, ok := s.Store.PinnedModel(key, ttl); ok && !strings.Contains(strings.ToLower(model), strings.ToLower(pm)) {
							continue
						}
					}
				}
			}
			cands = append(cands, cand{key: key, weight: int(na.Weight), reserve: na.Role == "reserve", createdAt: nodeCreatedMs(n)})
		}
	}
	// Deterministic order: weight desc, then key asc as tiebreak. Without the key
	// tiebreak, equal-weight accounts (all weight=100 by default) kept the
	// non-deterministic node_accounts row order, so the elastic baseline ("first N")
	// was a DIFFERENT random N each request — reserve accounts randomly received
	// non-affinity traffic and the 待命/活跃 status flickered. A stable tiebreak makes
	// the baseline a fixed working set (affinity-status-fix).
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].weight != cands[j].weight {
			return cands[i].weight > cands[j].weight
		}
		// Seniority tiebreak: older accounts (smaller createdAt) sort first → baseline;
		// newly-added nodes' accounts sort to the BACK → reserve. So a freshly-pushed node
		// never jumps the queue and interrupts accounts already serving in the active set.
		if cands[i].createdAt != cands[j].createdAt {
			return cands[i].createdAt < cands[j].createdAt
		}
		return cands[i].key < cands[j].key
	})

	// Build inflight map from the store snapshot for idle-first ordering.
	// Moved here (before the elastic branch) so both paths can use it.
	snap := s.Store.Snapshot(nowMs)
	inflightOf := make(map[string]int, len(snap))
	for _, sn := range snap {
		inflightOf[sn.Key] = sn.Inflight
	}

	// Build allKeys: every eligible candidate in weight-desc order, BEFORE elastic
	// partitioning. Used by callers to resolve affinity pins against the full
	// candidate set — so a pinned reserve account is reachable even when elastic
	// is active and the baseline is the working set (affinity > elastic, Fix 2).
	allKeysFull := make([]string, len(cands))
	for i, c := range cands {
		allKeysFull[i] = c.key
	}

	if !cfg.ElasticEnabled {
		// Non-elastic path: all accounts, weight-desc (unchanged behaviour).
		ord := make([]string, len(cands))
		for i, c := range cands {
			ord[i] = c.key
		}
		if cfg.IdleFirstSelection {
			orderIdleFirst(ord, inflightOf)
		}
		res := func(key string) (NodeRef, bool) { r, ok := refs[key]; return r, ok }
		return ord, allKeysFull, res, keyOwner, keyCfg
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
	// Cap the reserve pool at ElasticMaxReserve so elastic never activates more than the
	// configured maximum: once that many reserve accounts can be in play, there is no
	// further scale-up. Applied at the source so BOTH the legacy count path AND the
	// model-aware path below honor it (the model-aware path otherwise iterated the full
	// reserve list and appended every eligible one, ignoring the cap). Default 1000 =
	// effectively uncapped.
	if cfg.ElasticMaxReserve > 0 && len(reserve) > cfg.ElasticMaxReserve {
		reserve = reserve[:cfg.ElasticMaxReserve]
	}
	if cfg.IdleFirstSelection {
		orderIdleFirst(baseline, inflightOf)
		orderIdleFirst(reserve, inflightOf)
	}

	// Compute baseline utilisation from the store snapshot (reuse snap already taken above).
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

	// ── Model-aware elastic (ModelElasticEnabled) ───────────────────────────────
	// `order` above is the model-AGNOSTIC active set (baseline, + reserve if scaled by
	// util). Now restrict it to accounts that can serve THIS request's model (pinned to
	// it, or unpinned). If the active set has none, activate reserve accounts FOR this
	// model — preferring reserves already pinned to it, else one fresh reserve (emitting
	// model_scale_up). This keeps model-pin from silently pulling reserve in via the
	// candidate filter, and keeps reserve idle until a model genuinely needs it.
	if cfg.ModelElasticEnabled && cfg.ModelPinEnabled {
		ttl := int64(cfg.AffinityTTLSec) * 1000
		eligible := func(key string) bool {
			pm, ok := s.Store.PinnedModel(key, ttl)
			return !ok || strings.Contains(strings.ToLower(model), strings.ToLower(pm))
		}
		var activeEligible []string
		inActive := make(map[string]bool, len(order))
		for _, k := range order {
			inActive[k] = true
			if eligible(k) {
				activeEligible = append(activeEligible, k)
			}
		}
		// Reserve accounts that can serve this model (pinned to it, or unpinned). These
		// are appended as FAILOVER after the active ones — so when the active accounts
		// fail or are at capacity the request falls over to reserve BEFORE the保底 channel
		// (fixing "reserve idle but 号池耗尽"). Under normal success the orchestrator stops
		// at the first active account, so reserve stays idle.
		var reservePinned, reserveFresh []string
		for _, k := range reserve {
			if inActive[k] {
				continue
			}
			if pm, ok := s.Store.PinnedModel(k, ttl); !ok {
				reserveFresh = append(reserveFresh, k) // unpinned → can take this model
			} else if strings.Contains(strings.ToLower(model), strings.ToLower(pm)) {
				reservePinned = append(reservePinned, k) // already serving this model
			}
		}
		reserveEligible := append(append([]string{}, reservePinned...), reserveFresh...)
		switch {
		case len(activeEligible) > 0:
			// Active set covers this model; reserve trails as failover only.
			order = append(activeEligible, reserveEligible...)
		case len(reserveEligible) > 0:
			// No active account can serve this model → activate reserve for it. Emit
			// model_scale_up only on a FRESH activation (no reserve already pinned to it);
			// once it serves it becomes reservePinned, so the event won't re-fire.
			order = reserveEligible
			if len(reservePinned) == 0 {
				s.recordModelScaleUp(ctx, acctOwnerOf(keyOwner, reserveFresh[0], ownerID), reserveFresh[0], model)
			}
		default:
			// Every account is pinned to OTHER models — surface "not enough accounts for
			// this model" and keep the active order as a last resort (request still tries).
			s.recordModelExhausted(ctx, ownerID, model)
		}
	}

	resolver = func(key string) (NodeRef, bool) { r, ok := refs[key]; return r, ok }
	return order, allKeysFull, resolver, keyOwner, keyCfg
}

// ReserveKeys returns the set of dispatch keys that are currently RESERVE (not in
// the elastic active working set) for the given owner. A key is reserve when:
//   - ElasticEnabled is true, AND
//   - the owner is NOT currently scaled up, AND
//   - the key is beyond the first ElasticBaselineCount accounts in weight-desc order.
//
// Returns an empty map when elastic is off (all accounts treated as active) or when
// the owner is currently scaled up (all reserves are activated). Mirrors the
// partition logic in buildCandidates exactly — including the slot-window filter —
// so the status display's reserve/active split always matches what dispatch actually
// does. Used by the status UI to show 待命 vs 活跃.
func (s *Service) ReserveKeys(ctx context.Context, ownerID string, cfg policy.Config) map[string]bool {
	if !cfg.ElasticEnabled {
		return nil // elastic off → all accounts active
	}
	// Scaled-up state. Do NOT early-return when scaled up: with ElasticMaxReserve only the
	// first MaxReserve reserves are activated; the ones BEYOND the cap stay reserve (待命).
	// The partition below skips just the activated prefix when scaled up.
	s.scaledUpMu.Lock()
	isScaledUp := s.scaledUp[ownerID]
	s.scaledUpMu.Unlock()

	nowMs := s.Now()

	// Quiet-hours flag (mirror buildCandidates lines ~961) — needed for the
	// rate-governor RPM filter below so the status partition matches dispatch.
	inQuiet := false
	if cfg.QuietHoursEnabled && len(cfg.QuietHoursWindows) > 0 {
		loc, _ := time.LoadLocation(cfg.QuietHoursTZ)
		if loc == nil {
			loc = time.UTC
		}
		t := time.UnixMilli(nowMs).In(loc)
		curMin := t.Hour()*60 + t.Minute()
		inQuiet = policy.InAnyWindow(curMin, cfg.QuietHoursWindows)
	}

	// Load slots once, mirroring buildCandidates' slot-window filter.
	// Accounts in inactive slot windows are excluded from dispatch and must therefore
	// also be excluded here so the baseline/reserve partition is identical.
	type slotEntry struct{ startMin, endMin int; enabled bool }
	slotMap := map[string]slotEntry{}
	if slotRows, serr := s.Q.ListSlots(ctx); serr == nil {
		for _, sl := range slotRows {
			slotMap[sl.ID] = slotEntry{startMin: int(sl.StartMin), endMin: int(sl.EndMin), enabled: sl.Enabled}
		}
	}

	// Replicate the candidate enumeration from buildCandidates (owner-scoped, weight-desc).
	type cand struct {
		key       string
		weight    int
		createdAt int64
	}
	var cands []cand
	nodes, _ := s.Q.ListNodes(ctx)
	acctOwner := map[string]string{}
	if ownerRows, err := s.Q.ListAccountOwners(ctx); err == nil {
		for _, row := range ownerRows {
			acctOwner[row.ID] = row.OwnerID
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
			if ownerID != "" && acctOwner[na.AccountID] != ownerID {
				continue
			}
			// Slot-window filter: mirrors buildCandidates exactly.
			// Skip only when the slot exists, is enabled, and the window is inactive.
			if na.SlotID != "" {
				if sl, ok := slotMap[na.SlotID]; ok && sl.enabled {
					if !slotActiveNow(sl.startMin, sl.endMin, nowMs, cfg.QuietHoursTZ) {
						continue
					}
				}
				// Unknown slot_id or disabled slot → treat as always-active (don't skip).
			}
			key := n.ID + ":" + na.ProfileID
			// Rate-governor + quiet-hours RPM filter: mirror buildCandidates (lines
			// ~1047-1077) so an account currently rate-limited OUT of dispatch is also
			// excluded from the baseline/reserve partition — otherwise the 待命/活跃
			// status drifts from the real routing set when the limiter is active.
			// NOTE: model-pin and warmup-opus skips are intentionally NOT mirrored —
			// they depend on the request's model, which the status partition (model-
			// agnostic) has no concept of; exact parity there is structurally impossible.
			acfg := s.resolveConfig(ctx, ownerID, na.AccountID)
			if acfg.RateGovEnabled || inQuiet {
				var rpm int64
				hasRPMLimit := false
				if acfg.RateGovEnabled {
					rpm = acfg.RateRPM.Resolve(key, "rpm")
					hasRPMLimit = true
				}
				if inQuiet {
					qrpm := acfg.QuietHoursRPM.Resolve(key, "qrpm")
					if !hasRPMLimit || qrpm < rpm {
						rpm = qrpm
					}
					hasRPMLimit = true
				}
				if hasRPMLimit && rpm > 0 && int64(s.Store.ReqsInWindow(key, 60000)) >= rpm {
					continue
				}
				if acfg.RateGovEnabled {
					rph := acfg.RateRPH.Resolve(key, "rph")
					rpd := acfg.RateRPD.Resolve(key, "rpd")
					if (rph > 0 && int64(s.Store.ReqsInWindow(key, 3600000)) >= rph) ||
						(rpd > 0 && int64(s.Store.ReqsInWindow(key, 86400000)) >= rpd) {
						continue
					}
				}
			}
			cands = append(cands, cand{key: key, weight: int(na.Weight), createdAt: nodeCreatedMs(n)})
		}
	}
	// Deterministic order: weight desc, then key asc as tiebreak. Without the key
	// tiebreak, equal-weight accounts (all weight=100 by default) kept the
	// non-deterministic node_accounts row order, so the elastic baseline ("first N")
	// was a DIFFERENT random N each request — reserve accounts randomly received
	// non-affinity traffic and the 待命/活跃 status flickered. A stable tiebreak makes
	// the baseline a fixed working set (affinity-status-fix).
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].weight != cands[j].weight {
			return cands[i].weight > cands[j].weight
		}
		// Seniority tiebreak: older accounts (smaller createdAt) sort first → baseline;
		// newly-added nodes' accounts sort to the BACK → reserve. So a freshly-pushed node
		// never jumps the queue and interrupts accounts already serving in the active set.
		if cands[i].createdAt != cands[j].createdAt {
			return cands[i].createdAt < cands[j].createdAt
		}
		return cands[i].key < cands[j].key
	})

	n := cfg.ElasticBaselineCount
	if n < 1 {
		n = 1
	}
	if n > len(cands) {
		n = len(cands)
	}
	// When scaled up, the first ElasticMaxReserve reserve accounts are activated (active);
	// only those BEYOND the cap remain reserve (待命). When not scaled up, every account
	// beyond the baseline is reserve. Mirrors the reserve cap in buildCandidates so the
	// status partition matches dispatch.
	reserveStart := n
	if isScaledUp {
		if cfg.ElasticMaxReserve > 0 {
			reserveStart = n + cfg.ElasticMaxReserve // skip baseline + the activated reserves
		} else {
			reserveStart = len(cands) // uncapped: all reserves active → none shown as 待命
		}
	}
	reserve := make(map[string]bool)
	for i := reserveStart; i < len(cands); i++ {
		reserve[cands[i].key] = true
	}
	return reserve
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

// weightedOrderWithinPriority keeps priority tiers in ascending order but
// shuffles channels WITHIN each tier by weight (weighted-random), so same-
// priority channels share load proportional to weight. Stateless per call.
// Input must already be sorted by priority (ListEnabledFallbackChannels is).
func weightedOrderWithinPriority(channels []sqlc.FallbackChannel) []sqlc.FallbackChannel {
	out := make([]sqlc.FallbackChannel, 0, len(channels))
	for i := 0; i < len(channels); {
		j := i
		for j < len(channels) && channels[j].Priority == channels[i].Priority {
			j++
		}
		out = append(out, weightedShuffle(channels[i:j])...)
		i = j
	}
	return out
}

// weightedShuffle returns a weighted-random permutation: each successive pick
// draws a remaining channel with probability proportional to its weight.
func weightedShuffle(chs []sqlc.FallbackChannel) []sqlc.FallbackChannel {
	if len(chs) <= 1 {
		return chs
	}
	// defensive copy: chs is a sub-slice of the caller's sorted slice; mutating
	// it in-place would corrupt the caller's view of the priority tier.
	remaining := append([]sqlc.FallbackChannel(nil), chs...)
	result := make([]sqlc.FallbackChannel, 0, len(chs))
	for len(remaining) > 0 {
		total := 0
		for _, c := range remaining {
			w := int(c.Weight)
			if w <= 0 { w = 1 }
			total += w
		}
		r := rand.Intn(total)
		idx := 0
		for idx < len(remaining) {
			w := int(remaining[idx].Weight)
			if w <= 0 { w = 1 }
			if r < w { break }
			r -= w
			idx++
		}
		result = append(result, remaining[idx])
		remaining = append(remaining[:idx], remaining[idx+1:]...)
	}
	return result
}

// HasActiveAffinity reports whether any conversation currently has an active
// (non-expired) affinity pin to the given dispatch key. Delegates to the session
// store; used by the status handler to show a reserve account as 亲和 instead of 待命.
func (s *Service) HasActiveAffinity(key string, now int64) bool {
	return s.sess.HasActiveAffinity(key, now)
}

func (s *Service) enabledChannels(ctx context.Context, ownerID string, model string) []sqlc.FallbackChannel {
	chs, err := s.Q.ListEnabledFallbackChannels(ctx)
	if err != nil {
		return nil
	}
	out := chs[:0]
	today := todayDayStr()
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

		// Spend cap: skip (or disable) channels that have exceeded daily/total spend cap.
		// cap=0 means disabled (overCap returns false), so default behavior is unchanged.
		capDaily := policy.RangeF{Min: c.SpendCapDailyMinUsd, Max: c.SpendCapDailyMaxUsd}.Resolve(c.ID, "fbdaily")
		capTotal := policy.RangeF{Min: c.SpendCapTotalMinUsd, Max: c.SpendCapTotalMaxUsd}.Resolve(c.ID, "fbtotal")
		spentTodayRow, _ := s.Q.GetFallbackSpendToday(ctx, sqlc.GetFallbackSpendTodayParams{ChannelID: c.ID, Day: today})
		spentTotalRow, _ := s.Q.GetFallbackSpendTotal(ctx, c.ID)
		if overCap(spentTodayRow.Cost, capDaily) || overCap(spentTotalRow.Cost, capTotal) {
			if c.SpendCapAction == "disable" {
				_ = s.Q.SetFallbackChannelEnabled(ctx, sqlc.SetFallbackChannelEnabledParams{ID: c.ID, Enabled: false})
			}
			continue
		}

		out = append(out, c)
	}
	out = weightedOrderWithinPriority(out)
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
	s.Store.SetCapacity(key, cap) // apply live MaxConcurrent changes (Ensure is create-only) (fallback-cap-1)
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
	cp := &ChannelProxy{Body: body, Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: s.Cipher.DecryptOrPlaintext(ch.ApiKey)}, IdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second, UpstreamTimeoutSec: cfg.UpstreamTimeoutSec}
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
	var in, out, cacheRead, cache5m, cache1h int64
	if status == "ok" {
		in, out, cacheRead, cache5m, cache1h = parseUsage(res.Body)
		cost = billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
		// Record the channel's last-observed balance so the spend row reflects
		// the balance at dispatch time (fallback-5: write observed balance).
		_ = s.Q.UpsertFallbackSpend(ctx, sqlc.UpsertFallbackSpendParams{ChannelID: ch.ID, Day: todayDayStr(), Requests: 1, EstCostUsd: cost, BalanceObserved: ch.BalanceUsd})
	}
	s.insertLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		ProfileID: "", Status: status, HttpStatus: int32(res.Status), LatencyMs: latencyMs, FallbackReason: reason,
		TokensIn: in, TokensOut: out, Stream: false, CostUsd: cost,
		CacheRead: cacheRead, CacheCreation: cache5m + cache1h,
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

func (s *Service) logOK(ctx context.Context, ownerID, model string, res ProxyResult, key string, latencyMs int64, reason string, affinityHit bool) {
	in, out, cacheRead, cache5m, cache1h := parseUsage(res.Body)
	cost := billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
	s.recordSpend(ctx, ownerID, key, cost) // node-account success path: accumulate spend + enforce caps
	if reason == "" && !billing.KnownModel(model) {
		reason = "unknown-model-pricing"
	}
	s.insertLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: "ok", HttpStatus: int32(res.Status), LatencyMs: latencyMs, TokensIn: in, TokensOut: out,
		FallbackReason: reason, TtfbMs: latencyMs, Stream: false, CostUsd: cost,
		CacheRead: cacheRead, CacheCreation: cache5m + cache1h,
		AffinityHit: affinityHit,
	})
	if in > 0 || out > 0 {
		_ = s.Q.AddCostRollup(ctx, sqlc.AddCostRollupParams{
			ScopeType: "owner", ScopeID: ownerID, Day: todayDayStr(), Model: model,
			Requests: 1, TokensIn: in, TokensOut: out, CostUsd: cost,
		})
	}
}

func (s *Service) logErr(ctx context.Context, ownerID, model string, status int, latencyMs int64, reason string) {
	s.insertLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "node", ProfileID: "",
		Status: "error", HttpStatus: int32(status), LatencyMs: latencyMs, FallbackReason: reason,
		Stream: false, CostUsd: 0,
	})
}

// logAttempt logs ONE failed dispatch attempt as its OWN self-consistent log row:
// a fresh request_id and its OWN detail (this attempt's request body + this attempt's
// failure response). It deliberately does NOT reuse the parent request's id/detail —
// so a "error 400 重试" row shows ITS 400, never the eventual 200 of the request that
// later succeeded on another account (retry-log-clarity). IsAttempt=true drives the
// 「重试」 badge. Latency is 0 (no settled TTFB for a failed attempt); stream marks
// the row's request type. The winning/final outcome is logged separately by
// logOK/logErr against the ctx request_id, whose detail is the request + final
// response only (no failed-attempt segments — those live on their own rows now).
func (s *Service) logAttempt(ctx context.Context, ownerID, model, key string, status int, stream bool, respBody string) {
	aid := NewRequestID()
	_ = s.Q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: "error", HttpStatus: int32(status), LatencyMs: 0, FallbackReason: "",
		Stream: stream, CostUsd: 0, IsAttempt: true, RequestID: aid,
	})
	// Own detail = THIS attempt's failure response only. The request body is not
	// duplicated per attempt (it lives on the parent request row) — duplicating it
	// across every failover would bloat the detail store (nexaxis-disk-wal-bloat).
	seg := strings.TrimSpace(respBody)
	if seg == "" {
		seg = "(无响应体 / 首字节前失败)"
	}
	if len(seg) > maxDetailBodyBytes {
		seg = seg[:maxDetailBodyBytes] + "\n…[truncated]"
	}
	_ = s.Q.UpsertDispatchLogDetail(ctx, sqlc.UpsertDispatchLogDetailParams{
		RequestID: aid, OwnerID: ownerID, Ts: s.Now(),
		ReqBody: "(失败尝试 — 请求体见主请求行)", ReqHeaders: "{}",
	})
	_ = s.Q.UpdateDispatchLogDetailResponse(ctx, sqlc.UpdateDispatchLogDetailResponseParams{
		RequestID: aid, RespStatus: int32(status),
		RespBody:  fmt.Sprintf("── 本次失败尝试 %s → HTTP %d ──\n%s", key, status, seg),
	})
}

// firstContentCapBytes caps how much SSE preamble readUntilContent will buffer
// before giving up and committing anyway. A pathological upstream that emits
// hundreds of KB of preamble without a content_block_delta should not stall us.
const firstContentCapBytes = 256 * 1024

// sseHasContentMarker reports whether an SSE buffer has begun streaming real
// content (the first content_block_delta — a text/thinking/tool token).
// message_start and content_block_start alone do NOT count: an upstream that
// emits them then dies still delivers nothing.
func sseHasContentMarker(b []byte) bool {
	return bytes.Contains(b, []byte("content_block_delta"))
}

// readUntilContent reads from src into a prefix buffer until the first content
// event appears (returns prefix, true) or the stream ends/errors before any
// content (returns prefix, false → caller must NOT commit and should fail over).
// capBytes bounds the buffer: an unusually long preamble commits rather than
// buffering unbounded. The caller's src is expected to carry an idle timeout
// (OpenStream wraps st.Body with idleTimeoutReader) so a hung read errors
// instead of blocking forever.
func readUntilContent(src io.Reader, capBytes int) (prefix []byte, hasContent bool) {
	buf := make([]byte, 16*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			prefix = append(prefix, buf[:n]...)
			if sseHasContentMarker(prefix) {
				return prefix, true
			}
			if len(prefix) >= capBytes {
				return prefix, true // pathological long preamble — commit, don't over-buffer
			}
		}
		if err != nil {
			return prefix, false // ended before any content → empty/dead stream
		}
	}
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
func (s *Service) logStream(ctx context.Context, ownerID, model, key string, status int, in, out, cacheRead, cache5m, cache1h, latencyMs, ttfbMs int64, affinityHit bool) {
	httpStatus := "ok"
	if status < 200 || status >= 300 {
		httpStatus = "error"
	}
	cost := billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
	if status >= 200 && status < 300 {
		s.recordSpend(ctx, ownerID, key, cost) // node-account success path: accumulate spend + enforce caps
	}
	streamReason := ""
	if !billing.KnownModel(model) {
		streamReason = "unknown-model-pricing"
	}
	s.insertLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: key, ProfileID: "",
		Status: httpStatus, HttpStatus: int32(status), LatencyMs: latencyMs,
		TokensIn: in, TokensOut: out, TtfbMs: ttfbMs, Stream: true, CostUsd: cost,
		FallbackReason: streamReason,
		CacheRead: cacheRead, CacheCreation: cache5m + cache1h,
		AffinityHit: affinityHit,
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
	s.writeRequestDetail(ctx, ownerID, body)
	cfg := s.resolveConfig(ctx, ownerID, "")
	// Per-model max_tokens ceiling (limits-1): reject 400 before any attempt. The
	// stream handler does not write our return value, so emit the 400 to w here.
	if limit := cfg.MaxTokensFor(model); limit > 0 {
		if req := reqMaxTokens(body); req > limit {
			s.logErr(ctx, ownerID, model, 400, 0, "max_tokens_exceeded")
			msg := maxTokensError(req, limit, model)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(msg))
			return Outcome{Status: 400, Body: msg, Target: "none", Reason: "max_tokens_exceeded"}
		}
	}
	breaker := state.BreakerCfg{
		PersistStreak: cfg.BanPersistStreak, PermStreak: cfg.PermanentBanStreak,
		BaseMs: cfg.CooldownBaseMs, MaxMs: cfg.CooldownMaxMs, Mult: cfg.CooldownMult,
	}
	order, allKeysS, resolver, keyOwner, keyCfgS := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID, model)

	// Envelope gate (CCEnvelopeEnabled): mirror of the Dispatch tier — identical logic.
	if cfg.CCEnvelopeEnabled {
		if miss := missingEnvelopePieces(cfg, body, requestQueryFrom(ctx), clientHeadersFrom(ctx)); len(miss) > 0 {
			action := cfg.CCEnvelopeAction
			if action == "" {
				action = "fallback"
			}
			if action == "fallback" {
				// Mirror the exhausted-pool fallback path — same call, reason "envelope".
				if cfg.FallbackEnabled && len(channels) > 0 {
					exReason := "envelope"
					if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, exReason, cfg); committed {
						return out
					}
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"envelope incomplete, no fallback channel"}`))
				s.logErr(ctx, ownerID, model, 503, 0, "envelope_no_channel")
				return Outcome{Status: 503, Target: "none", Reason: "envelope_no_channel"}
			}
			// "complete": stash the inject-set so proxy.Send can apply it.
			ctx = withEnvelopeInject(ctx, miss)
		}
	}

	conv := session.ConvID(body)
	nowMs := s.Now()

	// Capture original body text BEFORE padding so keyword matching uses the unpadded body
	// (matches the non-stream Dispatch path which uses bodyText before padBody).
	bodyText := string(body)

	// BodyPad (disguise-phase4): inject padding into metadata.pad before dispatch.
	// Guard: only active when explicitly enabled AND BodyPadBytes resolves to > 0.
	// Default BodyPadEnabled=false + BodyPadBytes={0,0} → this block never executes.
	// padBody is always safe: any error returns the original body unchanged.
	if cfg.BodyPadEnabled {
		n := int(cfg.BodyPadBytes.Resolve(conv, "bodypad"))
		body = padBody(body, n, conv)
	}

	// TIER 1: keyword + model — highest priority, before affinity.
	// Terminal: if a keyword or model rule matches, the request MUST go to channels.
	// If channels are at capacity/error (committed=false), return 503 rather than
	// falling through to node accounts — mirroring the non-stream Dispatch path.
	if cfg.FallbackEnabled && len(channels) > 0 {
		if fallback.MatchesKeyword(bodyText, cfg.FallbackKeywords) {
			if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, "keyword", cfg); committed {
				return out
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"fallback channel at capacity"}`))
			s.logErr(ctx, ownerID, model, 503, 0, "keyword_no_channel")
			return Outcome{Status: 503, Target: "none"}
		}
		if fallback.MatchesModel(model, cfg.FallbackModels) {
			if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, "model", cfg); committed {
				return out
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"fallback channel at capacity"}`))
			s.logErr(ctx, ownerID, model, 503, 0, "model_no_channel")
			return Outcome{Status: 503, Target: "none"}
		}
	}

	// TIER 2: affinity pinning.
	// Resolve affinity pin against allKeysS (full candidate set) NOT just the elastic
	// baseline — so a reserve account that was pinned in a prior turn is reachable
	// even when elastic has it off the active working set (affinity > elastic, Fix 2).
	_, pinActiveS := s.sess.Affinity(conv, nowMs)
	pinnedS := false
	affinityWaitKeyS := ""
	if cfg.AffinityTTLSec > 0 && pinActiveS {
		pinnedOrderS := s.pinToAffinity(conv, allKeysS, nowMs)
		if len(pinnedOrderS) == 1 {
			// Found the pinned account in the full set → force it as the sole candidate.
			order = pinnedOrderS
			affinityWaitKeyS = pinnedOrderS[0]
		} else {
			// Pinned account absent anywhere → empty list → falls through to exhausted fallback.
			order = nil
		}
		pinnedS = true
	}

	// TIER 3: soft rules (probe, price) + session-exile — only when NOT pinned.
	if !pinnedS {
		est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
		probeText := lastUserText(body)
		var chPriceThresholdS float64
		if len(channels) > 0 {
			chPriceThresholdS = channels[0].PriceThreshold
		}
		softTrigS := fallback.DecideSoft(fallback.DecideInput{
			Model: model, BodyText: bodyText, ProbeText: probeText, EstCostUsd: est, PoolEmpty: len(order) == 0,
			Keywords: cfg.FallbackKeywords, FallbackModels: cfg.FallbackModels,
			PriceThresholdUsd: fallback.EffectivePriceThreshold(chPriceThresholdS, cfg.FallbackPriceThresholdUsd),
			ProbeEnabled: cfg.FallbackProbeEnabled,
		})
		if cfg.FallbackEnabled && softTrigS != fallback.None && softTrigS != fallback.Exhausted && len(channels) > 0 {
			if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, string(softTrigS), cfg); committed {
				return out
			}
		}
		if (cfg.SessionErrorThreshold > 0 || cfg.ResponseExileEnabled) && s.sess.Exiled(conv, nowMs) && cfg.FallbackEnabled && len(channels) > 0 {
			if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, "session", cfg); committed {
				return out
			}
		}
	}

	maxFailover := cfg.MaxFailover
	if maxFailover <= 0 {
		maxFailover = 50
	}
	// Build per-key serial-wait maps for the stream path (same logic as Dispatch).
	serialWaitKeysS := map[string]bool{}
	serialWaitMsS := map[string]int64{}
	for _, k := range order {
		if ac, ok := keyCfgS[k]; ok && ac.SerialQueueEnabled && ac.SerialQueueWaitMs > 0 {
			serialWaitKeysS[k] = true
			serialWaitMsS[k] = int64(ac.SerialQueueWaitMs)
		}
	}
	// Affinity wait: bounded slot-wait for the pinned account before failover.
	if affinityWaitKeyS != "" && cfg.AffinityWaitMs > 0 {
		serialWaitKeysS[affinityWaitKeyS] = true
		if int64(cfg.AffinityWaitMs) > serialWaitMsS[affinityWaitKeyS] {
			serialWaitMsS[affinityWaitKeyS] = int64(cfg.AffinityWaitMs)
		}
	}
	attempts := 0
	for _, key := range order {
		if attempts >= maxFailover {
			break
		}
		// Serial-wait: bounded slot-wait before attempting (same semantics as Dispatch path).
		if serialWaitKeysS[key] {
			if waitMs := serialWaitMsS[key]; waitMs > 0 {
				if !s.Store.WaitForSlot(ctx, key, s.Now()+waitMs, s.Now) {
					continue
				}
			}
		}
		ok, trial := s.Store.TryDispatchTrial(key, model, breaker)
		if !ok {
			continue
		}
		attempts++
		out, committed, sseBody := s.streamOneWithBody(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model, keyOwner, pinnedS)
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
				if cfg.ModelPinEnabled && cfg.ModelPinMode == "sticky" {
					if wasNew := s.Store.RecordModel(key, model, int64(cfg.AffinityTTLSec)*1000); wasNew && cfg.ModelElasticEnabled {
						s.recordModelPin(ctx, acctOwnerOf(keyOwner, key, ownerID), key, model)
					}
				}
			}
			// Expose the captured SSE stream as the outcome body so the log-detail
			// modal can show a streaming request's response, not just an empty 200
			// (logs-detail-2). The handler caps it; nothing writes out.Body to the
			// client (the stream already went to w).
			if cfg.RateGovEnabled {
				s.Store.RecordReq(key)
			}
			sscfg := keyCfgS[key]
			if sscfg.SessionSimEnabled && out.Status < 300 {
				target := int(sscfg.SessionBurstCount.Resolve(key, "burst"))
				s.Store.BurstTick(key)
				if s.Store.BurstShouldPause(key, target) {
					pause := sscfg.SessionPauseMs.Resolve(key, "pause")
					s.Store.SetLimited(key, sscfg.MaxConcurrent, map[string]int64{"all": nowMs + pause})
					s.Store.BurstReset(key)
				}
			}
			out.Body = sseBody
			return out
		}
		// not committed → failed before first byte → log per-attempt error + failover
		s.logAttempt(ctx, ownerID, model, key, out.Status, true, out.Body) // own row + own detail (retry-log-clarity)
		s.recordRetry(ctx, acctOwnerOf(keyOwner, key, ownerID), model, key, out.Status, ClassifyBanned(out.Status, "", cfg.BanSignals, nil))
		s.maybeCooldown(ctx, acctOwnerOf(keyOwner, key, ownerID), key, out.Status, cfg)
		// Reactive quota rotation on the stream path too (account-limit-reactive).
		if limited, resetMs := parseLimitReset(out.Status, out.Body, nowMs, cfg.QuotaLimitKeywords, cfg.QuotaLimitStatusCodes, cfg.QuotaLimitDefaultResetMs); limited {
			s.applyReactiveLimit(ctx, acctOwnerOf(keyOwner, key, ownerID), key, resetMs)
		}
		// Terminal 400: deterministic invalid_request_error — return immediately to
		// client, no further accounts tried, no fallback channel.
		if terminalError(out.Status, out.Body, cfg) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(out.Status)
			_, _ = w.Write([]byte(out.Body))
			s.logErr(ctx, ownerID, model, out.Status, 0, "terminal_error")
			return Outcome{Status: out.Status, Body: out.Body, Target: key, Reason: "terminal_error"}
		}
		// DirectFallback: if the response matches the configured status+keyword pattern,
		// stop trying further accounts and fall through to exhausted fallback immediately.
		if directFallbackMatch(out.Status, out.Body, cfg) {
			break // → exhausted fallback (streamChannels) below
		}
		if justExiled := s.sess.RecordError(conv, int64(cfg.SessionErrorThreshold), int64(cfg.SessionCooldownSec)*1000, nowMs); justExiled {
			_ = events.Record(ctx, s.Q, nowMs, events.Event{Type: "session_exile", Target: "session", OwnerID: ownerID})
		}
		// RetryDelayMs between failover attempts.
		if cfg.RetryDelayMs > 0 {
			if ctx.Err() == nil {
				time.Sleep(time.Duration(cfg.RetryDelayMs) * time.Millisecond)
			}
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
func (s *Service) streamOneWithBody(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string, keyOwner map[string]string, affinityHit bool) (Outcome, bool, string) {
	out, committed, sseBody := s.streamOneInternal(ctx, w, key, trial, resolver, breaker, body, cfg, ownerID, model, keyOwner, affinityHit)
	return out, committed, sseBody
}

// streamOneInternal is the actual implementation; streamOneWithBody is a thin wrapper.
func (s *Service) streamOneInternal(ctx context.Context, w http.ResponseWriter, key string, trial bool, resolver Resolver, breaker state.BreakerCfg, body []byte, cfg policy.Config, ownerID, model string, keyOwner map[string]string, affinityHit bool) (Outcome, bool, string) {
	settled := false
	sendReturned := false
	lastStatus := 0
	lastBanned := false
	settle := func(success bool) {
		if settled {
			return
		}
		settled = true
		humanDist := "uniform"
		if cfg.HumanDelayEnabled {
			humanDist = cfg.HumanDelayDist
		}
		s.Store.CompleteDelay(key, humanDist,
				cfg.HumanDelayP50Ms.Resolve(key, "p50"), cfg.HumanDelayP95Ms.Resolve(key, "p95"),
				cfg.SlotCooldownMinMs, cfg.SlotCooldownMaxMs)
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
		// Read the error body (capped) before closing. Previously it was discarded,
		// which left the log detail empty for stream failures (e.g. a 429) AND blinded
		// reactive-limit keyword detection to streamed usage-limit messages
		// (logs-detail-3). st.Body is already gzip-decoded by OpenStream.
		errBody, _ := io.ReadAll(io.LimitReader(st.Body, maxDetailBodyBytes))
		_ = st.Body.Close()
		return Outcome{Status: httpStatus, Target: key, Body: string(errBody)}, false, "" // bad status before first byte → settle(false) via defer, failover
	}
	// PREFLIGHT: buffer the SSE until the first content event before committing,
	// so an upstream that returns 200 then an empty/dead stream fails over instead
	// of delivering nothing. start is set here so ttfb measures time-to-first-content.
	start := time.Now()
	prefix, hasContent := readUntilContent(st.Body, firstContentCapBytes)
	if !hasContent {
		_ = st.Body.Close()
		lastStatus = st.Status
		// Carry ban signals in the prefix (e.g. a 200 + authentication_error stream)
		// so settle(false) opens the breaker; parseLimitReset in the caller reads out.Body.
		if ClassifyBanned(st.Status, string(prefix), cfg.BanSignals, cfg.BanKeywords) {
			lastBanned = true
		}
		// 502 = bad upstream response; NOT committed → DispatchStream fails over.
		return Outcome{Status: 502, Target: key, Body: string(prefix)}, false, string(prefix)
	}
	// content arrived → commit; replay the buffered prefix then continue copying.
	CopyForwardableHeaders(w.Header(), st.Header)
	w.WriteHeader(st.Status)
	ttfb, sseBody := flushCopyCapture(w, io.MultiReader(bytes.NewReader(prefix), st.Body), start)
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
	s.logStream(ctx, ownerID, model, key, effStatus, in, out, cacheRead, cache5m, cache1h, total, ttfb, affinityHit)
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
// Additionally, for cpa nodes, it kicks a background goroutine to fetch live quota
// and upgrade the transient cooldown to a typed "5h"/"7d" limit if a window is full.
func (s *Service) maybeCooldown(ctx context.Context, ownerID, key string, status int, cfg policy.Config) {
	if !isCooldownSignal(status, cfg) {
		return
	}
	now := s.Now()
	until := now + int64(cfg.CooldownSignalSec)*1000
	s.Store.SetCooldown(key, cfg.MaxConcurrent, until)
	_ = events.Record(ctx, s.Q, now, events.Event{
		Type: "cooldown", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "status": status, "seconds": cfg.CooldownSignalSec},
	})
	// Background goroutine: fetch live quota and upgrade to typed limit if a window is full.
	// Uses detached context (request ctx dies after response); guarded by quotaInFlight so
	// only one goroutine runs per key at a time (burst-429 safety).
	if _, loaded := s.quotaInFlight.LoadOrStore(key, struct{}{}); loaded {
		return // another goroutine already classifying this key
	}
	cfgSnap := cfg // capture config value before goroutine
	go func() {
		defer s.quotaInFlight.Delete(key)
		fetchCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		reason, recoveryMs := s.classifyQuotaLimit(fetchCtx, key, cfgSnap, now)
		if reason == "" {
			return
		}
		s.Store.SetLimitedReason(key, s.Base.MaxConcurrent, recoveryMs, reason)
		s.persistLimit(context.Background(), key, recoveryMs, reason)
		_ = events.Record(context.Background(), s.Q, s.Now(), events.Event{
			Type: "quota_limited", Target: key, OwnerID: ownerID,
			Detail: map[string]any{"account": key, "source": reason, "resetsAt": recoveryMs},
		})
	}()
}

// recordRetry records a failover event: an attempt to account `key` failed
// (status / banned), so dispatch moves on to the next candidate.
func (s *Service) recordRetry(ctx context.Context, ownerID, model, key string, status int, banned bool) {
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{
		Type: "retry", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "model": model, "status": status, "banned": banned},
	})
}

// recordModelPin records that an account got pinned to a model for the FIRST time
// (model-aware-elastic visibility — "号X 首次钉定模型M").
func (s *Service) recordModelPin(ctx context.Context, ownerID, key, model string) {
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{
		Type: "model_pin", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "model": model},
	})
}

// recordModelScaleUp records that a reserve account was activated for a model because
// the elastic active set had no account able to serve it ("模型不够用,弹性扩容一个号").
func (s *Service) recordModelScaleUp(ctx context.Context, ownerID, key, model string) {
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{
		Type: "model_scale_up", Target: key, OwnerID: ownerID,
		Detail: map[string]any{"account": key, "model": model},
	})
}

// recordModelExhausted records that NO account anywhere could serve a model (all are
// pinned to other models) — the operator needs more accounts for this model.
func (s *Service) recordModelExhausted(ctx context.Context, ownerID, model string) {
	_ = events.Record(ctx, s.Q, s.Now(), events.Event{
		Type: "model_exhausted", Target: "model:" + model, OwnerID: ownerID,
		Detail: map[string]any{"model": model},
	})
}

func splitKey(key string) (node, profile string, ok bool) {
	i := strings.LastIndex(key, ":")
	if i < 0 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

// parseResetsAt parses an RFC3339 timestamp string into unix milliseconds.
// Returns 0 when s is empty or unparseable.
func parseResetsAt(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// pickQuotaLimit is a pure helper (testable without network) that classifies which
// quota window is exhausted. Checks 7d first (harder cap), then 5h.
// Returns (reason, recoveryMs) or ("", 0) when no window is full.
func pickQuotaLimit(u *cpaclient.Usage, threshold float64, now int64) (string, int64) {
	if u == nil {
		return "", 0
	}
	if u.SevenDay != nil && u.SevenDay.Utilization >= threshold {
		recov := parseResetsAt(u.SevenDay.ResetsAt)
		if recov <= 0 {
			recov = now + 7*24*3600*1000
		}
		return "7d", recov
	}
	if u.FiveHour != nil && u.FiveHour.Utilization >= threshold {
		recov := parseResetsAt(u.FiveHour.ResetsAt)
		if recov <= 0 {
			recov = now + 5*3600*1000
		}
		return "5h", recov
	}
	return "", 0
}

// fetchUsage fetches a cpa account's live subscription quota (5h/7d windows) from the
// CPA usage API. Returns nil for non-cpa nodes, missing mgmt_key, or fetch errors.
// Never panics — the dispatch hot path is safe.
func (s *Service) fetchUsage(ctx context.Context, key string) *cpaclient.Usage {
	if s.Q == nil {
		return nil
	}
	nodeID, profileID, ok := splitKey(key)
	if !ok {
		return nil
	}
	node, err := s.Q.GetNode(ctx, nodeID)
	if err != nil || node.Kind != "cpa" || node.MgmtKey == "" {
		return nil
	}
	mgmtKey := node.MgmtKey
	if s.Cipher != nil {
		mgmtKey = s.Cipher.DecryptOrPlaintext(node.MgmtKey)
	}
	c := cpaclient.New(node.BaseUrl, mgmtKey)
	// Resolve the account's auth_index (the CPA v7.x api-call selector). The dispatch
	// context only carries profileID (the .json filename), so list the node's accounts
	// and match. Falls back to "" (legacy account-usage endpoint) if listing fails.
	authIndex := ""
	if accts, lerr := c.ListAccounts(ctx); lerr == nil {
		for _, a := range accts {
			if a.AuthIndex != "" && (a.ID == profileID || a.DispatchSelector() == profileID) {
				authIndex = a.AuthIndex
				break
			}
		}
	}
	u, err := c.Usage(ctx, authIndex, profileID)
	if err != nil {
		return nil
	}
	return u
}

// classifyQuotaLimit fetches the cpa account's live quota and returns a typed limit if
// a window is exhausted. Returns ("",0) for non-cpa nodes, missing mgmt_key, fetch
// errors, or when no window is full. Never panics — the dispatch hot path is safe.
func (s *Service) classifyQuotaLimit(ctx context.Context, key string, cfg policy.Config, now int64) (string, int64) {
	u := s.fetchUsage(ctx, key)
	if u == nil {
		return "", 0
	}
	threshold := cfg.QuotaFullThreshold
	if threshold <= 0 {
		threshold = 99.0
	}
	return pickQuotaLimit(u, threshold, now)
}

// RealignSpendLimits re-evaluates every CURRENTLY-limited account against its LIVE
// quota and fixes its recovery time (a one-shot maintenance op for limits set before
// the spend-cap-reset fix). For each limited account: if a quota window is genuinely
// full → set recovery to that window's real reset; else if the 5h window has a real
// resets_at → align recovery to it; else (5h empty/null → real capacity available) →
// CLEAR the limit so the account recovers now. Accounts whose quota can't be fetched
// are left untouched. Returns counts (realigned, recovered, skipped).
func (s *Service) RealignSpendLimits(ctx context.Context) (realigned, recovered, skipped int) {
	if s.Q == nil || s.Store == nil {
		return
	}
	now := s.Now()
	rows, err := s.Q.ListActiveAccountLimitState(ctx, now)
	if err != nil {
		return
	}
	cfg := s.ResolveConfigForOwner(ctx, "")
	threshold := cfg.QuotaFullThreshold
	if threshold <= 0 {
		threshold = 99.0
	}
	for _, r := range rows {
		u := s.fetchUsage(ctx, r.Key)
		if u == nil {
			skipped++ // can't fetch live quota → leave the limit as-is
			continue
		}
		if reason, recov := pickQuotaLimit(u, threshold, now); reason != "" {
			s.Store.SetLimitedReason(r.Key, s.Base.MaxConcurrent, recov, reason)
			s.persistLimit(ctx, r.Key, recov, reason)
			realigned++
			continue
		}
		if u.FiveHour != nil {
			if recov := parseResetsAt(u.FiveHour.ResetsAt); recov > now {
				s.Store.SetLimitedReason(r.Key, s.Base.MaxConcurrent, recov, "5h")
				s.persistLimit(ctx, r.Key, recov, "5h")
				realigned++
				continue
			}
		}
		// 5h empty/null → the account has real 5h capacity → recover it now.
		s.Store.SetLimited(r.Key, s.Base.MaxConcurrent, map[string]int64{})
		s.clearPersistedLimit(ctx, r.Key)
		recovered++
	}
	return
}

// spendLimitRecovery resolves the recovery time when the SPEND CAP trips. Per design:
// fetch the account's LIVE quota and set recovery to the real 5h-window reset time —
// the spend budget is a per-5h-window throttle, so the account should release when its
// real 5h window rolls over, NOT a fixed +5h from trip time (which drifts and holds a
// quota-fine account needlessly). If a 7d/5h window is genuinely FULL, that real quota
// limit takes precedence (its own reset). Returns (reason, recovMs, source); ("",0,"")
// when the quota fetch fails so the caller falls back to a fixed +5h.
func (s *Service) spendLimitRecovery(ctx context.Context, key string, cfg policy.Config, now int64) (string, int64, string) {
	u := s.fetchUsage(ctx, key)
	if u == nil {
		return "", 0, ""
	}
	threshold := cfg.QuotaFullThreshold
	if threshold <= 0 {
		threshold = 99.0
	}
	if reason, recov := pickQuotaLimit(u, threshold, now); reason != "" {
		return reason, recov, reason // a window is genuinely full → real quota limit
	}
	// No window full — the spend cap is the trigger. Align recovery to the real 5h reset.
	if u.FiveHour != nil {
		if recov := parseResetsAt(u.FiveHour.ResetsAt); recov > now {
			return "5h", recov, "spend5h"
		}
	}
	return "", 0, ""
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
	s.Store.SetCapacity(key, cap) // apply live MaxConcurrent changes (Ensure is create-only) (fallback-cap-1)
	bk := state.BreakerCfg{PersistStreak: 1 << 30, BaseMs: 0, MaxMs: 0, Mult: 1}
	// Slot set full (MaxConcurrent reached): do not forward. Return committed=false
	// so the caller falls through to the next attempt / 503 (fallback-2).
	if !s.Store.TryDispatch(key, model, bk) {
		return Outcome{}, false
	}
	defer s.Store.Complete(key, int64(ch.CooldownMs), int64(ch.CooldownMs))

	_ = events.Record(ctx, s.Q, s.Now(), events.Event{Type: "fallback", Target: ch.ID, OwnerID: ownerID, Detail: map[string]any{"reason": reason, "channelId": ch.ID, "channelName": ch.Name}})

	// Decrypt the channel api_key transparently before forwarding (vault-crypto-3).
	cp := &ChannelProxy{Body: body, Ch: ChannelRef{BaseURL: ch.BaseUrl, APIKey: s.Cipher.DecryptOrPlaintext(ch.ApiKey)}, IdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second, UpstreamTimeoutSec: cfg.UpstreamTimeoutSec}
	st, err := cp.OpenStream(ctx, ch.ID)
	if err != nil {
		return Outcome{}, false
	}
	if st.Status >= 400 {
		_ = st.Body.Close()
		return Outcome{}, false
	}
	// PREFLIGHT: buffer until first content event before committing, so an upstream
	// that returns 200 then an empty/dead stream fails over to the next channel
	// instead of delivering nothing to the client.
	start := time.Now()
	prefix, hasContent := readUntilContent(st.Body, firstContentCapBytes)
	if !hasContent {
		_ = st.Body.Close()
		return Outcome{}, false // not committed → streamChannels tries next channel
	}
	// content arrived → commit; replay the buffered prefix then continue copying.
	CopyForwardableHeaders(w.Header(), st.Header)
	w.WriteHeader(st.Status)
	ttfb, sseBody := flushCopyCapture(w, io.MultiReader(bytes.NewReader(prefix), st.Body), start)
	_ = st.Body.Close()
	in, out, cacheRead, cache5m, cache1h := parseUsageSSE(sseBody)
	cost := billing.CostUsdFull(model, in, out, cacheRead, cache5m, cache1h)
	// Record the channel's last-observed balance so the spend row reflects
	// the balance at dispatch time (fallback-5: write observed balance).
	_ = s.Q.UpsertFallbackSpend(ctx, sqlc.UpsertFallbackSpendParams{ChannelID: ch.ID, Day: todayDayStr(), Requests: 1, EstCostUsd: cost, BalanceObserved: ch.BalanceUsd})
	s.insertLog(ctx, sqlc.InsertDispatchLogParams{
		Ts: s.Now(), OwnerID: ownerID, Model: model, Target: "fallback:" + ch.ID,
		Status: "ok", HttpStatus: int32(st.Status), FallbackReason: reason,
		LatencyMs: time.Since(start).Milliseconds(), TtfbMs: ttfb,
		TokensIn: in, TokensOut: out, Stream: true, CostUsd: cost,
		CacheRead: cacheRead, CacheCreation: cache5m + cache1h,
	})
	return Outcome{Status: st.Status, Target: "fallback:" + ch.ID, Reason: reason}, true
}
