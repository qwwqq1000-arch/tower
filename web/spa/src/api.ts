// ============================================================
// Tower SPA — typed fetch wrapper + endpoint helpers
// ============================================================
import type {
  User,
  NodeRecord,
  DispatchKeyRecord,
  DispatchKeyCreated,
  DashboardResponse,
  ProvisionRequest,
  ProvisionJob,
  Policy,
  PolicyPatch,
  PolicyDryRunResult,
  DesiredConfig,
  LogEntry,
  EventRecord,
  AuditRecord,
  SettleRequest,
  SettleResult,
  LedgerEntry,
  AccountRow,
  NodeProfile,
  OAuthStartResult,
  OAuthExchangeResult,
  DispatchStatus,
} from './types';

// ------------------------------------------------------------------
// Core wrapper
// ------------------------------------------------------------------
export async function api<T = unknown>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401) throw new Error('unauthorized');
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
  const ct = res.headers.get('content-type') ?? '';
  return ct.includes('application/json') ? (res.json() as Promise<T>) : (undefined as unknown as T);
}

// ------------------------------------------------------------------
// Auth
// ------------------------------------------------------------------
export const login = (username: string, password: string) =>
  api<{ role: string }>('POST', '/auth/login', { username, password });

export const logout = () => api<void>('POST', '/auth/logout');

export const me = () => api<User>('GET', '/auth/me');

// ------------------------------------------------------------------
// Dashboard
// ------------------------------------------------------------------
export const getDashboard = () =>
  api<DashboardResponse>('GET', '/api/dashboard');

// ------------------------------------------------------------------
// Nodes
// ------------------------------------------------------------------
export const listNodes = () =>
  api<NodeRecord[]>('GET', '/api/admin/nodes');

export const createNode = (data: { name: string; baseUrl: string; apiKey?: string; ownerId?: string }) =>
  api<NodeRecord>('POST', '/api/admin/nodes', data);

export const deleteNode = (id: string) =>
  api<void>('DELETE', `/api/admin/nodes/${id}`);

// ------------------------------------------------------------------
// Dispatch Keys (调度密钥)
// ------------------------------------------------------------------
export const listDispatchKeys = () =>
  api<DispatchKeyRecord[]>('GET', '/api/admin/dispatch-keys');

export const createDispatchKey = (data: { label?: string }) =>
  api<DispatchKeyCreated>('POST', '/api/admin/dispatch-keys', data);

export const disableDispatchKey = (id: string) =>
  api<void>('DELETE', `/api/admin/dispatch-keys/${id}`);

// ------------------------------------------------------------------
// Provision
// ------------------------------------------------------------------
export const startProvision = (req: ProvisionRequest) =>
  api<ProvisionJob>('POST', '/api/admin/provision', req);

export const getProvision = (jobId: string) =>
  api<ProvisionJob>('GET', `/api/admin/provision/${jobId}`);

// ------------------------------------------------------------------
// Policies (封控策略)
// ------------------------------------------------------------------
export const listPolicies = () =>
  api<Policy[]>('GET', '/api/admin/policies');

export const putGlobalPolicy = (data: PolicyPatch) =>
  api<{ ok: string }>('PUT', '/api/admin/policies/global', data);

export const dryRunPolicy = (data: PolicyPatch) =>
  api<PolicyDryRunResult>('POST', '/api/admin/policies/dry-run', data);

// ------------------------------------------------------------------
// Desired config (配置对账)
// ------------------------------------------------------------------
export const getDesired = () =>
  api<DesiredConfig>('GET', '/api/admin/desired');

export const putDesired = (data: DesiredConfig) =>
  api<{ ok: string }>('PUT', '/api/admin/desired', data);

// ------------------------------------------------------------------
// Logs
// ------------------------------------------------------------------
export const getLogs = (params?: Record<string, string>) => {
  const qs = params ? '?' + new URLSearchParams(params).toString() : '';
  return api<LogEntry[]>('GET', `/api/admin/logs${qs}`);
};

// ------------------------------------------------------------------
// Events
// ------------------------------------------------------------------
export const getEvents = (params?: Record<string, string>) => {
  const qs = params ? '?' + new URLSearchParams(params).toString() : '';
  return api<EventRecord[]>('GET', `/api/admin/events${qs}`);
};

// ------------------------------------------------------------------
// Audit
// ------------------------------------------------------------------
export const getAudit = (params?: Record<string, string>) => {
  const qs = params ? '?' + new URLSearchParams(params).toString() : '';
  return api<AuditRecord[]>('GET', `/api/admin/audit${qs}`);
};

// ------------------------------------------------------------------
// Settle / Ledger (计费)
// ------------------------------------------------------------------
export const settle = (req: SettleRequest) =>
  api<SettleResult>('POST', '/api/admin/settle', req);

export const getLedger = (tenantId: string) => {
  const qs = '?' + new URLSearchParams({ tenantId }).toString();
  return api<LedgerEntry[]>('GET', `/api/admin/ledger${qs}`);
};

// ------------------------------------------------------------------
// Accounts (号库)
// ------------------------------------------------------------------
export const listAccounts = () =>
  api<AccountRow[]>('GET', '/api/admin/accounts');

export const unassignAccount = (nodeId: string, accountId: string) =>
  api<void>('DELETE', `/api/admin/accounts/${nodeId}/${accountId}`);

export const listNodeProfiles = (nodeId: string) =>
  api<NodeProfile[]>('GET', `/api/admin/nodes/${nodeId}/profiles`);

export const oauthStart = (nodeId: string) =>
  api<OAuthStartResult>('POST', `/api/admin/nodes/${nodeId}/oauth/start`);

export const oauthExchange = (
  nodeId: string,
  payload: { codeVerifier: string; state: string; code: string },
) => api<OAuthExchangeResult>('POST', `/api/admin/nodes/${nodeId}/oauth/exchange`, payload);

// ------------------------------------------------------------------
// Dispatch status (调度状态)
// ------------------------------------------------------------------
export const getDispatchStatus = () =>
  api<DispatchStatus>('GET', '/api/admin/dispatch/status');
