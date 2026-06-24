// ============================================================
// Tower SPA — shared TypeScript types
// ============================================================

export interface User {
  sub: string;
  role: string; // superadmin | admin | operator | tenant | viewer
  perms: string[];
  mustChangePw?: boolean;
}

// --- Nodes ---
export interface NodeRecord {
  id: string;
  name: string;
  baseUrl: string;
  ownerId: string;
  enabled: boolean;
  kind?: string; // "meridian" | "cpa"
  version?: string;
  status?: string;
  createdAt?: number;
  loggedIn?: boolean;
  email?: string;
  liveVersion?: string;
}

// --- Keys (号库) ---
export interface KeyRecord {
  id: string;
  key: string;
  label?: string;
  created_at: string;
}

// --- Dispatch Keys (调度密钥) ---
export interface DispatchKeyRecord {
  id: string;
  prefix: string;
  label: string;
  ownerId: string;
  enabled: boolean;
}

export interface DispatchKeyCreated {
  id: string;
  key: string; // plaintext, shown once
}

// --- Dashboard ---
/** @deprecated use DashboardData */
export interface DashboardResponse {
  nodes: NodeRecord[];
}

export interface DashboardNodeItem {
  id: string;
  name: string;
  baseUrl: string;
  enabled: boolean;
  status: string;
  version: string;
  region: string;
}

export interface DashboardByModel {
  model: string;
  requests: number;
  tokensIn: number;
  tokensOut: number;
  costUsd: number;
}

export interface DashboardHostingRow {
  tenantId: string;
  username: string;
  role: string;
  consumptionUsd: number;
  rate: number;
  feeUsd: number;
  unsettledUsd: number;
}

export interface DashboardData {
  nodes: {
    total: number;
    enabled: number;
    byStatus: Record<string, number>;
    list: DashboardNodeItem[];
  };
  accounts: {
    total: number;
  };
  today: {
    requests: number;
    ok: number;
    successRate: number;
    tokensIn: number;
    tokensOut: number;
    costUsd: number;
    byModel: DashboardByModel[];
  };
  hosting: DashboardHostingRow[];
  totalCostUsd: number;
  quota5hAvg?: number;
  quota7dAvg?: number;
}

// --- Provision ---
export interface ProvisionRequest {
  host: string;
  user?: string;
  password: string;
  ownerId?: string;
}

export interface ProvisionJob {
  id: string;
  host: string;
  name: string;
  status: 'pending' | 'running' | 'success' | 'failed';
  step: string;
  log: string;
}

// --- Policies (封控策略) ---
export interface Policy {
  scopeType: string;
  scopeId: string;
  params: Record<string, unknown>;
}

// policy.Patch fields (all optional, pointer-like)
export interface PolicyPatch {
  MaxConcurrent?: number;
  SlotCooldownMinMs?: number;
  SlotCooldownMaxMs?: number;
  BanPersistStreak?: number;
  PermanentBanStreak?: number;
  CooldownBaseMs?: number;
  CooldownMaxMs?: number;
  CooldownMult?: number;
  AffinityTTLSec?: number;
  FallbackEnabled?: boolean;
  FallbackPriceThresholdUsd?: number;
  FallbackKeywords?: string[];
  FallbackModels?: string[];
  FallbackProbeEnabled?: boolean;
  BanSignals?: number[];
  CooldownSignals?: number[];
  CooldownSignalSec?: number;
  BanKeywords?: string[];
  MaxFailover?: number;
  ModelMaxTokens?: Record<string, number>; // per-model output (max_tokens) ceiling; over-limit → 400 no retry
  WarmupHours?: number;
  WarmupMaxConcurrent?: number;
  WarmupBlockOpus?: boolean;
  SessionErrorThreshold?: number;
  SessionCooldownSec?: number;
  ResponseExileEnabled?: boolean;
  ResponseExileKeywords?: string[];
  QuotaLimitKeywords?: string[];
  QuotaLimitStatusCodes?: number[];
  ElasticEnabled?: boolean;
  ElasticScaleUpUtil?: number;
  ElasticScaleDownUtil?: number;
  ElasticMaxReserve?: number;
  ElasticBaselineCount?: number;
}

// policy.Config (resolved)
export interface PolicyConfig {
  MaxConcurrent: number;
  SlotCooldownMinMs: number;
  SlotCooldownMaxMs: number;
  BanPersistStreak: number;
  PermanentBanStreak: number;
  CooldownBaseMs: number;
  CooldownMaxMs: number;
  CooldownMult: number;
  AffinityTTLSec: number;
  FallbackEnabled: boolean;
  FallbackPriceThresholdUsd: number;
  BanSignals: number[];
  BanKeywords: string[];
  CooldownSignals: number[];
  CooldownSignalSec: number;
}

export interface PolicyDiff {
  Field: string;
  From: string;
  To: string;
}

export interface PolicyDryRunResult {
  final: PolicyConfig;
  diffs: PolicyDiff[];
}

// --- Desired config ---
// The backend stores raw JSON; we treat it as an opaque Record.
export type DesiredConfig = Record<string, unknown>;

// --- Logs ---
export interface LogEntry {
  ts: number;         // unix ms
  model: string;
  target: string;
  status: string;
  httpStatus: number;
  latencyMs: number;
  ttfbMs?: number;
  tokensIn: number;
  tokensOut: number;
  fallbackReason: string;
  stream?: boolean;
  costUsd?: number;
  requestId?: string; // links to stored request detail (body+headers) for the 点开 view
}

// --- Events ---
export interface EventRecord {
  ts: number;         // unix ms
  type: string;
  target: string;
  detail?: Record<string, unknown>;
}

// --- Audit ---
export interface AuditRecord {
  ts: number;         // unix ms
  actor: string;
  action: string;
  target: string;
}

// --- Settle / Ledger ---
export interface SettleRequest {
  tenantId: string;
  periodStart?: number;
  periodEnd?: number;
}

export interface SettleResult {
  id: string;
  tenantId: string;
  gross: number;
  settled?: number;
  status: string;
}

export interface LedgerEntry {
  ts: number;         // unix ms
  type: string;
  amount: number;
  ref: string;
  note: string;
}

// --- Pagination wrapper (kept for future use) ---
export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

// --- Accounts (号库) ---
export interface AccountRow {
  nodeId: string;
  nodeName: string;
  baseUrl: string;
  accountId: string;
  profileId: string;
  enabled: boolean;
  weight: number;
  role: string;
  egress: string;
  email: string;
  status?: string;
  limitedUntil?: number;     // quota-limit reset deadline (unix ms) when status==='limited'
  slotId?: string;
  todayCostUsd?: number;
  totalCostUsd?: number;
  expiresAt?: number;        // unix ms
  ownerId?: string;          // tenant owner id ("" = shared)
  subscriptionType?: string; // e.g. "claude_max_5x"
  cpaQuota?: CpaQuota | null; // CPA account rate-limit usage (5h/7d/7d-sonnet)
}

export interface CpaQuota {
  fiveHourUtil: number;
  fiveHourResetsAt: string;
  sevenDayUtil: number;
  sevenDayResetsAt: string;
  sevenDaySonnetUtil: number;
  sevenDaySonnetResetsAt: string;
  updatedAt: number;
}

// --- Node Quota ---
export interface QuotaWindow {
  type: string;        // e.g. "five_hour" | "seven_day"
  status: string;
  utilization: number;
  resetsAt?: number;   // unix ms — limit recovery time
}

export interface QuotaProfile {
  id: string;
  isActive: boolean;
  windows: QuotaWindow[];
}

export interface QuotaAll {
  profiles: QuotaProfile[];
  activeProfile: string;
}

export interface NodeProfile {
  id: string;
  email?: string;
  name?: string;
  loggedIn?: boolean;
  subscriptionType?: string;
  type?: string;
}

export interface OAuthStartResult {
  authorizeUrl: string;
  codeVerifier: string;
  state: string;
}

export interface OAuthExchangeResult {
  accountId: string;
}

// --- Fallback Channels (保底渠道) ---
export interface FallbackChannel {
  id: string;
  name: string;
  baseUrl: string;
  hasKey: boolean;
  priority: number;
  weight: number;
  maxConcurrent: number;
  cooldownMs: number;
  priceThreshold: number;
  modelAllowlist: string;
  enabled: boolean;
  ownerId: string;
  todayRequests?: number;
  todayCostUsd?: number;
  totalRequests?: number;
  totalCostUsd?: number;
  balanceUsd?: number;
  balanceAlertUsd?: number;
  hasBalanceToken?: boolean;
  balanceUserId?: string;
  balanceCheckedAt?: number;
  balanceError?: string;
}

// --- Users ---
export interface UserRow {
  id: string;
  username: string;
  role: string;
  rate: number;
  channelRate?: number;
  fallbackLimit?: number;
}

// --- Node Telemetry (遥测 / 健康) ---
export interface NodeHealth {
  version: string;
  loggedIn: boolean;
  email: string;
  subscriptionType: string;
  mode: string;
}

export interface NodeTelemetryPercentiles {
  p50: number;
  p95: number;
}

export interface NodeTelemetryModelStat {
  count: number;
  avgTotalMs: number;
}

export interface NodeTelemetryTokenUsage {
  totalInputTokens: number;
  totalOutputTokens: number;
  totalCacheReadTokens: number;
  totalCacheCreationTokens: number;
  avgCacheHitRate: number;
}

export interface NodeTelemetryStats {
  windowMs: number;
  totalRequests: number;
  errorCount: number;
  requestsPerMinute: number;
  ttfb: NodeTelemetryPercentiles;
  totalDuration: NodeTelemetryPercentiles;
  byModel: Record<string, NodeTelemetryModelStat>;
  tokenUsage: NodeTelemetryTokenUsage;
}

export interface NodeTelemetry {
  health: NodeHealth;
  telemetry: NodeTelemetryStats | null;
}

// --- Dispatch Status ---
export interface DispatchAccountSnapshot {
  key: string;
  label?: string;
  status: string; // active | banned(封控·冷却) | half_open | permanent | offline | disabled
  inflight: number;
  available: number;
  recoverAt?: number; // ms; when a cooling breaker half-opens (0 if not cooling)
  limitedUntil?: number; // ms; quota-limit reset deadline when status==='limited'
  todayCostUsd?: number;
  totalCostUsd?: number;
}

export interface DispatchTraffic {
  total: number;
  rpm?: number;
  ok: number;
  error: number;
  tokensIn: number;
  tokensOut: number;
}

export interface DispatchEvent {
  ts: number;
  type: string;
  target: string;
  detail?: any;
}

export interface DispatchFallbackChannel {
  id: string;
  name: string;
  enabled: boolean;
  priority: number;
  weight: number;
  todayRequests: number;
  todayCostUsd: number;
  inflight?: number;
  available?: number;
  balanceUsd?: number;
}

export interface DispatchStatus {
  accounts: DispatchAccountSnapshot[];
  traffic: DispatchTraffic;
  events: DispatchEvent[];
  nodes: { total: number; enabled: number };
  asOf: number;
  fallbackChannels?: DispatchFallbackChannel[];
  quota5hAvg?: number;
  quota7dAvg?: number;
}

// --- Slots (时段槽位) ---
export interface Slot {
  id: string;
  name: string;
  startMin: number;
  endMin: number;
  enabled: boolean;
}

// --- Tenant mode (/api/me/*) ---
export interface MeAccountRow {
  nodeName: string;
  accountId: string;
  profileId: string;
  email: string;
  enabled: boolean;
  status?: string;           // live breaker status: active|banned|half_open|permanent|cooldown|limited
  limitedUntil?: number;     // quota-limit reset deadline (unix ms) when status==='limited'
  weight: number;
  role: string;
  expiresAt?: number;        // unix ms
  subscriptionType?: string;
  todayCostUsd?: number;
  totalCostUsd?: number;
}

export interface MeDashboard {
  accounts: { total: number; active: number };
  today: { requests: number; costUsd: number };
  consumptionUsd: number;
  hostingRate: number;
  unsettledUsd: number;
  accumulatedUsd: number;
  channelConsumptionUsd?: number;
  channelRate?: number;
  channelHostingFeeUsd?: number;
}

export interface ServerStatus {
  uptimeSec: number;
  goroutines: number;
  memAllocMB: number;
  memSysMB: number;
  numGC: number;
  goVersion: string;
  numCPU: number;
  startedAt: string;
  diskTotalGB?: number;
  diskUsedGB?: number;
  diskUsedPct?: number;
  netRxMBps?: number;
  netTxMBps?: number;
  netRxTotalMB?: number;
  netTxTotalMB?: number;
}
