// ============================================================
// Tower SPA — 时段槽位 (Slots) management page
// GET    /api/admin/slots
// POST   /api/admin/slots       {name, startMin, endMin}
// PATCH  /api/admin/slots/{id}/enabled  {enabled}
// DELETE /api/admin/slots/{id}
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { listSlots, createSlot, deleteSlot, setSlotEnabled } from '../api';
import type { Slot } from '../types';

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------
function minsToHHMM(mins: number): string {
  const h = Math.floor(mins / 60).toString().padStart(2, '0');
  const m = (mins % 60).toString().padStart(2, '0');
  return `${h}:${m}`;
}

function hhmmToMins(hhmm: string): number {
  const [h, m] = hhmm.split(':').map(Number);
  return (h || 0) * 60 + (m || 0);
}

// ------------------------------------------------------------------
// Enable toggle
// ------------------------------------------------------------------
interface EnableToggleProps {
  slot: Slot;
  onChanged: (id: string, enabled: boolean) => void;
}

function EnableToggle({ slot, onChanged }: EnableToggleProps) {
  const [busy, setBusy] = useState(false);

  async function toggle() {
    setBusy(true);
    try {
      await setSlotEnabled(slot.id, !slot.enabled);
      onChanged(slot.id, !slot.enabled);
    } finally {
      setBusy(false);
    }
  }

  return (
    <button
      onClick={() => { void toggle(); }}
      disabled={busy}
      title={slot.enabled ? '点击禁用' : '点击启用'}
      className={[
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition',
        slot.enabled ? 'bg-ok' : 'bg-line',
        busy ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
      ].join(' ')}
    >
      <span
        className={[
          'inline-block h-3.5 w-3.5 rounded-full bg-white shadow transition-transform',
          slot.enabled ? 'translate-x-4' : 'translate-x-1',
        ].join(' ')}
      />
    </button>
  );
}

// ------------------------------------------------------------------
// Main page
// ------------------------------------------------------------------
export default function Slots() {
  const [slots, setSlots] = useState<Slot[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  // Create form state
  const [name, setName] = useState('');
  const [startTime, setStartTime] = useState('00:00');
  const [endTime, setEndTime] = useState('08:00');
  const [creating, setCreating] = useState(false);
  const [createErr, setCreateErr] = useState<string | null>(null);

  // Delete confirm
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  // ------------------------------------------------------------------
  // Fetch
  // ------------------------------------------------------------------
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listSlots();
      setSlots(data);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  // ------------------------------------------------------------------
  // Create
  // ------------------------------------------------------------------
  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setCreateErr(null);
    try {
      await createSlot({
        name,
        startMin: hhmmToMins(startTime),
        endMin: hhmmToMins(endTime),
      });
      setName('');
      setStartTime('00:00');
      setEndTime('08:00');
      await load();
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
      await deleteSlot(id);
      setSlots((prev) => prev.filter((s) => s.id !== id));
      setDeleteId(null);
    } catch {
      // ignore
    } finally {
      setDeleteBusy(false);
    }
  }

  // ------------------------------------------------------------------
  // Enable toggle (optimistic)
  // ------------------------------------------------------------------
  function handleEnabledChanged(id: string, enabled: boolean) {
    setSlots((prev) => prev.map((s) => s.id === id ? { ...s, enabled } : s));
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
          <h1 className="text-2xl font-semibold text-ink">时段槽位</h1>
          <p className="text-xs text-muted mt-1">
            时间为北京时间；账户在号库里分配槽位。
          </p>
        </div>
        <button
          onClick={() => { void load(); }}
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
      {!loading && err && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{err}</div>
      )}

      {/* Slot list */}
      {!loading && !err && (
        <>
          {slots.length === 0 ? (
            <div className="bg-surface border border-line rounded-xl p-12 text-center space-y-2">
              <p className="text-3xl text-muted">—</p>
              <p className="text-ink font-medium">暂无时段槽位</p>
              <p className="text-xs text-muted">在下方表单中新建第一个槽位。</p>
            </div>
          ) : (
            <>
              {/* Desktop table */}
              <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
                <table className="w-full text-left text-sm">
                  <thead>
                    <tr className="text-xs text-muted uppercase tracking-wide border-b border-line bg-bg/50">
                      <th className="px-4 py-3 font-medium">名称</th>
                      <th className="px-4 py-3 font-medium">时间窗</th>
                      <th className="px-4 py-3 font-medium">状态</th>
                      <th className="px-4 py-3 font-medium">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {slots.map((slot) => (
                      <tr key={slot.id} className="border-t border-line/50 hover:bg-line/30 transition">
                        <td className="px-4 py-3 font-medium text-ink">{slot.name}</td>
                        <td className="px-4 py-3 font-mono text-sm text-muted">
                          {minsToHHMM(slot.startMin)}–{minsToHHMM(slot.endMin)}
                        </td>
                        <td className="px-4 py-3">
                          <EnableToggle slot={slot} onChanged={handleEnabledChanged} />
                        </td>
                        <td className="px-4 py-3">
                          <button
                            onClick={() => setDeleteId(slot.id)}
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
                {slots.map((slot) => (
                  <div key={slot.id} className="bg-surface border border-line rounded-xl p-4 space-y-3">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium text-ink text-sm">{slot.name}</span>
                      <button
                        onClick={() => setDeleteId(slot.id)}
                        className="text-xs text-muted hover:text-err transition"
                      >
                        删除
                      </button>
                    </div>
                    <div className="flex items-center gap-4 text-xs">
                      <span className="text-muted">时间窗</span>
                      <span className="font-mono text-ink">
                        {minsToHHMM(slot.startMin)}–{minsToHHMM(slot.endMin)}
                      </span>
                    </div>
                    <div className="flex items-center gap-3 text-xs">
                      <span className="text-muted">状态</span>
                      <EnableToggle slot={slot} onChanged={handleEnabledChanged} />
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}

          {/* Create form */}
          <div className="bg-surface border border-line rounded-xl p-5 space-y-4">
            <h2 className="text-sm font-semibold text-ink">新建时段槽位</h2>
            {createErr && (
              <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-sm">{createErr}</div>
            )}
            <form onSubmit={(e) => { void handleCreate(e); }} className="space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div>
                  <label className="block text-xs text-muted mb-1">名称 *</label>
                  <input
                    required
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="早高峰"
                    autoComplete="off"
                    className={inputCls}
                  />
                </div>
                <div>
                  <label className="block text-xs text-muted mb-1">开始时间</label>
                  <input
                    type="time"
                    required
                    value={startTime}
                    onChange={(e) => setStartTime(e.target.value)}
                    className={inputCls}
                  />
                </div>
                <div>
                  <label className="block text-xs text-muted mb-1">结束时间</label>
                  <input
                    type="time"
                    required
                    value={endTime}
                    onChange={(e) => setEndTime(e.target.value)}
                    className={inputCls}
                  />
                </div>
              </div>
              <button
                type="submit"
                disabled={creating}
                className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                           hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
              >
                {creating ? '创建中…' : '创建槽位'}
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
            <p className="text-sm text-muted">此操作不可撤销，槽位将被永久删除。</p>
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
