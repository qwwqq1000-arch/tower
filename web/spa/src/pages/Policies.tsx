// ============================================================
// Tower SPA — Policies page (封控策略)
// Form editing policy.Patch fields
// "Preview (dry-run)" → POST /api/admin/policies/dry-run → show diffs
// "Save global" → PUT /api/admin/policies/global
// Only sends fields the user explicitly set.
// ============================================================
import { useState } from 'react';
import { dryRunPolicy, putGlobalPolicy } from '../api';
import type { PolicyPatch, PolicyDryRunResult } from '../types';

// ------------------------------------------------------------------
// Field helpers
// ------------------------------------------------------------------
interface FieldState<T> {
  enabled: boolean;
  value: T;
}

function useField<T>(defaultValue: T) {
  const [state, setState] = useState<FieldState<T>>({
    enabled: false,
    value: defaultValue,
  });

  const toggle = () =>
    setState((s) => ({ ...s, enabled: !s.enabled }));

  const set = (value: T) =>
    setState((s) => ({ ...s, value }));

  return { ...state, toggle, set };
}

// ------------------------------------------------------------------
// Labelled field row
// ------------------------------------------------------------------
interface FieldRowProps {
  label: string;
  desc: string;
  enabled: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}

function FieldRow({ label, desc, enabled, onToggle, children }: FieldRowProps) {
  return (
    <div className={`flex flex-col sm:flex-row sm:items-center gap-2 py-3 border-b border-line/50 ${!enabled ? 'opacity-50' : ''}`}>
      <div className="flex items-center gap-2 sm:w-56 shrink-0">
        <input
          type="checkbox"
          checked={enabled}
          onChange={onToggle}
          className="accent-accent w-4 h-4 cursor-pointer"
          id={`field-${label}`}
        />
        <label htmlFor={`field-${label}`} className="text-sm font-medium text-ink cursor-pointer">
          {label}
        </label>
      </div>
      <div className="flex-1 min-w-0">
        {children}
        <p className="text-xs text-muted mt-0.5">{desc}</p>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Number input
// ------------------------------------------------------------------
interface NumInputProps {
  value: number;
  onChange: (v: number) => void;
  disabled?: boolean;
  min?: number;
  step?: number;
  placeholder?: string;
}

function NumInput({ value, onChange, disabled, min, step, placeholder }: NumInputProps) {
  return (
    <input
      type="number"
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      disabled={disabled}
      min={min}
      step={step ?? 1}
      placeholder={placeholder}
      className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                 placeholder:text-muted focus:outline-none focus:border-accent transition
                 disabled:cursor-not-allowed"
    />
  );
}

// ------------------------------------------------------------------
// Diff display
// ------------------------------------------------------------------
function DiffTable({ result }: { result: PolicyDryRunResult }) {
  return (
    <div className="space-y-3">
      {result.diffs && result.diffs.length > 0 ? (
        <div className="bg-surface border border-line rounded-xl overflow-hidden">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="text-xs text-muted uppercase tracking-wide border-b border-line">
                <th className="px-4 py-2 font-medium">字段</th>
                <th className="px-4 py-2 font-medium">当前值</th>
                <th className="px-4 py-2 font-medium">新值</th>
              </tr>
            </thead>
            <tbody>
              {result.diffs.map((d) => (
                <tr key={d.Field} className="border-t border-line/50">
                  <td className="px-4 py-2 font-mono text-xs text-ink">{d.Field}</td>
                  <td className="px-4 py-2 text-xs text-err line-through">{d.From}</td>
                  <td className="px-4 py-2 text-xs text-ok">{d.To}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="bg-surface border border-line rounded-xl p-4 text-sm text-muted text-center">
          无差异 — 所选字段值与当前默认值相同
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// Policies page
// ------------------------------------------------------------------
export default function Policies() {
  // Integer fields
  const maxConcurrent = useField<number>(3);
  const slotCooldownMinMs = useField<number>(2000);
  const slotCooldownMaxMs = useField<number>(5000);
  const banPersistStreak = useField<number>(3);
  const cooldownBaseMs = useField<number>(10000);
  const cooldownMaxMs = useField<number>(600000);
  const affinityTTLSec = useField<number>(300);
  // Float field
  const cooldownMult = useField<number>(2);
  const fallbackPriceThresholdUsd = useField<number>(0.005);
  // Boolean
  const fallbackEnabled = useField<boolean>(false);
  const fallbackProbeEnabled = useField<boolean>(false);
  // Array fields (as raw text)
  const fallbackKeywords = useField<string>('');
  const fallbackModels = useField<string>('');
  const banSignals = useField<string>('401,403');
  const banKeywords = useField<string>('authentication_error,account_disabled,account_suspended');

  const [dryRunResult, setDryRunResult] = useState<PolicyDryRunResult | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  // Build patch — only include enabled fields
  function buildPatch(): PolicyPatch {
    const patch: PolicyPatch = {};
    if (maxConcurrent.enabled) patch.MaxConcurrent = maxConcurrent.value;
    if (slotCooldownMinMs.enabled) patch.SlotCooldownMinMs = slotCooldownMinMs.value;
    if (slotCooldownMaxMs.enabled) patch.SlotCooldownMaxMs = slotCooldownMaxMs.value;
    if (banPersistStreak.enabled) patch.BanPersistStreak = banPersistStreak.value;
    if (cooldownBaseMs.enabled) patch.CooldownBaseMs = cooldownBaseMs.value;
    if (cooldownMaxMs.enabled) patch.CooldownMaxMs = cooldownMaxMs.value;
    if (cooldownMult.enabled) patch.CooldownMult = cooldownMult.value;
    if (affinityTTLSec.enabled) patch.AffinityTTLSec = affinityTTLSec.value;
    if (fallbackEnabled.enabled) patch.FallbackEnabled = fallbackEnabled.value;
    if (fallbackPriceThresholdUsd.enabled) patch.FallbackPriceThresholdUsd = fallbackPriceThresholdUsd.value;
    if (fallbackKeywords.enabled) patch.FallbackKeywords = fallbackKeywords.value.split(',').map(s => s.trim()).filter(Boolean);
    if (fallbackModels.enabled) patch.FallbackModels = fallbackModels.value.split(',').map(s => s.trim()).filter(Boolean);
    if (fallbackProbeEnabled.enabled) patch.FallbackProbeEnabled = fallbackProbeEnabled.value;
    if (banSignals.enabled) {
      patch.BanSignals = banSignals.value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
        .map(Number)
        .filter((n) => !isNaN(n));
    }
    if (banKeywords.enabled) {
      patch.BanKeywords = banKeywords.value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    }
    return patch;
  }

  async function handlePreview() {
    setPreviewing(true);
    setErr(null);
    setDryRunResult(null);
    try {
      const result = await dryRunPolicy(buildPatch());
      setDryRunResult(result);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '预览失败');
    } finally {
      setPreviewing(false);
    }
  }

  async function handleSave() {
    setSaving(true);
    setErr(null);
    setSaveMsg(null);
    try {
      await putGlobalPolicy(buildPatch());
      setSaveMsg('全局策略已保存');
      setTimeout(() => setSaveMsg(null), 3000);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '保存失败');
    } finally {
      setSaving(false);
    }
  }

  const anyEnabled = [
    maxConcurrent, slotCooldownMinMs, slotCooldownMaxMs, banPersistStreak,
    cooldownBaseMs, cooldownMaxMs, cooldownMult, affinityTTLSec,
    fallbackEnabled, fallbackPriceThresholdUsd, fallbackKeywords, fallbackModels, fallbackProbeEnabled, banSignals, banKeywords,
  ].some((f) => f.enabled);

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">封控策略</h1>
          <p className="text-xs text-muted mt-1">
            勾选字段并输入值，未勾选的字段将保持默认。
          </p>
        </div>
        <div className="flex gap-2 shrink-0">
          <button
            onClick={() => { void handlePreview(); }}
            disabled={previewing || !anyEnabled}
            className="px-4 py-2 text-sm font-medium border border-accent text-accent rounded-lg
                       hover:bg-accent/10 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {previewing ? '预览中…' : '预览 (dry-run)'}
          </button>
          <button
            onClick={() => { void handleSave(); }}
            disabled={saving || !anyEnabled}
            className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                       hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {saving ? '保存中…' : '保存全局'}
          </button>
        </div>
      </div>

      {err && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-3 text-err text-sm">
          {err}
        </div>
      )}
      {saveMsg && (
        <div className="bg-ok/10 border border-ok/30 rounded-xl p-3 text-ok text-sm">
          {saveMsg}
        </div>
      )}

      {/* Fields form */}
      <div className="bg-surface border border-line rounded-xl px-4 py-2">
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">并发 / 冷却</h2>

        <FieldRow
          label="MaxConcurrent"
          desc="每节点最大并发槽位数"
          enabled={maxConcurrent.enabled}
          onToggle={maxConcurrent.toggle}
        >
          <NumInput
            value={maxConcurrent.value}
            onChange={maxConcurrent.set}
            disabled={!maxConcurrent.enabled}
            min={1}
          />
        </FieldRow>

        <FieldRow
          label="SlotCooldownMinMs"
          desc="槽位冷却最小时长 (ms)"
          enabled={slotCooldownMinMs.enabled}
          onToggle={slotCooldownMinMs.toggle}
        >
          <NumInput
            value={slotCooldownMinMs.value}
            onChange={slotCooldownMinMs.set}
            disabled={!slotCooldownMinMs.enabled}
            min={0}
            step={100}
          />
        </FieldRow>

        <FieldRow
          label="SlotCooldownMaxMs"
          desc="槽位冷却最大时长 (ms)"
          enabled={slotCooldownMaxMs.enabled}
          onToggle={slotCooldownMaxMs.toggle}
        >
          <NumInput
            value={slotCooldownMaxMs.value}
            onChange={slotCooldownMaxMs.set}
            disabled={!slotCooldownMaxMs.enabled}
            min={0}
            step={100}
          />
        </FieldRow>

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">封禁 / 恢复</h2>

        <FieldRow
          label="BanPersistStreak"
          desc="连续 N 次封禁信号才标记 auth_valid=false"
          enabled={banPersistStreak.enabled}
          onToggle={banPersistStreak.toggle}
        >
          <NumInput
            value={banPersistStreak.value}
            onChange={banPersistStreak.set}
            disabled={!banPersistStreak.enabled}
            min={1}
          />
        </FieldRow>

        <FieldRow
          label="CooldownBaseMs"
          desc="指数退避冷却基础值 (ms)"
          enabled={cooldownBaseMs.enabled}
          onToggle={cooldownBaseMs.toggle}
        >
          <NumInput
            value={cooldownBaseMs.value}
            onChange={cooldownBaseMs.set}
            disabled={!cooldownBaseMs.enabled}
            min={0}
            step={1000}
          />
        </FieldRow>

        <FieldRow
          label="CooldownMaxMs"
          desc="冷却退避上限 (ms)"
          enabled={cooldownMaxMs.enabled}
          onToggle={cooldownMaxMs.toggle}
        >
          <NumInput
            value={cooldownMaxMs.value}
            onChange={cooldownMaxMs.set}
            disabled={!cooldownMaxMs.enabled}
            min={0}
            step={10000}
          />
        </FieldRow>

        <FieldRow
          label="CooldownMult"
          desc="指数退避乘数（例: 2 = 每次翻倍）"
          enabled={cooldownMult.enabled}
          onToggle={cooldownMult.toggle}
        >
          <NumInput
            value={cooldownMult.value}
            onChange={cooldownMult.set}
            disabled={!cooldownMult.enabled}
            min={1}
            step={0.5}
          />
        </FieldRow>

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">亲和性 / 兜底</h2>

        <FieldRow
          label="AffinityTTLSec"
          desc="亲和性缓存 TTL (秒)"
          enabled={affinityTTLSec.enabled}
          onToggle={affinityTTLSec.toggle}
        >
          <NumInput
            value={affinityTTLSec.value}
            onChange={affinityTTLSec.set}
            disabled={!affinityTTLSec.enabled}
            min={0}
            step={60}
          />
        </FieldRow>

        <FieldRow
          label="FallbackEnabled"
          desc="是否启用兜底通道（直连 Anthropic API）"
          enabled={fallbackEnabled.enabled}
          onToggle={fallbackEnabled.toggle}
        >
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={fallbackEnabled.value}
              onChange={(e) => fallbackEnabled.set(e.target.checked)}
              disabled={!fallbackEnabled.enabled}
              className="accent-accent w-4 h-4"
            />
            <span className="text-sm text-ink">
              {fallbackEnabled.value ? '已启用' : '已禁用'}
            </span>
          </label>
        </FieldRow>

        <FieldRow
          label="FallbackPriceThresholdUsd"
          desc="兜底通道价格阈值（美元/请求）"
          enabled={fallbackPriceThresholdUsd.enabled}
          onToggle={fallbackPriceThresholdUsd.toggle}
        >
          <NumInput
            value={fallbackPriceThresholdUsd.value}
            onChange={fallbackPriceThresholdUsd.set}
            disabled={!fallbackPriceThresholdUsd.enabled}
            min={0}
            step={0.001}
          />
        </FieldRow>

        <FieldRow
          label="保底关键词(逗号分隔,命中即走保底)"
          desc="响应内容命中任一关键词时强制走兜底通道"
          enabled={fallbackKeywords.enabled}
          onToggle={fallbackKeywords.toggle}
        >
          <input
            type="text"
            value={fallbackKeywords.value}
            onChange={(e) => fallbackKeywords.set(e.target.value)}
            disabled={!fallbackKeywords.enabled}
            placeholder="keyword1,keyword2"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
        </FieldRow>

        <FieldRow
          label="指定模型走保底(逗号分隔,子串匹配)"
          desc="请求模型名含子串时强制走兜底通道"
          enabled={fallbackModels.enabled}
          onToggle={fallbackModels.toggle}
        >
          <input
            type="text"
            value={fallbackModels.value}
            onChange={(e) => fallbackModels.set(e.target.value)}
            disabled={!fallbackModels.enabled}
            placeholder="claude-3-opus,claude-3-5"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
        </FieldRow>

        <FieldRow
          label="探活/hi 走保底"
          desc="探活心跳请求（hi 消息）强制走兜底通道"
          enabled={fallbackProbeEnabled.enabled}
          onToggle={fallbackProbeEnabled.toggle}
        >
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={fallbackProbeEnabled.value}
              onChange={(e) => fallbackProbeEnabled.set(e.target.checked)}
              disabled={!fallbackProbeEnabled.enabled}
              className="accent-accent w-4 h-4"
            />
            <span className="text-sm text-ink">
              {fallbackProbeEnabled.value ? '已启用' : '已禁用'}
            </span>
          </label>
        </FieldRow>

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">封禁信号</h2>

        <FieldRow
          label="BanSignals"
          desc="触发封禁的 HTTP 状态码，逗号分隔（例: 401,403）"
          enabled={banSignals.enabled}
          onToggle={banSignals.toggle}
        >
          <input
            type="text"
            value={banSignals.value}
            onChange={(e) => banSignals.set(e.target.value)}
            disabled={!banSignals.enabled}
            placeholder="401,403"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
        </FieldRow>

        <FieldRow
          label="BanKeywords"
          desc="触发封禁的错误关键词，逗号分隔"
          enabled={banKeywords.enabled}
          onToggle={banKeywords.toggle}
        >
          <input
            type="text"
            value={banKeywords.value}
            onChange={(e) => banKeywords.set(e.target.value)}
            disabled={!banKeywords.enabled}
            placeholder="authentication_error,account_disabled"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
        </FieldRow>
      </div>

      {/* Dry-run result */}
      {dryRunResult && (
        <div className="space-y-3">
          <h2 className="text-sm font-semibold text-ink">预览差异</h2>
          <DiffTable result={dryRunResult} />
        </div>
      )}
    </div>
  );
}
