# 设计:封控修复 + 权限隔离 + 封控事件日志 + CPA 节点接入

日期:2026-06-22
状态:已批准(用户授权自主执行至部署+发布)

## 背景与目标

Tower 是 Claude 号池调度总控台(Go + Postgres)。本次一并修复/新增:

1. **封禁/半开 bug + 永久封禁**:被封号冷却过后会误显示「半开」;缺少「永久封禁」状态。
2. **封禁信号配置**:默认封禁状态码定为 `[401]`。
3. **权限隔离**:`superadmin` 看全部;其余角色(admin/operator/tenant/viewer)只看自己 `owner_id` 名下的节点/号/日志/事件/计费。
4. **封控事件日志**:每次触发封控(封禁/重试/失败转移/保底)写入带时间戳的事件,前端可见;对应号显示对应状态。
5. **封控策略生效审计**:逐项检查 `policy.Config` 每个设置是否真的被执行(已发现 `AffinityTTLSec` 0 处使用)。
6. **CPA 节点接入**:节点新增 `kind`(meridian|cpa);CPA 读取其账户与额度显示到号库;支持「调度指定账户」(需改造 CLIProxyAPI)。
7. **收尾**:channel-rate / fallback-limit 前端 UI。

## 决策(已确认)

- 永久封禁:连续 N 次封禁信号 → 永久禁用,不再半开(N 可配,`PermanentBanStreak`)。
- 封禁信号默认 `[401]`(可配)。
- 权限:admin 仅自己名下,superadmin 全局。
- CPA = https://github.com/router-for-me/CLIProxyAPI;fork 发布到用户 GitHub(私有,仓库名 `CLIProxyAPI`)。
- CPA 缺「指定账户调度」→ 改 fork 加该能力;实测服务器 23.134.76.25。

## 一、封禁状态机(internal/state, internal/policy)

### 永久封禁
- `Breaker` 增加 `permanent bool` 字段(持久化)。
- `policy.Config` 增加 `PermanentBanStreak int`(默认 5;用户可设 2)。`Patch`/`apply`/`DryRun` 同步。
- `Breaker.OnBanSignal`:streak 达到 `PermanentBanStreak` 时设 `permanent=true`(永不恢复);否则维持原逻辑(达到 `BanPersistStreak` 开启可恢复熔断)。
- `Breaker.State(now)`:`permanent` → 返回新状态 `"permanent"`;否则原逻辑。
- `OnSuccess`/`Restore`/`Snapshot`:处理 permanent 持久化与清除。
- 永久封禁号不参与调度(`CanDispatch` 返回 false)。

### 状态标签(account.go Status)
- `disabled`(人工禁用)> `offline` > `permanent`(永久封禁)> `open`→`banned`(封禁·冷却) > `half_open`(半开·探测) > `active`。
- 修复点:`half_open` 仅代表「冷却已过、可探测」,真封号现在是 `permanent`,不会再误显半开。

### 人工恢复
- 新增管理接口 `POST /api/admin/accounts/{id}/recover`(owner 校验):清熔断(`OnSuccess` + 清 permanent)、`enabled=true`、`status='active'`,并落库 + 写事件 `account_recovered`。
- 前端号库卡片加「恢复」按钮(仅永久封禁/封禁态显示)。

### 持久化
- `account_state` 迁移加 `permanent BOOLEAN`(migration 20260622000026)。`state/persist.go` 读写。

## 二、封禁信号默认值
- `policy.Defaults().BanSignals = []int{401}`(去掉 403)。保留可配。

## 三、权限隔离(internal/api)

新增辅助:
```go
// scope returns (ownerID, all). all=true → no filter (superadmin).
func scope(r *http.Request) (string, bool)
```
- `superadmin` → all=true;其余 → 按 `Sub` 过滤。
- 改造所有 admin 列表/详情接口按 owner 过滤:节点、号(listAccountsHandler)、日志、事件(listEventsHandler)、计费(hosting)、调度密钥。
- 写/删操作加 owner 校验(非 superadmin 不能操作他人资源)。
- 测试:admin A 看不到 admin B 的资源;superadmin 看全部。

## 四、封控事件日志(internal/dispatch, internal/events)

在 orchestrator 回调与 service 转发路径补 `events.Record`(均带 `s.Now()` 时间戳、`OwnerID`、`Target`):
- `ban_detected`:命中封禁信号 → detail{key, status, streak}
- `ban_permanent`:触发永久封禁 → detail{key, streak}
- `retry`:失败转移到下一个号 → detail{fromKey, status, error, attempt}
- `fallback`:触发保底 → detail{reason, channelId, channelName}
现有 `session_exile`/`balance_low` 保留。前端事件页(owner 过滤)展示完整链路;号库状态与事件一致。

## 五、封控策略生效审计

逐项核对 `policy.Config` 字段在 dispatch/state/telemetry 是否被执行:
- 已知:`AffinityTTLSec`(0 处使用)→ 接入会话亲和 TTL 或移除并记录。
- 核对 Warmup*/Elastic*/Session* 等是否真正 enforce;补齐缺口或在 spec 标注。
- 每个 enforce 点尽量有测试。

## 六、CPA 节点接入

### Tower 侧
- 迁移加 `nodes.kind TEXT NOT NULL DEFAULT 'meridian'`(meridian|cpa)。queries/sqlc 同步。
- 建节点接口/表单选择类型;节点卡片显示「类型:MERIDIAN/CPA」。
- 新增 `internal/cpaclient`:
  - `ListAccounts(ctx)`:`GET {base}/v0/management/auth-files`,`Authorization: Bearer <mgmtKey>`。映射 id/email/provider/status/disabled/success/failed。
  - `Quota(ctx, authIndex)`:读 5h/7天/7天Sonnet 用量(走 fork 新增的额度端点,见下)。
- 轮询:CPA 节点定期拉取账户写入 `accounts`/`node_accounts`(profile_id = auth_index/文件名),号库每个 CPA 号一行,显示状态 + 额度。
- 调度:Tower 转发到 CPA 节点推理端点;通过请求头 `X-CLIProxy-Account: <auth_index/email>` 指定账户(fork 能力)。封控/状态按账户粒度。
- 额度无法获取时优雅降级(显示 success/failed)。

### CLIProxyAPI fork 侧(私有仓库 CLIProxyAPI)
- **指定账户调度**:`sdk/cliproxy/auth/selector.go` 三个 `Pick`(RoundRobin/FillFirst/SessionAffinity)读取 `opts.Headers.Get("X-CLIProxy-Account")`;非空时只在 `available` 里匹配该账户(按 auth_index/id/email/filename),命中返回,否则返回明确错误。`opts.Headers` 已存在,改动最小。
- **额度端点**(若现版本没有):`GET /v0/management/quota?auth_index=` 返回该账户 Claude 用量(5h/7天/7天Sonnet),数据来自 Anthropic OAuth usage(用账户 token)。先连测试服确认是否已有等价端点,有则复用。
- 加最小单测;`go build ./...` + `go test ./...` 通过。

## 七、收尾 UI(web/spa)
- Users.tsx:接入 `setUserChannelRate`/`setUserFallbackLimit`,加两列内联编辑。
- Me 仪表盘:展示 `channelConsumptionUsd / channelRate / channelHostingFeeUsd`。

## 部署与发布(外部操作)
1. fork 改完本地 build/test 通过 → scp/部署到 23.134.76.25(`server` 文件凭据),重启 CPA,实测「指定账户调度」+「额度」。
2. 通过后 `gh repo create qwwqq1000-arch/CLIProxyAPI --private`,推送 fork。
3. **`server` 文件含私钥/密码,绝不提交任何仓库(已在 .gitignore 确认)。**

## 实施顺序
F(UI 收尾) → 一/二(封禁状态机) → 四(事件) → 三(权限) → 五(审计) → 六-Tower(CPA 客户端) → 六-fork(CLIProxyAPI 改造) → 部署 → 发布 → 全量 build/test + 审计 debug。

## 封控策略生效审计结果(2026-06-22)

逐项核对 `policy.Config` 字段在 dispatch/state/telemetry 的执行情况:

| 设置 | 生效? | 执行点 |
|---|---|---|
| MaxConcurrent | ✓ | buildCandidates SetCapacity + Slots |
| SlotCooldownMin/MaxMs | ✓ | Store.Complete 冷却 |
| BanPersistStreak | ✓ | Breaker.OnBanSignal |
| PermanentBanStreak | ✓ (本次新增) | Breaker 永久封禁 |
| CooldownBase/Max/Mult | ✓ | backoffMs |
| **AffinityTTLSec** | **✗→✓(本次修复)** | 之前 0 处使用;现 applyAffinity + session.SetAffinity 粘性路由 |
| FallbackEnabled/PriceThreshold/Keywords/Models/Probe | ✓ | fallback.Decide |
| BanSignals/BanKeywords | ✓ | ClassifyBanned |
| QuotaRotateThreshold | ✓ | telemetry.Poller 配额轮转 |
| MaxFailover | ✓ | Orchestrator.MaxAttempts / stream 循环 |
| WarmupHours/MaxConcurrent/BlockOpus | ✓ | buildCandidates warmup |
| Elastic*(Enabled/Baseline/ScaleUp/Down/MaxReserve) | ✓ | buildCandidates 弹性分区 + scale_up/down 事件 |
| SessionErrorThreshold/CooldownSec | ✓ | session.RecordError/Exiled |
| ResponseExileEnabled/Keywords | ✓ | matchesAny 安全拒答放逐 |

结论:唯一未生效的 `AffinityTTLSec` 已实现会话粘性路由(同对话 TTL 内复用同账户,利于 prompt 缓存)。所有触发点均写事件日志(见第四节)。

## 实施结果(2026-06-22 交付)

全部完成并验证(分支 `feat/banctl-rbac-cpa`,6 提交,`go test ./...` 22 包全绿、`go vet` 干净、前端 `tsc`+`vite build` 通过)。

**CLIProxyAPI fork**(per-account 选号):
- 仓库:`github.com/qwwqq1000-arch/CLIProxyAPI`(私有,基于 v7.2.27)。
- 接口:推理请求带 `X-CLIProxy-Account: <id/email/auth_index/...>`(或 `?account=`)即可指定账户;命中不可用账户返回 `auth_not_found`(不静默回退)。见仓库 `ACCOUNT_PIN.md`。
- 已部署到测试服 **23.134.76.25** 的主实例(:8317),旧二进制备份为 `cli-proxy-api.bak-*` / `cli-proxy-api.prev`(可回滚)。线上实测:指定真实账户→200、bogus→503、不带头→正常。

**CPA 接入 Tower**(端到端实测通过):
- 在「节点」用类型 **CPA** 添加,填 baseUrl(`http://23.134.76.25:8317`)、api-key(推理 `sk-...`)、**管理密钥**。
- 测试服管理密钥:服务已设 `Environment=MANAGEMENT_PASSWORD=TowerCPA-mgmt-2026`(附加明文,不动原 hash 配置)。Tower 用它读 `/v0/management/auth-files`。
- 实测:Tower `POST /v1/messages`(dispatchKey)→ 命中 CPA 账户 → 返回 pong(200)→ 号库状态 active、计费 today=$0.0052。

> 凭据(SSH root/密码、SSH 私钥)仅存于仓库根 `server` 文件(已 .gitignore,绝不提交)。

## 测试策略
- 状态机/熔断:`internal/state` 单测覆盖 permanent 触发、不恢复、人工恢复。
- 权限:`internal/api` 表驱动测试 owner 过滤。
- 事件:断言各封控路径写入对应事件类型。
- 全程 `go test ./...`(test DB)+ `go build ./...` + 前端 `tsc`/`vite build`。
- CPA fork:`go build/test`;测试服实测。
