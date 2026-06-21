// ============================================================
// Tower SPA — Keys page (调度密钥)
// List dispatch keys (prefix / label / enabled / disable)
// "New key" → POST → show plaintext ONCE with copy button
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { listDispatchKeys, createDispatchKey, disableDispatchKey } from '../api';
import type { DispatchKeyRecord } from '../types';

// ------------------------------------------------------------------
// New-key modal — shows plaintext key once after creation
// ------------------------------------------------------------------
interface NewKeyResult {
  id: string;
  key: string;
}

interface NewKeyModalProps {
  result: NewKeyResult;
  onClose: () => void;
}

function NewKeyModal({ result, onClose }: NewKeyModalProps) {
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    void navigator.clipboard.writeText(result.key).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center px-4 bg-black/60"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg bg-surface border border-line rounded-xl shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-line">
          <h2 className="text-base font-semibold text-ink">新密钥已创建</h2>
          <button
            onClick={onClose}
            className="w-7 h-7 flex items-center justify-center text-muted hover:text-ink rounded transition"
          >
            ✕
          </button>
        </div>

        <div className="px-5 py-4 space-y-4">
          <div className="bg-warn/10 border border-warn/40 rounded-lg p-3">
            <p className="text-xs text-warn font-medium">
              ⚠ 密钥明文仅显示一次，请立即复制保存。关闭后无法再次查看。
            </p>
          </div>

          <div>
            <label className="block text-xs text-muted mb-1">密钥明文</label>
            <div className="flex gap-2">
              <input
                readOnly
                value={result.key}
                className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                           font-mono focus:outline-none focus:border-accent transition"
              />
              <button
                onClick={handleCopy}
                className={`px-3 py-2 text-sm font-medium rounded-lg border transition whitespace-nowrap ${
                  copied
                    ? 'bg-ok/10 border-ok/40 text-ok'
                    : 'bg-accent text-white border-accent hover:bg-accent/80'
                }`}
              >
                {copied ? '已复制' : '复制'}
              </button>
            </div>
          </div>

          <div className="flex justify-end pt-1">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                         hover:bg-accent/80 transition"
            >
              我已保存，关闭
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Create-key form
// ------------------------------------------------------------------
interface CreateKeyFormProps {
  onCreated: (result: NewKeyResult) => void;
}

function CreateKeyForm({ onCreated }: CreateKeyFormProps) {
  const [label, setLabel] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setErr(null);
    try {
      const result = await createDispatchKey({ label: label.trim() || undefined });
      setLabel('');
      onCreated(result);
    } catch (error) {
      setErr(error instanceof Error ? error.message : '创建失败');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={(e) => { void handleSubmit(e); }}
      className="bg-surface border border-line rounded-xl p-4"
    >
      <h2 className="text-sm font-semibold text-ink mb-3">新建调度密钥</h2>
      <div className="flex flex-col sm:flex-row gap-2">
        <input
          type="text"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          placeholder="标签（选填）"
          className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <button
          type="submit"
          disabled={submitting}
          className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                     hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition whitespace-nowrap"
        >
          {submitting ? '创建中…' : '+ 新建密钥'}
        </button>
      </div>
      {err && <p className="text-xs text-err mt-2">{err}</p>}
    </form>
  );
}

// ------------------------------------------------------------------
// Key row (desktop table)
// ------------------------------------------------------------------
function KeyRow({
  k,
  onDisabled,
}: {
  k: DispatchKeyRecord;
  onDisabled: (id: string) => void;
}) {
  const [disabling, setDisabling] = useState(false);

  async function handleDisable() {
    if (!confirm(`确认停用密钥 ${k.prefix}…？`)) return;
    setDisabling(true);
    try {
      await disableDispatchKey(k.id);
      onDisabled(k.id);
    } catch {
      setDisabling(false);
    }
  }

  return (
    <tr className="border-t border-line hover:bg-line/30 transition">
      <td className="px-4 py-3 text-sm text-ink font-mono">{k.prefix}…</td>
      <td className="px-4 py-3 text-sm text-muted">{k.label || <span className="italic text-muted/50">无标签</span>}</td>
      <td className="px-4 py-3">
        <span
          className={`inline-flex items-center gap-1.5 text-xs font-medium ${
            k.enabled ? 'text-ok' : 'text-err'
          }`}
        >
          <span className={`w-1.5 h-1.5 rounded-full ${k.enabled ? 'bg-ok' : 'bg-err'}`} />
          {k.enabled ? '启用' : '已停用'}
        </span>
      </td>
      <td className="px-4 py-3 text-xs text-muted">{k.ownerId || '—'}</td>
      <td className="px-4 py-3">
        {k.enabled ? (
          <button
            onClick={() => { void handleDisable(); }}
            disabled={disabling}
            className="text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
          >
            {disabling ? '停用中…' : '停用'}
          </button>
        ) : (
          <span className="text-xs text-muted/50">—</span>
        )}
      </td>
    </tr>
  );
}

// ------------------------------------------------------------------
// Key card (mobile)
// ------------------------------------------------------------------
function KeyMobileCard({
  k,
  onDisabled,
}: {
  k: DispatchKeyRecord;
  onDisabled: (id: string) => void;
}) {
  const [disabling, setDisabling] = useState(false);

  async function handleDisable() {
    if (!confirm(`确认停用密钥 ${k.prefix}…？`)) return;
    setDisabling(true);
    try {
      await disableDispatchKey(k.id);
      onDisabled(k.id);
    } catch {
      setDisabling(false);
    }
  }

  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-ink font-mono truncate">
            {k.prefix}…
          </p>
          <p className="text-xs text-muted mt-0.5 truncate">
            {k.label || <span className="italic">无标签</span>}
          </p>
        </div>
        {k.enabled ? (
          <button
            onClick={() => { void handleDisable(); }}
            disabled={disabling}
            className="shrink-0 text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
          >
            {disabling ? '…' : '停用'}
          </button>
        ) : (
          <span className="shrink-0 text-xs text-err font-medium">已停用</span>
        )}
      </div>
      <div className="flex items-center gap-2 text-xs text-muted">
        <span className={`inline-block w-2 h-2 rounded-full ${k.enabled ? 'bg-ok' : 'bg-err'}`} />
        <span>{k.enabled ? '启用中' : '已停用'}</span>
        {k.ownerId && <span className="ml-2 truncate">归属: {k.ownerId}</span>}
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Keys page
// ------------------------------------------------------------------
export default function Keys() {
  const [keys, setKeys] = useState<DispatchKeyRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [newKeyResult, setNewKeyResult] = useState<NewKeyResult | null>(null);

  const fetchKeys = useCallback(async () => {
    try {
      const data = await listDispatchKeys();
      setKeys(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchKeys();
  }, [fetchKeys]);

  function handleCreated(result: NewKeyResult) {
    setNewKeyResult(result);
    void fetchKeys();
  }

  function handleDisabled(id: string) {
    setKeys((prev) =>
      prev.map((k) => (k.id === id ? { ...k, enabled: false } : k)),
    );
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold text-ink">调度密钥</h1>
        <span className="text-xs text-muted">
          {keys.filter((k) => k.enabled).length} / {keys.length} 启用
        </span>
      </div>

      {/* Create form */}
      <CreateKeyForm onCreated={handleCreated} />

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center min-h-32">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}

      {/* Error */}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">
          {error}
        </div>
      )}

      {/* Empty */}
      {!loading && !error && keys.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
          暂无调度密钥 — 使用上方表单新建
        </div>
      )}

      {/* Desktop table */}
      {!loading && !error && keys.length > 0 && (
        <>
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
            <table className="w-full text-left">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-4 py-3 font-medium">前缀</th>
                  <th className="px-4 py-3 font-medium">标签</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">归属</th>
                  <th className="px-4 py-3 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {keys.map((k) => (
                  <KeyRow key={k.id} k={k} onDisabled={handleDisabled} />
                ))}
              </tbody>
            </table>
          </div>

          {/* Mobile cards */}
          <div className="md:hidden space-y-3">
            {keys.map((k) => (
              <KeyMobileCard key={k.id} k={k} onDisabled={handleDisabled} />
            ))}
          </div>
        </>
      )}

      {/* New-key modal */}
      {newKeyResult && (
        <NewKeyModal
          result={newKeyResult}
          onClose={() => setNewKeyResult(null)}
        />
      )}
    </div>
  );
}
