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
  showOnlyConfigured?: boolean;
}

function FieldRow({ label, desc, enabled, onToggle, children, showOnlyConfigured }: FieldRowProps) {
  if (showOnlyConfigured && !enabled) return null;
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
        {!enabled && (
          <p className="text-xs text-muted/60 italic mb-1">继承全局</p>
        )}
        {children}
        <p className="text-xs text-muted mt-0.5">{desc}</p>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Group master switch (总开关)
// ------------------------------------------------------------------
function GroupMaster({ field, label = '总开关' }: { field: { value: boolean; enabled: boolean; set: (v: boolean) => void; toggle: () => void }; label?: string }) {
  return (
    <label className="flex items-center gap-2 cursor-pointer shrink-0">
      <span className="text-xs text-muted">{label}</span>
      <input type="checkbox" checked={field.value}
        onChange={(e) => { field.set(e.target.checked); if (!field.enabled) field.toggle(); }}
        className="accent-accent w-4 h-4 cursor-pointer" />
    </label>
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

// Category IDs
type CatId = 'cadence' | 'concurrency' | 'limits' | 'fallback' | 'signals';

interface Category {
  id: CatId;
  label: string;
}

const CATEGORIES: Category[] = [
  { id: 'cadence', label: '拟人节奏' },
  { id: 'concurrency', label: '并发与预热' },
  { id: 'limits', label: '限额自保' },
  { id: 'fallback', label: '保底与故障转移' },
  { id: 'signals', label: '封号识别与恢复' },
];

export default function Policies() {
  const { role } = useAuth();
  const isSuperadmin = role === 'superadmin';

  // Scope selector state
  const [scope, setScope] = useState<Scope>('global');
  const [accounts, setAccounts] = useState<AccountRow[]>([]);
  const [selectedAccountId, setSelectedAccountId] = useState<string>('');
  const [loadingAccounts, setLoadingAccounts] = useState(false);
  // Loaded policy rows (global + per-account)
  const [policyRows, setPolicyRows] = useState<Array<{ scopeType: string; scopeId?: string; params: Record<string, unknown> }>>([]);

  // Category nav + filter state
  const [cat, setCat] = useState<CatId>('cadence');
  const [onlyConfigured, setOnlyConfigured] = useState(false);

  // Integer fields
  const idleFirstSelection = useField<boolean>(true);
  const maxConcurrent = useField<number>(3);
  const slotCooldownMinMs = useField<number>(2000);
  const slotCooldownMaxMs = useField<number>(5000);
  const banPersistStreak = useField<number>(3);
  const permanentBanStreak = useField<number>(5);
  const cooldownBaseMs = useField<number>(10000);
  const cooldownMaxMs = useField<number>(600000);
  const affinityTTLSec = useField<number>(300);
  const affinityWaitMs = useField<number>(2000);
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
  // SpendCap (cumulative today-spend vs raising threshold)
  const spendCap5hEnabled = useField<boolean>(false);
  const spendCap5hMin = useField<number>(0);
  const spendCap5hMax = useField<number>(0);
  // Phase 3: HumanDelay (人类延迟)
  const humanDelayEnabled = useField<boolean>(false);
  const humanDelayDist = useField<string>('uniform');
  const humanDelayP50Min = useField<number>(500);
  const humanDelayP50Max = useField<number>(2000);
  const humanDelayP95Min = useField<number>(3000);
  const humanDelayP95Max = useField<number>(8000);
  // Phase 3: RateGovernor (利率治理)
  const rateGovEnabled = useField<boolean>(false);
  const rateRPMMin = useField<number>(0);
  const rateRPMMax = useField<number>(60);
  const rateRPHMin = useField<number>(0);
  const rateRPHMax = useField<number>(600);
  const rateRPDMin = useField<number>(0);
  const rateRPDMax = useField<number>(5000);
  const rateExceedAction = useField<string>('rotate');
  // Phase 3: SessionSim (会话模拟)
  const sessionSimEnabled = useField<boolean>(false);
  const sessionBurstCountMin = useField<number>(1);
  const sessionBurstCountMax = useField<number>(5);
  const sessionPauseMsMin = useField<number>(30000);
  const sessionPauseMsMax = useField<number>(120000);
  // Phase 3: QuietHours (安静时段)
  const quietHoursEnabled = useField<boolean>(false);
  const quietHoursStartMin = useField<number>(0);   // minutes since midnight
  const quietHoursEndMin = useField<number>(360);   // 06:00
  const quietHoursRPMMin = useField<number>(0);
  const quietHoursRPMMax = useField<number>(10);
  const quietHoursConcurrency = useField<number>(1);
  // Phase 4: ModelPin (模型锁定)
  const modelPinEnabled = useField<boolean>(false);
  const modelPinMode = useField<string>('sticky');
  const modelPinTarget = useField<string>('');
  const modelElasticEnabled = useField<boolean>(false);
  // Phase 4: SerialQueue (串行队列)
  const serialQueueEnabled = useField<boolean>(false);
  const serialQueueWaitMs = useField<number>(30000);
  // Phase 4: BodyPad (请求体填充)
  const bodyPadEnabled = useField<boolean>(false);
  const bodyPadBytesMin = useField<number>(0);
  const bodyPadBytesMax = useField<number>(512);
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
  // Retry policy
  const directFallbackStatusCodes = useField<string>('400');
  const directFallbackKeywords = useField<string>('rate_limit_error');
  const terminalErrorKeywords = useField<string>('invalid_request_error');
  const retryDelayMs = useField<number>(0);
  const retrySameAccountMax = useField<number>(0);

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

  // Fetch all policy rows once on mount (stored in state so hydration effect can react)
  useEffect(() => {
    void listPolicies()
      .then((rows) => {
        setPolicyRows(rows.map((r) => ({
          scopeType: r.scopeType,
          scopeId: r.scopeId,
          params: (r.params ?? {}) as Record<string, unknown>,
        })));
      })
      .catch(() => { /* silently ignore — page still usable */ });
  }, []);

  // Hydrate form fields whenever scope / selectedAccount / loaded rows change.
  // Uses a stable callback reference trick: define helpers inline so they close
  // over the current field refs without being listed as deps (the helpers only
  // read .enabled which is stable between renders caused by these deps).
  useEffect(() => {
    // ---- helpers ----
    const setNum = (f: FieldState<number> & { set: (v: number) => void; toggle: () => void }, p: Record<string, unknown>, key: string) => {
      if (key in p) { f.set(Number(p[key])); if (!f.enabled) f.toggle(); }
    };
    const setBool = (f: FieldState<boolean> & { set: (v: boolean) => void; toggle: () => void }, p: Record<string, unknown>, key: string) => {
      if (key in p) { f.set(Boolean(p[key])); if (!f.enabled) f.toggle(); }
    };
    const setStr = (f: FieldState<string> & { set: (v: string) => void; toggle: () => void }, p: Record<string, unknown>, key: string) => {
      if (key in p) { f.set(String(p[key] ?? '')); if (!f.enabled) f.toggle(); }
    };
    const setRange = (
      fMin: FieldState<number> & { set: (v: number) => void; toggle: () => void },
      fMax: FieldState<number> & { set: (v: number) => void; toggle: () => void },
      p: Record<string, unknown>,
      key: string,
    ) => {
      if (key in p) {
        const r = p[key] as { Min?: number; Max?: number } | undefined;
        if (r && typeof r === 'object') {
          fMin.set(Number(r.Min ?? 0));
          fMax.set(Number(r.Max ?? 0));
          if (!fMin.enabled) fMin.toggle();
        }
      }
    };
    const setArr = (f: FieldState<string> & { set: (v: string) => void; toggle: () => void }, p: Record<string, unknown>, key: string) => {
      if (key in p) {
        const arr = p[key] ?? [];
        f.set((arr as unknown[]).join(','));
        if (!f.enabled) f.toggle();
      }
    };

    // ---- resetAllFields: disable every field so switching to an account
    //      with no overrides shows all fields as "继承全局" ----
    const allFields: Array<{ enabled: boolean; toggle: () => void }> = [
      idleFirstSelection, maxConcurrent, slotCooldownMinMs, slotCooldownMaxMs,
      banPersistStreak, permanentBanStreak, cooldownBaseMs, cooldownMaxMs, cooldownMult,
      affinityTTLSec, affinityWaitMs,
      fallbackEnabled, fallbackPriceThresholdUsd, fallbackKeywords, fallbackModels, fallbackProbeEnabled,
      banSignals, banKeywords, cooldownSignals, cooldownSignalSec,
      maxFailover,
      directFallbackStatusCodes, directFallbackKeywords, terminalErrorKeywords, retryDelayMs, retrySameAccountMax,
      limitOpus48, limitOpus47, limitSonnet46, limitHaiku45,
      warmupHours, warmupMaxConcurrent, warmupBlockOpus,
      sessionErrorThreshold, sessionCooldownSec, responseExileEnabled, responseExileKeywords,
      quotaLimitKeywords, quotaLimitCodes,
      elasticEnabled, elasticScaleUpUtil, elasticScaleDownUtil, elasticMaxReserve, elasticBaselineCount,
      spendCap5hEnabled, spendCap5hMin, spendCap5hMax,
      humanDelayEnabled, humanDelayDist, humanDelayP50Min, humanDelayP50Max, humanDelayP95Min, humanDelayP95Max,
      rateGovEnabled, rateRPMMin, rateRPMMax, rateRPHMin, rateRPHMax, rateRPDMin, rateRPDMax, rateExceedAction,
      sessionSimEnabled, sessionBurstCountMin, sessionBurstCountMax, sessionPauseMsMin, sessionPauseMsMax,
      quietHoursEnabled, quietHoursStartMin, quietHoursEndMin, quietHoursRPMMin, quietHoursRPMMax, quietHoursConcurrency,
      modelPinEnabled, modelPinMode, modelPinTarget, modelElasticEnabled,
      serialQueueEnabled, serialQueueWaitMs,
      bodyPadEnabled, bodyPadBytesMin, bodyPadBytesMax,
    ];
    // Disable every enabled field
    for (const f of allFields) {
      if (f.enabled) f.toggle();
    }

    // ---- hydrateFrom: apply all key→field mappings from params object p ----
    const hydrateFrom = (p: Record<string, unknown>) => {
      setBool(idleFirstSelection, p, 'IdleFirstSelection');
      setNum(maxConcurrent, p, 'MaxConcurrent');
      setNum(slotCooldownMinMs, p, 'SlotCooldownMinMs');
      setNum(slotCooldownMaxMs, p, 'SlotCooldownMaxMs');
      setNum(banPersistStreak, p, 'BanPersistStreak');
      setNum(permanentBanStreak, p, 'PermanentBanStreak');
      setNum(cooldownBaseMs, p, 'CooldownBaseMs');
      setNum(cooldownMaxMs, p, 'CooldownMaxMs');
      setNum(cooldownMult, p, 'CooldownMult');
      setNum(affinityTTLSec, p, 'AffinityTTLSec');
      setNum(affinityWaitMs, p, 'AffinityWaitMs');
      setBool(fallbackEnabled, p, 'FallbackEnabled');
      setNum(fallbackPriceThresholdUsd, p, 'FallbackPriceThresholdUsd');
      setArr(fallbackKeywords, p, 'FallbackKeywords');
      setArr(fallbackModels, p, 'FallbackModels');
      setBool(fallbackProbeEnabled, p, 'FallbackProbeEnabled');
      setArr(banSignals, p, 'BanSignals');
      setArr(banKeywords, p, 'BanKeywords');
      setArr(cooldownSignals, p, 'CooldownSignals');
      setNum(cooldownSignalSec, p, 'CooldownSignalSec');
      setNum(maxFailover, p, 'MaxFailover');
      setArr(directFallbackStatusCodes, p, 'DirectFallbackStatusCodes');
      setArr(directFallbackKeywords, p, 'DirectFallbackKeywords');
      setArr(terminalErrorKeywords, p, 'TerminalErrorKeywords');
      setNum(retryDelayMs, p, 'RetryDelayMs');
      setNum(retrySameAccountMax, p, 'RetrySameAccountMax');
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
      setNum(warmupHours, p, 'WarmupHours');
      setNum(warmupMaxConcurrent, p, 'WarmupMaxConcurrent');
      setBool(warmupBlockOpus, p, 'WarmupBlockOpus');
      setNum(sessionErrorThreshold, p, 'SessionErrorThreshold');
      setNum(sessionCooldownSec, p, 'SessionCooldownSec');
      setBool(responseExileEnabled, p, 'ResponseExileEnabled');
      setArr(responseExileKeywords, p, 'ResponseExileKeywords');
      setArr(quotaLimitKeywords, p, 'QuotaLimitKeywords');
      setArr(quotaLimitCodes, p, 'QuotaLimitStatusCodes');
      setBool(elasticEnabled, p, 'ElasticEnabled');
      setNum(elasticScaleUpUtil, p, 'ElasticScaleUpUtil');
      setNum(elasticScaleDownUtil, p, 'ElasticScaleDownUtil');
      setNum(elasticMaxReserve, p, 'ElasticMaxReserve');
      setNum(elasticBaselineCount, p, 'ElasticBaselineCount');
      // Phase 2: SpendCap (cumulative today-spend vs raising threshold)
      setBool(spendCap5hEnabled, p, 'SpendCap5hEnabled');
      setRange(spendCap5hMin, spendCap5hMax, p, 'SpendCap5hUsd');
      // Phase 3: HumanDelay
      setBool(humanDelayEnabled, p, 'HumanDelayEnabled');
      setStr(humanDelayDist, p, 'HumanDelayDist');
      setRange(humanDelayP50Min, humanDelayP50Max, p, 'HumanDelayP50Ms');
      setRange(humanDelayP95Min, humanDelayP95Max, p, 'HumanDelayP95Ms');
      // Phase 3: RateGovernor
      setBool(rateGovEnabled, p, 'RateGovEnabled');
      setRange(rateRPMMin, rateRPMMax, p, 'RateRPM');
      setRange(rateRPHMin, rateRPHMax, p, 'RateRPH');
      setRange(rateRPDMin, rateRPDMax, p, 'RateRPD');
      setStr(rateExceedAction, p, 'RateExceedAction');
      // Phase 3: SessionSim
      setBool(sessionSimEnabled, p, 'SessionSimEnabled');
      setRange(sessionBurstCountMin, sessionBurstCountMax, p, 'SessionBurstCount');
      setRange(sessionPauseMsMin, sessionPauseMsMax, p, 'SessionPauseMs');
      // Phase 3: QuietHours
      setBool(quietHoursEnabled, p, 'QuietHoursEnabled');
      if ('QuietHoursWindows' in p) {
        const wins = p['QuietHoursWindows'] as Array<{ StartMin?: number; EndMin?: number }> | undefined;
        if (Array.isArray(wins) && wins.length > 0) {
          quietHoursStartMin.set(Number(wins[0].StartMin ?? 0));
          quietHoursEndMin.set(Number(wins[0].EndMin ?? 0));
          if (!quietHoursStartMin.enabled) quietHoursStartMin.toggle();
        }
      }
      setRange(quietHoursRPMMin, quietHoursRPMMax, p, 'QuietHoursRPM');
      setNum(quietHoursConcurrency, p, 'QuietHoursConcurrency');
      // Phase 4: ModelPin
      setBool(modelPinEnabled, p, 'ModelPinEnabled');
      setStr(modelPinMode, p, 'ModelPinMode');
      setStr(modelPinTarget, p, 'ModelPinTarget');
      setBool(modelElasticEnabled, p, 'ModelElasticEnabled');
      // Phase 4: SerialQueue
      setBool(serialQueueEnabled, p, 'SerialQueueEnabled');
      setNum(serialQueueWaitMs, p, 'SerialQueueWaitMs');
      // Phase 4: BodyPad
      setBool(bodyPadEnabled, p, 'BodyPadEnabled');
      setRange(bodyPadBytesMin, bodyPadBytesMax, p, 'BodyPadBytes');
    };

    // ---- Pick the right row to hydrate from ----
    if (policyRows.length === 0) return; // rows not loaded yet — wait

    const globalRow = policyRows.find((r) => r.scopeType === 'global');

    if (scope === 'global') {
      if (globalRow?.params) hydrateFrom(globalRow.params);
    } else {
      // scope === 'account'
      const accountRow = policyRows.find(
        (r) => r.scopeType === 'account' && r.scopeId === selectedAccountId,
      );
      if (accountRow?.params) {
        hydrateFrom(accountRow.params);
      }
      // else: all fields left as disabled (inherit global) — no-op needed
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, selectedAccountId, policyRows]);

  // Build patch — only include enabled fields
  function buildPatch(): PolicyPatch {
    const patch: PolicyPatch = {};
    if (idleFirstSelection.enabled) patch.IdleFirstSelection = idleFirstSelection.value;
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
    if (affinityWaitMs.enabled) patch.AffinityWaitMs = affinityWaitMs.value;
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
    if (directFallbackStatusCodes.enabled) patch.DirectFallbackStatusCodes = directFallbackStatusCodes.value.split(',').map(s => parseInt(s.trim(), 10)).filter(n => !isNaN(n));
    if (directFallbackKeywords.enabled) patch.DirectFallbackKeywords = directFallbackKeywords.value.split(',').map(s => s.trim()).filter(Boolean);
    if (terminalErrorKeywords.enabled) patch.TerminalErrorKeywords = terminalErrorKeywords.value.split(',').map(s => s.trim()).filter(Boolean);
    if (retryDelayMs.enabled) patch.RetryDelayMs = retryDelayMs.value;
    if (retrySameAccountMax.enabled) patch.RetrySameAccountMax = retrySameAccountMax.value;
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
    // SpendCap: cumulative today-spend vs raising threshold
    if (spendCap5hEnabled.enabled) patch.SpendCap5hEnabled = spendCap5hEnabled.value;
    if (spendCap5hMin.enabled) patch.SpendCap5hUsd = { Min: spendCap5hMin.value, Max: spendCap5hMax.value };
    // Phase 3: HumanDelay
    if (humanDelayEnabled.enabled) patch.HumanDelayEnabled = humanDelayEnabled.value;
    if (humanDelayDist.enabled) patch.HumanDelayDist = humanDelayDist.value;
    if (humanDelayP50Min.enabled) patch.HumanDelayP50Ms = { Min: humanDelayP50Min.value, Max: humanDelayP50Max.value };
    if (humanDelayP95Min.enabled) patch.HumanDelayP95Ms = { Min: humanDelayP95Min.value, Max: humanDelayP95Max.value };
    // Phase 3: RateGovernor
    if (rateGovEnabled.enabled) patch.RateGovEnabled = rateGovEnabled.value;
    if (rateRPMMin.enabled) patch.RateRPM = { Min: rateRPMMin.value, Max: rateRPMMax.value };
    if (rateRPHMin.enabled) patch.RateRPH = { Min: rateRPHMin.value, Max: rateRPHMax.value };
    if (rateRPDMin.enabled) patch.RateRPD = { Min: rateRPDMin.value, Max: rateRPDMax.value };
    if (rateExceedAction.enabled) patch.RateExceedAction = rateExceedAction.value;
    // Phase 3: SessionSim
    if (sessionSimEnabled.enabled) patch.SessionSimEnabled = sessionSimEnabled.value;
    if (sessionBurstCountMin.enabled) patch.SessionBurstCount = { Min: sessionBurstCountMin.value, Max: sessionBurstCountMax.value };
    if (sessionPauseMsMin.enabled) patch.SessionPauseMs = { Min: sessionPauseMsMin.value, Max: sessionPauseMsMax.value };
    // Phase 3: QuietHours
    if (quietHoursEnabled.enabled) patch.QuietHoursEnabled = quietHoursEnabled.value;
    if (quietHoursStartMin.enabled) patch.QuietHoursWindows = [{ StartMin: quietHoursStartMin.value, EndMin: quietHoursEndMin.value }];
    if (quietHoursRPMMin.enabled) patch.QuietHoursRPM = { Min: quietHoursRPMMin.value, Max: quietHoursRPMMax.value };
    if (quietHoursConcurrency.enabled) patch.QuietHoursConcurrency = quietHoursConcurrency.value;
    // Phase 4: ModelPin
    if (modelPinEnabled.enabled) patch.ModelPinEnabled = modelPinEnabled.value;
    if (modelPinMode.enabled) patch.ModelPinMode = modelPinMode.value;
    if (modelPinTarget.enabled) patch.ModelPinTarget = modelPinTarget.value;
    if (modelElasticEnabled.enabled) patch.ModelElasticEnabled = modelElasticEnabled.value;
    // Phase 4: SerialQueue
    if (serialQueueEnabled.enabled) patch.SerialQueueEnabled = serialQueueEnabled.value;
    if (serialQueueWaitMs.enabled) patch.SerialQueueWaitMs = serialQueueWaitMs.value;
    // Phase 4: BodyPad
    if (bodyPadEnabled.enabled) patch.BodyPadEnabled = bodyPadEnabled.value;
    if (bodyPadBytesMin.enabled) patch.BodyPadBytes = { Min: bodyPadBytesMin.value, Max: bodyPadBytesMax.value };
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
      // Re-fetch policy rows so switching scope reflects the saved state immediately.
      const freshRows = await listPolicies();
      setPolicyRows(freshRows.map((r) => ({
        scopeType: r.scopeType,
        scopeId: r.scopeId,
        params: (r.params ?? {}) as Record<string, unknown>,
      })));
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
      // Re-fetch policy rows so the deleted account override is removed from state.
      const freshRows = await listPolicies();
      setPolicyRows(freshRows.map((r) => ({
        scopeType: r.scopeType,
        scopeId: r.scopeId,
        params: (r.params ?? {}) as Record<string, unknown>,
      })));
      setTimeout(() => setSaveMsg(null), 3000);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '清除失败');
    }
  }

  const anyEnabled = [
    idleFirstSelection, maxConcurrent, slotCooldownMinMs, banPersistStreak, permanentBanStreak,
    cooldownBaseMs, cooldownMaxMs, cooldownMult, affinityTTLSec, affinityWaitMs,
    fallbackEnabled, fallbackPriceThresholdUsd, fallbackKeywords, fallbackModels, fallbackProbeEnabled, banSignals, banKeywords, cooldownSignals, cooldownSignalSec,
    maxFailover,
    warmupHours, warmupMaxConcurrent, warmupBlockOpus,
    sessionErrorThreshold, sessionCooldownSec, responseExileEnabled, responseExileKeywords, quotaLimitKeywords, quotaLimitCodes,
    elasticEnabled, elasticScaleUpUtil, elasticScaleDownUtil, elasticMaxReserve, elasticBaselineCount,
    spendCap5hEnabled, spendCap5hMin, spendCap5hMax,
    // Phase 3
    humanDelayEnabled, humanDelayDist, humanDelayP50Min, humanDelayP95Min,
    rateGovEnabled, rateRPMMin, rateRPHMin, rateRPDMin, rateExceedAction,
    sessionSimEnabled, sessionBurstCountMin, sessionPauseMsMin,
    quietHoursEnabled, quietHoursStartMin, quietHoursRPMMin, quietHoursConcurrency,
    // Phase 4
    modelPinEnabled, modelPinMode, modelPinTarget,
    serialQueueEnabled, serialQueueWaitMs,
    bodyPadEnabled, bodyPadBytesMin,
    directFallbackStatusCodes, directFallbackKeywords, terminalErrorKeywords, retryDelayMs, retrySameAccountMax,
  ].some((f) => f.enabled);

  // ------------------------------------------------------------------
  // Per-category field membership (for badge counts)
  // ------------------------------------------------------------------
  const catFields: Record<CatId, Array<{ enabled: boolean }>> = {
    cadence: [
      humanDelayEnabled, humanDelayDist, humanDelayP50Min, humanDelayP95Min,
      rateGovEnabled, rateRPMMin, rateRPHMin, rateRPDMin, rateExceedAction,
      sessionSimEnabled, sessionBurstCountMin, sessionPauseMsMin,
      quietHoursEnabled, quietHoursStartMin, quietHoursRPMMin, quietHoursConcurrency,
      serialQueueEnabled, serialQueueWaitMs,
      modelPinEnabled, modelPinMode, modelPinTarget, modelElasticEnabled,
      bodyPadEnabled, bodyPadBytesMin,
    ],
    concurrency: [
      idleFirstSelection, maxConcurrent, slotCooldownMinMs,
      affinityTTLSec, affinityWaitMs,
      warmupHours, warmupMaxConcurrent, warmupBlockOpus,
    ],
    limits: [
      spendCap5hEnabled, spendCap5hMin,
      quotaLimitKeywords, quotaLimitCodes,
      limitOpus48, limitOpus47, limitSonnet46, limitHaiku45,
    ],
    fallback: [
      fallbackEnabled, fallbackPriceThresholdUsd, fallbackKeywords, fallbackModels, fallbackProbeEnabled,
      maxFailover,
      directFallbackStatusCodes, directFallbackKeywords, terminalErrorKeywords, retryDelayMs, retrySameAccountMax,
      elasticEnabled, elasticScaleUpUtil, elasticScaleDownUtil, elasticMaxReserve, elasticBaselineCount,
    ],
    signals: [
      banSignals, banKeywords,
      cooldownSignals, cooldownSignalSec,
      banPersistStreak, permanentBanStreak, cooldownBaseMs, cooldownMaxMs, cooldownMult,
      responseExileEnabled, responseExileKeywords,
      sessionErrorThreshold, sessionCooldownSec,
    ],
  };

  function catCount(id: CatId): number {
    return catFields[id].filter((f) => f.enabled).length;
  }

  // ------------------------------------------------------------------
  // Render helpers
  // ------------------------------------------------------------------
  const so = onlyConfigured; // shorthand for showOnlyConfigured prop

  function CatContent() {
    switch (cat) {
      case 'cadence':
        return (
          <>
            {/* Group 1: HumanDelay (人类延迟) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">人类延迟（HumanDelay）</h2>
                  <p className="text-xs text-muted/70 mt-0.5">在每次请求前注入仿人类延迟。uniform = 均匀随机；lognormal = 对数正态（更真实）。</p>
                </div>
                <GroupMaster field={humanDelayEnabled} />
              </div>
              <div className={humanDelayEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="HumanDelayDist" desc="延迟分布类型：uniform（均匀）或 lognormal（对数正态，需设置 P50/P95）" enabled={humanDelayDist.enabled} onToggle={humanDelayDist.toggle} showOnlyConfigured={so}>
                  <select value={humanDelayDist.value} onChange={(e) => humanDelayDist.set(e.target.value)} disabled={!humanDelayDist.enabled} className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink focus:outline-none focus:border-accent transition disabled:cursor-not-allowed">
                    <option value="uniform">uniform（均匀随机）</option>
                    <option value="lognormal">lognormal（对数正态）</option>
                  </select>
                </FieldRow>

                <FieldRow label="HumanDelayP50Ms (min ~ max)" desc="P50 延迟随机区间 (ms)，仅 lognormal 模式生效" enabled={humanDelayP50Min.enabled} onToggle={humanDelayP50Min.toggle} showOnlyConfigured={so}>
                  <RangeInput min={humanDelayP50Min.value} max={humanDelayP50Max.value} onChangeMin={humanDelayP50Min.set} onChangeMax={humanDelayP50Max.set} disabled={!humanDelayP50Min.enabled} step={100} minLabel="min ms" maxLabel="max ms" />
                </FieldRow>

                <FieldRow label="HumanDelayP95Ms (min ~ max)" desc="P95 延迟随机区间 (ms)，仅 lognormal 模式生效" enabled={humanDelayP95Min.enabled} onToggle={humanDelayP95Min.toggle} showOnlyConfigured={so}>
                  <RangeInput min={humanDelayP95Min.value} max={humanDelayP95Max.value} onChangeMin={humanDelayP95Min.set} onChangeMax={humanDelayP95Max.set} disabled={!humanDelayP95Min.enabled} step={100} minLabel="min ms" maxLabel="max ms" />
                </FieldRow>
              </div>
            </div>

            {/* Group 2: RateGovernor (利率治理) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">利率治理（RateGovernor）</h2>
                  <p className="text-xs text-muted/70 mt-0.5">按请求频率（RPM/RPH/RPD）限速，超出时执行 rotate（换号）。0 = 不限。</p>
                </div>
                <GroupMaster field={rateGovEnabled} />
              </div>
              <div className={rateGovEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="RateRPM (min ~ max)" desc="每分钟请求数限制随机区间；0 = 不限" enabled={rateRPMMin.enabled} onToggle={rateRPMMin.toggle} showOnlyConfigured={so}>
                  <RangeInput min={rateRPMMin.value} max={rateRPMMax.value} onChangeMin={rateRPMMin.set} onChangeMax={rateRPMMax.set} disabled={!rateRPMMin.enabled} step={1} minLabel="min rpm" maxLabel="max rpm" />
                </FieldRow>

                <FieldRow label="RateRPH (min ~ max)" desc="每小时请求数限制随机区间；0 = 不限" enabled={rateRPHMin.enabled} onToggle={rateRPHMin.toggle} showOnlyConfigured={so}>
                  <RangeInput min={rateRPHMin.value} max={rateRPHMax.value} onChangeMin={rateRPHMin.set} onChangeMax={rateRPHMax.set} disabled={!rateRPHMin.enabled} step={10} minLabel="min rph" maxLabel="max rph" />
                </FieldRow>

                <FieldRow label="RateRPD (min ~ max)" desc="每天请求数限制随机区间；0 = 不限" enabled={rateRPDMin.enabled} onToggle={rateRPDMin.toggle} showOnlyConfigured={so}>
                  <RangeInput min={rateRPDMin.value} max={rateRPDMax.value} onChangeMin={rateRPDMin.set} onChangeMax={rateRPDMax.set} disabled={!rateRPDMin.enabled} step={100} minLabel="min rpd" maxLabel="max rpd" />
                </FieldRow>

                <FieldRow label="RateExceedAction" desc="超出频率限制时的动作：rotate（切换到下一个账号）。仅支持 rotate。" enabled={rateExceedAction.enabled} onToggle={rateExceedAction.toggle} showOnlyConfigured={so}>
                  <select value={rateExceedAction.value} onChange={(e) => rateExceedAction.set(e.target.value)} disabled={!rateExceedAction.enabled} className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink focus:outline-none focus:border-accent transition disabled:cursor-not-allowed">
                    <option value="rotate">rotate（换号）</option>
                  </select>
                </FieldRow>
              </div>
            </div>

            {/* Group 3: SessionSim (会话模拟) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">会话模拟（SessionSim）</h2>
                  <p className="text-xs text-muted/70 mt-0.5">模拟真实用户会话节奏：连发 N 条后暂停一段时间，避免持续高并发。</p>
                </div>
                <GroupMaster field={sessionSimEnabled} />
              </div>
              <div className={sessionSimEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="SessionBurstCount (min ~ max)" desc="每个会话突发请求数随机区间（连发 N 条后进入暂停）" enabled={sessionBurstCountMin.enabled} onToggle={sessionBurstCountMin.toggle} showOnlyConfigured={so}>
                  <RangeInput min={sessionBurstCountMin.value} max={sessionBurstCountMax.value} onChangeMin={sessionBurstCountMin.set} onChangeMax={sessionBurstCountMax.set} disabled={!sessionBurstCountMin.enabled} step={1} minLabel="min 条" maxLabel="max 条" />
                </FieldRow>

                <FieldRow label="SessionPauseMs (min ~ max)" desc="突发后暂停时长随机区间 (ms)" enabled={sessionPauseMsMin.enabled} onToggle={sessionPauseMsMin.toggle} showOnlyConfigured={so}>
                  <RangeInput min={sessionPauseMsMin.value} max={sessionPauseMsMax.value} onChangeMin={sessionPauseMsMin.set} onChangeMax={sessionPauseMsMax.set} disabled={!sessionPauseMsMin.enabled} step={1000} minLabel="min ms" maxLabel="max ms" />
                </FieldRow>
              </div>
            </div>

            {/* Group 4: QuietHours (安静时段) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">安静时段（QuietHours）</h2>
                  <p className="text-xs text-muted/70 mt-0.5">在指定时段内自动降速（低 RPM + 低并发），模拟人类夜间休息行为。时间以本地分钟数（0=00:00, 360=06:00）表示。</p>
                </div>
                <GroupMaster field={quietHoursEnabled} />
              </div>
              <div className={quietHoursEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="安静时段窗口（开始 ~ 结束）" desc="以分钟数表示（0=00:00, 360=06:00, 1380=23:00）" enabled={quietHoursStartMin.enabled} onToggle={quietHoursStartMin.toggle} showOnlyConfigured={so}>
                  <div className="flex items-center gap-2">
                    <div className="flex flex-col flex-1 min-w-0">
                      <span className="text-xs text-muted mb-0.5">开始 (min)</span>
                      <NumInput value={quietHoursStartMin.value} onChange={quietHoursStartMin.set} disabled={!quietHoursStartMin.enabled} min={0} max={1439} step={30} placeholder="0" />
                    </div>
                    <span className="text-muted text-sm mt-4">~</span>
                    <div className="flex flex-col flex-1 min-w-0">
                      <span className="text-xs text-muted mb-0.5">结束 (min)</span>
                      <NumInput value={quietHoursEndMin.value} onChange={quietHoursEndMin.set} disabled={!quietHoursStartMin.enabled} min={0} max={1439} step={30} placeholder="360" />
                    </div>
                  </div>
                </FieldRow>

                <FieldRow label="QuietHoursRPM (min ~ max)" desc="安静时段允许的每分钟请求数随机区间" enabled={quietHoursRPMMin.enabled} onToggle={quietHoursRPMMin.toggle} showOnlyConfigured={so}>
                  <RangeInput min={quietHoursRPMMin.value} max={quietHoursRPMMax.value} onChangeMin={quietHoursRPMMin.set} onChangeMax={quietHoursRPMMax.set} disabled={!quietHoursRPMMin.enabled} step={1} minLabel="min rpm" maxLabel="max rpm" />
                </FieldRow>

                <FieldRow label="QuietHoursConcurrency" desc="安静时段最大并发数" enabled={quietHoursConcurrency.enabled} onToggle={quietHoursConcurrency.toggle} showOnlyConfigured={so}>
                  <NumInput value={quietHoursConcurrency.value} onChange={quietHoursConcurrency.set} disabled={!quietHoursConcurrency.enabled} min={1} />
                </FieldRow>
              </div>
            </div>

            {/* Group 5: SerialQueue (串行队列) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">串行队列（SerialQueue）</h2>
                  <p className="text-xs text-muted/70 mt-0.5">强制账号内请求串行执行（同一账号同时只跑一个请求），模拟单用户行为。超时后自动放弃等位。</p>
                </div>
                <GroupMaster field={serialQueueEnabled} />
              </div>
              <div className={serialQueueEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="SerialQueueWaitMs" desc="等位超时 (ms)：超时后放弃排队，换号或返回 503" enabled={serialQueueWaitMs.enabled} onToggle={serialQueueWaitMs.toggle} showOnlyConfigured={so}>
                  <NumInput value={serialQueueWaitMs.value} onChange={serialQueueWaitMs.set} disabled={!serialQueueWaitMs.enabled} min={0} step={1000} />
                </FieldRow>
              </div>
            </div>

            {/* Group 6: ModelPin (模型锁定) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">模型锁定（ModelPin）</h2>
                  <p className="text-xs text-muted/70 mt-0.5">将账号绑定到特定模型或按首次请求粘性锁定，避免不同模型混用暴露多账号特征。</p>
                </div>
                <GroupMaster field={modelPinEnabled} />
              </div>
              <div className={modelPinEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="ModelPinMode" desc="锁定模式：sticky = 按首次请求模型粘性绑定；fixed = 始终锁定到 Target 模型" enabled={modelPinMode.enabled} onToggle={modelPinMode.toggle} showOnlyConfigured={so}>
                  <select value={modelPinMode.value} onChange={(e) => modelPinMode.set(e.target.value)} disabled={!modelPinMode.enabled} className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink focus:outline-none focus:border-accent transition disabled:cursor-not-allowed">
                    <option value="sticky">sticky（粘性，按首次请求锁定）</option>
                    <option value="fixed">fixed（固定，始终用 Target）</option>
                  </select>
                </FieldRow>

                <FieldRow label="ModelPinTarget" desc="固定模式下锁定的目标模型名（仅 fixed 模式生效）" enabled={modelPinTarget.enabled} onToggle={modelPinTarget.toggle} showOnlyConfigured={so}>
                  <input type="text" value={modelPinTarget.value} onChange={(e) => modelPinTarget.set(e.target.value)} disabled={!modelPinTarget.enabled} placeholder="claude-sonnet-4-6" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
                </FieldRow>
                <FieldRow label="ModelElasticEnabled" desc="模型感知弹性:开启后弹性基准不受模型钉影响,模型钉只在活跃集内选号;某模型活跃集覆盖不了才为它弹性激活一个待命号(发模型扩容事件)。需同时开 ModelPin + Elastic。关=旧行为(模型钉可能把待命号拉进轮换)。" enabled={modelElasticEnabled.enabled} onToggle={modelElasticEnabled.toggle} showOnlyConfigured={so}>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input type="checkbox" checked={modelElasticEnabled.value} onChange={(e) => modelElasticEnabled.set(e.target.checked)} disabled={!modelElasticEnabled.enabled} className="accent-accent w-4 h-4" />
                    <span className="text-sm text-muted">{modelElasticEnabled.value ? '模型感知弹性(开)' : '旧行为(关)'}</span>
                  </label>
                </FieldRow>
              </div>
            </div>

            {/* Group 7: BodyPad (请求体填充) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">
                    请求体填充（BodyPad）
                    <span className="ml-2 text-xs font-normal text-muted normal-case">(需上游验证)</span>
                  </h2>
                  <p className="text-xs text-muted/70 mt-0.5">在请求体末尾追加随机填充字节，使每次请求大小不完全相同，混淆流量指纹。</p>
                </div>
                <GroupMaster field={bodyPadEnabled} />
              </div>
              <div className={bodyPadEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="BodyPadBytes (min ~ max)" desc="每次请求随机填充字节数区间；0 ~ 0 = 不填充" enabled={bodyPadBytesMin.enabled} onToggle={bodyPadBytesMin.toggle} showOnlyConfigured={so}>
                  <RangeInput min={bodyPadBytesMin.value} max={bodyPadBytesMax.value} onChangeMin={bodyPadBytesMin.set} onChangeMax={bodyPadBytesMax.set} disabled={!bodyPadBytesMin.enabled} step={64} minLabel="min B" maxLabel="max B" />
                </FieldRow>
              </div>
            </div>
          </>
        );

      case 'concurrency':
        return (
          <>
            {/* Group: 并发 / 冷却 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">并发 / 冷却</h2>

              <FieldRow label="IdleFirstSelection" desc="空闲优先选号:按当前并发数从低到高排候选号(相同空闲随机打散),让流量铺满所有号。关闭则按权重固定顺序。" enabled={idleFirstSelection.enabled} onToggle={idleFirstSelection.toggle} showOnlyConfigured={so}>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input type="checkbox" checked={idleFirstSelection.value} onChange={(e) => idleFirstSelection.set(e.target.checked)} disabled={!idleFirstSelection.enabled} className="accent-accent w-4 h-4" />
                  <span className="text-sm text-muted">{idleFirstSelection.value ? '空闲优先(开)' : '固定权重顺序(关)'}</span>
                </label>
              </FieldRow>

              <FieldRow label="MaxConcurrent" desc="每账号最大并发槽位数(节点总并发 = 账号数 × 此值)" enabled={maxConcurrent.enabled} onToggle={maxConcurrent.toggle} showOnlyConfigured={so}>
                <NumInput value={maxConcurrent.value} onChange={maxConcurrent.set} disabled={!maxConcurrent.enabled} min={1} />
              </FieldRow>

              {/* SlotCooldown rendered as a single RangeInput widget.
                  Backend still stores SlotCooldownMinMs / SlotCooldownMaxMs separately;
                  buildPatch() maps Min→SlotCooldownMinMs, Max→SlotCooldownMaxMs.
                  Both fields share the same enabled toggle (slotCooldownMinMs). */}
              <FieldRow label="SlotCooldownMs (min ~ max)" desc="槽位冷却时长区间 (ms)：每次随机取 [min, max] 内的值" enabled={slotCooldownMinMs.enabled} onToggle={slotCooldownMinMs.toggle} showOnlyConfigured={so}>
                <RangeInput min={slotCooldownMinMs.value} max={slotCooldownMaxMs.value} onChangeMin={slotCooldownMinMs.set} onChangeMax={slotCooldownMaxMs.set} disabled={!slotCooldownMinMs.enabled} step={100} />
              </FieldRow>
            </div>

            {/* Group: 亲和性 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">亲和性</h2>

              <FieldRow label="AffinityTTLSec" desc="亲和性缓存 TTL (秒)" enabled={affinityTTLSec.enabled} onToggle={affinityTTLSec.toggle} showOnlyConfigured={so}>
                <NumInput value={affinityTTLSec.value} onChange={affinityTTLSec.set} disabled={!affinityTTLSec.enabled} min={0} step={60} />
              </FieldRow>

              <FieldRow label="AffinityWaitMs" desc="亲和号忙时排队等位上限(ms);0=不等待直接转保底" enabled={affinityWaitMs.enabled} onToggle={affinityWaitMs.toggle} showOnlyConfigured={so}>
                <NumInput value={affinityWaitMs.value} onChange={affinityWaitMs.set} disabled={!affinityWaitMs.enabled} min={0} step={500} />
              </FieldRow>
            </div>

            {/* Group: 预热 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">预热</h2>

              <FieldRow label="WarmupHours" desc="预热时长(小时,0=关)" enabled={warmupHours.enabled} onToggle={warmupHours.toggle} showOnlyConfigured={so}>
                <NumInput value={warmupHours.value} onChange={warmupHours.set} disabled={!warmupHours.enabled} min={0} />
              </FieldRow>

              <FieldRow label="WarmupMaxConcurrent" desc="预热期最大并发" enabled={warmupMaxConcurrent.enabled} onToggle={warmupMaxConcurrent.toggle} showOnlyConfigured={so}>
                <NumInput value={warmupMaxConcurrent.value} onChange={warmupMaxConcurrent.set} disabled={!warmupMaxConcurrent.enabled} min={1} />
              </FieldRow>

              <FieldRow label="WarmupBlockOpus" desc="预热期挡 opus" enabled={warmupBlockOpus.enabled} onToggle={warmupBlockOpus.toggle} showOnlyConfigured={so}>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input type="checkbox" checked={warmupBlockOpus.value} onChange={(e) => warmupBlockOpus.set(e.target.checked)} disabled={!warmupBlockOpus.enabled} className="accent-accent w-4 h-4" />
                  <span className="text-sm text-ink">{warmupBlockOpus.value ? '已启用' : '已禁用'}</span>
                </label>
              </FieldRow>
            </div>
          </>
        );

      case 'limits':
        return (
          <>
            {/* Group: 自保限额 (SpendCap) */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">自保限额（今日累计花费 vs 递增阈值）</h2>
              <p className="text-xs text-muted/70 -mt-1 mb-1">每号今日累计花费超过阈值 T 则触发限额；恢复后阈值自动提升（T → T+cap），当日清零后重新锚定 T₀。0 = 不限。</p>

              {/* 今日累计花费 vs 递增阈值 */}
              <FieldRow label="SpendCap5hEnabled" desc="启用今日累计花费上限检测。每号初始阈值 T₀∈[min,max]；触发后按配额重置时间恢复，每次恢复阈值提升一个 cap 档（T → T+cap）" enabled={spendCap5hEnabled.enabled} onToggle={spendCap5hEnabled.toggle} showOnlyConfigured={so}>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input type="checkbox" checked={spendCap5hEnabled.value} onChange={(e) => spendCap5hEnabled.set(e.target.checked)} disabled={!spendCap5hEnabled.enabled} className="accent-accent w-4 h-4" />
                  <span className="text-sm text-ink">{spendCap5hEnabled.value ? '已启用' : '已禁用'}</span>
                </label>
              </FieldRow>

              <FieldRow label="SpendCap5hUsd (min ~ max)" desc="每档阈值区间 (USD)：每号/每轮随机抽取，初始 T₀，每轮触发后 T += 再抽一次。0 = 不限。" enabled={spendCap5hMin.enabled} onToggle={spendCap5hMin.toggle} showOnlyConfigured={so}>
                <RangeInput min={spendCap5hMin.value} max={spendCap5hMax.value} onChangeMin={spendCap5hMin.set} onChangeMax={spendCap5hMax.set} disabled={!spendCap5hMin.enabled} step={0.01} minLabel="min $" maxLabel="max $" />
              </FieldRow>

            </div>

            {/* Group: 限额检测关键词 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">限额检测</h2>

              <FieldRow label="限额关键词(逗号分隔,命中即配额限流)" desc="错误响应命中任一关键词时,把该号标记为限额(配额),按返回的重置时间自动恢复。仅匹配真实用量耗尽措辞(如 hit your limit),不要填 rate_limit_error(瞬时限流会误触)" enabled={quotaLimitKeywords.enabled} onToggle={quotaLimitKeywords.toggle} showOnlyConfigured={so}>
                <input type="text" value={quotaLimitKeywords.value} onChange={(e) => quotaLimitKeywords.set(e.target.value)} disabled={!quotaLimitKeywords.enabled} placeholder="hit your limit,usage limit" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>

              <FieldRow label="限额拦截错误码(逗号分隔)" desc="只在这些 HTTP 状态码的响应上扫描限额关键词,避免每个请求都扫描影响速率。默认 429(CPA冷却)+500(meridian/内嵌错误);留空=所有错误响应都扫" enabled={quotaLimitCodes.enabled} onToggle={quotaLimitCodes.toggle} showOnlyConfigured={so}>
                <input type="text" value={quotaLimitCodes.value} onChange={(e) => quotaLimitCodes.set(e.target.value)} disabled={!quotaLimitCodes.enabled} placeholder="429,500" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>
            </div>

            {/* Group: 模型输出上限 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">模型输出上限(max_tokens)</h2>
              <p className="text-xs text-muted/70 -mt-1 mb-1">请求的 max_tokens 超过该模型上限时直接返回 400、不重试不兜底。默认值为官方上限。</p>
              {([
                ['claude-opus-4-8', limitOpus48],
                ['claude-opus-4-7', limitOpus47],
                ['claude-sonnet-4-6', limitSonnet46],
                ['claude-haiku-4-5', limitHaiku45],
              ] as [string, typeof limitOpus48][]).map(([model, f]) => (
                <FieldRow key={model} label={model} desc="" enabled={f.enabled} onToggle={f.toggle} showOnlyConfigured={so}>
                  <NumInput value={f.value} onChange={f.set} disabled={!f.enabled} min={1} step={1000} />
                </FieldRow>
              ))}
            </div>
          </>
        );

      case 'fallback':
        return (
          <>
            {/* Group: 保底触发 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">保底触发（兜底通道触发条件）</h2>
                </div>
                <GroupMaster field={fallbackEnabled} />
              </div>
              <div className={fallbackEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="FallbackPriceThresholdUsd" desc="兜底通道价格阈值（美元/请求）" enabled={fallbackPriceThresholdUsd.enabled} onToggle={fallbackPriceThresholdUsd.toggle} showOnlyConfigured={so}>
                  <NumInput value={fallbackPriceThresholdUsd.value} onChange={fallbackPriceThresholdUsd.set} disabled={!fallbackPriceThresholdUsd.enabled} min={0} step={0.001} />
                </FieldRow>

                <FieldRow label="保底关键词(逗号分隔,命中即走保底)" desc="响应内容命中任一关键词时强制走兜底通道" enabled={fallbackKeywords.enabled} onToggle={fallbackKeywords.toggle} showOnlyConfigured={so}>
                  <input type="text" value={fallbackKeywords.value} onChange={(e) => fallbackKeywords.set(e.target.value)} disabled={!fallbackKeywords.enabled} placeholder="keyword1,keyword2" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
                </FieldRow>

                <FieldRow label="指定模型走保底(逗号分隔,子串匹配)" desc="请求模型名含子串时强制走兜底通道" enabled={fallbackModels.enabled} onToggle={fallbackModels.toggle} showOnlyConfigured={so}>
                  <input type="text" value={fallbackModels.value} onChange={(e) => fallbackModels.set(e.target.value)} disabled={!fallbackModels.enabled} placeholder="claude-3-opus,claude-3-5" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
                </FieldRow>

                <FieldRow label="探活/hi 走保底" desc="探活心跳请求（hi 消息）强制走兜底通道" enabled={fallbackProbeEnabled.enabled} onToggle={fallbackProbeEnabled.toggle} showOnlyConfigured={so}>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input type="checkbox" checked={fallbackProbeEnabled.value} onChange={(e) => fallbackProbeEnabled.set(e.target.checked)} disabled={!fallbackProbeEnabled.enabled} className="accent-accent w-4 h-4" />
                    <span className="text-sm text-ink">{fallbackProbeEnabled.value ? '已启用' : '已禁用'}</span>
                  </label>
                </FieldRow>
              </div>
            </div>

            {/* Group: 故障转移 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">故障转移</h2>

              <FieldRow label="最大故障转移次数(失败换号尝试上限)" desc="单次请求最多尝试换号的次数上限（故障转移 / 重试上限）" enabled={maxFailover.enabled} onToggle={maxFailover.toggle} showOnlyConfigured={so}>
                <NumInput value={maxFailover.value} onChange={maxFailover.set} disabled={!maxFailover.enabled} min={1} />
              </FieldRow>

              <FieldRow label="DirectFallbackStatusCodes" desc="触发直接跳保底的 HTTP 状态码，逗号分隔（例: 400）。命中码 + 关键词 → 停止尝试其余号直接走保底渠道" enabled={directFallbackStatusCodes.enabled} onToggle={directFallbackStatusCodes.toggle} showOnlyConfigured={so}>
                <input type="text" value={directFallbackStatusCodes.value} onChange={(e) => directFallbackStatusCodes.set(e.target.value)} disabled={!directFallbackStatusCodes.enabled} placeholder="400" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>

              <FieldRow label="DirectFallbackKeywords" desc="触发直接跳保底的错误关键词，逗号分隔（例: rate_limit_error）。需同时匹配 DirectFallbackStatusCodes 才生效" enabled={directFallbackKeywords.enabled} onToggle={directFallbackKeywords.toggle} showOnlyConfigured={so}>
                <input type="text" value={directFallbackKeywords.value} onChange={(e) => directFallbackKeywords.set(e.target.value)} disabled={!directFallbackKeywords.enabled} placeholder="rate_limit_error" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>

              <FieldRow label="TerminalErrorKeywords" desc="终止型 400 关键词，逗号分隔（例: invalid_request_error）。命中时立即返回 400 给客户端，不换号不走保底——此类错误每个账户结果相同，重试纯属浪费" enabled={terminalErrorKeywords.enabled} onToggle={terminalErrorKeywords.toggle} showOnlyConfigured={so}>
                <input type="text" value={terminalErrorKeywords.value} onChange={(e) => terminalErrorKeywords.set(e.target.value)} disabled={!terminalErrorKeywords.enabled} placeholder="invalid_request_error" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>

              <FieldRow label="RetryDelayMs" desc="故障转移（换号）间隔毫秒数；0=无等待（默认）" enabled={retryDelayMs.enabled} onToggle={retryDelayMs.toggle} showOnlyConfigured={so}>
                <NumInput value={retryDelayMs.value} onChange={retryDelayMs.set} disabled={!retryDelayMs.enabled} min={0} step={100} />
              </FieldRow>

              <FieldRow label="RetrySameAccountMax" desc="在换号前对同一账户额外重试的次数（0=失败立即换号，默认）" enabled={retrySameAccountMax.enabled} onToggle={retrySameAccountMax.toggle} showOnlyConfigured={so}>
                <NumInput value={retrySameAccountMax.value} onChange={retrySameAccountMax.set} disabled={!retrySameAccountMax.enabled} min={0} />
              </FieldRow>
            </div>

            {/* Group: 弹性伸缩 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">弹性伸缩</h2>
                </div>
                <GroupMaster field={elasticEnabled} />
              </div>
              <div className={elasticEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="ElasticScaleUpUtil" desc="扩容利用率阈值" enabled={elasticScaleUpUtil.enabled} onToggle={elasticScaleUpUtil.toggle} showOnlyConfigured={so}>
                  <NumInput value={elasticScaleUpUtil.value} onChange={elasticScaleUpUtil.set} disabled={!elasticScaleUpUtil.enabled} min={0} max={1} step={0.05} />
                </FieldRow>

                <FieldRow label="缩容利用率阈值(利用率≤此值才释放备用号)" desc="利用率持续低于此阈值时才释放备用号（与扩容阈值形成迟滞区间）" enabled={elasticScaleDownUtil.enabled} onToggle={elasticScaleDownUtil.toggle} showOnlyConfigured={so}>
                  <NumInput value={elasticScaleDownUtil.value} onChange={elasticScaleDownUtil.set} disabled={!elasticScaleDownUtil.enabled} min={0} max={1} step={0.05} />
                </FieldRow>

                <FieldRow label="ElasticMaxReserve" desc="最大备用数" enabled={elasticMaxReserve.enabled} onToggle={elasticMaxReserve.toggle} showOnlyConfigured={so}>
                  <NumInput value={elasticMaxReserve.value} onChange={elasticMaxReserve.set} disabled={!elasticMaxReserve.enabled} min={0} />
                </FieldRow>

                <FieldRow label="默认活跃账户数(打满后才按弹性扩容)" desc="弹性扩容触发前默认保持活跃的账户数量" enabled={elasticBaselineCount.enabled} onToggle={elasticBaselineCount.toggle} showOnlyConfigured={so}>
                  <NumInput value={elasticBaselineCount.value} onChange={elasticBaselineCount.set} disabled={!elasticBaselineCount.enabled} min={1} />
                </FieldRow>
              </div>
            </div>
          </>
        );

      case 'signals':
        return (
          <>
            {/* Group: 封禁信号 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">封禁信号</h2>

              <FieldRow label="BanSignals" desc="触发封禁的 HTTP 状态码，逗号分隔（例: 401,403）" enabled={banSignals.enabled} onToggle={banSignals.toggle} showOnlyConfigured={so}>
                <input type="text" value={banSignals.value} onChange={(e) => banSignals.set(e.target.value)} disabled={!banSignals.enabled} placeholder="401,403" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>

              <FieldRow label="BanKeywords" desc="触发封禁的错误关键词，逗号分隔" enabled={banKeywords.enabled} onToggle={banKeywords.toggle} showOnlyConfigured={so}>
                <input type="text" value={banKeywords.value} onChange={(e) => banKeywords.set(e.target.value)} disabled={!banKeywords.enabled} placeholder="authentication_error,account_disabled" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>
            </div>

            {/* Group: 冷却信号 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">冷却信号</h2>

              <FieldRow label="CooldownSignals" desc="触发账户临时冷却的 HTTP 状态码，逗号分隔（例: 429）。命中后冷却该号，不封禁、自动恢复" enabled={cooldownSignals.enabled} onToggle={cooldownSignals.toggle} showOnlyConfigured={so}>
                <input type="text" value={cooldownSignals.value} onChange={(e) => cooldownSignals.set(e.target.value)} disabled={!cooldownSignals.enabled} placeholder="429" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
              </FieldRow>

              <FieldRow label="CooldownSignalSec" desc="命中 CooldownSignals 后，该账户冷却的秒数" enabled={cooldownSignalSec.enabled} onToggle={cooldownSignalSec.toggle} showOnlyConfigured={so}>
                <NumInput value={cooldownSignalSec.value} onChange={cooldownSignalSec.set} disabled={!cooldownSignalSec.enabled} min={1} />
              </FieldRow>
            </div>

            {/* Group: 封禁 / 恢复 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <h2 className="text-xs font-medium text-muted uppercase tracking-wide py-2">封禁 / 恢复</h2>

              <FieldRow label="BanPersistStreak" desc="连续 N 次封禁信号才标记 auth_valid=false" enabled={banPersistStreak.enabled} onToggle={banPersistStreak.toggle} showOnlyConfigured={so}>
                <NumInput value={banPersistStreak.value} onChange={banPersistStreak.set} disabled={!banPersistStreak.enabled} min={1} />
              </FieldRow>

              <FieldRow label="PermanentBanStreak" desc="连续 N 次封禁信号 → 永久封禁(不再半开恢复，需人工恢复)；0=关闭" enabled={permanentBanStreak.enabled} onToggle={permanentBanStreak.toggle} showOnlyConfigured={so}>
                <NumInput value={permanentBanStreak.value} onChange={permanentBanStreak.set} disabled={!permanentBanStreak.enabled} min={0} />
              </FieldRow>

              <FieldRow label="CooldownBaseMs" desc="指数退避冷却基础值 (ms)" enabled={cooldownBaseMs.enabled} onToggle={cooldownBaseMs.toggle} showOnlyConfigured={so}>
                <NumInput value={cooldownBaseMs.value} onChange={cooldownBaseMs.set} disabled={!cooldownBaseMs.enabled} min={0} step={1000} />
              </FieldRow>

              <FieldRow label="CooldownMaxMs" desc="冷却退避上限 (ms)" enabled={cooldownMaxMs.enabled} onToggle={cooldownMaxMs.toggle} showOnlyConfigured={so}>
                <NumInput value={cooldownMaxMs.value} onChange={cooldownMaxMs.set} disabled={!cooldownMaxMs.enabled} min={0} step={10000} />
              </FieldRow>

              <FieldRow label="CooldownMult" desc="指数退避乘数（例: 2 = 每次翻倍）" enabled={cooldownMult.enabled} onToggle={cooldownMult.toggle} showOnlyConfigured={so}>
                <NumInput value={cooldownMult.value} onChange={cooldownMult.set} disabled={!cooldownMult.enabled} min={1} step={0.5} />
              </FieldRow>
            </div>

            {/* Group: 响应 / 会话放逐 */}
            <div className="bg-surface border border-line rounded-xl px-4 py-2 mb-4">
              <div className="flex items-center justify-between py-2">
                <div>
                  <h2 className="text-xs font-medium text-muted uppercase tracking-wide">响应 / 会话放逐</h2>
                </div>
                <GroupMaster field={responseExileEnabled} />
              </div>
              <div className={responseExileEnabled.value ? '' : 'opacity-40 pointer-events-none'}>
                <FieldRow label="SessionErrorThreshold" desc="会话连错阈值(0=关)" enabled={sessionErrorThreshold.enabled} onToggle={sessionErrorThreshold.toggle} showOnlyConfigured={so}>
                  <NumInput value={sessionErrorThreshold.value} onChange={sessionErrorThreshold.set} disabled={!sessionErrorThreshold.enabled} min={0} />
                </FieldRow>

                <FieldRow label="SessionCooldownSec" desc="会话放逐冷却(秒)" enabled={sessionCooldownSec.enabled} onToggle={sessionCooldownSec.toggle} showOnlyConfigured={so}>
                  <NumInput value={sessionCooldownSec.value} onChange={sessionCooldownSec.set} disabled={!sessionCooldownSec.enabled} min={0} />
                </FieldRow>

                <FieldRow label="拒答关键词(逗号分隔)" desc="拒答关键词(逗号分隔)" enabled={responseExileKeywords.enabled} onToggle={responseExileKeywords.toggle} showOnlyConfigured={so}>
                  <input type="text" value={responseExileKeywords.value} onChange={(e) => responseExileKeywords.set(e.target.value)} disabled={!responseExileKeywords.enabled} placeholder="keyword1,keyword2" className="w-full bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition disabled:cursor-not-allowed" />
                </FieldRow>
              </div>
            </div>
          </>
        );

      default:
        return null;
    }
  }

  return (
    <div className="flex flex-col h-full">
      {/* ================================================================
          Sticky top bar: scope selector + filter + action buttons
          ================================================================ */}
      <div className="sticky top-0 z-10 bg-bg border-b border-line px-4 md:px-6 py-3 space-y-2">
        {/* Row 1: title + actions */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <div>
            <h1 className="text-xl font-semibold text-ink">封控策略</h1>
            <p className="text-xs text-muted mt-0.5">
              {isSuperadmin
                ? '勾选字段并输入值，未勾选的字段将保持默认。'
                : '只读 — 仅 superadmin 可修改策略。'}
            </p>
          </div>
          {isSuperadmin && (
            <div className="flex gap-2 shrink-0 flex-wrap justify-end items-center">
              {/* 只看已配置 toggle */}
              <label className="flex items-center gap-1.5 cursor-pointer select-none text-sm text-muted">
                <input
                  type="checkbox"
                  checked={onlyConfigured}
                  onChange={(e) => setOnlyConfigured(e.target.checked)}
                  className="accent-accent w-4 h-4"
                />
                只看已配置
              </label>
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

        {/* Row 2: scope selector */}
        {isSuperadmin && (
          <div className="flex flex-col sm:flex-row sm:items-center gap-3 bg-surface border border-line rounded-xl px-4 py-2">
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
      </div>

      {/* ================================================================
          Main body: left-rail nav + right content pane
          ================================================================ */}
      <div className="flex flex-col md:flex-row flex-1 min-h-0 px-4 md:px-6 pt-4 pb-6 gap-4">

        {/* Left rail */}
        <nav className="md:w-48 shrink-0 flex flex-row md:flex-col gap-1 overflow-x-auto md:overflow-x-visible">
          {CATEGORIES.map((c) => {
            const count = catCount(c.id);
            const active = cat === c.id;
            return (
              <button
                key={c.id}
                onClick={() => setCat(c.id)}
                className={`flex items-center justify-between gap-2 px-3 py-2 rounded-lg text-sm font-medium whitespace-nowrap transition text-left
                  ${active
                    ? 'bg-accent text-white'
                    : 'text-muted hover:bg-surface hover:text-ink border border-transparent hover:border-line'
                  }`}
              >
                <span>{c.label}</span>
                {count > 0 && (
                  <span className={`text-xs px-1.5 py-0.5 rounded-full font-semibold ${active ? 'bg-white/25 text-white' : 'bg-accent/15 text-accent'}`}>
                    {count}
                  </span>
                )}
              </button>
            );
          })}
        </nav>

        {/* Right content pane */}
        <fieldset disabled={!isSuperadmin} className="flex-1 min-w-0">
          {CatContent()}
        </fieldset>
      </div>

      {/* Dry-run result */}
      {dryRunResult && (
        <div className="px-4 md:px-6 pb-6 space-y-3">
          <h2 className="text-sm font-semibold text-ink">预览差异</h2>
          <DiffTable result={dryRunResult} />
        </div>
      )}
    </div>
  );
}
