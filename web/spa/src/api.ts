// ============================================================
// Tower SPA — typed fetch wrapper + endpoint helpers
// ============================================================
import type {
  User,
  NodeRecord,
  DispatchKeyRecord,
  DispatchKeyCreated,
  DashboardData,
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
  FallbackChannel,
  UserRow,
  NodeTelemetry,
  QuotaAll,
  ServerStatus,
  Slot,
  MeAccountRow,
  MeDashboard,
} from './types';

// ------------------------------------------------------------------
// Core wrapper
// ------------------------------------------------------------------
export async function api<T = unknown>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  // CSRF guard: the server requires X-Requested-With: tower on all cookie-auth
  // mutations (any method other than GET/HEAD). Cross-origin attackers cannot
  // set custom headers, so this header acts as a lightweight CSRF token.
  const isMutation = method !== 'GET' && method !== 'HEAD';
  const csrfHeader: Record<string, string> = isMutation ? { 'X-Requested-With': 'tower' } : {};
  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers: body
      ? { 'Content-Type': 'application/json', ...csrfHeader }
      : { ...csrfHeader },
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
  api<DashboardData>('GET', '/api/dashboard');

// ------------------------------------------------------------------
// Nodes
// ------------------------------------------------------------------
export const listNodes = () =>
  api<NodeRecord[]>('GET', '/api/admin/nodes');

export const createNode = (data: { baseUrl: string; apiKey?: string; ownerId?: string; accountOwnerId?: string; kind?: string; mgmtKey?: string; passthrough?: boolean }) =>
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
  api<{ jobId: string }>('POST', '/api/admin/provision', req);

export const getProvision = (jobId: string) =>
  api<ProvisionJob>('GET', `/api/admin/provision/${jobId}`);

// ------------------------------------------------------------------
// Node Account (per-account tuning)
// ------------------------------------------------------------------
export const updateNodeAccount = (
  nodeId: string,
  accountId: string,
  body: { egress?: string; weight?: number; role?: string; enabled?: boolean; slotId?: string },
) => api<{ ok: string }>('PATCH', `/api/admin/accounts/${nodeId}/${accountId}`, body);

// ------------------------------------------------------------------
// Policies (封控策略)
// ------------------------------------------------------------------
export const listPolicies = () =>
  api<Policy[]>('GET', '/api/admin/policies');

export const putGlobalPolicy = (data: PolicyPatch) =>
  api<{ ok: string }>('PUT', '/api/admin/policies/global', data);

export const putTenantPolicy = (id: string, data: PolicyPatch) =>
  api<{ ok: string }>('PUT', `/api/admin/policies/tenant/${id}`, data);

export const deleteTenantPolicy = (id: string) =>
  api<{ ok: string }>('DELETE', `/api/admin/policies/tenant/${id}`);

export const putAccountPolicy = (accountId: string, data: PolicyPatch) =>
  api<{ ok: string }>('PUT', `/api/admin/policies/account/${accountId}`, data);

export const deleteAccountPolicy = (accountId: string) =>
  api<{ ok: string }>('DELETE', `/api/admin/policies/account/${accountId}`);

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

export interface LogDetail { requestId: string; ts: number; reqBody: string; reqHeaders: string; respStatus?: number; respBody?: string }
export const getLogDetail = (requestId: string) =>
  api<LogDetail>('GET', `/api/admin/logs/detail?requestId=${encodeURIComponent(requestId)}`);
export const getMeLogDetail = (requestId: string) =>
  api<LogDetail>('GET', `/api/me/logs/detail?requestId=${encodeURIComponent(requestId)}`);

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

export const setAccountExpiry = (accountId: string, expiresAt: number) =>
  api<{ ok: string }>('PATCH', `/api/admin/accounts/${accountId}/expiry`, { expiresAt });

export const setAccountOwner = (accountId: string, ownerId: string) =>
  api<{ ok: string }>('PATCH', `/api/admin/accounts/${accountId}/owner`, { ownerId });

export const recoverAccount = (accountId: string) =>
  api<{ ok: boolean; accountId: string }>('POST', `/api/admin/accounts/${accountId}/recover`, {});

export const clearNo1M = (accountId: string) =>
  api<{ ok: string }>('POST', `/api/admin/accounts/${accountId}/clear-no1m`, {});

// Quota refresh (manual, on-demand — auto-polling is off). All CPA accounts, or one.
export const refreshAllQuota = () =>
  api<{ refreshed: number }>('POST', '/api/admin/accounts/refresh-quota', {});

export const refreshAccountQuota = (accountId: string) =>
  api<{ refreshed: number }>('POST', `/api/admin/accounts/${accountId}/refresh-quota`, {});

export const listNodeProfiles = (nodeId: string) =>
  api<NodeProfile[]>('GET', `/api/admin/nodes/${nodeId}/profiles`);

export const importNodeProfile = (nodeId: string, profileId: string) =>
  api<{ ok: boolean; profileId: string; email?: string; reused?: boolean }>(
    'POST',
    `/api/admin/nodes/${nodeId}/accounts/import`,
    { profileId },
  );

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

// ------------------------------------------------------------------
// Node features (SDK 设置)
// ------------------------------------------------------------------
export const getNodeFeatures = (id: string) =>
  api<Record<string, Record<string, unknown>>>('GET', `/api/admin/nodes/${id}/features`);

export const patchNodeFeatures = (id: string, adapter: string, patch: Record<string, unknown>) =>
  api<void>('PATCH', `/api/admin/nodes/${id}/features/${adapter}`, patch);

export const refreshNode = (id: string) =>
  api<void>('POST', `/api/admin/nodes/${id}/refresh`);

export const getNodeConsoleUrl = (id: string) =>
  api<{ url: string }>('GET', `/api/admin/nodes/${id}/console-url`);

export const setNodeEnabled = (id: string, enabled: boolean) =>
  api<void>('PATCH', `/api/admin/nodes/${id}/enabled`, { enabled });

export const setNodePassthrough = (id: string, passthrough: boolean) =>
  api<void>('PATCH', `/api/admin/nodes/${id}/passthrough`, { passthrough });

// ------------------------------------------------------------------
// Fallback Channels (保底渠道)
// ------------------------------------------------------------------
export const listFallbackChannels = () =>
  api<FallbackChannel[]>('GET', '/api/admin/fallback-channels');

export const createFallbackChannel = (body: {
  name: string;
  baseUrl: string;
  apiKey?: string;
  priority?: number;
  weight?: number;
  maxConcurrent?: number;
  cooldownMs?: number;
  priceThreshold?: number;
  modelAllowlist?: string;
  balanceToken?: string;
  balanceUserId?: string;
  balanceAlertUsd?: number;
  spendCapDailyMinUsd?: number;
  spendCapDailyMaxUsd?: number;
  spendCapTotalMinUsd?: number;
  spendCapTotalMaxUsd?: number;
  spendCapAction?: string;
}) => api<FallbackChannel>('POST', '/api/admin/fallback-channels', body);

export const updateFallbackChannel = (id: string, body: Partial<{
  name: string;
  baseUrl: string;
  apiKey: string;
  priority: number;
  weight: number;
  maxConcurrent: number;
  cooldownMs: number;
  priceThreshold: number;
  modelAllowlist: string;
  balanceToken: string;
  balanceUserId: string;
  balanceAlertUsd: number;
  spendCapDailyMinUsd: number;
  spendCapDailyMaxUsd: number;
  spendCapTotalMinUsd: number;
  spendCapTotalMaxUsd: number;
  spendCapAction: string;
}>) => api<FallbackChannel>('PATCH', `/api/admin/fallback-channels/${id}`, body);

export const refreshFallbackBalance = (id: string) =>
  api<{ balanceUsd: number; error?: string }>('POST', `/api/admin/fallback-channels/${id}/balance`);

export const setFallbackEnabled = (id: string, enabled: boolean) =>
  api<void>('PATCH', `/api/admin/fallback-channels/${id}/enabled`, { enabled });

export const deleteFallbackChannel = (id: string) =>
  api<void>('DELETE', `/api/admin/fallback-channels/${id}`);

// ------------------------------------------------------------------
// Users (用户管理)
// ------------------------------------------------------------------
export const listUsers = () =>
  api<UserRow[]>('GET', '/api/admin/users');

export const createUser = (body: { username: string; password: string; role: string }) =>
  api<{ id: string }>('POST', '/api/admin/users', body);

export const deleteUser = (id: string) =>
  api<void>('DELETE', `/api/admin/users/${id}`);

export const setUserRole = (id: string, role: string) =>
  api<{ ok: string }>('PATCH', `/api/admin/users/${id}/role`, { role });

export const setUserHostingRate = (id: string, rate: number) =>
  api<{ ok: string }>('PATCH', `/api/admin/users/${id}/hosting-rate`, { rate });

export const setUserChannelRate = (id: string, rate: number) =>
  api<{ ok: string }>('PATCH', `/api/admin/users/${id}/channel-rate`, { rate });

export const setUserFallbackLimit = (id: string, limit: number) =>
  api<{ ok: string }>('PATCH', `/api/admin/users/${id}/fallback-limit`, { limit });

export const changePassword = (body: { oldPassword: string; newPassword: string }) =>
  api<{ ok: string }>('POST', '/auth/change-password', body);

// ------------------------------------------------------------------
// Node Telemetry (遥测 / 健康)
// ------------------------------------------------------------------
export const getNodeTelemetry = (id: string) =>
  api<NodeTelemetry>('GET', `/api/admin/nodes/${id}/telemetry`);

// ------------------------------------------------------------------
// Node Quota (限额)
// ------------------------------------------------------------------
export const getNodeQuota = (id: string) =>
  api<QuotaAll>('GET', `/api/admin/nodes/${id}/quota`);

// ------------------------------------------------------------------
// Ban analysis (封号分析)
// ------------------------------------------------------------------
export interface BanBucket { bucket: number; count: number }
export interface BanAccountEntry { email: string; count: number }
export interface BanAnalysis {
  total: number;
  byWeekday: BanBucket[];
  byHour: BanBucket[];
  byAccount?: BanAccountEntry[];
}
export const getBanAnalysis = () =>
  api<BanAnalysis>('GET', '/api/admin/ban-analysis');

// ------------------------------------------------------------------
// Server status (服务器状态)
// ------------------------------------------------------------------
export const getServerStatus = () =>
  api<ServerStatus>('GET', '/api/admin/server-status');

// ------------------------------------------------------------------
// Slots (时段槽位)
// ------------------------------------------------------------------
export const listSlots = () =>
  api<Slot[]>('GET', '/api/admin/slots');

export const createSlot = (body: { name: string; startMin: number; endMin: number }) =>
  api<{ id: string }>('POST', '/api/admin/slots', body);

export const deleteSlot = (id: string) =>
  api<void>('DELETE', `/api/admin/slots/${id}`);

export const setSlotEnabled = (id: string, enabled: boolean) =>
  api<void>('PATCH', `/api/admin/slots/${id}/enabled`, { enabled });

// ------------------------------------------------------------------
// Tenant self-service (/api/me/*) — owner-scoped to the caller
// ------------------------------------------------------------------
export const getMeAccounts = () =>
  api<MeAccountRow[]>('GET', '/api/me/accounts');

export const pauseMeAccount = (accountId: string, enabled: boolean) =>
  api<{ ok: string }>('POST', `/api/me/accounts/${accountId}/pause`, { enabled });

export const getMeDashboard = () =>
  api<MeDashboard>('GET', '/api/me/dashboard');

export const getMeLogs = (limit = 200) =>
  api<LogEntry[]>('GET', `/api/me/logs?limit=${limit}`);

export const getMeEvents = (limit = 200) =>
  api<EventRecord[]>('GET', `/api/me/events?limit=${limit}`);

export const getMeLedger = () =>
  api<LedgerEntry[]>('GET', '/api/me/ledger');

export const listMeFallback = () =>
  api<FallbackChannel[]>('GET', '/api/me/fallback-channels');

export const createMeFallback = (body: {
  name: string;
  baseUrl: string;
  apiKey?: string;
  priority?: number;
  weight?: number;
  maxConcurrent?: number;
  cooldownMs?: number;
  priceThreshold?: number;
  modelAllowlist?: string;
  balanceToken?: string;
  balanceUserId?: string;
  balanceAlertUsd?: number;
  spendCapDailyMinUsd?: number;
  spendCapDailyMaxUsd?: number;
  spendCapTotalMinUsd?: number;
  spendCapTotalMaxUsd?: number;
  spendCapAction?: string;
}) => api<FallbackChannel>('POST', '/api/me/fallback-channels', body);

export const updateMeFallback = (id: string, body: Partial<{
  name: string;
  baseUrl: string;
  apiKey: string;
  priority: number;
  weight: number;
  maxConcurrent: number;
  cooldownMs: number;
  priceThreshold: number;
  modelAllowlist: string;
  balanceToken: string;
  balanceUserId: string;
  balanceAlertUsd: number;
  spendCapDailyMinUsd: number;
  spendCapDailyMaxUsd: number;
  spendCapTotalMinUsd: number;
  spendCapTotalMaxUsd: number;
  spendCapAction: string;
}>) => api<FallbackChannel>('PATCH', `/api/me/fallback-channels/${id}`, body);

export const deleteMeFallback = (id: string) =>
  api<void>('DELETE', `/api/me/fallback-channels/${id}`);

export const setMeFallbackEnabled = (id: string, enabled: boolean) =>
  api<void>('PATCH', `/api/me/fallback-channels/${id}/enabled`, { enabled });

// --- Tenant slots (时段槽位) ---
export const getMeSlots = () =>
  api<Slot[]>('GET', '/api/me/slots');

export const createMeSlot = (body: { name: string; startMin: number; endMin: number }) =>
  api<{ id: string }>('POST', '/api/me/slots', body);

export const deleteMeSlot = (id: string) =>
  api<void>('DELETE', `/api/me/slots/${id}`);

export const setMeSlotEnabled = (id: string, enabled: boolean) =>
  api<void>('PATCH', `/api/me/slots/${id}/enabled`, { enabled });

// --- Tenant dispatch keys (调度密钥) ---
export const getMeDispatchKeys = () =>
  api<DispatchKeyRecord[]>('GET', '/api/me/dispatch-keys');

export const createMeDispatchKey = (label: string) =>
  api<DispatchKeyCreated>('POST', '/api/me/dispatch-keys', { label });

export const deleteMeDispatchKey = (id: string) =>
  api<void>('DELETE', `/api/me/dispatch-keys/${id}`);

// --- Tenant ban analysis (封号分析) ---
export const getMeBanAnalysis = () =>
  api<BanAnalysis>('GET', '/api/me/ban-analysis');

// --- Tenant dispatch overview (调度) ---
export const getMeDispatchStatus = () =>
  api<DispatchStatus>('GET', '/api/me/dispatch/status');
