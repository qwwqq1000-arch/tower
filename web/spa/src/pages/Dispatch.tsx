// ============================================================
// Tower SPA — Dispatch real-time dashboard
// SSE primary; polling fallback on error
// ============================================================
import { useEffect, useRef, useState } from 'react';
import { getDispatchStatus, listFallbackChannels } from '../api';
import type { DispatchStatus, DispatchAccountSnapshot, DispatchEvent } from '../types';

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
    { label: '今日请求', value: traffic.total.toLocaleString() },
    { label: '成功率', value: traffic.total > 0 ? `${rate}%` : '—' },
    { label: '并发中', value: inflight.toString() },
  ];

  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-6">
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
            </tr>
          </thead>
          <tbody>
            {accounts.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-6 text-center text-muted text-xs">无数据</td>
              </tr>
            )}
            {accounts.map((a) => (
              <tr key={a.key} className="border-b border-line/50 hover:bg-line/30 transition">
                <td className="px-4 py-2">
                  <p className="text-sm text-ink font-medium">{a.label ?? '—'}</p>
                  <p className="text-xs font-mono text-muted mt-0.5 truncate max-w-[200px]">{a.key}</p>
                </td>
                <td className="px-4 py-2"><StatusBadge status={a.status} /></td>
                <td className="px-4 py-2 text-right tabular-nums">{a.inflight}</td>
                <td className="px-4 py-2 text-right tabular-nums">{a.available}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
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
// Event type badge
// ------------------------------------------------------------------
function EventTypeBadge({ type }: { type: string }) {
  let cls = 'bg-blue-500/20 text-blue-400 border-blue-500/40';
  if (type === 'ban' || type.startsWith('ban_')) {
    cls = 'bg-red-500/20 text-red-400 border-red-500/40';
  } else if (type === 'dispatch_ok' || type === 'ok') {
    cls = 'bg-green-500/20 text-green-400 border-green-500/40';
  } else if (type === 'dispatch_err' || type === 'error' || type.endsWith('_err')) {
    cls = 'bg-orange-500/20 text-orange-400 border-orange-500/40';
  } else if (type === 'half_open' || type === 'recover') {
    cls = 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40';
  }
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[10px] font-mono shrink-0 ${cls}`}>
      {type}
    </span>
  );
}

// ------------------------------------------------------------------
// Events timeline
// ------------------------------------------------------------------
function EventTimeline({ events, fallbackNames }: { events: DispatchEvent[]; fallbackNames: Map<string, string> }) {
  function renderTarget(target: string): string {
    if (target.startsWith('fallback:')) {
      const id = target.slice('fallback:'.length);
      const name = fallbackNames.get(id);
      return name !== undefined ? `保底: ${name}` : target;
    }
    return target;
  }

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
        {events.map((e, idx) => (
          <li key={idx} className="flex items-center gap-3 px-4 py-2.5 hover:bg-line/30 transition">
            <span className="text-xs text-muted tabular-nums shrink-0 w-20">
              {formatTs(e.ts)}
            </span>
            <EventTypeBadge type={e.type} />
            <span className="text-xs text-muted truncate">{renderTarget(e.target)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// ------------------------------------------------------------------
// Main page
// ------------------------------------------------------------------
export default function Dispatch() {
  const [data, setData] = useState<DispatchStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<'sse' | 'poll'>('sse');
  const [fallbackNames, setFallbackNames] = useState<Map<string, string>>(new Map());
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
          <StatsBar data={data} />
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
            <div>
              <ConcurrencyPanel accounts={data.accounts} />
              <TrafficPanel data={data} />
            </div>
            <EventTimeline events={data.events} fallbackNames={fallbackNames} />
          </div>
        </>
      )}
    </div>
  );
}
