// ============================================================
// Tower SPA — 用户管理 page (adminOnly, /users)
// GET    /api/admin/users → [{id,username,role,rate}]
// POST   /api/admin/users {username,password,role}
// DELETE /api/admin/users/{id}
// PATCH  /api/admin/users/{id}/role {role}
// PATCH  /api/admin/users/{id}/hosting-rate {rate}
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import {
  listUsers,
  createUser,
  deleteUser,
  setUserRole,
  setUserHostingRate,
  setUserChannelRate,
  setUserFallbackLimit,
} from '../api';
import type { UserRow } from '../types';

// ------------------------------------------------------------------
// Constants
// ------------------------------------------------------------------
const ROLES = ['superadmin', 'admin', 'operator', 'tenant', 'viewer'] as const;

// ------------------------------------------------------------------
// Inline role select
// ------------------------------------------------------------------
interface RoleSelectProps {
  userId: string;
  current: string;
  onChanged: (id: string, role: string) => void;
}

function RoleSelect({ userId, current, onChanged }: RoleSelectProps) {
  const [busy, setBusy] = useState(false);

  async function handleChange(role: string) {
    setBusy(true);
    try {
      await setUserRole(userId, role);
      onChanged(userId, role);
    } catch {
      // silently ignore — user sees no change
    } finally {
      setBusy(false);
    }
  }

  const selectCls =
    'bg-bg border border-line rounded px-2 py-1 text-xs text-ink ' +
    'focus:outline-none focus:border-accent transition ' +
    (busy ? 'opacity-50 cursor-not-allowed' : '');

  return (
    <select
      disabled={busy}
      value={current}
      onChange={(e) => { void handleChange(e.target.value); }}
      className={selectCls}
    >
      {ROLES.map((r) => (
        <option key={r} value={r}>{r}</option>
      ))}
    </select>
  );
}

// ------------------------------------------------------------------
// Inline rate editor
// ------------------------------------------------------------------
interface NumberEditorProps {
  userId: string;
  current: number;
  onChanged: (id: string, val: number) => void;
  save: (id: string, val: number) => Promise<unknown>;
  format: (val: number) => string;
  step?: string;
  title?: string;
}

function NumberEditor({ userId, current, onChanged, save, format, step = '0.01', title }: NumberEditorProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(String(current));
  const [busy, setBusy] = useState(false);

  // Sync if parent resets
  useEffect(() => {
    if (!editing) setDraft(String(current));
  }, [current, editing]);

  async function commit() {
    const val = parseFloat(draft);
    if (isNaN(val)) { setEditing(false); setDraft(String(current)); return; }
    setBusy(true);
    try {
      await save(userId, val);
      onChanged(userId, val);
      setEditing(false);
    } catch {
      // restore on error
      setDraft(String(current));
      setEditing(false);
    } finally {
      setBusy(false);
    }
  }

  if (editing) {
    return (
      <div className="flex items-center gap-1">
        <input
          autoFocus
          type="number"
          step={step}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') { void commit(); }
            if (e.key === 'Escape') { setEditing(false); setDraft(String(current)); }
          }}
          disabled={busy}
          className="w-20 bg-bg border border-accent rounded px-2 py-0.5 text-xs text-ink focus:outline-none"
        />
        <button
          onClick={() => { void commit(); }}
          disabled={busy}
          className="text-xs text-accent hover:underline"
        >
          {busy ? '…' : '✓'}
        </button>
      </div>
    );
  }

  return (
    <button
      onClick={() => setEditing(true)}
      className="text-xs text-ink hover:text-accent hover:underline tabular-nums"
      title={title ?? '点击编辑'}
    >
      {format(current)}
    </button>
  );
}

// ------------------------------------------------------------------
// Main page
// ------------------------------------------------------------------
export default function Users() {
  const [users, setUsers] = useState<UserRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Create form state
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<string>('tenant');
  const [creating, setCreating] = useState(false);
  const [createErr, setCreateErr] = useState<string | null>(null);

  // Delete confirm
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  // ------------------------------------------------------------------
  // Fetch
  // ------------------------------------------------------------------
  const fetchUsers = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listUsers();
      setUsers(data);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void fetchUsers(); }, [fetchUsers]);

  // ------------------------------------------------------------------
  // Create
  // ------------------------------------------------------------------
  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setCreateErr(null);
    try {
      const res = await createUser({ username, password, role });
      // optimistic: add a placeholder row; real data comes from re-fetch
      setUsers((prev) => [...prev, { id: res.id, username, role, rate: 0 }]);
      setUsername('');
      setPassword('');
      setRole('tenant');
    } catch (e) {
      setCreateErr(e instanceof Error ? e.message : '创建失败');
    } finally {
      setCreating(false);
    }
  }

  // ------------------------------------------------------------------
  // Delete
  // ------------------------------------------------------------------
  async function handleDelete(id: string) {
    setDeleteBusy(true);
    try {
      await deleteUser(id);
      setUsers((prev) => prev.filter((u) => u.id !== id));
      setDeleteId(null);
    } catch {
      // ignore
    } finally {
      setDeleteBusy(false);
    }
  }

  // ------------------------------------------------------------------
  // Role / rate change (optimistic)
  // ------------------------------------------------------------------
  function handleRoleChanged(id: string, newRole: string) {
    setUsers((prev) => prev.map((u) => u.id === id ? { ...u, role: newRole } : u));
  }

  function handleRateChanged(id: string, newRate: number) {
    setUsers((prev) => prev.map((u) => u.id === id ? { ...u, rate: newRate } : u));
  }

  function handleChannelRateChanged(id: string, val: number) {
    setUsers((prev) => prev.map((u) => u.id === id ? { ...u, channelRate: val } : u));
  }

  function handleFallbackLimitChanged(id: string, val: number) {
    setUsers((prev) => prev.map((u) => u.id === id ? { ...u, fallbackLimit: val } : u));
  }

  // ------------------------------------------------------------------
  // Shared input class
  // ------------------------------------------------------------------
  const inputCls =
    'w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink ' +
    'placeholder:text-muted focus:outline-none focus:border-accent transition';

  // ------------------------------------------------------------------
  // Render
  // ------------------------------------------------------------------
  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">用户管理</h1>
          <p className="text-xs text-muted mt-1">
            新建/删除用户、调整角色与托管费率。
          </p>
        </div>
        <button
          onClick={() => { void fetchUsers(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center min-h-32">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}

      {/* Error */}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}

      {/* User list */}
      {!loading && !error && (
        <>
          {users.length === 0 ? (
            <div className="bg-surface border border-line rounded-xl p-12 text-center space-y-2">
              <p className="text-3xl">👤</p>
              <p className="text-ink font-medium">暂无用户</p>
              <p className="text-xs text-muted">在下方表单中新建第一个用户。</p>
            </div>
          ) : (
            <>
              {/* Desktop table */}
              <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
                <table className="w-full text-left text-sm">
                  <thead>
                    <tr className="text-xs text-muted uppercase tracking-wide border-b border-line bg-bg/50">
                      <th className="px-4 py-3 font-medium">用户名</th>
                      <th className="px-4 py-3 font-medium">角色</th>
                      <th className="px-4 py-3 font-medium text-right">托管费率</th>
                      <th className="px-4 py-3 font-medium text-right">渠道倍率</th>
                      <th className="px-4 py-3 font-medium text-right">保底上限</th>
                      <th className="px-4 py-3 font-medium">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {users.map((u) => (
                      <tr key={u.id} className="border-t border-line/50 hover:bg-line/30 transition">
                        <td className="px-4 py-3 font-medium text-ink">{u.username}</td>
                        <td className="px-4 py-3">
                          <RoleSelect
                            userId={u.id}
                            current={u.role}
                            onChanged={handleRoleChanged}
                          />
                        </td>
                        <td className="px-4 py-3 text-right">
                          <NumberEditor
                            userId={u.id}
                            current={u.rate}
                            onChanged={handleRateChanged}
                            save={setUserHostingRate}
                            format={(v) => v.toFixed(4)}
                            title="点击编辑托管费率"
                          />
                        </td>
                        <td className="px-4 py-3 text-right">
                          <NumberEditor
                            userId={u.id}
                            current={u.channelRate ?? 0}
                            onChanged={handleChannelRateChanged}
                            save={setUserChannelRate}
                            format={(v) => v.toFixed(4)}
                            title="点击编辑渠道中转倍率"
                          />
                        </td>
                        <td className="px-4 py-3 text-right">
                          <NumberEditor
                            userId={u.id}
                            current={u.fallbackLimit ?? 0}
                            onChanged={handleFallbackLimitChanged}
                            save={setUserFallbackLimit}
                            format={(v) => String(v)}
                            step="1"
                            title="点击编辑保底渠道数量上限"
                          />
                        </td>
                        <td className="px-4 py-3">
                          <button
                            onClick={() => setDeleteId(u.id)}
                            className="text-xs text-muted hover:text-err transition"
                          >
                            删除
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {/* Mobile cards */}
              <div className="md:hidden space-y-3">
                {users.map((u) => (
                  <div key={u.id} className="bg-surface border border-line rounded-xl p-4 space-y-3">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium text-ink text-sm">{u.username}</span>
                      <button
                        onClick={() => setDeleteId(u.id)}
                        className="text-xs text-muted hover:text-err transition"
                      >
                        删除
                      </button>
                    </div>
                    <div className="flex items-center gap-4 text-xs">
                      <div className="flex items-center gap-2">
                        <span className="text-muted">角色</span>
                        <RoleSelect
                          userId={u.id}
                          current={u.role}
                          onChanged={handleRoleChanged}
                        />
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-muted">费率</span>
                        <NumberEditor
                          userId={u.id}
                          current={u.rate}
                          onChanged={handleRateChanged}
                          save={setUserHostingRate}
                          format={(v) => v.toFixed(4)}
                          title="点击编辑托管费率"
                        />
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-muted">渠道倍率</span>
                        <NumberEditor
                          userId={u.id}
                          current={u.channelRate ?? 0}
                          onChanged={handleChannelRateChanged}
                          save={setUserChannelRate}
                          format={(v) => v.toFixed(4)}
                          title="点击编辑渠道中转倍率"
                        />
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-muted">保底上限</span>
                        <NumberEditor
                          userId={u.id}
                          current={u.fallbackLimit ?? 0}
                          onChanged={handleFallbackLimitChanged}
                          save={setUserFallbackLimit}
                          format={(v) => String(v)}
                          step="1"
                          title="点击编辑保底渠道数量上限"
                        />
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}

          {/* Create form */}
          <div className="bg-surface border border-line rounded-xl p-5 space-y-4">
            <h2 className="text-sm font-semibold text-ink">新建用户</h2>
            {createErr && (
              <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-sm">{createErr}</div>
            )}
            <form onSubmit={(e) => { void handleCreate(e); }} className="space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div>
                  <label className="block text-xs text-muted mb-1">用户名 *</label>
                  <input
                    required
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    placeholder="alice"
                    autoComplete="off"
                    className={inputCls}
                  />
                </div>
                <div>
                  <label className="block text-xs text-muted mb-1">密码 (≥8位) *</label>
                  <input
                    required
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="••••••••"
                    autoComplete="new-password"
                    minLength={8}
                    className={inputCls}
                  />
                </div>
                <div>
                  <label className="block text-xs text-muted mb-1">角色</label>
                  <select
                    value={role}
                    onChange={(e) => setRole(e.target.value)}
                    className={inputCls}
                  >
                    {ROLES.map((r) => (
                      <option key={r} value={r}>{r}</option>
                    ))}
                  </select>
                </div>
              </div>
              <button
                type="submit"
                disabled={creating}
                className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                           hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
              >
                {creating ? '创建中…' : '创建用户'}
              </button>
            </form>
          </div>
        </>
      )}

      {/* Delete confirm modal */}
      {deleteId && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center px-4 bg-black/50"
          onClick={() => { if (!deleteBusy) setDeleteId(null); }}
        >
          <div
            className="w-full max-w-sm bg-surface border border-line rounded-xl shadow-2xl p-6 space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-base font-semibold text-ink">确认删除</h2>
            <p className="text-sm text-muted">此操作不可撤销，用户将被永久删除。</p>
            <div className="flex gap-3">
              <button
                onClick={() => { void handleDelete(deleteId); }}
                disabled={deleteBusy}
                className="px-4 py-2 text-sm font-medium bg-err text-white rounded-lg
                           hover:bg-err/80 disabled:opacity-50 transition"
              >
                {deleteBusy ? '删除中…' : '确认删除'}
              </button>
              <button
                onClick={() => setDeleteId(null)}
                disabled={deleteBusy}
                className="px-4 py-2 text-sm font-medium border border-line text-muted rounded-lg
                           hover:text-ink transition"
              >
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
