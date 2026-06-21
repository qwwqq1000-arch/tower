// ============================================================
// Tower SPA — Accounts page (号库)
// Read-only placeholder.
// 账户/号库 CRUD API 待补 — OAuth onboarding is a follow-up.
// ============================================================

export default function Accounts() {
  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-semibold text-ink">号库</h1>
        <p className="text-xs text-muted mt-1">Claude 账户管理</p>
      </div>

      {/* Placeholder card */}
      <div className="bg-surface border border-line rounded-xl p-8 flex flex-col items-center text-center space-y-4 max-w-lg mx-auto">
        <div className="w-14 h-14 rounded-full bg-accent/10 flex items-center justify-center text-2xl text-accent">
          ⚿
        </div>
        <div className="space-y-2">
          <h2 className="text-base font-semibold text-ink">账户/号库 CRUD API 待补</h2>
          <p className="text-sm text-muted leading-relaxed">
            后端尚未提供账户列表 API（<code className="text-xs bg-line px-1.5 py-0.5 rounded">GET /api/admin/accounts</code>
            ）。此页面为占位符，待 OAuth 引导流程接入后完善。
          </p>
        </div>
        <div className="w-full bg-bg border border-line rounded-xl p-4 text-left space-y-2">
          <p className="text-xs font-semibold text-muted uppercase tracking-wide">规划中的功能</p>
          <ul className="text-sm text-muted space-y-1.5 list-none">
            <li className="flex items-start gap-2">
              <span className="text-accent mt-0.5">○</span>
              <span>列表展示所有 Claude 账户（邮箱、绑定节点、状态）</span>
            </li>
            <li className="flex items-start gap-2">
              <span className="text-accent mt-0.5">○</span>
              <span>添加账户 — OAuth / Cookie 引导</span>
            </li>
            <li className="flex items-start gap-2">
              <span className="text-accent mt-0.5">○</span>
              <span>账户与节点关系管理（节点可绑定多账户）</span>
            </li>
            <li className="flex items-start gap-2">
              <span className="text-accent mt-0.5">○</span>
              <span>手动标记账户状态（封号 / 恢复）</span>
            </li>
          </ul>
        </div>
        <p className="text-xs text-muted">
          当前节点的账户状态可在
          {' '}
          <a href="/" className="text-accent hover:underline">看板</a>
          {' '}和{' '}
          <a href="/nodes" className="text-accent hover:underline">节点</a>
          {' '}页面查看。
        </p>
      </div>
    </div>
  );
}
