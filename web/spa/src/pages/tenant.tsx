// ============================================================
// Tower SPA — Tenant-mode views (role === 'tenant')
// Own-data only, via /api/me/* endpoints.
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import {
  getMeDashboard,
  getMeAccounts,
  pauseMeAccount,
  getMeLedger,
} from '../api';
import type { MeAccountRow, MeDashboard, LedgerEntry } from '../types';

// ------------------------------------------------------------------
// Shared formatters
// ------------------------------------------------------------------
function fmtCost(usd: number | undefined): string {
  if (usd == null) return '—';
  if (usd === 0) return '$0.00';
  if (Math.abs(usd) < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

function fmtTime(ms: number): string {
  if (!ms) return '—';
  return new Date(ms).toLocaleString('zh-CN', {
    month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

function fmtUsd(amount: number): string {
  return `$${amount.toFixed(6)}`;
}

function fmtDate(ms: number | undefined): string {
  if (!ms) return '—';
  return new Date(ms).toISOString().slice(0, 10);
}

// ------------------------------------------------------------------
// Stat card
// ------------------------------------------------------------------
function StatCard({ label, value, sub, accent, warn }: {
  label: string; value: string | number; sub?: string; accent?: boolean; warn?: boolean;
}) {
  const valueClass = accent ? 'text-accent' : warn ? 'text-warn' : 'text-ink';
  return (
    <div className="bg-surface border border-line rounded-xl p-4 flex flex-col gap-1">
      <span className="text-xs text-muted uppercase tracking-wide">{label}</span>
      <span className={`text-2xl font-bold ${valueClass}`}>{value}</span>
      {sub && <span className="text-xs text-muted">{sub}</span>}
    </div>
  );
}

// ============================================================
// TenantDashboard (/)
// ============================================================
const REFRESH_INTERVAL_MS = 30_000;

export function TenantDashboard() {
  const [data, setData] = useState<MeDashboard | null>(null);
  const [accounts, setAccounts] = useState<MeAccountRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (showLoading = true) => {
    if (showLoading) setLoading(true);
    setError(null);
    try {
      const [d, accs] = await Promise.all([getMeDashboard(), getMeAccounts()]);
      setData(d);
      setAccounts(Array.isArray(accs) ? accs : []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      if (showLoading) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(true);
    const timer = setInterval(() => void load(false), REFRESH_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [load]);

  if (loading) {
    return (
      <div className="p-6 flex items-center justify-center min-h-64">
        <span className="text-muted animate-pulse">加载中…</span>
      </div>
    );
  }
  if (error) {
    return (
      <div className="p-6">
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      </div>
    );
  }
  if (!data) return null;

  return (
    <div className="p-4 md:p-6 space-y-6">
      <h1 className="text-2xl font-semibold text-ink">看板</h1>

      {/* Stat cards */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">总览</h2>
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
          <StatCard
            label="号库"
            value={`${data.accounts.active} / ${data.accounts.total}`}
            sub="活跃 / 总数"
            accent
          />
          <StatCard label="今日请求" value={data.today.requests.toLocaleString()} />
          <StatCard label="今日消费" value={fmtCost(data.today.costUsd)} />
          <StatCard label="累计消耗" value={fmtCost(data.consumptionUsd)} />
          <StatCard label="累计托管费" value={fmtCost(data.accumulatedUsd)} />
          <StatCard label="未结算托管费" value={fmtCost(data.unsettledUsd)} warn={data.unsettledUsd > 0} />
        </div>
      </section>

      {/* Account list (read-only) */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">号库列表</h2>
        <div className="bg-surface border border-line rounded-xl overflow-x-auto">
          {accounts.length === 0 ? (
            <p className="p-6 text-center text-sm text-muted">暂无账户</p>
          ) : (
            <table className="w-full text-left min-w-[640px]">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="py-2 pr-3 pl-3 font-medium">邮箱</th>
                  <th className="py-2 pr-3 font-medium">节点</th>
                  <th className="py-2 pr-3 font-medium">订阅类型</th>
                  <th className="py-2 pr-3 font-medium">状态</th>
                  <th className="py-2 pr-3 font-medium text-right">今日消费</th>
                  <th className="py-2 pr-3 font-medium text-right">总消费</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map((a) => (
                  <tr key={a.accountId} className="border-t border-line hover:bg-line/20 transition">
                    <td className="py-2 pr-3 pl-3 text-sm text-ink truncate max-w-[200px]">{a.email || '—'}</td>
                    <td className="py-2 pr-3 text-xs text-muted">{a.nodeName || '—'}</td>
                    <td className="py-2 pr-3 text-xs text-muted">{a.subscriptionType || '—'}</td>
                    <td className="py-2 pr-3">
                      <span className={`inline-flex items-center gap-1 text-xs ${a.enabled ? 'text-ok' : 'text-muted'}`}>
                        <span className={`w-1.5 h-1.5 rounded-full ${a.enabled ? 'bg-ok' : 'bg-muted'}`} />
                        {a.enabled ? '启用' : '暂停'}
                      </span>
                    </td>
                    <td className="py-2 pr-3 text-xs text-muted text-right tabular-nums">{fmtCost(a.todayCostUsd)}</td>
                    <td className="py-2 pr-3 text-xs text-muted text-right tabular-nums">{fmtCost(a.totalCostUsd)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
    </div>
  );
}

// ============================================================
// TenantAccounts (/accounts) — read-only + pause/resume only
// ============================================================
function daysRemaining(ms: number | undefined): number | null {
  if (!ms) return null;
  return Math.floor((ms - Date.now()) / 86400000);
}

function TenantAccountRow({ account, onChanged }: { account: MeAccountRow; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const days = daysRemaining(account.expiresAt);

  async function toggle() {
    setBusy(true);
    try {
      await pauseMeAccount(account.accountId, !account.enabled);
      onChanged();
    } catch {
      setBusy(false);
    }
  }

  return (
    <tr className="border-t border-line hover:bg-line/30 transition">
      <td className="px-4 py-3">
        <p className="text-xs text-ink">{account.email || '—'}</p>
        <p className="text-[10px] text-muted font-mono mt-0.5">{account.profileId || '—'}</p>
      </td>
      <td className="px-4 py-3 text-sm text-ink">{account.nodeName || '—'}</td>
      <td className="px-4 py-3 text-xs text-muted">{account.subscriptionType || '—'}</td>
      <td className="px-4 py-3">
        <span className={`text-xs font-mono ${days !== null && days < 7 ? 'text-red-400' : 'text-muted'}`}>
          {fmtDate(account.expiresAt)}
          {days !== null && <span className="ml-1">({days < 0 ? `过期${Math.abs(days)}天` : `${days}天`})</span>}
        </span>
      </td>
      <td className="px-4 py-3 text-sm text-muted">{account.weight}</td>
      <td className="px-4 py-3 text-xs text-muted">{account.role || '—'}</td>
      <td className="px-4 py-3 text-xs text-muted text-right tabular-nums">{fmtCost(account.todayCostUsd)}</td>
      <td className="px-4 py-3 text-xs text-muted text-right tabular-nums">{fmtCost(account.totalCostUsd)}</td>
      <td className="px-4 py-3">
        <span className={`flex items-center gap-1 text-xs ${account.enabled ? 'text-ok' : 'text-muted'}`}>
          <span className={`w-1.5 h-1.5 rounded-full ${account.enabled ? 'bg-ok' : 'bg-muted'}`} />
          {account.enabled ? '启用' : '暂停'}
        </span>
      </td>
      <td className="px-4 py-3">
        <button
          onClick={() => { void toggle(); }}
          disabled={busy}
          className={`text-xs transition disabled:opacity-50 ${
            account.enabled ? 'text-yellow-500 hover:text-yellow-400' : 'text-ok hover:text-ok/70'
          }`}
        >
          {busy ? '…' : account.enabled ? '暂停' : '启用'}
        </button>
      </td>
    </tr>
  );
}

function TenantAccountCard({ account, onChanged }: { account: MeAccountRow; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const days = daysRemaining(account.expiresAt);

  async function toggle() {
    setBusy(true);
    try {
      await pauseMeAccount(account.accountId, !account.enabled);
      onChanged();
    } catch {
      setBusy(false);
    }
  }

  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-ink truncate">{account.email || '—'}</p>
          <p className="text-xs text-muted mt-0.5 truncate">{account.nodeName || '—'}</p>
        </div>
        <button
          onClick={() => { void toggle(); }}
          disabled={busy}
          className={`text-xs transition disabled:opacity-50 shrink-0 ${
            account.enabled ? 'text-yellow-500 hover:text-yellow-400' : 'text-ok hover:text-ok/70'
          }`}
        >
          {busy ? '…' : account.enabled ? '暂停' : '启用'}
        </button>
      </div>
      <div className="flex flex-wrap items-center gap-3 text-xs text-muted">
        <span className={`flex items-center gap-1 ${account.enabled ? 'text-ok' : 'text-muted'}`}>
          <span className={`w-1.5 h-1.5 rounded-full ${account.enabled ? 'bg-ok' : 'bg-muted'}`} />
          {account.enabled ? '启用' : '暂停'}
        </span>
        {account.subscriptionType && <span>{account.subscriptionType}</span>}
        <span>权重 {account.weight}</span>
        {account.role && <span>角色 {account.role}</span>}
        <span>到期 {fmtDate(account.expiresAt)}{days !== null ? `(${days < 0 ? `过期${Math.abs(days)}天` : `${days}天`})` : ''}</span>
        <span>今日 {fmtCost(account.todayCostUsd)}</span>
        <span>总计 {fmtCost(account.totalCostUsd)}</span>
      </div>
    </div>
  );
}

export function TenantAccounts() {
  const [accounts, setAccounts] = useState<MeAccountRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const accs = await getMeAccounts();
      setAccounts(Array.isArray(accs) ? accs : []);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  return (
    <div className="p-4 md:p-6 space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">号库</h1>
          <p className="text-xs text-muted mt-1">我的 Claude 账户（只读，可暂停/启用）</p>
        </div>
        <button
          onClick={() => { void load(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {loading && (
        <div className="flex items-center justify-center min-h-32">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}
      {!loading && !error && accounts.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">暂无账户</div>
      )}
      {!loading && !error && accounts.length > 0 && (
        <>
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-x-auto">
            <table className="w-full text-left min-w-[820px]">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-4 py-3 font-medium">邮箱</th>
                  <th className="px-4 py-3 font-medium">节点</th>
                  <th className="px-4 py-3 font-medium">订阅类型</th>
                  <th className="px-4 py-3 font-medium">订阅到期</th>
                  <th className="px-4 py-3 font-medium">权重</th>
                  <th className="px-4 py-3 font-medium">角色</th>
                  <th className="px-4 py-3 font-medium text-right">今日消费</th>
                  <th className="px-4 py-3 font-medium text-right">总消费</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map((a) => (
                  <TenantAccountRow key={a.accountId} account={a} onChanged={() => { void load(); }} />
                ))}
              </tbody>
            </table>
          </div>
          <div className="md:hidden space-y-3">
            {accounts.map((a) => (
              <TenantAccountCard key={a.accountId} account={a} onChanged={() => { void load(); }} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}

// ============================================================
// TenantBilling (/billing) — own ledger + hosting summary
// ============================================================
export function TenantBilling() {
  const [dash, setDash] = useState<MeDashboard | null>(null);
  const [ledger, setLedger] = useState<LedgerEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [d, entries] = await Promise.all([getMeDashboard(), getMeLedger()]);
      setDash(d);
      setLedger(Array.isArray(entries) ? entries : []);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  return (
    <div className="p-4 md:p-6 space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">计费</h1>
          <p className="text-xs text-muted mt-1">我的托管费用与账本</p>
        </div>
        <button
          onClick={() => { void load(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {loading && (
        <div className="flex items-center justify-center min-h-32">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}

      {!loading && !error && dash && (
        <>
          {/* Hosting fee summary */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <StatCard label="累计消耗" value={fmtCost(dash.consumptionUsd)} />
            <StatCard label="托管费率" value={`${(dash.hostingRate * 100).toFixed(1)}%`} />
            <StatCard label="累计托管费" value={fmtCost(dash.accumulatedUsd)} accent />
            <StatCard label="未结算托管费" value={fmtCost(dash.unsettledUsd)} warn={dash.unsettledUsd > 0} />
          </div>

          {/* Ledger */}
          <div className="space-y-3">
            <h2 className="text-sm font-semibold text-ink">账本</h2>
            {ledger.length === 0 ? (
              <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
                暂无账本记录
              </div>
            ) : (
              <>
                <div className="hidden md:block bg-surface border border-line rounded-xl overflow-x-auto">
                  <table className="w-full text-left min-w-[520px]">
                    <thead>
                      <tr className="text-xs text-muted uppercase tracking-wide">
                        <th className="px-3 py-3 font-medium">时间</th>
                        <th className="px-3 py-3 font-medium">类型</th>
                        <th className="px-3 py-3 font-medium">金额 (USD)</th>
                        <th className="px-3 py-3 font-medium">引用</th>
                        <th className="px-3 py-3 font-medium">备注</th>
                      </tr>
                    </thead>
                    <tbody>
                      {ledger.map((row, i) => {
                        const isDebit = row.amount < 0;
                        return (
                          <tr key={i} className="border-t border-line hover:bg-line/20 transition text-sm">
                            <td className="px-3 py-2 text-xs text-muted whitespace-nowrap font-mono">{fmtTime(row.ts)}</td>
                            <td className="px-3 py-2 text-ink">{row.type || '—'}</td>
                            <td className={`px-3 py-2 font-mono text-xs font-medium ${isDebit ? 'text-err' : 'text-ok'}`}>
                              {isDebit ? '' : '+'}{fmtUsd(row.amount)}
                            </td>
                            <td className="px-3 py-2 text-xs text-muted font-mono truncate max-w-[100px]" title={row.ref}>{row.ref || '—'}</td>
                            <td className="px-3 py-2 text-xs text-muted truncate max-w-[160px]" title={row.note}>{row.note || '—'}</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                <div className="md:hidden space-y-3">
                  {ledger.map((row, i) => {
                    const isDebit = row.amount < 0;
                    return (
                      <div key={i} className="bg-surface border border-line rounded-xl p-4 space-y-1.5 text-sm">
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-ink font-medium">{row.type || '—'}</span>
                          <span className={`font-mono text-sm font-semibold ${isDebit ? 'text-err' : 'text-ok'}`}>
                            {isDebit ? '' : '+'}{fmtUsd(row.amount)}
                          </span>
                        </div>
                        <p className="text-xs text-muted">{fmtTime(row.ts)}</p>
                        {row.note && <p className="text-xs text-muted truncate">{row.note}</p>}
                      </div>
                    );
                  })}
                </div>
                <p className="text-xs text-muted text-right">{ledger.length} 条记录</p>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}
