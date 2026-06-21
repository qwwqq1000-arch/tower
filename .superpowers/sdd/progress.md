# SDD Progress Ledger — feat(dispatch): elastic scaling

Tasks:
1. policy.Config: ElasticEnabled, ElasticScaleUpUtil, ElasticMaxReserve fields + Patch + apply + DryRun — PENDING
2. buildCandidates: elastic logic (partition, util, pickElastic helper, scaledUp map, events) — PENDING
3. Tests: pickElastic table test + build/vet/race verification — PENDING

Task 1: complete (policy.Config elastic fields + Patch + apply + DryRun)
Task 2: complete (buildCandidates elastic logic, scaledUp map, pickElastic helper)
Task 3: complete (TestPickElastic table test, build/vet/race all pass)
Commit: 2f542fb feat(dispatch): elastic scaling — activate reserve accounts when baseline saturated
