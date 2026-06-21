// ============================================================
// Tower SPA — Accounts page (号库)
// "加号" OAuth wizard + account list table/cards
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import {
  listNodes,
  listAccounts,
  unassignAccount,
  oauthStart,
  oauthExchange,
  updateNodeAccount,
  listNodeProfiles,
  importNodeProfile,
  getNodeQuota,
  listSlots,
} from '../api';
import type { NodeRecord, AccountRow, NodeProfile, QuotaAll, Slot } from '../types';

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
// Quota badge helper
// ------------------------------------------------------------------
function QuotaBadge({ utilization, label }: { utilization: number; label: string }) {
  const pct = Math.round(utilization * 100);
  let cls = 'bg-green-500/20 text-green-400 border-green-500/40';
  if (utilization >= 0.9) cls = 'bg-red-500/20 text-red-400 border-red-500/40';
  else if (utilization >= 0.7) cls = 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40';
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[10px] font-mono ${cls}`}>
      {label} {pct}%
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
    <div className="flex items-center gap-1 flex-wrap">
      {w5h && <QuotaBadge utilization={w5h.utilization} label="5h" />}
      {w7d && <QuotaBadge utilization={w7d.utilization} label="7d" />}
    </div>
  );
}

// ------------------------------------------------------------------
// OAuth Wizard ("加号"区)
// Steps: select-node → authorizing → exchange-code
// ------------------------------------------------------------------
type WizardStep = 'idle' | 'starting' | 'waitcode' | 'exchanging';

function OAuthWizard({
  nodes,
  accounts,
  onSuccess,
}: {
  nodes: NodeRecord[];
  accounts: AccountRow[];
  onSuccess: (msg: string) => void;
}) {
  const [nodeId, setNodeId] = useState('');
  const [step, setStep] = useState<WizardStep>('idle');
  const [authorizeUrl, setAuthorizeUrl] = useState('');
  const [codeVerifier, setCodeVerifier] = useState('');
  const [oauthState, setOauthState] = useState('');
  const [code, setCode] = useState('');
  const [err, setErr] = useState<string | null>(null);

  // Node profiles
  const [profiles, setProfiles] = useState<NodeProfile[]>([]);
  const [profilesLoading, setProfilesLoading] = useState(false);
  const [profilesErr, setProfilesErr] = useState<string | null>(null);
  const [importingId, setImportingId] = useState<string | null>(null);

  // Fetch profiles when a node is selected
  useEffect(() => {
    if (!nodeId) {
      setProfiles([]);
      setProfilesErr(null);
      return;
    }
    setProfilesLoading(true);
    setProfilesErr(null);
    listNodeProfiles(nodeId)
      .then((data) => setProfiles(data))
      .catch((e) => setProfilesErr(e instanceof Error ? e.message : '加载失败'))
      .finally(() => setProfilesLoading(false));
  }, [nodeId]);

  // Profiles already imported (by profileId)
  const importedProfileIds = new Set(accounts.filter((a) => a.nodeId === nodeId).map((a) => a.profileId));

  async function handleImport(profileId: string) {
    setImportingId(profileId);
    try {
      await importNodeProfile(nodeId, profileId);
      onSuccess('导入成功！账户已绑定到节点。');
    } catch (e) {
      setErr(e instanceof Error ? e.message : '导入失败');
    } finally {
      setImportingId(null);
    }
  }

  async function handleStart() {
    if (!nodeId) return;
    setStep('starting');
    setErr(null);
    try {
      const res = await oauthStart(nodeId);
      setAuthorizeUrl(res.authorizeUrl);
      setCodeVerifier(res.codeVerifier);
      setOauthState(res.state);
      setStep('waitcode');
    } catch (e) {
      setErr(e instanceof Error ? e.message : '启动授权失败');
      setStep('idle');
    }
  }

  async function handleExchange() {
    if (!code.trim()) return;
    setStep('exchanging');
    setErr(null);
    try {
      await oauthExchange(nodeId, {
        codeVerifier,
        state: oauthState,
        code: code.trim(),
      });
      // Reset wizard
      setStep('idle');
      setAuthorizeUrl('');
      setCodeVerifier('');
      setOauthState('');
      setCode('');
      setNodeId('');
      onSuccess('加号成功！账户已绑定到节点。');
    } catch (e) {
      setErr(e instanceof Error ? e.message : '授权码兑换失败');
      setStep('waitcode'); // let user retry with new code
    }
  }

  function handleReset() {
    setStep('idle');
    setAuthorizeUrl('');
    setCodeVerifier('');
    setOauthState('');
    setCode('');
    setErr(null);
  }

  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-4">
      <h2 className="text-sm font-semibold text-ink">加号 — OAuth 授权向导</h2>

      {/* Step 1: pick node */}
      {(step === 'idle' || step === 'starting') && (
        <div className="flex flex-col sm:flex-row gap-2">
          <select
            value={nodeId}
            onChange={(e) => setNodeId(e.target.value)}
            disabled={step === 'starting'}
            className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                       focus:outline-none focus:border-accent transition disabled:opacity-50"
          >
            <option value="">选择节点…</option>
            {nodes.map((n) => (
              <option key={n.id} value={n.id}>
                {n.name} ({n.baseUrl})
              </option>
            ))}
          </select>
          <button
            onClick={() => { void handleStart(); }}
            disabled={!nodeId || step === 'starting'}
            className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                       hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition whitespace-nowrap"
          >
            {step === 'starting' ? '请求中…' : '开始授权'}
          </button>
        </div>
      )}

      {/* Node existing profiles (shown when a node is selected) */}
      {nodeId && (step === 'idle' || step === 'starting') && (
        <div className="border border-line rounded-lg p-3 space-y-2">
          <p className="text-xs font-semibold text-muted uppercase tracking-wide">
            该节点已有账户（可直接导入）
          </p>
          {profilesLoading && (
            <p className="text-xs text-muted animate-pulse">加载中…</p>
          )}
          {profilesErr && (
            <p className="text-xs text-err">{profilesErr}</p>
          )}
          {!profilesLoading && !profilesErr && profiles.length === 0 && (
            <p className="text-xs text-muted">该节点暂无已登录账户</p>
          )}
          {!profilesLoading && !profilesErr && profiles.length > 0 && (
            <ul className="space-y-2">
              {profiles.map((p) => {
                const alreadyImported = importedProfileIds.has(p.id);
                return (
                  <li key={p.id} className="flex items-center justify-between gap-2">
                    <div className="min-w-0">
                      <span className="text-xs text-ink font-mono truncate block">
                        {p.email || p.id}
                      </span>
                      <div className="flex items-center gap-2 mt-0.5">
                        {p.loggedIn ? (
                          <span className="text-[10px] bg-ok/10 text-ok border border-ok/30 rounded px-1">
                            已登录
                          </span>
                        ) : (
                          <span className="text-[10px] bg-muted/10 text-muted border border-line rounded px-1">
                            未登录
                          </span>
                        )}
                        {p.subscriptionType && (
                          <span className="text-[10px] text-muted">{p.subscriptionType}</span>
                        )}
                      </div>
                    </div>
                    {alreadyImported ? (
                      <span className="text-xs text-muted shrink-0">已导入</span>
                    ) : (
                      <button
                        onClick={() => { void handleImport(p.id); }}
                        disabled={importingId === p.id}
                        className="shrink-0 text-xs px-2 py-1 bg-accent text-white rounded-lg
                                   hover:bg-accent/80 disabled:opacity-50 transition"
                      >
                        {importingId === p.id ? '导入中…' : '导入'}
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}

      {/* Step 2: show auth URL + code input */}
      {(step === 'waitcode' || step === 'exchanging') && (
        <div className="space-y-3">
          <div className="bg-bg border border-line rounded-lg p-3 space-y-2">
            <p className="text-xs font-semibold text-muted uppercase tracking-wide">授权链接</p>
            <a
              href={authorizeUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-accent break-all hover:underline"
            >
              {authorizeUrl}
            </a>
            <p className="text-xs text-muted">
              点击上方链接在新标签完成 Claude OAuth 登录，登录后页面会显示授权码，将其粘贴到下方。
            </p>
          </div>

          <div className="flex flex-col sm:flex-row gap-2">
            <textarea
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="粘贴授权码 (code)…"
              rows={2}
              disabled={step === 'exchanging'}
              className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                         placeholder:text-muted focus:outline-none focus:border-accent transition resize-none
                         disabled:opacity-50"
            />
            <div className="flex flex-col gap-2 sm:shrink-0">
              <button
                onClick={() => { void handleExchange(); }}
                disabled={!code.trim() || step === 'exchanging'}
                className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                           hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition whitespace-nowrap"
              >
                {step === 'exchanging' ? '兑换中…' : '完成加号'}
              </button>
              <button
                onClick={handleReset}
                disabled={step === 'exchanging'}
                className="px-4 py-2 text-sm font-medium bg-bg border border-line text-muted rounded-lg
                           hover:text-ink disabled:opacity-50 transition whitespace-nowrap"
              >
                取消
              </button>
            </div>
          </div>
        </div>
      )}

      {err && <p className="text-xs text-err">{err}</p>}
    </div>
  );
}

// ------------------------------------------------------------------
// Edit modal for per-account tuning
// ------------------------------------------------------------------
function AccountEditModal({
  account,
  onSave,
  onClose,
}: {
  account: AccountRow;
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

  useEffect(() => {
    listSlots()
      .then(setSlots)
      .catch(() => { /* ignore, dropdown just stays empty */ });
  }, []);

  async function handleSave(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setErr(null);
    try {
      await updateNodeAccount(account.nodeId, account.accountId, {
        weight: Number(weight),
        role,
        egress,
        enabled,
        slotId: slotId || undefined,
      });
      onSave();
      onClose();
    } catch (saveErr) {
      setErr(saveErr instanceof Error ? saveErr.message : '保存失败');
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <div className="bg-surface border border-line rounded-xl shadow-xl w-full max-w-md p-6 space-y-4">
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
// Account row (desktop table)
// ------------------------------------------------------------------
function AccountTableRow({
  account,
  quotaMap,
  onUnassign,
  onRefresh,
}: {
  account: AccountRow;
  quotaMap: Map<string, QuotaAll>;
  onUnassign: (nodeId: string, accountId: string) => void;
  onRefresh: () => void;
}) {
  const [removing, setRemoving] = useState(false);
  const [editing, setEditing] = useState(false);
  const [toggling, setToggling] = useState(false);

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
        <td className="px-4 py-3">
          <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${account.enabled ? 'text-ok' : 'text-muted'}`}>
            <span className={`w-1.5 h-1.5 rounded-full ${account.enabled ? 'bg-ok' : 'bg-muted'}`} />
            {account.enabled ? '启用' : '禁用'}
          </span>
        </td>
        <td className="px-4 py-3">
          <QuotaCell nodeId={account.nodeId} profileId={account.profileId} quotaMap={quotaMap} />
        </td>
        <td className="px-4 py-3 text-sm text-muted">{account.weight}</td>
        <td className="px-4 py-3 text-xs text-muted">{account.role || '—'}</td>
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
  onUnassign,
  onRefresh,
}: {
  account: AccountRow;
  quotaMap: Map<string, QuotaAll>;
  onUnassign: (nodeId: string, accountId: string) => void;
  onRefresh: () => void;
}) {
  const [removing, setRemoving] = useState(false);
  const [editing, setEditing] = useState(false);
  const [toggling, setToggling] = useState(false);

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
          <span>权重 {account.weight}</span>
          {account.role && <span>角色 {account.role}</span>}
          {account.egress && <span className="font-mono">出口 {account.egress}</span>}
        </div>

        <QuotaCell nodeId={account.nodeId} profileId={account.profileId} quotaMap={quotaMap} />
      </div>
      {editing && (
        <AccountEditModal
          account={account}
          onSave={onRefresh}
          onClose={() => setEditing(false)}
        />
      )}
    </>
  );
}

// ------------------------------------------------------------------
// Accounts page
// ------------------------------------------------------------------
export default function Accounts() {
  const [nodes, setNodes] = useState<NodeRecord[]>([]);
  const [accounts, setAccounts] = useState<AccountRow[]>([]);
  const [quotaMap, setQuotaMap] = useState<Map<string, QuotaAll>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const fetchAll = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [nodeList, accountList] = await Promise.all([
        listNodes(),
        listAccounts(),
      ]);
      setNodes(nodeList);
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

  function handleUnassign(nodeId: string, accountId: string) {
    setAccounts((prev) =>
      prev.filter((a) => !(a.nodeId === nodeId && a.accountId === accountId)),
    );
  }

  function handleSuccess(msg: string) {
    setToast(msg);
    void fetchAll();
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-semibold text-ink">号库</h1>
        <p className="text-xs text-muted mt-1">Claude 账户管理</p>
      </div>

      {/* OAuth Wizard */}
      <OAuthWizard nodes={nodes} accounts={accounts} onSuccess={handleSuccess} />

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
          暂无账户 — 使用上方向导添加 Claude 账户
        </div>
      )}

      {/* Desktop table */}
      {!loading && !error && accounts.length > 0 && (
        <>
          {/* Table: hidden on mobile */}
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
            <table className="w-full text-left">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-4 py-3 font-medium">节点</th>
                  <th className="px-4 py-3 font-medium">邮箱 / Profile</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">限额</th>
                  <th className="px-4 py-3 font-medium">权重</th>
                  <th className="px-4 py-3 font-medium">角色</th>
                  <th className="px-4 py-3 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map((a) => (
                  <AccountTableRow
                    key={`${a.nodeId}/${a.accountId}`}
                    account={a}
                    quotaMap={quotaMap}
                    onUnassign={handleUnassign}
                    onRefresh={() => { void fetchAll(); }}
                  />
                ))}
              </tbody>
            </table>
          </div>

          {/* Cards: visible only on mobile */}
          <div className="md:hidden space-y-3">
            {accounts.map((a) => (
              <AccountMobileCard
                key={`${a.nodeId}/${a.accountId}`}
                account={a}
                quotaMap={quotaMap}
                onUnassign={handleUnassign}
                onRefresh={() => { void fetchAll(); }}
              />
            ))}
          </div>
        </>
      )}

      {/* Toast */}
      {toast && (
        <Toast msg={toast} onClose={() => setToast(null)} />
      )}
    </div>
  );
}
