// Shared account/node status presentation: Chinese labels + Tailwind color classes.
// Keep keys in sync with internal/state Account.Status():
//   active | banned (open/cooling) | half_open (probing) | permanent | offline | disabled

export const STATUS_LABELS: Record<string, string> = {
  active: '活跃',
  reserve: '待命',   // elastic standby — beyond baseline, not yet scaled up
  banned: '封控·冷却',
  half_open: '半开·探测',
  permanent: '封禁(永久)',
  cooldown: '限流·冷却',
  limited: '限额(配额)', // quota-rotated out of dispatch until quota resets (quota-3)
  offline: '离线',
  disabled: '禁用',
};

export const STATUS_COLORS: Record<string, string> = {
  active: 'bg-green-500/20 text-green-400 border-green-500/40',
  reserve: 'bg-muted/15 text-muted-foreground border-muted/30', // elastic standby — grey/muted
  banned: 'bg-red-500/20 text-red-400 border-red-500/40',
  half_open: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40',
  permanent: 'bg-red-600/30 text-red-300 border-red-600/50',
  cooldown: 'bg-cyan-500/20 text-cyan-400 border-cyan-500/40',
  limited: 'bg-orange-500/20 text-orange-400 border-orange-500/40',
  offline: 'bg-gray-500/20 text-gray-400 border-gray-500/40',
  disabled: 'bg-gray-500/10 text-gray-500 border-gray-500/20',
};

export function statusLabel(status: string): string {
  return STATUS_LABELS[status] ?? status;
}

export function statusColor(status: string): string {
  return STATUS_COLORS[status] ?? STATUS_COLORS.offline;
}
