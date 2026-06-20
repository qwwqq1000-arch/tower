# Task 1 Report: settle trials via OnTrialResult

## Files Changed

- `internal/state/store.go` — refactored `TryDispatch` to delegate to new `TryDispatchTrial`; added `TryDispatchTrial` and `OnTrialResult` methods
- `internal/dispatch/orchestrator.go` — updated `attempt()` to call `TryDispatchTrial`, branch settlement by `trial` flag, and guard proxy panics with `sendReturned` (slot released, breaker NOT penalized on panic)
- `internal/dispatch/orchestrator_test.go` — appended `TestDispatch_TrialNoWedge_PersistStreak3`, `TestDispatch_PanicProxySettles`, and `panicSendProxy` stub

## Notable deviation from spec

The spec's `attempt()` called `settle(ok)` on defer which would call `OnBanSignal` on a proxy panic. With `newOrch()`'s `PersistStreak=1`, this opened the breaker, causing `TestDispatch_PanicProxySettles` to fail because `TryDispatch` saw a banned account (not a held slot). Fixed by adding a `sendReturned` boolean: if `Send` panics, only `Complete` (slot release) runs — no breaker penalty.

## Build/Vet Output

```
BUILD OK
VET OK
```

## Test Output (new tests)

```
=== RUN   TestDispatch_TrialNoWedge_PersistStreak3
--- PASS: TestDispatch_TrialNoWedge_PersistStreak3 (0.00s)
=== RUN   TestDispatch_PanicProxySettles
--- PASS: TestDispatch_PanicProxySettles (0.00s)
```

All 10 dispatch tests PASS; all 13 state tests PASS (1 SKIP: DB not configured). Race detector clean.

## Commit SHA

7db3dde
