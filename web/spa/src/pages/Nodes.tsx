// ============================================================
// Tower SPA — Nodes page
// Table (desktop) / Cards (mobile): name/baseUrl/enabled/delete
// "Add node" form + multi-select + batch operations
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { listNodes, createNode, deleteNode, setNodeEnabled, refreshNode } from '../api';
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
// Batch action bar
// ------------------------------------------------------------------
interface BatchBarProps {
  selectedCount: number;
  onEnableAll: () => void;
  onDisableAll: () => void;
  onRefreshAll: () => void;
  batchRunning: boolean;
  batchResult: string | null;
}

function BatchBar({ selectedCount, onEnableAll, onDisableAll, onRefreshAll, batchRunning, batchResult }: BatchBarProps) {
  return (
    <div className="flex items-center gap-3 flex-wrap bg-accent/10 border border-accent/30 rounded-xl px-4 py-2.5">
      <span className="text-sm text-ink font-medium">已选 {selectedCount} 个节点</span>
      <div className="flex items-center gap-2 ml-auto flex-wrap">
        <button
          onClick={onEnableAll}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-ok text-white rounded-lg
                     hover:bg-ok/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          批量启用
        </button>
        <button
          onClick={onDisableAll}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-err text-white rounded-lg
                     hover:bg-err/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          批量停用
        </button>
        <button
          onClick={onRefreshAll}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-ink rounded-lg
                     hover:bg-line/30 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          批量刷新 token
        </button>
      </div>
      {batchRunning && (
        <span className="text-xs text-muted animate-pulse">处理中…</span>
      )}
      {batchResult && !batchRunning && (
        <span className="text-xs text-ok">{batchResult}</span>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// Node row (desktop table)
// ------------------------------------------------------------------
function NodeRow({
  node,
  selected,
  onSelect,
  onDelete,
}: {
  node: NodeRecord;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
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
      <td className="px-4 py-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={(e) => onSelect(node.id, e.target.checked)}
          className="rounded border-line accent-accent cursor-pointer"
        />
      </td>
      <td className="px-4 py-3 text-sm font-medium">
        <Link
          to={`/nodes/${node.id}`}
          className="text-accent hover:underline"
        >
          {node.name}
        </Link>
      </td>
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
  selected,
  onSelect,
  onDelete,
}: {
  node: NodeRecord;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
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
    <div className={`bg-surface border rounded-xl p-4 space-y-2 ${selected ? 'border-accent' : 'border-line'}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-start gap-2 min-w-0">
          <input
            type="checkbox"
            checked={selected}
            onChange={(e) => onSelect(node.id, e.target.checked)}
            className="mt-0.5 rounded border-line accent-accent cursor-pointer shrink-0"
          />
          <div className="min-w-0">
            <Link
              to={`/nodes/${node.id}`}
              className="text-sm font-semibold text-accent hover:underline truncate block"
            >
              {node.name}
            </Link>
            <p className="text-xs text-muted mt-0.5 truncate">{node.baseUrl}</p>
          </div>
        </div>
        <button
          onClick={() => { void handleDelete(); }}
          disabled={deleting}
          className="shrink-0 text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
        >
          {deleting ? '…' : '删除'}
        </button>
      </div>

      <div className="flex items-center gap-4 text-xs text-muted pl-6">
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

  // Multi-select state
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchRunning, setBatchRunning] = useState(false);
  const [batchResult, setBatchResult] = useState<string | null>(null);

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
    setSelected((prev) => { const s = new Set(prev); s.delete(id); return s; });
  }

  function handleSelect(id: string, checked: boolean) {
    setSelected((prev) => {
      const s = new Set(prev);
      if (checked) s.add(id); else s.delete(id);
      return s;
    });
  }

  const allSelected = nodes.length > 0 && selected.size === nodes.length;
  const someSelected = selected.size > 0 && !allSelected;

  function handleSelectAll(checked: boolean) {
    if (checked) {
      setSelected(new Set(nodes.map((n) => n.id)));
    } else {
      setSelected(new Set());
    }
  }

  async function runBatch(op: (id: string) => Promise<void>, label: string) {
    setBatchRunning(true);
    setBatchResult(null);
    const ids = Array.from(selected);
    let ok = 0;
    let fail = 0;
    await Promise.allSettled(
      ids.map((id) =>
        op(id)
          .then(() => { ok++; })
          .catch(() => { fail++; }),
      ),
    );
    setBatchResult(`${label}: ${ok} 成功, ${fail} 失败`);
    setBatchRunning(false);
    void fetchNodes();
  }

  function handleBatchEnable() {
    void runBatch((id) => setNodeEnabled(id, true), '批量启用');
  }

  function handleBatchDisable() {
    void runBatch((id) => setNodeEnabled(id, false), '批量停用');
  }

  function handleBatchRefresh() {
    void runBatch((id) => refreshNode(id), '批量刷新');
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold text-ink">节点</h1>
      </div>

      {/* Add node form */}
      <AddNodeForm onAdded={() => { void fetchNodes(); }} />

      {/* Batch action bar */}
      {selected.size > 0 && (
        <BatchBar
          selectedCount={selected.size}
          onEnableAll={handleBatchEnable}
          onDisableAll={handleBatchDisable}
          onRefreshAll={handleBatchRefresh}
          batchRunning={batchRunning}
          batchResult={batchResult}
        />
      )}

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
                  <th className="px-4 py-3 font-medium">
                    <input
                      type="checkbox"
                      checked={allSelected}
                      ref={(el) => {
                        if (el) el.indeterminate = someSelected;
                      }}
                      onChange={(e) => handleSelectAll(e.target.checked)}
                      className="rounded border-line accent-accent cursor-pointer"
                    />
                  </th>
                  <th className="px-4 py-3 font-medium">名称</th>
                  <th className="px-4 py-3 font-medium">地址</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">启用</th>
                  <th className="px-4 py-3 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {nodes.map((node) => (
                  <NodeRow
                    key={node.id}
                    node={node}
                    selected={selected.has(node.id)}
                    onSelect={handleSelect}
                    onDelete={handleDelete}
                  />
                ))}
              </tbody>
            </table>
          </div>

          {/* Cards: visible only on mobile */}
          <div className="md:hidden space-y-3">
            <div className="flex items-center gap-2 px-1">
              <input
                type="checkbox"
                checked={allSelected}
                ref={(el) => {
                  if (el) el.indeterminate = someSelected;
                }}
                onChange={(e) => handleSelectAll(e.target.checked)}
                className="rounded border-line accent-accent cursor-pointer"
              />
              <span className="text-xs text-muted">全选</span>
            </div>
            {nodes.map((node) => (
              <NodeMobileCard
                key={node.id}
                node={node}
                selected={selected.has(node.id)}
                onSelect={handleSelect}
                onDelete={handleDelete}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
