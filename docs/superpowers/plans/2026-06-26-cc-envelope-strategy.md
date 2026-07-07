# Claude Code 三件套 Envelope 策略 (#2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Optionally enforce the Claude Code three-piece envelope (CC system prompt / `?beta=true` / claude-cli headers) on outbound requests at the dispatch layer; each piece toggles independently; when an enabled piece is missing take one global action — route to 保底 (fallback) or 补全 (inject) and dispatch locally.

**Architecture:** Detection runs once per request from the request body + URL query + client headers, returning the *missing enabled* pieces. Dispatch then either short-circuits to the 保底 channel, or stashes the miss-set on ctx so `proxy.Send` injects it where the upstream request is built. All values configurable; default off = behavior-neutral.

**Tech Stack:** Go (policy, dispatch, proxy), React/Vite SPA, Postgres-backed policy.

## Global Constraints
- Default off: `CCEnvelopeEnabled=false` + all piece toggles false ⇒ zero behavior change. New fields default to neutral in `policy.Defaults()`.
- Account-scope aware: read via the per-request `cfg` already resolved by `resolveConfig` (same as spend-cap/rate-gov).
- Granular: a piece is checked only if its toggle is on. One toggle on ⇒ only that piece checked.
- No hardcoding: CC system-prompt text + cli header values live in config (defaults overridable on the Policies page). An empty header value ⇒ skip just that header.
- `CCEnvelopeAction` is a single global string `"fallback" | "complete"`; empty ⇒ `"fallback"`.
- Mirror Dispatch AND DispatchStream (tier-mirroring rule): both consult detection identically.
- 保底 reuses the existing exhausted-pool fallback path; no new fallback mechanism.
- Exact CC system prompt string: `You are Claude Code, Anthropic's official CLI for Claude.`

---

### Task 1: policy config fields

**Files:**
- Modify: `internal/policy/policy.go` (Config struct, `Defaults()`, `Patch` struct, `Apply`, `diff()`)
- Test: `internal/policy/policy_test.go` (or the existing patch/diff test file)

**Interfaces:**
- Produces on `policy.Config`: `CCEnvelopeEnabled bool`, `CCEnforceSystemPrompt bool`, `CCEnforceBetaParam bool`, `CCEnforceCliHeaders bool`, `CCEnvelopeAction string`, `CCSystemPromptText string`, `CCCliUserAgent string`, `CCCliAnthropicBeta string`, `CCCliXApp string`.

- [ ] **Step 1: Write a failing test** that `Defaults()` is neutral and `Apply` patches the new fields. Append to the policy test file (mirror an existing `TestApply`/`TestDefaults` test):

```go
func TestCCEnvelopeDefaultsAndApply(t *testing.T) {
	d := Defaults()
	if d.CCEnvelopeEnabled || d.CCEnforceSystemPrompt || d.CCEnforceBetaParam || d.CCEnforceCliHeaders {
		t.Fatal("CC envelope toggles must default false")
	}
	if d.CCSystemPromptText == "" || d.CCCliXApp == "" {
		t.Fatal("CC value defaults must be set")
	}
	on := true
	act := "complete"
	c := Defaults()
	c.Apply(Patch{CCEnvelopeEnabled: &on, CCEnforceBetaParam: &on, CCEnvelopeAction: &act})
	if !c.CCEnvelopeEnabled || !c.CCEnforceBetaParam || c.CCEnvelopeAction != "complete" {
		t.Fatalf("Apply did not patch CC fields: %+v", c)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/policy/ -run TestCCEnvelopeDefaultsAndApply -v`
Expected: FAIL (fields undefined).

- [ ] **Step 3: Add the Config fields** (after the SpendCap block, ~line 152):

```go
// Claude Code envelope enforcement. Default off (behavior-neutral). See #2 spec.
CCEnvelopeEnabled     bool
CCEnforceSystemPrompt bool
CCEnforceBetaParam    bool
CCEnforceCliHeaders   bool
CCEnvelopeAction      string // "fallback" | "complete"
CCSystemPromptText    string
CCCliUserAgent        string
CCCliAnthropicBeta    string
CCCliXApp             string
```

- [ ] **Step 4: Add defaults** in `Defaults()` (mirror `SpendCap5hEnabled: false,`):

```go
CCEnvelopeEnabled:     false,
CCEnforceSystemPrompt: false,
CCEnforceBetaParam:    false,
CCEnforceCliHeaders:   false,
CCEnvelopeAction:      "fallback",
CCSystemPromptText:    "You are Claude Code, Anthropic's official CLI for Claude.",
CCCliUserAgent:        "claude-cli/1.0.119 (external, cli)",
CCCliAnthropicBeta:    "oauth-2025-04-20",
CCCliXApp:             "cli",
```

- [ ] **Step 5: Add Patch fields** (mirror `SpendCap5hEnabled *bool` for bools, `QuotaLimitKeywords *string` for strings):

```go
CCEnvelopeEnabled     *bool
CCEnforceSystemPrompt *bool
CCEnforceBetaParam    *bool
CCEnforceCliHeaders   *bool
CCEnvelopeAction      *string
CCSystemPromptText    *string
CCCliUserAgent        *string
CCCliAnthropicBeta    *string
CCCliXApp             *string
```

- [ ] **Step 6: Add Apply blocks** (one per field, mirror `if p.SpendCap5hEnabled != nil { c.SpendCap5hEnabled = *p.SpendCap5hEnabled }`) for all nine fields.

- [ ] **Step 7: Add diff() lines** (mirror `add("SpendCap5hEnabled", base.SpendCap5hEnabled, final.SpendCap5hEnabled)` — use the bool/string `add` overloads already used for nearby fields) for all nine fields.

- [ ] **Step 8: Run tests + build**

Run: `go test ./internal/policy/ -run TestCCEnvelope -v && go build ./...`
Expected: PASS + build clean.

- [ ] **Step 9: Commit**

```bash
git add internal/policy/policy.go internal/policy/policy_test.go
git commit -m "feat(policy): CC envelope config fields (default off)"
```

---

### Task 2: detection — missingEnvelopePieces

**Files:**
- Create: `internal/dispatch/envelope.go`
- Test: `internal/dispatch/envelope_test.go`

**Interfaces:**
- Produces: `type EnvelopePiece int` with `PieceSystemPrompt, PieceBetaParam, PieceCliHeaders`; `func missingEnvelopePieces(cfg policy.Config, body []byte, q url.Values, h http.Header) []EnvelopePiece`.

- [ ] **Step 1: Write the failing test** — `internal/dispatch/envelope_test.go`:

```go
package dispatch

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

func ccCfg() policy.Config {
	c := policy.Defaults()
	c.CCEnvelopeEnabled = true
	c.CCSystemPromptText = "You are Claude Code, Anthropic's official CLI for Claude."
	return c
}

func TestMissingEnvelopePieces(t *testing.T) {
	withSys := []byte(`{"system":"You are Claude Code, Anthropic's official CLI for Claude.\n\nrest"}`)
	noSys := []byte(`{"messages":[]}`)
	q := func(s string) url.Values { v, _ := url.ParseQuery(s); return v }
	hdr := func(ua, beta, xapp string) http.Header {
		h := http.Header{}
		if ua != "" { h.Set("User-Agent", ua) }
		if beta != "" { h.Set("anthropic-beta", beta) }
		if xapp != "" { h.Set("x-app", xapp) }
		return h
	}

	// All pieces off → nothing missing even when absent.
	c := ccCfg()
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("", "", "")); got != nil {
		t.Fatalf("all-off should be nil, got %v", got)
	}

	// Only system-prompt piece on, prompt absent → just PieceSystemPrompt.
	c = ccCfg(); c.CCEnforceSystemPrompt = true
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("", "", "")); len(got) != 1 || got[0] != PieceSystemPrompt {
		t.Fatalf("want [SystemPrompt], got %v", got)
	}
	// system present → none.
	if got := missingEnvelopePieces(c, withSys, q(""), hdr("", "", "")); got != nil {
		t.Fatalf("system present should be nil, got %v", got)
	}

	// Only beta piece on, beta absent → PieceBetaParam; present → none.
	c = ccCfg(); c.CCEnforceBetaParam = true
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("", "", "")); len(got) != 1 || got[0] != PieceBetaParam {
		t.Fatalf("want [BetaParam], got %v", got)
	}
	if got := missingEnvelopePieces(c, noSys, q("beta=true"), hdr("", "", "")); got != nil {
		t.Fatalf("beta present should be nil, got %v", got)
	}

	// Only cli-headers on, headers absent → PieceCliHeaders; full set → none.
	c = ccCfg(); c.CCEnforceCliHeaders = true
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("Go-http-client/1.1", "", "")); len(got) != 1 || got[0] != PieceCliHeaders {
		t.Fatalf("want [CliHeaders], got %v", got)
	}
	if got := missingEnvelopePieces(c, noSys, q(""), hdr("claude-cli/1.0", "oauth-2025-04-20", "cli")); got != nil {
		t.Fatalf("full cli headers should be nil, got %v", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/dispatch/ -run TestMissingEnvelopePieces -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement** `internal/dispatch/envelope.go`:

```go
package dispatch

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

type EnvelopePiece int

const (
	PieceSystemPrompt EnvelopePiece = iota
	PieceBetaParam
	PieceCliHeaders
)

// missingEnvelopePieces returns the ENABLED three-piece-set pieces absent from the
// request. Only pieces whose toggle is on are checked; returns nil when nothing
// enabled is missing (the common path).
func missingEnvelopePieces(cfg policy.Config, body []byte, q url.Values, h http.Header) []EnvelopePiece {
	if !cfg.CCEnvelopeEnabled {
		return nil
	}
	var miss []EnvelopePiece
	if cfg.CCEnforceSystemPrompt && !bodyHasSystemPrompt(body, cfg.CCSystemPromptText) {
		miss = append(miss, PieceSystemPrompt)
	}
	if cfg.CCEnforceBetaParam && q.Get("beta") != "true" {
		miss = append(miss, PieceBetaParam)
	}
	if cfg.CCEnforceCliHeaders {
		ua := h.Get("User-Agent")
		if !strings.HasPrefix(strings.ToLower(ua), "claude-cli") || h.Get("anthropic-beta") == "" || h.Get("x-app") == "" {
			miss = append(miss, PieceCliHeaders)
		}
	}
	return miss
}

// bodyHasSystemPrompt reports whether the request body's system field contains want.
// system may be a string or an array of {type:"text",text:...} blocks.
func bodyHasSystemPrompt(body []byte, want string) bool {
	if want == "" {
		return true
	}
	var probe struct {
		System json.RawMessage `json:"system"`
	}
	if json.Unmarshal(body, &probe) != nil || len(probe.System) == 0 {
		return false
	}
	var asStr string
	if json.Unmarshal(probe.System, &asStr) == nil {
		return strings.Contains(asStr, want)
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(probe.System, &blocks) == nil {
		for _, b := range blocks {
			if strings.Contains(b.Text, want) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/dispatch/ -run TestMissingEnvelopePieces -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/envelope.go internal/dispatch/envelope_test.go
git commit -m "feat(dispatch): three-piece envelope detection (per-piece, enabled-only)"
```

---

### Task 3: ctx plumbing for request query + miss-set

**Files:**
- Modify: `internal/dispatch/headers.go` (add ctx stashers/readers)
- Modify: `internal/api/dispatch_handler.go` (stash the request query)
- Test: `internal/dispatch/headers_test.go` (new or existing)

**Interfaces:**
- Produces: `func WithRequestQuery(ctx, url.Values) context.Context`, `func requestQueryFrom(ctx) url.Values`, `func withEnvelopeInject(ctx, []EnvelopePiece) context.Context`, `func envelopeInjectFrom(ctx) []EnvelopePiece`.

- [ ] **Step 1: Write a failing round-trip test** in `internal/dispatch/headers_test.go`:

```go
func TestRequestQueryAndInjectCtx(t *testing.T) {
	ctx := WithRequestQuery(context.Background(), url.Values{"beta": {"true"}})
	if requestQueryFrom(ctx).Get("beta") != "true" {
		t.Fatal("query round-trip failed")
	}
	ctx = withEnvelopeInject(ctx, []EnvelopePiece{PieceBetaParam})
	if got := envelopeInjectFrom(ctx); len(got) != 1 || got[0] != PieceBetaParam {
		t.Fatalf("inject round-trip failed: %v", got)
	}
	if envelopeInjectFrom(context.Background()) != nil {
		t.Fatal("absent inject should be nil")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/dispatch/ -run TestRequestQueryAndInjectCtx -v`
Expected: FAIL.

- [ ] **Step 3: Implement** in `internal/dispatch/headers.go` (mirror the existing `WithClientHeaders`/`clientHeadersFrom` pattern): two context keys + the four funcs above. `requestQueryFrom`/`envelopeInjectFrom` return zero value (`nil`) when absent.

- [ ] **Step 4: Stash the query in the handler.** In `internal/api/dispatch_handler.go`, where it already calls `dispatch.WithClientHeaders(ctx, r.Header)`, also wrap: `ctx = dispatch.WithRequestQuery(ctx, r.URL.Query())`.

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/dispatch/ -run TestRequestQueryAndInjectCtx -v && go build ./...`
Expected: PASS + build clean.

- [ ] **Step 6: Commit**

```bash
git add internal/dispatch/headers.go internal/api/dispatch_handler.go internal/dispatch/headers_test.go
git commit -m "feat(dispatch): ctx plumbing for request query + envelope inject-set"
```

---

### Task 4: dispatch integration (Dispatch + DispatchStream)

**Files:**
- Modify: `internal/dispatch/service.go` (`Dispatch` and `DispatchStream`)
- Test: `internal/dispatch/envelope_dispatch_test.go` (new) — a mirror-guard test

**Interfaces:**
- Consumes: `missingEnvelopePieces` (T2), `requestQueryFrom`/`withEnvelopeInject` (T3), `clientHeadersFrom` (existing).

- [ ] **Step 1: Locate the body + start of each tier.** In `Dispatch` and `DispatchStream`, right after `cfg` is resolved and the request body (`body []byte`) is available, insert the envelope gate (identical text in both):

```go
if cfg.CCEnvelopeEnabled {
	if miss := missingEnvelopePieces(cfg, body, requestQueryFrom(ctx), clientHeadersFrom(ctx)); len(miss) > 0 {
		action := cfg.CCEnvelopeAction
		if action == "" {
			action = "fallback"
		}
		if action == "fallback" {
			// Skip local accounts → go straight to the 保底 channel, same as an exhausted pool.
			return s.dispatchFallbackOnly(ctx, ownerID, model, body, /* the existing fallback args */)
		}
		// "complete": inject the missing pieces downstream in proxy.Send.
		ctx = withEnvelopeInject(ctx, miss)
	}
}
```

Implement `dispatchFallbackOnly` (or reuse the existing function the code already calls when local candidates are exhausted — read `service.go` to find that path; it emits a `fallback` event with `reason:"exhausted"`). Use the SAME function so behavior is identical; record the event reason as `"envelope"` instead of `"exhausted"` by passing the reason through.

- [ ] **Step 2: Write the mirror-guard test** `internal/dispatch/envelope_dispatch_test.go` — assert both `Dispatch` and `DispatchStream` source contain the `missingEnvelopePieces` call, guarding against tier drift:

```go
func TestBothTiersConsultEnvelope(t *testing.T) {
	src, err := os.ReadFile("service.go")
	if err != nil { t.Fatal(err) }
	// crude but effective tier-drift guard: detection must appear at least twice
	if strings.Count(string(src), "missingEnvelopePieces(cfg") < 2 {
		t.Fatal("Dispatch and DispatchStream must both consult missingEnvelopePieces")
	}
}
```

- [ ] **Step 3: Run tests + build**

Run: `go test ./internal/dispatch/ -run TestBothTiersConsultEnvelope -v && go build ./... && go test ./internal/dispatch/`
Expected: PASS + build clean + suite green.

- [ ] **Step 4: Commit**

```bash
git add internal/dispatch/service.go internal/dispatch/envelope_dispatch_test.go
git commit -m "feat(dispatch): envelope gate — fallback or stash inject-set (both tiers)"
```

---

### Task 5: injection in proxy.Send (complete)

**Files:**
- Modify: `internal/dispatch/proxy.go` (the node-account `Send`; `msgURL`)
- Test: `internal/dispatch/proxy_envelope_test.go` (new)

**Interfaces:**
- Consumes: `envelopeInjectFrom(ctx)` (T3), `policy.Config` cli values; the per-request `cfg` available in `Send` (thread it via the existing payload/ctx — read `proxy.go` to see how `cfg` reaches `Send`; if not present, pass the three string values + miss-set on ctx).

- [ ] **Step 1: Write the failing test** — build a request body + ctx with an inject-set, call the injection helper, assert results. Extract a pure helper to keep it testable:

```go
func TestInjectEnvelope(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-8","messages":[]}`)
	vals := envelopeVals{system: "You are Claude Code, Anthropic's official CLI for Claude.", ua: "claude-cli/1.0", beta: "oauth-2025-04-20", xapp: "cli"}
	miss := []EnvelopePiece{PieceSystemPrompt, PieceCliHeaders}

	h := http.Header{}
	newBody := injectEnvelope(miss, body, h, vals)

	if h.Get("User-Agent") != "claude-cli/1.0" || h.Get("x-app") != "cli" || h.Get("anthropic-beta") != "oauth-2025-04-20" {
		t.Fatalf("headers not injected: %v", h)
	}
	if !strings.Contains(string(newBody), "You are Claude Code") {
		t.Fatalf("system prompt not injected: %s", newBody)
	}
}

func TestInjectEnvelopeBadBodyUnchanged(t *testing.T) {
	body := []byte(`not json`)
	got := injectEnvelope([]EnvelopePiece{PieceSystemPrompt}, body, http.Header{}, envelopeVals{system: "x"})
	if string(got) != "not json" {
		t.Fatalf("bad body must be returned unchanged, got %s", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/dispatch/ -run TestInjectEnvelope -v`
Expected: FAIL.

- [ ] **Step 3: Implement** the pure helper in `internal/dispatch/envelope.go`:

```go
type envelopeVals struct{ system, ua, beta, xapp string }

// injectEnvelope sets the missing cli headers on h and prepends the system prompt to
// body for the requested pieces. Best-effort: a body that is not JSON is returned
// unchanged. (BetaParam is applied at the URL, not here.)
func injectEnvelope(miss []EnvelopePiece, body []byte, h http.Header, v envelopeVals) []byte {
	want := func(p EnvelopePiece) bool {
		for _, m := range miss {
			if m == p {
				return true
			}
		}
		return false
	}
	if want(PieceCliHeaders) {
		if v.ua != "" {
			h.Set("User-Agent", v.ua)
		}
		if v.beta != "" {
			h.Set("anthropic-beta", v.beta)
		}
		if v.xapp != "" {
			h.Set("x-app", v.xapp)
		}
	}
	if want(PieceSystemPrompt) && v.system != "" {
		var m map[string]json.RawMessage
		if json.Unmarshal(body, &m) == nil {
			var existing string
			if raw, ok := m["system"]; ok {
				_ = json.Unmarshal(raw, &existing) // string form; array form falls through to replace
			}
			combined := v.system
			if existing != "" && !strings.Contains(existing, v.system) {
				combined = v.system + "\n\n" + existing
			}
			if enc, err := json.Marshal(combined); err == nil {
				m["system"] = enc
				if nb, err := json.Marshal(m); err == nil {
					return nb
				}
			}
		}
	}
	return body
}
```

- [ ] **Step 4: Wire it into `Send`.** In `proxy.go`'s node-account `Send`, after building `req` and before sending: read `miss := envelopeInjectFrom(ctx)`; if non-empty, set `p.Body = injectEnvelope(miss, p.Body, req.Header, envelopeVals{system: cfg.CCSystemPromptText, ua: cfg.CCCliUserAgent, beta: cfg.CCCliAnthropicBeta, xapp: cfg.CCCliXApp})` and rebuild the request body reader; if `PieceBetaParam` ∈ miss, build the URL with `?beta=true` (extend `msgURL` to take a `beta bool`, or append the query in `Send`). Only the node-account `Send` is touched — never the 保底 channel `Send`.

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/dispatch/ -run 'TestInjectEnvelope' -v && go build ./... && go test ./internal/dispatch/`
Expected: PASS + build clean + suite green.

- [ ] **Step 6: Commit**

```bash
git add internal/dispatch/envelope.go internal/dispatch/proxy.go internal/dispatch/proxy_envelope_test.go
git commit -m "feat(dispatch): inject ?beta=true / cli headers / CC system prompt on complete"
```

---

### Task 6: frontend Policies group

**Files:**
- Modify: `web/spa/src/pages/Policies.tsx`

**Interfaces:**
- Consumes: the existing field-state hook pattern (`field.enabled/value/toggle/set`), `setBool(...)` in the load effect, `patch.X = field.value` in save.

- [ ] **Step 1: Declare field-state hooks** for the nine fields near the other policy fields (mirror `spendCap5hEnabled` for bools and `quotaLimitKeywords` for strings): `ccEnvelopeEnabled`, `ccEnforceSystemPrompt`, `ccEnforceBetaParam`, `ccEnforceCliHeaders`, `ccEnvelopeAction`, `ccSystemPromptText`, `ccCliUserAgent`, `ccCliAnthropicBeta`, `ccCliXApp`.

- [ ] **Step 2: Load** — in the load effect add (mirror `setBool(spendCap5hEnabled, p, 'SpendCap5hEnabled')`): `setBool` for the four bools, `setStr` (or the string-load equivalent used for `quotaLimitKeywords`) for `CCEnvelopeAction`, `CCSystemPromptText`, `CCCliUserAgent`, `CCCliAnthropicBeta`, `CCCliXApp`.

- [ ] **Step 3: Save** — in the patch builder add (mirror `if spendCap5hEnabled.enabled) patch.SpendCap5hEnabled = spendCap5hEnabled.value`): one `if field.enabled` line per field assigning `patch.<FieldName>`.

- [ ] **Step 4: Render the group** — add a new `<div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">` section titled "Claude Code 三件套" with a master `FieldRow` (checkbox like SpendCap5hEnabled), three piece-toggle `FieldRow`s (checkboxes), an action `FieldRow` with a `<select>` (走保底 / 补全), and value `FieldRow`s (text inputs like `quotaLimitKeywords`) for `CCSystemPromptText`, `CCCliUserAgent`, `CCCliAnthropicBeta`, `CCCliXApp`. Include `showOnlyConfigured={so}` on each `FieldRow` (matches the page).

- [ ] **Step 5: Build the SPA**

Run: `cd web/spa && npm run build`
Expected: builds, no TS errors.

- [ ] **Step 6: Commit**

```bash
git add web/spa/src/pages/Policies.tsx
git commit -m "feat(ui): Policies Claude Code 三件套 group (toggles + action + values)"
```

---

## Self-Review notes
- Spec coverage: config (T1), detection (T2), ctx plumbing (T3), dispatch gate both tiers (T4), injection (T5), frontend (T6) — all spec components covered.
- Default-off neutrality: T1 defaults + the `if cfg.CCEnvelopeEnabled` guard in T4 ⇒ no behavior change until enabled.
- "不写死": all prompt/header values come from config (T1), surfaced on the Policies page (T6).
- Type consistency: `EnvelopePiece` constants, `missingEnvelopePieces`, `injectEnvelope`, `envelopeVals`, and the four ctx funcs are used with the same signatures across T2–T5.
- Open implementation detail flagged in T4/T5: the exact name of the existing exhausted-pool fallback function and how `cfg` reaches `Send` must be read from `service.go`/`proxy.go` by the implementer (the plan tells them to reuse it, not invent one).
