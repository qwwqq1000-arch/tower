// ============================================================
// Tower SPA — AuthProvider + useAuth + LoginGate + RequireRole
// ============================================================
import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  type ReactNode,
} from 'react';
import { Navigate } from 'react-router-dom';
import { me, login as apiLogin, logout as apiLogout, changePassword } from './api';
import type { User } from './types';

// ------------------------------------------------------------------
// Context
// ------------------------------------------------------------------
interface AuthCtx {
  user: User | null;
  role: string | null;
  isTenant: boolean;
  perms: string[];
  loading: boolean;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
}

const Ctx = createContext<AuthCtx | null>(null);

export function useAuth(): AuthCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error('useAuth must be used within <AuthProvider>');
  return ctx;
}

// ------------------------------------------------------------------
// Provider
// ------------------------------------------------------------------
export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const u = await me();
      setUser(u);
    } catch {
      setUser(null);
    } finally {
      setLoading(false);
    }
  }, []);

  const logout = useCallback(async () => {
    try { await apiLogout(); } catch { /* ignore */ }
    setUser(null);
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  const value: AuthCtx = {
    user,
    role: user?.role ?? null,
    isTenant: user?.role === 'tenant',
    perms: user?.perms ?? [],
    loading,
    refresh,
    logout,
  };

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

// ------------------------------------------------------------------
// ForcedPasswordChange — shown when the backend sets must_change_pw
// ------------------------------------------------------------------
function ForcedPasswordChange({ onDone, onLogout }: { onDone: () => void; onLogout: () => void }) {
  const [oldPw, setOldPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [confirmPw, setConfirmPw] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const inputCls =
    'w-full bg-bg border border-line rounded-lg px-3 py-2 text-ink text-sm ' +
    'focus:outline-none focus:ring-2 focus:ring-accent';

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (newPw.length < 8) {
      setError('新密码至少 8 位');
      return;
    }
    if (newPw !== confirmPw) {
      setError('两次输入的密码不一致');
      return;
    }
    setSubmitting(true);
    try {
      await changePassword({ oldPassword: oldPw, newPassword: newPw });
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : '修改失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg px-4">
      <div className="w-full max-w-sm">
        <h1 className="text-3xl font-bold text-accent text-center mb-2">CCMAX POOL</h1>
        <p className="text-center text-sm text-warn mb-6">
          首次登录必须修改密码，才能继续使用。
        </p>
        <form
          onSubmit={(e) => { void handleSubmit(e); }}
          className="bg-surface border border-line rounded-xl p-8 space-y-5 shadow-lg"
        >
          <div>
            <label className="block text-sm text-muted mb-1" htmlFor="old-pw">
              当前密码
            </label>
            <input
              id="old-pw"
              type="password"
              autoComplete="current-password"
              value={oldPw}
              onChange={(e) => setOldPw(e.target.value)}
              required
              className={inputCls}
            />
          </div>
          <div>
            <label className="block text-sm text-muted mb-1" htmlFor="new-pw">
              新密码（至少 8 位）
            </label>
            <input
              id="new-pw"
              type="password"
              autoComplete="new-password"
              value={newPw}
              onChange={(e) => setNewPw(e.target.value)}
              required
              className={inputCls}
            />
          </div>
          <div>
            <label className="block text-sm text-muted mb-1" htmlFor="confirm-pw">
              确认新密码
            </label>
            <input
              id="confirm-pw"
              type="password"
              autoComplete="new-password"
              value={confirmPw}
              onChange={(e) => setConfirmPw(e.target.value)}
              required
              className={inputCls}
            />
          </div>

          {error && <p className="text-err text-sm">{error}</p>}

          <button
            type="submit"
            disabled={submitting}
            className="w-full bg-accent text-white rounded-lg py-2 text-sm font-medium
                       hover:opacity-90 disabled:opacity-50 transition"
          >
            {submitting ? '修改中…' : '修改密码'}
          </button>

          <button
            type="button"
            onClick={onLogout}
            className="w-full text-xs text-muted hover:text-ink transition text-center"
          >
            退出登录
          </button>
        </form>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// LoginGate — shows login form if not authenticated,
//             shows ForcedPasswordChange when must_change_pw is true
// ------------------------------------------------------------------
export function LoginGate({ children }: { children: ReactNode }) {
  const { user, loading, refresh, logout } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-bg">
        <span className="text-muted text-sm animate-pulse">Loading…</span>
      </div>
    );
  }

  // Authenticated but must change password before accessing anything else.
  if (user?.mustChangePw) {
    return (
      <ForcedPasswordChange
        onDone={() => { void refresh(); }}
        onLogout={() => { void logout(); }}
      />
    );
  }

  if (user) return <>{children}</>;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await apiLogin(username, password);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg px-4">
      <div className="w-full max-w-sm">
        <h1 className="text-3xl font-bold text-accent text-center mb-8">CCMAX POOL</h1>
        <form
          onSubmit={(e) => { void handleSubmit(e); }}
          className="bg-surface border border-line rounded-xl p-8 space-y-5 shadow-lg"
        >
          <div>
            <label className="block text-sm text-muted mb-1" htmlFor="username">
              用户名
            </label>
            <input
              id="username"
              type="text"
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              className="w-full bg-bg border border-line rounded-lg px-3 py-2 text-ink text-sm
                         focus:outline-none focus:ring-2 focus:ring-accent"
            />
          </div>
          <div>
            <label className="block text-sm text-muted mb-1" htmlFor="password">
              密码
            </label>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              className="w-full bg-bg border border-line rounded-lg px-3 py-2 text-ink text-sm
                         focus:outline-none focus:ring-2 focus:ring-accent"
            />
          </div>

          {error && (
            <p className="text-err text-sm">{error}</p>
          )}

          <button
            type="submit"
            disabled={submitting}
            className="w-full bg-accent text-white rounded-lg py-2 text-sm font-medium
                       hover:opacity-90 disabled:opacity-50 transition"
          >
            {submitting ? '登录中…' : '登录'}
          </button>
        </form>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// RequireRole — redirects to "/" when the user's role is not in
// the allowed list. Tenants and viewers are bounced away from
// admin/superadmin-only pages before they can even render.
// The backend enforces authz independently; this is a UX guard.
// ------------------------------------------------------------------
interface RequireRoleProps {
  /** Roles that are allowed to see this page. */
  allow: string[];
  children: ReactNode;
  /** Where to redirect on access denied. Defaults to "/". */
  redirectTo?: string;
}

export function RequireRole({ allow, children, redirectTo = '/' }: RequireRoleProps) {
  const { role, loading } = useAuth();

  // While the auth state is still loading, render nothing (LoginGate will
  // show the loading spinner; once resolved we re-render).
  if (loading) return null;

  // If the user's role is not in the allow list, redirect.
  if (!role || !allow.includes(role)) {
    return <Navigate to={redirectTo} replace />;
  }

  return <>{children}</>;
}
