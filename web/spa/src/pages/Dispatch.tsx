// ============================================================
// Tower SPA — Dispatch real-time dashboard
// SSE primary; polling fallback on error
// ============================================================
import { useEffect, useRef, useState } from 'react';
import { getDispatchStatus, listFallbackChannels, listAccounts, getServerStatus } from '../api';
import type { DispatchStatus, DispatchAccountSnapshot, DispatchEvent, DispatchFallbackChannel, ServerStatus } from '../types';
import { useAuth } from '../auth';
import { TenantDispatch } from './tenant';

// ------------------------------------------------------------------
// Badge
// ------------------------------------------------------------------
function StatusBadge({ status }: { status: string }) {
  const colorMap: Record<string, string> = {
    active:    'bg-green-500/20 text-green-400 border-green-500/40',
    banned:    'bg-red-500/20 text-red-400 border-red-500/40',
    half_open: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40',
    offline:   'bg-gray-500/20 text-gray-400 border-gray-500/40',
    disabled:  'bg-gray-500/10 text-gray-500 border-gray-500/20',
  };
  const cls = colorMap[status] ?? colorMap['offline'];
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded border text-xs font-mono ${cls}`}>
      {status}
    </span>
  );
}

// ------------------------------------------------------------------
// Top stats bar
// ------------------------------------------------------------------
function StatsBar({ data }: { data: DispatchStatus }) {
  const { nodes, traffic, accounts } = data;
  const inflight = accounts.reduce((s, a) => s + a.inflight, 0);
  const rate = traffic.total > 0
    ? ((traffic.ok / traffic.total) * 100).toFixed(1)
    : '—';

  const stats = [
    { label: '节点 (启用/总)', value: `${nodes.enabled} / ${nodes.total}` },
    { label: '总请求', value: traffic.total.toLocaleString() },
    { label: '成功率', value: traffic.total > 0 ? `${rate}%` : '—' },
    { label: '并发中', value: inflight.toString() },
    { label: 'RPM', value: traffic.rpm != null ? traffic.rpm.toString() : '—' },
  ];

  return (
    <div className="grid grid-cols-2 sm:grid-cols-5 gap-4 mb-6">
      {stats.map((s) => (
        <div key={s.label} className="bg-surface border border-line rounded-xl p-4">
          <p className="text-xs text-muted mb-1">{s.label}</p>
          <p className="text-2xl font-semibold text-ink">{s.value}</p>
        </div>
      ))}
    </div>
  );
}

// ------------------------------------------------------------------
// Concurrency panel
// ------------------------------------------------------------------
function fmtCost(n: number | undefined): string {
  if (n == null) return '—';
  if (n === 0) return '$0.0000';
  return n < 0.01 ? `$${n.toFixed(4)}` : `$${n.toFixed(2)}`;
}

function ConcurrencyPanel({ accounts }: { accounts: DispatchAccountSnapshot[] }) {
  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden mb-6">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">并发 / 账户</div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-line text-left text-xs text-muted">
              <th className="px-4 py-2 font-medium">邮箱 / 账户</th>
              <th className="px-4 py-2 font-medium">状态</th>
              <th className="px-4 py-2 font-medium text-right">并发中</th>
              <th className="px-4 py-2 font-medium text-right">可用</th>
              <th className="px-4 py-2 font-medium text-right">今日消费</th>
              <th className="px-4 py-2 font-medium text-right">总消费</th>
            </tr>
          </thead>
          <tbody>
            {accounts.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-muted text-xs">无数据</td>
              </tr>
            )}
            {accounts.map((a) => (
              <tr key={a.key} className="border-b border-line/50 hover:bg-line/30 transition">
                <td className="px-4 py-2">
                  <p className="text-sm text-ink font-medium">{a.label || '—'}</p>
                </td>
                <td className="px-4 py-2"><StatusBadge status={a.status} /></td>
                <td className="px-4 py-2 text-right tabular-nums">{a.inflight}</td>
                <td className="px-4 py-2 text-right tabular-nums">{a.available}</td>
                <td className="px-4 py-2 text-right tabular-nums text-xs text-muted">{fmtCost(a.todayCostUsd)}</td>
                <td className="px-4 py-2 text-right tabular-nums text-xs text-muted">{fmtCost(a.totalCostUsd)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Fallback channels panel
// ------------------------------------------------------------------
function FallbackChannelsPanel({ channels }: { channels: DispatchFallbackChannel[] }) {
  function formatCost(usd: number): string {
    if (usd >= 1) return `$${usd.toFixed(2)}`;
    if (usd >= 0.01) return `$${usd.toFixed(4)}`;
    return `$${usd.toFixed(6)}`;
  }

  function formatBalance(usd: number | undefined): string {
    if (usd == null || usd === 0) return '—';
    if (usd >= 100) return `$${usd.toFixed(0)}`;
    if (usd >= 1) return `$${usd.toFixed(2)}`;
    if (usd >= 0.01) return `$${usd.toFixed(4)}`;
    return `$${usd.toFixed(6)}`;
  }

  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden mb-6">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">保底渠道</div>
      {channels.length === 0 ? (
        <p className="px-4 py-6 text-center text-muted text-xs">无保底渠道</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-left text-xs text-muted">
                <th className="px-4 py-2 font-medium">渠道名称</th>
                <th className="px-4 py-2 font-medium">状态</th>
                <th className="px-4 py-2 font-medium text-right">优先级</th>
                <th className="px-4 py-2 font-medium text-right">权重</th>
                <th className="px-4 py-2 font-medium text-right">余额</th>
                <th className="px-4 py-2 font-medium text-right">今日消费</th>
                <th className="px-4 py-2 font-medium text-right">并发中</th>
                <th className="px-4 py-2 font-medium text-right">可用</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((ch) => (
                <tr key={ch.id} className="border-b border-line/50 hover:bg-line/30 transition">
                  <td className="px-4 py-2">
                    <p className="text-sm text-ink font-medium">{ch.name}</p>
                  </td>
                  <td className="px-4 py-2">
                    <span className={`inline-flex items-center px-2 py-0.5 rounded border text-xs font-mono ${ch.enabled ? 'bg-green-500/20 text-green-400 border-green-500/40' : 'bg-gray-500/10 text-gray-500 border-gray-500/20'}`}>
                      {ch.enabled ? '启用' : '停用'}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.priority}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.weight}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{formatBalance(ch.balanceUsd)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{formatCost(ch.todayCostUsd)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.inflight ?? '—'}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{ch.available ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// Traffic panel
// ------------------------------------------------------------------
function TrafficPanel({ data }: { data: DispatchStatus }) {
  const { traffic } = data;
  const items = [
    { label: '总请求', value: traffic.total.toLocaleString() },
    { label: '成功', value: traffic.ok.toLocaleString(), cls: 'text-green-400' },
    { label: '错误', value: traffic.error.toLocaleString(), cls: 'text-red-400' },
    { label: 'Tokens 入', value: traffic.tokensIn.toLocaleString() },
    { label: 'Tokens 出', value: traffic.tokensOut.toLocaleString() },
  ];

  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden mb-6">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">流量统计</div>
      <div className="grid grid-cols-2 sm:grid-cols-5 divide-x divide-line">
        {items.map((item) => (
          <div key={item.label} className="px-4 py-4">
            <p className="text-xs text-muted mb-1">{item.label}</p>
            <p className={`text-xl font-semibold tabular-nums ${item.cls ?? 'text-ink'}`}>{item.value}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Shared event type helpers (labels + styles)
// ------------------------------------------------------------------
const FALLBACK_REASON_CN: Record<string, string> = {
  probe:      '探活',
  price:      '低价',
  keyword:    '关键词',
  model:      '指定模型',
  exhausted:  '号池耗尽',
  session:    '会话连错',
  cyber:      '安全拒答',
  no_nodes:   '无节点可用',
};

interface EventLabel { label: string; cls: string; }

export function getEventLabel(type: string): EventLabel {
  switch (type) {
    case 'dispatch_ok':   return { label: '派单成功',    cls: 'bg-green-500/20 text-green-400 border-green-500/40' };
    case 'ban':           return { label: '封控',        cls: 'bg-red-500/20 text-red-400 border-red-500/40' };
    case 'recover':       return { label: '恢复',        cls: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40' };
    case 'fallback':      return { label: '保底触发',    cls: 'bg-blue-500/20 text-blue-400 border-blue-500/40' };
    case 'quota_limited': return { label: '账户限额',    cls: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40' };
    case 'session_exile': return { label: '会话连错放逐', cls: 'bg-orange-500/20 text-orange-400 border-orange-500/40' };
    case 'scale_up':      return { label: '弹性扩容',    cls: 'bg-blue-500/20 text-blue-400 border-blue-500/40' };
    case 'scale_down':    return { label: '弹性缩容',    cls: 'bg-gray-500/20 text-gray-400 border-gray-500/40' };
    case 'balance_low':   return { label: '余额不足',    cls: 'bg-red-500/20 text-red-400 border-red-500/40' };
    default:
      if (type === 'dispatch_err' || type.endsWith('_err') || type === 'error') {
        return { label: type, cls: 'bg-orange-500/20 text-orange-400 border-orange-500/40' };
      }
      return { label: type, cls: 'bg-blue-500/20 text-blue-400 border-blue-500/40' };
  }
}

const SESSION_EXILE_SUFFIX_CN: Record<string, string> = {
  cyber:   '安全拒答',
  session: '连续错误',
};

// Returns the extra detail text shown next to the badge (does NOT repeat the badge label).
export function renderEventDetail(
  type: string,
  target: string,
  fallbackNames?: Map<string, string>,
  accountNames?: Map<string, string>,
  detail?: any,
): string {
  if (type === 'dispatch_ok') {
    const email = accountNames?.get(target);
    // show email; fall back to suppressed raw id → empty
    return email ?? (target && !target.startsWith('n_') && !target.startsWith('fc_') ? target : '');
  }
  if (type === 'ban') {
    const email = accountNames?.get(target);
    return email ?? (target && !target.startsWith('n_') && !target.startsWith('fc_') ? target : '');
  }
  if (type === 'fallback') {
    const cn = FALLBACK_REASON_CN[target] ?? target;
    // Resolve channel name from detail.channel
    let channelName: string | undefined;
    try {
      const d: any = typeof detail === 'string' ? JSON.parse(detail) : detail;
      if (d && typeof d.channel === 'string' && d.channel) {
        channelName = fallbackNames?.get(d.channel);
      }
    } catch { /* ignore */ }
    const parts = ['保底触发'];
    if (cn) parts.push(cn);
    if (channelName) parts.push(channelName);
    return parts.join(' · ');
  }
  if (type === 'quota_limited') {
    const email = accountNames?.get(target);
    return email ? `账户限额 · ${email}` : `账户限额 · ${target || '节点'}`;
  }
  if (type === 'session_exile') {
    const suffix = SESSION_EXILE_SUFFIX_CN[target] ?? target;
    return suffix ? `会话连错放逐 · ${suffix}` : '会话连错放逐';
  }
  if (type === 'balance_low') {
    const channelName = fallbackNames?.get(target) ?? target;
    try {
      const d: any = typeof detail === 'string' ? JSON.parse(detail) : detail;
      if (d && d.balance != null && d.alert != null) {
        const bal = typeof d.balance === 'number' ? `$${(d.balance as number).toFixed(2)}` : String(d.balance);
        const alert = typeof d.alert === 'number' ? `$${(d.alert as number).toFixed(2)}` : String(d.alert);
        return `余额不足 · ${channelName} · ${bal}/${alert}`;
      }
    } catch { /* ignore */ }
    return `余额不足 · ${channelName}`;
  }
  if (type === 'scale_up' || type === 'scale_down') {
    return target || '';
  }
  if (target.startsWith('fallback:')) {
    const id = target.slice('fallback:'.length);
    const name = fallbackNames?.get(id);
    return name !== undefined ? `保底: ${name}` : '保底';
  }
  if (target.startsWith('n_') || target.startsWith('fc_') || target.startsWith('fb:')) return '';
  return target || '';
}

// ------------------------------------------------------------------
// Events timeline
// ------------------------------------------------------------------
export function EventTimeline({
  events,
  fallbackNames,
  accountNames,
}: {
  events: DispatchEvent[];
  fallbackNames: Map<string, string>;
  accountNames: Map<string, string>;
}) {
  function formatTs(ts: number | undefined): string {
    if (ts == null || ts === 0) return '—';
    try {
      return new Date(ts).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
    } catch {
      return '—';
    }
  }

  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">事件时间线（最近 20）</div>
      {events.length === 0 && (
        <p className="px-4 py-6 text-center text-muted text-xs">暂无事件</p>
      )}
      <ul className="divide-y divide-line/50 max-h-80 overflow-y-auto">
        {events.map((e, idx) => {
          const { label, cls } = getEventLabel(e.type);
          const detail = renderEventDetail(e.type, e.target, fallbackNames, accountNames, e.detail);
          return (
            <li key={idx} className="flex items-center gap-3 px-4 py-2.5 hover:bg-line/30 transition">
              <span className="text-xs text-muted tabular-nums shrink-0 w-20">
                {formatTs(e.ts)}
              </span>
              <span className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[10px] font-mono shrink-0 ${cls}`}>
                {label}
              </span>
              {detail && <span className="text-xs text-muted truncate">{detail}</span>}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// ------------------------------------------------------------------
// Server status card
// ------------------------------------------------------------------
function ServerStatusCard({ status }: { status: ServerStatus | null }) {
  if (!status) return null;

  function fmtUptime(sec: number): string {
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return `${d}d ${h}h ${m}m`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  const diskVal = (status.diskUsedGB != null && status.diskTotalGB != null)
    ? `${status.diskUsedGB.toFixed(1)}/${status.diskTotalGB.toFixed(1)} GB (${(status.diskUsedPct ?? 0).toFixed(0)}%)`
    : '—';
  const netVal = (status.netRxMBps != null && status.netTxMBps != null)
    ? `↓${status.netRxMBps.toFixed(2)} / ↑${status.netTxMBps.toFixed(2)} MB/s`
    : '—';

  const items = [
    { label: '运行时长', value: fmtUptime(status.uptimeSec) },
    { label: 'Goroutines', value: status.goroutines.toString() },
    { label: '内存', value: `${status.memAllocMB.toFixed(1)} / ${status.memSysMB.toFixed(1)} MB` },
    { label: 'GC 次数', value: status.numGC.toString() },
    { label: 'CPU 核数', value: status.numCPU.toString() },
    { label: 'Go 版本', value: status.goVersion },
    { label: '磁盘', value: diskVal },
    { label: '网络', value: netVal },
  ];

  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden mb-6">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">调度服务器状态</div>
      <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-8 divide-x divide-line">
        {items.map((item) => (
          <div key={item.label} className="px-4 py-3">
            <p className="text-xs text-muted mb-1">{item.label}</p>
            <p className="text-sm font-semibold text-ink tabular-nums">{item.value}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Main page
// ------------------------------------------------------------------
export default function Dispatch() {
  const { isTenant } = useAuth();
  if (isTenant) return <TenantDispatch />;
  return <AdminDispatch />;
}

function AdminDispatch() {
  const [data, setData] = useState<DispatchStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<'sse' | 'poll'>('sse');
  const [fallbackNames, setFallbackNames] = useState<Map<string, string>>(new Map());
  const [accountNames, setAccountNames] = useState<Map<string, string>>(new Map());
  const [serverStatus, setServerStatus] = useState<ServerStatus | null>(null);
  const esRef = useRef<EventSource | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Polling fallback
  const startPolling = () => {
    setMode('poll');
    const poll = () => {
      getDispatchStatus()
        .then(setData)
        .catch((err: unknown) => setError(String(err)));
    };
    poll();
    timerRef.current = setInterval(poll, 3000);
  };

  const stopPolling = () => {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  };

  const stopSSE = () => {
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
  };

  useEffect(() => {
    // Fetch fallback channel names for display (best-effort)
    listFallbackChannels()
      .then((channels) => {
        const m = new Map<string, string>();
        for (const ch of channels) m.set(ch.id, ch.name);
        setFallbackNames(m);
      })
      .catch(() => { /* non-fatal: show raw target on failure */ });

    // Fetch account email map for display (best-effort)
    listAccounts()
      .then((accounts) => {
        const m = new Map<string, string>();
        for (const a of accounts) {
          const key = `${a.nodeId}:${a.profileId}`;
          if (a.email) m.set(key, a.email);
        }
        setAccountNames(m);
      })
      .catch(() => { /* non-fatal */ });

    // Fetch server status
    const fetchServerStatus = () => {
      getServerStatus().then(setServerStatus).catch(() => {});
    };
    fetchServerStatus();
    const serverStatusTimer = setInterval(fetchServerStatus, 30000);

    // Try SSE first
    const es = new EventSource('/api/admin/dispatch/stream');
    esRef.current = es;

    es.onmessage = (evt) => {
      try {
        const parsed = JSON.parse(evt.data as string) as DispatchStatus;
        setData(parsed);
        setError(null);
      } catch {
        // ignore malformed frame
      }
    };

    es.onerror = () => {
      stopSSE();
      setError('SSE 断开，已切换轮询');
      startPolling();
    };

    return () => {
      stopSSE();
      stopPolling();
      clearInterval(serverStatusTimer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="p-4 md:p-6 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl font-semibold text-ink">调度仪表盘</h1>
        <div className="flex items-center gap-3">
          {error && (
            <span className="text-xs text-yellow-400 bg-yellow-500/10 border border-yellow-500/30 rounded px-2 py-1">
              {error}
            </span>
          )}
          <span className="text-xs text-muted border border-line rounded px-2 py-1">
            {mode === 'sse' ? '● SSE 实时' : '↻ 轮询 3s'}
          </span>
          {data && (
            <span className="text-xs text-muted">
              更新于 {new Date(data.asOf).toLocaleTimeString('zh-CN', { hour12: false })}
            </span>
          )}
        </div>
      </div>

      {/* Loading */}
      {!data && !error && (
        <div className="flex items-center justify-center py-24 text-muted text-sm">加载中…</div>
      )}

      {/* Content */}
      {data && (
        <>
          <ServerStatusCard status={serverStatus} />
          <StatsBar data={data} />
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
            <div>
              <ConcurrencyPanel accounts={data.accounts} />
              <FallbackChannelsPanel channels={data.fallbackChannels ?? []} />
              <TrafficPanel data={data} />
            </div>
            <EventTimeline events={data.events} fallbackNames={fallbackNames} accountNames={accountNames} />
          </div>
        </>
      )}
    </div>
  );
}
