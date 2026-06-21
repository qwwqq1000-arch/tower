// ============================================================
// Tower SPA — 日志 unified page (调度日志 | 审计日志 | 事件)
// Tabs: DispatchLogsTab | AuditTab | EventsTab
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { getLogs, getAudit, getEvents, listFallbackChannels, listAccounts } from '../api';
import type { LogEntry, AuditRecord, EventRecord } from '../types';

// ============================================================
// Shared helpers
// ============================================================
function fmtTime(ms: number): string {
  if (!ms) return '—';
  return new Date(ms).toLocaleString('zh-CN', {
    month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

// ============================================================
// DispatchLogsTab
// ============================================================
function fmtMs(ms: number | undefined): string {
  if (!ms) return '—';
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

function statusBadge(status: string, http: number) {
  const ok = status === 'ok' || (http >= 200 && http < 300);
  return (
    <span className={`inline-flex items-center gap-1 text-xs font-medium ${ok ? 'text-ok' : 'text-err'}`}>
      <span className={`w-1.5 h-1.5 rounded-full ${ok ? 'bg-ok' : 'bg-err'}`} />
      {status || String(http)}
    </span>
  );
}

function streamBadge(stream?: boolean) {
  if (stream === undefined) return <span className="text-muted/40">—</span>;
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border ${
      stream
        ? 'bg-blue-500/20 text-blue-400 border-blue-500/40'
        : 'bg-gray-500/10 text-gray-500 border-gray-500/20'
    }`}>
      {stream ? '流' : '非流'}
    </span>
  );
}

function fmtCost(usd?: number): string {
  if (!usd) return '—';
  return `$${usd.toFixed(4)}`;
}

function renderTarget(target: string, channelMap: Map<string, string>, accountMap: Map<string, string>): React.ReactNode {
  if (!target) return '—';
  if (target.startsWith('fallback:')) {
    const id = target.slice('fallback:'.length);
    const name = channelMap.get(id);
    return name ? `保底: ${name}` : '保底';
  }
  const email = accountMap.get(target);
  return email ?? '节点';
}

function LogRow({ row, channelMap, accountMap }: { row: LogEntry; channelMap: Map<string, string>; accountMap: Map<string, string> }) {
  const targetLabel = renderTarget(row.target, channelMap, accountMap);
  return (
    <tr className="border-t border-line hover:bg-line/20 transition text-sm">
      <td className="px-3 py-2 text-xs text-muted whitespace-nowrap font-mono">{fmtTime(row.ts)}</td>
      <td className="px-3 py-2 text-ink truncate max-w-[140px]" title={row.model}>{row.model || '—'}</td>
      <td className="px-3 py-2 text-ink truncate max-w-[120px] font-mono text-xs" title={row.target}>
        {targetLabel}
      </td>
      <td className="px-3 py-2">{statusBadge(row.status, row.httpStatus)}</td>
      <td className="px-3 py-2 text-muted text-xs">{row.httpStatus || '—'}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">{fmtMs(row.latencyMs)}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">{fmtMs(row.ttfbMs)}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">
        {row.tokensIn ? `↑${row.tokensIn}` : '—'} / {row.tokensOut ? `↓${row.tokensOut}` : '—'}
      </td>
      <td className="px-3 py-2">{streamBadge(row.stream)}</td>
      <td className="px-3 py-2 text-muted text-xs">{fmtCost(row.costUsd)}</td>
      <td className="px-3 py-2 text-xs text-muted truncate max-w-[120px]" title={row.fallbackReason}>
        {row.fallbackReason || <span className="text-muted/40 italic">—</span>}
      </td>
    </tr>
  );
}

function LogCard({ row, channelMap, accountMap }: { row: LogEntry; channelMap: Map<string, string>; accountMap: Map<string, string> }) {
  const targetLabel = renderTarget(row.target, channelMap, accountMap);
  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2 text-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="font-medium text-ink truncate">{row.model || '—'}</p>
          <p className="text-xs text-muted font-mono truncate mt-0.5">{targetLabel}</p>
        </div>
        {statusBadge(row.status, row.httpStatus)}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
        <span>{fmtTime(row.ts)}</span>
        <span>HTTP {row.httpStatus || '—'}</span>
        <span>延迟 {fmtMs(row.latencyMs)}</span>
        <span>首字 {fmtMs(row.ttfbMs)}</span>
        <span>↑{row.tokensIn ?? 0} / ↓{row.tokensOut ?? 0}</span>
        <span>{streamBadge(row.stream)}</span>
        <span>{fmtCost(row.costUsd) !== '—' ? `费用 ${fmtCost(row.costUsd)}` : ''}</span>
      </div>
      {row.fallbackReason && (
        <p className="text-xs text-warn bg-warn/10 border border-warn/30 rounded px-2 py-1 truncate">
          兜底: {row.fallbackReason}
        </p>
      )}
    </div>
  );
}

function DispatchLogsTab() {
  const [rows, setRows] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [channelMap, setChannelMap] = useState<Map<string, string>>(new Map());
  const [accountMap, setAccountMap] = useState<Map<string, string>>(new Map());

  useEffect(() => {
    listFallbackChannels()
      .then((channels) => {
        const m = new Map<string, string>();
        for (const ch of channels) m.set(ch.id, ch.name);
        setChannelMap(m);
      })
      .catch(() => {});

    listAccounts()
      .then((accounts) => {
        const m = new Map<string, string>();
        for (const a of accounts) m.set(`${a.nodeId}:${a.profileId}`, a.email);
        setAccountMap(m);
      })
      .catch(() => {});
  }, []);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    try {
      const data = await getLogs({ limit: '200' });
      setRows(Array.isArray(data) ? data : []);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void fetchLogs(); }, [fetchLogs]);

  const q = query.trim().toLowerCase();
  const filtered = q
    ? rows.filter(
        (r) =>
          r.model?.toLowerCase().includes(q) ||
          r.target?.toLowerCase().includes(q) ||
          r.status?.toLowerCase().includes(q) ||
          r.fallbackReason?.toLowerCase().includes(q) ||
          String(r.httpStatus).includes(q),
      )
    : rows;

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <p className="text-xs text-muted">最近 200 条请求记录</p>
        <button
          onClick={() => { void fetchLogs(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      <div className="relative max-w-sm">
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索模型 / 目标 / 状态 / 兜底原因…"
          className="w-full bg-surface border border-line rounded-lg pl-9 pr-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-muted text-xs select-none">⌕</span>
      </div>

      {loading && (
        <div className="flex items-center justify-center min-h-40">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}
      {!loading && !error && filtered.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-10 text-center text-muted text-sm">
          {rows.length === 0 ? '暂无日志记录' : '无匹配结果'}
        </div>
      )}
      {!loading && !error && filtered.length > 0 && (
        <>
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-x-auto">
            <table className="w-full text-left min-w-[880px]">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-3 py-3 font-medium">时间</th>
                  <th className="px-3 py-3 font-medium">模型</th>
                  <th className="px-3 py-3 font-medium">目标</th>
                  <th className="px-3 py-3 font-medium">状态</th>
                  <th className="px-3 py-3 font-medium">HTTP</th>
                  <th className="px-3 py-3 font-medium">延迟</th>
                  <th className="px-3 py-3 font-medium">首字</th>
                  <th className="px-3 py-3 font-medium">Token ↑/↓</th>
                  <th className="px-3 py-3 font-medium">类型</th>
                  <th className="px-3 py-3 font-medium">费用</th>
                  <th className="px-3 py-3 font-medium">兜底原因</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((row, i) => (
                  <LogRow key={i} row={row} channelMap={channelMap} accountMap={accountMap} />
                ))}
              </tbody>
            </table>
          </div>
          <div className="md:hidden space-y-3">
            {filtered.map((row, i) => <LogCard key={i} row={row} channelMap={channelMap} accountMap={accountMap} />)}
          </div>
          <p className="text-xs text-muted text-right">
            显示 {filtered.length} / {rows.length} 条
          </p>
        </>
      )}
    </div>
  );
}

// ============================================================
// AuditTab
// ============================================================
const ACTION_COLOR: Record<string, string> = {
  create:  'bg-ok/10 text-ok border-ok/30',
  delete:  'bg-err/10 text-err border-err/30',
  update:  'bg-accent/10 text-accent border-accent/30',
  login:   'bg-warn/10 text-warn border-warn/30',
  logout:  'bg-muted/10 text-muted border-line',
};

function actionBadge(action: string) {
  const key = action.toLowerCase().split('_')[0] ?? '';
  const cls = ACTION_COLOR[key] ?? 'bg-surface text-muted border-line';
  return (
    <span className={`inline-flex items-center text-xs font-medium border rounded-full px-2 py-0.5 ${cls}`}>
      {action}
    </span>
  );
}

function AuditRow({ row }: { row: AuditRecord }) {
  return (
    <tr className="border-t border-line hover:bg-line/20 transition text-sm">
      <td className="px-3 py-2 text-xs text-muted whitespace-nowrap font-mono">{fmtTime(row.ts)}</td>
      <td className="px-3 py-2 text-ink font-mono text-xs truncate max-w-[120px]" title={row.actor}>{row.actor || '—'}</td>
      <td className="px-3 py-2">{actionBadge(row.action)}</td>
      <td className="px-3 py-2 text-muted text-xs truncate max-w-[200px]" title={row.target}>{row.target || '—'}</td>
    </tr>
  );
}

function AuditCard({ row }: { row: AuditRecord }) {
  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2 text-sm">
      <div className="flex items-start justify-between gap-2">
        <span className="text-ink font-mono text-xs truncate">{row.actor || '—'}</span>
        {actionBadge(row.action)}
      </div>
      <p className="text-xs text-muted truncate">{row.target || '—'}</p>
      <p className="text-xs text-muted/60 font-mono">{fmtTime(row.ts)}</p>
    </div>
  );
}

function AuditTab() {
  const [rows, setRows] = useState<AuditRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');

  const fetchAudit = useCallback(async () => {
    setLoading(true);
    try {
      const data = await getAudit({ limit: '200' });
      setRows(Array.isArray(data) ? data : []);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void fetchAudit(); }, [fetchAudit]);

  const q = query.trim().toLowerCase();
  const filtered = q
    ? rows.filter(
        (r) =>
          r.actor?.toLowerCase().includes(q) ||
          r.action?.toLowerCase().includes(q) ||
          r.target?.toLowerCase().includes(q),
      )
    : rows;

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <p className="text-xs text-muted">操作者 / 动作 / 目标</p>
        <button
          onClick={() => { void fetchAudit(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      <div className="relative max-w-sm">
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索操作者 / 动作 / 目标…"
          className="w-full bg-surface border border-line rounded-lg pl-9 pr-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-muted text-xs select-none">⌕</span>
      </div>

      {loading && (
        <div className="flex items-center justify-center min-h-40">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}
      {!loading && !error && filtered.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-10 text-center text-muted text-sm">
          {rows.length === 0 ? '暂无审计记录' : '无匹配结果'}
        </div>
      )}
      {!loading && !error && filtered.length > 0 && (
        <>
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-x-auto">
            <table className="w-full text-left min-w-[520px]">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-3 py-3 font-medium">时间</th>
                  <th className="px-3 py-3 font-medium">操作者</th>
                  <th className="px-3 py-3 font-medium">动作</th>
                  <th className="px-3 py-3 font-medium">目标</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((row, i) => <AuditRow key={i} row={row} />)}
              </tbody>
            </table>
          </div>
          <div className="md:hidden space-y-3">
            {filtered.map((row, i) => <AuditCard key={i} row={row} />)}
          </div>
          <p className="text-xs text-muted text-right">
            显示 {filtered.length} / {rows.length} 条
          </p>
        </>
      )}
    </div>
  );
}

// ============================================================
// EventsTab
// ============================================================
const TYPE_STYLES: Record<string, { dot: string; badge: string; label?: string }> = {
  node_up:        { dot: 'bg-ok',     badge: 'bg-ok/10 text-ok border-ok/30' },
  node_down:      { dot: 'bg-err',    badge: 'bg-err/10 text-err border-err/30' },
  ban:            { dot: 'bg-err',    badge: 'bg-err/10 text-err border-err/30',     label: '封控' },
  unban:          { dot: 'bg-ok',     badge: 'bg-ok/10 text-ok border-ok/30' },
  recover:        { dot: 'bg-warn',   badge: 'bg-warn/10 text-warn border-warn/30',  label: '恢复' },
  dispatch_ok:    { dot: 'bg-ok',     badge: 'bg-ok/10 text-ok border-ok/30',        label: '派单成功' },
  fallback:       { dot: 'bg-accent', badge: 'bg-accent/10 text-accent border-accent/30', label: '保底触发' },
  quota_limited:  { dot: 'bg-warn',   badge: 'bg-warn/10 text-warn border-warn/30',  label: '账户限额' },
  session_exile:  { dot: 'bg-warn',   badge: 'bg-warn/10 text-warn border-warn/30',  label: '会话连错放逐' },
  scale_up:       { dot: 'bg-accent', badge: 'bg-accent/10 text-accent border-accent/30', label: '弹性扩容' },
  scale_down:     { dot: 'bg-muted',  badge: 'bg-surface text-muted border-line',    label: '弹性缩容' },
  provision:      { dot: 'bg-accent', badge: 'bg-accent/10 text-accent border-accent/30' },
  settle:         { dot: 'bg-accent', badge: 'bg-accent/10 text-accent border-accent/30' },
};

const FALLBACK_REASON_CN: Record<string, string> = {
  probe:     '探活',
  price:     '低价',
  keyword:   '关键词',
  model:     '指定模型',
  exhausted: '号池耗尽',
  session:   '会话连错',
  cyber:     '安全拒答',
  no_nodes:  '无节点可用',
};

function getStyle(type: string) {
  const key = type.toLowerCase().split('.')[0] ?? '';
  return (
    TYPE_STYLES[key] ??
    TYPE_STYLES[type.toLowerCase()] ?? { dot: 'bg-muted', badge: 'bg-surface text-muted border-line' }
  );
}

function parseDetail(detail: Record<string, unknown> | string | undefined): Record<string, unknown> {
  if (!detail) return {};
  if (typeof detail === 'string') {
    try { return JSON.parse(detail) as Record<string, unknown>; } catch { return {}; }
  }
  return detail;
}

const SESSION_EXILE_SUFFIX: Record<string, string> = {
  cyber:   '安全拒答',
  session: '连续错误',
};

function renderTargetText(
  type: string,
  target: string,
  detail: Record<string, unknown>,
  accountMap: Map<string, string>,
  channelMap: Map<string, string>,
): string {
  if (type === 'dispatch_ok') {
    const email = accountMap.get(target);
    return email ? `派单成功 · ${email}` : `派单成功 · ${target || '节点'}`;
  }
  if (type === 'ban') {
    const email = accountMap.get(target);
    return email ? `封控 · ${email}` : `封控 · ${target || '节点'}`;
  }
  if (type === 'fallback') {
    const cn = FALLBACK_REASON_CN[target] ?? target;
    const channelId = typeof detail['channel'] === 'string' ? detail['channel'] : '';
    const channelName = channelId ? channelMap.get(channelId) : undefined;
    const base = cn ? `保底触发 · ${cn}` : '保底触发';
    return channelName ? `${base} · ${channelName}` : base;
  }
  if (type === 'quota_limited') {
    const email = accountMap.get(target);
    return email ? `账户限额 · ${email}` : `账户限额 · ${target || '节点'}`;
  }
  if (type === 'session_exile') {
    const suffix = SESSION_EXILE_SUFFIX[target] ?? target;
    return suffix ? `会话连错放逐 · ${suffix}` : '会话连错放逐';
  }
  if (type === 'scale_up') {
    return `弹性扩容 · ${target || ''}`.trimEnd().replace(/ · $/, '');
  }
  if (type === 'scale_down') {
    return `弹性缩容 · ${target || ''}`.trimEnd().replace(/ · $/, '');
  }
  if (!target || target.startsWith('n_') || target.startsWith('fc_') || target.startsWith('fb:')) return '';
  return target;
}

function EventItem({
  row,
  accountMap,
  channelMap,
}: {
  row: EventRecord;
  accountMap: Map<string, string>;
  channelMap: Map<string, string>;
}) {
  const style = getStyle(row.type);
  const { dot, badge } = style;
  const label = style.label ?? row.type;
  const detail = parseDetail(row.detail as Record<string, unknown> | string | undefined);
  const targetText = renderTargetText(row.type, row.target ?? '', detail, accountMap, channelMap);
  const showDetail = row.detail && Object.keys(row.detail).length > 0 && row.type !== 'fallback';
  return (
    <div className="flex gap-4">
      <div className="flex flex-col items-center">
        <div className={`w-3 h-3 rounded-full mt-1 shrink-0 ${dot}`} />
        <div className="w-px flex-1 bg-line mt-1" />
      </div>
      <div className="pb-5 min-w-0 flex-1">
        <div className="flex flex-wrap items-start gap-x-3 gap-y-1">
          <span className={`inline-flex items-center text-xs font-medium border rounded-full px-2 py-0.5 ${badge}`}>
            {label}
          </span>
          <span className="text-xs text-muted font-mono mt-0.5">{fmtTime(row.ts)}</span>
        </div>
        {targetText && (
          <p className="text-sm text-ink mt-1 truncate" title={targetText}>{targetText}</p>
        )}
        {showDetail && (
          <pre className="mt-1.5 text-xs text-muted bg-bg border border-line rounded-lg px-3 py-2 overflow-x-auto max-w-full">
            {JSON.stringify(row.detail, null, 2)}
          </pre>
        )}
      </div>
    </div>
  );
}

function EventsTab() {
  const [rows, setRows] = useState<EventRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [accountMap, setAccountMap] = useState<Map<string, string>>(new Map());
  const [channelMap, setChannelMap] = useState<Map<string, string>>(new Map());

  const fetchEvents = useCallback(async () => {
    setLoading(true);
    try {
      const data = await getEvents({ limit: '200' });
      setRows(Array.isArray(data) ? data : []);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void fetchEvents(); }, [fetchEvents]);

  useEffect(() => {
    listAccounts()
      .then((accounts) => {
        const m = new Map<string, string>();
        for (const a of accounts) {
          const key = `${a.nodeId}:${a.profileId}`;
          if (a.email) m.set(key, a.email);
        }
        setAccountMap(m);
      })
      .catch(() => {});
    listFallbackChannels()
      .then((channels) => {
        const m = new Map<string, string>();
        for (const ch of channels) m.set(ch.id, ch.name);
        setChannelMap(m);
      })
      .catch(() => {});
  }, []);

  const q = query.trim().toLowerCase();
  const filtered = q
    ? rows.filter(
        (r) =>
          r.type?.toLowerCase().includes(q) ||
          r.target?.toLowerCase().includes(q),
      )
    : rows;

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <p className="text-xs text-muted">最近 200 条系统事件时间线</p>
        <button
          onClick={() => { void fetchEvents(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      <div className="relative max-w-sm">
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索事件类型 / 目标…"
          className="w-full bg-surface border border-line rounded-lg pl-9 pr-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-muted text-xs select-none">⌕</span>
      </div>

      {loading && (
        <div className="flex items-center justify-center min-h-40">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}
      {!loading && !error && filtered.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-10 text-center text-muted text-sm">
          {rows.length === 0 ? '暂无系统事件' : '无匹配结果'}
        </div>
      )}
      {!loading && !error && filtered.length > 0 && (
        <div className="bg-surface border border-line rounded-xl px-5 pt-5 pb-0">
          {filtered.map((row, i) => (
            <EventItem key={i} row={row} accountMap={accountMap} channelMap={channelMap} />
          ))}
        </div>
      )}
      {!loading && !error && filtered.length > 0 && (
        <p className="text-xs text-muted text-right">
          显示 {filtered.length} / {rows.length} 条
        </p>
      )}
    </div>
  );
}

// ============================================================
// Unified Logs page
// ============================================================
type Tab = 'dispatch' | 'audit' | 'events';

const TABS: { key: Tab; label: string }[] = [
  { key: 'dispatch', label: '调度日志' },
  { key: 'audit',    label: '审计日志' },
  { key: 'events',   label: '事件' },
];

export default function Logs() {
  const [tab, setTab] = useState<Tab>('dispatch');

  return (
    <div className="p-4 md:p-6 space-y-4">
      {/* Header */}
      <h1 className="text-2xl font-semibold text-ink">日志</h1>

      {/* Tab switcher */}
      <div className="flex gap-1 border-b border-line">
        {TABS.map(({ key, label }) => (
          <button
            key={key}
            onClick={() => setTab(key)}
            className={[
              'px-4 py-2 text-sm font-medium border-b-2 -mb-px transition',
              tab === key
                ? 'border-accent text-accent'
                : 'border-transparent text-muted hover:text-ink',
            ].join(' ')}
          >
            {label}
          </button>
        ))}
      </div>

      {/* Tab panels */}
      {tab === 'dispatch' && <DispatchLogsTab />}
      {tab === 'audit'    && <AuditTab />}
      {tab === 'events'   && <EventsTab />}
    </div>
  );
}
