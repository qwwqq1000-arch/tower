// ============================================================
// Tower SPA — Dispatch real-time dashboard
// SSE primary; polling fallback on error
// ============================================================
import { useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { getDispatchStatus, getServerStatus } from '../api';
import type { DispatchStatus, DispatchAccountSnapshot, DispatchEvent, DispatchFallbackChannel, ServerStatus } from '../types';
import { useAuth } from '../auth';
import { TenantDispatch } from './tenant';
import { StatusBadge, statusRank } from '../components/AccountStatus';

// fmtRecover formats a future ms timestamp as a countdown ("剩 20秒" / "剩 1:23" / "即将").
function fmtRecover(ms: number, now: number): string {
  const remain = ms - now;
  if (remain <= 0) return '即将';
  const s = Math.ceil(remain / 1000);
  if (s < 60) return `剩 ${s}秒`;
  const m = Math.floor(s / 60);
  return `剩 ${m}:${String(s % 60).padStart(2, '0')}`;
}

// RecoverCountdown ticks every second so the display updates without waiting for
// the next SSE push (which arrives every ~2 s).
function RecoverCountdown({ recoverAt }: { recoverAt: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return <span>恢复 {fmtRecover(recoverAt, now)}</span>;
}

// ------------------------------------------------------------------
// Top stats bar
// ------------------------------------------------------------------
function StatsBar({ data }: { data: DispatchStatus }) {
  const { nodes, elastic, traffic, accounts } = data;
  const inflight = accounts.reduce((s, a) => s + a.inflight, 0);
  const rate = traffic.total > 0
    ? ((traffic.ok / traffic.total) * 100).toFixed(1)
    : '—';

  const stats: { label: string; value: string }[] = [
    { label: '号库 (启用/总)', value: `${nodes.enabled} / ${nodes.total}` },
  ];
  if (elastic && elastic.max > 0) {
    stats.push({ label: '弹性 (当前/最大)', value: `${elastic.current} / ${elastic.max}` });
  }
  stats.push(
    { label: '总请求', value: traffic.total.toLocaleString() },
    { label: '成功率', value: traffic.total > 0 ? `${rate}%` : '—' },
    { label: '并发中', value: inflight.toString() },
    { label: 'RPM', value: traffic.rpm != null ? traffic.rpm.toString() : '—' },
  );

  return (
    <div className={`grid grid-cols-2 gap-4 mb-6 ${stats.length <= 5 ? 'sm:grid-cols-5' : 'sm:grid-cols-6'}`}>
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

const ACCOUNTS_PAGE_SIZE = 10;

export function ConcurrencyPanel({ accounts }: { accounts: DispatchAccountSnapshot[] }) {
  const [page, setPage] = useState(0);
  const sorted = [...accounts].sort((x, y) => statusRank(x.status) - statusRank(y.status));
  const pageCount = Math.max(1, Math.ceil(sorted.length / ACCOUNTS_PAGE_SIZE));
  const cur = Math.min(page, pageCount - 1);
  const pageRows = sorted.slice(cur * ACCOUNTS_PAGE_SIZE, (cur + 1) * ACCOUNTS_PAGE_SIZE);
  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden mb-6">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink flex items-center justify-between">
        <span>并发 / 账户</span>
        <span className="text-xs text-muted font-normal">{sorted.length} 个号</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-line text-left text-xs text-muted whitespace-nowrap">
              <th className="px-4 py-2 font-medium">邮箱 / 账户</th>
              <th className="px-4 py-2 font-medium">状态</th>
              <th className="px-4 py-2 font-medium">模型</th>
              <th className="px-4 py-2 font-medium text-right">并发中</th>
              <th className="px-4 py-2 font-medium text-right">可用</th>
              <th className="px-4 py-2 font-medium text-right">今日消费</th>
              <th className="px-4 py-2 font-medium text-right">单号RPM</th>
            </tr>
          </thead>
          <tbody>
            {accounts.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-6 text-center text-muted text-xs">无数据</td>
              </tr>
            )}
            {pageRows.map((a) => (
              <tr key={a.key} className="border-b border-line/50 hover:bg-line/30 transition">
                <td className="px-4 py-2">
                  <p className="text-sm text-ink font-medium">{(a.label || '—').split('@')[0]}</p>
                </td>
                <td className="px-4 py-2">
                  <StatusBadge status={a.status} limitedUntil={a.limitedUntil} limitReason={a.limitReason} pausedUntil={a.pausedUntil} />
                  {(a.status === 'banned' || a.status === 'half_open' || a.status === 'cooldown') && a.recoverAt && a.recoverAt > 0 && (
                    <div className="text-[10px] text-muted mt-0.5"><RecoverCountdown recoverAt={a.recoverAt} /></div>
                  )}
                </td>
                <td className="px-4 py-2 text-xs text-muted font-mono">{a.pinnedModel ? a.pinnedModel.replace('claude-', '') : '—'}</td>
                <td className="px-4 py-2 text-right tabular-nums">{a.inflight}</td>
                <td className="px-4 py-2 text-right tabular-nums">{a.available}</td>
                <td className="px-4 py-2 text-right tabular-nums text-xs text-muted">{fmtCost(a.todayCostUsd)}</td>
                <td className="px-4 py-2 text-right tabular-nums text-xs text-muted">{a.rpm != null && a.rpm > 0 ? a.rpm : '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {pageCount > 1 && (
        <div className="flex items-center justify-end gap-3 px-4 py-2 border-t border-line text-xs text-muted">
          <button
            onClick={() => setPage(Math.max(0, cur - 1))}
            disabled={cur === 0}
            className="px-2 py-1 rounded border border-line hover:bg-line/30 transition disabled:opacity-40 disabled:cursor-not-allowed"
          >
            上一页
          </button>
          <span className="tabular-nums">第 {cur + 1} / {pageCount} 页</span>
          <button
            onClick={() => setPage(Math.min(pageCount - 1, cur + 1))}
            disabled={cur >= pageCount - 1}
            className="px-2 py-1 rounded border border-line hover:bg-line/30 transition disabled:opacity-40 disabled:cursor-not-allowed"
          >
            下一页
          </button>
        </div>
      )}
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
function fmtQuota(v: number | undefined): string {
  if (v == null) return '—';
  return `${(v * 100).toFixed(0)}%`;
}

function quotaCls(v: number | undefined): string {
  if (v == null || v === 0) return 'text-ink';
  if (v >= 0.9) return 'text-red-400';
  if (v >= 0.7) return 'text-yellow-400';
  return 'text-green-400';
}

function TrafficPanel({ data }: { data: DispatchStatus }) {
  const { traffic } = data;
  const items = [
    { label: '总请求', value: traffic.total.toLocaleString() },
    { label: '成功', value: traffic.ok.toLocaleString(), cls: 'text-green-400' },
    { label: '错误', value: traffic.error.toLocaleString(), cls: 'text-red-400' },
    { label: '5h 均额度', value: fmtQuota(data.quota5hAvg), cls: quotaCls(data.quota5hAvg) },
    { label: '7d 均额度', value: fmtQuota(data.quota7dAvg), cls: quotaCls(data.quota7dAvg) },
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
    case 'ban_detected':  return { label: '封禁触发',    cls: 'bg-red-500/20 text-red-400 border-red-500/40' };
    case 'ban_permanent': return { label: '永久封禁',    cls: 'bg-red-600/30 text-red-300 border-red-600/50' };
    case 'retry':         return { label: '节点报错',    cls: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40' };
    case 'cooldown':      return { label: '限流冷却',    cls: 'bg-cyan-500/20 text-cyan-400 border-cyan-500/40' };
    case 'account_recovered': return { label: '账户恢复', cls: 'bg-green-500/20 text-green-400 border-green-500/40' };
    case 'recover':       return { label: '恢复',        cls: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40' };
    case 'fallback':      return { label: '保底触发',    cls: 'bg-blue-500/20 text-blue-400 border-blue-500/40' };
    case 'quota_limited': return { label: '账户限额',    cls: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40' };
    case 'session_exile': return { label: '会话连错放逐', cls: 'bg-orange-500/20 text-orange-400 border-orange-500/40' };
    case 'scale_up':      return { label: '弹性扩容',    cls: 'bg-blue-500/20 text-blue-400 border-blue-500/40' };
    case 'scale_down':    return { label: '弹性缩容',    cls: 'bg-gray-500/20 text-gray-400 border-gray-500/40' };
    case 'model_pin':     return { label: '钉定模型',    cls: 'bg-indigo-500/20 text-indigo-300 border-indigo-500/40' };
    case 'model_scale_up':return { label: '模型扩容',    cls: 'bg-blue-500/20 text-blue-400 border-blue-500/40' };
    case 'model_exhausted':return { label: '模型不足',   cls: 'bg-orange-500/20 text-orange-400 border-orange-500/40' };
    case 'balance_low':   return { label: '余额不足',    cls: 'bg-red-500/20 text-red-400 border-red-500/40' };
    default:
      if (type === 'dispatch_err' || type.endsWith('_err') || type === 'error') {
        return { label: '错误', cls: 'bg-orange-500/20 text-orange-400 border-orange-500/40' };
      }
      // Unknown type → show a generic Chinese label (keep raw type in title via detail).
      return { label: '事件', cls: 'bg-blue-500/20 text-blue-400 border-blue-500/40' };
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
  if (type === 'ban_detected' || type === 'ban_permanent' || type === 'retry' || type === 'account_recovered') {
    let status: number | undefined; let streak: number | undefined; let detailEmail: string | undefined;
    try {
      const d: any = typeof detail === 'string' ? JSON.parse(detail) : detail;
      if (d && typeof d.status === 'number') status = d.status;
      if (d && typeof d.streak === 'number') streak = d.streak;
      if (d && typeof d.email === 'string' && d.email) detailEmail = d.email;
    } catch { /* ignore */ }
    // account_recovered's target is the raw account id (not node:profile), so the
    // accountNames map misses — prefer the email carried in detail.
    const email = detailEmail ?? accountNames?.get(target) ?? target;
    const head = type === 'ban_permanent' ? '永久封禁' : type === 'retry' ? '节点报错' : type === 'account_recovered' ? '账户恢复' : '封禁触发';
    const parts = [head, email];
    if (status) parts.push(`HTTP ${status}`);
    if (streak !== undefined && (type === 'ban_detected' || type === 'ban_permanent')) parts.push(`连续${streak}次`);
    return parts.filter(Boolean).join(' · ');
  }
  if (type === 'cooldown') {
    const email = accountNames?.get(target) ?? target;
    let status: number | undefined; let sec: number | undefined;
    try {
      const d: any = typeof detail === 'string' ? JSON.parse(detail) : detail;
      if (d && typeof d.status === 'number') status = d.status;
      if (d && typeof d.seconds === 'number') sec = d.seconds;
    } catch { /* ignore */ }
    const parts = ['限流冷却', email];
    if (status) parts.push(`HTTP ${status}`);
    if (sec) parts.push(`${sec}秒`);
    return parts.filter(Boolean).join(' · ');
  }
  if (type === 'fallback') {
    // Resolve reason + channel from detail (new shape: reason/channelId/channelName).
    let channelName: string | undefined; let reason = target;
    try {
      const d: any = typeof detail === 'string' ? JSON.parse(detail) : detail;
      if (d && typeof d.reason === 'string' && d.reason) reason = d.reason;
      if (d && typeof d.channelName === 'string' && d.channelName) channelName = d.channelName;
      else {
        const cid = d && (typeof d.channelId === 'string' ? d.channelId : (typeof d.channel === 'string' ? d.channel : ''));
        if (cid) channelName = fallbackNames?.get(cid);
      }
    } catch { /* ignore */ }
    const cn = FALLBACK_REASON_CN[reason] ?? reason;
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
  if (type === 'model_pin' || type === 'model_scale_up') {
    let model = ''; let acct = '';
    try {
      const d: any = typeof detail === 'string' ? JSON.parse(detail) : detail;
      if (d) { if (typeof d.model === 'string') model = d.model; if (typeof d.account === 'string') acct = d.account; }
    } catch { /* ignore */ }
    const email = accountNames?.get(target) ?? (acct ? accountNames?.get(acct) : undefined) ?? acct ?? target;
    const head = type === 'model_scale_up' ? '模型扩容' : '钉定模型';
    return [head, email, model].filter(Boolean).join(' · ');
  }
  if (type === 'model_exhausted') {
    let model = '';
    try { const d: any = typeof detail === 'string' ? JSON.parse(detail) : detail; if (d && typeof d.model === 'string') model = d.model; } catch { /* ignore */ }
    return model ? `模型不足 · ${model}` : '模型不足';
  }
  if (target.startsWith('fallback:')) {
    const id = target.slice('fallback:'.length);
    const name = fallbackNames?.get(id);
    return name !== undefined ? `保底: ${name}` : '保底';
  }
  if (target.startsWith('n_') || target.startsWith('fc_') || target.startsWith('fb:')) return '';
  return target || '';
}

// useElementHeight measures an element's live height (offsetHeight) and keeps it
// in sync via ResizeObserver. Used so the event timeline can cap its scroll region
// to the concurrency panel's height (they sit side-by-side and should stay balanced).
export function useElementHeight<T extends HTMLElement>(): [RefObject<T | null>, number] {
  const ref = useRef<T>(null);
  const [h, setH] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setH(el.offsetHeight));
    ro.observe(el);
    setH(el.offsetHeight);
    return () => ro.disconnect();
  }, []);
  return [ref, h];
}

// ------------------------------------------------------------------
// Events timeline
// ------------------------------------------------------------------
export function EventTimeline({
  events,
  fallbackNames,
  accountNames,
  maxHeightPx,
}: {
  events: DispatchEvent[];
  fallbackNames: Map<string, string>;
  accountNames: Map<string, string>;
  // When set, the list scrolls within ~this height (minus the header) so it tracks
  // the concurrency panel; when content is shorter the card simply hugs it (no black).
  maxHeightPx?: number;
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
    // self-start so the card hugs its content height in the 2-col grid instead of
    // stretching to match the taller left column (which left a big black empty block).
    // No inner max-height/scroll: the list grows downward with its (≤20) rows.
    <div className="bg-surface border border-line rounded-xl overflow-hidden">
      <div className="px-4 py-3 border-b border-line text-sm font-medium text-ink">事件时间线（最近 20）</div>
      {events.length === 0 && (
        <p className="px-4 py-6 text-center text-muted text-xs">暂无事件</p>
      )}
      <ul
        className={`divide-y divide-line/50 ${maxHeightPx ? 'overflow-y-auto' : ''}`}
        style={maxHeightPx ? { maxHeight: Math.max(160, maxHeightPx - 49) } : undefined}
      >
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
  const [leftColRef, leftColH] = useElementHeight<HTMLDivElement>();
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

  // Build accountNames / fallbackNames from the status data itself (no extra requests).
  useEffect(() => {
    if (!data) return;
    const am = new Map<string, string>();
    for (const a of data.accounts) {
      if (a.label) am.set(a.key, a.label);
    }
    setAccountNames(am);
    const fm = new Map<string, string>();
    if (data.fallbackChannels) {
      for (const ch of data.fallbackChannels) fm.set(ch.id, ch.name);
    }
    setFallbackNames(fm);
  }, [data]);

  useEffect(() => {
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
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 items-start">
            <div ref={leftColRef}>
              <ConcurrencyPanel accounts={data.accounts} />
              <FallbackChannelsPanel channels={data.fallbackChannels ?? []} />
              <TrafficPanel data={data} />
            </div>
            <EventTimeline events={data.events} fallbackNames={fallbackNames} accountNames={accountNames} maxHeightPx={leftColH > 0 ? leftColH : undefined} />
          </div>
        </>
      )}
    </div>
  );
}
