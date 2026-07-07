// ============================================================
// Tower SPA — 日志 unified page (调度日志 | 审计日志 | 事件)
// Tabs: DispatchLogsTab | AuditTab | EventsTab
// ============================================================
import { useEffect, useState, useCallback, useMemo } from 'react';
import {
  getLogs, getAudit, getEvents, listFallbackChannels, listAccounts,
  getMeLogs, getMeEvents, listMeFallback, getMeAccounts,
  getLogDetail, getMeLogDetail, type LogDetail,
} from '../api';
import type { LogEntry, AuditRecord, EventRecord } from '../types';
import { useAuth } from '../auth';

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

const PAGE_SIZE = 25;

function PaginationBar({ page, total, pageSize, onPrev, onNext }: {
  page: number; total: number; pageSize: number;
  onPrev: () => void; onNext: () => void;
}) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  return (
    <div className="flex items-center justify-between text-xs text-muted">
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

// resolveEmail maps a dispatch target to an account email. The target is the dispatch
// key "<nodeId>:<profileId>"; the admin map is keyed by that full key, the tenant map by
// profileId alone (no nodeId), so try the full key first then the profileId part.
function resolveEmail(target: string, accountMap: Map<string, string>): string | undefined {
  if (!target) return undefined;
  const direct = accountMap.get(target);
  if (direct) return direct;
  const i = target.indexOf(':');
  return i >= 0 ? accountMap.get(target.slice(i + 1)) : undefined;
}

function renderTarget(target: string, channelMap: Map<string, string>, accountMap: Map<string, string>, targetEmail?: string): React.ReactNode {
  if (!target) return '—';
  if (target.startsWith('fallback:')) {
    const id = target.slice('fallback:'.length);
    const name = channelMap.get(id);
    return name ? `保底: ${name}` : '保底';
  }
  // Prefer server-resolved email (logs-email-1): avoids accountMap lookup failures for CPA keys.
  if (targetEmail) return targetEmail;
  // Account-less targets: surface a clear label rather than misleading "节点".
  if (target === 'node') return '无可用节点';
  if (target === 'none') return '—';
  // Last-resort: client-side map lookup, then "节点" fallback.
  const email = resolveEmail(target, accountMap);
  return email ?? '节点';
}

// prettifies a JSON body for the detail view; falls back to the raw string.
function prettyJSON(s: string): string {
  if (!s) return '';
  try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; }
}

// LogDetailModal fetches and shows the stored request body + redacted headers for
// a clicked log row (logs-detail-1).
function LogDetailModal({ requestId, isTenant, onClose }: { requestId: string; isTenant: boolean; onClose: () => void }) {
  const [detail, setDetail] = useState<LogDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    (isTenant ? getMeLogDetail(requestId) : getLogDetail(requestId))
      .then(setDetail)
      .catch((e) => setError(e instanceof Error ? e.message : '加载失败'));
  }, [requestId, isTenant]);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="bg-surface border border-line rounded-xl w-full max-w-3xl max-h-[85vh] overflow-hidden flex flex-col" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-3 border-b border-line">
          <h3 className="text-sm font-semibold text-ink">请求详情</h3>
          <button onClick={onClose} className="text-muted hover:text-ink text-lg leading-none">×</button>
        </div>
        <div className="overflow-y-auto p-5 space-y-4">
          {error && <div className="text-err text-sm bg-err/10 border border-err/30 rounded-lg px-3 py-2">{error}</div>}
          {!error && !detail && <div className="text-muted text-sm animate-pulse">加载中…</div>}
          {detail && (
            <>
              {(detail.respBody || detail.respStatus) ? (() => {
                const isErr = (detail.respStatus ?? 0) >= 400;
                return (
                  <div>
                    <p className="text-xs uppercase tracking-wide mb-1.5">
                      <span className={isErr ? 'text-err' : 'text-muted'}>响应{detail.respStatus ? ` · HTTP ${detail.respStatus}` : ''}{isErr ? '(错误)' : ''}</span>
                    </p>
                    <pre className={`text-xs rounded-lg px-3 py-2 overflow-x-auto whitespace-pre-wrap break-all max-h-[40vh] border ${isErr ? 'text-err bg-err/10 border-err/30' : 'text-ink bg-bg border-line'}`}>{prettyJSON(detail.respBody ?? '') || <span className="text-muted/40 italic">空</span>}</pre>
                  </div>
                );
              })() : null}
              <div>
                <p className="text-xs text-muted uppercase tracking-wide mb-1.5">请求头(密钥已脱敏)</p>
                <pre className="text-xs text-muted bg-bg border border-line rounded-lg px-3 py-2 overflow-x-auto whitespace-pre-wrap break-all">{prettyJSON(detail.reqHeaders)}</pre>
              </div>
              <div>
                <p className="text-xs text-muted uppercase tracking-wide mb-1.5">请求体</p>
                <pre className="text-xs text-ink bg-bg border border-line rounded-lg px-3 py-2 overflow-x-auto whitespace-pre-wrap break-all max-h-[45vh]">{prettyJSON(detail.reqBody) || <span className="text-muted/40 italic">空</span>}</pre>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function LogRow({ row, channelMap, accountMap, onOpen }: { row: LogEntry; channelMap: Map<string, string>; accountMap: Map<string, string>; onOpen?: () => void }) {
  const targetLabel = renderTarget(row.target, channelMap, accountMap, row.targetEmail);
  return (
    <tr className={`border-t border-line hover:bg-line/20 transition text-sm ${onOpen ? 'cursor-pointer' : ''}`} onClick={onOpen}>
      <td className="px-3 py-2 text-xs text-muted whitespace-nowrap font-mono">{fmtTime(row.ts)}</td>
      <td className="px-3 py-2 text-ink truncate max-w-[140px]" title={row.model}>{row.model || '—'}</td>
      <td className="px-3 py-2 text-ink truncate max-w-[120px] font-mono text-xs">
        {targetLabel}
      </td>
      <td className="px-3 py-2">{statusBadge(row.status, row.httpStatus)}</td>
      <td className="px-3 py-2 text-muted text-xs">{row.httpStatus || '—'}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">{fmtMs(row.latencyMs)}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">{fmtMs(row.ttfbMs)}</td>
      <td className="px-3 py-2 text-muted text-xs whitespace-nowrap">
        <div>{row.tokensIn ? `↑${row.tokensIn}` : '—'} / {row.tokensOut ? `↓${row.tokensOut}` : '—'}</div>
        {(row.cacheRead > 0 || row.cacheCreation > 0) && (
          <div className="text-muted/60 text-[10px] leading-tight mt-0.5">
            {row.cacheRead > 0 && <span>缓存读 {row.cacheRead.toLocaleString()}</span>}
            {row.cacheRead > 0 && row.cacheCreation > 0 && <span className="mx-1">·</span>}
            {row.cacheCreation > 0 && <span>缓存写 {row.cacheCreation.toLocaleString()}</span>}
          </div>
        )}
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap gap-1 items-center">
          {streamBadge(row.stream)}
          {row.affinityHit && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border bg-green-500/20 text-green-400 border-green-500/40">亲和</span>
          )}
          {row.isAttempt && (
            <span
              title={`失败尝试 · ${row.targetEmail || row.target} · HTTP ${row.httpStatus || '—'} · 已自动重试到其他账户（这是被放弃的那次尝试，不是最终结果）`}
              className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border bg-amber-500/20 text-amber-400 border-amber-500/40 cursor-help"
            >重试</span>
          )}
        </div>
      </td>
      <td className="px-3 py-2 text-muted text-xs">{fmtCost(row.costUsd)}</td>
      <td className="px-3 py-2 text-xs text-muted truncate max-w-[120px]" title={row.fallbackReason}>
        {row.fallbackReason || <span className="text-muted/40 italic">—</span>}
      </td>
    </tr>
  );
}

function LogCard({ row, channelMap, accountMap, onOpen }: { row: LogEntry; channelMap: Map<string, string>; accountMap: Map<string, string>; onOpen?: () => void }) {
  const targetLabel = renderTarget(row.target, channelMap, accountMap, row.targetEmail);
  return (
    <div className={`bg-surface border border-line rounded-xl p-4 space-y-2 text-sm ${onOpen ? 'cursor-pointer active:bg-line/20' : ''}`} onClick={onOpen}>
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
        <span>↑{row.tokensIn ?? 0} / ↓{row.tokensOut ?? 0}{(row.cacheRead > 0 || row.cacheCreation > 0) && <span className="text-muted/60 ml-1 text-[10px]">{row.cacheRead > 0 ? `缓存读 ${row.cacheRead.toLocaleString()}` : ''}{row.cacheRead > 0 && row.cacheCreation > 0 ? ' · ' : ''}{row.cacheCreation > 0 ? `缓存写 ${row.cacheCreation.toLocaleString()}` : ''}</span>}</span>
        <span>{streamBadge(row.stream)}</span>
        {row.affinityHit && (
          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border bg-green-500/20 text-green-400 border-green-500/40">亲和</span>
        )}
        {row.isAttempt && (
          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border bg-amber-500/20 text-amber-400 border-amber-500/40">重试</span>
        )}
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

function DispatchLogsTab({ isTenant }: { isTenant: boolean }) {
  const [rows, setRows] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [channelMap, setChannelMap] = useState<Map<string, string>>(new Map());
  const [accountMap, setAccountMap] = useState<Map<string, string>>(new Map());
  const [detailId, setDetailId] = useState<string | null>(null); // requestId of the row being inspected
  const [dispatchPage, setDispatchPage] = useState(0);

  useEffect(() => {
    (isTenant ? listMeFallback() : listFallbackChannels())
      .then((channels) => {
        const m = new Map<string, string>();
        for (const ch of channels) m.set(ch.id, ch.name);
        setChannelMap(m);
      })
      .catch(() => {});

    if (isTenant) {
      getMeAccounts()
        .then((accounts) => {
          const m = new Map<string, string>();
          for (const a of accounts) {
            if (a.email) {
              m.set(a.accountId, a.email);
              m.set(a.profileId, a.email);
            }
          }
          setAccountMap(m);
        })
        .catch(() => {});
    } else {
      listAccounts()
        .then((accounts) => {
          const m = new Map<string, string>();
          for (const a of accounts) m.set(`${a.nodeId}:${a.profileId}`, a.email);
          setAccountMap(m);
        })
        .catch(() => {});
    }
  }, [isTenant]);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    try {
      const data = isTenant ? await getMeLogs(500) : await getLogs({ limit: '500' });
      setRows(Array.isArray(data) ? data : []);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, [isTenant]);

  useEffect(() => { void fetchLogs(); }, [fetchLogs]);

  const q = query.trim().toLowerCase();
  const filtered = useMemo(() => q
    ? rows.filter(
        (r) =>
          r.model?.toLowerCase().includes(q) ||
          r.target?.toLowerCase().includes(q) ||
          r.status?.toLowerCase().includes(q) ||
          r.fallbackReason?.toLowerCase().includes(q) ||
          String(r.httpStatus).includes(q),
      )
    : rows, [rows, q]);

  useEffect(() => { setDispatchPage(0); }, [q]);

  const pagedDispatch = filtered.slice(dispatchPage * PAGE_SIZE, (dispatchPage + 1) * PAGE_SIZE);

  return (
    <div className="space-y-4">
      {detailId && <LogDetailModal requestId={detailId} isTenant={isTenant} onClose={() => setDetailId(null)} />}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <p className="text-xs text-muted">最近 500 条请求记录{!isTenant && ' · 点击行查看完整请求'}</p>
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
                {pagedDispatch.map((row, i) => (
                  <LogRow key={i} row={row} channelMap={channelMap} accountMap={accountMap}
                    onOpen={!isTenant && row.requestId ? () => setDetailId(row.requestId!) : undefined} />
                ))}
              </tbody>
            </table>
          </div>
          <div className="md:hidden space-y-3">
            {pagedDispatch.map((row, i) => <LogCard key={i} row={row} channelMap={channelMap} accountMap={accountMap}
              onOpen={!isTenant && row.requestId ? () => setDetailId(row.requestId!) : undefined} />)}
          </div>
          <PaginationBar
            page={dispatchPage} total={filtered.length} pageSize={PAGE_SIZE}
            onPrev={() => setDispatchPage((p) => Math.max(0, p - 1))}
            onNext={() => setDispatchPage((p) => p + 1)}
          />
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
  const actorDisplay = row.actorName ?? row.actor;
  const targetDisplay = row.targetName ?? row.target;
  return (
    <tr className="border-t border-line hover:bg-line/20 transition text-sm">
      <td className="px-3 py-2 text-xs text-muted whitespace-nowrap font-mono">{fmtTime(row.ts)}</td>
      <td className="px-3 py-2 text-ink font-mono text-xs truncate max-w-[120px]" title={row.actor}>{actorDisplay || '—'}</td>
      <td className="px-3 py-2">{actionBadge(row.action)}</td>
      <td className="px-3 py-2 text-muted text-xs truncate max-w-[200px]" title={row.target}>{targetDisplay || '—'}</td>
    </tr>
  );
}

function AuditCard({ row }: { row: AuditRecord }) {
  const actorDisplay = row.actorName ?? row.actor;
  const targetDisplay = row.targetName ?? row.target;
  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-2 text-sm">
      <div className="flex items-start justify-between gap-2">
        <span className="text-ink font-mono text-xs truncate" title={row.actor}>{actorDisplay || '—'}</span>
        {actionBadge(row.action)}
      </div>
      <p className="text-xs text-muted truncate" title={row.target}>{targetDisplay || '—'}</p>
      <p className="text-xs text-muted/60 font-mono">{fmtTime(row.ts)}</p>
    </div>
  );
}

function AuditTab() {
  const [rows, setRows] = useState<AuditRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [auditPage, setAuditPage] = useState(0);

  const fetchAudit = useCallback(async () => {
    setLoading(true);
    try {
      const data = await getAudit({ limit: '500' });
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
  const filtered = useMemo(() => q
    ? rows.filter(
        (r) =>
          r.actor?.toLowerCase().includes(q) ||
          r.actorName?.toLowerCase().includes(q) ||
          r.action?.toLowerCase().includes(q) ||
          r.target?.toLowerCase().includes(q) ||
          r.targetName?.toLowerCase().includes(q),
      )
    : rows, [rows, q]);

  useEffect(() => { setAuditPage(0); }, [q]);

  const pagedAudit = filtered.slice(auditPage * PAGE_SIZE, (auditPage + 1) * PAGE_SIZE);

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
                {pagedAudit.map((row, i) => <AuditRow key={i} row={row} />)}
              </tbody>
            </table>
          </div>
          <div className="md:hidden space-y-3">
            {pagedAudit.map((row, i) => <AuditCard key={i} row={row} />)}
          </div>
          <PaginationBar
            page={auditPage} total={filtered.length} pageSize={PAGE_SIZE}
            onPrev={() => setAuditPage((p) => Math.max(0, p - 1))}
            onNext={() => setAuditPage((p) => p + 1)}
          />
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
  balance_low:    { dot: 'bg-err',    badge: 'bg-err/10 text-err border-err/30',      label: '余额不足' },
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
  serverTargetName?: string,
): string {
  if (type === 'dispatch_ok') {
    const email = serverTargetName || resolveEmail(target, accountMap) || target || '节点';
    return `派单成功 · ${email}`;
  }
  if (type === 'ban') {
    const email = serverTargetName || resolveEmail(target, accountMap) || target || '节点';
    return `封控 · ${email}`;
  }
  if (type === 'fallback') {
    const cn = FALLBACK_REASON_CN[target] ?? target;
    const channelId = typeof detail['channel'] === 'string' ? detail['channel'] : '';
    const channelName = (channelId ? channelMap.get(channelId) : undefined) ?? serverTargetName;
    const base = cn ? `保底触发 · ${cn}` : '保底触发';
    return channelName ? `${base} · ${channelName}` : base;
  }
  if (type === 'quota_limited') {
    const email = serverTargetName || resolveEmail(target, accountMap) || target || '节点';
    return `账户限额 · ${email}`;
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
  if (type === 'balance_low') {
    const channelName = serverTargetName ?? channelMap.get(target) ?? target;
    const balance = typeof detail['balance'] === 'number' ? `$${(detail['balance'] as number).toFixed(2)}` : undefined;
    const alert = typeof detail['alert'] === 'number' ? `$${(detail['alert'] as number).toFixed(2)}` : undefined;
    const base = `余额不足 · ${channelName}`;
    return balance && alert ? `${base} · ${balance}/${alert}` : base;
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
  const targetText = renderTargetText(row.type, row.target ?? '', detail, accountMap, channelMap, row.targetName);
  const showDetail = row.detail && Object.keys(row.detail).length > 0 && row.type !== 'fallback' && row.type !== 'balance_low';

  // Build display detail: replace detail.account with resolved email if available
  const displayDetail = showDetail && (row.detailAccount || detail['account'])
    ? (() => {
        const copy = { ...detail };
        if (row.detailAccount && copy['account']) copy['account'] = row.detailAccount;
        return copy;
      })()
    : detail;

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
            {JSON.stringify(displayDetail, null, 2)}
          </pre>
        )}
      </div>
    </div>
  );
}

function EventsTab({ isTenant }: { isTenant: boolean }) {
  const [rows, setRows] = useState<EventRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [accountMap, setAccountMap] = useState<Map<string, string>>(new Map());
  const [channelMap, setChannelMap] = useState<Map<string, string>>(new Map());
  const [eventsPage, setEventsPage] = useState(0);

  const fetchEvents = useCallback(async () => {
    setLoading(true);
    try {
      const data = isTenant ? await getMeEvents(500) : await getEvents({ limit: '500' });
      setRows(Array.isArray(data) ? data : []);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, [isTenant]);

  useEffect(() => { void fetchEvents(); }, [fetchEvents]);

  useEffect(() => {
    if (isTenant) {
      getMeAccounts()
        .then((accounts) => {
          const m = new Map<string, string>();
          for (const a of accounts) {
            if (a.email) {
              m.set(a.accountId, a.email);
              m.set(a.profileId, a.email);
            }
          }
          setAccountMap(m);
        })
        .catch(() => {});
    } else {
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
    }
    (isTenant ? listMeFallback() : listFallbackChannels())
      .then((channels) => {
        const m = new Map<string, string>();
        for (const ch of channels) m.set(ch.id, ch.name);
        setChannelMap(m);
      })
      .catch(() => {});
  }, [isTenant]);

  const q = query.trim().toLowerCase();
  const filtered = useMemo(() => q
    ? rows.filter(
        (r) =>
          r.type?.toLowerCase().includes(q) ||
          r.target?.toLowerCase().includes(q) ||
          r.targetName?.toLowerCase().includes(q),
      )
    : rows, [rows, q]);

  useEffect(() => { setEventsPage(0); }, [q]);

  const pagedEvents = filtered.slice(eventsPage * PAGE_SIZE, (eventsPage + 1) * PAGE_SIZE);

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <p className="text-xs text-muted">最近 500 条系统事件时间线</p>
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
        <>
          <div className="bg-surface border border-line rounded-xl px-5 pt-5 pb-0">
            {pagedEvents.map((row, i) => (
              <EventItem key={i} row={row} accountMap={accountMap} channelMap={channelMap} />
            ))}
          </div>
          <PaginationBar
            page={eventsPage} total={filtered.length} pageSize={PAGE_SIZE}
            onPrev={() => setEventsPage((p) => Math.max(0, p - 1))}
            onNext={() => setEventsPage((p) => p + 1)}
          />
        </>
      )}
    </div>
  );
}

// ============================================================
// Unified Logs page
// ============================================================
type Tab = 'dispatch' | 'audit' | 'events';

const ALL_TABS: { key: Tab; label: string }[] = [
  { key: 'dispatch', label: '调度日志' },
  { key: 'audit',    label: '审计日志' },
  { key: 'events',   label: '事件' },
];

export default function Logs() {
  const { isTenant } = useAuth();
  const [tab, setTab] = useState<Tab>('dispatch');

  // Tenants don't see the 审计 (audit) tab.
  const tabs = isTenant ? ALL_TABS.filter((t) => t.key !== 'audit') : ALL_TABS;
  const activeTab: Tab = isTenant && tab === 'audit' ? 'dispatch' : tab;

  return (
    <div className="p-4 md:p-6 space-y-4">
      {/* Header */}
      <h1 className="text-2xl font-semibold text-ink">日志</h1>

      {/* Tab switcher */}
      <div className="flex gap-1 border-b border-line">
        {tabs.map(({ key, label }) => (
          <button
            key={key}
            onClick={() => setTab(key)}
            className={[
              'px-4 py-2 text-sm font-medium border-b-2 -mb-px transition',
              activeTab === key
                ? 'border-accent text-accent'
                : 'border-transparent text-muted hover:text-ink',
            ].join(' ')}
          >
            {label}
          </button>
        ))}
      </div>

      {/* Tab panels */}
      {activeTab === 'dispatch' && <DispatchLogsTab isTenant={isTenant} />}
      {activeTab === 'audit'    && !isTenant && <AuditTab />}
      {activeTab === 'events'   && <EventsTab isTenant={isTenant} />}
    </div>
  );
}
