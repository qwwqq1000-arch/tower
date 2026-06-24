# 封控策略重构 Phase 3(拟人节奏引擎)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development。步骤用 `- [ ]` 勾选。
> **RESUME NOTE(开新会话从这里接):** Phase 1+2 已完成且 green,在分支 `feat/anticontrol-phase1`(HEAD 见 `git log`,Phase2 末为 b201b77)。先 `cat .superpowers/sdd/anticontrol-phase2-progress.md` 确认,再建 `.superpowers/sdd/anticontrol-phase3-progress.md` ledger,然后按本计划逐任务 subagent-driven 执行。机械任务 haiku、集成 sonnet、删除/终审 opus。

**Goal:** 给 dispatch 加拟人节奏:log-normal 人类延迟(合并 SlotCooldown)、利率治理 RPM/RPH/RPD、会话模拟(连发→暂停轮换)、安静时段降速。**全部默认 OFF / 行为中性**(HumanDelay 默认 uniform = 现 SlotCooldown 行为;RateGov/SessionSim/QuietHours 默认 disabled),只有显式开启才改时序。

**Architecture:** 节奏状态全走 `state.Account`/`state.Store` 内存态(每请求 O(1))。门控点:`CanDispatch`(account.go:112,加 rate/session/quiet gate)+ `buildCandidates`(service.go:589)+ `Store.Complete`(store.go:128,HumanDelay 采样)。复用 Phase1 `RangeI/RangeF.Resolve(key,salt)` 种子 + Phase1 `QuietHoursTZ`。

**Tech Stack:** Go + React/TS。绿门:`go build/vet/test ./...` + `cd web/spa && npx tsc --noEmit && npm run build` + sqlc 无 drift。

## Global Constraints
- **默认行为中性**:所有 Phase 3 开关默认关;HumanDelay 默认 `uniform` 用现有 SlotCooldownMin/Max(2000-5000),日志/采样不变。
- 区间种子:RPM/RPH/RPD/burst/pause 用 `RangeI.Resolve(key, salt)` 每号定稳定值。
- 节奏状态全内存(Account/Store),每请求 O(1),不查库;首版重启清零(标 TODO)。
- 每旋钮显式作用域;能区间就区间;零写死(分布参数、窗口长进 Config)。
- 代码推 `qwwqq1000-arch`;密钥仅服务器侧;日志脱敏。

## 文件结构
- `internal/policy/policy.go` — 加 HumanDelay/RateGov/SessionSim/QuietHours 字段(Config/Patch/apply/Defaults)。
- `internal/policy/lognormal.go`(新)— log-normal 采样(p50/p95 → μ,σ → sample)。
- `internal/state/account.go` + `store.go` — 每号 RPM/RPH/RPD 滑窗计数 + burst 状态 + 对应 Store 包装。
- `internal/dispatch/service.go` — buildCandidates/CanDispatch 接 rate+quiet+session gate;Complete 用 HumanDelay 采样。
- `internal/dispatch/orchestrator.go` — Complete 传分布参数。
- `web/spa/src/pages/Policies.tsx` + `types.ts` — 4 个新分组(RangeInput)。

---

### Task 1: HumanDelay(log-normal)+ 合并 SlotCooldown

**policy.Config 加**(默认=现行为):`HumanDelayDist string`(默认 `"uniform"`;`uniform`|`lognormal`)、`HumanDelayP50Ms RangeI`(默认 `{2000,2000}`)、`HumanDelayP95Ms RangeI`(默认 `{5000,5000}`)。**保留** `SlotCooldownMinMs/MaxMs`(uniform 分支继续用,不删——只是 HumanDelay 多一个 lognormal 选项)。

**Files:** `internal/policy/lognormal.go`(新)、`internal/policy/policy.go`、`internal/state/store.go`、`internal/dispatch/orchestrator.go`、`internal/dispatch/service.go`

- [ ] **Step 1: 失败测试**(log-normal 采样:给定 p50/p95,大量采样的中位数≈p50)
```go
// internal/policy/lognormal_test.go
func TestLogNormalMedianApproxP50(t *testing.T) {
	const p50, p95 = 2000.0, 5000.0
	var samples []float64
	for i := 0; i < 4000; i++ {
		samples = append(samples, SampleLogNormal(p50, p95, fmt.Sprintf("k%d", i)))
	}
	sort.Float64s(samples)
	med := samples[len(samples)/2]
	if med < p50*0.7 || med > p50*1.4 { t.Fatalf("中位数 %v 偏离 p50 %v", med, p50) }
	// 应有显著尾部(p95 附近有样本)
	if samples[len(samples)*94/100] < p50 { t.Fatalf("无右尾") }
}
```
- [ ] **Step 2:** `go test ./internal/policy/ -run TestLogNormal -v` → FAIL
- [ ] **Step 3: 实现** `internal/policy/lognormal.go`:
```go
package policy
import ("math"; "hash/fnv")
// SampleLogNormal 由 p50/p95 反推 μ,σ(log-normal),用 seed 做确定性单位均匀样本映射。
// μ=ln(p50);σ=(ln(p95)-μ)/z95, z95≈1.6448536。返回 exp(μ+σ·Φ⁻¹(u))。
func SampleLogNormal(p50, p95 float64, seed string) float64 {
	if p50 <= 0 { return 0 }
	if p95 <= p50 { return p50 }
	mu := math.Log(p50)
	sigma := (math.Log(p95) - mu) / 1.6448536269514722
	u := seedUnit(seed)               // (0,1)
	z := normInv(u)                   // Φ⁻¹
	return math.Exp(mu + sigma*z)
}
func seedUnit(seed string) float64 {
	h := fnv.New64a(); _, _ = h.Write([]byte(seed))
	return (float64(h.Sum64()>>11) + 0.5) / float64(uint64(1)<<53)
}
// normInv: Acklam 近似 Φ⁻¹(p),p∈(0,1)。
func normInv(p float64) float64 { /* 实现 Acklam 有理逼近;实现者补全标准公式 */ return acklam(p) }
```
(实现者:`acklam` 用标准 Peter Acklam 系数;或用 `math.Erfinv`:`z = math.Sqrt2 * math.Erfinv(2*p-1)`——Go 1.10+ 有 `math.Erfinv`,**优先用它**,免得手写系数:`func normInv(p float64) float64 { return math.Sqrt2 * math.Erfinv(2*p-1) }`。)
- [ ] **Step 4:** policy 加字段;`Store.Complete` 改签名带分布(或加 `Store.CompleteDelay(key string, dist string, p50, p95, minMs, maxMs int64)`):`lognormal` 时 `cd = int64(SampleLogNormal(p50,p95,key))`,否则 `cd = rnd(min,max)`;orchestrator.go:56 传 cfg 的对应值。默认 `uniform` → 行为不变。
- [ ] **Step 5:** `go test ./internal/policy/ ./internal/dispatch/ && go build ./...` 绿;提交 `feat(cadence): HumanDelay log-normal 延迟(默认 uniform 行为不变)`。

---

### Task 2: RateGovernor(RPM/RPH/RPD)

**policy.Config 加**(默认 disabled):`RateGovEnabled bool`、`RateRPM RangeI`(默认 `{8,8}`)、`RateRPH RangeI`(`{100,100}`)、`RateRPD RangeI`(`{600,600}`)、`RateExceedAction string`(默认 `"rotate"`;`rotate`|`delay`)。

**Files:** `internal/state/account.go`(滑窗计数)、`internal/state/store.go`、`internal/dispatch/service.go`(buildCandidates 过滤)

- [ ] **Step 1: 失败测试**(滑窗:1 分钟内第 9 次超过 RPM=8)
```go
func TestRateWindowRPM(t *testing.T) {
	a := NewAccount(3)
	for i := 0; i < 8; i++ { a.RecordReq(int64(i)) } // t=0..7 ms,都在 1min 窗
	if a.ReqsInWindow(100, 60000) != 8 { t.Fatal("窗内应=8") }
	if a.ReqsInWindow(70000, 60000) != 0 { t.Fatal("1min 后应滚出=0") }
}
```
- [ ] **Step 2/3:** `Account.RecordReq(now)`(append 时间戳 + 用最长窗 1d prune)+ `ReqsInWindow(now, winMs)`(计数);Store 包装 `RecordReq`/`ReqsInWindow`。在 buildCandidates(service.go:589 候选筛选)对每号:`rpm := cfg.RateRPM.Resolve(key,"rpm")`;若 `ReqsInWindow(key,now,60000) >= rpm` 或 RPH(3600000)或 RPD(86400000)超 → action=rotate 则该号不入候选(`continue`),delay 则…(首版 rotate 即可,delay 标 TODO)。`RecordReq` 在派单成功后(logOK/logStream,挨着 recordSpend)调。仅 `RateGovEnabled` 时生效。
- [ ] **Step 4:** 测试+构建绿;提交 `feat(cadence): 利率治理 RPM/RPH/RPD(默认关)`。

---

### Task 3: SessionSim(连发→暂停轮换)

**policy.Config 加**(默认 disabled):`SessionSimEnabled bool`、`SessionBurstCount RangeI`(默认 `{3,10}`)、`SessionPauseMs RangeI`(默认 `{30000,180000}`)。

**Files:** `internal/state/account.go`(burst 状态:已发数/本轮目标/暂停截止)、`store.go`、`internal/dispatch/service.go`

- [ ] **Step 1: 失败测试**(状态机:发够 burst → 暂停)
```go
func TestSessionBurstThenPause(t *testing.T) {
	a := NewAccount(3)
	target := 3
	for i := 0; i < target; i++ { a.BurstTick(target, 0) }
	if !a.BurstShouldPause(target) { t.Fatal("发够应暂停") }
}
```
- [ ] **Step 2/3:** Account 加 burst 计数 + `BurstTick(target, now)` / `BurstShouldPause(target)`;派单成功后(挨着 recordSpend),若 `SessionSimEnabled`:`target := int(cfg.SessionBurstCount.Resolve(key,"burst"))`;`BurstTick`;若发够 → `pause := cfg.SessionPauseMs.Resolve(key,"pause")`;`SetLimited(key, cfg.MaxConcurrent, {"all": now+pause})`(暂停=该号 limited 一段,轮换给别号,复用现机制)+ 重置 burst 计数。仅 enabled 时生效。
- [ ] **Step 4:** 测试+构建绿;提交 `feat(cadence): 会话模拟 连发→coffee break 暂停轮换(默认关)`。

---

### Task 4: QuietHours(安静时段降速)

**policy.Config 加**(默认 disabled):`QuietHoursEnabled bool`、`QuietHoursWindows []TimeWindow`(`{StartMin,EndMin}`,默认 `[{1260,240}]`=21:00-04:00)、`QuietHoursRPM RangeI`(默认 `{1,2}`)、`QuietHoursConcurrency int`(默认 1)。复用 Phase1 `QuietHoursTZ`。

**Files:** `internal/policy/policy.go`、`internal/dispatch/service.go`(buildCandidates rate/concurrency 取 min)

- [ ] **Step 1: 失败测试**(跨夜窗判定,复用 slotActiveNow 风格)
```go
func TestQuietWindowOvernight(t *testing.T) {
	// 21:00-04:00 跨夜:23:00 命中,12:00 不命中
	if !inAnyWindow(23*60, []TimeWindow{{1260,240}}) { t.Fatal("23点应命中") }
	if inAnyWindow(12*60, []TimeWindow{{1260,240}}) { t.Fatal("12点不应命中") }
}
```
- [ ] **Step 2/3:** `inAnyWindow(minOfDay, windows)`(跨夜 start>end 用 OR);在 buildCandidates,若 `QuietHoursEnabled` 且当前(按 QuietHoursTZ 的 minute-of-day)命中任一窗口 → 该号有效 RPM 取 `min(正常RPM, QuietHoursRPM.Resolve)`、有效并发取 `min(MaxConcurrent, QuietHoursConcurrency)`(与 Task 2 的 rate 检查叠加;并发可通过 SetCapacity 或 WarmupCap 类机制)。仅 enabled 时生效。
- [ ] **Step 4:** 测试+构建绿;提交 `feat(cadence): 安静时段降速(默认关,时区用 QuietHoursTZ)`。

---

### Task 5: 前端 — 4 个节奏分组

**Files:** `web/spa/src/pages/Policies.tsx`、`types.ts`

- [ ] PolicyPatch 加全部 Phase3 字段(HumanDelay/RateGov/SessionSim/QuietHours;区间用 `{Min,Max}`,TimeWindow 用 `{StartMin,EndMin}[]`)。
- [ ] Policies.tsx 加 4 个分组,RangeInput 复用;分布用下拉(uniform/lognormal)、action 下拉(rotate/delay)、QuietHours 时段可加多段。账户作用域下尤其有用。
- [ ] `cd web/spa && npx tsc --noEmit && npm run build` 绿;提交 `feat(ui): 拟人节奏 4 分组配置(HumanDelay/利率/会话/安静时段)`。

---

## 收尾验证(Phase 3)
- [ ] `go build/vet/test ./...` 绿;`tsc --noEmit && npm run build` 绿;sqlc 无 drift。
- [ ] **默认行为中性自检**:全开关默认关 + HumanDelay 默认 uniform → dispatch 时序与 Phase 2 末完全一致(可用一个"全默认"集成断言)。
- [ ] 终审(opus 全分支,base=Phase2 末 b201b77)。
- [ ] Phase 4 计划(SerialQueue/ModelPin/BodyPad)再写。**全 4 阶段绿后才部署**(用户要求)。

## 自查(spec 覆盖)
- HumanDelay log-normal ✅T1 / RPM-RPH-RPD ✅T2 / SessionSim ✅T3 / QuietHours 降速 ✅T4 / 前端 ✅T5。
- 默认行为中性贯穿(production 安全)。本期不含 Phase 4(SerialQueue/ModelPin/BodyPad)。
