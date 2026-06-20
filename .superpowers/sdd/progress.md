# SDD Progress Ledger — fix(dispatch): trial settlement robustness

Tasks:
1. Add TryDispatchTrial + OnTrialResult to store.go — PENDING
2. Update orchestrator.go attempt() to use TryDispatchTrial — PENDING
3. Add two new tests — PENDING

Task 1 (store.go TryDispatchTrial + OnTrialResult): complete (commits d83085c..7db3dde, review clean)
Task 2 (orchestrator.go attempt via TryDispatchTrial): complete (same commit 7db3dde, review clean)
Task 3 (two new tests): complete (same commit 7db3dde, review clean)
