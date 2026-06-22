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
  getMeDispatchStatus,
  getMeSlots,
  createMeSlot,
  deleteMeSlot,
  setMeSlotEnabled,
  getMeDispatchKeys,
  createMeDispatchKey,
  deleteMeDispatchKey,
  getMeBanAnalysis,
} from '../api';
import type {
  MeAccountRow,
  MeDashboard,
  LedgerEntry,
  DispatchStatus,
  DispatchFallbackChannel,
  Slot,
  DispatchKeyRecord,
} from '../types';
import type { BanAnalysis, BanBucket } from '../api';
import { EventTimeline } from './Dispatch';
import { copyText } from '../lib/clipboard';

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

// ============================================================
// TenantDispatch (/dispatch) — own dispatch overview via /api/me
// ============================================================

function TenantFallbackChannelsPanel({ channels }: { channels: DispatchFallbackChannel[] }) {
  function formatBalance(usd: number | undefined): string {
    if (usd == null || usd === 0) return '—';
    if (usd >= 100) return `$${usd.toFixed(0)}`;
    if (usd >= 1) return `$${usd.toFixed(2)}`;
    if (usd >= 0.01) return `$${usd.toFixed(4)}`;
    return `$${usd.toFixed(6)}`;
  }

  function formatCost(usd: number): string {
    if (usd >= 1) return `$${usd.toFixed(2)}`;
    if (usd >= 0.01) return `$${usd.toFixed(4)}`;
    return `$${usd.toFixed(6)}`;
  }

  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">保底渠道</div>
      {channels.length === 0 ? (
        <p className="px-4 py-6 text-center text-muted text-xs">无保底渠道</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-left text-xs text-muted">
                <th className="px-4 py-2 font-medium">渠道名</th>
                <th className="px-4 py-2 font-medium">状态</th>
                <th className="px-4 py-2 font-medium text-right">优先级</th>
                <th className="px-4 py-2 font-medium text-right">权重</th>
                <th className="px-4 py-2 font-medium text-right">余额($)</th>
                <th className="px-4 py-2 font-medium text-right">今日消费</th>
                <th className="px-4 py-2 font-medium text-right">并发中</th>
                <th className="px-4 py-2 font-medium text-right">可用</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((ch) => (
                <tr key={ch.id} className="border-b border-line/50 hover:bg-line/30 transition">
                  <td className="px-4 py-2">
                    <p className="text-sm text-ink font-medium">{ch.name}</p>
                  </td>
                  <td className="px-4 py-2">
                    <span className={`inline-flex items-center px-2 py-0.5 rounded border text-xs font-mono ${ch.enabled ? 'bg-green-500/20 text-green-400 border-green-500/40' : 'bg-gray-500/10 text-gray-500 border-gray-500/20'}`}>
                      {ch.enabled ? '启用' : '停用'}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.priority}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.weight}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{formatBalance(ch.balanceUsd)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{formatCost(ch.todayCostUsd)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.inflight ?? '—'}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.available ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function dispStatusCls(status: string): string {
  const colorMap: Record<string, string> = {
    active:    'bg-green-500/20 text-green-400 border-green-500/40',
    banned:    'bg-red-500/20 text-red-400 border-red-500/40',
    half_open: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40',
    offline:   'bg-gray-500/20 text-gray-400 border-gray-500/40',
    disabled:  'bg-gray-500/10 text-gray-500 border-gray-500/20',
  };
  return colorMap[status] ?? colorMap['offline'];
}

const DISPATCH_REFRESH_MS = 5_000;

export function TenantDispatch() {
  const [data, setData] = useState<DispatchStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const d = await getMeDispatchStatus();
      setData(d);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    }
  }, []);

  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), DISPATCH_REFRESH_MS);
    return () => clearInterval(t);
  }, [load]);

  const accounts = data?.accounts ?? [];
  const traffic = data?.traffic;
  const inflight = accounts.reduce((s, a) => s + a.inflight, 0);
  const rate = traffic && traffic.total > 0 ? ((traffic.ok / traffic.total) * 100).toFixed(1) : '—';

  return (
    <div className="p-4 md:p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-ink">调度</h1>
          <p className="text-xs text-muted mt-1">我的账户并发与实时流量（每 5 秒刷新）</p>
        </div>
        {error && (
          <span className="text-xs text-yellow-400 bg-yellow-500/10 border border-yellow-500/30 rounded px-2 py-1">
            {error}
          </span>
        )}
      </div>

      {!data && !error && (
        <div className="flex items-center justify-center py-24 text-muted text-sm">加载中…</div>
      )}

      {data && (
        <>
          {/* Stats */}
          <div className="grid grid-cols-2 sm:grid-cols-5 gap-3">
            <StatCard label="总请求" value={traffic ? traffic.total.toLocaleString() : '—'} />
            <StatCard label="RPM" value={traffic?.rpm != null ? traffic.rpm : '—'} />
            <StatCard label="成功" value={traffic ? traffic.ok.toLocaleString() : '—'} />
            <StatCard label="错误" value={traffic ? traffic.error.toLocaleString() : '—'} warn={(traffic?.error ?? 0) > 0} />
            <StatCard label="并发中" value={inflight} sub={`成功率 ${traffic && traffic.total > 0 ? rate + '%' : '—'}`} accent />
          </div>

          <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
            {/* Concurrency */}
            <div className="bg-surface border border-line rounded-xl overflow-hidden">
              <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">并发 / 账户</div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-line text-left text-xs text-muted">
                      <th className="px-4 py-2 font-medium">邮箱</th>
                      <th className="px-4 py-2 font-medium">状态</th>
                      <th className="px-4 py-2 font-medium text-right">并发中</th>
                      <th className="px-4 py-2 font-medium text-right">可用</th>
                      <th className="px-4 py-2 font-medium text-right">今日消费</th>
                      <th className="px-4 py-2 font-medium text-right">总消费</th>
                    </tr>
                  </thead>
                  <tbody>
                    {accounts.length === 0 && (
                      <tr><td colSpan={6} className="px-4 py-6 text-center text-muted text-xs">无数据</td></tr>
                    )}
                    {accounts.map((a) => (
                      <tr key={a.key} className="border-b border-line/50 hover:bg-line/30 transition">
                        <td className="px-4 py-2 text-sm text-ink font-medium truncate max-w-[200px]">{a.label || '—'}</td>
                        <td className="px-4 py-2">
                          <span className={`inline-flex items-center px-2 py-0.5 rounded border text-xs font-mono ${dispStatusCls(a.status)}`}>
                            {a.status}
                          </span>
                        </td>
                        <td className="px-4 py-2 text-right tabular-nums">{a.inflight}</td>
                        <td className="px-4 py-2 text-right tabular-nums">{a.available}</td>
                        <td className="px-4 py-2 text-right tabular-nums text-xs text-muted">{fmtCost(a.todayCostUsd)}</td>
                        <td className="px-4 py-2 text-right tabular-nums text-xs text-muted">{fmtCost(a.totalCostUsd)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>

            {/* Event timeline (reuse shared rendering) */}
            <EventTimeline events={data.events} fallbackNames={new Map()} accountNames={new Map()} />
          </div>

          {/* Fallback channels */}
          <TenantFallbackChannelsPanel channels={data.fallbackChannels ?? []} />
        </>
      )}
    </div>
  );
}

// ============================================================
// TenantSettings (/settings) — slots + dispatch keys + ban analysis
// ============================================================
function minsToHHMM(mins: number): string {
  const h = Math.floor(mins / 60).toString().padStart(2, '0');
  const m = (mins % 60).toString().padStart(2, '0');
  return `${h}:${m}`;
}

function hhmmToMins(hhmm: string): number {
  const [h, m] = hhmm.split(':').map(Number);
  return (h || 0) * 60 + (m || 0);
}

const tenantInputCls =
  'w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink ' +
  'placeholder:text-muted focus:outline-none focus:border-accent transition';

// ---- Slot enable toggle ----
function MeSlotToggle({ slot, onChanged }: { slot: Slot; onChanged: (id: string, enabled: boolean) => void }) {
  const [busy, setBusy] = useState(false);
  async function toggle() {
    setBusy(true);
    try {
      await setMeSlotEnabled(slot.id, !slot.enabled);
      onChanged(slot.id, !slot.enabled);
    } finally {
      setBusy(false);
    }
  }
  return (
    <button
      onClick={() => { void toggle(); }}
      disabled={busy}
      title={slot.enabled ? '点击禁用' : '点击启用'}
      className={[
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition',
        slot.enabled ? 'bg-ok' : 'bg-line',
        busy ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
      ].join(' ')}
    >
      <span className={[
        'inline-block h-3.5 w-3.5 rounded-full bg-white shadow transition-transform',
        slot.enabled ? 'translate-x-4' : 'translate-x-1',
      ].join(' ')} />
    </button>
  );
}

// ---- Slots section ----
function TenantSlotsSection() {
  const [slots, setSlots] = useState<Slot[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [startTime, setStartTime] = useState('00:00');
  const [endTime, setEndTime] = useState('08:00');
  const [creating, setCreating] = useState(false);
  const [createErr, setCreateErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setSlots(await getMeSlots());
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setCreateErr(null);
    try {
      await createMeSlot({ name, startMin: hhmmToMins(startTime), endMin: hhmmToMins(endTime) });
      setName(''); setStartTime('00:00'); setEndTime('08:00');
      await load();
    } catch (e) {
      setCreateErr(e instanceof Error ? e.message : '创建失败');
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteMeSlot(id);
      setSlots((prev) => prev.filter((s) => s.id !== id));
    } catch { /* ignore */ }
  }

  function handleEnabledChanged(id: string, enabled: boolean) {
    setSlots((prev) => prev.map((s) => s.id === id ? { ...s, enabled } : s));
  }

  return (
    <div className="space-y-4">
      {loading && <p className="text-muted text-sm animate-pulse">加载中…</p>}
      {!loading && err && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{err}</div>
      )}
      {!loading && !err && (
        <>
          {slots.length === 0 ? (
            <p className="text-sm text-muted">暂无时段槽位，在下方新建。</p>
          ) : (
            <div className="bg-surface border border-line rounded-xl overflow-hidden">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="text-xs text-muted uppercase tracking-wide border-b border-line bg-bg/50">
                    <th className="px-4 py-3 font-medium">名称</th>
                    <th className="px-4 py-3 font-medium">时间窗</th>
                    <th className="px-4 py-3 font-medium">状态</th>
                    <th className="px-4 py-3 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {slots.map((slot) => (
                    <tr key={slot.id} className="border-t border-line/50 hover:bg-line/30 transition">
                      <td className="px-4 py-3 font-medium text-ink">{slot.name}</td>
                      <td className="px-4 py-3 font-mono text-sm text-muted">
                        {minsToHHMM(slot.startMin)}–{minsToHHMM(slot.endMin)}
                      </td>
                      <td className="px-4 py-3"><MeSlotToggle slot={slot} onChanged={handleEnabledChanged} /></td>
                      <td className="px-4 py-3">
                        <button onClick={() => { void handleDelete(slot.id); }} className="text-xs text-muted hover:text-err transition">删除</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <form onSubmit={(e) => { void handleCreate(e); }} className="bg-surface border border-line rounded-xl p-5 space-y-3">
            <h3 className="text-sm font-semibold text-ink">新建时段槽位</h3>
            {createErr && <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-sm">{createErr}</div>}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div>
                <label className="block text-xs text-muted mb-1">名称 *</label>
                <input required value={name} onChange={(e) => setName(e.target.value)} placeholder="早高峰" autoComplete="off" className={tenantInputCls} />
              </div>
              <div>
                <label className="block text-xs text-muted mb-1">开始时间</label>
                <input type="time" required value={startTime} onChange={(e) => setStartTime(e.target.value)} className={tenantInputCls} />
              </div>
              <div>
                <label className="block text-xs text-muted mb-1">结束时间</label>
                <input type="time" required value={endTime} onChange={(e) => setEndTime(e.target.value)} className={tenantInputCls} />
              </div>
            </div>
            <button type="submit" disabled={creating} className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg hover:bg-accent/80 disabled:opacity-50 transition">
              {creating ? '创建中…' : '创建槽位'}
            </button>
          </form>
        </>
      )}
    </div>
  );
}

// ---- Copy button ----
function CopyBtn({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  async function doCopy() {
    const ok = await copyText(text);
    if (ok) { setCopied(true); setTimeout(() => setCopied(false), 1500); }
  }
  return (
    <button onClick={() => { void doCopy(); }} className="text-xs text-accent hover:text-accent/70 transition shrink-0">
      {copied ? '已复制' : '复制'}
    </button>
  );
}

// ---- Dispatch keys section ----
function TenantKeysSection() {
  const [keys, setKeys] = useState<DispatchKeyRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [label, setLabel] = useState('');
  const [creating, setCreating] = useState(false);
  const [plaintext, setPlaintext] = useState<string | null>(null);

  const gatewayUrl = window.location.origin;

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setKeys(await getMeDispatchKeys());
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    try {
      const created = await createMeDispatchKey(label);
      setPlaintext(created.key);
      setLabel('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '创建失败');
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteMeDispatchKey(id);
      setKeys((prev) => prev.filter((k) => k.id !== id));
    } catch { /* ignore */ }
  }

  return (
    <div className="space-y-4">
      {/* Gateway URL */}
      <div className="bg-surface border border-line rounded-xl p-4 space-y-1">
        <p className="text-xs text-muted">网关地址</p>
        <div className="flex items-center gap-3">
          <code className="text-sm text-ink font-mono truncate">{gatewayUrl}</code>
          <CopyBtn text={gatewayUrl} />
        </div>
      </div>

      {/* Newly created plaintext (shown once) */}
      {plaintext && (
        <div className="bg-accent/10 border border-accent/30 rounded-xl p-4 space-y-2">
          <p className="text-xs text-accent font-medium">新密钥已创建（仅此一次显示，请妥善保存）</p>
          <div className="flex items-center gap-3">
            <code className="text-sm text-ink font-mono break-all">{plaintext}</code>
            <CopyBtn text={plaintext} />
          </div>
          <button onClick={() => setPlaintext(null)} className="text-xs text-muted hover:text-ink transition">关闭</button>
        </div>
      )}

      {loading && <p className="text-muted text-sm animate-pulse">加载中…</p>}
      {!loading && err && <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{err}</div>}
      {!loading && !err && (
        <>
          {keys.length === 0 ? (
            <p className="text-sm text-muted">暂无调度密钥，在下方新建。</p>
          ) : (
            <div className="bg-surface border border-line rounded-xl overflow-hidden">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="text-xs text-muted uppercase tracking-wide border-b border-line bg-bg/50">
                    <th className="px-4 py-3 font-medium">标签</th>
                    <th className="px-4 py-3 font-medium">前缀</th>
                    <th className="px-4 py-3 font-medium">状态</th>
                    <th className="px-4 py-3 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {keys.map((k) => (
                    <tr key={k.id} className="border-t border-line/50 hover:bg-line/30 transition">
                      <td className="px-4 py-3 font-medium text-ink">{k.label || '—'}</td>
                      <td className="px-4 py-3 font-mono text-xs text-muted">{k.prefix}…</td>
                      <td className="px-4 py-3">
                        <span className={`inline-flex items-center gap-1 text-xs ${k.enabled ? 'text-ok' : 'text-muted'}`}>
                          <span className={`w-1.5 h-1.5 rounded-full ${k.enabled ? 'bg-ok' : 'bg-muted'}`} />
                          {k.enabled ? '启用' : '停用'}
                        </span>
                      </td>
                      <td className="px-4 py-3">
                        <button onClick={() => { void handleDelete(k.id); }} className="text-xs text-muted hover:text-err transition">删除</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <form onSubmit={(e) => { void handleCreate(e); }} className="bg-surface border border-line rounded-xl p-5 space-y-3">
            <h3 className="text-sm font-semibold text-ink">新建调度密钥</h3>
            <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-3 items-end">
              <div>
                <label className="block text-xs text-muted mb-1">标签</label>
                <input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="我的网关密钥" autoComplete="off" className={tenantInputCls} />
              </div>
              <button type="submit" disabled={creating} className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg hover:bg-accent/80 disabled:opacity-50 transition">
                {creating ? '创建中…' : '新建'}
              </button>
            </div>
          </form>
        </>
      )}
    </div>
  );
}

// ---- Ban analysis section ----
const TENANT_WEEKDAY_LABELS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];

function TenantBarChart({ data, labelFn, color }: { data: BanBucket[]; labelFn: (b: number) => string; color: string }) {
  const maxCount = data.reduce((m, d) => Math.max(m, d.count), 0);
  if (maxCount === 0) return <p className="text-xs text-muted py-4 text-center">暂无数据</p>;
  return (
    <div className="w-full overflow-x-auto">
      <div className="flex items-end gap-1 min-w-0" style={{ minHeight: '120px' }}>
        {data.map((d) => {
          const pct = (d.count / maxCount) * 100;
          return (
            <div key={d.bucket} className="flex flex-col items-center flex-1 min-w-0 gap-1 group" title={`${labelFn(d.bucket)}: ${d.count}`}>
              <span className="text-[10px] text-muted opacity-0 group-hover:opacity-100 transition whitespace-nowrap">{d.count}</span>
              <div className="w-full flex flex-col justify-end" style={{ height: '96px' }}>
                <div className={`w-full rounded-t ${color} transition-all duration-300`} style={{ height: `${pct}%`, minHeight: d.count > 0 ? '4px' : '0' }} />
              </div>
              <span className="text-[10px] text-muted truncate w-full text-center leading-none">{labelFn(d.bucket)}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function fillWeekday(data: BanBucket[]): BanBucket[] {
  const map = new Map(data.map((d) => [d.bucket, d.count]));
  return Array.from({ length: 7 }, (_, i) => ({ bucket: i, count: map.get(i) ?? 0 }));
}
function fillHour(data: BanBucket[]): BanBucket[] {
  const map = new Map(data.map((d) => [d.bucket, d.count]));
  return Array.from({ length: 24 }, (_, i) => ({ bucket: i, count: map.get(i) ?? 0 }));
}

function TenantBanSection() {
  const [data, setData] = useState<BanAnalysis | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      setData(await getMeBanAnalysis());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void fetchData(); }, [fetchData]);

  const weekdayData = data ? fillWeekday(data.byWeekday) : [];
  const hourData = data ? fillHour(data.byHour) : [];

  return (
    <div className="space-y-4">
      {loading && <p className="text-muted text-sm animate-pulse">加载中…</p>}
      {!loading && error && <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>}
      {!loading && !error && data && data.total === 0 && (
        <div className="bg-surface border border-line rounded-xl p-12 text-center space-y-2">
          <p className="text-4xl">🎉</p>
          <p className="text-ink font-medium">暂无封号记录</p>
          <p className="text-xs text-muted">所有账号运行正常，继续保持！</p>
        </div>
      )}
      {!loading && !error && data && data.total > 0 && (
        <>
          <div className="bg-surface border border-line rounded-xl p-5 flex items-center gap-4">
            <div className="w-12 h-12 rounded-full bg-err/10 flex items-center justify-center text-2xl shrink-0">⚠</div>
            <div>
              <p className="text-xs text-muted uppercase tracking-wide">累计封号次数</p>
              <p className="text-3xl font-bold text-ink tabular-nums">{data.total.toLocaleString()}</p>
            </div>
          </div>
          <div className="bg-surface border border-line rounded-xl p-5 space-y-3">
            <div>
              <h3 className="text-sm font-semibold text-ink">按星期分布</h3>
              <p className="text-xs text-muted">封号集中在哪些星期</p>
            </div>
            <TenantBarChart data={weekdayData} labelFn={(b) => TENANT_WEEKDAY_LABELS[b] ?? String(b)} color="bg-err" />
          </div>
          <div className="bg-surface border border-line rounded-xl p-5 space-y-3">
            <div>
              <h3 className="text-sm font-semibold text-ink">按小时分布</h3>
              <p className="text-xs text-muted">封号集中在哪些时段（0–23 时）</p>
            </div>
            <TenantBarChart data={hourData} labelFn={(b) => String(b).padStart(2, '0')} color="bg-warn" />
          </div>
        </>
      )}
    </div>
  );
}

type SettingsTab = 'slots' | 'keys' | 'ban';

export function TenantSettings() {
  const [tab, setTab] = useState<SettingsTab>('slots');

  const tabs: { id: SettingsTab; label: string }[] = [
    { id: 'slots', label: '时段槽位' },
    { id: 'keys', label: '调度密钥' },
    { id: 'ban', label: '封号分析' },
  ];

  return (
    <div className="p-4 md:p-6 space-y-6 max-w-4xl mx-auto">
      <div>
        <h1 className="text-2xl font-semibold text-ink">设置</h1>
        <p className="text-xs text-muted mt-1">管理我的时段槽位、调度密钥与封号分析</p>
      </div>

      <div className="flex gap-2 border-b border-line">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={[
              'px-4 py-2 text-sm font-medium transition border-b-2 -mb-px',
              tab === t.id ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-ink',
            ].join(' ')}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'slots' && <TenantSlotsSection />}
      {tab === 'keys' && <TenantKeysSection />}
      {tab === 'ban' && <TenantBanSection />}
    </div>
  );
}
