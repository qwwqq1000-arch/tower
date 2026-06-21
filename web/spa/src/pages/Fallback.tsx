// ============================================================
// Tower SPA — 保底渠道 (Fallback Channels) management page
// GET/POST /api/admin/fallback-channels
// PATCH    /api/admin/fallback-channels/{id}
// PATCH    /api/admin/fallback-channels/{id}/enabled
// DELETE   /api/admin/fallback-channels/{id}
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import {
  listFallbackChannels,
  createFallbackChannel,
  updateFallbackChannel,
  setFallbackEnabled,
  deleteFallbackChannel,
  refreshFallbackBalance,
  listMeFallback,
  createMeFallback,
  updateMeFallback,
  setMeFallbackEnabled,
  deleteMeFallback,
} from '../api';
import type { FallbackChannel } from '../types';
import { useAuth } from '../auth';

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------
/** Adaptive cost formatting: <0.01 → 4 decimals, else 2 */
function fmtCost(usd: number | undefined): string {
  if (usd === undefined || usd === null) return '—';
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

function emptyForm() {
  return {
    name: '',
    baseUrl: '',
    apiKey: '',
    priority: 0,
    weight: 1,
    maxConcurrent: 4,
    cooldownMs: 0,
    priceThreshold: 0,
    modelAllowlist: '',
    balanceToken: '',
    balanceUserId: '',
    balanceAlertUsd: 0,
  };
}

type FormState = ReturnType<typeof emptyForm>;

// ------------------------------------------------------------------
// Create / Edit form (shared)
// ------------------------------------------------------------------
interface ChannelFormProps {
  initial: FormState;
  submitLabel: string;
  submitting: boolean;
  onSubmit: (f: FormState) => void;
  onCancel?: () => void;
}

function ChannelForm({ initial, submitLabel, submitting, onSubmit, onCancel }: ChannelFormProps) {
  const [f, setF] = useState<FormState>(initial);

  // reset when initial changes (edit re-opens with fresh data)
  useEffect(() => { setF(initial); }, [initial]);

  function set<K extends keyof FormState>(key: K, val: FormState[K]) {
    setF((prev) => ({ ...prev, [key]: val }));
  }

  const inputCls =
    'w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink ' +
    'placeholder:text-muted focus:outline-none focus:border-accent transition';

  return (
    <form
      onSubmit={(e) => { e.preventDefault(); onSubmit(f); }}
      className="space-y-3"
    >
      {/* Row 1: name + baseUrl */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-muted mb-1">渠道名称 *</label>
          <input
            required
            value={f.name}
            onChange={(e) => set('name', e.target.value)}
            placeholder="my-fallback"
            className={inputCls}
          />
        </div>
        <div>
          <label className="block text-xs text-muted mb-1">Base URL *</label>
          <input
            required
            value={f.baseUrl}
            onChange={(e) => set('baseUrl', e.target.value)}
            placeholder="https://api.example.com"
            className={inputCls}
          />
        </div>
      </div>

      {/* Row 2: apiKey */}
      <div>
        <label className="block text-xs text-muted mb-1">API Key（留空表示不更改）</label>
        <input
          type="password"
          value={f.apiKey}
          onChange={(e) => set('apiKey', e.target.value)}
          placeholder="sk-..."
          autoComplete="new-password"
          className={inputCls}
        />
      </div>

      {/* Row 3: priority / weight / maxConcurrent */}
      <div className="grid grid-cols-3 gap-3">
        <div>
          <label className="block text-xs text-muted mb-1">Priority</label>
          <input
            type="number"
            value={f.priority}
            onChange={(e) => set('priority', Number(e.target.value))}
            min={0}
            className={inputCls}
          />
        </div>
        <div>
          <label className="block text-xs text-muted mb-1">Weight</label>
          <input
            type="number"
            value={f.weight}
            onChange={(e) => set('weight', Number(e.target.value))}
            min={1}
            className={inputCls}
          />
        </div>
        <div>
          <label className="block text-xs text-muted mb-1">MaxConcurrent</label>
          <input
            type="number"
            value={f.maxConcurrent}
            onChange={(e) => set('maxConcurrent', Number(e.target.value))}
            min={1}
            className={inputCls}
          />
        </div>
      </div>

      {/* Row 4: cooldownMs / priceThreshold */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-muted mb-1">Cooldown (ms)</label>
          <input
            type="number"
            value={f.cooldownMs}
            onChange={(e) => set('cooldownMs', Number(e.target.value))}
            min={0}
            step={100}
            className={inputCls}
          />
        </div>
        <div>
          <label className="block text-xs text-muted mb-1">Price Threshold (USD)</label>
          <input
            type="number"
            value={f.priceThreshold}
            onChange={(e) => set('priceThreshold', Number(e.target.value))}
            min={0}
            step={0.001}
            className={inputCls}
          />
        </div>
      </div>

      {/* Row 5: modelAllowlist */}
      <div>
        <label className="block text-xs text-muted mb-1">Model Allowlist（逗号分隔，留空表示全部）</label>
        <input
          value={f.modelAllowlist}
          onChange={(e) => set('modelAllowlist', e.target.value)}
          placeholder="claude-3-5-sonnet-20241022,claude-3-haiku-20240307"
          className={inputCls}
        />
      </div>

      {/* Row 6: balance fields */}
      <div className="border-t border-line pt-3">
        <p className="text-xs text-muted mb-2 font-medium">余额监控（可选）</p>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="block text-xs text-muted mb-1">余额访问令牌（留空表示不更改）</label>
            <input
              type="password"
              value={f.balanceToken}
              onChange={(e) => set('balanceToken', e.target.value)}
              placeholder="token..."
              autoComplete="new-password"
              className={inputCls}
            />
          </div>
          <div>
            <label className="block text-xs text-muted mb-1">余额用户ID</label>
            <input
              value={f.balanceUserId}
              onChange={(e) => set('balanceUserId', e.target.value)}
              placeholder="user_xxx"
              className={inputCls}
            />
          </div>
        </div>
        <div className="mt-3">
          <label className="block text-xs text-muted mb-1">余额提醒阈值 $ (0 = 不提醒)</label>
          <input
            type="number"
            value={f.balanceAlertUsd}
            onChange={(e) => set('balanceAlertUsd', Number(e.target.value))}
            min={0}
            step={1}
            className={inputCls}
          />
        </div>
      </div>

      {/* Actions */}
      <div className="flex gap-2 pt-1">
        <button
          type="submit"
          disabled={submitting}
          className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                     hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          {submitting ? '提交中…' : submitLabel}
        </button>
        {onCancel && (
          <button
            type="button"
            onClick={onCancel}
            className="px-4 py-2 text-sm font-medium border border-line text-muted rounded-lg
                       hover:text-ink hover:border-accent transition"
          >
            取消
          </button>
        )}
      </div>
    </form>
  );
}

// ------------------------------------------------------------------
// Edit modal
// ------------------------------------------------------------------
interface EditModalProps {
  channel: FallbackChannel;
  onSave: (id: string, f: FormState) => Promise<void>;
  onClose: () => void;
}

function EditModal({ channel, onSave, onClose }: EditModalProps) {
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const initial: FormState = {
    name: channel.name,
    baseUrl: channel.baseUrl,
    apiKey: '',
    priority: channel.priority,
    weight: channel.weight,
    maxConcurrent: channel.maxConcurrent,
    cooldownMs: channel.cooldownMs,
    priceThreshold: channel.priceThreshold,
    modelAllowlist: channel.modelAllowlist,
    balanceToken: '',
    balanceUserId: channel.balanceUserId ?? '',
    balanceAlertUsd: channel.balanceAlertUsd ?? 0,
  };

  async function handleSubmit(f: FormState) {
    setSubmitting(true);
    setErr(null);
    try {
      await onSave(channel.id, f);
      onClose();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '保存失败');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center px-4 bg-black/50"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg bg-surface border border-line rounded-xl shadow-2xl p-6 space-y-4"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-base font-semibold text-ink">编辑保底渠道</h2>
        {err && (
          <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-sm">{err}</div>
        )}
        <ChannelForm
          initial={initial}
          submitLabel="保存"
          submitting={submitting}
          onSubmit={(f) => { void handleSubmit(f); }}
          onCancel={onClose}
        />
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Enable toggle
// ------------------------------------------------------------------
interface EnableToggleProps {
  id: string;
  enabled: boolean;
  onChange: (id: string, enabled: boolean) => void;
  setEnabledFn: (id: string, enabled: boolean) => Promise<void>;
}

function EnableToggle({ id, enabled, onChange, setEnabledFn }: EnableToggleProps) {
  const [busy, setBusy] = useState(false);

  async function toggle() {
    setBusy(true);
    try {
      await setEnabledFn(id, !enabled);
      onChange(id, !enabled);
    } finally {
      setBusy(false);
    }
  }

  return (
    <button
      onClick={() => { void toggle(); }}
      disabled={busy}
      title={enabled ? '点击禁用' : '点击启用'}
      className={[
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition',
        enabled ? 'bg-ok' : 'bg-line',
        busy ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
      ].join(' ')}
    >
      <span
        className={[
          'inline-block h-3.5 w-3.5 rounded-full bg-white shadow transition-transform',
          enabled ? 'translate-x-4' : 'translate-x-1',
        ].join(' ')}
      />
    </button>
  );
}

// ------------------------------------------------------------------
// Main page
// ------------------------------------------------------------------
export default function Fallback() {
  const { isTenant } = useAuth();
  // Choose owner-scoped (/api/me) or admin endpoints based on role.
  const listFn = isTenant ? listMeFallback : listFallbackChannels;
  const createFn = isTenant ? createMeFallback : createFallbackChannel;
  const updateFn = isTenant ? updateMeFallback : updateFallbackChannel;
  const setEnabledFn = isTenant ? setMeFallbackEnabled : setFallbackEnabled;
  const deleteFn = isTenant ? deleteMeFallback : deleteFallbackChannel;

  const [channels, setChannels] = useState<FallbackChannel[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [creating, setCreating] = useState(false);
  const [createErr, setCreateErr] = useState<string | null>(null);

  const [editTarget, setEditTarget] = useState<FallbackChannel | null>(null);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [balanceBusy, setBalanceBusy] = useState<string | null>(null); // channel id being refreshed

  // ---- fetch ----
  const fetchChannels = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listFn();
      setChannels(data);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, [listFn]);

  useEffect(() => { void fetchChannels(); }, [fetchChannels]);

  // ---- create ----
  async function handleCreate(f: FormState) {
    setCreating(true);
    setCreateErr(null);
    try {
      const ch = await createFn({
        name: f.name,
        baseUrl: f.baseUrl,
        ...(f.apiKey ? { apiKey: f.apiKey } : {}),
        priority: f.priority,
        weight: f.weight,
        maxConcurrent: f.maxConcurrent,
        cooldownMs: f.cooldownMs,
        priceThreshold: f.priceThreshold,
        modelAllowlist: f.modelAllowlist,
        ...(f.balanceToken ? { balanceToken: f.balanceToken } : {}),
        ...(f.balanceUserId ? { balanceUserId: f.balanceUserId } : {}),
        ...(f.balanceAlertUsd > 0 ? { balanceAlertUsd: f.balanceAlertUsd } : {}),
      });
      setChannels((prev) => [...prev, ch]);
    } catch (e) {
      setCreateErr(e instanceof Error ? e.message : '创建失败');
    } finally {
      setCreating(false);
    }
  }

  // ---- edit ----
  async function handleEdit(id: string, f: FormState) {
    const updated = await updateFn(id, {
      name: f.name,
      baseUrl: f.baseUrl,
      ...(f.apiKey ? { apiKey: f.apiKey } : {}),
      priority: f.priority,
      weight: f.weight,
      maxConcurrent: f.maxConcurrent,
      cooldownMs: f.cooldownMs,
      priceThreshold: f.priceThreshold,
      modelAllowlist: f.modelAllowlist,
      ...(f.balanceToken ? { balanceToken: f.balanceToken } : {}),
      balanceUserId: f.balanceUserId,
      balanceAlertUsd: f.balanceAlertUsd,
    });
    setChannels((prev) => prev.map((c) => (c.id === id ? updated : c)));
  }

  // ---- toggle enabled ----
  function handleToggle(id: string, enabled: boolean) {
    setChannels((prev) => prev.map((c) => (c.id === id ? { ...c, enabled } : c)));
  }

  // ---- delete ----
  async function handleDelete(id: string) {
    setDeleteBusy(true);
    try {
      await deleteFn(id);
      setChannels((prev) => prev.filter((c) => c.id !== id));
      setDeleteId(null);
    } finally {
      setDeleteBusy(false);
    }
  }

  // ---- refresh balance ----
  async function handleRefreshBalance(id: string) {
    setBalanceBusy(id);
    try {
      const result = await refreshFallbackBalance(id);
      setChannels((prev) =>
        prev.map((c) =>
          c.id === id
            ? {
                ...c,
                balanceUsd: result.balanceUsd,
                balanceError: result.error,
                balanceCheckedAt: Date.now(),
              }
            : c,
        ),
      );
    } catch (e) {
      setChannels((prev) =>
        prev.map((c) =>
          c.id === id
            ? { ...c, balanceError: e instanceof Error ? e.message : '获取失败', balanceCheckedAt: Date.now() }
            : c,
        ),
      );
    } finally {
      setBalanceBusy(null);
    }
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">保底渠道</h1>
          <p className="text-xs text-muted mt-1">
            保底渠道是外部 Anthropic 兼容中转；命中低价/容量打满/关键词等触发时使用。
          </p>
        </div>
        <button
          onClick={() => { void fetchChannels(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center min-h-32">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}

      {/* Fetch error */}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}

      {/* Channel list */}
      {!loading && !error && (
        <>
          {channels.length === 0 ? (
            <div className="bg-surface border border-line rounded-xl p-12 text-center space-y-2">
              <p className="text-3xl">⤵</p>
              <p className="text-ink font-medium">暂无保底渠道</p>
              <p className="text-xs text-muted">在下方表单中添加第一个保底渠道。</p>
            </div>
          ) : (
            <>
              {/* Desktop table */}
              <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
                <table className="w-full text-left text-sm">
                  <thead>
                    <tr className="text-xs text-muted uppercase tracking-wide border-b border-line bg-bg/50">
                      <th className="px-4 py-3 font-medium">名称</th>
                      <th className="px-4 py-3 font-medium">Base URL</th>
                      <th className="px-4 py-3 font-medium text-right">Priority</th>
                      <th className="px-4 py-3 font-medium text-right">Weight</th>
                      <th className="px-4 py-3 font-medium text-right">并发</th>
                      <th className="px-4 py-3 font-medium text-right">价格阈值</th>
                      <th className="px-4 py-3 font-medium text-right">今日消费</th>
                      <th className="px-4 py-3 font-medium text-right">总消费</th>
                      <th className="px-4 py-3 font-medium text-right">余额</th>
                      <th className="px-4 py-3 font-medium">启用</th>
                      <th className="px-4 py-3 font-medium">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {channels.map((c) => (
                      <tr key={c.id} className="border-t border-line/50 hover:bg-line/30 transition">
                        <td className="px-4 py-3 font-medium text-ink">
                          <div className="flex items-center gap-2">
                            {c.name}
                            {c.hasKey && (
                              <span className="text-[10px] border border-ok/40 text-ok rounded px-1">KEY</span>
                            )}
                          </div>
                        </td>
                        <td className="px-4 py-3 text-muted font-mono text-xs truncate max-w-[200px]">{c.baseUrl}</td>
                        <td className="px-4 py-3 text-right tabular-nums">{c.priority}</td>
                        <td className="px-4 py-3 text-right tabular-nums">{c.weight}</td>
                        <td className="px-4 py-3 text-right tabular-nums">{c.maxConcurrent}</td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {c.priceThreshold > 0 ? `$${c.priceThreshold.toFixed(4)}` : '—'}
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          <span>{fmtCost(c.todayCostUsd)}</span>
                          {c.todayRequests !== undefined && (
                            <span className="block text-[10px] text-muted">({c.todayRequests} 次)</span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          <span>{fmtCost(c.totalCostUsd)}</span>
                          {c.totalRequests !== undefined && (
                            <span className="block text-[10px] text-muted">({c.totalRequests} 次)</span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {c.balanceError ? (
                            <span className="text-err text-xs" title={c.balanceError}>错误</span>
                          ) : c.balanceUsd !== undefined ? (
                            <span className={
                              c.balanceAlertUsd && c.balanceAlertUsd > 0 && c.balanceUsd < c.balanceAlertUsd
                                ? 'text-err font-medium'
                                : 'text-ink'
                            }>
                              {c.balanceUsd < 0.01 ? `$${c.balanceUsd.toFixed(4)}` : `$${c.balanceUsd.toFixed(2)}`}
                            </span>
                          ) : (
                            <span className="text-muted text-xs">—</span>
                          )}
                          {c.hasBalanceToken && (
                            <button
                              onClick={() => { void handleRefreshBalance(c.id); }}
                              disabled={balanceBusy === c.id}
                              className="block text-[10px] text-muted hover:text-accent transition mt-0.5 disabled:opacity-50"
                            >
                              {balanceBusy === c.id ? '获取中…' : '获取余额'}
                            </button>
                          )}
                        </td>
                        <td className="px-4 py-3">
                          <EnableToggle id={c.id} enabled={c.enabled} onChange={handleToggle} setEnabledFn={setEnabledFn} />
                        </td>
                        <td className="px-4 py-3">
                          <div className="flex items-center gap-2">
                            <button
                              onClick={() => setEditTarget(c)}
                              className="text-xs text-muted hover:text-accent transition"
                            >
                              编辑
                            </button>
                            <button
                              onClick={() => setDeleteId(c.id)}
                              className="text-xs text-muted hover:text-err transition"
                            >
                              删除
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {/* Mobile cards */}
              <div className="md:hidden space-y-3">
                {channels.map((c) => (
                  <div key={c.id} className="bg-surface border border-line rounded-xl p-4 space-y-3">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-ink text-sm">{c.name}</span>
                          {c.hasKey && (
                            <span className="text-[10px] border border-ok/40 text-ok rounded px-1">KEY</span>
                          )}
                        </div>
                        <p className="text-xs text-muted font-mono truncate mt-0.5">{c.baseUrl}</p>
                      </div>
                      <EnableToggle id={c.id} enabled={c.enabled} onChange={handleToggle} setEnabledFn={setEnabledFn} />
                    </div>
                    <div className="grid grid-cols-4 gap-2 text-xs">
                      <div>
                        <p className="text-muted">Priority</p>
                        <p className="text-ink font-medium tabular-nums">{c.priority}</p>
                      </div>
                      <div>
                        <p className="text-muted">Weight</p>
                        <p className="text-ink font-medium tabular-nums">{c.weight}</p>
                      </div>
                      <div>
                        <p className="text-muted">并发</p>
                        <p className="text-ink font-medium tabular-nums">{c.maxConcurrent}</p>
                      </div>
                      <div>
                        <p className="text-muted">价格阈值</p>
                        <p className="text-ink font-medium tabular-nums">
                          {c.priceThreshold > 0 ? `$${c.priceThreshold.toFixed(4)}` : '—'}
                        </p>
                      </div>
                    </div>
                    <div className="grid grid-cols-3 gap-2 text-xs">
                      <div>
                        <p className="text-muted">今日消费</p>
                        <p className="text-ink font-medium tabular-nums">{fmtCost(c.todayCostUsd)}</p>
                        {c.todayRequests !== undefined && (
                          <p className="text-[10px] text-muted">({c.todayRequests} 次)</p>
                        )}
                      </div>
                      <div>
                        <p className="text-muted">总消费</p>
                        <p className="text-ink font-medium tabular-nums">{fmtCost(c.totalCostUsd)}</p>
                        {c.totalRequests !== undefined && (
                          <p className="text-[10px] text-muted">({c.totalRequests} 次)</p>
                        )}
                      </div>
                      <div>
                        <p className="text-muted">余额</p>
                        {c.balanceError ? (
                          <p className="text-err text-xs font-medium" title={c.balanceError}>错误</p>
                        ) : c.balanceUsd !== undefined ? (
                          <p className={[
                            'tabular-nums font-medium',
                            c.balanceAlertUsd && c.balanceAlertUsd > 0 && c.balanceUsd < c.balanceAlertUsd
                              ? 'text-err'
                              : 'text-ink',
                          ].join(' ')}>
                            {c.balanceUsd < 0.01 ? `$${c.balanceUsd.toFixed(4)}` : `$${c.balanceUsd.toFixed(2)}`}
                          </p>
                        ) : (
                          <p className="text-muted tabular-nums">—</p>
                        )}
                        {c.hasBalanceToken && (
                          <button
                            onClick={() => { void handleRefreshBalance(c.id); }}
                            disabled={balanceBusy === c.id}
                            className="text-[10px] text-muted hover:text-accent transition mt-0.5 disabled:opacity-50"
                          >
                            {balanceBusy === c.id ? '获取中…' : '获取余额'}
                          </button>
                        )}
                      </div>
                    </div>
                    {c.modelAllowlist && (
                      <p className="text-xs text-muted truncate">
                        模型: {c.modelAllowlist}
                      </p>
                    )}
                    <div className="flex gap-3 pt-1">
                      <button
                        onClick={() => setEditTarget(c)}
                        className="text-xs text-muted hover:text-accent transition"
                      >
                        编辑
                      </button>
                      <button
                        onClick={() => setDeleteId(c.id)}
                        className="text-xs text-muted hover:text-err transition"
                      >
                        删除
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}

          {/* Create form */}
          <div className="bg-surface border border-line rounded-xl p-5 space-y-4">
            <h2 className="text-sm font-semibold text-ink">添加保底渠道</h2>
            {createErr && (
              <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-sm">{createErr}</div>
            )}
            <ChannelForm
              key={channels.length} // reset form after successful create
              initial={emptyForm()}
              submitLabel="添加"
              submitting={creating}
              onSubmit={(f) => { void handleCreate(f); }}
            />
          </div>
        </>
      )}

      {/* Edit modal */}
      {editTarget && (
        <EditModal
          channel={editTarget}
          onSave={handleEdit}
          onClose={() => setEditTarget(null)}
        />
      )}

      {/* Delete confirm modal */}
      {deleteId && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center px-4 bg-black/50"
          onClick={() => { if (!deleteBusy) setDeleteId(null); }}
        >
          <div
            className="w-full max-w-sm bg-surface border border-line rounded-xl shadow-2xl p-6 space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-base font-semibold text-ink">确认删除</h2>
            <p className="text-sm text-muted">此操作不可撤销，渠道将被永久删除。</p>
            <div className="flex gap-3">
              <button
                onClick={() => { void handleDelete(deleteId); }}
                disabled={deleteBusy}
                className="px-4 py-2 text-sm font-medium bg-err text-white rounded-lg
                           hover:bg-err/80 disabled:opacity-50 transition"
              >
                {deleteBusy ? '删除中…' : '确认删除'}
              </button>
              <button
                onClick={() => setDeleteId(null)}
                disabled={deleteBusy}
                className="px-4 py-2 text-sm font-medium border border-line text-muted rounded-lg
                           hover:text-ink transition"
              >
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
