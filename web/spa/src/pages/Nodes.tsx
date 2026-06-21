// ============================================================
// Tower SPA — Nodes page
// Table (desktop) / Cards (mobile): name/baseUrl/enabled/delete
// "Add node" form
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { listNodes, createNode, deleteNode } from '../api';
import type { NodeRecord } from '../types';

// ------------------------------------------------------------------
// Add-node form
// ------------------------------------------------------------------
interface AddNodeFormProps {
  onAdded: () => void;
}

function AddNodeForm({ onAdded }: AddNodeFormProps) {
  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim() || !baseUrl.trim()) return;
    setSubmitting(true);
    setErr(null);
    try {
      await createNode({
        name: name.trim(),
        baseUrl: baseUrl.trim(),
        ...(apiKey.trim() ? { apiKey: apiKey.trim() } : {}),
      });
      setName('');
      setBaseUrl('');
      setApiKey('');
      onAdded();
    } catch (error) {
      setErr(error instanceof Error ? error.message : '添加失败');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={(e) => { void handleSubmit(e); }}
      className="bg-surface border border-line rounded-xl p-4"
    >
      <h2 className="text-sm font-semibold text-ink mb-3">添加节点</h2>
      <div className="flex flex-col sm:flex-row gap-2">
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="节点名称 *"
          required
          className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <input
          type="text"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          placeholder="接入地址 * (如 http://ip:3456)"
          required
          className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <input
          type="text"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder="API密钥（选填）"
          className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <button
          type="submit"
          disabled={submitting || !name.trim() || !baseUrl.trim()}
          className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                     hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition whitespace-nowrap"
        >
          {submitting ? '添加中…' : '+ 添加'}
        </button>
      </div>
      {err && <p className="text-xs text-err mt-2">{err}</p>}
    </form>
  );
}

// ------------------------------------------------------------------
// Node row (desktop table)
// ------------------------------------------------------------------
function NodeRow({
  node,
  onDelete,
}: {
  node: NodeRecord;
  onDelete: (id: string) => void;
}) {
  const [deleting, setDeleting] = useState(false);

  async function handleDelete() {
    if (!confirm(`确认删除节点 ${node.name}？`)) return;
    setDeleting(true);
    try {
      await deleteNode(node.id);
      onDelete(node.id);
    } catch {
      setDeleting(false);
    }
  }

  const statusLabel = node.status
    ? node.status
    : node.enabled ? '运行中' : '已停用';

  const statusClass = node.enabled ? 'text-ok' : 'text-muted';

  return (
    <tr className="border-t border-line hover:bg-line/30 transition">
      <td className="px-4 py-3 text-sm text-ink font-medium">{node.name}</td>
      <td className="px-4 py-3 text-sm text-muted">{node.baseUrl}</td>
      <td className="px-4 py-3">
        <span
          className={`inline-flex items-center gap-1.5 text-xs font-medium ${statusClass}`}
        >
          <span
            className={`w-1.5 h-1.5 rounded-full ${
              node.enabled ? 'bg-ok' : 'bg-muted'
            }`}
          />
          {statusLabel}
        </span>
      </td>
      <td className="px-4 py-3">
        <span className={`text-xs ${node.enabled ? 'text-ok' : 'text-muted'}`}>
          {node.enabled ? '启用' : '禁用'}
        </span>
      </td>
      <td className="px-4 py-3">
        <button
          onClick={() => { void handleDelete(); }}
          disabled={deleting}
          className="text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
        >
          {deleting ? '删除中…' : '删除'}
        </button>
      </td>
    </tr>
  );
}

// ------------------------------------------------------------------
// Node card (mobile)
// ------------------------------------------------------------------
function NodeMobileCard({
  node,
  onDelete,
}: {
  node: NodeRecord;
  onDelete: (id: string) => void;
}) {
  const [deleting, setDeleting] = useState(false);

  async function handleDelete() {
    if (!confirm(`确认删除节点 ${node.name}？`)) return;
    setDeleting(true);
    try {
      await deleteNode(node.id);
      onDelete(node.id);
    } catch {
      setDeleting(false);
    }
  }

  const statusLabel = node.status
    ? node.status
    : node.enabled ? '运行中' : '已停用';

  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-ink truncate">
            {node.name}
          </p>
          <p className="text-xs text-muted mt-0.5 truncate">{node.baseUrl}</p>
        </div>
        <button
          onClick={() => { void handleDelete(); }}
          disabled={deleting}
          className="shrink-0 text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
        >
          {deleting ? '…' : '删除'}
        </button>
      </div>

      <div className="flex items-center gap-4 text-xs text-muted">
        <span
          className={`flex items-center gap-1 ${
            node.enabled ? 'text-ok' : 'text-muted'
          }`}
        >
          <span
            className={`w-1.5 h-1.5 rounded-full ${
              node.enabled ? 'bg-ok' : 'bg-muted'
            }`}
          />
          {statusLabel}
        </span>
        <span className={node.enabled ? 'text-ok' : 'text-muted'}>
          {node.enabled ? '启用' : '禁用'}
        </span>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Nodes page
// ------------------------------------------------------------------
export default function Nodes() {
  const [nodes, setNodes] = useState<NodeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchNodes = useCallback(async () => {
    try {
      const data = await listNodes();
      setNodes(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchNodes();
  }, [fetchNodes]);

  function handleDelete(id: string) {
    setNodes((prev) => prev.filter((n) => n.id !== id));
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold text-ink">节点</h1>
      </div>

      {/* Add node form */}
      <AddNodeForm onAdded={() => { void fetchNodes(); }} />

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
      {!loading && !error && nodes.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
          暂无节点 — 使用上方表单添加新节点
        </div>
      )}

      {/* Desktop table */}
      {!loading && !error && nodes.length > 0 && (
        <>
          {/* Table: hidden on mobile */}
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
            <table className="w-full text-left">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-4 py-3 font-medium">名称</th>
                  <th className="px-4 py-3 font-medium">地址</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">启用</th>
                  <th className="px-4 py-3 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {nodes.map((node) => (
                  <NodeRow key={node.id} node={node} onDelete={handleDelete} />
                ))}
              </tbody>
            </table>
          </div>

          {/* Cards: visible only on mobile */}
          <div className="md:hidden space-y-3">
            {nodes.map((node) => (
              <NodeMobileCard
                key={node.id}
                node={node}
                onDelete={handleDelete}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
