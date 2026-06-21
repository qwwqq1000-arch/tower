// ============================================================
// Tower SPA — AuthProvider + useAuth + LoginGate
// ============================================================
import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  type ReactNode,
} from 'react';
import { me, login as apiLogin, logout as apiLogout } from './api';
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
// LoginGate — shows login form if not authenticated
// ------------------------------------------------------------------
export function LoginGate({ children }: { children: ReactNode }) {
  const { user, loading, refresh } = useAuth();
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
        <h1 className="text-3xl font-bold text-accent text-center mb-8">Tower</h1>
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
