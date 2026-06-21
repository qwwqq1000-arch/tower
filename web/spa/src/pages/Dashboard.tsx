// ============================================================
// Tower SPA — Dashboard page
// GET /api/dashboard → node card grid + top stats
// ============================================================
import { useEffect, useState } from 'react';
import { getDashboard, listNodes } from '../api';
import type { DashboardStats, NodeRecord } from '../types';

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
  const isHealthy = node.auth_valid && node.server_state === 'running';
  const statusColor = isHealthy
    ? 'text-ok'
    : node.server_state === 'stopped'
      ? 'text-muted'
      : 'text-err';

  const statusLabel = !node.auth_valid
    ? '封号'
    : node.server_state === 'running'
      ? '运行中'
      : node.server_state === 'stopped'
        ? '已停止'
        : node.server_state;

  return (
    <div className="bg-surface border border-line rounded-xl p-4 flex flex-col gap-2 hover:border-accent/50 transition">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-ink truncate">
            {node.host}:{node.port}
          </p>
          <p className="text-xs text-muted mt-0.5 truncate">ID: {node.id}</p>
        </div>
        <span
          className={`shrink-0 text-xs font-medium px-2 py-0.5 rounded-full border ${statusColor} border-current`}
        >
          {statusLabel}
        </span>
      </div>

      <div className="flex items-center gap-2 text-xs text-muted">
        <span
          className={`inline-block w-2 h-2 rounded-full ${node.auth_valid ? 'bg-ok' : 'bg-err'}`}
        />
        <span>{node.auth_valid ? '密钥有效' : '密钥失效'}</span>
      </div>

      <p className="text-xs text-muted">
        更新: {new Date(node.updated_at).toLocaleString('zh-CN')}
      </p>
    </div>
  );
}

// ------------------------------------------------------------------
// Dashboard page
// ------------------------------------------------------------------
export default function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [nodes, setNodes] = useState<NodeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const [s, n] = await Promise.all([getDashboard(), listNodes()]);
        if (!cancelled) {
          setStats(s);
          setNodes(n);
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

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Page title */}
      <h1 className="text-2xl font-semibold text-ink">看板</h1>

      {/* Top stats */}
      {stats && (
        <section>
          <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">
            总览
          </h2>
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
            <StatCard label="节点总数" value={stats.nodes_total} />
            <StatCard label="健康节点" value={stats.nodes_healthy} accent />
            <StatCard label="密钥总数" value={stats.keys_total} />
            <StatCard label="今日请求" value={stats.requests_today} />
            <StatCard
              label="今日错误"
              value={stats.errors_today}
              warn={stats.errors_today > 0}
            />
            <StatCard
              label="P99 延迟"
              value={`${stats.latency_p99_ms} ms`}
            />
          </div>
        </section>
      )}

      {/* Node grid */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">
          节点状态
        </h2>

        {nodes.length === 0 ? (
          <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
            暂无节点 — 前往「节点」页面添加或开通新节点
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
