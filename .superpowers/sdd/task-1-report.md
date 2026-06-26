# Task 1 Report: policy.Config — 1M long-context gating fields (#143)

## Status
DONE

## Commit
- `c203689` — feat(policy): 1M long-context gating config fields (#143, default off)

## Test result
`go test ./internal/policy/ -run TestApplyLongContextFields` — PASS (0.00s)
Full suite: 19/19 tests pass. `go build ./...` clean.

## What Was Implemented

6 fields added in all 5 places in `internal/policy/policy.go`:

1. **Config struct**: `LongContextGateEnabled bool`, `LongContextTokenThreshold int`, `LongContextModelMarkers []string`, `ExtraUsageKeywords []string`, `ExtraUsageStatusCodes []int`, `No1MRecoveryMs int64` — inserted before `QuotaLimitKeywords` block, with #143 comment.

2. **Defaults()**: `LongContextGateEnabled=false`, `LongContextTokenThreshold=200000`, `LongContextModelMarkers=["1m"]`, `ExtraUsageKeywords=["draw from your external","extra usage"]`, `ExtraUsageStatusCodes=[400]`, `No1MRecoveryMs=86400000`.

3. **Patch struct**: 6 pointer fields (`*bool`, `*int`, `*[]string`, `*[]string`, `*[]int`, `*int64`) inserted before `QuotaLimitKeywords *[]string`.

4. **apply()**: 6 nil-guard `if p.X != nil { c.X = *p.X }` blocks inserted before the QuotaLimit blocks.

5. **DryRun()**: 6 `add(field, base.X, final.X)` calls inserted before Phase 5 reactive quota. Used same `add(...)` style as all other fields — `fmt.Sprintf("%v", ...)` handles slices natively, no wrapping needed.

Test added to `internal/policy/policy_test.go` — `TestApplyLongContextFields` (verbatim from brief).

## Concerns
None. Feature is behavior-neutral by default (`LongContextGateEnabled=false`). Tasks 3/4/5/6 can now consume these fields from `policy.Config`.
