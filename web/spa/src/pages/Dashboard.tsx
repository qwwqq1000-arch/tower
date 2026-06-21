// ============================================================
// Tower SPA — Dashboard page
// GET /api/dashboard → {nodes} → derive stats locally
// ============================================================
import { useEffect, useState } from 'react';
import { getDashboard } from '../api';
import type { NodeRecord } from '../types';

// ------------------------------------------------------------------
// Stat card
// ------------------------------------------------------------------
interface StatCardProps {
  label: string;
  value: number | string;
  accent?: boolean;
  warn?: boolean;
}

function StatCard({ label, value, accent, warn }: StatCardProps) {
  const valueClass = accent
    ? 'text-accent'
    : warn
      ? 'text-warn'
      : 'text-ink';
  return (
    <div className="bg-surface border border-line rounded-xl p-4 flex flex-col gap-1">
      <span className="text-xs text-muted uppercase tracking-wide">{label}</span>
      <span className={`text-2xl font-bold ${valueClass}`}>{value}</span>
    </div>
  );
}

// ------------------------------------------------------------------
// Node card
// ------------------------------------------------------------------
function NodeCard({ node }: { node: NodeRecord }) {
  const isHealthy = node.enabled;
  const statusColor = isHealthy ? 'text-ok' : 'text-muted';

  const statusLabel = node.status
    ? node.status
    : node.enabled ? '运行中' : '已停用';

  return (
    <div className="bg-surface border border-line rounded-xl p-4 flex flex-col gap-2 hover:border-accent/50 transition">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-ink truncate">
            {node.name}
          </p>
          <p className="text-xs text-muted mt-0.5 truncate">{node.baseUrl}</p>
        </div>
        <span
          className={`shrink-0 text-xs font-medium px-2 py-0.5 rounded-full border ${statusColor} border-current`}
        >
          {statusLabel}
        </span>
      </div>

      <div className="flex items-center gap-2 text-xs text-muted">
        <span
          className={`inline-block w-2 h-2 rounded-full ${node.enabled ? 'bg-ok' : 'bg-muted'}`}
        />
        <span>{node.enabled ? '已启用' : '已停用'}</span>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Dashboard page
// ------------------------------------------------------------------
export default function Dashboard() {
  const [nodes, setNodes] = useState<NodeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const data = await getDashboard();
        if (!cancelled) {
          setNodes(data.nodes);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : '加载失败');
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  // ------ loading ------
  if (loading) {
    return (
      <div className="p-6 flex items-center justify-center min-h-64">
        <span className="text-muted animate-pulse">加载中…</span>
      </div>
    );
  }

  // ------ error ------
  if (error) {
    return (
      <div className="p-6">
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">
          {error}
        </div>
      </div>
    );
  }

  const total = nodes.length;
  const enabled = nodes.filter((n) => n.enabled).length;

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Page title */}
      <h1 className="text-2xl font-semibold text-ink">看板</h1>

      {/* Top stats */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">
          总览
        </h2>
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
          <StatCard label="节点总数" value={total} />
          <StatCard label="启用节点" value={enabled} accent />
        </div>
      </section>

      {/* Node grid */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">
          节点状态
        </h2>

        {nodes.length === 0 ? (
          <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
            暂无节点 — 前往「节点」页面添加新节点
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
            {nodes.map((node) => (
              <NodeCard key={node.id} node={node} />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
