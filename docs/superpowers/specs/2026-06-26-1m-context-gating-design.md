# 1M 长上下文模型门控 (#143) — Design Spec

**Date:** 2026-06-26
**Status:** Approved (design decisions locked)
**Feature flag:** `LongContextGateEnabled` — default **OFF** (behavior-neutral until enabled)

## Problem

`claude-sonnet-4-6` 的 1M / 长上下文请求(输入 >200k tokens)打到**未开通 extra-usage / 1M billing 的账号**会返回:

```
status_code=400  "Third-party apps now draw from your external/extra usage …"
```

这是纯浪费(必失败 + 触发 failover)。要：**提前检测**长上下文请求，**只路由到支持 1M 的号**；账号 1M 能力是账号侧 billing 设置、无法直接探知 → **反应式学习**:某号回这个 400 就标记「不支持1M」,后续长上下文请求不再给它(TTL 自愈 + 手动清除)。

全部在**调度层**做(符合架构原则 dispatch-layer-anticontrol);节点保持干净。

## Design decisions (locked)

1. **检测口径**:请求为「长上下文」当且仅当 —— `estTokens(=len(body)/4) > LongContextTokenThreshold` **或** `model` 串(小写)包含 `LongContextModelMarkers` 任一标记。任一命中即长上下文。两路都可配、不写死。
2. **恢复策略**:TTL 自愈。标记带恢复窗口 `No1MRecoveryMs`(默认 24h),过期自动重新尝试;另给手动清除。`No1MRecoveryMs<=0` 视为永久(留作可选)。

## Architecture

### 1. 检测 (internal/dispatch/longcontext.go, 新文件)

```go
// isLongContextRequest reports whether this request should be treated as 1M/long-context.
// est = len(body)/4 (the same rough token heuristic used for cost estimation).
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

// isExtraUsageNo1M reports whether a failed response is the extra-usage 400
// that means "this account does not support 1M / extra usage".
func isExtraUsageNo1M(status int, body string, cfg policy.Config) bool {
    okCode := false
    for _, c := range cfg.ExtraUsageStatusCodes {
        if c == status { okCode = true; break }
    }
    if !okCode { return false }
    lb := strings.ToLower(body)
    for _, kw := range cfg.ExtraUsageKeywords {
        if kw != "" && strings.Contains(lb, strings.ToLower(kw)) { return true }
    }
    return false
}
```

镜像现有 `parseLimitReset` 的对称风格(status-code gate → keyword scan)。

### 2. 存储 — accounts 表加列 (反应式标记,DB 落地)

迁移:`ALTER TABLE accounts ADD COLUMN no_1m_until BIGINT NOT NULL DEFAULT 0;`

- `0` = 支持 / 未标记(**乐观默认**:假设所有号支持,直到被证伪)。
- `>0` = unix-ms,在此之前视为「不支持1M」。`now >= no_1m_until` 即自动恢复。

**DB 落地 → 重启自动生效,无需内存态/warm-restore**(`buildCandidates` 本就每候选调 `GetAccount`,顺读这列)。sqlc 手改(二进制未装):`Account` model + 所有读 `accounts` 的查询 Scan(GetAccount 等)+ 新查询 `SetAccountNo1MUntil(id, no_1m_until)` 和 `ClearAccountNo1M(id)`(置 0)。

### 3. 门控点 — buildCandidates

`buildCandidates` 当前签名无 body,所以在 `Dispatch`/`DispatchStream` 调用前算好 `longCtx := isLongContextRequest(model, body, cfg)`,作为**新参数**传入(两处调用都改)。内层候选循环新增 `continue` 守卫(在 `cands = append(...)` 前):

```go
if longCtx && cfg.LongContextGateEnabled && acc.No1MUntil > s.Now() {
    continue // 长上下文请求,此号不支持1M(标记未过期)→ 跳过
}
```

- **仅**长上下文请求受影响;普通请求完全不变。
- 全在 `cfg.LongContextGateEnabled` 内 → 默认关时零行为变化。
- `acc` 来自该候选已有的 `GetAccount` 调用;若 GetAccount 失败,**不过滤**(保守:当作支持)。

`buildCandidates` 额外暴露 `key→accountID` 映射(镜像现有 `keyOwner map[string]string` 的返回方式),供标记时用 `key` 反查 `accountID`。

### 4. 候选被过滤空 → 保底

若长上下文过滤把本地候选清空(无任何号支持1M),`order` 为空 → 复用现有「本地耗尽 → viaChannels/streamChannels 保底(exhausted)」路径(付费渠道支持大上下文)。**无需特殊处理**;计划任务中验证空集确实落保底而非 503。

### 5. 反应式标记 — 两处对称观测点

helper `isExtraUsageNo1M` 在两处调用(镜像 `parseLimitReset` 的两处):
- **非流** `Dispatch` 的 `OnAttempt` 回调(service.go ~713–725,失败分支):`res.Status`/`res.Body`。
- **流** `DispatchStream` 的 `streamOneWithBody` 后未提交分支(service.go ~2182–2200):`out.Status`/`out.Body`。

命中 **且** `longCtx` **且** `cfg.LongContextGateEnabled` → `s.markNo1M(ctx, accountID)`:

```go
func (s *Service) markNo1M(ctx context.Context, accountID string, cfg policy.Config) {
    until := s.Now() + cfg.No1MRecoveryMs
    if cfg.No1MRecoveryMs <= 0 { until = permanentNo1M } // 1<<62, 实质永久
    _ = s.Q.SetAccountNo1MUntil(ctx, sqlc.SetAccountNo1MUntilParams{ID: accountID, No1MUntil: until})
}
```

`accountID` 由 `key` 经 buildCandidates 暴露的 `key→accountID` 映射反查。

### 6. policy.Config — 6 字段 (5 处 plumbing,默认中性)

| 字段 | 类型 | 默认 | 含义 |
|---|---|---|---|
| `LongContextGateEnabled` | bool | `false` | 总开关 |
| `LongContextTokenThreshold` | int | `200000` | est tokens(len(body)/4)超过即长上下文;`0`=不按 token 判 |
| `LongContextModelMarkers` | []string | `["1m"]` | model 串含任一(小写子串)即长上下文;不写死 |
| `ExtraUsageKeywords` | []string | `["draw from your external", "extra usage"]` | 命中即判该号不支持1M;不写死 |
| `ExtraUsageStatusCodes` | []int | `[400]` | 只在这些状态码上查关键词 |
| `No1MRecoveryMs` | int64 | `86400000` (24h) | 标记恢复窗口;`<=0`=永久 |

5 处:Config struct / `Defaults()` / `Patch`(指针字段)/ `apply()`(`if p.X!=nil`)/ `DryRun()`(`add(...)`)。Policy 测试 `package policy` 内联调 `apply()`。

### 7. 前端

- **Policies 页**新增「1M 长上下文门控」组(镜像现有字段模式,每行 `showOnlyConfigured={so}`):总开关 + token 阈值(number)+ model 标记(string list)+ extra-usage 关键词(string list)+ 状态码(number list)+ 恢复窗口 ms(number)。`PolicyPatch` TS 接口加这 6 字段,Go 名逐字节对齐。
- **Accounts 视图**:`no_1m_until > now` 的号显示「不支持1M」徽章;手动清除按钮 → 新增 admin 端点 `POST /api/admin/accounts/{id}/clear-no1m`(`ClearAccountNo1M`,置 0),镜像现有账号操作端点 + CSRF 头。账号列表 API 需带 `no_1m_until`(或派生 `no1m: bool`)字段。

## 默认中性 (behavior-neutral) 保证

- `LongContextGateEnabled=false` → 门控 `continue` 和 `markNo1M` 都不触发;`isLongContextRequest` 即使被算也无副作用。
- `no_1m_until` 默认 `0` → 任何 `0 > now` 恒假 → 即便误读也不过滤。
- 不改任何现有 dispatch 行为路径。

## 测试要点

- `isLongContextRequest`:token 超阈值命中、model 标记命中(大小写)、两者皆不命中不判长、阈值 0 时只看标记。
- `isExtraUsageNo1M`:400+关键词命中、非 400 不命中、400 无关键词不命中。
- 门控:long+enabled+marked → 跳过;long 但 disabled → 不跳;non-long → 不跳;marked 已过期(no_1m_until<now)→ 不跳。
- 两层镜像守卫(source-grep,如 #2 Task4 的 TestBothTiersConsultEnvelope)。
- policy 6 字段在 5 处全到位。

## Out of scope (YAGNI)

- 主动探测账号 1M 能力(无 API)——只反应式学。
- count_tokens 精确计数——`len(body)/4` 够用。
- 按 model 区分不同 1M 阈值——单一全局阈值。
