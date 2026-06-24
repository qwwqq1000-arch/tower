# 封控策略重构 Phase 2(自保限额)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development。步骤用 `- [ ]` 勾选。

**Goal:** 给每个号加 5h/7d **花费上限**(区间种子、到顶主动退出、窗口滚动后重置),给保底渠道加**花费上限自动跳过/禁用**,并移除已被花费上限替代的 `QuotaRotateThreshold`(顺序:先上花费上限,再删阈值——不留空窗)。

**Architecture:** 花费累加器走 `state.Account` 内存态(每请求 O(1),不查库);花费上限是 Phase 1 账户作用域的**第一个真实消费者**——dispatch 在选定账号后用 `resolveConfig(ctx, ownerID, accountID)` 取该号配置,用 `RangeF.Resolve(accountID, "spend5h")` 种子定该号的上限。到顶复用 `SetLimited` 同一限额机制。保底花费上限在 `enabledChannels` 用已有 `fallback_spend` 表过滤。

**Tech Stack:** Go(Postgres + sqlc/goose)、React/TS SPA。测试:`go test`、`tsc --noEmit`、`npm run build`。

## Global Constraints(逐条,每任务隐含适用)

- 区间种子:每号用 `RangeF.Resolve(accountID, salt)` 在区间内定**稳定**上限(同号跨重启一致);salt 区分 `"spend5h"`/`"spend7d"`。
- 花费累加器**全内存**(`state.Account`),每请求 O(1),不每请求查库;首版重启清零(标 TODO 持久化)。
- 窗口语义:从触达点起的固定窗口(5h/7d),到顶 `SetLimited(key,{"all":now+窗口长})` 并在 reset 时清零重计。
- 删 `QuotaRotateThreshold` **必须排在花费上限上线之后**(无空窗);删除要完整(policy + PickThreshold + poller 轮换投影 + cpaclient/discovery + telemetry/map + account_handlers overlay),不留半删死代码。
- 零写死:窗口长度(5h=18000000ms / 7d=604800000ms)进 Config。
- 代码推 `qwwqq1000-arch`;密钥仅服务器侧;日志脱敏。

## 文件结构

- 改 `internal/state/account.go` — Account 加花费窗口累加器 + `AddSpend`/`SpendOver` 方法。
- 改 `internal/state/store.go` — `AddSpend(key, cost, now)` 包装 + 必要锁。
- 改 `internal/policy/policy.go` — 加 SpendCap* 字段(Config/Patch/apply/Defaults);**删** QuotaRotateThreshold + PickThreshold。
- 改 `internal/dispatch/service.go` — 节点成功路径 hook 花费累加 + 到顶 SetLimited;`enabledChannels` 加保底花费上限过滤。
- 改 `internal/telemetry/poller.go`、`internal/cpaclient/discovery.go`、`internal/telemetry/map.go`、`internal/api/account_handlers.go` — 摘除 QuotaRotateThreshold 轮换。
- 新 migration + `queries/fallback_channels.sql` + `internal/db/sqlc/*` — 保底 spend_cap 列。
- 改 `web/spa/src/pages/Policies.tsx`(SpendCap RangeInput)、`Fallback.tsx`(spend cap 输入 + 删 weight 输入)、`Dispatch.tsx`/`tenant.tsx`(删 `ch.weight` 渲染)。

---

### Task 1: state — 每号花费窗口累加器

**Files:** Modify `internal/state/account.go`; Test `internal/state/spend_test.go`

**Interfaces:**
- Produces: `Account.AddSpend(now, cost int64ms?, windowMs int64) ...` — 见下。`Store.AddSpend(key string, costUsd float64, now int64, win5h, win7d int64) (sum5h, sum7d float64)`。

- [ ] **Step 1: 失败测试**
```go
// internal/state/spend_test.go
package state

import "testing"

func TestSpendWindowAccumulateAndRoll(t *testing.T) {
	a := NewAccount(3)
	const win = int64(18000000) // 5h
	// t=0 累加 30
	a.AddSpend(0, 30, win)
	if got := a.SpendInWindow(0, win); got != 30 {
		t.Fatalf("窗口内应=30,得 %v", got)
	}
	// t=win-1 再加 30 → 窗口内 60(都在窗口)
	a.AddSpend(win-1, 30, win)
	if got := a.SpendInWindow(win-1, win); got != 60 {
		t.Fatalf("应=60,得 %v", got)
	}
	// t=win+1 → 第一笔(t=0)已滚出,只剩第二笔 30
	if got := a.SpendInWindow(win+1, win); got != 30 {
		t.Fatalf("滚动后应=30,得 %v", got)
	}
}
```

- [ ] **Step 2:** `go test ./internal/state/ -run TestSpendWindow -v` → FAIL

- [ ] **Step 3: 实现**(Account 加一个时间戳+金额的环形/切片记录,SpendInWindow 求和窗口内,顺带裁剪过期)
```go
// 在 Account struct 内加:
//   spendLog []spendEntry  // 升序时间
// type spendEntry struct { ts int64; usd float64 }

type spendEntry struct {
	ts  int64
	usd float64
}

// AddSpend 记一笔花费并裁剪掉早于 (now-windowMs) 的旧记录(用最长窗口裁剪由调用方保证;
// 这里用传入 windowMs 裁剪,调用时传 7d 最长窗口避免误裁 7d 数据 —— 见 Store.AddSpend)。
func (a *Account) AddSpend(now int64, usd float64, pruneWindowMs int64) {
	a.spendLog = append(a.spendLog, spendEntry{ts: now, usd: usd})
	cut := now - pruneWindowMs
	i := 0
	for i < len(a.spendLog) && a.spendLog[i].ts < cut {
		i++
	}
	if i > 0 {
		a.spendLog = a.spendLog[i:]
	}
}

// SpendInWindow 求和 [now-windowMs, now] 内的花费。
func (a *Account) SpendInWindow(now, windowMs int64) float64 {
	cut := now - windowMs
	var sum float64
	for _, e := range a.spendLog {
		if e.ts > cut {
			sum += e.usd
		}
	}
	return sum
}
```
(测试里 `AddSpend(0,30,win)` 的 pruneWindowMs 用 win=5h;为同时支持 7d,Store.AddSpend 用 7d 做 prune——见 Task 2。测试用 win 一致即可通过。)

- [ ] **Step 4:** `go test ./internal/state/ -run TestSpendWindow -v` → PASS;`go build ./...`
- [ ] **Step 5: 提交** `git commit -m "feat(state): 每号花费窗口累加器(AddSpend/SpendInWindow)"`

---

### Task 2: policy — SpendCap 配置字段 + 窗口长度

**Files:** Modify `internal/policy/policy.go`; 复用 `internal/policy/range.go`(RangeF)

加字段(Config + Defaults + Patch 指针 + apply,nil 安全):
- `SpendCap5hEnabled bool`(默认 false)、`SpendCap5hUsd RangeF`(默认 `{100,200}`)
- `SpendCap7dEnabled bool`(默认 false)、`SpendCap7dUsd RangeF`(默认 `{500,1000}`)
- `SpendWindow5hMs int64`(默认 `18000000`)、`SpendWindow7dMs int64`(默认 `604800000`)

- [ ] **Step 1: 失败测试**(在现有 policy_test 风格)
```go
func TestSpendCapDefaults(t *testing.T) {
	d := Defaults()
	if d.SpendCap5hUsd.Min != 100 || d.SpendCap5hUsd.Max != 200 { t.Fatal("5h 默认区间错") }
	if d.SpendWindow7dMs != 604800000 { t.Fatal("7d 窗口长错") }
}
func TestSpendCapPatch(t *testing.T) {
	c := Defaults()
	en := true
	apply(&c, Patch{SpendCap5hEnabled: &en, SpendCap5hUsd: &RangeF{Min:50,Max:60}})
	if !c.SpendCap5hEnabled || c.SpendCap5hUsd.Max != 60 { t.Fatal("patch 未生效") }
}
```
- [ ] **Step 2:** `go test ./internal/policy/ -run TestSpendCap -v` → FAIL
- [ ] **Step 3:** 加字段到 Config/Defaults/Patch/apply(RangeF 指针在 Patch;apply 里 `if p.SpendCap5hUsd != nil { c.SpendCap5hUsd = *p.SpendCap5hUsd }`)。
- [ ] **Step 4:** `go test ./internal/policy/ -run TestSpendCap -v` → PASS;`go build ./...`
- [ ] **Step 5: 提交** `git commit -m "feat(policy): 5h/7d 花费上限配置(区间)+ 窗口长度"`

---

### Task 3: dispatch — 节点成功后累加花费 + 到顶主动退出(账户作用域首个真实消费者)

**Files:** Modify `internal/dispatch/service.go`; Modify `internal/state/store.go`(加 `AddSpend` 包装)

**Interfaces:** Consumes `state.Account.AddSpend/SpendInWindow`(T1)、`policy.Config.SpendCap*`(T2)、Phase1 `resolveConfig(ctx,ownerID,accountID)` + `RangeF.Resolve`。

逻辑:节点账号成功服务一次后(`cost := billing.CostUsdFull(...)` 处,service.go:888 与 :1006 两条节点成功路径),对该号 key:
1. `store.AddSpend(key, cost, now)`(内部用 7d 窗口 prune)。
2. 取该号 cfg = `resolveConfig(ctx, ownerID, accountID)`(**这里第一次传真实 accountID**——账号已选定)。
3. 若 `cfg.SpendCap5hEnabled`:`cap5 := cfg.SpendCap5hUsd.Resolve(accountID,"spend5h")`;`if store.SpendInWindow(key, now, cfg.SpendWindow5hMs) >= cap5 { store.SetLimited(key, cfg.MaxConcurrent, map[string]int64{"all": now+cfg.SpendWindow5hMs}); 记 quota_limited 事件(reason="spend5h") }`。
4. 7d 同理(salt `"spend7d"`,窗口 `SpendWindow7dMs`)。

- [ ] **Step 1: 失败测试**(单测 Store.AddSpend + 一个判定 helper;把"是否到顶"抽成纯函数便于测)
```go
// internal/dispatch/spendcap_test.go
package dispatch
import "testing"
func TestSpendCapHit(t *testing.T) {
	// overCap(sum, capUsd) 纯函数
	if !overCap(150, 140) { t.Fatal("150>=140 应到顶") }
	if overCap(100, 140) { t.Fatal("100<140 不应到顶") }
}
```
- [ ] **Step 2:** `go test ./internal/dispatch/ -run TestSpendCapHit -v` → FAIL
- [ ] **Step 3:** 加 `func overCap(sum, capUsd float64) bool { return capUsd > 0 && sum >= capUsd }`;`Store.AddSpend(key string, usd float64, now int64)`(取/建 account,加锁,调 `a.AddSpend(now, usd, 604800000)`);在 service.go 两条节点成功路径接入上面 1-4 逻辑(用 overCap 判定)。注意:accountID 从该号 key 解析(key 格式 `nodeId:profileId`;account 业务 ID 用于种子——用 key 即可作为 seed 输入,稳定即可)。
- [ ] **Step 4:** `go test ./internal/dispatch/ -run TestSpendCap -v` → PASS;`go build ./... && go test ./internal/dispatch/...`
- [ ] **Step 5: 提交** `git commit -m "feat(dispatch): 节点花费累加 + 5h/7d 上限到顶主动退出(账户作用域首个消费者)"`

---

### Task 4: 移除 QuotaRotateThreshold(此时花费上限已顶上,无空窗)

**Files:** `internal/policy/policy.go`、`internal/telemetry/poller.go`、`internal/telemetry/map.go`、`internal/cpaclient/discovery.go`、`internal/api/account_handlers.go`、`cmd/tower/main.go`、前端 `Policies.tsx`(删 quotaRotateThreshold 字段)

- [ ] **Step 1:** 删 `policy.go`:`QuotaRotateThreshold`(Config/Defaults/Patch/apply/DryRun add)+ `PickThreshold` 函数。
- [ ] **Step 2:** `poller.go`:删 `threshold()` 及 PollOnce 里按利用率 `SetLimited`/轮换的投影;保留 poller 的健康/鉴权刷新等其它职责(**只删利用率轮换那段**)。`map.go`/`discovery.go:86/127`:删 Threshold 字段与 `PickThreshold` 调用及其投影。`account_handlers.go:289`:删"saturated past QuotaRotateThreshold"叠加层(改依赖 `LimitState`)。
- [ ] **Step 3:** `cmd/tower/main.go:77`:Poller 构造里删 `Threshold: 0.95`(若字段删了)。前端 `Policies.tsx`:删 `quotaRotateThreshold` 字段块。
- [ ] **Step 4:** `go build ./... && go vet ./... && go test ./...`;`cd web/spa && npx tsc --noEmit && npm run build`。全绿。grep 确认无残留 `QuotaRotateThreshold`/`PickThreshold`。
- [ ] **Step 5: 提交** `git commit -m "chore(policy): 移除 QuotaRotateThreshold/PickThreshold + poller 利用率轮换(已由花费上限+反应式替代)"`

---

### Task 5: 保底渠道花费上限(自动跳过/禁用)

**Files:** 新 migration + `queries/fallback_channels.sql` + `internal/db/sqlc/*` + `internal/dispatch/service.go`(enabledChannels)

加列(区间用 min/max):`spend_cap_daily_min_usd`、`spend_cap_daily_max_usd`、`spend_cap_total_min_usd`、`spend_cap_total_max_usd`、`spend_cap_action TEXT`(`skip`|`disable`,默认 `skip`)。

- [ ] **Step 1: migration**(时间戳排在最新之后,可逆 Down 删列)。
- [ ] **Step 2:** `queries/fallback_channels.sql` 的 create/update/select 加这些列;`sqlc generate`。
- [ ] **Step 3: 失败测试**
```go
// internal/dispatch/fallback_spendcap_test.go
func TestFallbackOverDailyCap(t *testing.T) {
	if !overCap(120, 100) { t.Fatal() } // 复用 overCap
}
```
(主要逻辑测试:enabledChannels 跳过逻辑——若构造 Service 重可改为对一个纯过滤 helper 测。)
- [ ] **Step 4:** `enabledChannels`(service.go:~743 区域)在筛选时:对每个渠道用 `RangeF.Resolve(ch.ID,"fbdaily")` 得日上限,`GetFallbackSpendToday(ch.ID, today)` 得今日花费,`overCap(today, capDaily)` 则跳过(action=disable 时另调 `SetFallbackEnabled(ch.ID,false)`);总上限同理用 `GetFallbackSpendTotal`。
- [ ] **Step 5:** `go build ./... && go test ./internal/dispatch/...`
- [ ] **Step 6: 提交** `git commit -m "feat(fallback): 渠道 5h... 日/总花费上限(区间)自动跳过/禁用"`

---

### Task 6: 前端 — SpendCap 字段 + 保底花费上限 + 清理死 weight

**Files:** `web/spa/src/pages/Policies.tsx`、`Fallback.tsx`、`Dispatch.tsx`、`tenant.tsx`、`web/spa/src/api.ts`(若 fallback 类型加字段)

- [ ] **Step 1:** Policies.tsx 加"自保限额"分组:SpendCap5hEnabled + SpendCap5hUsd(RangeInput)、7d 同;窗口长度数字输入。账户作用域下尤其有意义(每号不同上限)。
- [ ] **Step 2:** Fallback.tsx:加日/总花费上限输入(RangeInput)+ action 下拉(跳过/禁用);**删除 row 3 的 `weight` 输入**(Phase 1 后端已删,前端残留)。
- [ ] **Step 3:** `Dispatch.tsx:168`、`tenant.tsx:594` 删 `ch.weight` 渲染(列或字段)。
- [ ] **Step 4:** `cd web/spa && npx tsc --noEmit && npm run build` 绿。
- [ ] **Step 5: 提交** `git commit -m "feat(ui): 自保限额(5h/7d)+ 保底花费上限编辑 + 清理死 weight 输入"`

---

## 收尾验证(Phase 2)
- [ ] `go build/vet/test ./...` 全绿;`tsc --noEmit && npm run build` 绿;sqlc 无 drift
- [ ] grep 确认 `QuotaRotateThreshold`/`PickThreshold` 零残留;`weight` 仅剩 node_accounts
- [ ] 终审(opus 全分支)+ 决定部署时机

## 自查(spec 覆盖)
- 5h/7d 花费上限(区间种子+窗口 reset)✅T1-3 / 删 QuotaRotateThreshold ✅T4 / 保底花费上限 ✅T5 / 前端+清理 ✅T6。
- 账户作用域在 T3 首次被真实使用(Phase 1 接口验证)。
- **本期不含**:HumanDelay/RateGovernor/SessionSim/QuietHours(Phase 3)、SerialQueue/ModelPin/BodyPad(Phase 4)。
