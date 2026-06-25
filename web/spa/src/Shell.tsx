// ============================================================
// Tower SPA — responsive app shell
// Desktop: left sidebar with labels
// Tablet (md): icon-only rail
// Mobile (<md): bottom nav (first 5 items)
// Topbar: title + ⌘K + theme toggle + logout
// ⌘K: command palette (filter + navigate)
// ============================================================
import { useState, useEffect, useCallback, type ReactNode } from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import { useAuth } from './auth';
import { useTheme } from './theme';
import { changePassword } from './api';

// ------------------------------------------------------------------
// Change-password modal
// ------------------------------------------------------------------
function ChangePasswordModal({ onClose }: { onClose: () => void }) {
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (newPassword.length < 8) {
      setErr('新密码至少 8 位');
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      await changePassword({ oldPassword, newPassword });
      setSuccess(true);
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : '修改失败');
    } finally {
      setBusy(false);
    }
  }

  const inputCls =
    'w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink ' +
    'focus:outline-none focus:border-accent transition';

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center px-4 bg-black/50"
      onClick={onClose}
    >
      <div
        className="w-full max-w-sm bg-surface border border-line rounded-xl shadow-2xl p-6 space-y-4"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-base font-semibold text-ink">修改密码</h2>

        {success ? (
          <div className="space-y-4">
            <div className="bg-ok/10 border border-ok/30 rounded-lg p-3 text-ok text-sm">
              密码修改成功！
            </div>
            <button
              onClick={onClose}
              className="w-full px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                         hover:bg-accent/80 transition"
            >
              关闭
            </button>
          </div>
        ) : (
          <form onSubmit={(e) => { void handleSubmit(e); }} className="space-y-3">
            {err && (
              <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-sm">{err}</div>
            )}
            <div>
              <label className="block text-xs text-muted mb-1">当前密码</label>
              <input
                type="password"
                required
                value={oldPassword}
                onChange={(e) => setOldPassword(e.target.value)}
                autoComplete="current-password"
                className={inputCls}
              />
            </div>
            <div>
              <label className="block text-xs text-muted mb-1">新密码 (≥8位)</label>
              <input
                type="password"
                required
                minLength={8}
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                autoComplete="new-password"
                className={inputCls}
              />
            </div>
            <div className="flex gap-3 pt-1">
              <button
                type="submit"
                disabled={busy}
                className="flex-1 px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                           hover:bg-accent/80 disabled:opacity-50 transition"
              >
                {busy ? '提交中…' : '确认修改'}
              </button>
              <button
                type="button"
                onClick={onClose}
                disabled={busy}
                className="px-4 py-2 text-sm font-medium border border-line text-muted rounded-lg
                           hover:text-ink transition"
              >
                取消
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Nav item definition
// ------------------------------------------------------------------
interface NavItem {
  path: string;
  label: string;
  icon: string;
  adminOnly?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  { path: '/',          label: '看板',     icon: '◈' },
  { path: '/dispatch',  label: '调度',     icon: '⇄', adminOnly: true },
  { path: '/nodes',     label: '节点',     icon: '⬡' },
  { path: '/accounts',  label: '号库',     icon: '⚿' },
  { path: '/fallback',  label: '保底', icon: '⤵', adminOnly: true },
  { path: '/logs',      label: '日志',     icon: '≡' },
  { path: '/billing',   label: '计费',     icon: '₿', adminOnly: true },
  { path: '/settings',  label: '设置',     icon: '⚙', adminOnly: true },
];

// Tenant nav: limited self-service set (own data only)
const TENANT_NAV: NavItem[] = [
  { path: '/',         label: '看板',     icon: '◈' },
  { path: '/dispatch', label: '调度',     icon: '⇄' },
  { path: '/accounts', label: '号库',     icon: '⚿' },
  { path: '/fallback', label: '保底', icon: '⤵' },
  { path: '/logs',     label: '日志',     icon: '≡' },
  { path: '/billing',  label: '计费',     icon: '₿' },
  { path: '/settings', label: '设置',     icon: '⚙' },
];

// Settings sub-pages — not in primary sidebar, but searchable via palette
const SETTINGS_ITEMS: NavItem[] = [
  { path: '/policies',    label: '封控策略', icon: '⛨', adminOnly: true },
  { path: '/slots',       label: '时段槽位', icon: '⏱', adminOnly: true },
  { path: '/desired',     label: '配置对账', icon: '⇌', adminOnly: true },
  { path: '/keys',        label: '调度密钥', icon: '🔑', adminOnly: true },
  { path: '/ban-analysis',label: '封号分析', icon: '⚠', adminOnly: true },
  { path: '/users',       label: '用户',     icon: '👤', adminOnly: true },
];

// ------------------------------------------------------------------
// Command palette
// ------------------------------------------------------------------
interface PaletteProps {
  items: NavItem[];
  onClose: () => void;
}

function Palette({ items, onClose }: PaletteProps) {
  const [query, setQuery] = useState('');
  const navigate = useNavigate();

  const filtered = items.filter(
    (i) =>
      i.label.includes(query) ||
      i.path.toLowerCase().includes(query.toLowerCase()),
  );

  const go = useCallback(
    (path: string) => {
      navigate(path);
      onClose();
    },
    [navigate, onClose],
  );

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-24 px-4 bg-black/50"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md bg-surface border border-line rounded-xl shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center border-b border-line px-4">
          <span className="text-muted mr-2">🔍</span>
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="跳转到页面…"
            className="flex-1 bg-transparent py-3 text-ink text-sm outline-none placeholder:text-muted"
          />
          <kbd className="text-xs text-muted border border-line rounded px-1">Esc</kbd>
        </div>
        <ul className="max-h-72 overflow-y-auto py-1">
          {filtered.map((item) => (
            <li key={item.path}>
              <button
                onClick={() => go(item.path)}
                className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-ink
                           hover:bg-line text-left transition"
              >
                <span className="text-base w-5 text-center">{item.icon}</span>
                <span>{item.label}</span>
                <span className="ml-auto text-xs text-muted">{item.path}</span>
              </button>
            </li>
          ))}
          {filtered.length === 0 && (
            <li className="px-4 py-3 text-sm text-muted">无匹配</li>
          )}
        </ul>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Shell
// ------------------------------------------------------------------
export function Shell({ children }: { children: ReactNode }) {
  const { role, isTenant, logout } = useAuth();
  const { theme, toggle } = useTheme();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [changePwOpen, setChangePwOpen] = useState(false);

  // Filter nav by role (superadmin/admin/operator all see admin-only items)
  const isAdmin = role === 'admin' || role === 'superadmin' || role === 'operator';
  // Tenants get a limited self-service nav; everyone else gets the admin app.
  const items = isTenant
    ? TENANT_NAV
    : NAV_ITEMS.filter((i) => !i.adminOnly || isAdmin);
  // Palette: primary nav + settings sub-pages (so moved pages remain searchable)
  const paletteItems = isTenant
    ? items
    : [
        ...items,
        ...SETTINGS_ITEMS.filter((i) => !i.adminOnly || isAdmin),
      ];

  // ⌘K / Ctrl+K global shortcut
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    [
      'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition',
      isActive
        ? 'bg-accent/15 text-accent'
        : 'text-muted hover:bg-line hover:text-ink',
    ].join(' ');

  const iconLinkClass = ({ isActive }: { isActive: boolean }) =>
    [
      'flex items-center justify-center w-10 h-10 rounded-lg text-lg transition',
      isActive
        ? 'bg-accent/15 text-accent'
        : 'text-muted hover:bg-line hover:text-ink',
    ].join(' ');

  return (
    <div className="flex h-screen bg-bg text-ink overflow-hidden">
      {/* ---- Desktop sidebar (lg+) ---- */}
      <aside className="hidden lg:flex flex-col w-56 shrink-0 border-r border-line bg-surface">
        <div className="px-4 py-5">
          <span className="text-xl font-bold text-accent">Tower</span>
        </div>
        <nav className="flex-1 overflow-y-auto px-2 space-y-0.5">
          {items.map((item) => (
            <NavLink key={item.path} to={item.path} end={item.path === '/'} className={navLinkClass}>
              <span className="text-base">{item.icon}</span>
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>
      </aside>

      {/* ---- Tablet icon rail (md–lg) ---- */}
      <aside className="hidden md:flex lg:hidden flex-col w-14 shrink-0 border-r border-line bg-surface items-center py-4 gap-1">
        <span className="text-lg font-bold text-accent mb-3">T</span>
        <nav className="flex flex-col gap-1">
          {items.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              end={item.path === '/'}
              className={iconLinkClass}
              title={item.label}
            >
              {item.icon}
            </NavLink>
          ))}
        </nav>
      </aside>

      {/* ---- Main content area ---- */}
      <div className="flex flex-col flex-1 min-w-0">
        {/* Topbar */}
        <header className="flex items-center h-12 px-4 border-b border-line bg-surface shrink-0 gap-3">
          {/* Title (mobile only — desktop/tablet show sidebar) */}
          <span className="md:hidden text-base font-bold text-accent mr-auto">Tower</span>
          <span className="hidden md:block text-sm font-medium text-muted mr-auto">控制台</span>

          {/* ⌘K button */}
          <button
            onClick={() => setPaletteOpen(true)}
            className="flex items-center gap-1.5 text-xs text-muted border border-line rounded-md px-2 py-1
                       hover:border-accent hover:text-accent transition"
            title="Command palette (⌘K)"
          >
            <span>⌘K</span>
          </button>

          {/* Theme toggle */}
          <button
            onClick={toggle}
            className="w-8 h-8 flex items-center justify-center text-muted hover:text-ink rounded-lg hover:bg-line transition"
            title={theme === 'dark' ? '切换亮色' : '切换暗色'}
          >
            {theme === 'dark' ? '☀' : '☽'}
          </button>

          {/* Change password */}
          <button
            onClick={() => setChangePwOpen(true)}
            className="w-8 h-8 flex items-center justify-center text-muted hover:text-ink rounded-lg hover:bg-line transition"
            title="改密"
          >
            🔒
          </button>

          {/* Logout */}
          <button
            onClick={() => { void logout(); }}
            className="w-8 h-8 flex items-center justify-center text-muted hover:text-err rounded-lg hover:bg-line transition"
            title="退出登录"
          >
            ⏻
          </button>
        </header>

        {/* Page content */}
        <main className="flex-1 overflow-y-auto pb-16 md:pb-0">
          {children}
        </main>
      </div>

      {/* ---- Mobile bottom nav (<md) ---- */}
      <nav className="md:hidden fixed bottom-0 inset-x-0 flex overflow-x-auto bg-surface border-t border-line z-40">
        {items.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            end={item.path === '/'}
            className={({ isActive }) =>
              [
                'flex-none flex flex-col items-center gap-0.5 py-2 px-3 text-xs transition',
                isActive ? 'text-accent' : 'text-muted',
              ].join(' ')
            }
          >
            <span className="text-lg leading-none">{item.icon}</span>
            <span className="whitespace-nowrap">{item.label}</span>
          </NavLink>
        ))}
      </nav>

      {/* Command palette */}
      {paletteOpen && (
        <Palette items={paletteItems} onClose={() => setPaletteOpen(false)} />
      )}

      {/* Change-password modal */}
      {changePwOpen && (
        <ChangePasswordModal onClose={() => setChangePwOpen(false)} />
      )}
    </div>
  );
}
