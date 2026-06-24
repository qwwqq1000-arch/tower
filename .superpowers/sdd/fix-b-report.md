# Fix-B Report: SerialQueue Bounded Wait + RateExceedAction De-trap

Date: 2026-06-24
Branch: feat/anticontrol-phase1

---

## Fix 1: SerialQueue Bounded Wait

### What was done

Added three methods to `internal/state/store.go`:

- `NowMs() int64` — exposes the store's injected clock for external callers.
- `SlotAvailable(key string, now int64) bool` — lock-guarded check; returns true if
  `a.Slots.Available(now) > 0`. Returns false for unknown keys.
- `WaitForSlot(key string, deadlineMs int64, now func() int64) bool` — polls
  `SlotAvailable` every 20 ms until a slot is free (true) or deadline passes (false).
  The store mutex is NOT held during sleep — only acquired briefly on each poll —
  so there is no contention and no risk of holding the lock across a sleep.

Added two optional fields to `Orchestrator` (`internal/dispatch/orchestrator.go`):

- `NowMs func() int64` — clock for computing deadlines (nil = feature off).
- `SerialWaitKeys map[string]bool` — which keys have serial wait enabled.
- `SerialWaitMs map[string]int64` — per-key wait deadline in ms.

In `Orchestrator.Dispatch`, before calling `attempt`, if `NowMs != nil &&
SerialWaitKeys[key]` and `SerialWaitMs[key] > 0`, calls `WaitForSlot`. If it
returns false (timeout), `continue` — same behaviour as slot-busy, does NOT
consume an attempt count.

Wired into `Service.Dispatch` and `Service.DispatchStream` (`internal/dispatch/service.go`):

- `buildCandidates` now returns a fourth value `map[string]policy.Config` (keyCfg).
- After `buildCandidates`, both paths iterate over `order` and populate
  `serialWaitKeys`/`serialWaitMs` for keys where `acfg.SerialQueueEnabled &&
  acfg.SerialQueueWaitMs > 0`.
- `Service.Dispatch` passes these maps + `NowMs: s.Now` to the `Orchestrator`.
- `Service.DispatchStream` (which uses its own loop, not Orchestrator) applies
  the same wait inline before `TryDispatchTrial`.

### Bound

Wait is strictly bounded by `SerialQueueWaitMs` ms (default 2000 ms). When
`SerialQueueEnabled=false` (default), the maps are empty and the nil/empty guard
in `Orchestrator.Dispatch` adds zero overhead (no loop, no check, no sleep).

### Why safe

- `WaitForSlot` never holds the store mutex during sleep → race-free.
- Timeout is always enforced (`now() >= deadlineMs` check at top of each iteration).
- Unknown key returns false immediately (never blocks).
- Does not consume attempt count → failover accounting is unaffected.

---

## Fix 2: RateExceedAction="delay" De-trap

### What was done

**UI** (`web/spa/src/pages/Policies.tsx`):
- Removed `<option value="delay">delay（延迟等待）</option>` from the
  RateExceedAction select element.
- Updated the group description text to remove mention of "delay".
- Updated the FieldRow `desc` to say only "rotate" is available.

**Policy** (`internal/policy/policy.go`):
- Updated the comment on `RateExceedAction` from `"delay" is TODO` to
  `only "rotate" is supported; "delay" was removed (see fix-b-report.md)`.
- Default `RateExceedAction: "rotate"` is unchanged.
- The field itself is kept (existing DB rows may contain "delay"; the rotate
  path is the only active code path regardless of stored value).

### Why "delay" was removed rather than implemented

The rate check happens inside `buildCandidates` during candidate filtering BEFORE
the Orchestrator runs. The filtered order returned is already rate-checked. Implementing
"delay" safely would require either re-running `buildCandidates` after a sleep (costly,
not safe under concurrent load) or a significant per-key rate re-check inside the
attempt loop. The cleanest bounded retrofit is to remove the UI option so the field
can never be a silent no-op trap for users who select it.

---

## TDD Evidence

### WaitForSlot tests (RED → GREEN)

RED run output:
```
internal/state/wait_test.go:20:11: s.WaitForSlot undefined (type *Store has no field or method WaitForSlot)
internal/state/wait_test.go:44:11: s.WaitForSlot undefined (type *Store has no field or method WaitForSlot)
internal/state/wait_test.go:62:11: s.WaitForSlot undefined (type *Store has no field or method WaitForSlot)
FAIL  github.com/qwwqq1000-arch/tower/internal/state [build failed]
```

GREEN run (after implementation):
```
=== RUN   TestWaitForSlot_AlreadyFree
--- PASS: TestWaitForSlot_AlreadyFree (0.00s)
=== RUN   TestWaitForSlot_FreesInTime
--- PASS: TestWaitForSlot_FreesInTime (0.04s)
=== RUN   TestWaitForSlot_Timeout
--- PASS: TestWaitForSlot_Timeout (0.06s)
PASS
ok    github.com/qwwqq1000-arch/tower/internal/state  1.650s
```

### Orchestrator serial-wait tests (RED → GREEN)

RED run output:
```
internal/dispatch/serial_queue_test.go:50:3: unknown field NowMs in struct literal of type Orchestrator
internal/dispatch/serial_queue_test.go:51:3: unknown field SerialWaitKeys in struct literal of type Orchestrator
internal/dispatch/serial_queue_test.go:52:3: unknown field SerialWaitMs in struct literal of type Orchestrator
FAIL  github.com/qwwqq1000-arch/tower/internal/dispatch [build failed]
```

GREEN run (after implementation):
```
=== RUN   TestSerialWait_SlotFreedBeforeDispatch
--- PASS: TestSerialWait_SlotFreedBeforeDispatch (0.04s)
=== RUN   TestSerialWait_Timeout
--- PASS: TestSerialWait_Timeout (0.06s)
PASS
ok    github.com/qwwqq1000-arch/tower/internal/dispatch  1.087s
```

---

## Race Test Result

```
ok  github.com/qwwqq1000-arch/tower/internal/dispatch  1.767s
ok  github.com/qwwqq1000-arch/tower/internal/state     2.104s
```

PASS — no data races detected.

---

## Files Changed

- `internal/state/store.go` — added `NowMs`, `SlotAvailable`, `WaitForSlot`; added `"time"` import
- `internal/state/wait_test.go` — new file: `TestWaitForSlot_AlreadyFree`, `_FreesInTime`, `_Timeout`
- `internal/dispatch/orchestrator.go` — added `NowMs`, `SerialWaitKeys`, `SerialWaitMs` fields; wired wait in `Dispatch`
- `internal/dispatch/serial_queue_test.go` — added `TestSerialWait_SlotFreedBeforeDispatch`, `TestSerialWait_Timeout`
- `internal/dispatch/service.go` — `buildCandidates` returns 4th value `keyCfg`; both `Dispatch` and `DispatchStream` populate and use serial-wait maps; updated TODO comment
- `internal/dispatch/service_test.go` — updated 3 `buildCandidates` call sites to unpack 4 return values
- `internal/policy/policy.go` — updated `RateExceedAction` comment
- `web/spa/src/pages/Policies.tsx` — removed `delay` option; updated description texts
