// ============================================================
// Tower SPA — Policies page (封控策略)
// Form editing policy.Patch fields
// "Preview (dry-run)" → POST /api/admin/policies/dry-run → show diffs
// "Save global" → PUT /api/admin/policies/global
// Only sends fields the user explicitly set.
// ============================================================
import { useState, useEffect } from 'react';
import { dryRunPolicy, listPolicies, putGlobalPolicy, putAccountPolicy, deleteAccountPolicy, listAccounts } from '../api';
import type { PolicyPatch, PolicyDryRunResult, AccountRow } from '../types';
import { useAuth } from '../auth';
import { RangeInput } from '../components/RangeInput';

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
  max?: number;
  step?: number;
  placeholder?: string;
}

function NumInput({ value, onChange, disabled, min, max, step, placeholder }: NumInputProps) {
  return (
    <input
      type="number"
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      disabled={disabled}
      min={min}
      max={max}
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
// Scope type: 'global' | 'account'
// TODO 租户 scope: putTenantPolicy exists on backend but is omitted in Phase 1
type Scope = 'global' | 'account';

export default function Policies() {
  const { role } = useAuth();
  const isSuperadmin = role === 'superadmin';

  // Scope selector state
  const [scope, setScope] = useState<Scope>('global');
  const [accounts, setAccounts] = useState<AccountRow[]>([]);
  const [selectedAccountId, setSelectedAccountId] = useState<string>('');
  const [loadingAccounts, setLoadingAccounts] = useState(false);

  // Integer fields
  const maxConcurrent = useField<number>(3);
  const slotCooldownMinMs = useField<number>(2000);
  const slotCooldownMaxMs = useField<number>(5000);
  const banPersistStreak = useField<number>(3);
  const permanentBanStreak = useField<number>(5);
  const cooldownBaseMs = useField<number>(10000);
  const cooldownMaxMs = useField<number>(600000);
  const affinityTTLSec = useField<number>(300);
  // Float field
  const cooldownMult = useField<number>(2);
  const fallbackPriceThresholdUsd = useField<number>(0.005);
  const maxFailover = useField<number>(50);
  // Per-model max_tokens ceilings (official defaults). Over-limit → 400, no retry.
  const limitOpus48 = useField<number>(128000);
  const limitOpus47 = useField<number>(128000);
  const limitSonnet46 = useField<number>(64000);
  const limitHaiku45 = useField<number>(64000);
  // Warmup
  const warmupHours = useField<number>(0);
  const warmupMaxConcurrent = useField<number>(1);
  const warmupBlockOpus = useField<boolean>(true);
  // Session / exile
  const sessionErrorThreshold = useField<number>(0);
  const sessionCooldownSec = useField<number>(300);
  const responseExileEnabled = useField<boolean>(false);
  const responseExileKeywords = useField<string>('');
  const quotaLimitKeywords = useField<string>('');
  const quotaLimitCodes = useField<string>('');
  // Elastic
  const elasticEnabled = useField<boolean>(false);
  const elasticScaleUpUtil = useField<number>(0.8);
  const elasticScaleDownUtil = useField<number>(0.3);
  const elasticMaxReserve = useField<number>(1000);
  const elasticBaselineCount = useField<number>(1);
  // SpendCap (5h / 7d rolling window)
  const spendCap5hEnabled = useField<boolean>(false);
  const spendCap5hMin = useField<number>(0);
  const spendCap5hMax = useField<number>(0);
  const spendCap7dEnabled = useField<boolean>(false);
  const spendCap7dMin = useField<number>(0);
  const spendCap7dMax = useField<number>(0);
  const spendWindow5hMs = useField<number>(18000000); // 5h in ms
  const spendWindow7dMs = useField<number>(604800000); // 7d in ms
  // Boolean
  const fallbackEnabled = useField<boolean>(false);
  const fallbackProbeEnabled = useField<boolean>(false);
  // Array fields (as raw text)
  const fallbackKeywords = useField<string>('');
  const fallbackModels = useField<string>('');
  const banSignals = useField<string>('401,403');
  const banKeywords = useField<string>('authentication_error,account_disabled,account_suspended');
  const cooldownSignals = useField<string>('429');
  const cooldownSignalSec = useField<number>(60);

  const [dryRunResult, setDryRunResult] = useState<PolicyDryRunResult | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  // Load accounts for scope selector
  useEffect(() => {
    if (!isSuperadmin) return;
    setLoadingAccounts(true);
    void listAccounts()
      .then((rows) => {
        setAccounts(rows);
        if (rows.length > 0) setSelectedAccountId(rows[0].accountId);
      })
      .catch(() => { /* silently ignore */ })
      .finally(() => setLoadingAccounts(false));
  }, [isSuperadmin]);

  // Load saved global policy on mount
  useEffect(() => {
    void (async () => {
      try {
        const policies = await listPolicies();
        const global = policies.find((p) => p.scopeType === 'global');
        if (!global?.params) return;
        const p = global.params as Record<string, unknown>;
        const setNum = (f: FieldState<number> & { set: (v: number) => void; toggle: () => void }, key: string) => {
          if (key in p) { f.set(Number(p[key])); if (!f.enabled) f.toggle(); }
        };
        const setBool = (f: FieldState<boolean> & { set: (v: boolean) => void; toggle: () => void }, key: string) => {
          if (key in p) { f.set(Boolean(p[key])); if (!f.enabled) f.toggle(); }
        };
        const setArr = (f: FieldState<string> & { set: (v: string) => void; toggle: () => void }, key: string) => {
          if (key in p) {
            const arr = p[key] ?? [];
            f.set((arr as unknown[]).join(','));
            if (!f.enabled) f.toggle();
          }
        };
        setNum(maxConcurrent, 'MaxConcurrent');
        setNum(slotCooldownMinMs, 'SlotCooldownMinMs');
        setNum(slotCooldownMaxMs, 'SlotCooldownMaxMs');
        setNum(banPersistStreak, 'BanPersistStreak');
        setNum(permanentBanStreak, 'PermanentBanStreak');
        setNum(cooldownBaseMs, 'CooldownBaseMs');
        setNum(cooldownMaxMs, 'CooldownMaxMs');
        setNum(cooldownMult, 'CooldownMult');
        setNum(affinityTTLSec, 'AffinityTTLSec');
        setBool(fallbackEnabled, 'FallbackEnabled');
        setNum(fallbackPriceThresholdUsd, 'FallbackPriceThresholdUsd');
        setArr(fallbackKeywords, 'FallbackKeywords');
        setArr(fallbackModels, 'FallbackModels');
        setBool(fallbackProbeEnabled, 'FallbackProbeEnabled');
        setArr(banSignals, 'BanSignals');
        setArr(banKeywords, 'BanKeywords');
        setArr(cooldownSignals, 'CooldownSignals');
        setNum(cooldownSignalSec, 'CooldownSignalSec');
        setNum(maxFailover, 'MaxFailover');
        {
          const mmt = p.ModelMaxTokens as Record<string, number> | undefined;
          const setMMT = (f: typeof limitOpus48, model: string) => {
            if (mmt && model in mmt) { f.set(Number(mmt[model])); if (!f.enabled) f.toggle(); }
          };
          setMMT(limitOpus48, 'claude-opus-4-8');
          setMMT(limitOpus47, 'claude-opus-4-7');
          setMMT(limitSonnet46, 'claude-sonnet-4-6');
          setMMT(limitHaiku45, 'claude-haiku-4-5');
        }
        setNum(warmupHours, 'WarmupHours');
        setNum(warmupMaxConcurrent, 'WarmupMaxConcurrent');
        setBool(warmupBlockOpus, 'WarmupBlockOpus');
        setNum(sessionErrorThreshold, 'SessionErrorThreshold');
        setNum(sessionCooldownSec, 'SessionCooldownSec');
        setBool(responseExileEnabled, 'ResponseExileEnabled');
        setArr(responseExileKeywords, 'ResponseExileKeywords');
        setArr(quotaLimitKeywords, 'QuotaLimitKeywords');
        setArr(quotaLimitCodes, 'QuotaLimitStatusCodes');
        setBool(elasticEnabled, 'ElasticEnabled');
        setNum(elasticScaleUpUtil, 'ElasticScaleUpUtil');
        setNum(elasticScaleDownUtil, 'ElasticScaleDownUtil');
        setNum(elasticMaxReserve, 'ElasticMaxReserve');
        setNum(elasticBaselineCount, 'ElasticBaselineCount');
      } catch {
        // silently ignore — page still usable
      }
    })();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Build patch — only include enabled fields
  function buildPatch(): PolicyPatch {
    const patch: PolicyPatch = {};
    if (maxConcurrent.enabled) patch.MaxConcurrent = maxConcurrent.value;
    // SlotCooldownMin/Max are controlled as a pair via the RangeInput widget
    if (slotCooldownMinMs.enabled) {
      patch.SlotCooldownMinMs = slotCooldownMinMs.value;
      patch.SlotCooldownMaxMs = slotCooldownMaxMs.value;
    }
    if (banPersistStreak.enabled) patch.BanPersistStreak = banPersistStreak.value;
    if (permanentBanStreak.enabled) patch.PermanentBanStreak = permanentBanStreak.value;
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
    if (cooldownSignals.enabled) {
      patch.CooldownSignals = cooldownSignals.value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
        .map(Number)
        .filter((n) => !isNaN(n));
    }
    if (cooldownSignalSec.enabled) patch.CooldownSignalSec = cooldownSignalSec.value;
    if (maxFailover.enabled) patch.MaxFailover = maxFailover.value;
    // ModelMaxTokens is a full-map replacement: when any override is enabled, send all
    // four models so the un-overridden ones keep their official ceiling (not unlimited).
    if (limitOpus48.enabled || limitOpus47.enabled || limitSonnet46.enabled || limitHaiku45.enabled) {
      patch.ModelMaxTokens = {
        'claude-opus-4-8': limitOpus48.value,
        'claude-opus-4-7': limitOpus47.value,
        'claude-sonnet-4-6': limitSonnet46.value,
        'claude-haiku-4-5': limitHaiku45.value,
      };
    }
    if (warmupHours.enabled) patch.WarmupHours = warmupHours.value;
    if (warmupMaxConcurrent.enabled) patch.WarmupMaxConcurrent = warmupMaxConcurrent.value;
    if (warmupBlockOpus.enabled) patch.WarmupBlockOpus = warmupBlockOpus.value;
    if (sessionErrorThreshold.enabled) patch.SessionErrorThreshold = sessionErrorThreshold.value;
    if (sessionCooldownSec.enabled) patch.SessionCooldownSec = sessionCooldownSec.value;
    if (responseExileEnabled.enabled) patch.ResponseExileEnabled = responseExileEnabled.value;
    if (responseExileKeywords.enabled) patch.ResponseExileKeywords = responseExileKeywords.value.split(',').map(s => s.trim()).filter(Boolean);
    if (quotaLimitKeywords.enabled) patch.QuotaLimitKeywords = quotaLimitKeywords.value.split(',').map(s => s.trim()).filter(Boolean);
    if (quotaLimitCodes.enabled) patch.QuotaLimitStatusCodes = quotaLimitCodes.value.split(',').map(s => parseInt(s.trim(), 10)).filter(n => !isNaN(n));
    if (elasticEnabled.enabled) patch.ElasticEnabled = elasticEnabled.value;
    if (elasticScaleUpUtil.enabled) patch.ElasticScaleUpUtil = elasticScaleUpUtil.value;
    if (elasticScaleDownUtil.enabled) patch.ElasticScaleDownUtil = elasticScaleDownUtil.value;
    if (elasticMaxReserve.enabled) patch.ElasticMaxReserve = elasticMaxReserve.value;
    if (elasticBaselineCount.enabled) patch.ElasticBaselineCount = elasticBaselineCount.value;
    // SpendCap fields — 5h and 7d share the same enable checkbox approach
    if (spendCap5hEnabled.enabled) patch.SpendCap5hEnabled = spendCap5hEnabled.value;
    if (spendCap5hMin.enabled) patch.SpendCap5hUsd = { Min: spendCap5hMin.value, Max: spendCap5hMax.value };
    if (spendCap7dEnabled.enabled) patch.SpendCap7dEnabled = spendCap7dEnabled.value;
    if (spendCap7dMin.enabled) patch.SpendCap7dUsd = { Min: spendCap7dMin.value, Max: spendCap7dMax.value };
    if (spendWindow5hMs.enabled) patch.SpendWindow5hMs = spendWindow5hMs.value;
    if (spendWindow7dMs.enabled) patch.SpendWindow7dMs = spendWindow7dMs.value;
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
      if (scope === 'account') {
        if (!selectedAccountId) throw new Error('请先选择账户');
        await putAccountPolicy(selectedAccountId, buildPatch());
        const acct = accounts.find((a) => a.accountId === selectedAccountId);
        setSaveMsg(`账户策略已保存 (${acct?.email ?? selectedAccountId})`);
      } else {
        await putGlobalPolicy(buildPatch());
        setSaveMsg('全局策略已保存');
      }
      setTimeout(() => setSaveMsg(null), 3000);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '保存失败');
    } finally {
      setSaving(false);
    }
  }

  async function handleDeleteAccountPolicy() {
    if (!selectedAccountId) return;
    if (!window.confirm('确认清除此号的策略覆盖？清除后将使用全局策略。')) return;
    setErr(null);
    setSaveMsg(null);
    try {
      await deleteAccountPolicy(selectedAccountId);
      const acct = accounts.find((a) => a.accountId === selectedAccountId);
      setSaveMsg(`账户策略已清除 (${acct?.email ?? selectedAccountId})`);
      setTimeout(() => setSaveMsg(null), 3000);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '清除失败');
    }
  }

  const anyEnabled = [
    maxConcurrent, slotCooldownMinMs, banPersistStreak, permanentBanStreak,
    cooldownBaseMs, cooldownMaxMs, cooldownMult, affinityTTLSec,
    fallbackEnabled, fallbackPriceThresholdUsd, fallbackKeywords, fallbackModels, fallbackProbeEnabled, banSignals, banKeywords, cooldownSignals, cooldownSignalSec,
    maxFailover,
    warmupHours, warmupMaxConcurrent, warmupBlockOpus,
    sessionErrorThreshold, sessionCooldownSec, responseExileEnabled, responseExileKeywords, quotaLimitKeywords, quotaLimitCodes,
    elasticEnabled, elasticScaleUpUtil, elasticScaleDownUtil, elasticMaxReserve, elasticBaselineCount,
    spendCap5hEnabled, spendCap5hMin, spendCap5hMax, spendCap7dEnabled, spendCap7dMin, spendCap7dMax, spendWindow5hMs, spendWindow7dMs,
  ].some((f) => f.enabled);

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">封控策略</h1>
          <p className="text-xs text-muted mt-1">
            {isSuperadmin
              ? '勾选字段并输入值，未勾选的字段将保持默认。'
              : '只读 — 仅 superadmin 可修改策略。'}
          </p>
        </div>
        {isSuperadmin && (
          <div className="flex gap-2 shrink-0 flex-wrap justify-end">
            {scope === 'global' && (
              <button
                onClick={() => { void handlePreview(); }}
                disabled={previewing || !anyEnabled}
                className="px-4 py-2 text-sm font-medium border border-accent text-accent rounded-lg
                           hover:bg-accent/10 disabled:opacity-50 disabled:cursor-not-allowed transition"
              >
                {previewing ? '预览中…' : '预览 (dry-run)'}
              </button>
            )}
            {scope === 'account' && (
              <button
                onClick={() => { void handleDeleteAccountPolicy(); }}
                disabled={!selectedAccountId}
                className="px-4 py-2 text-sm font-medium border border-err text-err rounded-lg
                           hover:bg-err/10 disabled:opacity-50 disabled:cursor-not-allowed transition"
              >
                清除此号配置
              </button>
            )}
            <button
              onClick={() => { void handleSave(); }}
              disabled={saving || !anyEnabled}
              className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                         hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
            >
              {saving ? '保存中…' : scope === 'account' ? '保存账户策略' : '保存全局'}
            </button>
          </div>
        )}
      </div>

      {/* Scope selector */}
      {isSuperadmin && (
        <div className="flex flex-col sm:flex-row sm:items-center gap-3 bg-surface border border-line rounded-xl px-4 py-3">
          <span className="text-sm font-medium text-ink shrink-0">作用域</span>
          <div className="flex gap-2">
            <button
              onClick={() => setScope('global')}
              className={`px-3 py-1.5 text-sm rounded-lg border transition ${
                scope === 'global'
                  ? 'bg-accent text-white border-accent'
                  : 'border-line text-muted hover:border-accent hover:text-ink'
              }`}
            >
              全局
            </button>
            <button
              onClick={() => setScope('account')}
              className={`px-3 py-1.5 text-sm rounded-lg border transition ${
                scope === 'account'
                  ? 'bg-accent text-white border-accent'
                  : 'border-line text-muted hover:border-accent hover:text-ink'
              }`}
            >
              账户
            </button>
          </div>
          {scope === 'account' && (
            <div className="flex items-center gap-2 flex-1 min-w-0">
              {loadingAccounts ? (
                <span className="text-xs text-muted">加载账户中…</span>
              ) : accounts.length === 0 ? (
                <span className="text-xs text-muted">无可用账户</span>
              ) : (
                <select
                  value={selectedAccountId}
                  onChange={(e) => setSelectedAccountId(e.target.value)}
                  className="flex-1 min-w-0 bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                             focus:outline-none focus:border-accent transition"
                >
                  {accounts.map((a) => (
                    <option key={a.accountId} value={a.accountId}>
                      {a.email || a.accountId}
                    </option>
                  ))}
                </select>
              )}
            </div>
          )}
        </div>
      )}

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
      <fieldset disabled={!isSuperadmin}>
      <div className="bg-surface border border-line rounded-xl px-4 py-2">
        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">并发 / 冷却</h2>

        <FieldRow
          label="MaxConcurrent"
          desc="每账号最大并发槽位数(节点总并发 = 账号数 × 此值)"
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

        {/* SlotCooldown rendered as a single RangeInput widget.
            Backend still stores SlotCooldownMinMs / SlotCooldownMaxMs separately;
            buildPatch() maps Min→SlotCooldownMinMs, Max→SlotCooldownMaxMs.
            Both fields share the same enabled toggle (slotCooldownMinMs). */}
        <FieldRow
          label="SlotCooldownMs (min ~ max)"
          desc="槽位冷却时长区间 (ms)：每次随机取 [min, max] 内的值"
          enabled={slotCooldownMinMs.enabled}
          onToggle={slotCooldownMinMs.toggle}
        >
          <RangeInput
            min={slotCooldownMinMs.value}
            max={slotCooldownMaxMs.value}
            onChangeMin={slotCooldownMinMs.set}
            onChangeMax={slotCooldownMaxMs.set}
            disabled={!slotCooldownMinMs.enabled}
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
          label="PermanentBanStreak"
          desc="连续 N 次封禁信号 → 永久封禁(不再半开恢复，需人工恢复)；0=关闭"
          enabled={permanentBanStreak.enabled}
          onToggle={permanentBanStreak.toggle}
        >
          <NumInput
            value={permanentBanStreak.value}
            onChange={permanentBanStreak.set}
            disabled={!permanentBanStreak.enabled}
            min={0}
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

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">亲和性</h2>

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

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">保底触发（兜底通道触发条件）</h2>

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

        <FieldRow
          label="SessionErrorThreshold"
          desc="会话连错阈值(0=关)"
          enabled={sessionErrorThreshold.enabled}
          onToggle={sessionErrorThreshold.toggle}
        >
          <NumInput
            value={sessionErrorThreshold.value}
            onChange={sessionErrorThreshold.set}
            disabled={!sessionErrorThreshold.enabled}
            min={0}
          />
        </FieldRow>

        <FieldRow
          label="SessionCooldownSec"
          desc="会话放逐冷却(秒)"
          enabled={sessionCooldownSec.enabled}
          onToggle={sessionCooldownSec.toggle}
        >
          <NumInput
            value={sessionCooldownSec.value}
            onChange={sessionCooldownSec.set}
            disabled={!sessionCooldownSec.enabled}
            min={0}
          />
        </FieldRow>

        <FieldRow
          label="ResponseExileEnabled"
          desc="安全拒答放逐(cyber)"
          enabled={responseExileEnabled.enabled}
          onToggle={responseExileEnabled.toggle}
        >
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={responseExileEnabled.value}
              onChange={(e) => responseExileEnabled.set(e.target.checked)}
              disabled={!responseExileEnabled.enabled}
              className="accent-accent w-4 h-4"
            />
            <span className="text-sm text-ink">
              {responseExileEnabled.value ? '已启用' : '已禁用'}
            </span>
          </label>
        </FieldRow>

        <FieldRow
          label="拒答关键词(逗号分隔)"
          desc="拒答关键词(逗号分隔)"
          enabled={responseExileKeywords.enabled}
          onToggle={responseExileKeywords.toggle}
        >
          <input
            type="text"
            value={responseExileKeywords.value}
            onChange={(e) => responseExileKeywords.set(e.target.value)}
            disabled={!responseExileKeywords.enabled}
            placeholder="keyword1,keyword2"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
        </FieldRow>

        <FieldRow
          label="限额关键词(逗号分隔,命中即配额限流)"
          desc="错误响应命中任一关键词时,把该号标记为限额(配额),按返回的重置时间自动恢复。仅匹配真实用量耗尽措辞(如 hit your limit),不要填 rate_limit_error(瞬时限流会误触)"
          enabled={quotaLimitKeywords.enabled}
          onToggle={quotaLimitKeywords.toggle}
        >
          <input
            type="text"
            value={quotaLimitKeywords.value}
            onChange={(e) => quotaLimitKeywords.set(e.target.value)}
            disabled={!quotaLimitKeywords.enabled}
            placeholder="hit your limit,usage limit"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
        </FieldRow>

        <FieldRow
          label="限额拦截错误码(逗号分隔)"
          desc="只在这些 HTTP 状态码的响应上扫描限额关键词,避免每个请求都扫描影响速率。默认 429(CPA冷却)+500(meridian/内嵌错误);留空=所有错误响应都扫"
          enabled={quotaLimitCodes.enabled}
          onToggle={quotaLimitCodes.toggle}
        >
          <input
            type="text"
            value={quotaLimitCodes.value}
            onChange={(e) => quotaLimitCodes.set(e.target.value)}
            disabled={!quotaLimitCodes.enabled}
            placeholder="429,500"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
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

        <FieldRow
          label="CooldownSignals"
          desc="触发账户临时冷却的 HTTP 状态码，逗号分隔（例: 429）。命中后冷却该号，不封禁、自动恢复"
          enabled={cooldownSignals.enabled}
          onToggle={cooldownSignals.toggle}
        >
          <input
            type="text"
            value={cooldownSignals.value}
            onChange={(e) => cooldownSignals.set(e.target.value)}
            disabled={!cooldownSignals.enabled}
            placeholder="429"
            className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition
                       disabled:cursor-not-allowed"
          />
        </FieldRow>

        <FieldRow
          label="CooldownSignalSec"
          desc="命中 CooldownSignals 后，该账户冷却的秒数"
          enabled={cooldownSignalSec.enabled}
          onToggle={cooldownSignalSec.toggle}
        >
          <NumInput
            value={cooldownSignalSec.value}
            onChange={cooldownSignalSec.set}
            disabled={!cooldownSignalSec.enabled}
            min={1}
          />
        </FieldRow>

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">模型输出上限(max_tokens)</h2>
        <p className="text-xs text-muted/70 -mt-1 mb-1">请求的 max_tokens 超过该模型上限时直接返回 400、不重试不兜底。默认值为官方上限。</p>
        {([
          ['claude-opus-4-8', limitOpus48],
          ['claude-opus-4-7', limitOpus47],
          ['claude-sonnet-4-6', limitSonnet46],
          ['claude-haiku-4-5', limitHaiku45],
        ] as [string, typeof limitOpus48][]).map(([model, f]) => (
          <FieldRow key={model} label={model} desc="" enabled={f.enabled} onToggle={f.toggle}>
            <NumInput value={f.value} onChange={f.set} disabled={!f.enabled} min={1} step={1000} />
          </FieldRow>
        ))}

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">故障转移</h2>

        <FieldRow
          label="最大故障转移次数(失败换号尝试上限)"
          desc="单次请求最多尝试换号的次数上限（故障转移 / 重试上限）"
          enabled={maxFailover.enabled}
          onToggle={maxFailover.toggle}
        >
          <NumInput
            value={maxFailover.value}
            onChange={maxFailover.set}
            disabled={!maxFailover.enabled}
            min={1}
          />
        </FieldRow>

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">预热</h2>

        <FieldRow
          label="WarmupHours"
          desc="预热时长(小时,0=关)"
          enabled={warmupHours.enabled}
          onToggle={warmupHours.toggle}
        >
          <NumInput
            value={warmupHours.value}
            onChange={warmupHours.set}
            disabled={!warmupHours.enabled}
            min={0}
          />
        </FieldRow>

        <FieldRow
          label="WarmupMaxConcurrent"
          desc="预热期最大并发"
          enabled={warmupMaxConcurrent.enabled}
          onToggle={warmupMaxConcurrent.toggle}
        >
          <NumInput
            value={warmupMaxConcurrent.value}
            onChange={warmupMaxConcurrent.set}
            disabled={!warmupMaxConcurrent.enabled}
            min={1}
          />
        </FieldRow>

        <FieldRow
          label="WarmupBlockOpus"
          desc="预热期挡 opus"
          enabled={warmupBlockOpus.enabled}
          onToggle={warmupBlockOpus.toggle}
        >
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={warmupBlockOpus.value}
              onChange={(e) => warmupBlockOpus.set(e.target.checked)}
              disabled={!warmupBlockOpus.enabled}
              className="accent-accent w-4 h-4"
            />
            <span className="text-sm text-ink">
              {warmupBlockOpus.value ? '已启用' : '已禁用'}
            </span>
          </label>
        </FieldRow>

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">弹性伸缩</h2>

        <FieldRow
          label="ElasticEnabled"
          desc="启用弹性"
          enabled={elasticEnabled.enabled}
          onToggle={elasticEnabled.toggle}
        >
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={elasticEnabled.value}
              onChange={(e) => elasticEnabled.set(e.target.checked)}
              disabled={!elasticEnabled.enabled}
              className="accent-accent w-4 h-4"
            />
            <span className="text-sm text-ink">
              {elasticEnabled.value ? '已启用' : '已禁用'}
            </span>
          </label>
        </FieldRow>

        <FieldRow
          label="ElasticScaleUpUtil"
          desc="扩容利用率阈值"
          enabled={elasticScaleUpUtil.enabled}
          onToggle={elasticScaleUpUtil.toggle}
        >
          <NumInput
            value={elasticScaleUpUtil.value}
            onChange={elasticScaleUpUtil.set}
            disabled={!elasticScaleUpUtil.enabled}
            min={0}
            max={1}
            step={0.05}
          />
        </FieldRow>

        <FieldRow
          label="缩容利用率阈值(利用率≤此值才释放备用号)"
          desc="利用率持续低于此阈值时才释放备用号（与扩容阈值形成迟滞区间）"
          enabled={elasticScaleDownUtil.enabled}
          onToggle={elasticScaleDownUtil.toggle}
        >
          <NumInput
            value={elasticScaleDownUtil.value}
            onChange={elasticScaleDownUtil.set}
            disabled={!elasticScaleDownUtil.enabled}
            min={0}
            max={1}
            step={0.05}
          />
        </FieldRow>

        <FieldRow
          label="ElasticMaxReserve"
          desc="最大备用数"
          enabled={elasticMaxReserve.enabled}
          onToggle={elasticMaxReserve.toggle}
        >
          <NumInput
            value={elasticMaxReserve.value}
            onChange={elasticMaxReserve.set}
            disabled={!elasticMaxReserve.enabled}
            min={0}
          />
        </FieldRow>

        <FieldRow
          label="默认活跃账户数(打满后才按弹性扩容)"
          desc="弹性扩容触发前默认保持活跃的账户数量"
          enabled={elasticBaselineCount.enabled}
          onToggle={elasticBaselineCount.toggle}
        >
          <NumInput
            value={elasticBaselineCount.value}
            onChange={elasticBaselineCount.set}
            disabled={!elasticBaselineCount.enabled}
            min={1}
          />
        </FieldRow>

        <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2 pt-4">自保限额（花费上限保护）</h2>
        <p className="text-xs text-muted/70 -mt-1 mb-1">账户作用域下可按号设置不同的上限（随机种子区间）。0 = 不限。</p>

        {/* 5h window */}
        <FieldRow
          label="SpendCap5hEnabled"
          desc="启用 5h 滚动窗口花费上限检测"
          enabled={spendCap5hEnabled.enabled}
          onToggle={spendCap5hEnabled.toggle}
        >
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={spendCap5hEnabled.value}
              onChange={(e) => spendCap5hEnabled.set(e.target.checked)}
              disabled={!spendCap5hEnabled.enabled}
              className="accent-accent w-4 h-4"
            />
            <span className="text-sm text-ink">{spendCap5hEnabled.value ? '已启用' : '已禁用'}</span>
          </label>
        </FieldRow>

        <FieldRow
          label="SpendCap5hUsd (min ~ max)"
          desc="5h 窗口花费上限随机区间 (USD)：每号启动时随机取 [min, max] 内的值"
          enabled={spendCap5hMin.enabled}
          onToggle={spendCap5hMin.toggle}
        >
          <RangeInput
            min={spendCap5hMin.value}
            max={spendCap5hMax.value}
            onChangeMin={spendCap5hMin.set}
            onChangeMax={spendCap5hMax.set}
            disabled={!spendCap5hMin.enabled}
            step={0.01}
            minLabel="min $"
            maxLabel="max $"
          />
        </FieldRow>

        <FieldRow
          label="SpendWindow5hMs"
          desc="5h 窗口时长 (ms)，默认 18000000 (5h)"
          enabled={spendWindow5hMs.enabled}
          onToggle={spendWindow5hMs.toggle}
        >
          <NumInput
            value={spendWindow5hMs.value}
            onChange={spendWindow5hMs.set}
            disabled={!spendWindow5hMs.enabled}
            min={0}
            step={60000}
          />
        </FieldRow>

        {/* 7d window */}
        <FieldRow
          label="SpendCap7dEnabled"
          desc="启用 7d 滚动窗口花费上限检测"
          enabled={spendCap7dEnabled.enabled}
          onToggle={spendCap7dEnabled.toggle}
        >
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={spendCap7dEnabled.value}
              onChange={(e) => spendCap7dEnabled.set(e.target.checked)}
              disabled={!spendCap7dEnabled.enabled}
              className="accent-accent w-4 h-4"
            />
            <span className="text-sm text-ink">{spendCap7dEnabled.value ? '已启用' : '已禁用'}</span>
          </label>
        </FieldRow>

        <FieldRow
          label="SpendCap7dUsd (min ~ max)"
          desc="7d 窗口花费上限随机区间 (USD)：每号启动时随机取 [min, max] 内的值"
          enabled={spendCap7dMin.enabled}
          onToggle={spendCap7dMin.toggle}
        >
          <RangeInput
            min={spendCap7dMin.value}
            max={spendCap7dMax.value}
            onChangeMin={spendCap7dMin.set}
            onChangeMax={spendCap7dMax.set}
            disabled={!spendCap7dMin.enabled}
            step={0.1}
            minLabel="min $"
            maxLabel="max $"
          />
        </FieldRow>

        <FieldRow
          label="SpendWindow7dMs"
          desc="7d 窗口时长 (ms)，默认 604800000 (7d)"
          enabled={spendWindow7dMs.enabled}
          onToggle={spendWindow7dMs.toggle}
        >
          <NumInput
            value={spendWindow7dMs.value}
            onChange={spendWindow7dMs.set}
            disabled={!spendWindow7dMs.enabled}
            min={0}
            step={3600000}
          />
        </FieldRow>
      </div>
      </fieldset>

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
