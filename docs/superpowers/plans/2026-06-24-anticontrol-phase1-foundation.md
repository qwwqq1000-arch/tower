# 封控策略重构 Phase 1(地基 + 清理 + 性能)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development(推荐)或 superpowers:executing-plans 逐任务实现。步骤用 `- [ ]` 勾选。

**Goal:** 给 Tower 封控配置加"账户"作用域 + 配置解析缓存 + 区间种子类型,并清理死配置/写死参数,为后续 Phase 2-4 的拟人节奏与限额模块打地基。

**Architecture:** 配置仍走 `policy.Config`/`Patch`;`resolveConfig` 从 3 层扩到 4 层(`Base→global→owner→account`)并加进程内版本缓存消除每请求全表扫;新增 `RangeF/RangeI` 确定性种子类型供后续模块用;删 `fallback_channels.weight`、移除 `QuotaRotateThreshold`(轮询已停)、把 3 处写死参数挪进 Config。

**Tech Stack:** Go(Postgres + sqlc/goose)、内嵌 React/Vite SPA。测试:`go test`、`tsc --noEmit`、`npm run build`、无头 Chrome 验收。

## Global Constraints(逐条来自 spec,每个任务都隐含适用)

- 每个行为旋钮显式声明作用域(错误码/模型/时段),不许笼统;UI 标注"作用于:<码>"。
- 能区间就区间;每号用账号 ID 做种子在区间内定**稳定**随机值(跨重启一致)。
- 零写死参数:现存硬编码挪进 `policy.Config`。
- 消重优先:删 `fallback_channels.weight`(死)、`QuotaRotateThreshold`(轮询停)。
- 热路径零回归:每请求新增逻辑只读内存态;配置解析必须缓存。
- 代码推 `qwwqq1000-arch`;密钥仅服务器侧;日志脱敏。
- **精确边界**:只删 `fallback_channels.weight`,**绝不动** `node_accounts.weight`(账户派单权重,在用)。

## 文件结构(本期触碰)

- 新建 `internal/policy/range.go` — `RangeF`/`RangeI` + 确定性 `Resolve(accountID, salt)`。
- 新建 `internal/policy/range_test.go`、`internal/dispatch/resolveconfig_test.go`。
- 改 `internal/dispatch/service.go` — `resolveConfig` 加账户层 + 缓存;挪 TZ/默认 reset 写死。
- 改 `internal/policy/policy.go` — 删 `QuotaRotateThreshold`/`PickThreshold`;Config 加 `QuietHoursTZ`/`QuotaLimitDefaultResetMs`/`UpstreamTimeoutSec`。
- 改 `internal/api/console_handlers.go` + `internal/api/router.go` — 账户层 PUT/DELETE 路由 + handler。
- 改 `queries/fallback_channels.sql` + 新 migration + `internal/db/sqlc/*` — drop `weight`。
- 改 `internal/telemetry/poller.go`、`internal/cpaclient/discovery.go`、`internal/api/account_handlers.go` — 摘除 `QuotaRotateThreshold` 轮换。
- 改 `internal/dispatch/proxy.go` — HTTP 超时读 Config。
- 改 `web/spa/src/pages/Policies.tsx` + `web/spa/src/api.ts` — 账户选择器 + 作用域切换 + Range 双输入。

---

### Task 1: `Range` 确定性种子类型

**Files:**
- Create: `internal/policy/range.go`
- Test: `internal/policy/range_test.go`

**Interfaces:**
- Produces: `type RangeF struct{ Min, Max float64 }`、`type RangeI struct{ Min, Max int64 }`;`(RangeF) Resolve(accountID, salt string) float64`;`(RangeI) Resolve(accountID, salt string) int64`。后续 Phase 2-4 全部区间字段都用这两个类型 + `Resolve`。

- [ ] **Step 1: 写失败测试**
```go
// internal/policy/range_test.go
package policy

import "testing"

func TestRangeResolveDeterministicAndInBounds(t *testing.T) {
	r := RangeF{Min: 100, Max: 200}
	a := r.Resolve("acc_123", "spend5h")
	b := r.Resolve("acc_123", "spend5h")
	if a != b {
		t.Fatalf("不稳定:%v != %v", a, b)
	}
	if a < 100 || a > 200 {
		t.Fatalf("越界:%v", a)
	}
	// 不同 salt 应通常给不同值(避免同号所有区间同分位)
	if r.Resolve("acc_123", "rpm") == a {
		t.Logf("提示:salt 区分弱,但非致命")
	}
	// 不同账号通常不同
	if r.Resolve("acc_999", "spend5h") == a {
		t.Logf("提示:账号区分弱")
	}
}

func TestRangeResolveDegenerate(t *testing.T) {
	if got := (RangeF{Min: 5, Max: 5}).Resolve("x", "y"); got != 5 {
		t.Fatalf("相等区间应=Min,得 %v", got)
	}
	if got := (RangeF{Min: 9, Max: 1}).Resolve("x", "y"); got != 9 {
		t.Fatalf("Max<Min 应=Min,得 %v", got)
	}
	if got := (RangeI{Min: 3, Max: 10}).Resolve("acc", "burst"); got < 3 || got > 10 {
		t.Fatalf("RangeI 越界:%v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**
Run: `go test ./internal/policy/ -run TestRange -v`
Expected: FAIL(`RangeF` undefined)

- [ ] **Step 3: 实现**
```go
// internal/policy/range.go
package policy

import "hash/fnv"

// RangeF 是一个 [Min,Max] 浮点区间。Resolve 用 accountID+salt 做种子,
// 在区间内取一个稳定值(同号同 salt 跨重启恒定),用于"每个号表现成区间内
// 某个固定的人"。salt 区分不同旋钮,避免同号所有区间取同一分位。
type RangeF struct {
	Min float64 `json:"Min"`
	Max float64 `json:"Max"`
}

func seedFrac(accountID, salt string) float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(accountID + "|" + salt))
	return float64(h.Sum64()%10000) / 10000.0
}

func (r RangeF) Resolve(accountID, salt string) float64 {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + seedFrac(accountID, salt)*(r.Max-r.Min)
}

// RangeI 同理,整数(时长 ms / 次数 / RPM 等)。
type RangeI struct {
	Min int64 `json:"Min"`
	Max int64 `json:"Max"`
}

func (r RangeI) Resolve(accountID, salt string) int64 {
	if r.Max <= r.Min {
		return r.Min
	}
	span := r.Max - r.Min
	return r.Min + int64(seedFrac(accountID, salt)*float64(span))
}
```

- [ ] **Step 4: 跑测试确认通过**
Run: `go test ./internal/policy/ -run TestRange -v`
Expected: PASS

- [ ] **Step 5: 提交**
```bash
git add internal/policy/range.go internal/policy/range_test.go
git commit -m "feat(policy): RangeF/RangeI 确定性种子区间类型"
```

---

### Task 2: `resolveConfig` 加账户作用域(4 层)+ 账户层 API

**Files:**
- Modify: `internal/dispatch/service.go`(`resolveConfig`,约 454-486;调用点传入 accountID)
- Modify: `internal/api/console_handlers.go`(新 `putAccountPolicyHandler`、`deleteAccountPolicyHandler`)
- Modify: `internal/api/router.go`(50-53 附近加路由)
- Test: `internal/dispatch/resolveconfig_test.go`

**Interfaces:**
- Consumes: `policy.Resolve(base, patches...)`、`q.ListPolicies`、`q.UpsertPolicy`、`q.DeletePolicy`(若无 DeletePolicy 需在 queries 加,见下)。
- Produces: `resolveConfig(ctx, ownerID, accountID string) policy.Config`(签名新增 accountID;空串=无账户层)。账户层 patch 存 `policies(scope_type="account", scope_id=accountID)`。

- [ ] **Step 1: 写失败测试**(直接测解析顺序,不连库:把 patch 列表抽成纯函数 `resolvePatches`)
```go
// internal/dispatch/resolveconfig_test.go
package dispatch

import (
	"testing"
	"github.com/qwwqq1000-arch/tower/internal/policy"
)

func TestResolveOrderAccountWins(t *testing.T) {
	base := policy.Defaults()
	mc := 3
	g := policy.Patch{MaxConcurrent: ptrInt(5)}   // 全局
	o := policy.Patch{MaxConcurrent: ptrInt(7)}   // 租户
	a := policy.Patch{MaxConcurrent: ptrInt(9)}   // 账户
	_ = mc
	got := policy.Resolve(base, g, o, a) // 顺序:全局→租户→账户,后者赢
	if got.MaxConcurrent != 9 {
		t.Fatalf("账户层应赢,得 %d", got.MaxConcurrent)
	}
	got2 := policy.Resolve(base, g, o) // 无账户层 → 租户值
	if got2.MaxConcurrent != 7 {
		t.Fatalf("无账户层应=租户7,得 %d", got2.MaxConcurrent)
	}
}

func ptrInt(i int) *int { return &i }
```

- [ ] **Step 2: 跑测试**
Run: `go test ./internal/dispatch/ -run TestResolveOrder -v`
Expected: 若 `policy.Resolve` 已支持可变 patch 顺序则 PASS;本测试主要锁定"账户最后、后者赢"的语义契约。先跑确认绿/红。

- [ ] **Step 3: 改 `resolveConfig` 签名 + 解析账户层**
在 `internal/dispatch/service.go` 的 `resolveConfig` 内,switch 增加账户分支,并按 `global→owner→account` 顺序 append:
```go
func (s *Service) resolveConfig(ctx context.Context, ownerID, accountID string) policy.Config {
	rows, err := s.Q.ListPolicies(ctx)
	if err != nil {
		return s.Base
	}
	var gp, op, ap *policy.Patch
	for _, r := range rows {
		switch {
		case r.ScopeType == "global":
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil { gp = &p }
		case ownerID != "" && r.ScopeType == "owner" && r.ScopeID == ownerID:
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil { op = &p }
		case accountID != "" && r.ScopeType == "account" && r.ScopeID == accountID:
			var p policy.Patch
			if json.Unmarshal(r.Params, &p) == nil { ap = &p }
		}
	}
	patches := make([]policy.Patch, 0, 3)
	if gp != nil { patches = append(patches, *gp) }
	if op != nil { patches = append(patches, *op) }
	if ap != nil { patches = append(patches, *ap) }
	return policy.Resolve(s.Base, patches...)
}
```
然后修每个调用点:把现在传 `ownerID` 的地方,在已知账户时传 accountID,未知传 `""`。用编译错误定位所有调用点:
Run: `go build ./... 2>&1 | grep resolveConfig`,逐个补第二个参数(派单内层已选定账号的路径传该账号 key 的 accountID;预检/未定账号阶段传 `""`)。

- [ ] **Step 4: 加账户层 handler + 路由 + DeletePolicy 查询**
- 若 `queries/` 无 `DeletePolicy`,在对应 .sql 加 `-- name: DeletePolicy :exec\nDELETE FROM policies WHERE scope_type=$1 AND scope_id=$2;` 并 `sqlc generate`(见 Task 4 的 sqlc 流程)。
- `internal/api/console_handlers.go` 仿 `putTenantPolicyHandler` 写 `putAccountPolicyHandler`(`ScopeType="account", ScopeID=accountId`,合并已有 params)与 `deleteAccountPolicyHandler`(调 `DeletePolicy("account", accountId)`)。
- `internal/api/router.go` 加:
```go
mux.HandleFunc("PUT /api/admin/policies/account/{accountId}", requireSuperadmin(secret, q, putAccountPolicyHandler(q)))
mux.HandleFunc("DELETE /api/admin/policies/account/{accountId}", requireSuperadmin(secret, q, deleteAccountPolicyHandler(q)))
```

- [ ] **Step 5: 跑测试 + 编译**
Run: `go test ./internal/dispatch/ -run TestResolveOrder -v && go build ./...`
Expected: PASS + 编译通过

- [ ] **Step 6: 提交**
```bash
git add internal/dispatch/service.go internal/dispatch/resolveconfig_test.go internal/api/console_handlers.go internal/api/router.go queries/ internal/db/sqlc/
git commit -m "feat(policy): 账户作用域(Base→全局→租户→账户)+ 账户层 PUT/DELETE"
```

---

### Task 3: 配置解析缓存(消除每请求全表扫)

**Files:**
- Modify: `internal/dispatch/service.go`(缓存字段 + `resolveConfig` 命中逻辑)
- Modify: `internal/api/console_handlers.go`(三个 put/delete handler 写库后 bump 版本)
- Test: `internal/dispatch/resolveconfig_test.go`(加缓存失效用例)

**Interfaces:**
- Produces: `Service.policyVer atomic.Int64`、`Service.BumpPolicyVersion()`(API handler 写库后调用)。`resolveConfig` 命中缓存返回同值,版本变后重建。

- [ ] **Step 1: 写失败测试**
```go
func TestPolicyCacheInvalidatesOnBump(t *testing.T) {
	s := newTestService(t)            // 测试已有的构造器(参考其它 *_test.go)
	c1 := s.resolveConfig(ctxBG(), "", "")
	s.BumpPolicyVersion()
	c2 := s.resolveConfig(ctxBG(), "", "")
	_ = c1; _ = c2                    // 主要验证:bump 后不 panic、重建路径走到
	if s.policyVer.Load() == 0 { t.Fatalf("版本未自增") }
}
```
(若无 `newTestService`,参考 `internal/dispatch/service_test.go` 现有构造方式,复用之。)

- [ ] **Step 2: 跑测试**
Run: `go test ./internal/dispatch/ -run TestPolicyCache -v` → FAIL(`BumpPolicyVersion` undefined)

- [ ] **Step 3: 实现缓存**
- `Service` 加字段:`policyVer atomic.Int64`;`policyCache sync.Map`(key=`ownerID+"|"+accountID`,value=`cachedCfg{ver int64; cfg policy.Config}`)。
- `BumpPolicyVersion()`:`s.policyVer.Add(1)`。
- `resolveConfig` 开头:读 `ver := s.policyVer.Load()`;查缓存,若存在且 `entry.ver==ver` 直接返回 `entry.cfg`;否则执行现有解析(Task 2),写 `policyCache.Store(key, cachedCfg{ver, cfg})` 后返回。
- 在 `putGlobalPolicyHandler`/`putTenantPolicyHandler`/`putAccountPolicyHandler`/`deleteAccountPolicyHandler` 写库成功后调用 `svc.BumpPolicyVersion()`(handler 需能拿到 Service 引用;若 handler 只有 `q`,改为给 router 传入 svc 或一个 `bump func()`)。

- [ ] **Step 4: 跑测试 + 全量**
Run: `go test ./internal/dispatch/... && go build ./...`
Expected: PASS

- [ ] **Step 5: 提交**
```bash
git add internal/dispatch/service.go internal/dispatch/resolveconfig_test.go internal/api/
git commit -m "perf(policy): resolveConfig 版本缓存,消除每请求 ListPolicies 全表扫"
```

---

### Task 4: 删 `fallback_channels.weight`(仅此列,不动 node_accounts.weight)

**Files:**
- Create: `migrations/20260624000050_drop_fallback_weight.sql`
- Modify: `queries/fallback_channels.sql`(去掉 weight 列/占位)
- Modify(生成): `internal/db/sqlc/fallback_channels.sql.go`、`internal/db/sqlc/models.go`
- Modify: `internal/dispatch/service.go` 及测试中引用 `ch.Weight` 处

- [ ] **Step 1: 写 migration**
```sql
-- migrations/20260624000050_drop_fallback_weight.sql
-- +goose Up
ALTER TABLE fallback_channels DROP COLUMN IF EXISTS weight;
-- +goose Down
ALTER TABLE fallback_channels ADD COLUMN weight INTEGER NOT NULL DEFAULT 1;
```

- [ ] **Step 2: 改查询**:在 `queries/fallback_channels.sql` 的 INSERT/UPDATE/SELECT 去掉 `weight`,重排 `$n` 占位。

- [ ] **Step 3: 重新生成 sqlc**
Run: `sqlc generate`(项目根有 `sqlc.yaml`)
Expected: `internal/db/sqlc/fallback_channels.sql.go`、`models.go` 不再含 `FallbackChannel.Weight`、`weight` 参数。
若环境无 sqlc 二进制:手改这两个生成文件,删 `Weight` 字段/扫描/参数。

- [ ] **Step 4: 修编译**
Run: `go build ./... 2>&1 | grep -i weight`
逐个删 `internal/dispatch/service.go` 及 `*_test.go` 里对 fallback 渠道 `.Weight` 的引用(确认是 fallback 渠道而非 node account)。
Run: `go build ./... && go test ./internal/dispatch/...`
Expected: 通过

- [ ] **Step 5: 提交**
```bash
git add migrations/20260624000050_drop_fallback_weight.sql queries/fallback_channels.sql internal/db/sqlc/ internal/dispatch/
git commit -m "chore(fallback): 删除未使用的 fallback_channels.weight 死配置"
```

---

### Task 5: 移除 `QuotaRotateThreshold`(轮询已停)

**前置确认(必须先做):** 该阈值由轮询路径使用(`internal/telemetry/poller.go:104`、`internal/cpaclient/discovery.go:127` 经 `policy.PickThreshold`)。用户已停轮询。**实现者先确认轮询确实不再驱动轮换**(检查 poller/discovery 是否仍被定时调用):
Run: `grep -rn "poller\|StartPoll\|discovery.*Sync\|ticker" internal --include="*.go" | grep -v _test`
- 若轮询路径已是死代码 → 一并删 poller/discovery 的轮换投影;
- 若 poller 仍跑别的事(如遥测展示)→ 只摘除"按 QuotaRotateThreshold 投影 SetLimited/轮换"的部分,保留其余。

**Files:** `internal/policy/policy.go`、`internal/telemetry/poller.go`、`internal/telemetry/map.go`、`internal/cpaclient/discovery.go`、`internal/api/account_handlers.go`

- [ ] **Step 1:** 删 `policy.go` 中 `QuotaRotateThreshold`(Config:67、Defaults:148、Patch:201、apply:276-281、DryRun add:396)与 `PickThreshold`(11-25)。
- [ ] **Step 2:** 删/改 `telemetry/poller.go:104`、`cpaclient/discovery.go:86/127`、`telemetry/map.go:60` 注释与阈值投影;`account_handlers.go:289` 的"saturated past QuotaRotateThreshold"叠加层(改为依赖反应式限额的 `LimitState`)。
- [ ] **Step 3: 编译 + 测试**
Run: `go build ./... && go test ./...`
Expected: 通过(若有测试断言该字段,删除/改写之)
- [ ] **Step 4: 提交**
```bash
git add internal/policy/policy.go internal/telemetry/ internal/cpaclient/discovery.go internal/api/account_handlers.go
git commit -m "chore(policy): 移除 QuotaRotateThreshold(轮询已停,改由反应式限额+花费上限收敛)"
```

---

### Task 6: 写死参数挪进 Config

**Files:** `internal/policy/policy.go`(Config/Patch/apply/Defaults)、`internal/dispatch/service.go`、`internal/dispatch/proxy.go`

新增三个 Config 字段(默认值=现行为,保证零行为变更):
- `QuietHoursTZ string`(默认 `"Asia/Shanghai"`)→ 替换 `service.go:496` 与 `:1344` 的 `time.LoadLocation("Asia/Shanghai")`。
- `QuotaLimitDefaultResetMs int64`(默认 `300000`)→ 替换 `service.go:205` 的 `now + 5*60*1000`。
- `UpstreamTimeoutSec int`(默认 `300`)→ 替换 `proxy.go:74` 的 `300 * time.Second`。

- [ ] **Step 1:** Config/Patch/apply/Defaults 加三字段(默认同上)。
- [ ] **Step 2:** `service.go:496/1344` 改 `time.LoadLocation(cfg.QuietHoursTZ)`(就近取得 cfg);`:205` 改 `now + cfg.QuotaLimitDefaultResetMs`;`proxy.go:74` 的 `newHTTP()` 改为接受 timeout 参数,构造处传 `time.Duration(cfg.UpstreamTimeoutSec)*time.Second`(注意:`newHTTP` 当前无 cfg,改签名 `newHTTP(timeoutSec int)`,调用点传值;默认 300 不变)。
- [ ] **Step 3: 编译 + 测试**
Run: `go build ./... && go test ./internal/dispatch/... ./internal/policy/...`
Expected: 通过,行为不变(默认值=旧硬编码)
- [ ] **Step 4:** 注:`fallback_handlers.go:14`、`admin_handlers.go:280` 的上海时区是**计费"日"边界**,语义不同,本期不动(后续如需做 `BillingTZ` 另开)。在本任务 commit message 注明。
- [ ] **Step 5: 提交**
```bash
git add internal/policy/policy.go internal/dispatch/service.go internal/dispatch/proxy.go
git commit -m "chore(config): 槽位时区/限额兜底reset/上游超时 三处写死参数挪进 Config(默认值不变;计费日TZ另议)"
```

---

### Task 7: 前端 — 账户选择器 + 作用域切换 + Range 双输入

**Files:** `web/spa/src/pages/Policies.tsx`、`web/spa/src/api.ts`

**Interfaces:**
- Consumes: 现有 `listAccounts()`(取账户列表)、`PUT /api/admin/policies/global|tenant`;新增 `PUT/DELETE /api/admin/policies/account/{id}`(Task 2)。
- Produces: `api.ts` 加 `putAccountPolicy(accountId, patch)`、`deleteAccountPolicy(accountId)`;`RangeInput` 小组件(min~max 双输入)供 Phase 2-4 复用。

- [ ] **Step 1:** `api.ts` 加:
```ts
export const putAccountPolicy = (accountId: string, patch: Record<string, unknown>) =>
  api('PUT', `/api/admin/policies/account/${accountId}`, patch);
export const deleteAccountPolicy = (accountId: string) =>
  api('DELETE', `/api/admin/policies/account/${accountId}`);
```
- [ ] **Step 2:** `Policies.tsx` 顶部加**作用域选择**:`全局 / 租户:<选> / 账户:<账户选择器,默认当前账户>`(沿用 nexaxis 账户选择器铁律:默认当前账户、按账户 scope)。保存按钮按所选 scope 调对应 API;账户 scope 多一个"清除此号配置"(`deleteAccountPolicy`)。
- [ ] **Step 3:** 加 `RangeInput`(两个 number 输入,值 `{Min,Max}`),Phase 2-4 区间字段统一用;本期至少给一个现有可区间化字段(如把 `SlotCooldownMin/Max` 在 UI 上并成一个 RangeInput 展示,后端仍是两字段,Phase 3 再正式并模块)。
- [ ] **Step 4: 构建校验**
Run: `cd web/spa && npx tsc --noEmit && npm run build`
Expected: 通过
- [ ] **Step 5: 无头浏览器验收**(本会话已建立的方法:puppeteer-core + 系统 Chrome,登录后载 `/policies`,确认渲染无 pageerror、账户选择器存在、切到账户 scope 保存命中 `PUT .../account/...`)。
- [ ] **Step 6: 提交**
```bash
git add web/spa/src/pages/Policies.tsx web/spa/src/api.ts
git commit -m "feat(ui): 封控策略账户作用域选择器 + 作用域切换 + RangeInput 复用组件"
```

---

## 收尾验证(Phase 1 整体)

- [ ] `go build ./... && go test ./...` 全绿
- [ ] `cd web/spa && npx tsc --noEmit && npm run build` 通过
- [ ] 部署到 23.237(单 ControlMaster 连接,见 deploy-creds 备忘)后无头浏览器验收 `/policies` 与 `/accounts`
- [ ] 确认 dispatch 热路径不再每请求 `ListPolicies`(加日志计数或 pprof 抽样)

## 自查(spec 覆盖)

- 账户作用域 ✅T2 / 配置缓存 ✅T3 / Range 种子 ✅T1 / 删 weight ✅T4 / 删 QuotaRotateThreshold ✅T5 / 挪写死参数 ✅T6 / 前端账户选择器+Range ✅T7。
- **本期不含**(留后续 Phase):HumanDelay/SlotCooldown 正式合并、RateGovernor、SessionSim、QuietHours 降速行为、SpendCap、FallbackSpendCap、SerialQueue、ModelPin、BodyPad。Phase 1 落地后再各自出计划(基于本期真实接口)。
