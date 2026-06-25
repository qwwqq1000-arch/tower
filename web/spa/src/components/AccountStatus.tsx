// Shared account-status rendering used by the admin 号库, the 调度 panel, and the
// tenant view so a quota-limited account shows the SAME live recovery countdown and the
// SAME ordering everywhere (quota-3 / 限额恢复倒计时).
import { useEffect, useState } from 'react';
import { statusColor, statusLabel } from '../lib/status';

// fmtCountdown renders a remaining-ms duration as a compact countdown.
export function fmtCountdown(remainMs: number): string {
  if (remainMs <= 0) return '已到';
  const s = Math.floor(remainMs / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}天${h}时`;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`;
  return `${m}:${String(sec).padStart(2, '0')}`;
}

// StatusBadge shows a quota-limited account as a live recovery countdown (timezone-
// agnostic, from the absolute limitedUntil), and any other status as its normal label.
// When limitReason is "5h" or "7d", the label reads "5h限额" / "7d限额" respectively.
export function StatusBadge({ status, limitedUntil, limitReason, className = '' }: { status?: string; limitedUntil?: number; limitReason?: string; className?: string }) {
  const limited = status === 'limited';
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!limited || !limitedUntil || limitedUntil <= 0) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [limited, limitedUntil]);
  if (!status) return null;
  const cls = `inline-flex items-center px-1.5 py-0.5 rounded border text-[10px] font-mono ${statusColor(status)} ${className}`;
  if (limited) {
    const cd = limitedUntil && limitedUntil > 0 ? fmtCountdown(limitedUntil - now) : null;
    const prefix = limitReason === '5h' ? '5h限额' : limitReason === '7d' ? '7d限额' : '限额';
    return <span className={cls}>{prefix}{cd ? `(恢复倒计时 ${cd})` : '(配额)'}</span>;
  }
  return <span className={cls}>{statusLabel(status)}</span>;
}

// statusRank orders accounts: 活跃 (0) → 待命/reserve (1) → cooldown/half_open (2)
// → limited (3) → banned/permanent/disabled LAST (4).
// Lower rank = higher in the list.
export function statusRank(status?: string): number {
  switch (status) {
    case 'banned':
    case 'permanent':
    case 'disabled':
      return 4; // banned last
    case 'limited':
      return 3;
    case 'cooldown':
    case 'half_open':
      return 2;
    case 'reserve':
      return 1; // 待命: after active, before limited/banned
    default:
      return 0; // active / unknown → first
  }
}
