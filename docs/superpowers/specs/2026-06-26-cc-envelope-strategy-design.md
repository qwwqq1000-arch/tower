# Claude Code 三件套 Envelope 策略 (#2) — Design

**Goal:** At the dispatch layer, optionally enforce the Claude Code "three-piece set" on outbound requests so OAuth accounts route to the Claude Code rate pool. Each of the three pieces toggles independently; only enabled pieces are checked. When an enabled piece is missing, take one global action: route the request to 保底 (fallback) **or** 补全 (inject the missing pieces) and dispatch locally.

**Architecture:** Detection runs once per request in the dispatch entry path (it has the URL, headers, and body). It returns the set of *missing enabled* pieces. The Dispatch/DispatchStream flow then either (fallback) short-circuits to the 保底 channel, or (complete) threads the missing-piece set into the proxy `Send` so injection happens where the upstream request is built (`proxy.go`). All values are configurable (no hardcoded prompt/headers — honors "不写死"). Everything is switch-gated and **defaults off → behavior-neutral**.

**Tech stack:** Go (policy config, dispatch, proxy) + React/Vite SPA (Policies page).

Reference: memory `tower-cc-envelope-dispatch-injection` (Tower passes through verbatim; `msgURL` drops `?beta=true`; `CopyForwardableHeaders` forwards client headers).

## Global Constraints
- **Default off**: `CCEnvelopeEnabled=false` and all piece toggles false → zero behavior change.
- **Account-scope aware**: resolved via the existing per-account policy override (`resolveConfig`), same as the spend-cap/rate-gov knobs.
- **Granular**: a piece is checked **only if its toggle is on**. One toggle on ⇒ only that piece is checked.
- **No hardcoding**: the CC system prompt text and the cli header values live in config with sensible defaults; operators may override.
- The 保底 path reuses the existing fallback routing (the same channel a "号池耗尽" request takes); no new fallback mechanism.

---

## Component 1: policy.Config fields

**File:** `internal/policy/policy.go` (Config + Defaults + Patch + Apply + diff)

```
CCEnvelopeEnabled      bool    // master switch (default false)
CCEnforceSystemPrompt  bool    // check/inject the CC system prompt
CCEnforceBetaParam     bool    // check/inject ?beta=true on the upstream URL
CCEnforceCliHeaders    bool    // check/inject claude-cli UA + anthropic-beta + x-app
CCEnvelopeAction       string  // "fallback" | "complete" (default "fallback")
// Configurable values (no hardcoding — operators tune to the exact strings they verified):
CCSystemPromptText     string  // default "You are Claude Code, Anthropic's official CLI for Claude."
CCCliUserAgent         string  // default "claude-cli/1.0.119 (external, cli)"
CCCliAnthropicBeta     string  // default "oauth-2025-04-20"
CCCliXApp              string  // default "cli"
```

The UA / anthropic-beta defaults are starting points; the operator who captured a real Claude Code request sets the exact strings via the Policies page. An empty value for any field means "do not set that header" (skip just that one).

Defaults set in `Defaults()`; all added to the `Patch` struct, `Apply`, and `diff()` (mirrors existing fields like `SpendCap5hEnabled`). Empty `CCEnvelopeAction` is treated as `"fallback"`.

## Component 2: detection

**File:** `internal/dispatch/envelope.go` (new)

```go
type EnvelopePiece int
const (PieceSystemPrompt EnvelopePiece = iota; PieceBetaParam; PieceCliHeaders)

// missingEnvelopePieces returns the enabled pieces ABSENT from the request. body is the
// raw request JSON; q is the request URL query; h is the request headers.
func missingEnvelopePieces(cfg policy.Config, body []byte, q url.Values, h http.Header) []EnvelopePiece
```

Per piece (only when its toggle is on):
- **SystemPrompt**: parse `body.system` (string or `[{type:"text",text}]` array); missing if it does not contain `cfg.CCSystemPromptText` (case-sensitive substring). Malformed/absent system ⇒ missing.
- **BetaParam**: missing if `q.Get("beta") != "true"`.
- **CliHeaders**: missing if the request UA does not start with `claude-cli` OR `anthropic-beta` header absent OR `x-app` header absent.

Returns `nil` when nothing enabled is missing (the common/disabled case → no work).

## Component 3: dispatch integration

**File:** `internal/dispatch/service.go` (Dispatch + DispatchStream — mirror both, per the tier-mirroring rule)

Early in dispatch, when `cfg.CCEnvelopeEnabled`:
```go
miss := missingEnvelopePieces(cfg, body, reqQuery, clientHeadersFrom(ctx))
if len(miss) > 0 {
    if action == "fallback" {
        // skip local accounts → go straight to the 保底 channel (reuse existing fallback path),
        // recording a dispatch event (type "envelope_fallback").
    } else { // "complete"
        // stash miss-set on ctx so proxy.Send injects it; continue normal local dispatch.
    }
}
```
`fallback` returns through the same code path as an exhausted local pool. `complete` attaches the miss-set via a ctx value (like `WithClientHeaders`).

## Component 4: injection (complete)

**File:** `internal/dispatch/proxy.go` (`Send` for node-account path)

Given the ctx miss-set:
- **BetaParam**: `msgURL` appends `?beta=true` (the only query Tower adds).
- **CliHeaders**: `req.Header.Set` UA = `CCCliUserAgent`, `anthropic-beta` = `CCCliAnthropicBeta`, `x-app` = `CCCliXApp`. A field left empty skips just that header.
- **SystemPrompt**: rewrite `p.Body` — parse JSON, prepend `CCSystemPromptText` to `system` (string → `prompt+"\n\n"+existing`; array → unshift a text block; absent → set string). Re-marshal once; on parse failure, leave the body unchanged (best-effort, never fail the request).

Injection touches only the local node-account `Send` (CPA/meridian), not the 保底 channel `Send`.

## Component 5: frontend

**File:** `web/spa/src/pages/Policies.tsx` (new group "Claude Code 三件套")

- Master toggle `CCEnvelopeEnabled`.
- Three piece toggles: `CCEnforceSystemPrompt` / `CCEnforceBetaParam` / `CCEnforceCliHeaders`.
- Action `<select>` `CCEnvelopeAction`: 走保底 / 补全.
- Value fields (collapsed/advanced): `CCSystemPromptText`, `CCCliUserAgent`, `CCCliAnthropicBeta`, `CCCliXApp`.
- Wire into the existing Policies patch/save flow + the FieldRow component, mirroring the spend-cap group.

## Error Handling
- Body not JSON / no system field → SystemPrompt counted missing (detect) / left unchanged (inject). Never fail the request on parse error.
- `complete` injection is best-effort: any single-piece failure logs and proceeds; the request still dispatches.
- `fallback` with no available 保底 channel → same outcome as today's exhausted-pool fallback.

## Testing
- `envelope_test`: table-driven `missingEnvelopePieces` — each toggle independently, string vs array system, beta present/absent, headers present/absent; all-off ⇒ nil.
- injection unit test: given a miss-set, `Send`'s built request has `?beta=true`, the headers, and the prepended system prompt (string + array cases).
- dispatch mirror check: a test asserting Dispatch and DispatchStream both consult the detection (guard against tier drift).
