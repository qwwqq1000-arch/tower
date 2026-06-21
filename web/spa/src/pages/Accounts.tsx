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
  listUsers,
} from '../api';
import type { AccountRow, QuotaAll, Slot, UserRow } from '../types';
import { useAuth } from '../auth';
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

/** Format a unix-ms timestamp as HH:MM, or MM-DD HH:MM if not today */
function fmtResetTime(ms: number | undefined): string | null {
  if (!ms) return null;
  const d = new Date(ms);
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  const hhmm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  if (sameDay) return hhmm;
  const mmdd = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  return `${mmdd} ${hhmm}`;
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
  const pct = Math.round(utilization * 100);
  let cls = 'bg-green-500/20 text-green-400 border-green-500/40';
  if (utilization >= 0.9) cls = 'bg-red-500/20 text-red-400 border-red-500/40';
  else if (utilization >= 0.7) cls = 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40';
  const resetStr = utilization >= 0.5 ? fmtResetTime(resetsAt) : null;
  return (
    <span className="inline-flex flex-col items-start gap-0">
      <span className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[10px] font-mono ${cls}`}>
        {label} {pct}%
      </span>
      {resetStr && (
        <span className="text-[9px] text-muted pl-0.5">恢复 {resetStr}</span>
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
  if (!quota) return <span className="text-xs text-muted">—</span>;

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
// Sort key type
// ------------------------------------------------------------------
type SortKey = 'email' | 'expiresAt' | 'todayCostUsd' | null;

// ------------------------------------------------------------------
// Account row (desktop table)
// ------------------------------------------------------------------
function AccountTableRow({
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
      <tr className="border-t border-line hover:bg-line/30 transition">
        <td className="px-4 py-3 text-sm text-ink font-medium">{account.nodeName}</td>
        <td className="px-4 py-3">
          <p className="text-xs text-ink">{account.email || '—'}</p>
          <p className="text-[10px] text-muted font-mono mt-0.5">{account.profileId || '—'}</p>
        </td>
        <td className="px-4 py-3 text-xs text-muted">{account.subscriptionType || '—'}</td>
        <td className="px-4 py-3">
          <QuotaCell nodeId={account.nodeId} profileId={account.profileId} quotaMap={quotaMap} />
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

        <QuotaCell nodeId={account.nodeId} profileId={account.profileId} quotaMap={quotaMap} />
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
const PAGE_SIZE = 12;

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

  // Search / sort / page
  const [search, setSearch] = useState('');
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

      // Fetch quota for each distinct nodeId (best-effort, resilient)
      const distinctNodeIds = [...new Set(accs.map((a) => a.nodeId))];
      const results = await Promise.allSettled(
        distinctNodeIds.map((id) => getNodeQuota(id).then((q) => ({ id, q }))),
      );
      const m = new Map<string, QuotaAll>();
      for (const r of results) {
        if (r.status === 'fulfilled') m.set(r.value.id, r.value.q);
      }
      setQuotaMap(m);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchAll();
  }, [fetchAll]);

  // Reset page when search changes
  useEffect(() => { setPage(1); }, [search]);

  function handleUnassign(nodeId: string, accountId: string) {
    setAccounts((prev) =>
      prev.filter((a) => !(a.nodeId === nodeId && a.accountId === accountId)),
    );
  }

  // Filtering + sorting + pagination
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    let list = q
      ? accounts.filter(
          (a) =>
            (a.email || '').toLowerCase().includes(q) ||
            (a.nodeName || '').toLowerCase().includes(q) ||
            (() => {
              if (!a.ownerId) return '超级管理员'.includes(q);
              const u = users.find((u) => u.id === a.ownerId);
              return (u ? u.username : a.ownerId).toLowerCase().includes(q);
            })(),
        )
      : accounts;

    if (sortKey) {
      list = [...list].sort((a, b) => {
        let av: number | string | undefined;
        let bv: number | string | undefined;
        if (sortKey === 'email') { av = a.email || ''; bv = b.email || ''; }
        else if (sortKey === 'expiresAt') { av = a.expiresAt ?? 0; bv = b.expiresAt ?? 0; }
        else if (sortKey === 'todayCostUsd') { av = a.todayCostUsd ?? 0; bv = b.todayCostUsd ?? 0; }
        if (av === undefined || bv === undefined) return 0;
        const cmp = av < bv ? -1 : av > bv ? 1 : 0;
        return sortDir === 'asc' ? cmp : -cmp;
      });
    }
    return list;
  }, [accounts, search, sortKey, sortDir, users]);

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
        {search && (
          <button
            onClick={() => setSearch('')}
            className="text-xs text-muted hover:text-ink transition"
          >
            清除
          </button>
        )}
        <span className="text-xs text-muted ml-auto">共 {filtered.length} 条</span>
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
                  <th className="px-4 py-3 font-medium">节点</th>
                  <th
                    className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none"
                    onClick={() => handleSort('email')}
                  >
                    邮箱 <SortIcon active={sortKey === 'email'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium">订阅类型</th>
                  <th className="px-4 py-3 font-medium">限额</th>
                  <th className="px-4 py-3 font-medium">权重</th>
                  <th className="px-4 py-3 font-medium">角色</th>
                  <th
                    className="px-4 py-3 font-medium cursor-pointer hover:text-ink select-none"
                    onClick={() => handleSort('expiresAt')}
                  >
                    订阅到期 <SortIcon active={sortKey === 'expiresAt'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium">租户</th>
                  <th
                    className="px-4 py-3 font-medium text-right cursor-pointer hover:text-ink select-none"
                    onClick={() => handleSort('todayCostUsd')}
                  >
                    今日消费 <SortIcon active={sortKey === 'todayCostUsd'} dir={sortDir} />
                  </th>
                  <th className="px-4 py-3 font-medium text-right">总消费</th>
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
