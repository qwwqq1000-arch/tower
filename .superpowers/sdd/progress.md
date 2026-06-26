# SDD Progress Ledger — #137 号库归属 dropdown + #2 三件套 envelope (2026-06-26)

Plans: docs/superpowers/plans/2026-06-26-node-account-owner-dropdown.md (#137, 5 tasks)
       docs/superpowers/plans/2026-06-26-cc-envelope-strategy.md (#2, 6 tasks)
Branch: feat/anticontrol-phase1
Base commit: ed6ea17 (docs: implementation plans)
Mode: subagent-driven, autonomous continuous (user authorized "不用问我"), implement→review→fix per task.
Recovery: each task = git commit(s). Trust git log + this ledger after compaction.
Pre-flight: clean. Note: #2 Task 4 TestBothTiersConsultEnvelope is a source-grep tier-drift guard (legit, used elsewhere) — kept.

## #137
(populated as tasks complete)

## #2
(populated as tasks complete)
Task 1: complete (commit b362962, review clean — spec ✅ + quality approved). nodes.account_owner_id column + sqlc Node.AccountOwnerID + SetNodeAccountOwner.
Task 2: complete (commit c84dfae, review clean — spec ✅ + quality approved). discovery Sync uses AccountOwnerID then OwnerID fallback + test.
Task 3: complete (commit a3308d8, review clean — spec ✅ + quality approved). createNodeHandler resolves accountOwnerId (default test). MINOR(final-review): default-tenant fallback path has no automated test.
Task 4: complete (commit 05e5404, controller-reviewed clean — diff in context: overwrite UPDATE +account_owner_id=$7=tenant.ID args correct, CreateNode +AccountOwnerID, inline UPDATE accounts removed). spec ✅.
Task 5: complete (commit 6c94cb0, review clean — spec ✅ + quality approved). frontend 号库归属 dropdown, default test, SPA builds 0 errors. MINOR(final): after submit the test default not re-applied on reset (in-spec).

#137 DONE: 5/5 tasks complete, commits b362962..6c94cb0, all reviews clean.

## #2
#2 Task 1: complete (commit 502a164 + fix 3d52733, review clean — spec ✅ all 9 fields in all 5 places; quality: removed YAGNI exported Apply wrapper, test calls apply() in-package). NOTE plan said diff() but real fn is DryRun() — implementer used correct one.
#2 Task 2: complete (commit 54b1bcb, review clean — spec ✅ + quality approved). missingEnvelopePieces detection. MINOR(final-review): array-of-blocks system shape handled in code but not unit-tested (only string form tested) — spec says handle BOTH shapes; add an array test case at final review.
#2 Task 3: complete (commit 5366d80, review clean — spec ✅ + quality approved). ctx plumbing WithRequestQuery/withEnvelopeInject + handler wiring.
#2 Task 4: complete (commit 4b9c59d, review clean — spec ✅ PASS + quality approved). envelope gate in Dispatch(602)+DispatchStream(1986), fully behind CCEnvelopeEnabled, fallback reuses viaChannels/streamChannels reason="envelope", ctx propagation verified. MINOR(cosmetic): DispatchStream uses exReason var vs inline literal.
#2 Task 5: complete (commit 4985e0b, review clean — spec ✅ PASS + quality approved). injectEnvelope into NodeProxy.Send+OpenStream (not ChannelProxy), ?beta=true URL node-only, headers override, body+ContentLength rebuilt, no-op when empty, EnvVals at both sites. MINOR(final-review): change-detection guard does full body string compare on hot path — could reassign unconditionally.
#2 Task 6: complete (commit 721ffc0, review clean — spec ✅ + quality approved). Policies "Claude Code 三件套" group: 9 fields wired into PolicyPatch(types.ts)+hooks+allFields+hydrateFrom+buildPatch+anyEnabled+catFields.cadence; render GroupMaster+3 piece toggles+select(走保底/补全→fallback/complete)+4 text inputs, all showOnlyConfigured={so}. Go names byte-for-byte. SPA builds 0 errors. No findings.

#2 DONE: 6/6 tasks complete, commits 502a164..721ffc0, all reviews clean.

## Final whole-branch review — DONE (opus, review-ed6ea17..721ffc0.diff, 12 commits 24 files)
Verdict: merge after fixing I-1. All invariants verified: default-off neutrality, tier mirroring (Dispatch+DispatchStream), injection target isolation (NodeProxy only, not ChannelProxy), owner semantics, sqlc Scan/column alignment, nil-safety/concurrency. build+vet+test green.
 - I-1 (Important): array-form `system` data-loss in injectEnvelope complete path — FIXED commit db67cda (unshift text block preserving content) + 4 array/string injection unit tests. go build/vet/test ./internal/dispatch all pass. Covers batched MINOR #3 too.
 - Deferred Minors (reviewer-approved defer): #1 default-tenant test (optional), #2 form-reset default (in-spec cosmetic), #4 exReason var (non-issue), #5 body string-compare (negligible, only on complete path).

Pre-deploy verify @db67cda: go build ./... OK, go vet ./... clean, go test policy/dispatch/cpaclient/api/db all ok. SPA build 0 errors.

## Original batched MINOR findings (handed to final reviewer):
 - #137 T3: createNode default-tenant fallback path has no automated test.
 - #137 T5: after submit the test default not re-applied on form reset (in-spec).
 - #2 T2: array-of-blocks `system` shape handled in code but only string form unit-tested — spec says handle BOTH; add an array test case.
 - #2 T4: DispatchStream uses exReason var vs inline literal (cosmetic).
 - #2 T5: change-detection guard does full body string compare on hot path — could reassign unconditionally.
