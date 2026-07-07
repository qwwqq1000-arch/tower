# 1M 长上下文模型门控 (#143) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 长上下文(1M)请求只路由到支持 extra-usage 的号;反应式学习并标记不支持的号(TTL 自愈);全在调度层,默认关行为中性。

**Architecture:** 检测 helper(token 估算 + model 标记)→ buildCandidates 候选过滤(仅长上下文请求 + 标记未过期时跳过)→ 反应式标记(extra-usage 400 → 写 `accounts.no_1m_until`)→ 前端 Policies 配置组 + Accounts 徽章/清除。

**Tech Stack:** Go (sqlc/pgx, goose migrations 自动应用) + React/TS/Tailwind SPA。

## Global Constraints

- **默认关行为中性**:所有新逻辑在 `if cfg.LongContextGateEnabled` 内;`no_1m_until` 默认 `0`(`0 > now` 恒假)。`LongContextGateEnabled` 默认 `false`。
- **不写死**:检测标记/关键词/阈值/状态码/恢复窗口全部来自 policy 配置,可在 Policies 页改。
- **两层镜像**:`Dispatch`(非流)和 `DispatchStream`(流)必须等价 —— 同样算 `longCtx`、同样传入 buildCandidates、同样在失败分支查 `isExtraUsageNo1M`。
- **乐观默认**:`no_1m_until` 列 `NOT NULL DEFAULT 0`,假设所有号支持直到被证伪。GetAccount 失败时**不过滤**(当作支持)。
- **policy 5-place plumbing**:Config struct / `Defaults()` / `Patch`(指针)/ `apply()`(`if p.X!=nil`)/ `DryRun()`(`add(...)`)。Policy 测试 `package policy` 内联调 `apply(&c, Patch{...})`。
- **sqlc 手改**(二进制未装):改 `internal/db/sqlc/*.go` 时,给 accounts 的每个 SELECT 列表 + 对应 Scan **末尾**追加 `no_1m_until`,顺序必须一致(列错位会污染相邻字段)。
- 6 个 config 字段的 Go 名(TS `PolicyPatch` 键逐字节对齐):`LongContextGateEnabled` bool、`LongContextTokenThreshold` int、`LongContextModelMarkers` []string、`ExtraUsageKeywords` []string、`ExtraUsageStatusCodes` []int、`No1MRecoveryMs` int64。

---

### Task 1: policy.Config — 6 字段 5-place plumbing

**Files:**
- Modify: `internal/policy/policy.go`
- Test: `internal/policy/policy_test.go`

**Interfaces:**
- Produces: `policy.Config{LongContextGateEnabled, LongContextTokenThreshold, LongContextModelMarkers, ExtraUsageKeywords, ExtraUsageStatusCodes, No1MRecoveryMs}` + 对应 `Patch` 指针字段。后续 Task 3/4/5/6 消费。

- [ ] **Step 1: 写失败测试**(append to `internal/policy/policy_test.go`)

```go
func TestApplyLongContextFields(t *testing.T) {
	c := Defaults()
	if c.LongContextGateEnabled {
		t.Fatal("default LongContextGateEnabled must be false (behavior-neutral)")
	}
	if c.LongContextTokenThreshold != 200000 {
		t.Fatalf("default threshold=%d want 200000", c.LongContextTokenThreshold)
	}
	if c.No1MRecoveryMs != 86400000 {
		t.Fatalf("default recovery=%d want 86400000", c.No1MRecoveryMs)
	}
	en := true
	thr := 150000
	rec := int64(3600000)
	mk := []string{"1m", "[1m]"}
	kw := []string{"extra usage"}
	sc := []int{400, 402}
	apply(&c, Patch{
		LongContextGateEnabled:    &en,
		LongContextTokenThreshold: &thr,
		LongContextModelMarkers:   &mk,
		ExtraUsageKeywords:        &kw,
		ExtraUsageStatusCodes:     &sc,
		No1MRecoveryMs:            &rec,
	})
	if !c.LongContextGateEnabled || c.LongContextTokenThreshold != 150000 || c.No1MRecoveryMs != 3600000 {
		t.Fatalf("apply scalar mismatch: %+v", c)
	}
	if len(c.LongContextModelMarkers) != 2 || c.LongContextModelMarkers[0] != "1m" {
		t.Fatalf("markers mismatch: %v", c.LongContextModelMarkers)
	}
	if len(c.ExtraUsageStatusCodes) != 2 || c.ExtraUsageStatusCodes[1] != 402 {
		t.Fatalf("codes mismatch: %v", c.ExtraUsageStatusCodes)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/policy/ -run TestApplyLongContextFields`
Expected: FAIL (字段未定义 / 编译错误)

- [ ] **Step 3: Config struct** — 在 `internal/policy/policy.go` 的 Config struct 内(靠近 `QuotaLimit*` 字段)加:

```go
	// 1M long-context gating (#143): route long-context requests only to accounts
	// that support extra-usage billing; reactively mark accounts that 400.
	LongContextGateEnabled    bool
	LongContextTokenThreshold int      // est tokens (len(body)/4) above which a request is "long"; 0 = don't judge by tokens
	LongContextModelMarkers   []string // model-string substrings (lower) that mark a request long-context
	ExtraUsageKeywords        []string // body substrings identifying the extra-usage 400
	ExtraUsageStatusCodes     []int    // status codes on which to scan for ExtraUsageKeywords
	No1MRecoveryMs            int64    // how long a no-1M mark lasts before re-probe; <=0 = permanent
```

- [ ] **Step 4: Defaults()** — 在 `Defaults()` 返回的 Config 字面量内加:

```go
		LongContextGateEnabled:    false,
		LongContextTokenThreshold: 200000,
		LongContextModelMarkers:   []string{"1m"},
		ExtraUsageKeywords:        []string{"draw from your external", "extra usage"},
		ExtraUsageStatusCodes:     []int{400},
		No1MRecoveryMs:            86400000,
```

- [ ] **Step 5: Patch struct** — 加指针字段:

```go
	LongContextGateEnabled    *bool
	LongContextTokenThreshold *int
	LongContextModelMarkers   *[]string
	ExtraUsageKeywords        *[]string
	ExtraUsageStatusCodes     *[]int
	No1MRecoveryMs            *int64
```

- [ ] **Step 6: apply()** — 加:

```go
	if p.LongContextGateEnabled != nil {
		c.LongContextGateEnabled = *p.LongContextGateEnabled
	}
	if p.LongContextTokenThreshold != nil {
		c.LongContextTokenThreshold = *p.LongContextTokenThreshold
	}
	if p.LongContextModelMarkers != nil {
		c.LongContextModelMarkers = *p.LongContextModelMarkers
	}
	if p.ExtraUsageKeywords != nil {
		c.ExtraUsageKeywords = *p.ExtraUsageKeywords
	}
	if p.ExtraUsageStatusCodes != nil {
		c.ExtraUsageStatusCodes = *p.ExtraUsageStatusCodes
	}
	if p.No1MRecoveryMs != nil {
		c.No1MRecoveryMs = *p.No1MRecoveryMs
	}
```

- [ ] **Step 7: DryRun()** — 在 diff 处加(用现有 `add(...)` helper;list 类型按现有 list 字段的 add 方式,若 `add` 只接受标量则用现有 `[]int`/`[]string` 字段相同的写法):

```go
	add("LongContextGateEnabled", base.LongContextGateEnabled, final.LongContextGateEnabled)
	add("LongContextTokenThreshold", base.LongContextTokenThreshold, final.LongContextTokenThreshold)
	add("LongContextModelMarkers", base.LongContextModelMarkers, final.LongContextModelMarkers)
	add("ExtraUsageKeywords", base.ExtraUsageKeywords, final.ExtraUsageKeywords)
	add("ExtraUsageStatusCodes", base.ExtraUsageStatusCodes, final.ExtraUsageStatusCodes)
	add("No1MRecoveryMs", base.No1MRecoveryMs, final.No1MRecoveryMs)
```

> 注:若 `add` 的签名只吃可比较标量、现有 `QuotaLimitKeywords`([]string)/`QuotaLimitStatusCodes`([]int)是怎么进 DryRun 的就照抄那种写法(可能用 `fmt.Sprint` 包一层)。以现有同类型字段为准。

- [ ] **Step 8: 跑测试确认通过**

Run: `go test ./internal/policy/ -run TestApplyLongContextFields && go build ./...`
Expected: PASS + build OK

- [ ] **Step 9: Commit**

```bash
git add internal/policy/policy.go internal/policy/policy_test.go
git commit -m "feat(policy): 1M long-context gating config fields (#143, default off)"
```

---

### Task 2: DB 迁移 + sqlc — accounts.no_1m_until

**Files:**
- Create: `migrations/20260626130000_account_no_1m.sql`
- Modify: `internal/db/sqlc/models.go`, `internal/db/sqlc/accounts.sql.go`(及任何含 accounts SELECT 的 sqlc 文件), `queries/accounts.sql`
- Test: `internal/db/migrate_test.go`

**Interfaces:**
- Produces: `sqlc.Account.No1MUntil int64`;`SetAccountNo1MUntil(ctx, SetAccountNo1MUntilParams{ID, No1MUntil})`;`ClearAccountNo1M(ctx, id)`。Task 4/5/7 消费。

- [ ] **Step 1: 写迁移** `migrations/20260626130000_account_no_1m.sql`:

```sql
-- +goose Up
ALTER TABLE accounts ADD COLUMN no_1m_until BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE accounts DROP COLUMN no_1m_until;
```

- [ ] **Step 2: 写迁移测试**(append `internal/db/migrate_test.go`,镜像 #137 的列存在测试)—— 断言迁移后 `accounts` 有 `no_1m_until` 列、类型 bigint、默认 0。若该文件的现有测试用某 helper 查 information_schema,照抄那个 helper 加一个 `TestAccountNo1MColumn`。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/db/ -run TestAccountNo1MColumn`
Expected: FAIL（列不存在 / 测试未编译）

- [ ] **Step 4: queries/accounts.sql** — 在 `GetAccount`(及其他需要的读)SELECT 末尾加 `no_1m_until`;新增:

```sql
-- name: SetAccountNo1MUntil :exec
UPDATE accounts SET no_1m_until = $2 WHERE id = $1;

-- name: ClearAccountNo1M :exec
UPDATE accounts SET no_1m_until = 0 WHERE id = $1;
```

- [ ] **Step 5: sqlc 手改** `internal/db/sqlc/models.go` —— `Account` struct 末尾加 `No1MUntil int64`。`internal/db/sqlc/accounts.sql.go` —— 每个读 accounts 的查询(`GetAccount` 等)：SELECT 列表末尾加 `no_1m_until`,对应 `row.Scan(...)` 末尾加 `&i.No1MUntil`(顺序必须与 SELECT 一致)。加 `SetAccountNo1MUntil`/`SetAccountNo1MUntilParams{ID string; No1MUntil int64}` 和 `ClearAccountNo1M(ctx, id string)`,镜像现有 `:exec` 查询的生成代码风格。

> 关键:逐个核对每个 accounts SELECT 的列顺序与 Scan 顺序。漏改一个读查询会编译失败(Scan 参数数不匹配)或列错位。

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/db/ -run TestAccountNo1MColumn && go build ./...`
Expected: PASS + build OK

- [ ] **Step 7: Commit**

```bash
git add migrations/20260626130000_account_no_1m.sql internal/db/sqlc/ queries/accounts.sql internal/db/migrate_test.go
git commit -m "feat(db): accounts.no_1m_until column + Set/Clear queries (#143)"
```

---

### Task 3: 检测 helpers — longcontext.go

**Files:**
- Create: `internal/dispatch/longcontext.go`, `internal/dispatch/longcontext_test.go`

**Interfaces:**
- Consumes: `policy.Config` 的 6 字段(Task 1)。
- Produces: `isLongContextRequest(model string, body []byte, cfg policy.Config) bool`、`isExtraUsageNo1M(status int, body string, cfg policy.Config) bool`、`const permanentNo1M int64 = 1 << 62`。Task 4/5 消费。

- [ ] **Step 1: 写失败测试** `internal/dispatch/longcontext_test.go`:

```go
package dispatch

import (
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

func cfgLC() policy.Config {
	c := policy.Defaults()
	c.LongContextGateEnabled = true
	return c
}

func TestIsLongContextRequest(t *testing.T) {
	c := cfgLC() // threshold 200000 tokens, markers ["1m"]
	big := []byte(strings.Repeat("x", 200001*4+8))
	if !isLongContextRequest("claude-sonnet-4-6", big, c) {
		t.Fatal("oversized body should be long-context by token estimate")
	}
	if isLongContextRequest("claude-sonnet-4-6", []byte(`{"a":1}`), c) {
		t.Fatal("small body, no marker → not long")
	}
	if !isLongContextRequest("claude-sonnet-4-6[1M]", []byte(`{"a":1}`), c) {
		t.Fatal("model marker (case-insensitive) → long")
	}
	c.LongContextTokenThreshold = 0 // disable token path
	if isLongContextRequest("claude-sonnet-4-6", big, c) {
		t.Fatal("threshold 0 → token path off")
	}
	if !isLongContextRequest("x-1m", big, c) {
		t.Fatal("threshold 0 but marker present → long")
	}
}

func TestIsExtraUsageNo1M(t *testing.T) {
	c := cfgLC()
	body := `{"type":"error","error":{"message":"Third-party apps now draw from your external/extra usage ..."}}`
	if !isExtraUsageNo1M(400, body, c) {
		t.Fatal("400 + keyword should match")
	}
	if isExtraUsageNo1M(429, body, c) {
		t.Fatal("non-400 status should not match")
	}
	if isExtraUsageNo1M(400, `{"error":"rate limited"}`, c) {
		t.Fatal("400 without keyword should not match")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/dispatch/ -run 'TestIsLongContextRequest|TestIsExtraUsageNo1M'`
Expected: FAIL（函数未定义）

- [ ] **Step 3: 写实现** `internal/dispatch/longcontext.go`:

```go
package dispatch

import (
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

// permanentNo1M is the sentinel no_1m_until used when recovery is disabled
// (No1MRecoveryMs <= 0): a far-future timestamp that never elapses in practice.
const permanentNo1M int64 = 1 << 62

// isLongContextRequest reports whether a request should be treated as 1M / long-context:
// estimated input tokens (len(body)/4) over the threshold, OR the model string contains
// any configured marker. Both inputs are config-driven (not hardcoded).
func isLongContextRequest(model string, body []byte, cfg policy.Config) bool {
	if cfg.LongContextTokenThreshold > 0 && len(body)/4 > cfg.LongContextTokenThreshold {
		return true
	}
	lm := strings.ToLower(model)
	for _, mk := range cfg.LongContextModelMarkers {
		if mk != "" && strings.Contains(lm, strings.ToLower(mk)) {
			return true
		}
	}
	return false
}

// isExtraUsageNo1M reports whether a failed response is the extra-usage 400 that means
// "this account does not support 1M / extra usage". Gated on status code first, then keyword.
func isExtraUsageNo1M(status int, body string, cfg policy.Config) bool {
	okCode := false
	for _, c := range cfg.ExtraUsageStatusCodes {
		if c == status {
			okCode = true
			break
		}
	}
	if !okCode {
		return false
	}
	lb := strings.ToLower(body)
	for _, kw := range cfg.ExtraUsageKeywords {
		if kw != "" && strings.Contains(lb, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/dispatch/ -run 'TestIsLongContextRequest|TestIsExtraUsageNo1M'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/longcontext.go internal/dispatch/longcontext_test.go
git commit -m "feat(dispatch): long-context + extra-usage detection helpers (#143)"
```

---

### Task 4: buildCandidates 门控 — longCtx 参数 + 过滤 + key→accountID 暴露

**Files:**
- Modify: `internal/dispatch/service.go`
- Test: `internal/dispatch/longcontext_gate_test.go`(source-grep 镜像守卫)

**Interfaces:**
- Consumes: `isLongContextRequest`(Task 3)、`sqlc.Account.No1MUntil`(Task 2)、`cfg.LongContextGateEnabled`(Task 1)。
- Produces: `buildCandidates` 新增末位返回值 `keyAccount map[string]string`(key→accountID),供 Task 5 标记用;新增入参 `longCtx bool`。

- [ ] **Step 1: 改 buildCandidates 签名** —— 加入参 `longCtx bool`,加返回值 `keyAccount map[string]string`。在循环开头 `keyAccount := map[string]string{}`,每当一个候选 `key` 被 append 进 `cands` 时同时 `keyAccount[key] = na.AccountID`。函数末尾 return 带上 `keyAccount`。

- [ ] **Step 2: 候选过滤守卫** —— 在该候选已有的 `GetAccount` 处读 `no_1m_until`(GetAccount 已被调用以取 onboardedAt),把返回的 `acc` 留到过滤用。在 `cands = append(...)` **之前**加:

```go
		if longCtx && cfg.LongContextGateEnabled && acc.No1MUntil > s.Now() {
			continue // #143: long-context request, this account doesn't support 1M (mark not expired)
		}
```

> 若现有代码里 `GetAccount` 的返回 `acc` 作用域不覆盖到 append 处,把那次 `GetAccount` 调整为在循环顶部统一取一次 `acc, aerr := s.Q.GetAccount(ctx, na.AccountID)`,onboardedAt 与 no_1m 都从它读;`aerr != nil` 时 `acc` 零值 → `No1MUntil=0` → 不过滤(保守)。

- [ ] **Step 3: 两层调用处计算并传 longCtx** —— `Dispatch`(service.go ~597)和 `DispatchStream`(~1982)在调 `buildCandidates` 前加:

```go
	longCtx := isLongContextRequest(model, body, cfg)
```

把 `longCtx` 传进 `buildCandidates(...)`,并接收新增的 `keyAccount` 返回值(暂用 `_` 或存入局部,Task 5 会用)。两处都改。

- [ ] **Step 4: 写镜像守卫测试** `internal/dispatch/longcontext_gate_test.go`(镜像 #2 的 `TestBothTiersConsultEnvelope` source-grep 风格):读 `service.go` 源码,断言 `isLongContextRequest(model, body, cfg)` 在 Dispatch 和 DispatchStream 两个函数体内各出现一次(两层都算 longCtx)。

```go
package dispatch

import (
	"os"
	"strings"
	"testing"
)

func TestBothTiersComputeLongCtx(t *testing.T) {
	src, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(string(src), "isLongContextRequest(model, body, cfg)")
	if n < 2 {
		t.Fatalf("expected isLongContextRequest computed in both Dispatch and DispatchStream, found %d", n)
	}
}
```

- [ ] **Step 5: 跑测试 + build**

Run: `go test ./internal/dispatch/ -run TestBothTiersComputeLongCtx && go build ./... && go vet ./internal/dispatch/...`
Expected: PASS + build/vet OK

- [ ] **Step 6: Commit**

```bash
git add internal/dispatch/service.go internal/dispatch/longcontext_gate_test.go
git commit -m "feat(dispatch): gate long-context requests off no-1M accounts in buildCandidates (#143)"
```

---

### Task 5: 反应式标记 — markNo1M + 两处观测点接线

**Files:**
- Modify: `internal/dispatch/service.go`
- Test: `internal/dispatch/longcontext_mark_test.go`

**Interfaces:**
- Consumes: `isExtraUsageNo1M`(Task 3)、`SetAccountNo1MUntil`(Task 2)、`keyAccount` 映射(Task 4)、`permanentNo1M`(Task 3)。

- [ ] **Step 1: 写 markNo1M 方法**(service.go,靠近 `applyReactiveLimit`):

```go
// markNo1M flags an account as not supporting 1M / extra-usage, persisting a recovery
// deadline (now + No1MRecoveryMs; <=0 → permanent). buildCandidates then skips it for
// long-context requests until the deadline elapses or it is manually cleared.
func (s *Service) markNo1M(ctx context.Context, accountID string, cfg policy.Config) {
	if accountID == "" {
		return
	}
	until := s.Now() + cfg.No1MRecoveryMs
	if cfg.No1MRecoveryMs <= 0 {
		until = permanentNo1M
	}
	_ = s.Q.SetAccountNo1MUntil(ctx, sqlc.SetAccountNo1MUntilParams{ID: accountID, No1MUntil: until})
}
```

- [ ] **Step 2: 接线非流观测点** —— `Dispatch` 的 `OnAttempt` 失败分支(service.go ~713–725,`parseLimitReset` 调用旁)加:

```go
				if longCtx && cfg.LongContextGateEnabled && isExtraUsageNo1M(res.Status, res.Body, cfg) {
					s.markNo1M(ctx, keyAccount[key], cfg)
				}
```

(此处需 `keyAccount` 在闭包作用域内 —— Task 4 已把 `keyAccount` 接成 Dispatch 的局部变量。)

- [ ] **Step 3: 接线流观测点** —— `DispatchStream` 的 `streamOneWithBody` 后未提交分支(service.go ~2182–2200,`parseLimitReset` 调用旁)加:

```go
			if longCtx && cfg.LongContextGateEnabled && isExtraUsageNo1M(out.Status, out.Body, cfg) {
				s.markNo1M(ctx, keyAccount[key], cfg)
			}
```

- [ ] **Step 4: 写测试** `internal/dispatch/longcontext_mark_test.go` —— source-grep 守卫,断言 `markNo1M` 在 Dispatch 与 DispatchStream 两处失败分支都被调用(`strings.Count(src, "isExtraUsageNo1M(") >= 2` 且 `strings.Count(src, "s.markNo1M(") >= 2`)。

```go
package dispatch

import (
	"os"
	"strings"
	"testing"
)

func TestBothTiersMarkNo1M(t *testing.T) {
	src, _ := os.ReadFile("service.go")
	s := string(src)
	if strings.Count(s, "isExtraUsageNo1M(") < 2 {
		t.Fatalf("isExtraUsageNo1M must be checked in both tiers")
	}
	if strings.Count(s, "s.markNo1M(") < 2 {
		t.Fatalf("markNo1M must be called in both tiers")
	}
}
```

- [ ] **Step 5: 跑测试 + build + vet**

Run: `go test ./internal/dispatch/ && go build ./... && go vet ./internal/dispatch/...`
Expected: PASS + OK

- [ ] **Step 6: Commit**

```bash
git add internal/dispatch/service.go internal/dispatch/longcontext_mark_test.go
git commit -m "feat(dispatch): reactively mark no-1M accounts on extra-usage 400 (#143, both tiers)"
```

---

### Task 6: 前端 Policies — 「1M 长上下文门控」组

**Files:**
- Modify: `web/spa/src/types.ts`, `web/spa/src/pages/Policies.tsx`

**Interfaces:**
- Consumes: 6 个 Go 字段名(逐字节对齐)。

- [ ] **Step 1: types.ts** —— `PolicyPatch` 接口加 6 字段:

```ts
  // 1M 长上下文门控 (#143)
  LongContextGateEnabled?: boolean;
  LongContextTokenThreshold?: number;
  LongContextModelMarkers?: string[];
  ExtraUsageKeywords?: string[];
  ExtraUsageStatusCodes?: number[];
  No1MRecoveryMs?: number;
```

- [ ] **Step 2: Policies.tsx hooks** —— 镜像现有 `spendCap5hEnabled`(bool)/`quotaLimitKeywords`(string-list)/数字字段模式,声明 6 个 `useField`:`longContextGateEnabled`(bool,false)、`longContextTokenThreshold`(number,200000)、`longContextModelMarkers`(string-list)、`extraUsageKeywords`(string-list)、`extraUsageStatusCodes`(number-list)、`no1MRecoveryMs`(number,86400000)。加进 `allFields` reset 数组、`hydrateFrom`(setBool/setStr/setNum 用精确 Go 名)、`buildPatch`(`if field.enabled patch.X=field.value`)、`anyEnabled`、对应 `catFields` 分类(放 cadence 或新建分类,与现有组织一致)。

- [ ] **Step 3: 渲染组** —— 加「1M 长上下文门控」`GroupMaster`(绑 `longContextGateEnabled`)+ 各字段 `FieldRow`(每行 `showOnlyConfigured={so}`):token 阈值(number input)、model 标记(string-list input)、extra-usage 关键词(string-list)、状态码(number-list)、恢复窗口 ms(number)。镜像现有 quota-limit 组的 list/number 输入控件。

- [ ] **Step 4: 构建 SPA**

Run: `cd web/spa && npm run build`
Expected: 0 TS 错误

- [ ] **Step 5: Commit**

```bash
git add web/spa/src/types.ts web/spa/src/pages/Policies.tsx
git commit -m "feat(ui): Policies 1M 长上下文门控 group (#143)"
```

---

### Task 7: Accounts「不支持1M」徽章 + 手动清除

**Files:**
- Modify: `internal/api/admin_handlers.go`(+ 路由注册文件), account 列表 handler, `web/spa/src/api.ts`, Accounts 页面组件
- Test: `internal/api/admin_handlers_test.go`

**Interfaces:**
- Consumes: `ClearAccountNo1M`(Task 2)、`sqlc.Account.No1MUntil`(Task 2)。

- [ ] **Step 1: 写 handler 测试**(append `internal/api/admin_handlers_test.go`,镜像现有账号操作端点测试)—— POST `/api/admin/accounts/{id}/clear-no1m` 对某 `no_1m_until>0` 的号 → 调用后该号 `no_1m_until==0`。若现有测试用 httptest + 真 DB harness,照抄那套。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/api/ -run ClearNo1M`
Expected: FAIL（handler/路由不存在）

- [ ] **Step 3: clearNo1MHandler** —— 在 `internal/api/admin_handlers.go` 加:

```go
func clearNo1MHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chiURLParam(r, "id") // 用本仓库实际的 path-param 取法(见相邻 handler)
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "id required"})
			return
		}
		if err := q.ClearAccountNo1M(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		recordAudit(r, q, "account.clear_no1m", "account:"+id, nil, nil)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
```

> `chiURLParam`/path 取法、`writeJSON`、`recordAudit` 用本仓库相邻 handler 的实际写法(照抄一个现有账号操作 handler 的骨架)。

- [ ] **Step 4: 注册路由** —— 在账号操作路由旁注册 `POST /api/admin/accounts/{id}/clear-no1m`(admin 鉴权 + CSRF 头,与相邻账号端点一致)。

- [ ] **Step 5: 账号列表带 no1m** —— 账号列表 API 响应里给每个账号加派生字段 `no1m: account.No1MUntil > now`(或直接回 `no1mUntil`),前端据此显示徽章。

- [ ] **Step 6: 前端** —— `web/spa/src/api.ts` 账号类型加 `no1m?: boolean`(或 `no1mUntil?: number`)+ `clearNo1M(id)` 调用。Accounts 页面:`no1m` 为真显示「不支持1M」徽章 + 「清除」按钮(调 `clearNo1M` 后刷新)。镜像现有账号行的操作按钮。

- [ ] **Step 7: 跑测试 + 构建**

Run: `go test ./internal/api/ -run ClearNo1M && go build ./... && cd web/spa && npm run build`
Expected: PASS + 0 TS 错误

- [ ] **Step 8: Commit**

```bash
git add internal/api/ web/spa/src/api.ts web/spa/src/pages/
git commit -m "feat(api+ui): 不支持1M 徽章 + 手动清除端点 (#143)"
```

---

## Self-Review

- **Spec coverage:** 检测(T3)、存储(T2)、配置(T1)、门控两层(T4)、反应式标记两层(T5)、Policies UI(T6)、Accounts 徽章/清除(T7)—— spec 各组件全覆盖。
- **默认中性:** T1 默认 + 所有逻辑 `if cfg.LongContextGateEnabled` + `no_1m_until` 默认 0 ⇒ 启用前零行为变化。
- **不写死:** 阈值/标记/关键词/状态码/恢复窗口全配置化(T1),Policies 页可改(T6)。
- **Type consistency:** `isLongContextRequest`/`isExtraUsageNo1M`/`permanentNo1M`/`markNo1M`/`keyAccount`/`Account.No1MUntil`/`SetAccountNo1MUntil`/`ClearAccountNo1M` 跨任务签名一致。
- **风险点(实现时读码确认):** (a) T4 中 `GetAccount` 返回 `acc` 的作用域是否覆盖到 append 处(否则按 Step 2 注释统一在循环顶部取一次);(b) T2 必须核对每个 accounts SELECT/Scan 列序;(c) T4/T5 中 `keyAccount` 与 `key`/`body`/`longCtx` 是否在 Dispatch/DispatchStream 闭包作用域内 —— 实现者读 service.go 确认;(d) 候选被过滤空时确实落保底(读 Dispatch 空 order 分支验证)。
