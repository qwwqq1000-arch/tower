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
  id: string;
  name: string;
  rules: Record<string, unknown>;
  enabled: boolean;
  updated_at: string;
}

export interface PolicyDryRunResult {
  affected_nodes: string[];
  summary: string;
}

// --- Desired config ---
export interface DesiredConfig {
  version: number;
  spec: Record<string, unknown>;
}

// --- Logs ---
export interface LogEntry {
  ts: string;
  level: string;
  msg: string;
  node_id?: string;
  [key: string]: unknown;
}

// --- Events ---
export interface EventRecord {
  id: string;
  type: string;
  payload: Record<string, unknown>;
  ts: string;
}

// --- Audit ---
export interface AuditRecord {
  id: string;
  actor: string;
  action: string;
  resource: string;
  ts: string;
  detail?: Record<string, unknown>;
}

// --- Settle / Ledger ---
export interface SettleResult {
  settled: number;
  skipped: number;
}

export interface LedgerEntry {
  id: string;
  node_id: string;
  tokens: number;
  cost_usd: number;
  ts: string;
}

// --- Pagination wrapper ---
export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}
