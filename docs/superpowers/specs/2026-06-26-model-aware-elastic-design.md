# Model-Aware Elastic (模型感知弹性) — Design

**Date:** 2026-06-26
**Goal:** Stop model-pin from silently pulling reserve accounts into rotation. Make
model-pin select WITHIN the elastic active set, and only scale up ONE reserve account
for a model when the active set genuinely can't serve it — with full event + status
visibility. Everything behind a switch; nothing hardcoded.

## Problem (observed live)
Traffic was multi-model (opus-4-6, opus-4-7… , sonnet-4-6, opus-4-8). `ModelPinMode=sticky`
filtered model-incompatible accounts BEFORE the elastic baseline partition, so the
baseline ("first N by weight") shrank and pulled in reserve accounts. Result: non-affinity
requests hit待命 accounts (no scale-up event, no ban) — violating the elastic working-set model.

## Design
New behavior gated by a NEW toggle `ModelElasticEnabled` (default **false** = current
behavior; turned on per-policy). Active only when `ModelElasticEnabled && ModelPinEnabled
&& ElasticEnabled`.

1. **Baseline is model-agnostic.** The model-pin filter is NOT applied while building
   `cands`; the elastic baseline = fixed first-N by weight-desc + key-asc, regardless of
   each account's pinned model.
2. **Model-pin selects within the active set.** For model M, eligible = accounts whose
   pinned model is M or which are unpinned. Pick eligible accounts from the active set
   (baseline, + reserve if concurrency-scaled by util).
3. **Model scale-up (one at a time).** If NO active-set account is eligible for M:
   - prefer reserve accounts already pinned to M (already the "M reserves") — use them, no new event;
   - else activate the FIRST unpinned reserve account for M, emit `model_scale_up`
     (target = account, detail = {model: M}); it pins to M on success.
   - if every account is pinned to other models, fall back to the active set and emit
     `model_exhausted` (deduped) so the operator sees "not enough accounts for M".
4. **Pin on first serve (existing).** `RecordModel` pins an account to a model on its first
   success; now it returns whether the pin was NEW so logOK can emit `model_pin` ("号X 首次
   钉定 M"). Pins expire on the existing AffinityTTL, so a scaled-down idle account frees up.
5. **Events (fine-grained):** `model_pin`, `model_scale_up`, `model_exhausted` + existing
   `scale_up`/`scale_down`. Deduped where they'd otherwise repeat per-request.
6. **Status visibility:** each account's current pinned model is surfaced in the dispatch
   status (`pinnedModel`) and shown in the concurrency panel + 号库.

## Switches (nothing hardcoded)
- `ModelElasticEnabled` (new, default false) — master switch for this behavior.
- Composes with existing `ModelPinEnabled`, `ElasticEnabled`, `ElasticBaselineCount`,
  `ElasticMaxReserve`, `AffinityTTLSec` (model-pin TTL). No magic numbers introduced.

## Non-elastic / model-pin-off paths unchanged
- ModelPinEnabled=false → no model filter (baseline serves all models).
- ElasticEnabled=false → existing non-elastic path (model-pin filters the full order).
- ModelElasticEnabled=false → existing (buggy) behavior, untouched — safe rollback.
