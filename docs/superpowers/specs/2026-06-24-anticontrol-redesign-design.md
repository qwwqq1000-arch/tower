# 封控策略重构(Anti-Control Redesign)设计文档

**日期**: 2026-06-24
**目标**: 把 Tower 的封控/防封策略从"全局平铺旋钮"重构为"统一拟人节奏引擎 + 4 层作用域 + 区间一等公民",新增 5h/7d 花费上限、保底花费上限、利率治理、会话模拟、log-normal 人类延迟、串行队列、模型 pinning、请求体 padding;删除重复/死配置;消除写死参数;并优化配置解析热路径性能。

**架构**: 防封全部在调度层(Tower)做,节点保持干净不下发(沿用现状)。配置走 `policy.Config`/`policy.Patch`,新增账户作用域;每请求消耗的状态(花费窗口、RPM 滑窗、连发计数)走内存态,不每请求查库。

**技术栈**: Go(单二进制 + Postgres + sqlc/goose)、内嵌 React/Vite SPA。

---

## 全局约束(Global Constraints)

1. **每个行为旋钮必须显式声明作用域**:不允许"对任何错误都生效"的笼统行为。信号类规则(封禁/冷却/限额检测)各自带**独立的错误码列表**;路由/伪装类规则带**模型/时段**条件。UI 每项旁标注"作用于:429,500"。
2. **能区间就区间**:凡是数值阈值/时长/次数,优先做成 `min~max` 区间;**每个号用账号 ID 做种子在区间内定一个稳定随机值**(同号恒定,跨重启一致),不每次抖动。
3. **零写死参数**:现存硬编码(槽位时区 `Asia/Shanghai`、限额兜底 `5min`、HTTP `300s` 超时、ms 换算除外)全部挪进 `policy.Config`。
4. **消重优先**:新增旋钮前先检查是否可并入既有模块;明确正交 vs 重复(正交保留但归类标注,重复合并/删除)。
5. **热路径零回归**:任何每请求新增逻辑只读内存态;配置解析必须缓存。
6. **代码身份**:推送到 `qwwqq1000-arch`,不推 `lianshu666` org。密钥仅在服务器侧处理,日志脱敏。

---

## 一、架构地基

### 1.1 第 4 层作用域:账户

现状解析链(`internal/dispatch/service.go:resolveConfig`,约 454-486):`Base → 全局(global) → 租户(owner)`。`policies` 表 PK 是 `(scope_type, scope_id)`,**无需改表**即可加账户层。

- 新增 `ScopeType="account"`, `ScopeID=<accountId>`。
- `resolveConfig(ctx, ownerID, accountID)` 解析顺序:`Base → global → owner=ownerID → account=accountID`(后者覆盖前者)。
- 新增 API:`PUT /api/admin/policies/account/{accountId}`(写账户层 patch);`DELETE` 同路径(清除账户层,回落默认)。
- "默认配置" = 全局/租户层;"仅此号" = 账户层。

### 1.2 配置解析缓存(性能关键)

**现状坑**:`resolveConfig` 每条请求 `s.Q.ListPolicies(ctx)` 全表扫 + 每行 JSON 反序列化。加账户层后更糟。

**方案**:
- `policies` 表写入时(`UpsertPolicy`/删除)对一个进程内 `policyVersion`(atomic int64)自增。
- `resolveConfig` 内存缓存 `map[cacheKey]policy.Config`,`cacheKey = {ownerID, accountID}`;缓存条目记录生成时的 `policyVersion`。
- 命中且版本未变 → 直接返回;否则重建并写缓存。
- 全表 `ListPolicies` 仅在版本变化后第一次解析时执行一次(或维护一份解析好的 patch 索引 `map[scopeType+scopeId]Patch`,版本变化时整体重建)。

### 1.3 区间(Range)一等公民

```go
// internal/policy/range.go
type RangeF struct{ Min, Max float64 } // 也有 RangeI for int64
// 确定性解析:同 accountID+salt 永远得同一值,跨重启稳定
func (r RangeF) Resolve(accountID, salt string) float64 {
    if r.Max <= r.Min { return r.Min }
    h := fnv64(accountID + "|" + salt)         // 稳定哈希
    frac := float64(h%10000) / 10000.0
    return r.Min + frac*(r.Max-r.Min)
}
```
- `salt` 区分不同旋钮(如 `"spend5h"`, `"rpm"`),避免同号所有区间取同一分位。
- 前端 Range 字段 = 两个数字输入框 `min` / `max`(相等即固定值)。

---

## 二、拟人节奏引擎

把分散计时旋钮收成可组合的层。组合关系:
```
基础节奏: 并发 + 每请求 log-normal 延迟 + RPM/RPH/RPD 上限
  ↓ 时间调制
安静时段: 命中时段 → 压低 RPM/并发(降速)
  ↓ 会话形态调制
会话模拟: 连发 N 次(区间)→ coffee break 暂停(区间,轮换出去)→ 回来
  ↓ 叠加伪装
模型 pinning + 请求体 padding/jitter
```

### 2.1 人类延迟 HumanDelay(合并 SlotCooldown)

**消重**:删 `SlotCooldownMinMs/MaxMs`,并入本模块的 `uniform` 分布分支(数据迁移:旧值 → `HumanDelayMinMs/MaxMs`,`HumanDelayDist="uniform"`)。

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `HumanDelayEnabled` | bool | true | 关 = 无每请求延迟 |
| `HumanDelayDist` | string | `"lognormal"` | `uniform` \| `lognormal` |
| `HumanDelayP50Ms` | RangeI | 2000~2000 | log-normal 中位(区间→种子定值) |
| `HumanDelayP95Ms` | RangeI | 5000~5000 | log-normal 95 分位;由 p50/p95 反推 μ,σ |
| `HumanDelayMinMs/MaxMs` | RangeI | 2000/5000 | uniform 分支用 |

- 应用点:替换现 `orchestrator.go` 槽位释放冷却(service.go:368/1151)。
- **种子 vs 抽样(明确两层)**:① 用账号种子在 `HumanDelayP50Ms`/`P95Ms` 区间内为该号定一对**稳定的 p50/p95**(使不同号节奏不同、同号跨重启一致);② 由该号的 p50/p95 反推 `μ,σ`,**每请求实时从 log-normal(μ,σ) 抽一个延迟**(同号每次延迟不同但分布稳定)。`uniform` 分支则每请求在 `[Min,Max]` 均匀抽。

### 2.2 利率治理 RateGovernor(RPM/RPH/RPD)— 全新

| 字段 | 类型 | 默认 | 作用域 |
|---|---|---|---|
| `RateGovEnabled` | bool | false | |
| `RateRPM` | RangeI | 8~8 | 每号每分钟 |
| `RateRPH` | RangeI | 100~100 | 每号每小时 |
| `RateRPD` | RangeI | 600~600 | 每号每天 |
| `RateExceedAction` | string | `"rotate"` | `delay` \| `rotate` |

- 状态:`state.Store` 内每号三个滑动窗口计数器(分/时/天),内存态。
- 超限 → `rotate`:本号本次不可派,选下一个候选;`delay`:等到窗口让出名额。

### 2.3 会话模拟 SessionSim — 全新

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `SessionSimEnabled` | bool | false | |
| `SessionBurstCount` | RangeI | 3~10 | 一个 burst 派多少次后暂停;每个 burst 重新在区间抽 |
| `SessionPauseMs` | RangeI | 30000~180000 | coffee break 时长 |

- 状态:每号「本 burst 已派次数 / 目标次数 / 暂停截止」。
- 达到目标 → 该号进入 pause(标 limited 到 `now+pause`,**轮换给别号**,你选的)→ pause 过期重置 burst。

### 2.4 安静时段 QuietHours(替换硬开关槽位窗口)

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `QuietHoursEnabled` | bool | false | |
| `QuietHoursWindows` | []{StartMin,EndMin} | `[{1260,240}]` | 21:00-04:00;支持多段、跨夜(start>end) |
| `QuietHoursTZ` | string | `"UTC"` | **替换写死的 Asia/Shanghai** |
| `QuietHoursRPM` | RangeI | 1~2 | 命中时段把 RPM 压到此(降速,你选的) |
| `QuietHoursConcurrency` | int | 1 | 命中时段并发上限 |

- 行为:命中任一窗口时,对该号的 RPM/并发取 `min(正常值, 安静值)`。不停派,只降速。
- 现 DB `slots`(start_min/end_min)与本模块语义重叠 → **迁移**:slots 表保留做"模型×时段可用性硬门控",但时区从写死改为读 `QuietHoursTZ`(统一时区源);"降低活动量"由本模块负责。

### 2.5 串行队列 SerialQueue

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `SerialQueueEnabled` | bool | false | = 并发 1 + 满了排队不拒 |

- 不是新并发旋钮:等价 `MaxConcurrent=1` + "槽位满时 FIFO 排队等待"而非 503。
- 状态:每号一个等待队列(有界,超界才拒)。

### 2.6 模型 pinning ModelPin

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `ModelPinEnabled` | bool | false | |
| `ModelPinMode` | string | `"sticky"` | `fixed`(指定)\| `sticky`(首次见到的粘住) |
| `ModelPinTarget` | string | `""` | fixed 模式的模型名 |

- `fixed`:该号只接 `ModelPinTarget`,其它模型该号不候选。
- `sticky`:该号首次服务的模型记入内存,之后只接该模型(像真人只用一个模型);可设 TTL 复用 `AffinityTTLSec`。

### 2.7 请求体 padding/jitter BodyPad

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `BodyPadEnabled` | bool | false | |
| `BodyPadBytes` | RangeI | 0~0 | 注入的填充字节数(区间,每请求抽) |

- **正确性约束(高风险,实现需谨慎)**:padding 绝不能改变实际 prompt/输出。实现方式:在请求 JSON 的**被上游忽略的字段**(如 `metadata` 自定义键)注入无意义填充,或利用 SSE 无关字段。**实现前必须验证目标上游确实忽略该字段**;不确定则本模块标记为"需上游验证后再启用",默认关。

---

## 三、自保限额

### 3.1 5h/7d 花费上限 SpendCap — 全新

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `SpendCap5hEnabled` | bool | false | |
| `SpendCap5hUsd` | RangeF | 100~200 | 每号 5h 滚动窗口花费上限(种子定值) |
| `SpendCap7dEnabled` | bool | false | |
| `SpendCap7dUsd` | RangeF | 500~1000 | 每号 7d 滚动窗口花费上限 |

- 状态:每号两个花费累加器(5h / 7d 窗口),按请求估算成本累加(复用 `billing.CostUsd`)。
- **窗口与 reset 语义(明确)**:采用"从触达点起的固定窗口"——首次累加时记窗口起点;累计 ≥ 种子上限 → `SetLimited(key, {"all": now+窗口长度})` 并在 reset 时刻把该窗口累加器清零重新计。5h 窗口长 = 5h,7d 窗口长 = 7d(都可后续配置化)。复用反应式限额同一 `LimitedUntil` 机制与"限额"显示。
- 与反应式限额互补:这是"自己花到上限主动退出"(在被 Anthropic 限之前),反应式是"被限了才退出"。
- 跨重启:窗口累加器内存态,启动时从近窗口的 `request_log`/计费数据重建(或接受重启清零,首版可先内存态 + 标注 TODO 持久化)。

### 3.2 保底渠道花费上限 FallbackSpendCap — 全新

- `fallback_channels` 新增列:`spend_cap_daily_min/max_usd`、`spend_cap_total_min/max_usd`(区间,种子用 channel id)。
- 行为:`enabledChannels()` 里跳过 `fallback_spend(今日/总) ≥ 种子上限` 的渠道。
- 触达动作可配:`仅跳过本请求` vs `自动置 enabled=false`(渠道级开关 `SpendCapAction`)。
- 复用既有 `fallback_spend` 表(现在只记账)+ `GetFallbackSpendToday/Total`。

---

## 四、配置清理(删除重复作用的配置)

| 动作 | 对象 | 原因 |
|---|---|---|
| **删** | `fallback_channels.weight` | dispatch 从不读,死配置(迁移 drop 列 + 去 struct/queries) |
| **删** | `policy.Config.QuotaRotateThreshold` + 其轮询轮换用法 | 已停轮询,失效(用户确认) |
| **合并** | `SlotCooldownMin/MaxMs` → HumanDelay | 与人类延迟重复,改成 `uniform` 分支 |
| **挪出硬编码** | 槽位时区 → `QuietHoursTZ`;限额兜底 `5*60*1000` → `QuotaLimitDefaultResetMs`;HTTP `300s`(proxy.go:74)→ `UpstreamTimeoutSec` | 零写死 |
| **保留但归类标注** | 3 个 cooldown(`SlotCooldown→HumanDelay` / `Cooldown{Base,Max,Mult}` 封禁退避 / `CooldownSignalSec` 429临时) | 正交不重复;UI 重新分组 + 每个标注作用错误码 |
| **补作用域** | 各信号旋钮 | 确认 `BanSignals`/`CooldownSignals`/`QuotaLimitStatusCodes` 各自独立错误码;UI 显式展示 |

---

## 五、性能优化

1. **配置解析缓存**(§1.2)——热路径最大赢:消除每请求 `ListPolicies` 全表扫。
2. **新模块状态全内存**:花费窗口、RPM 滑窗、连发计数、串行队列、模型 sticky 均在 `state.Store`,每请求 O(1),不查库。
3. **号库加载优化**(已上线)思路延续:页面/接口避免每次拉 CPA 实时 usage,优先持久化值 + 按需刷新。
4. 性能基线:重构后用无头浏览器 + 压测确认 dispatch 热路径 P50/P95 不回归。

---

## 六、前端(Policies.tsx)

- **账户选择器**(默认当前账户)+ 作用域切换:`默认(全局/租户)` / `仅此号`。沿用 nexaxis 账户选择器铁律。
- 按模块重新分组:节奏引擎(并发/人类延迟/利率/会话模拟/安静时段/串行队列)、自保限额(5h/7d/保底)、伪装(模型 pin/padding)、信号(封禁/冷却/限额检测,各带错误码)、保底渠道、模型输出上限、预热、弹性。
- Range 字段渲染为 `min ~ max` 双输入;每个信号字段旁显示"作用于:<错误码>"。
- 保留 dry-run 预览;账户层走新 `PUT /api/admin/policies/account/{id}`。

---

## 七、数据模型变更

- `policies`:无表结构变更(账户层用现有 PK)。
- `fallback_channels`:`DROP weight`;`ADD spend_cap_daily_min/max_usd, spend_cap_total_min/max_usd, spend_cap_action`。
- 新增内存结构(`state.Store`):每号 `{spend5h, spend7d, rpm/rph/rpd 滑窗, burst 状态, serial 队列, model sticky}`。首版花费窗口可不持久化(标注 TODO)。
- `policy.Config`/`Patch`:新增上述所有字段(PascalCase,Patch 指针),删 `QuotaRotateThreshold`/`SlotCooldown*`。

---

## 八、实现分阶段(spec 全量,落地分批)

1. **地基 + 清理 + 性能**:账户 scope + API + 前端选择器;配置解析缓存;删 `weight`/`QuotaRotateThreshold`;挪硬编码;`SlotCooldown→HumanDelay(uniform)` 迁移。
2. **自保限额**:5h/7d 花费上限 + 保底花费上限自动禁用/跳过。
3. **拟人节奏**:HumanDelay log-normal、RateGovernor、SessionSim、QuietHours 降速。
4. **伪装**:SerialQueue、ModelPin、BodyPad(BodyPad 需先验证上游忽略字段)。

---

## 九、测试策略

- 单测:`RangeF/I.Resolve` 确定性;QuietHours 窗口数学(跨夜 + 时区);SpendCap 窗口滚动与重置;RateGovernor 滑窗;SessionSim burst/pause 状态机;log-normal 抽样 p50/p95 合理性;FallbackSpendCap 跳过逻辑;4 层 resolveConfig + 缓存失效。
- 集成:dispatch 在各模块开/关下的路由正确;缓存版本失效不串配置。
- 性能:dispatch 热路径压测 P50/P95 无回归;`ListPolicies` 调用次数下降。
- 验证:无头浏览器验收前端账户选择器 + Range 编辑 + dry-run。

---

## 十、非目标 / 待定

- **BodyPad 正确性**:必须确认上游忽略注入字段,否则不启用。首版可仅落配置+开关,启用门控在上游验证后。
- **花费窗口持久化**:首版内存态,重启清零;后续视需要加 `account_spend_window` 表或从计费重建。
- 不改节点侧(防封全在调度层);不引入跨节点分布式状态(单 Tower 实例内存态)。
