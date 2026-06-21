// ============================================================
// Tower SPA — Events page (系统事件)
// GET /api/admin/events → timeline (time / type / target)
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { getEvents } from '../api';
import type { EventRecord } from '../types';

// ---- helpers ----
function fmtTime(ms: number): string {
  if (!ms) return '—';
  return new Date(ms).toLocaleString('zh-CN', {
    month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

const TYPE_STYLES: Record<string, { dot: string; badge: string; label?: string }> = {
  node_up:      { dot: 'bg-ok',     badge: 'bg-ok/10 text-ok border-ok/30' },
  node_down:    { dot: 'bg-err',    badge: 'bg-err/10 text-err border-err/30' },
  ban:          { dot: 'bg-err',    badge: 'bg-err/10 text-err border-err/30',     label: '封号' },
  unban:        { dot: 'bg-ok',     badge: 'bg-ok/10 text-ok border-ok/30' },
  recover:      { dot: 'bg-warn',   badge: 'bg-warn/10 text-warn border-warn/30',  label: '恢复' },
  dispatch_ok:  { dot: 'bg-ok',     badge: 'bg-ok/10 text-ok border-ok/30',        label: '派单成功' },
  fallback:     { dot: 'bg-warn',   badge: 'bg-warn/10 text-warn border-warn/30',  label: '保底触发' },
  scale_up:     { dot: 'bg-accent', badge: 'bg-accent/10 text-accent border-accent/30', label: '弹性扩容' },
  scale_down:   { dot: 'bg-muted',  badge: 'bg-surface text-muted border-line',    label: '弹性缩容' },
  provision:    { dot: 'bg-accent', badge: 'bg-accent/10 text-accent border-accent/30' },
  settle:       { dot: 'bg-accent', badge: 'bg-accent/10 text-accent border-accent/30' },
};

const FALLBACK_REASON_CN: Record<string, string> = {
  probe:     '探活',
  price:     '低价',
  keyword:   '关键词',
  model:     '指定模型',
  exhausted: '号池耗尽',
  session:   '会话连错',
  cyber:     '安全拒答',
};

function getStyle(type: string) {
  const key = type.toLowerCase().split('.')[0] ?? '';
  return (
    TYPE_STYLES[key] ??
    TYPE_STYLES[type.toLowerCase()] ?? { dot: 'bg-muted', badge: 'bg-surface text-muted border-line' }
  );
}

function renderTargetText(type: string, target: string): string {
  if (type === 'fallback') {
    const cn = FALLBACK_REASON_CN[target] ?? target;
    return cn ? `保底触发 · ${cn}` : '保底触发';
  }
  if (type === 'scale_up' || type === 'scale_down') {
    return target || '';
  }
  // suppress raw internal ids
  if (!target || target.startsWith('n_') || target.startsWith('fc_') || target.startsWith('fb:')) return '';
  return target;
}

// ---- Timeline item ----
function EventItem({ row }: { row: EventRecord }) {
  const style = getStyle(row.type);
  const { dot, badge } = style;
  const label = style.label ?? row.type;
  const targetText = renderTargetText(row.type, row.target ?? '');
  return (
    <div className="flex gap-4">
      {/* Timeline track */}
      <div className="flex flex-col items-center">
        <div className={`w-3 h-3 rounded-full mt-1 shrink-0 ${dot}`} />
        <div className="w-px flex-1 bg-line mt-1" />
      </div>
      {/* Content */}
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
        {row.detail && Object.keys(row.detail).length > 0 && (
          <pre className="mt-1.5 text-xs text-muted bg-bg border border-line rounded-lg px-3 py-2 overflow-x-auto max-w-full">
            {JSON.stringify(row.detail, null, 2)}
          </pre>
        )}
      </div>
    </div>
  );
}

// ---- Page ----
export default function Events() {
  const [rows, setRows] = useState<EventRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');

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

  const q = query.trim().toLowerCase();
  const filtered = q
    ? rows.filter(
        (r) =>
          r.type?.toLowerCase().includes(q) ||
          r.target?.toLowerCase().includes(q),
      )
    : rows;

  return (
    <div className="p-4 md:p-6 space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">系统事件</h1>
          <p className="text-xs text-muted mt-1">最近 200 条系统事件时间线</p>
        </div>
        <button
          onClick={() => { void fetchEvents(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {/* Search */}
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

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center min-h-40">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}

      {/* Error */}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}

      {/* Empty */}
      {!loading && !error && filtered.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-10 text-center text-muted text-sm">
          {rows.length === 0 ? '暂无系统事件' : '无匹配结果'}
        </div>
      )}

      {/* Timeline */}
      {!loading && !error && filtered.length > 0 && (
        <div className="bg-surface border border-line rounded-xl px-5 pt-5 pb-0">
          {filtered.map((row, i) => <EventItem key={i} row={row} />)}
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
