// ============================================================
// Tower SPA — Desired page (配置对账)
// Textarea editing desired-features JSON (GET/PUT /api/admin/desired)
// Validates JSON before save.
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { getDesired, putDesired } from '../api';

export default function Desired() {
  const [raw, setRaw] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [jsonErr, setJsonErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState<string | null>(null);
  const [saveErr, setSaveErr] = useState<string | null>(null);

  const fetchDesired = useCallback(async () => {
    setLoading(true);
    setLoadErr(null);
    try {
      const data = await getDesired();
      setRaw(JSON.stringify(data, null, 2));
    } catch (err) {
      setLoadErr(err instanceof Error ? err.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchDesired();
  }, [fetchDesired]);

  // Validate JSON on every keystroke
  function handleChange(val: string) {
    setRaw(val);
    try {
      JSON.parse(val);
      setJsonErr(null);
    } catch (e) {
      setJsonErr(e instanceof Error ? e.message : '无效 JSON');
    }
  }

  async function handleSave() {
    // Final validation
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch (e) {
      setJsonErr(e instanceof Error ? e.message : '无效 JSON，无法保存');
      return;
    }
    setSaving(true);
    setSaveErr(null);
    setSaveMsg(null);
    try {
      await putDesired(parsed as Record<string, unknown>);
      setSaveMsg('配置已保存');
      setTimeout(() => setSaveMsg(null), 3000);
    } catch (err) {
      setSaveErr(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  }

  function handleFormat() {
    try {
      setRaw(JSON.stringify(JSON.parse(raw), null, 2));
      setJsonErr(null);
    } catch {
      // already invalid — leave as is
    }
  }

  if (loading) {
    return (
      <div className="p-6 flex items-center justify-center min-h-64">
        <span className="text-muted animate-pulse">加载中…</span>
      </div>
    );
  }

  if (loadErr) {
    return (
      <div className="p-6">
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">
          {loadErr}
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 md:p-6 space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">配置对账</h1>
          <p className="text-xs text-muted mt-1">
            编辑 desired-features JSON。保存前会自动校验格式。
          </p>
        </div>
        <div className="flex gap-2 shrink-0">
          <button
            onClick={handleFormat}
            disabled={!!jsonErr}
            className="px-3 py-2 text-sm border border-line text-muted rounded-lg
                       hover:text-ink hover:border-ink/50 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            格式化
          </button>
          <button
            onClick={() => { void fetchDesired(); }}
            className="px-3 py-2 text-sm border border-line text-muted rounded-lg
                       hover:text-ink hover:border-ink/50 transition"
          >
            重置
          </button>
          <button
            onClick={() => { void handleSave(); }}
            disabled={saving || !!jsonErr}
            className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                       hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {saving ? '保存中…' : '保存'}
          </button>
        </div>
      </div>

      {/* Status messages */}
      {saveMsg && (
        <div className="bg-ok/10 border border-ok/30 rounded-xl p-3 text-ok text-sm">
          {saveMsg}
        </div>
      )}
      {saveErr && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-3 text-err text-sm">
          {saveErr}
        </div>
      )}

      {/* Editor */}
      <div className="space-y-1.5">
        <textarea
          value={raw}
          onChange={(e) => handleChange(e.target.value)}
          spellCheck={false}
          rows={24}
          className={`w-full bg-surface border rounded-xl px-4 py-3 text-sm text-ink font-mono
                      focus:outline-none transition resize-y leading-relaxed
                      ${jsonErr ? 'border-err focus:border-err' : 'border-line focus:border-accent'}`}
        />
        {jsonErr && (
          <p className="text-xs text-err flex items-center gap-1.5">
            <span>✕</span>
            <span>JSON 语法错误: {jsonErr}</span>
          </p>
        )}
        {!jsonErr && raw.trim() && (
          <p className="text-xs text-ok flex items-center gap-1.5">
            <span>✓</span>
            <span>JSON 格式正确</span>
          </p>
        )}
      </div>
    </div>
  );
}
