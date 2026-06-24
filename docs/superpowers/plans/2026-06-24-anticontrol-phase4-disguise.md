# 封控策略重构 Phase 4(伪装)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development。步骤用 `- [ ]` 勾选。
> **RESUME NOTE:** 在 Phase 3 **之后**做(Phase 3 完成且 green 后)。分支 `feat/anticontrol-phase1`。先 `cat .superpowers/sdd/anticontrol-phase3-progress.md` 确认 Phase 3 落地,建 `anticontrol-phase4-progress.md` ledger,再按本计划 subagent-driven。

**Goal:** 三个反指纹伪装:SerialQueue(并发1+排队,不并发)、ModelPin(号锁定一个模型)、BodyPad(请求体 padding/jitter)。**全部默认 OFF / 行为中性**。BodyPad 有正确性风险(不能改 prompt/输出),先验证安全注入向量再实现,否则只落配置+守卫式 no-op。

**Architecture:** 状态走 `state.Account`/内存态;门控在 `buildCandidates`(service.go:589,ModelPin 过滤)、`SetCapacity`(SerialQueue=cap1)、proxy 发包前(BodyPad 改 body)。复用 Phase1 RangeI、pinToAffinity 风格的内存 map。

## Global Constraints
- 默认全关 = 行为中性(production 安全)。
- **BodyPad 铁律**:绝不改变实际 prompt/输出;只往**上游确证会忽略**的字段注入;不确定就不启用(配置存在但运行期守卫 no-op)。任何注入失败/异常都不得让请求失败。
- 区间种子(padding 字节、模型 sticky TTL)用 RangeI/Resolve;零写死;每旋钮显式作用域。
- 代码推 `qwwqq1000-arch`;密钥仅服务器侧;日志脱敏。

## 文件结构
- `internal/policy/policy.go` — SerialQueue/ModelPin/BodyPad 字段。
- `internal/state/account.go` + `store.go` — ModelPin sticky 记录(号→模型 + TTL);SerialQueue 排队等位。
- `internal/dispatch/service.go` — buildCandidates ModelPin 过滤;SerialQueue cap=1 + 等位;BodyPad 在发包前改 body。
- `web/spa/src/pages/Policies.tsx` + `types.ts` — 3 个新分组。

---

### Task 1: ModelPin(号锁模型)

**policy.Config 加**(默认 disabled):`ModelPinEnabled bool`、`ModelPinMode string`(默认 `"sticky"`;`fixed`|`sticky`)、`ModelPinTarget string`(fixed 用)。

**Files:** `internal/state/account.go`(sticky:号首次服务的模型 + 截止 ms)、`store.go`、`internal/dispatch/service.go`(buildCandidates 过滤)

- [ ] **Step 1: 失败测试**
```go
func TestModelPinSticky(t *testing.T) {
	a := NewAccount(3)
	// 首次记 opus;之后 pinnedModel 返回 opus(TTL 内)
	a.RecordModel("claude-opus-4-8", 0, 300000)
	if m, ok := a.PinnedModel(1000, 300000); !ok || m != "claude-opus-4-8" { t.Fatal("应粘 opus") }
	if _, ok := a.PinnedModel(400000, 300000); ok { t.Fatal("TTL 过期应解除") }
}
```
- [ ] **Step 2/3:** Account 加 `pinModel string` + `pinUntil int64`;`RecordModel(model, now, ttl)`(仅当未粘或已过期时记)、`PinnedModel(now, ttl) (string,bool)`;Store 包装。在 buildCandidates(service.go:672 候选 append 前):若 `ModelPinEnabled`:
  - `fixed`:`if cfg.ModelPinTarget != "" && !modelMatch(requestModel, cfg.ModelPinTarget) { continue }`(该号只接 target;非 target 模型该号不入候选)。
  - `sticky`:`if pm, ok := store.PinnedModel(key, now, ttl); ok && !modelMatch(requestModel, pm) { continue }`(已粘别的模型 → 不入候选)。派单成功后 `store.RecordModel(key, requestModel, now, ttl)`(首次粘)。ttl 复用 `AffinityTTLSec`。
- [ ] **Step 4:** 测试+构建绿;提交 `feat(disguise): 模型 pinning(fixed/sticky,默认关)`。

---

### Task 2: SerialQueue(并发1+排队)

**policy.Config 加**(默认 disabled):`SerialQueueEnabled bool`、`SerialQueueWaitMs int`(默认 2000;等位上限,超时则按常规失败/失败转移)。

**Files:** `internal/state/store.go`(等位)、`internal/dispatch/service.go`

**语义**:`SerialQueueEnabled` → 该号有效并发=1(`SetCapacity(key,1)`,在 buildCandidates 设容量处用 `min(cfg.MaxConcurrent, 1)`),且当该号被选中但槽位忙时,**有界等位** `SerialQueueWaitMs` 而非立刻失败转移(让同号请求串行排队)。等位用 `Slots.Available` 轮询(带小睡)或一个 per-account 信号量(有界 timeout)。等不到再走常规失败转移。

- [ ] **Step 1: 失败测试**(纯逻辑:有效容量取 min)
```go
func TestSerialEffectiveCap(t *testing.T) {
	if effectiveCap(true, 5) != 1 { t.Fatal("串行应=1") }
	if effectiveCap(false, 5) != 5 { t.Fatal("非串行=原值") }
}
```
- [ ] **Step 2/3:** `func effectiveCap(serial bool, maxc int) int { if serial { return 1 }; return maxc }`;buildCandidates 设容量处用它。等位:在 orchestrator 取槽失败时,若该号 serial,用 `store.WaitForSlot(key, deadline)`(轮询 `Slots.Available(now)>0`,每 ~20ms,直到 deadline)再重试一次;否则常规失败转移。**注意**:等位不得阻塞整体(有界 timeout + 仅 serial 号);实现者评估在 orchestrator 何处插入最干净,必要时把 serial 等位做成 dispatch 层一次有界重试。
- [ ] **Step 4:** 测试+构建绿(含并发 -race);提交 `feat(disguise): 串行队列 并发1+有界等位(默认关)`。

---

### Task 3: BodyPad(请求体 padding/jitter)——先验证再实现

**policy.Config 加**(默认 disabled):`BodyPadEnabled bool`、`BodyPadBytes RangeI`(默认 `{0,0}`)。

- [ ] **Step 1: 验证安全注入向量(关键,先做)**:在 23.237 用一个真实 opus 请求,测试上游(meridian / CPA → Anthropic)对以下注入的反应,确认**不改变响应且不报 400**:
  - 候选 A:`metadata` 对象内加一个无关键(如 `metadata.pad`);
  - 候选 B:顶层加无关键(很可能被 Anthropic 400 拒——预期失败,用来排除);
  - 候选 C:某个被忽略的可选字段。
  把结论(哪个安全 / 都不安全)写进 task report。**若都不安全 → BodyPad 只落配置 + 运行期守卫 no-op(永不注入),Step 3 跳过注入,加注释说明。**
- [ ] **Step 2: 失败测试**(padding 大小确定性 + 不破坏 JSON)
```go
func TestBodyPadKeepsValidJSONAndPrompt(t *testing.T) {
	orig := []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`)
	out := padBody(orig, 100, "seedk") // 注入约 100 字节到安全字段
	var m map[string]any
	if json.Unmarshal(out, &m) != nil { t.Fatal("padding 破坏了 JSON") }
	if !reflect.DeepEqual(m["messages"], /* 原 messages */) { t.Fatal("padding 改了 prompt!") }
}
```
- [ ] **Step 3: 实现** `padBody(body []byte, nBytes int, seed string) []byte`:unmarshal → 往 Step 1 确认的安全字段塞 `nBytes` 字节随机/重复字符 → marshal;**任何错误返回原 body 不变**。在 proxy 发包前(node + 可选 fallback 路径),若 `BodyPadEnabled`:`n := int(cfg.BodyPadBytes.Resolve(key,"bodypad"))`;`body = padBody(body, n, key)`。`BodyPadBytes` 默认 0 → padBody no-op。
- [ ] **Step 4:** 测试+构建绿;提交 `feat(disguise): 请求体 padding/jitter(默认关,注入向量已验证安全)`。

---

### Task 4: 前端 — 3 个伪装分组

**Files:** `web/spa/src/pages/Policies.tsx`、`types.ts`

- [ ] PolicyPatch 加 ModelPin/SerialQueue/BodyPad 字段。
- [ ] Policies.tsx 加 3 分组:ModelPin(开关 + mode 下拉 fixed/sticky + target 输入)、SerialQueue(开关 + 等位 ms)、BodyPad(开关 + 字节 RangeInput;旁注"需上游验证")。
- [ ] `tsc --noEmit && npm run build` 绿;提交 `feat(ui): 伪装 3 分组(ModelPin/SerialQueue/BodyPad)`。

---

## 收尾验证(Phase 4 = 全重构最后一阶段)
- [ ] `go build/vet/test ./...` 绿;`tsc --noEmit && npm run build` 绿;sqlc 无 drift。
- [ ] 默认行为中性自检(全开关默认关 → 时序/请求体与 Phase 3 末一致)。
- [ ] 终审(opus 全分支,base=Phase3 末)。
- [ ] **🚀 四阶段全绿 → 部署**:rsync 工作树(排除 web/spa/dist + .superpowers)→ 23.237.28.170:8088 `docker compose up -d --build`(单 ControlMaster 连接,见 [[deploy-creds-23237]])→ 无头 Chrome 验收 `/policies`(4+3 新分组渲染)+ `/accounts` + `/dispatch` 无 pageerror → 抽查一个默认配置请求时序无回归。

## 自查(spec 覆盖)
- SerialQueue ✅T2 / ModelPin ✅T1 / BodyPad(验证→实现/守卫)✅T3 / 前端 ✅T4。
- BodyPad 正确性铁律(不改 prompt、上游验证、永不致请求失败)贯穿。这是 spec 全部能力的最后一块。
