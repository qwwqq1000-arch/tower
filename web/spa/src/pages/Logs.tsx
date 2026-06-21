// ============================================================
// Tower SPA — Logs page (调度日志)
// GET /api/admin/logs?limit=200 → table with client-side search
// Columns: time / model / target / status / http / latency / tokens in+out / fallbackReason
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { getLogs } from '../api';
import type { LogEntry } from '../types';

// ---- helpers ----
function fmtTime(ms: number): string {
  if (!ms) return '—';
  return new Date(ms).toLocaleString('zh-CN', {
    month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
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

// ---- Desktop table row ----
function LogRow({ row }: { row: LogEntry }) {
  return (
    <tr className="border-t border-line hover:bg-line/20 transition text-sm">
      <td className="px-3 py-2 text-xs text-muted whitespace-nowrap font-mono">{fmtTime(row.ts)}</td>
      <td className="px-3 py-2 text-ink truncate max-w-[140px]" title={row.model}>{row.model || '—'}</td>
      <td className="px-3 py-2 text-ink truncate max-w-[120px] font-mono text-xs" title={row.target}>{row.target || '—'}</td>
      <td className="px-3 py-2">{statusBadge(row.status, row.httpStatus)}</td>
      <td className="px-3 py-2 text-muted text-xs">{row.httpStatus || '—'}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">{row.latencyMs ? `${row.latencyMs} ms` : '—'}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">
        {row.tokensIn ? `↑${row.tokensIn}` : '—'} / {row.tokensOut ? `↓${row.tokensOut}` : '—'}
      </td>
      <td className="px-3 py-2 text-xs text-muted truncate max-w-[120px]" title={row.fallbackReason}>
        {row.fallbackReason || <span className="text-muted/40 italic">—</span>}
      </td>
    </tr>
  );
}

// ---- Mobile card ----
function LogCard({ row }: { row: LogEntry }) {
  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2 text-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="font-medium text-ink truncate">{row.model || '—'}</p>
          <p className="text-xs text-muted font-mono truncate mt-0.5">{row.target || '—'}</p>
        </div>
        {statusBadge(row.status, row.httpStatus)}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
        <span>{fmtTime(row.ts)}</span>
        <span>HTTP {row.httpStatus || '—'}</span>
        <span>{row.latencyMs ? `${row.latencyMs} ms` : '—'}</span>
        <span>↑{row.tokensIn ?? 0} / ↓{row.tokensOut ?? 0}</span>
      </div>
      {row.fallbackReason && (
        <p className="text-xs text-warn bg-warn/10 border border-warn/30 rounded px-2 py-1 truncate">
          兜底: {row.fallbackReason}
        </p>
      )}
    </div>
  );
}

// ---- Page ----
export default function Logs() {
  const [rows, setRows] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');

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
    <div className="p-4 md:p-6 space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">调度日志</h1>
          <p className="text-xs text-muted mt-1">最近 200 条请求记录</p>
        </div>
        <button
          onClick={() => { void fetchLogs(); }}
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
          placeholder="搜索模型 / 目标 / 状态 / 兜底原因…"
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
          {rows.length === 0 ? '暂无日志记录' : '无匹配结果'}
        </div>
      )}

      {/* Desktop table */}
      {!loading && !error && filtered.length > 0 && (
        <>
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-x-auto">
            <table className="w-full text-left min-w-[780px]">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-3 py-3 font-medium">时间</th>
                  <th className="px-3 py-3 font-medium">模型</th>
                  <th className="px-3 py-3 font-medium">目标</th>
                  <th className="px-3 py-3 font-medium">状态</th>
                  <th className="px-3 py-3 font-medium">HTTP</th>
                  <th className="px-3 py-3 font-medium">延迟</th>
                  <th className="px-3 py-3 font-medium">Token ↑/↓</th>
                  <th className="px-3 py-3 font-medium">兜底原因</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((row, i) => (
                  <LogRow key={i} row={row} />
                ))}
              </tbody>
            </table>
          </div>

          {/* Mobile cards */}
          <div className="md:hidden space-y-3">
            {filtered.map((row, i) => <LogCard key={i} row={row} />)}
          </div>

          <p className="text-xs text-muted text-right">
            显示 {filtered.length} / {rows.length} 条
          </p>
        </>
      )}
    </div>
  );
}
