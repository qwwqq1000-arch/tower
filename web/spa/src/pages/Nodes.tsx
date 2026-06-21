// ============================================================
// Tower SPA — Nodes page
// Table (desktop) / Cards (mobile): name/host/status/delete
// "Add node" form + "One-click provision" modal with job polling
// ============================================================
import { useEffect, useState, useRef, useCallback } from 'react';
import { listNodes, createNode, deleteNode, startProvision, getProvision } from '../api';
import type { NodeRecord, ProvisionJob } from '../types';

// ------------------------------------------------------------------
// Provision modal
// ------------------------------------------------------------------
interface ProvisionModalProps {
  onClose: () => void;
  onDone: () => void; // refresh nodes list after success
}

function ProvisionModal({ onClose, onDone }: ProvisionModalProps) {
  const [host, setHost] = useState('');
  const [port, setPort] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [job, setJob] = useState<ProvisionJob | null>(null);
  const [pollError, setPollError] = useState<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const pollJob = useCallback(async (jobId: string) => {
    try {
      const j = await getProvision(jobId);
      if (!mountedRef.current) return;
      setJob(j);
      if (j.status === 'done') {
        onDone();
        return;
      }
      if (j.status === 'failed') return;
      // still pending/running — poll again
      timerRef.current = setTimeout(() => {
        void pollJob(jobId);
      }, 1500);
    } catch (err) {
      if (!mountedRef.current) return;
      setPollError(err instanceof Error ? err.message : '轮询失败');
    }
  }, [onDone]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!host.trim()) return;
    setSubmitting(true);
    setPollError(null);
    try {
      const req = {
        host: host.trim(),
        ...(port.trim() ? { port: parseInt(port.trim(), 10) } : {}),
      };
      const newJob = await startProvision(req);
      setJob(newJob);
      // start polling
      timerRef.current = setTimeout(() => {
        void pollJob(newJob.id);
      }, 1500);
    } catch (err) {
      setPollError(err instanceof Error ? err.message : '启动失败');
      setSubmitting(false);
    }
  }

  const isRunning = job && (job.status === 'pending' || job.status === 'running');
  const isDone = job?.status === 'done';
  const isFailed = job?.status === 'failed';

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center px-4 bg-black/60"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg bg-surface border border-line rounded-xl shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-line">
          <h2 className="text-base font-semibold text-ink">一键开通节点</h2>
          <button
            onClick={onClose}
            className="w-7 h-7 flex items-center justify-center text-muted hover:text-ink rounded transition"
          >
            ✕
          </button>
        </div>

        {/* Body */}
        <div className="px-5 py-4 space-y-4">
          {/* Input form — only shown before submit */}
          {!job && (
            <form onSubmit={(e) => { void handleSubmit(e); }} className="space-y-3">
              <div>
                <label className="block text-xs text-muted mb-1">主机地址 *</label>
                <input
                  type="text"
                  value={host}
                  onChange={(e) => setHost(e.target.value)}
                  placeholder="192.168.1.100 或 example.com"
                  required
                  className="w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
              </div>
              <div>
                <label className="block text-xs text-muted mb-1">SSH 端口（默认 22）</label>
                <input
                  type="number"
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                  placeholder="22"
                  min={1}
                  max={65535}
                  className="w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
              </div>

              {pollError && (
                <p className="text-xs text-err">{pollError}</p>
              )}

              <div className="flex justify-end gap-2 pt-1">
                <button
                  type="button"
                  onClick={onClose}
                  className="px-4 py-2 text-sm text-muted hover:text-ink border border-line
                             rounded-lg transition"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={submitting || !host.trim()}
                  className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                             hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
                >
                  {submitting ? '提交中…' : '开始部署'}
                </button>
              </div>
            </form>
          )}

          {/* Job progress */}
          {job && (
            <div className="space-y-3">
              {/* Status badge */}
              <div className="flex items-center gap-2">
                <span
                  className={`inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full border ${
                    isDone
                      ? 'text-ok border-ok/40 bg-ok/10'
                      : isFailed
                        ? 'text-err border-err/40 bg-err/10'
                        : 'text-accent border-accent/40 bg-accent/10'
                  }`}
                >
                  {isRunning && (
                    <span className="inline-block w-1.5 h-1.5 rounded-full bg-accent animate-pulse" />
                  )}
                  {isDone ? '✓ 成功' : isFailed ? '✕ 失败' : '部署中…'}
                </span>
                <span className="text-xs text-muted">主机: {job.host}</span>
              </div>

              {/* Log output */}
              <div className="bg-bg border border-line rounded-lg p-3 max-h-56 overflow-y-auto font-mono text-xs text-muted whitespace-pre-wrap leading-relaxed">
                {job.log || '(等待输出…)'}
              </div>

              {pollError && (
                <p className="text-xs text-err">{pollError}</p>
              )}

              {/* Close / retry */}
              <div className="flex justify-end gap-2 pt-1">
                {isFailed && (
                  <button
                    onClick={() => { setJob(null); setSubmitting(false); }}
                    className="px-4 py-2 text-sm text-muted hover:text-ink border border-line
                               rounded-lg transition"
                  >
                    重试
                  </button>
                )}
                <button
                  onClick={onClose}
                  className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                             hover:bg-accent/80 transition"
                >
                  {isDone ? '完成' : '后台运行'}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Add-node form
// ------------------------------------------------------------------
interface AddNodeFormProps {
  onAdded: () => void;
}

function AddNodeForm({ onAdded }: AddNodeFormProps) {
  const [host, setHost] = useState('');
  const [port, setPort] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!host.trim()) return;
    setSubmitting(true);
    setErr(null);
    try {
      await createNode({
        host: host.trim(),
        ...(port.trim() ? { port: parseInt(port.trim(), 10) } : {}),
      });
      setHost('');
      setPort('');
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
          value={host}
          onChange={(e) => setHost(e.target.value)}
          placeholder="主机地址 *"
          required
          className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <input
          type="number"
          value={port}
          onChange={(e) => setPort(e.target.value)}
          placeholder="端口（选填）"
          min={1}
          max={65535}
          className="w-full sm:w-32 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <button
          type="submit"
          disabled={submitting || !host.trim()}
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
    if (!confirm(`确认删除节点 ${node.host}:${node.port}？`)) return;
    setDeleting(true);
    try {
      await deleteNode(node.id);
      onDelete(node.id);
    } catch {
      setDeleting(false);
    }
  }

  const isHealthy = node.auth_valid && node.server_state === 'running';
  const statusLabel = !node.auth_valid
    ? '封号'
    : node.server_state === 'running'
      ? '运行中'
      : node.server_state === 'stopped'
        ? '已停止'
        : node.server_state;

  const statusClass = !node.auth_valid
    ? 'text-err'
    : isHealthy
      ? 'text-ok'
      : 'text-muted';

  return (
    <tr className="border-t border-line hover:bg-line/30 transition">
      <td className="px-4 py-3 text-sm text-ink font-medium">{node.host}</td>
      <td className="px-4 py-3 text-sm text-muted">{node.port}</td>
      <td className="px-4 py-3">
        <span
          className={`inline-flex items-center gap-1.5 text-xs font-medium ${statusClass}`}
        >
          <span
            className={`w-1.5 h-1.5 rounded-full ${
              isHealthy ? 'bg-ok' : node.auth_valid ? 'bg-muted' : 'bg-err'
            }`}
          />
          {statusLabel}
        </span>
      </td>
      <td className="px-4 py-3">
        <span
          className={`text-xs ${node.auth_valid ? 'text-ok' : 'text-err'}`}
        >
          {node.auth_valid ? '有效' : '失效'}
        </span>
      </td>
      <td className="px-4 py-3 text-xs text-muted">
        {new Date(node.updated_at).toLocaleString('zh-CN')}
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
    if (!confirm(`确认删除节点 ${node.host}:${node.port}？`)) return;
    setDeleting(true);
    try {
      await deleteNode(node.id);
      onDelete(node.id);
    } catch {
      setDeleting(false);
    }
  }

  const isHealthy = node.auth_valid && node.server_state === 'running';
  const statusLabel = !node.auth_valid
    ? '封号'
    : node.server_state === 'running'
      ? '运行中'
      : node.server_state === 'stopped'
        ? '已停止'
        : node.server_state;

  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-ink truncate">
            {node.host}:{node.port}
          </p>
          <p className="text-xs text-muted mt-0.5 truncate">ID: {node.id}</p>
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
            isHealthy ? 'text-ok' : node.auth_valid ? 'text-muted' : 'text-err'
          }`}
        >
          <span
            className={`w-1.5 h-1.5 rounded-full ${
              isHealthy ? 'bg-ok' : node.auth_valid ? 'bg-muted' : 'bg-err'
            }`}
          />
          {statusLabel}
        </span>
        <span className={node.auth_valid ? 'text-ok' : 'text-err'}>
          密钥{node.auth_valid ? '有效' : '失效'}
        </span>
      </div>

      <p className="text-xs text-muted">
        {new Date(node.updated_at).toLocaleString('zh-CN')}
      </p>
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
  const [provisionOpen, setProvisionOpen] = useState(false);

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
        <button
          onClick={() => setProvisionOpen(true)}
          className="flex items-center gap-2 px-3 py-2 text-sm font-medium bg-accent text-white
                     rounded-lg hover:bg-accent/80 transition"
        >
          <span>⚡</span>
          <span>一键开通</span>
        </button>
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
          暂无节点 — 使用上方表单添加，或点击「一键开通」自动部署新节点
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
                  <th className="px-4 py-3 font-medium">主机</th>
                  <th className="px-4 py-3 font-medium">端口</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">密钥</th>
                  <th className="px-4 py-3 font-medium">更新时间</th>
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

      {/* Provision modal */}
      {provisionOpen && (
        <ProvisionModal
          onClose={() => setProvisionOpen(false)}
          onDone={() => {
            setProvisionOpen(false);
            void fetchNodes();
          }}
        />
      )}
    </div>
  );
}
