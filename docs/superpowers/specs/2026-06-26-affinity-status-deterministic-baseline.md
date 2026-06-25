# 修复:弹性基准工作集不稳定导致 reserve 号收非亲和流量

## 根因
`ListNodeAccountsByNode` 无 ORDER BY → Postgres 返回行顺序非确定。buildCandidates
按此建 cands,`sort.SliceStable(weight 降序)` 在所有号 weight 相等(=100)时保持
非确定输入序 → 弹性 baseline(前 N)每请求是随机的不同 N 个号。后果:reserve 号随机
进 baseline 收到非亲和流量(inflight>0);状态快照(ReserveKeys 同样非确定)又算它
reserve → 显示「待命+inflight+非亲和」。佐证:期间 0 个 scale_up 事件(弹性没弹),
仍有 reserve 非亲和流量 → 只能是基准集随机。

## 方案 A(已认可):确定性排序
buildCandidates(:1096)和 ReserveKeys(:1271)两处的 cands 排序改为
`weight 降序 + key 升序兜底`(`sort.Slice`)。基准 = 前 N 变确定 → 工作集固定 →
只有固定 baseline 号接非亲和流量;reserve 号真待命(除非弹性弹起或亲和)。状态准确。

## 测试
- buildCandidates 与 ReserveKeys 对同一组号返回一致的 baseline/reserve 划分。
- 多次调用基准集稳定(同一批号 → 同一前 N)。
