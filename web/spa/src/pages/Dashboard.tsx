// ============================================================
// Tower SPA — Dashboard page (comprehensive rewrite)
// GET /api/dashboard → DashboardData (multi-card overview)
// ============================================================
import { useEffect, useState, useCallback, useMemo } from 'react';
import { getDashboard } from '../api';
import type { DashboardData, DashboardNodeItem, DashboardByModel, DashboardHostingRow } from '../types';
import { useAuth } from '../auth';
import { statusLabel } from '../lib/status';
import { TenantDashboard } from './tenant';

// ------------------------------------------------------------------
// Formatters
// ------------------------------------------------------------------
function fmtCost(usd: number): string {
  if (usd === 0) return '$0.00';
  if (Math.abs(usd) < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

function fmtTokens(n: number): string {
  return n.toLocaleString();
}

function fmtPct(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`;
}

// ------------------------------------------------------------------
// Stat card
// ------------------------------------------------------------------
// ------------------------------------------------------------------
// Quota helpers
// ------------------------------------------------------------------
function fmtQuota(v: number | undefined): string {
  if (v == null) return '—';
  return `${(v * 100).toFixed(0)}%`;
}

function quotaValueClass(v: number | undefined): string {
  if (v == null || v === 0) return 'text-ink';
  if (v >= 0.9) return 'text-err';
  if (v >= 0.7) return 'text-warn';
  return 'text-ok';
}

// ------------------------------------------------------------------
interface StatCardProps {
  label: string;
  value: string | number;
  sub?: string;
  accent?: boolean;
  warn?: boolean;
  valueClass?: string;
}

function StatCard({ label, value, sub, accent, warn, valueClass: valueClassProp }: StatCardProps) {
  const valueClass = valueClassProp ?? (accent ? 'text-accent' : warn ? 'text-warn' : 'text-ink');
  return (
    <div className="bg-surface border border-line rounded-xl p-4 flex flex-col gap-1">
      <span className="text-xs text-muted uppercase tracking-wide">{label}</span>
      <span className={`text-2xl font-bold ${valueClass}`}>{value}</span>
      {sub && <span className="text-xs text-muted">{sub}</span>}
    </div>
  );
}

// ------------------------------------------------------------------
// Status badge
// ------------------------------------------------------------------
const STATUS_COLOR: Record<string, string> = {
  active: 'bg-ok/20 text-ok border-ok/30',
  banned: 'bg-err/20 text-err border-err/30',
  half_open: 'bg-warn/20 text-warn border-warn/30',
  permanent: 'bg-err/30 text-err border-err/50',
  offline: 'bg-muted/20 text-muted border-muted/30',
  disabled: 'bg-muted/10 text-muted border-muted/20',
};

function StatusBadge({ status, count }: { status: string; count: number }) {
  const cls = STATUS_COLOR[status] ?? 'bg-muted/10 text-muted border-muted/20';
  return (
    <span className={`inline-flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full border ${cls}`}>
      {statusLabel(status)}
      <span className="font-bold">{count}</span>
    </span>
  );
}

// ------------------------------------------------------------------
// Node row (compact list)
// ------------------------------------------------------------------
function NodeRow({ node }: { node: DashboardNodeItem }) {
  const cls = STATUS_COLOR[node.status] ?? 'bg-muted/10 text-muted border-muted/20';
  return (
    <tr className="border-t border-line hover:bg-surface/60 transition-colors">
      <td className="py-2 pr-3 pl-1">
        <p className="text-sm font-medium text-ink truncate max-w-[160px]">{node.name}</p>
        {node.region && <p className="text-xs text-muted">{node.region}</p>}
      </td>
      <td className="py-2 pr-3 text-xs text-muted truncate max-w-[180px]">{node.baseUrl}</td>
      <td className="py-2 pr-3">
        <span className={`inline-block text-xs font-medium px-2 py-0.5 rounded-full border ${cls}`}>
          {node.status ? statusLabel(node.status) : statusLabel(node.enabled ? 'active' : 'disabled')}
        </span>
      </td>
      <td className="py-2 text-xs text-muted">{node.version || '—'}</td>
    </tr>
  );
}

// ------------------------------------------------------------------
// By-model table row
// ------------------------------------------------------------------
function ModelRow({ row }: { row: DashboardByModel }) {
  return (
    <tr className="border-t border-line hover:bg-surface/60 transition-colors">
      <td className="py-2 pr-3 pl-1 text-sm font-medium text-ink truncate max-w-[200px]">{row.model}</td>
      <td className="py-2 pr-3 text-sm text-ink text-right">{row.requests.toLocaleString()}</td>
      <td className="py-2 pr-3 text-sm text-muted text-right">{fmtTokens(row.tokensIn)}</td>
      <td className="py-2 pr-3 text-sm text-muted text-right">{fmtTokens(row.tokensOut)}</td>
      <td className="py-2 text-sm text-ink text-right font-mono">{fmtCost(row.costUsd)}</td>
    </tr>
  );
}

// ------------------------------------------------------------------
// Hosting fee table row
// ------------------------------------------------------------------
function HostingRow({ row }: { row: DashboardHostingRow }) {
  return (
    <tr className="border-t border-line hover:bg-surface/60 transition-colors">
      <td className="py-2 pr-3 pl-1 text-sm font-medium text-ink">{row.username}</td>
      <td className="py-2 pr-3 text-xs text-muted">{row.role}</td>
      <td className="py-2 pr-3 text-sm text-ink text-right font-mono">{fmtCost(row.consumptionUsd)}</td>
      <td className="py-2 pr-3 text-sm text-muted text-right">{(row.rate * 100).toFixed(1)}%</td>
      <td className="py-2 pr-3 text-sm text-ink text-right font-mono">{fmtCost(row.feeUsd)}</td>
      <td className="py-2 text-sm text-accent text-right font-mono font-medium">{fmtCost(row.unsettledUsd)}</td>
    </tr>
  );
}

// ------------------------------------------------------------------
// Shared pagination for dashboard tables
// ------------------------------------------------------------------
// 10/page so lists like the 节点列表 (17 nodes) actually paginate — 25 was too
// coarse and the pager never appeared. Matches the dispatch panel's page size.
const PAGE_SIZE = 10;

function PaginationBar({ page, total, pageSize, onPrev, onNext }: {
  page: number; total: number; pageSize: number;
  onPrev: () => void; onNext: () => void;
}) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  return (
    <div className="flex items-center justify-between text-xs text-muted px-3 py-2 border-t border-line">
      <button
        onClick={onPrev} disabled={page === 0}
        className="px-3 py-1.5 border border-line rounded-lg hover:text-ink hover:border-accent transition disabled:opacity-40"
      >上一页</button>
      <span>第 {page + 1} / {totalPages} 页 · 共 {total} 条</span>
      <button
        onClick={onNext} disabled={(page + 1) * pageSize >= total}
        className="px-3 py-1.5 border border-line rounded-lg hover:text-ink hover:border-accent transition disabled:opacity-40"
      >下一页</button>
    </div>
  );
}

// ------------------------------------------------------------------
// Dashboard page
// ------------------------------------------------------------------
const REFRESH_INTERVAL_MS = 30_000;

export default function Dashboard() {
  const { isTenant } = useAuth();
  if (isTenant) return <TenantDashboard />;
  return <AdminDashboard />;
}

function AdminDashboard() {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [modelPage, setModelPage] = useState(0);
  const [hostingPage, setHostingPage] = useState(0);
  const [nodePage, setNodePage] = useState(0);

  const load = useCallback(async (showLoading = true) => {
    if (showLoading) setLoading(true);
    setError(null);
    try {
      const d = await getDashboard();
      setData(d);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      if (showLoading) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(true);
    const timer = setInterval(() => void load(false), REFRESH_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [load]);

  const pagedModels = useMemo(
    () => data ? data.today.byModel.slice(modelPage * PAGE_SIZE, (modelPage + 1) * PAGE_SIZE) : [],
    [data, modelPage],
  );
  const pagedHosting = useMemo(
    () => data ? data.hosting.slice(hostingPage * PAGE_SIZE, (hostingPage + 1) * PAGE_SIZE) : [],
    [data, hostingPage],
  );
  const pagedNodes = useMemo(
    () => data ? data.nodes.list.slice(nodePage * PAGE_SIZE, (nodePage + 1) * PAGE_SIZE) : [],
    [data, nodePage],
  );

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
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      </div>
    );
  }

  if (!data) return null;

  const { nodes, accounts, today, hosting, totalCostUsd, channelTodayCostUsd, channelTotalCostUsd, quota5hAvg, quota7dAvg } = data;
  const byStatus = nodes.byStatus ?? {};

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Page title */}
      <h1 className="text-2xl font-semibold text-ink">看板</h1>

      {/* ---- Top stat cards ---- */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">总览</h2>
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
          <StatCard
            label="号库"
            value={`${accounts.enabled} / ${accounts.total}`}
            sub="启用 / 总数"
            accent
          />
          <StatCard
            label="今日请求"
            value={today.requests.toLocaleString()}
            sub={`成功率 ${fmtPct(today.successRate)}`}
            accent={today.successRate >= 0.95}
            warn={today.successRate < 0.8}
          />
          <StatCard
            label="号库今日消费"
            value={fmtCost(today.costUsd)}
          />
          <StatCard
            label="号库总消费"
            value={fmtCost(totalCostUsd)}
          />
          <StatCard
            label="渠道今日消费"
            value={fmtCost(channelTodayCostUsd ?? 0)}
          />
          <StatCard
            label="渠道总消费"
            value={fmtCost(channelTotalCostUsd ?? 0)}
          />
          <StatCard
            label="5h 均额度"
            value={fmtQuota(quota5hAvg)}
            valueClass={quotaValueClass(quota5hAvg)}
          />
          <StatCard
            label="7d 均额度"
            value={fmtQuota(quota7dAvg)}
            valueClass={quotaValueClass(quota7dAvg)}
          />
        </div>
      </section>

      {/* ---- Node status distribution ---- */}
      {Object.keys(byStatus).length > 0 && (
        <section>
          <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">节点状态分布</h2>
          <div className="bg-surface border border-line rounded-xl p-4 flex flex-wrap gap-2">
            {Object.entries(byStatus).map(([status, count]) => (
              <StatusBadge key={status} status={status} count={count} />
            ))}
          </div>
        </section>
      )}

      {/* ---- Today by-model table ---- */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">今日按模型</h2>
        <div className="bg-surface border border-line rounded-xl overflow-x-auto">
          {today.byModel.length === 0 ? (
            <p className="p-6 text-center text-sm text-muted">今日暂无请求数据</p>
          ) : (
            <>
              <table className="w-full text-left">
                <thead>
                  <tr>
                    <th className="py-2 pr-3 pl-3 text-xs text-muted font-medium">模型</th>
                    <th className="py-2 pr-3 text-xs text-muted font-medium text-right">请求</th>
                    <th className="py-2 pr-3 text-xs text-muted font-medium text-right">入 Token</th>
                    <th className="py-2 pr-3 text-xs text-muted font-medium text-right">出 Token</th>
                    <th className="py-2 pr-3 text-xs text-muted font-medium text-right">成本</th>
                  </tr>
                </thead>
                <tbody>
                  {pagedModels.map((row) => (
                    <ModelRow key={row.model} row={row} />
                  ))}
                </tbody>
              </table>
              {today.byModel.length > PAGE_SIZE && (
                <PaginationBar
                  page={modelPage} total={today.byModel.length} pageSize={PAGE_SIZE}
                  onPrev={() => setModelPage((p) => Math.max(0, p - 1))}
                  onNext={() => setModelPage((p) => p + 1)}
                />
              )}
            </>
          )}
        </div>
      </section>

      {/* ---- Hosting fee table ---- */}
      {hosting.length > 0 && (
        <section>
          <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">托管费用</h2>
          <div className="bg-surface border border-line rounded-xl overflow-x-auto">
            <table className="w-full text-left">
              <thead>
                <tr>
                  <th className="py-2 pr-3 pl-3 text-xs text-muted font-medium">用户</th>
                  <th className="py-2 pr-3 text-xs text-muted font-medium">角色</th>
                  <th className="py-2 pr-3 text-xs text-muted font-medium text-right">累计消耗</th>
                  <th className="py-2 pr-3 text-xs text-muted font-medium text-right">费率</th>
                  <th className="py-2 pr-3 text-xs text-muted font-medium text-right">应收费</th>
                  <th className="py-2 text-xs text-muted font-medium text-right">未结算</th>
                </tr>
              </thead>
              <tbody>
                {pagedHosting.map((row) => (
                  <HostingRow key={row.tenantId} row={row} />
                ))}
              </tbody>
            </table>
            {hosting.length > PAGE_SIZE && (
              <PaginationBar
                page={hostingPage} total={hosting.length} pageSize={PAGE_SIZE}
                onPrev={() => setHostingPage((p) => Math.max(0, p - 1))}
                onNext={() => setHostingPage((p) => p + 1)}
              />
            )}
          </div>
        </section>
      )}

      {/* ---- Compact node list ---- */}
      <section>
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide mb-3">节点列表</h2>
        <div className="bg-surface border border-line rounded-xl overflow-x-auto">
          {nodes.list.length === 0 ? (
            <p className="p-6 text-center text-sm text-muted">暂无节点 — 前往「节点」页面添加</p>
          ) : (
            <>
              <table className="w-full text-left">
                <thead>
                  <tr>
                    <th className="py-2 pr-3 pl-3 text-xs text-muted font-medium">名称</th>
                    <th className="py-2 pr-3 text-xs text-muted font-medium">地址</th>
                    <th className="py-2 pr-3 text-xs text-muted font-medium">状态</th>
                    <th className="py-2 text-xs text-muted font-medium">版本</th>
                  </tr>
                </thead>
                <tbody>
                  {pagedNodes.map((node) => (
                    <NodeRow key={node.id} node={node} />
                  ))}
                </tbody>
              </table>
              {nodes.list.length > PAGE_SIZE && (
                <PaginationBar
                  page={nodePage} total={nodes.list.length} pageSize={PAGE_SIZE}
                  onPrev={() => setNodePage((p) => Math.max(0, p - 1))}
                  onNext={() => setNodePage((p) => p + 1)}
                />
              )}
            </>
          )}
        </div>
      </section>
    </div>
  );
}
