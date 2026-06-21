// ============================================================
// Tower SPA — Audit page (审计日志)
// GET /api/admin/audit → table (time / actor / action / target)
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { getAudit } from '../api';
import type { AuditRecord } from '../types';

// ---- helpers ----
function fmtTime(ms: number): string {
  if (!ms) return '—';
  return new Date(ms).toLocaleString('zh-CN', {
    month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

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

// ---- Desktop row ----
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

// ---- Mobile card ----
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

// ---- Page ----
export default function Audit() {
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
    <div className="p-4 md:p-6 space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">审计日志</h1>
          <p className="text-xs text-muted mt-1">操作者 / 动作 / 目标</p>
        </div>
        <button
          onClick={() => { void fetchAudit(); }}
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
          placeholder="搜索操作者 / 动作 / 目标…"
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
          {rows.length === 0 ? '暂无审计记录' : '无匹配结果'}
        </div>
      )}

      {/* Desktop table */}
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

          {/* Mobile cards */}
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
