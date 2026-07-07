# Affinity Priority Routing + Queue-on-Busy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure dispatch routing priority to Keyword/Model → Affinity → Probe/Price/Session-exile, and add `AffinityWaitMs` bounded-wait so an affinity-pinned conversation queues for a busy account instead of falling to exhausted fallback.

**Architecture:** Three changes across four files. (1) `internal/fallback/decide.go` — export `MatchesKeyword`, `MatchesModel`, and add `DecideSoft` (probe+price only). (2) `internal/policy/policy.go` — add `AffinityWaitMs int` field with default 2000ms. (3) `internal/dispatch/service.go` — restructure both `Dispatch` and `DispatchStream` identically: keyword/model hard-divert first, then affinity pin, then probe/price/session-exile only when NOT pinned, then affinityWaitKey wired into existing serialWait maps for bounded wait. (4) `web/spa/src/` — add `AffinityWaitMs` field to `types.ts` and `Policies.tsx`.

**Tech Stack:** Go 1.21+, React/TypeScript (Vite), PostgreSQL. Module: `github.com/qwwqq1000-arch/tower`.

## Global Constraints

- Branch: `feat/anticontrol-phase1`
- Hot path — both `Dispatch` and `DispatchStream` must be structurally **identical** for the new tier ordering. Only stream vs non-stream proxy calls differ (`viaChannels` / `streamChannels`).
- New `AffinityWaitMs` default = 2000 (ms). `AffinityWaitMs: 0` = no wait (existing behaviour).
- Keep existing `Decide` function intact — other callers / tests depend on it.
- All new features default-off or default-safe: `AffinityWaitMs` defaults to 2000 but only activates when `AffinityTTLSec > 0` and a pin is active.
- `go build ./... && go vet ./... && go test ./internal/...` must stay green.
- `cd web/spa && npx tsc --noEmit && npm run build` must stay green.
- Commit message: `feat(dispatch): routing priority keyword>affinity>rest + bounded affinity queue-on-busy`

---

### Task 1: Add exported helpers + `DecideSoft` to fallback package

**Files:**
- Modify: `internal/fallback/decide.go`
- Modify: `internal/fallback/decide_test.go`

**Interfaces:**
- Produces:
  - `func MatchesKeyword(bodyText string, keywords []string) bool`
  - `func MatchesModel(model string, models []string) bool`
  - `func DecideSoft(in DecideInput) Trigger` — returns only `Probe`, `Price`, `Exhausted`, or `None`; NEVER `Keyword` or `Model`

- [ ] **Step 1: Write failing tests for the three new functions in `internal/fallback/decide_test.go`**

Append after the existing tests (keep all existing tests untouched):

```go
// ---- new helpers ----

func TestMatchesKeyword_True(t *testing.T) {
	if !MatchesKeyword("please refactor this code", []string{"refactor"}) {
		t.Fatal("expected true")
	}
}

func TestMatchesKeyword_False(t *testing.T) {
	if MatchesKeyword("hello world", []string{"refactor"}) {
		t.Fatal("expected false")
	}
}

func TestMatchesKeyword_Empty(t *testing.T) {
	if MatchesKeyword("anything", nil) {
		t.Fatal("empty keywords list must never match")
	}
}

func TestMatchesModel_True(t *testing.T) {
	if !MatchesModel("claude-opus-4-8", []string{"opus-4-8"}) {
		t.Fatal("expected true")
	}
}

func TestMatchesModel_False(t *testing.T) {
	if MatchesModel("claude-sonnet-4-6", []string{"opus-4-8"}) {
		t.Fatal("expected false")
	}
}

func TestDecideSoft_Probe(t *testing.T) {
	in := base()
	in.ProbeText = "hi"
	in.ProbeEnabled = true
	if g := DecideSoft(in); g != Probe {
		t.Fatalf("got %v, want Probe", g)
	}
}

func TestDecideSoft_Price(t *testing.T) {
	in := base()
	in.EstCostUsd = 0.001 // below PriceThresholdUsd 0.005
	if g := DecideSoft(in); g != Price {
		t.Fatalf("got %v, want Price", g)
	}
}

func TestDecideSoft_None(t *testing.T) {
	in := base()
	// EstCostUsd=1.0 (above threshold), no probe
	if g := DecideSoft(in); g != None {
		t.Fatalf("got %v, want None", g)
	}
}

func TestDecideSoft_Exhausted(t *testing.T) {
	in := base()
	in.PoolEmpty = true
	if g := DecideSoft(in); g != Exhausted {
		t.Fatalf("got %v, want Exhausted", g)
	}
}

func TestDecideSoft_NeverReturnsKeywordOrModel(t *testing.T) {
	// Even when keywords and models are configured, DecideSoft must not return them
	in := base()
	in.Keywords = []string{"refactor"}
	in.FallbackModels = []string{"opus-4-8"}
	in.EstCostUsd = 0.0001
	g := DecideSoft(in)
	if g == Keyword || g == Model {
		t.Fatalf("DecideSoft must never return Keyword/Model, got %v", g)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/leo/总控台/tower && go test ./internal/fallback/... -run "TestMatchesKeyword|TestMatchesModel|TestDecideSoft" -v 2>&1 | tail -20
```

Expected: FAIL with "undefined: MatchesKeyword" or similar.

- [ ] **Step 3: Add the three functions to `internal/fallback/decide.go`**

Append after the closing brace of `Decide` (after line 85), before end of file:

```go

// MatchesKeyword reports whether bodyText contains any configured fallback keyword.
func MatchesKeyword(bodyText string, keywords []string) bool {
	return containsAny(bodyText, keywords)
}

// MatchesModel reports whether the model matches any configured fallback-model rule.
func MatchesModel(model string, models []string) bool {
	return containsAny(model, models)
}

// DecideSoft returns only the soft triggers (Probe, Price, Exhausted, or None).
// Keyword and Model are intentionally excluded — they are handled as higher-priority
// hard rules in the dispatch tier before affinity is evaluated.
func DecideSoft(in DecideInput) Trigger {
	if in.ProbeEnabled && IsProbe(in.ProbeText) {
		return Probe
	}
	if in.PriceThresholdUsd > 0 && in.EstCostUsd < in.PriceThresholdUsd {
		return Price
	}
	if in.PoolEmpty {
		return Exhausted
	}
	return None
}
```

- [ ] **Step 4: Run all fallback tests (new + existing) and confirm all pass**

```bash
cd /Users/leo/总控台/tower && go test ./internal/fallback/... -v 2>&1 | tail -30
```

Expected: all tests PASS (including all pre-existing tests — Decide, IsProbe, EffectivePriceThreshold, etc.).

- [ ] **Step 5: Commit**

```bash
cd /Users/leo/总控台/tower && git add internal/fallback/decide.go internal/fallback/decide_test.go
git commit -m "feat(fallback): export MatchesKeyword/MatchesModel + add DecideSoft for soft-only triggers"
```

---

### Task 2: Add `AffinityWaitMs` to policy

**Files:**
- Modify: `internal/policy/policy.go`

**Interfaces:**
- Produces: `Config.AffinityWaitMs int`, `Patch.AffinityWaitMs *int`
- `Defaults()` sets `AffinityWaitMs: 2000`
- `DryRun` emits a diff line for `AffinityWaitMs`

- [ ] **Step 1: Write a failing test in `internal/policy/policy_test.go`**

Open `internal/policy/policy_test.go` and append:

```go
func TestDefaults_AffinityWaitMs(t *testing.T) {
	d := Defaults()
	if d.AffinityWaitMs != 2000 {
		t.Fatalf("default AffinityWaitMs should be 2000, got %d", d.AffinityWaitMs)
	}
}

func TestPatch_AffinityWaitMs(t *testing.T) {
	v := 500
	p := Patch{AffinityWaitMs: &v}
	c := Resolve(Defaults(), p)
	if c.AffinityWaitMs != 500 {
		t.Fatalf("AffinityWaitMs patch: want 500, got %d", c.AffinityWaitMs)
	}
}

func TestDryRun_AffinityWaitMs(t *testing.T) {
	v := 0
	_, diffs := DryRun(Defaults(), Patch{AffinityWaitMs: &v})
	found := false
	for _, d := range diffs {
		if d.Field == "AffinityWaitMs" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("DryRun should emit AffinityWaitMs diff when changed")
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
cd /Users/leo/总控台/tower && go test ./internal/policy/... -run "TestDefaults_AffinityWaitMs|TestPatch_AffinityWaitMs|TestDryRun_AffinityWaitMs" -v 2>&1 | tail -15
```

Expected: FAIL (field does not exist yet).

- [ ] **Step 3: Add `AffinityWaitMs` to `Config` struct**

In `internal/policy/policy.go`, find the `AffinityTTLSec int` line (~line 62) and add immediately after:

```go
	// AffinityWaitMs is the maximum time (ms) to wait for a busy but healthy
	// affinity-pinned account before failing over. 0 = no wait (falls to exhausted
	// fallback immediately). Default 2000. Reuses the existing serialWait slot-wait
	// path: the bounded wait fires only when a conversation is actively pinned.
	AffinityWaitMs int
```

- [ ] **Step 4: Add `AffinityWaitMs` to `Defaults()`**

In `Defaults()`, find `AffinityTTLSec: 300,` and add after:

```go
		AffinityWaitMs:            2000,
```

- [ ] **Step 5: Add `AffinityWaitMs` to `Patch` struct**

In the `Patch` struct, find `AffinityTTLSec *int` and add after:

```go
	AffinityWaitMs *int
```

- [ ] **Step 6: Add `apply()` branch for `AffinityWaitMs`**

In `apply()`, find the `if p.AffinityTTLSec != nil` block and add after:

```go
	if p.AffinityWaitMs != nil {
		c.AffinityWaitMs = *p.AffinityWaitMs
	}
```

- [ ] **Step 7: Add `AffinityWaitMs` to `DryRun`**

In `DryRun`, find `add("AffinityTTLSec", ...)` and add after:

```go
	add("AffinityWaitMs", base.AffinityWaitMs, final.AffinityWaitMs)
```

- [ ] **Step 8: Run policy tests (new + existing) and confirm all pass**

```bash
cd /Users/leo/总控台/tower && go test ./internal/policy/... -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 9: Commit**

```bash
cd /Users/leo/总控台/tower && git add internal/policy/policy.go internal/policy/policy_test.go
git commit -m "feat(policy): add AffinityWaitMs (default 2000ms) for bounded affinity queue-on-busy"
```

---

### Task 3: Restructure `Dispatch` and `DispatchStream` in `service.go`

This is the hot-path change. Both functions must be changed identically in structure; only the fallback proxy calls differ.

**Files:**
- Modify: `internal/dispatch/service.go`
- Modify: `internal/dispatch/affinity_test.go` (add keyword-wins-over-pinned test)

**Before (current structure in both functions):**
```
buildCandidates / enabledChannels / ConvID
if AffinityTTLSec > 0 { pinToAffinity }   ← affinity BEFORE keyword
BodyPad
est / probeText / chPriceThreshold
Decide(all: keyword+model+probe+price)     ← lumped, fires AFTER affinity
if FallbackEnabled && trig != None/Exhausted { → fallback }
if Exiled { → fallback("session") }
node loop (serialWait maps from keyCfg)
bottom: exhausted fallback
```

**After (new structure — apply to BOTH):**
```
buildCandidates / enabledChannels / ConvID
BodyPad  (unchanged, uses conv)

// TIER 1: keyword + model — hardest rules, before affinity
if FallbackEnabled && len(channels) > 0 {
    if MatchesKeyword(bodyText, cfg.FallbackKeywords)     → fallback("keyword")
    if MatchesModel(model, cfg.FallbackModels)            → fallback("model")
}

// TIER 2: affinity pin
_, pinActive := s.sess.Affinity(conv, nowMs)
pinned := false
order := order0
affinityWaitKey := ""
if cfg.AffinityTTLSec > 0 && pinActive {
    order = s.pinToAffinity(conv, order0, nowMs)
    pinned = true
    if len(order) == 1 { affinityWaitKey = order[0] }
}

// TIER 3: soft rules (probe, price) + session-exile — only when NOT pinned
if !pinned {
    softTrig := fallback.DecideSoft(...)
    if FallbackEnabled && softTrig not None/Exhausted && len(channels) > 0 { → fallback(softTrig) }
    if (SessionErrorThreshold>0||ResponseExileEnabled) && Exiled && FallbackEnabled && len(channels)>0 { → fallback("session") }
}

// node loop: add affinityWaitKey → serialWait maps
// rest of loop unchanged
// bottom exhausted fallback unchanged
```

**Note on variable naming:**
- `Dispatch` uses `order`, `keyCfg`, `serialWaitKeys`, `serialWaitMs`, `channels`, `est`, `probeText`, `chPriceThreshold`.
- `DispatchStream` uses `order`, `keyCfgS`, `serialWaitKeysS`, `serialWaitMsS`, `channels`, `est`, `probeText`, `chPriceThresholdS`.
- In both cases, `order0` is the pre-affinity list (rename the initial `order` from `buildCandidates` to `order0`, then re-declare `order` after the affinity block).

**Interfaces:**
- Consumes: `fallback.MatchesKeyword`, `fallback.MatchesModel`, `fallback.DecideSoft` (from Task 1); `policy.Config.AffinityWaitMs` (from Task 2).
- `s.sess.Affinity(conv, nowMs)` returns `(key string, ok bool)` — use `_, pinActive :=` since we only need the bool here (pinToAffinity re-reads the key internally).

- [ ] **Step 1: Write a dispatch-level test asserting keyword wins over a pinned conversation**

Append to `internal/dispatch/affinity_test.go`:

```go
// TestPinToAffinity_KeywordTierNotInvolvedHere documents that pinToAffinity itself
// has no knowledge of keywords — keyword-wins-over-affinity is enforced in service.go
// BEFORE pinToAffinity is called. This test verifies pinToAffinity still returns the
// pinned account (the service layer is responsible for the tier ordering).
func TestPinToAffinity_PinnedAccountReturnedWhenPresent(t *testing.T) {
	svc := &Service{sess: session.NewStore(), Now: func() int64 { return 1000 }}
	order := []string{"nA:default", "nB:default", "nC:default"}
	svc.sess.SetAffinity("conv2", "nA:default", 5000, 1000)
	got := svc.pinToAffinity("conv2", order, 2000)
	if len(got) != 1 || got[0] != "nA:default" {
		t.Fatalf("want [nA:default], got %v", got)
	}
}
```

- [ ] **Step 2: Run the new test to confirm it passes (it's just a pinToAffinity unit test)**

```bash
cd /Users/leo/总控台/tower && go test ./internal/dispatch/... -run "TestPinToAffinity" -v 2>&1 | tail -15
```

Expected: PASS (this test verifies existing behaviour, not new code yet).

- [ ] **Step 3: Restructure `Dispatch` (~line 390–555 of service.go)**

Replace the block from just after the `BodyPad` block through the session-exile check. The exact replacement region in `Dispatch` is:

**OLD** (lines ~427–448, between BodyPad block and "// Our nodes." comment):

```go
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
```

Also, the affinity block must move: remove the existing `if cfg.AffinityTTLSec > 0 { order = s.pinToAffinity(...) }` block and integrate it into the new structure.

Here is the full replacement for **`Dispatch`** — from `order, resolver, keyOwner, keyCfg := s.buildCandidates(...)` through the start of `// Our nodes.`:

```go
	order0, resolver, keyOwner, keyCfg := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID, model)

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

	// ---- TIER 1: hard fallback rules (keyword + model) — HIGHEST priority ----
	// Checked before affinity so a pinned conversation still routes to fallback
	// when its body contains a forbidden keyword or uses a forbidden model.
	if cfg.FallbackEnabled && len(channels) > 0 {
		if fallback.MatchesKeyword(bodyText, cfg.FallbackKeywords) {
			return s.viaChannels(ctx, ownerID, model, body, channels, "keyword", time.Since(start).Milliseconds(), cfg)
		}
		if fallback.MatchesModel(model, cfg.FallbackModels) {
			return s.viaChannels(ctx, ownerID, model, body, channels, "model", time.Since(start).Milliseconds(), cfg)
		}
	}

	// ---- TIER 2: affinity pin ----
	_, pinActive := s.sess.Affinity(conv, nowMs)
	pinned := false
	order := order0
	affinityWaitKey := ""
	if cfg.AffinityTTLSec > 0 && pinActive {
		order = s.pinToAffinity(conv, order0, nowMs)
		pinned = true
		if len(order) == 1 {
			affinityWaitKey = order[0] // healthy pinned account → enable bounded wait
		}
		// len(order)==0: pinned account gone → falls through to exhausted fallback (unavoidable cache miss)
	}

	// ---- TIER 3: soft rules (probe, price) + session-exile — only when NOT pinned ----
	// Skipped when pinned so affinity is honoured even for probe/cheap requests.
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
```

- [ ] **Step 4: Wire `affinityWaitKey` into the serialWait maps inside `Dispatch`'s node loop**

After the `for _, k := range order { if ac, ok := keyCfg[k]; ... }` serial-wait map build loop (lines ~460–465), add:

```go
		// Affinity queue-on-busy: if the conversation is pinned to a single healthy
		// account, wire it into the serialWait maps so the node loop waits up to
		// AffinityWaitMs ms for a slot before failing over (reuses proven serial-wait path).
		// A banned/cooldown account: WaitForSlot returns when slot-free, then
		// TryDispatchTrial fails fast → no pointless wait beyond what the slot reports.
		if affinityWaitKey != "" && cfg.AffinityWaitMs > 0 {
			serialWaitKeys[affinityWaitKey] = true
			existing := serialWaitMs[affinityWaitKey]
			if int64(cfg.AffinityWaitMs) > existing {
				serialWaitMs[affinityWaitKey] = int64(cfg.AffinityWaitMs)
			}
		}
```

- [ ] **Step 5: Restructure `DispatchStream` identically (~line 1361–1440 of service.go)**

**OLD** in `DispatchStream` (between BodyPad block and `maxFailover` variable, lines ~1400–1424):

```go
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
```

Also remove the existing `if cfg.AffinityTTLSec > 0 { order = s.pinToAffinity(...) }` block.

**NEW** — replace the full block from `order, resolver, keyOwner, keyCfgS := s.buildCandidates(...)` through the end of the session-exile check:

```go
	order0S, resolver, keyOwner, keyCfgS := s.buildCandidates(ctx, ownerID, model, cfg)
	channels := s.enabledChannels(ctx, ownerID, model)

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

	// ---- TIER 1: hard fallback rules (keyword + model) — HIGHEST priority ----
	if cfg.FallbackEnabled && len(channels) > 0 {
		if fallback.MatchesKeyword(string(body), cfg.FallbackKeywords) {
			if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, "keyword", cfg); committed {
				return out
			}
		}
		if fallback.MatchesModel(model, cfg.FallbackModels) {
			if out, committed := s.streamChannels(ctx, w, channels, body, ownerID, model, "model", cfg); committed {
				return out
			}
		}
	}

	// ---- TIER 2: affinity pin ----
	_, pinActiveS := s.sess.Affinity(conv, nowMs)
	pinnedS := false
	orderS := order0S
	affinityWaitKeyS := ""
	if cfg.AffinityTTLSec > 0 && pinActiveS {
		orderS = s.pinToAffinity(conv, order0S, nowMs)
		pinnedS = true
		if len(orderS) == 1 {
			affinityWaitKeyS = orderS[0]
		}
	}

	// ---- TIER 3: soft rules (probe, price) + session-exile — only when NOT pinned ----
	if !pinnedS {
		est := billing.CostUsd(model, int64(len(body)/4), 2000, 0, 0)
		probeText := lastUserText(body)
		var chPriceThresholdS float64
		if len(channels) > 0 {
			chPriceThresholdS = channels[0].PriceThreshold
		}
		softTrigS := fallback.DecideSoft(fallback.DecideInput{
			Model: model, BodyText: string(body), ProbeText: probeText, EstCostUsd: est, PoolEmpty: len(orderS) == 0,
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
```

**Important:** After this replacement, the rest of `DispatchStream` uses `order` in the node loop — rename all uses of `order` in the loop body to `orderS`, and `keyCfg` → `keyCfgS` (they already use `keyCfgS`). Also rename `order` → `orderS` in the exhausted check at the bottom.

- [ ] **Step 6: Wire `affinityWaitKeyS` into `DispatchStream`'s serialWait maps**

In `DispatchStream`, after the `for _, k := range orderS { if ac, ok := keyCfgS[k]; ... }` loop:

```go
		// Affinity queue-on-busy (same as Dispatch path).
		if affinityWaitKeyS != "" && cfg.AffinityWaitMs > 0 {
			serialWaitKeysS[affinityWaitKeyS] = true
			existing := serialWaitMsS[affinityWaitKeyS]
			if int64(cfg.AffinityWaitMs) > existing {
				serialWaitMsS[affinityWaitKeyS] = int64(cfg.AffinityWaitMs)
			}
		}
```

- [ ] **Step 7: Fix `order` → `orderS` references in `DispatchStream` node loop and bottom exhausted check**

In `DispatchStream`, the node loop currently iterates `for _, key := range order`. Change to `for _, key := range orderS`.

At the bottom exhausted check in `DispatchStream`:
```go
// OLD:
		exReason = "no_nodes"  // was: if len(order) == 0
// NEW:
		if len(orderS) == 0 {
			exReason = "no_nodes"
		}
```

(This was already `len(order)` but must now be `len(orderS)`.)

- [ ] **Step 8: Build and verify no compile errors**

```bash
cd /Users/leo/总控台/tower && go build ./... 2>&1
```

Expected: no output (clean build).

- [ ] **Step 9: Run all dispatch and related tests**

```bash
cd /Users/leo/总控台/tower && go test ./internal/... -count=1 2>&1 | tail -30
```

Expected: all pass. Tests requiring `TEST_DATABASE_URL` will be skipped — that is fine.

- [ ] **Step 10: Self-review checklist (manual inspection — do NOT skip)**

Read both `Dispatch` and `DispatchStream` side by side and verify:
- [ ] Keyword/model are checked BEFORE `pinToAffinity` is called
- [ ] `pinToAffinity` is called only when `AffinityTTLSec > 0 && pinActive`
- [ ] `probe/price/session-exile` are skipped entirely when `pinned == true` / `pinnedS == true`
- [ ] `affinityWaitKey` / `affinityWaitKeyS` is set only when `len(order)==1` / `len(orderS)==1`
- [ ] `affinityWaitKey` is wired into `serialWaitKeys` + `serialWaitMs` with `max(existing, AffinityWaitMs)`
- [ ] Bottom exhausted-fallback path is unchanged in both functions
- [ ] `DispatchStream` uses `orderS` everywhere in the node loop (no stale `order` variable)

- [ ] **Step 11: Commit**

```bash
cd /Users/leo/总控台/tower && git add internal/dispatch/service.go internal/dispatch/affinity_test.go
git commit -m "feat(dispatch): routing priority keyword>affinity>rest + bounded affinity queue-on-busy"
```

---

### Task 4: Frontend — add `AffinityWaitMs` to types and Policies UI

**Files:**
- Modify: `web/spa/src/types.ts`
- Modify: `web/spa/src/pages/Policies.tsx`

**Interfaces:**
- Consumes: `AffinityWaitMs` field in `PolicyPatch` (types.ts) and resolved `Config` (already read from API).
- Produces: A `FieldRow` in the 亲和性 group of the `concurrency` cat, below `AffinityTTLSec`.

- [ ] **Step 1: Add `AffinityWaitMs` to `PolicyPatch` in `types.ts`**

In `web/spa/src/types.ts`, find:

```ts
  AffinityTTLSec?: number;
```

Add after:

```ts
  AffinityWaitMs?: number;
```

Also find `AffinityTTLSec: number;` in the resolved Config interface (line ~214) and add:

```ts
  AffinityWaitMs: number;
```

- [ ] **Step 2: Add `affinityWaitMs` field state in `Policies.tsx`**

Find:
```ts
  const affinityTTLSec = useField<number>(300);
```

Add after:
```ts
  const affinityWaitMs = useField<number>(2000);
```

- [ ] **Step 3: Load `AffinityWaitMs` in the mount effect**

Find (in the mount/load effect, after `setNum(affinityTTLSec, 'AffinityTTLSec')`):
```ts
        setNum(affinityTTLSec, 'AffinityTTLSec');
```

Add after:
```ts
        setNum(affinityWaitMs, 'AffinityWaitMs');
```

- [ ] **Step 4: Emit `AffinityWaitMs` in `buildPatch`**

Find (in the buildPatch / patch-emit block, after `if (affinityTTLSec.enabled) patch.AffinityTTLSec = affinityTTLSec.value;`):
```ts
    if (affinityTTLSec.enabled) patch.AffinityTTLSec = affinityTTLSec.value;
```

Add after:
```ts
    if (affinityWaitMs.enabled) patch.AffinityWaitMs = affinityWaitMs.value;
```

- [ ] **Step 5: Add `affinityWaitMs` to `anyEnabled` array**

Find:
```ts
    affinityTTLSec,
```
(in the `anyEnabled` array)

Add after:
```ts
    affinityWaitMs,
```

- [ ] **Step 6: Add `affinityWaitMs` to `catFields.concurrency`**

Find (in `catFields`):
```ts
    concurrency: [
      maxConcurrent, slotCooldownMinMs,
      affinityTTLSec,
```

Add `affinityWaitMs` after `affinityTTLSec`:
```ts
    concurrency: [
      maxConcurrent, slotCooldownMinMs,
      affinityTTLSec,
      affinityWaitMs,
```

- [ ] **Step 7: Add the `FieldRow` to the 亲和性 group in the JSX**

Find:
```tsx
              <FieldRow label="AffinityTTLSec" desc="亲和性缓存 TTL (秒)" enabled={affinityTTLSec.enabled} onToggle={affinityTTLSec.toggle} showOnlyConfigured={so}>
                <NumInput value={affinityTTLSec.value} onChange={affinityTTLSec.set} disabled={!affinityTTLSec.enabled} min={0} step={60} />
              </FieldRow>
            </div>
```

Replace with:
```tsx
              <FieldRow label="AffinityTTLSec" desc="亲和性缓存 TTL (秒)" enabled={affinityTTLSec.enabled} onToggle={affinityTTLSec.toggle} showOnlyConfigured={so}>
                <NumInput value={affinityTTLSec.value} onChange={affinityTTLSec.set} disabled={!affinityTTLSec.enabled} min={0} step={60} />
              </FieldRow>

              <FieldRow label="AffinityWaitMs" desc="亲和号忙时排队等位上限(ms);0=不等待直接转保底" enabled={affinityWaitMs.enabled} onToggle={affinityWaitMs.toggle} showOnlyConfigured={so}>
                <NumInput value={affinityWaitMs.value} onChange={affinityWaitMs.set} disabled={!affinityWaitMs.enabled} min={0} step={500} />
              </FieldRow>
            </div>
```

- [ ] **Step 8: TypeScript check**

```bash
cd /Users/leo/总控台/tower/web/spa && npx tsc --noEmit 2>&1 | tail -20
```

Expected: no errors.

- [ ] **Step 9: Frontend build**

```bash
cd /Users/leo/总控台/tower/web/spa && npm run build 2>&1 | tail -15
```

Expected: build succeeds.

- [ ] **Step 10: Commit**

```bash
cd /Users/leo/总控台/tower && git add web/spa/src/types.ts web/spa/src/pages/Policies.tsx
git commit -m "feat(ui): add AffinityWaitMs field in Policies 亲和性 group"
```

---

### Task 5: Write report + final verification

**Files:**
- Create: `/Users/leo/总控台/tower/.superpowers/sdd/affinity-priority-report.md`

- [ ] **Step 1: Run full Go test suite**

```bash
cd /Users/leo/总控台/tower && go build ./... && go vet ./... && go test ./internal/... -count=1 2>&1 | tail -30
```

Expected: all pass or skip (skipped = needs TEST_DATABASE_URL).

- [ ] **Step 2: Run frontend checks**

```bash
cd /Users/leo/总控台/tower/web/spa && npx tsc --noEmit && npm run build 2>&1 | tail -10
```

Expected: clean.

- [ ] **Step 3: Get commit hash**

```bash
cd /Users/leo/总控台/tower && git log --oneline -4
```

- [ ] **Step 4: Write report**

```bash
mkdir -p /Users/leo/总控台/tower/.superpowers/sdd
```

Create `/Users/leo/总控台/tower/.superpowers/sdd/affinity-priority-report.md` with:

```markdown
# Affinity Priority Routing — Implementation Report

## New Tier Structure (both Dispatch + DispatchStream, identical)

1. **TIER 1 — Keyword / Model (hard rules):** `fallback.MatchesKeyword` / `fallback.MatchesModel` checked immediately after BodyPad. If matched and FallbackEnabled+channels>0, diverts to `viaChannels`/`streamChannels("keyword"/"model")` and returns. Fires BEFORE any affinity check.

2. **TIER 2 — Affinity pin:** `s.sess.Affinity(conv, nowMs)` → if active and `AffinityTTLSec>0`, calls `pinToAffinity`. Sets `pinned=true`. If pinned to exactly one account, sets `affinityWaitKey` to enable bounded wait.

3. **TIER 3 — Soft rules (only when NOT pinned):** `fallback.DecideSoft` (probe + price only). If triggered, diverts to fallback. Session-exile check also runs here only when not pinned.

4. **Node loop:** unchanged except `affinityWaitKey` is wired into `serialWaitKeys`+`serialWaitMs` with `max(existing, AffinityWaitMs)` — reuses proven `WaitForSlot` path.

5. **Bottom exhausted fallback:** unchanged.

## Fallback Package Additions

- `MatchesKeyword(bodyText, keywords)` — exported wrapper around `containsAny`
- `MatchesModel(model, models)` — exported wrapper around `containsAny`
- `DecideSoft(in)` — returns Probe/Price/Exhausted/None; never Keyword/Model
- Existing `Decide` function preserved intact

## AffinityWaitMs Wiring

`policy.Config.AffinityWaitMs` (default 2000ms). When a conversation is pinned to exactly one account (`len(order)==1` after affinity), that account key is added to `serialWaitKeys` with `max(existing wait, AffinityWaitMs)` ms deadline. The Orchestrator / stream loop calls `s.Store.WaitForSlot(ctx, key, s.Now()+waitMs, s.Now)` — polls every 20ms until a slot is free or deadline reached — then `TryDispatchTrial` proceeds. If the account is banned/cooldown, `WaitForSlot` returns promptly when the slot state reports available, then `TryDispatchTrial` fails fast → failover to exhausted-fallback.

## Test Results

[paste go test output here]

## Build/tsc Output

[paste output here]

## Commits

[paste git log --oneline -4 here]
```

- [ ] **Step 5: Commit report**

```bash
cd /Users/leo/总控台/tower && git add .superpowers/sdd/affinity-priority-report.md
git commit -m "docs: add affinity-priority routing implementation report"
```

---

## Self-Review Checklist

### Spec coverage:
- [x] `MatchesKeyword`, `MatchesModel`, `DecideSoft` added to fallback package — Task 1
- [x] Unit tests for those three functions — Task 1
- [x] `DecideSoft` never returns Keyword/Model — `TestDecideSoft_NeverReturnsKeywordOrModel` — Task 1
- [x] `AffinityWaitMs` in Config/Defaults/Patch/apply/DryRun — Task 2
- [x] `AffinityWaitMs` default 2000 — Task 2
- [x] Both `Dispatch` and `DispatchStream` restructured identically — Task 3
- [x] Keyword/model before affinity — Task 3 Steps 3+5
- [x] Probe/price/session-exile skipped when pinned — Task 3 Steps 3+5
- [x] `affinityWaitKey` wired into serialWait maps — Task 3 Steps 4+6
- [x] Bottom exhausted-fallback unchanged — Task 3 Step 10
- [x] Frontend `AffinityWaitMs` field in 亲和性 group — Task 4
- [x] `go build/vet/test` green — Task 5
- [x] `tsc --noEmit && npm run build` green — Task 5
- [x] Report written to `.superpowers/sdd/affinity-priority-report.md` — Task 5
- [x] Commit message matches spec — Task 3 Step 11

### Placeholder scan:
No TBD, TODO, or vague steps — all code blocks are complete and exact.

### Type consistency:
- `affinityWaitKey` (string) set from `order[0]` (string) — consistent
- `serialWaitKeys[affinityWaitKey]` (map[string]bool) — consistent with existing map type
- `serialWaitMs[affinityWaitKey]` (map[string]int64) — `cfg.AffinityWaitMs` is `int`, cast to `int64` — consistent
- `fallback.MatchesKeyword(bodyText string, keywords []string) bool` — matches usage in service.go
- `fallback.DecideSoft(in fallback.DecideInput) fallback.Trigger` — matches usage
- Stream path: `order0S`, `orderS`, `pinnedS`, `affinityWaitKeyS`, `pin ActiveS` — all local to DispatchStream, no collision with Dispatch locals

**One naming note:** The DispatchStream path uses local variable names ending in `S` (e.g. `order0S`, `orderS`) to avoid any shadowing concern even though they're in a separate function. This is slightly verbose but safe and clear.
