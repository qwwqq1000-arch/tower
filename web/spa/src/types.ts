// ============================================================
// Tower SPA — shared TypeScript types
// ============================================================

export interface User {
  sub: string;
  role: 'admin' | 'viewer';
  perms: string[];
}

// --- Nodes ---
export interface NodeRecord {
  id: string;
  host: string;
  port: number;
  auth_valid: boolean;
  server_state: string;
  created_at: string;
  updated_at: string;
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
export interface DashboardStats {
  nodes_total: number;
  nodes_healthy: number;
  keys_total: number;
  requests_today: number;
  errors_today: number;
  latency_p99_ms: number;
}

// --- Provision ---
export interface ProvisionRequest {
  host: string;
  port?: number;
  key_id?: string;
}

export interface ProvisionJob {
  id: string;
  host: string;
  status: 'pending' | 'running' | 'done' | 'failed';
  log: string;
  created_at: string;
  updated_at: string;
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
  CooldownBaseMs?: number;
  CooldownMaxMs?: number;
  CooldownMult?: number;
  AffinityTTLSec?: number;
  FallbackEnabled?: boolean;
  FallbackPriceThresholdUsd?: number;
  BanSignals?: number[];
  BanKeywords?: string[];
}

// policy.Config (resolved)
export interface PolicyConfig {
  MaxConcurrent: number;
  SlotCooldownMinMs: number;
  SlotCooldownMaxMs: number;
  BanPersistStreak: number;
  CooldownBaseMs: number;
  CooldownMaxMs: number;
  CooldownMult: number;
  AffinityTTLSec: number;
  FallbackEnabled: boolean;
  FallbackPriceThresholdUsd: number;
  BanSignals: number[];
  BanKeywords: string[];
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
  tokensIn: number;
  tokensOut: number;
  fallbackReason: string;
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
