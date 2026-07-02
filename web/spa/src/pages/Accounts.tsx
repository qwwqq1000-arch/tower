// ============================================================
// Tower SPA — Accounts page (号库)
// Account list + management (OAuth wizard moved to Nodes page)
// ============================================================
import { useEffect, useState, useCallback, useMemo } from 'react';
import {
  listAccounts,
  unassignAccount,
  updateNodeAccount,
  getNodeQuota,
  listSlots,
  setAccountExpiry,
  setAccountOwner,
  recoverAccount,
  clearNo1M,
  refreshAllQuota,
  refreshAccountQuota,
  testAccount,
  listUsers,
} from '../api';
import type { AccountRow, CpaQuota, QuotaAll, Slot, UserRow } from '../types';
import { useAuth } from '../auth';
import { statusColor, statusLabel } from '../lib/status';
import { statusRank } from '../components/AccountStatus';
import { TenantAccounts } from './tenant';

// ------------------------------------------------------------------
// Small toast helper
// ------------------------------------------------------------------
function Toast({ msg, onClose }: { msg: string; onClose: () => void }) {
  useEffect(() => {
    const t = setTimeout(onClose, 4000);
    return () => clearTimeout(t);
  }, [onClose]);
  return (
    <div className="fixed bottom-4 right-4 z-50 bg-ok text-white text-sm px-4 py-2.5 rounded-xl shadow-lg flex items-center gap-3">
      <span>{msg}</span>
      <button onClick={onClose} className="text-white/70 hover:text-white text-lg leading-none">×</button>
    </div>
  );
}

// ------------------------------------------------------------------
// Cost formatter
// ------------------------------------------------------------------
function fmtCost(n: number | undefined): string {
  if (n == null) return '—';
  if (n === 0) return '$0.0000';
  return n < 0.01 ? `$${n.toFixed(4)}` : `$${n.toFixed(2)}`;
}

// ------------------------------------------------------------------
// Date helpers
// ------------------------------------------------------------------
function fmtDate(ms: number | undefined): string {
  if (!ms) return '—';
  const d = new Date(ms);
  return d.toISOString().slice(0, 10); // YYYY-MM-DD
}

function daysRemaining(ms: number | undefined): number | null {
  if (!ms) return null;
  return Math.floor((ms - Date.now()) / 86400000);
}

/** Format remaining ms as a countdown: "已到" / "Xd Yh" / "H:MM:SS" / "MM:SS". */
function fmtCountdown(remainMs: number): string {
  if (remainMs <= 0) return '已到';
  const s = Math.floor(remainMs / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}天${h}时`;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`;
  return `${m}:${String(sec).padStart(2, '0')}`;
}

// LimitedBadge shows a quota-limited account as a live RECOVERY COUNTDOWN instead of a
// wall-clock reset (which was UTC and confusing). The deadline is an absolute ms
// timestamp, so the countdown is timezone-agnostic (quota-3 / 限额恢复倒计时).
function LimitedBadge({ until }: { until?: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!until || until <= 0) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [until]);
  const cd = until && until > 0 ? fmtCountdown(until - now) : null;
  return (
    <span className={`inline-flex items-center mt-1 px-1.5 py-0.5 rounded border text-[10px] font-mono ${statusColor('limited')}`}>
      限额{cd ? `(${cd})` : '(配额)'}
    </span>
  );
}

// No1MBadge: shows "不支持1M" for accounts where no_1m_until > now
function No1MBadge() {
  return (
    <span className="inline-flex items-center mt-1 px-1.5 py-0.5 rounded border text-[10px] font-mono bg-amber-500/20 text-amber-400 border-amber-500/40">
      不支持1M
    </span>
  );
}

// ------------------------------------------------------------------
// Quota badge helper
// ------------------------------------------------------------------
function QuotaBadge({
  utilization,
  label,
  resetsAt,
}: {
  utilization: number;
  label: string;
  resetsAt?: number;
}) {
  // Tick once a second so the reset shows a live countdown.
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!resetsAt || resetsAt <= 0) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [resetsAt]);
  const pct = Math.round(utilization * 100);
  let cls = 'bg-green-500/20 text-green-400 border-green-500/40';
  if (utilization >= 0.9) cls = 'bg-red-500/20 text-red-400 border-red-500/40';
  else if (utilization >= 0.7) cls = 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40';
  const countdown = resetsAt && resetsAt > 0 ? fmtCountdown(resetsAt - now) : null;
  return (
    <span className="inline-flex flex-col items-start gap-0">
      <span className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[10px] font-mono ${cls}`}>
        {label} {pct}%
      </span>
      {countdown && (
        <span className="text-[9px] text-muted pl-0.5">重置 {countdown}</span>
      )}
    </span>
  );
}

function QuotaCell({
  nodeId,
  profileId,
  quotaMap,
}: {
  nodeId: string;
  profileId: string;
  quotaMap: Map<string, QuotaAll>;
}) {
  const quota = quotaMap.get(nodeId);
  // quota.profiles can be null/absent (e.g. a node whose quota was never polled or
  // returned empty) — guard before .find() or the whole 号库 render crashes.
  if (!quota || !quota.profiles) return <span className="text-xs text-muted">—</span>;

  const profile = quota.profiles.find((p) => p.id === profileId);
  if (!profile || !profile.windows || profile.windows.length === 0) {
    return <span className="text-xs text-muted">—</span>;
  }

  const w5h = profile.windows.find((w) => w.type === 'five_hour');
  const w7d = profile.windows.find((w) => w.type === 'seven_day');

  if (!w5h && !w7d) return <span className="text-xs text-muted">—</span>;

  return (
    <div className="flex items-start gap-2 flex-wrap">
      {w5h && (
        <QuotaBadge utilization={w5h.utilization} label="5h" resetsAt={w5h.resetsAt} />
      )}
      {w7d && (
        <QuotaBadge utilization={w7d.utilization} label="7d" resetsAt={w7d.resetsAt} />
      )}
    </div>
  );
}

// CPA account quota cell — utilization is 0–100, resets_at is an ISO string.
export function CpaQuotaCell({ q }: { q: CpaQuota }) {
  const toMs = (s: string): number | undefined => {
    if (!s) return undefined;
    const t = Date.parse(s);
    return isNaN(t) ? undefined : t;
  };
  return (
    <div className="flex items-start gap-2 flex-wrap">
      <QuotaBadge utilization={(q.fiveHourUtil ?? 0) / 100} label="5h" resetsAt={toMs(q.fiveHourResetsAt)} />
      <QuotaBadge utilization={(q.sevenDayUtil ?? 0) / 100} label="7d" resetsAt={toMs(q.sevenDayResetsAt)} />
      <QuotaBadge utilization={(q.sevenDaySonnetUtil ?? 0) / 100} label="7dS" resetsAt={toMs(q.sevenDaySonnetResetsAt)} />
    </div>
  );
}

// ------------------------------------------------------------------
// Expiry display cell
// ------------------------------------------------------------------
function ExpiryCell({ expiresAt }: { expiresAt?: number }) {
  if (!expiresAt) return <span className="text-xs text-muted">—</span>;
  const days = daysRemaining(expiresAt);
  const expired = days !== null && days < 0;
  const urgent = days !== null && days < 7 && !expired;
  const cls = expired || urgent ? 'text-red-400' : 'text-muted';
  return (
    <div className="flex flex-col gap-0.5">
      <span className={`text-xs font-mono ${cls}`}>{fmtDate(expiresAt)}</span>
      {days !== null && (
        <span className={`text-[10px] ${cls}`}>
          {expired ? `已过期 ${Math.abs(days)}天` : `剩余${days}天`}
        </span>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// Edit modal for per-account tuning
// ------------------------------------------------------------------
function AccountEditModal({
  account,
  users,
  onSave,
  onClose,
}: {
  account: AccountRow;
  users: UserRow[];
  onSave: () => void;
  onClose: () => void;
}) {
  const [weight, setWeight] = useState(String(account.weight));
  const [role, setRole] = useState(account.role || 'baseline');
  const [egress, setEgress] = useState(account.egress || '');
  const [enabled, setEnabled] = useState(account.enabled);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [slotId, setSlotId] = useState(account.slotId ?? '');
  const [slots, setSlots] = useState<Slot[]>([]);

  // Expiry state
  const [expiryMode, setExpiryMode] = useState<'none' | 'custom'>('none');
  const [expiryDate, setExpiryDate] = useState(() => {
    if (!account.expiresAt) return '';
    return new Date(account.expiresAt).toISOString().slice(0, 10);
  });

  // Owner/tenant state
  const [ownerId, setOwnerId] = useState(account.ownerId ?? '');

  useEffect(() => {
    listSlots()
      .then(setSlots)
      .catch(() => { /* ignore */ });
  }, []);

  function applyQuickExpiry(days: number) {
    const d = new Date();
    d.setDate(d.getDate() + days);
    setExpiryDate(d.toISOString().slice(0, 10));
    setExpiryMode('custom');
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setErr(null);
    try {
      // Core account settings
      await updateNodeAccount(account.nodeId, account.accountId, {
        weight: Number(weight),
        role,
        egress,
        enabled,
        slotId: slotId || undefined,
      });

      // Expiry update if changed
      if (expiryMode === 'custom' && expiryDate) {
        const ms = new Date(expiryDate).getTime();
        if (!isNaN(ms)) {
          await setAccountExpiry(account.accountId, ms);
        }
      }

      // Owner update if changed
      if (ownerId !== (account.ownerId ?? '')) {
        await setAccountOwner(account.accountId, ownerId);
      }

      onSave();
      onClose();
    } catch (saveErr) {
      setErr(saveErr instanceof Error ? saveErr.message : '保存失败');
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <div className="bg-surface border border-line rounded-xl shadow-xl w-full max-w-md p-6 space-y-4 max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold text-ink">编辑账户</h2>
          <button onClick={onClose} className="text-muted hover:text-ink text-lg leading-none">×</button>
        </div>
        <p className="text-xs text-muted truncate">{account.email || '—'}</p>
        <form onSubmit={(e) => { void handleSave(e); }} className="space-y-3">
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted font-medium">权重</label>
            <input
              type="number"
              value={weight}
              onChange={(e) => setWeight(e.target.value)}
              min={0}
              className="bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                         focus:outline-none focus:border-accent transition"
            />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted font-medium">角色</label>
            <select
              value={role}
              onChange={(e) => setRole(e.target.value)}
              className="bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                         focus:outline-none focus:border-accent transition"
            >
              <option value="baseline">baseline</option>
              <option value="reserve">reserve</option>
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted font-medium">出口 IP (egress)</label>
            <input
              type="text"
              value={egress}
              onChange={(e) => setEgress(e.target.value)}
              placeholder="留空表示默认出口"
              className="bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                         placeholder:text-muted focus:outline-none focus:border-accent transition"
            />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted font-medium">槽位</label>
            <select
              value={slotId}
              onChange={(e) => setSlotId(e.target.value)}
              className="bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                         focus:outline-none focus:border-accent transition"
            >
              <option value="">不限</option>
              {slots.map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-3">
            <label className="text-xs text-muted font-medium">启用</label>
            <button
              type="button"
              onClick={() => setEnabled((v) => !v)}
              className={`relative inline-flex h-5 w-9 items-center rounded-full transition
                ${enabled ? 'bg-accent' : 'bg-line'}`}
            >
              <span
                className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition
                  ${enabled ? 'translate-x-4' : 'translate-x-1'}`}
              />
            </button>
            <span className={`text-xs ${enabled ? 'text-ok' : 'text-muted'}`}>
              {enabled ? '启用' : '禁用'}
            </span>
          </div>

          {/* Subscription expiry */}
          <div className="flex flex-col gap-1.5 border-t border-line pt-3">
            <label className="text-xs text-muted font-medium">订阅到期</label>
            <div className="text-xs text-muted">
              当前: <span className="text-ink font-mono">{fmtDate(account.expiresAt)}</span>
              {account.expiresAt && (() => {
                const d = daysRemaining(account.expiresAt);
                if (d === null) return null;
                const cls = d < 7 ? 'text-red-400' : 'text-muted';
                return <span className={`ml-1.5 ${cls}`}>({d < 0 ? `已过期${Math.abs(d)}天` : `剩余${d}天`})</span>;
              })()}
            </div>
            <div className="flex items-center gap-2 flex-wrap">
              <button
                type="button"
                onClick={() => applyQuickExpiry(30)}
                className="text-xs px-2.5 py-1 bg-accent/10 text-accent border border-accent/30 rounded-lg hover:bg-accent/20 transition"
              >
                +30天
              </button>
              <button
                type="button"
                onClick={() => applyQuickExpiry(90)}
                className="text-xs px-2.5 py-1 bg-accent/10 text-accent border border-accent/30 rounded-lg hover:bg-accent/20 transition"
              >
                +90天
              </button>
              <button
                type="button"
                onClick={() => setExpiryMode('custom')}
                className="text-xs px-2.5 py-1 bg-bg border border-line text-muted rounded-lg hover:text-ink transition"
              >
                自定义
              </button>
            </div>
            {expiryMode === 'custom' && (
              <input
                type="date"
                value={expiryDate}
                onChange={(e) => setExpiryDate(e.target.value)}
                className="bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                           focus:outline-none focus:border-accent transition"
              />
            )}
          </div>

          {/* Tenant / owner assignment */}
          <div className="flex flex-col gap-1.5 border-t border-line pt-3">
            <label className="text-xs text-muted font-medium">租户分配</label>
            <select
              value={ownerId}
              onChange={(e) => setOwnerId(e.target.value)}
              className="bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                         focus:outline-none focus:border-accent transition"
            >
              <option value="">超级管理员（默认）</option>
              {users.filter((u) => u.role !== 'superadmin' && u.role !== 'admin').map((u) => (
                <option key={u.id} value={u.id}>{u.username} ({u.role})</option>
              ))}
            </select>
          </div>

          {err && <p className="text-xs text-err">{err}</p>}
          <div className="flex gap-2 pt-1">
            <button
              type="submit"
              disabled={saving}
              className="flex-1 px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                         hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
            >
              {saving ? '保存中…' : '保存'}
            </button>
            <button
              type="button"
              onClick={onClose}
              disabled={saving}
              className="flex-1 px-4 py-2 text-sm font-medium bg-bg border border-line text-muted rounded-lg
                         hover:text-ink disabled:opacity-50 transition"
            >
              取消
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Model test modal
// ------------------------------------------------------------------
const TEST_MODELS = [
  'claude-fable-5',
  'claude-sonnet-5',
  'claude-opus-4-8',
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5-20251001',
];

function TestModal({
  account,
  onClose,
}: {
  account: AccountRow;
  onClose: () => void;
}) {
  const [model, setModel] = useState(TEST_MODELS[0]);
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<{ status: number; body: Record<string, unknown> } | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleTest() {
    setTesting(true);
    setResult(null);
    setError(null);
    try {
      const r = await testAccount(account.accountId, model);
      setResult({ status: r.status, body: r.body });
    } catch (e) {
      setError(e instanceof Error ? e.message : '测试失败');
    } finally {
      setTesting(false);
    }
  }

  const ok = result && result.status >= 200 && result.status < 300;
  const text = result?.body?.content
    ? String((result.body.content as Array<{ text?: string }>)?.[0]?.text ?? '')
    : null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <div className="bg-surface border border-line rounded-xl shadow-xl w-full max-w-md p-6 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold text-ink">模型测试</h2>
          <button onClick={onClose} className="text-muted hover:text-ink text-lg leading-none">×</button>
        </div>
        <p className="text-xs text-muted truncate">{account.email || account.profileId}</p>

        <div className="flex gap-2">
          <select
            value={model}
            onChange={(e) => setModel(e.target.value)}
            className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                       focus:outline-none focus:border-accent transition"
          >
            {TEST_MODELS.map((m) => (
              <option key={m} value={m}>{m}</option>
            ))}
          </select>
          <button
            onClick={() => { void handleTest(); }}
            disabled={testing}
            className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                       hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {testing ? '测试中…' : '测试'}
          </button>
        </div>

        {error && (
          <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-xs">{error}</div>
        )}

        {result && (
          <div className={`rounded-lg p-3 text-xs space-y-1 border ${ok ? 'bg-green-500/10 border-green-500/30' : 'bg-red-500/10 border-red-500/30'}`}>
            <div className="flex items-center gap-2">
              <span className={`font-semibold ${ok ? 'text-green-400' : 'text-red-400'}`}>
                {ok ? '✓ 成功' : '✗ 失败'} ({result.status})
              </span>
              {result.body?.model != null && <span className="text-muted font-mono">{String(result.body.model)}</span>}
            </div>
            {text && <p className="text-ink">{text}</p>}
            {!ok && (
              <pre className="text-muted whitespace-pre-wrap break-all mt-1 max-h-32 overflow-auto">
                {JSON.stringify(result.body, null, 2)}
              </pre>
            )}
          </div>
        )}

        <div className="flex justify-end">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-muted bg-bg border border-line rounded-lg hover:text-ink transition"
          >
            关闭
          </button>
        </div>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Sort key type
// ------------------------------------------------------------------
type SortKey = 'nodeName' | 'email' | 'subscriptionType' | 'weight' | 'role' | 'expiresAt' | 'ownerId' | 'todayCostUsd' | 'totalCostUsd' | null;

// ------------------------------------------------------------------
// Account row (desktop table)
// ------------------------------------------------------------------
function AccountTableRow({
  account,
  quotaMap,
  users,
  onUnassign,
  onRefresh,
  onToast,
}: {
  account: AccountRow;
  quotaMap: Map<string, QuotaAll>;
  users: UserRow[];
  onUnassign: (nodeId: string, accountId: string) => void;
  onRefresh: () => void;
  onToast?: (msg: string) => void;
}) {
  const [removing, setRemoving] = useState(false);
  const [editing, setEditing] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [showTest, setShowTest] = useState(false);

  const ownerName = useMemo(() => {
    if (!account.ownerId) return '超级管理员';
    const u = users.find((u) => u.id === account.ownerId);
    return u ? u.username : account.ownerId;
  }, [account.ownerId, users]);

  async function handleUnassign() {
    if (!confirm(`确认解绑账户 ${account.accountId} 与节点 ${account.nodeName}？`)) return;
    setRemoving(true);
    try {
      await unassignAccount(account.nodeId, account.accountId);
      onUnassign(account.nodeId, account.accountId);
    } catch {
      setRemoving(false);
    }
  }

  async function handleToggleEnabled() {
    setToggling(true);
    try {
      await updateNodeAccount(account.nodeId, account.accountId, {
        egress: account.egress,
        weight: account.weight,
        role: account.role,
        enabled: !account.enabled,
      });
      onRefresh();
    } catch {
      setToggling(false);
    }
  }

  const [recovering, setRecovering] = useState(false);
  const banned = account.status === 'permanent' || account.status === 'banned' || account.status === 'half_open';
  async function handleRecover() {
    setRecovering(true);
    try {
      await recoverAccount(account.accountId);
      onRefresh();
    } finally {
      setRecovering(false);
    }
  }

  const no1mActive = !!account.no1mUntil && account.no1mUntil > Date.now();
  const [clearingNo1M, setClearingNo1M] = useState(false);
  async function handleClearNo1M() {
    setClearingNo1M(true);
    try {
      await clearNo1M(account.accountId);
      onRefresh();
    } finally {
      setClearingNo1M(false);
    }
  }

  const [refreshingQuota, setRefreshingQuota] = useState(false);
  async function handleRefreshQuota() {
    setRefreshingQuota(true);
    try {
      await refreshAccountQuota(account.accountId);
      onRefresh();
      onToast?.(`已刷新额度 (${account.email || account.profileId})`);
    } catch (e) {
      onToast?.(`刷新失败: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setRefreshingQuota(false);
    }
  }

  return (
    <>
      <tr className="border-t border-line hover:bg-line/30 transition">
        <td className="px-4 py-3 text-sm text-ink font-medium">{account.nodeName}</td>
        <td className="px-4 py-3">
          <p className="text-xs text-ink">{account.email || '—'}</p>
          <p className="text-[10px] text-muted font-mono mt-0.5">{account.profileId || '—'}</p>
          {account.status === 'limited' ? (
            <LimitedBadge until={account.limitedUntil} />
          ) : account.status && (
            <span className={`inline-flex items-center mt-1 px-1.5 py-0.5 rounded border text-[10px] font-mono ${statusColor(account.status)}`}>
              {statusLabel(account.status)}
            </span>
          )}
          {no1mActive && <No1MBadge />}
        </td>
        <td className="px-4 py-3 text-xs text-muted">{account.subscriptionType || '—'}</td>
        <td className="px-4 py-3">
          {account.cpaQuota ? <CpaQuotaCell q={account.cpaQuota} /> : <QuotaCell nodeId={account.nodeId} profileId={account.profileId} quotaMap={quotaMap} />}
        </td>
        <td className="px-4 py-3 text-sm text-muted">{account.weight}</td>
        <td className="px-4 py-3 text-xs text-muted">{account.role || '—'}</td>
        <td className="px-4 py-3">
          <ExpiryCell expiresAt={account.expiresAt} />
        </td>
        <td className="px-4 py-3 text-xs text-muted">{ownerName}</td>
        <td className="px-4 py-3 text-xs text-muted text-right tabular-nums">{fmtCost(account.todayCostUsd)}</td>
        <td className="px-4 py-3 text-xs text-muted text-right tabular-nums">{fmtCost(account.totalCostUsd)}</td>
        <td className="px-4 py-3">
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowTest(true)}
              className="text-xs text-cyan-400 hover:text-cyan-300 transition"
              title="测试该号是否支持某模型"
            >
              测试
            </button>
            <button
              onClick={() => { void handleRefreshQuota(); }}
              disabled={refreshingQuota}
              className="text-xs text-accent hover:text-accent/70 disabled:opacity-50 transition"
              title="刷新该号额度"
            >
              {refreshingQuota ? '刷新中…' : '刷新'}
            </button>
            <button
              onClick={() => setEditing(true)}
              className="text-xs text-accent hover:text-accent/70 transition"
            >
              编辑
            </button>
            <button
              onClick={() => { void handleToggleEnabled(); }}
              disabled={toggling}
              className={`text-xs transition disabled:opacity-50 ${
                account.enabled
                  ? 'text-yellow-500 hover:text-yellow-400'
                  : 'text-ok hover:text-ok/70'
              }`}
            >
              {toggling ? '…' : account.enabled ? '暂停' : '启用'}
            </button>
            {banned && (
              <button
                onClick={() => { void handleRecover(); }}
                disabled={recovering}
                className="text-xs text-ok hover:text-ok/70 disabled:opacity-50 transition"
                title="清除封禁/冷却/永久封禁状态并重新启用"
              >
                {recovering ? '恢复中…' : '恢复'}
              </button>
            )}
            {no1mActive && (
              <button
                onClick={() => { void handleClearNo1M(); }}
                disabled={clearingNo1M}
                className="text-xs text-amber-400 hover:text-amber-300 disabled:opacity-50 transition"
                title="清除「不支持1M」标记，允许该号重新承接长上下文请求"
              >
                {clearingNo1M ? '清除中…' : '清除'}
              </button>
            )}
            <button
              onClick={() => { void handleUnassign(); }}
              disabled={removing}
              className="text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
            >
              {removing ? '解绑中…' : '解绑'}
            </button>
          </div>
        </td>
      </tr>
      {editing && (
        <AccountEditModal
          account={account}
          users={users}
          onSave={onRefresh}
          onClose={() => setEditing(false)}
        />
      )}
      {showTest && (
        <TestModal account={account} onClose={() => setShowTest(false)} />
      )}
    </>
  );
}

// ------------------------------------------------------------------
// Account card (mobile)
// ------------------------------------------------------------------
function AccountMobileCard({
  account,
  quotaMap,
  users,
  onUnassign,
  onRefresh,
}: {
  account: AccountRow;
  quotaMap: Map<string, QuotaAll>;
  users: UserRow[];
  onUnassign: (nodeId: string, accountId: string) => void;
  onRefresh: () => void;
}) {
  const [removing, setRemoving] = useState(false);
  const [editing, setEditing] = useState(false);
  const [toggling, setToggling] = useState(false);

  const ownerName = useMemo(() => {
    if (!account.ownerId) return '超级管理员';
    const u = users.find((u) => u.id === account.ownerId);
    return u ? u.username : account.ownerId;
  }, [account.ownerId, users]);

  async function handleUnassign() {
    if (!confirm(`确认解绑账户 ${account.accountId} 与节点 ${account.nodeName}？`)) return;
    setRemoving(true);
    try {
      await unassignAccount(account.nodeId, account.accountId);
      onUnassign(account.nodeId, account.accountId);
    } catch {
      setRemoving(false);
    }
  }

  async function handleToggleEnabled() {
    setToggling(true);
    try {
      await updateNodeAccount(account.nodeId, account.accountId, {
        egress: account.egress,
        weight: account.weight,
        role: account.role,
        enabled: !account.enabled,
      });
      onRefresh();
    } catch {
      setToggling(false);
    }
  }

  return (
    <>
      <div className="bg-surface border border-line rounded-xl p-4 space-y-2">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <p className="text-sm font-semibold text-ink truncate">{account.nodeName}</p>
            <p className="text-xs text-ink mt-0.5 truncate">{account.email || '—'}</p>
            <p className="text-[10px] text-muted font-mono mt-0.5 truncate">{account.profileId || '—'}</p>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <button
              onClick={() => setEditing(true)}
              className="text-xs text-accent hover:text-accent/70 transition"
            >
              编辑
            </button>
            <button
              onClick={() => { void handleToggleEnabled(); }}
              disabled={toggling}
              className={`text-xs transition disabled:opacity-50 ${
                account.enabled
                  ? 'text-yellow-500 hover:text-yellow-400'
                  : 'text-ok hover:text-ok/70'
              }`}
            >
              {toggling ? '…' : account.enabled ? '暂停' : '启用'}
            </button>
            <button
              onClick={() => { void handleUnassign(); }}
              disabled={removing}
              className="text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
            >
              {removing ? '…' : '解绑'}
            </button>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-3 text-xs text-muted">
          <span className={`flex items-center gap-1 ${account.enabled ? 'text-ok' : 'text-muted'}`}>
            <span className={`w-1.5 h-1.5 rounded-full ${account.enabled ? 'bg-ok' : 'bg-muted'}`} />
            {account.enabled ? '启用' : '禁用'}
          </span>
          {account.subscriptionType && <span>{account.subscriptionType}</span>}
          <span>权重 {account.weight}</span>
          {account.role && <span>角色 {account.role}</span>}
          {account.egress && <span className="font-mono">出口 {account.egress}</span>}
          <span>今日 {fmtCost(account.todayCostUsd)}</span>
          <span>总计 {fmtCost(account.totalCostUsd)}</span>
          <span>租户 {ownerName}</span>
        </div>

        {account.expiresAt && (
          <ExpiryCell expiresAt={account.expiresAt} />
        )}

        {account.cpaQuota ? <CpaQuotaCell q={account.cpaQuota} /> : <QuotaCell nodeId={account.nodeId} profileId={account.profileId} quotaMap={quotaMap} />}
      </div>
      {editing && (
        <AccountEditModal
          account={account}
          users={users}
          onSave={onRefresh}
          onClose={() => setEditing(false)}
        />
      )}
    </>
  );
}

// ------------------------------------------------------------------
// Sort icon helper
// ------------------------------------------------------------------
function SortIcon({ active, dir }: { active: boolean; dir: 'asc' | 'desc' }) {
  if (!active) return <span className="text-muted/40 ml-0.5">↕</span>;
  return <span className="text-accent ml-0.5">{dir === 'asc' ? '↑' : '↓'}</span>;
}

// ------------------------------------------------------------------
// Accounts page
// ------------------------------------------------------------------
// 10/page — consistent with the tenant 号库 and dashboard lists.
const PAGE_SIZE = 10;

export default function Accounts() {
  const { isTenant } = useAuth();
  if (isTenant) return <TenantAccounts />;
  return <AdminAccounts />;
}

function AdminAccounts() {
  const [accounts, setAccounts] = useState<AccountRow[]>([]);
  const [quotaMap, setQuotaMap] = useState<Map<string, QuotaAll>>(new Map());
  const [users, setUsers] = useState<UserRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [refreshingAll, setRefreshingAll] = useState(false);

  // Search / status filter / sort / page
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<'' | 'active' | 'limited' | 'banned' | 'cooldown'>('');
  const [sortKey, setSortKey] = useState<SortKey>(null);
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');
  const [page, setPage] = useState(1);

  const fetchAll = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [accountList, userList] = await Promise.all([
        listAccounts(),
        listUsers(),
      ]);
      setUsers(userList ?? []);
      const accs = accountList ?? [];
      setAccounts(accs);
      setLoading(false); // show the list immediately; quota badges fill in async below

      // Only meridian accounts render from the live node-quota map (QuotaCell). CPA
      // accounts render from the persisted account.cpaQuota that listAccounts already
      // returned — so DON'T fetch their nodes' quota here. getNodeQuota on a CPA node
      // hits the upstream usage endpoint once PER account, which was the slow part of
      // every 号库 load (and the result wasn't even displayed). CPA quota refreshes via
      // the 刷新 buttons now.
      const meridianNodeIds = [...new Set(
        accs.filter((a) => !a.accountId?.startsWith('cpa:')).map((a) => a.nodeId),
      )];
      if (meridianNodeIds.length > 0) {
        const results = await Promise.allSettled(
          meridianNodeIds.map((id) => getNodeQuota(id).then((q) => ({ id, q }))),
        );
        const m = new Map<string, QuotaAll>();
        for (const r of results) {
          if (r.status === 'fulfilled') m.set(r.value.id, r.value.q);
        }
        setQuotaMap(m);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchAll();
  }, [fetchAll]);

  // When a CPA quota window reaches its reset time, re-fetch so the account's
  // status/utilisation refreshes for the new window. Schedule a single timer at
  // the soonest upcoming reset (+3s buffer for the backend's quota poll).
  useEffect(() => {
    const resets: number[] = [];
    for (const a of accounts) {
      const q = a.cpaQuota;
      if (!q) continue;
      for (const s of [q.fiveHourResetsAt, q.sevenDayResetsAt, q.sevenDaySonnetResetsAt]) {
        const t = s ? Date.parse(s) : NaN;
        if (!isNaN(t) && t > Date.now()) resets.push(t);
      }
    }
    if (resets.length === 0) return;
    const delay = Math.max(1000, Math.min(...resets) - Date.now() + 3000);
    const id = setTimeout(() => { void fetchAll(); }, Math.min(delay, 2_000_000_000));
    return () => clearTimeout(id);
  }, [accounts, fetchAll]);

  // Reset page when search changes
  useEffect(() => { setPage(1); }, [search, statusFilter]);

  function handleUnassign(nodeId: string, accountId: string) {
    setAccounts((prev) =>
      prev.filter((a) => !(a.nodeId === nodeId && a.accountId === accountId)),
    );
  }

  // Filtering + sorting + pagination
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const inStatus = (s?: string) => {
      if (!statusFilter) return true;
      const banned = s === 'banned' || s === 'permanent' || s === 'half_open' || s === 'disabled';
      if (statusFilter === 'banned') return banned;
      if (statusFilter === 'limited') return s === 'limited';
      if (statusFilter === 'cooldown') return s === 'cooldown';
      return !banned && s !== 'limited' && s !== 'cooldown'; // 'active' (含 待命/亲和)
    };
    let list = accounts.filter((a) => {
      if (!inStatus(a.status)) return false;
      if (!q) return true;
      if ((a.email || '').toLowerCase().includes(q)) return true;
      if ((a.nodeName || '').toLowerCase().includes(q)) return true;
      if (!a.ownerId) return '超级管理员'.includes(q);
      const u = users.find((u) => u.id === a.ownerId);
      return (u ? u.username : a.ownerId).toLowerCase().includes(q);
    });

    if (sortKey) {
      list = [...list].sort((a, b) => {
        let av: number | string;
        let bv: number | string;
        switch (sortKey) {
          case 'nodeName': av = a.nodeName || ''; bv = b.nodeName || ''; break;
          case 'email': av = a.email || ''; bv = b.email || ''; break;
          case 'subscriptionType': av = a.subscriptionType || ''; bv = b.subscriptionType || ''; break;
          case 'weight': av = a.weight ?? 0; bv = b.weight ?? 0; break;
          case 'role': av = a.role || ''; bv = b.role || ''; break;
          case 'expiresAt': av = a.expiresAt ?? 0; bv = b.expiresAt ?? 0; break;
          case 'ownerId': {
            const ua = users.find((u) => u.id === a.ownerId);
            const ub = users.find((u) => u.id === b.ownerId);
            av = ua ? ua.username : a.ownerId || '';
            bv = ub ? ub.username : b.ownerId || '';
            break;
          }
          case 'todayCostUsd': av = a.todayCostUsd ?? 0; bv = b.todayCostUsd ?? 0; break;
          case 'totalCostUsd': av = a.totalCostUsd ?? 0; bv = b.totalCostUsd ?? 0; break;
          default: av = 0; bv = 0;
        }
        const cmp = av < bv ? -1 : av > bv ? 1 : 0;
        return sortDir === 'asc' ? cmp : -cmp;
      });
    } else {
      // Default order: active first, quota-limited last (限额排最后，正常排前面).
      list = [...list].sort((a, b) => statusRank(a.status) - statusRank(b.status));
    }
    return list;
  }, [accounts, search, statusFilter, sortKey, sortDir, users]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const paginated = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  function handleSort(key: SortKey) {
    if (sortKey === key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortKey(key);
      setSortDir('asc');
    }
    setPage(1);
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-semibold text-ink">号库</h1>
        <p className="text-xs text-muted mt-1">Claude 账户管理</p>
      </div>

      {/* Search */}
      <div className="flex flex-col sm:flex-row gap-3 items-start sm:items-center">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索邮箱、节点、租户…"
          className="w-full sm:w-72 bg-surface border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as '' | 'active' | 'limited' | 'banned' | 'cooldown')}
          className="bg-surface border border-line rounded-lg px-3 py-2 text-sm text-ink focus:outline-none focus:border-accent transition"
        >
          <option value="">全部状态</option>
          <option value="active">活跃</option>
          <option value="limited">限额</option>
          <option value="banned">封号</option>
          <option value="cooldown">冷却</option>
        </select>
        {search && (
          <button
            onClick={() => setSearch('')}
            className="text-xs text-muted hover:text-ink transition"
          >
            清除
          </button>
        )}
        <button
          onClick={async () => {
            setRefreshingAll(true);
            try {
              const r = await refreshAllQuota();
              await fetchAll();
              setToast(`已刷新 ${r?.refreshed ?? 0} 个号的额度`);
            } catch {
              setToast('刷新额度失败');
            } finally {
              setRefreshingAll(false);
            }
          }}
          disabled={refreshingAll || loading}
          className="ml-auto text-xs px-3 py-1.5 rounded-lg border border-accent/40 text-accent
                     hover:bg-accent/10 disabled:opacity-50 transition"
          title="拉取全部 CPA 号的最新额度"
        >
          {refreshingAll ? '刷新中…' : '刷新全部额度'}
        </button>
        <span className="text-xs text-muted">共 {filtered.length} 条</span>
      </div>

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center min-h-32">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}

      {/* Error */}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm flex items-center justify-between gap-3">
          <span>{error}</span>
          <button
            onClick={() => { void fetchAll(); }}
            className="text-xs text-err underline hover:no-underline"
          >
            重试
          </button>
        </div>
      )}

      {/* Empty */}
      {!loading && !error && accounts.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
          暂无账户 — 请前往节点页面添加 Claude 账户
        </div>
      )}

      {/* Desktop table */}
      {!loading && !error && accounts.length > 0 && (
        <>
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
            <table className="w-full text-left">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none" onClick={() => handleSort('nodeName')}>
                    节点 <SortIcon active={sortKey === 'nodeName'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none" onClick={() => handleSort('email')}>
                    邮箱 <SortIcon active={sortKey === 'email'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none" onClick={() => handleSort('subscriptionType')}>
                    订阅类型 <SortIcon active={sortKey === 'subscriptionType'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium">限额</th>
                  <th className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none" onClick={() => handleSort('weight')}>
                    权重 <SortIcon active={sortKey === 'weight'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none" onClick={() => handleSort('role')}>
                    角色 <SortIcon active={sortKey === 'role'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none" onClick={() => handleSort('expiresAt')}>
                    订阅到期 <SortIcon active={sortKey === 'expiresAt'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none" onClick={() => handleSort('ownerId')}>
                    租户 <SortIcon active={sortKey === 'ownerId'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium text-right cursor-pointer hover:text-ink select-none" onClick={() => handleSort('todayCostUsd')}>
                    今日消费 <SortIcon active={sortKey === 'todayCostUsd'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium text-right cursor-pointer hover:text-ink select-none" onClick={() => handleSort('totalCostUsd')}>
                    总消费 <SortIcon active={sortKey === 'totalCostUsd'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {paginated.map((a) => (
                  <AccountTableRow
                    key={`${a.nodeId}/${a.accountId}`}
                    account={a}
                    quotaMap={quotaMap}
                    users={users}
                    onUnassign={handleUnassign}
                    onRefresh={() => { void fetchAll(); }}
                    onToast={setToast}
                  />
                ))}
              </tbody>
            </table>
          </div>

          {/* Cards: visible only on mobile */}
          <div className="md:hidden space-y-3">
            {paginated.map((a) => (
              <AccountMobileCard
                key={`${a.nodeId}/${a.accountId}`}
                account={a}
                quotaMap={quotaMap}
                users={users}
                onUnassign={handleUnassign}
                onRefresh={() => { void fetchAll(); }}
              />
            ))}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-center gap-2 pt-2">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={page === 1}
                className="text-xs px-3 py-1.5 bg-surface border border-line rounded-lg text-muted
                           hover:text-ink disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                上一页
              </button>
              <span className="text-xs text-muted">
                第 {page} / {totalPages} 页
              </span>
              <button
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                disabled={page === totalPages}
                className="text-xs px-3 py-1.5 bg-surface border border-line rounded-lg text-muted
                           hover:text-ink disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                下一页
              </button>
            </div>
          )}
        </>
      )}

      {/* Toast */}
      {toast && (
        <Toast msg={toast} onClose={() => setToast(null)} />
      )}
    </div>
  );
}
