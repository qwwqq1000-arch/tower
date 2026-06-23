// ============================================================
// Tower SPA — Nodes page
// Table (desktop) / Cards (mobile): name/baseUrl/email/createdAt/uptime/status/version
// Per-node OAuth wizard modal + search/sort/pagination + batch ops
// ============================================================
import { useEffect, useState, useCallback, useRef } from 'react';
import { Link } from 'react-router-dom';
import {
  listNodes,
  createNode,
  deleteNode,
  setNodeEnabled,
  refreshNode,
  getNodeConsoleUrl,
  startProvision,
  getProvision,
  oauthStart,
  oauthExchange,
  listNodeProfiles,
  importNodeProfile,
} from '../api';
import type { NodeRecord, NodeProfile } from '../types';

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------
function fmtDate(ms?: number): string {
  if (!ms) return '—';
  const d = new Date(ms);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${dd}`;
}

function fmtDays(ms?: number): string {
  if (!ms) return '—';
  const days = Math.floor((Date.now() - ms) / 86400000);
  return `${days}天`;
}

// ------------------------------------------------------------------
// Status badge
// ------------------------------------------------------------------
// openConsole opens a node's management console. The URL (with the node's key for
// meridian, so the dashboard opens authenticated) is built server-side. The blank tab is
// opened synchronously within the click so it isn't popup-blocked, then redirected once
// the URL resolves (node-console-1).
function openConsole(node: NodeRecord) {
  const win = window.open('', '_blank', 'noopener,noreferrer');
  getNodeConsoleUrl(node.id)
    .then(({ url }) => { if (win) win.location.href = url; })
    .catch(() => { if (win) win.close(); });
}

function NodeKindBadge({ kind }: { kind?: string }) {
  const isCpa = (kind ?? 'meridian').toLowerCase() === 'cpa';
  const cls = isCpa
    ? 'bg-purple-500/15 text-purple-400 border-purple-500/30'
    : 'bg-sky-500/15 text-sky-400 border-sky-500/30';
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[10px] font-mono uppercase ${cls}`}>
      {isCpa ? 'CPA' : 'MERIDIAN'}
    </span>
  );
}

function NodeStatusBadge({ node }: { node: NodeRecord }) {
  if (!node.enabled) {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs font-medium text-muted">
        <span className="w-1.5 h-1.5 rounded-full bg-muted" />
        停用
      </span>
    );
  }
  if (node.loggedIn === false) {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs font-medium text-orange-400">
        <span className="w-1.5 h-1.5 rounded-full bg-orange-400" />
        未上传凭证
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-xs font-medium text-ok">
      <span className="w-1.5 h-1.5 rounded-full bg-ok" />
      正常
    </span>
  );
}

// ------------------------------------------------------------------
// OAuth wizard modal (per-node)
// ------------------------------------------------------------------
interface OAuthWizardModalProps {
  node: NodeRecord;
  onClose: () => void;
  onSuccess: () => void;
}

function OAuthWizardModal({ node, onClose, onSuccess }: OAuthWizardModalProps) {
  // OAuth flow state
  const [step, setStep] = useState<'idle' | 'authorizing' | 'exchange' | 'done'>('idle');
  const [authorizeUrl, setAuthorizeUrl] = useState('');
  const [codeVerifier, setCodeVerifier] = useState('');
  const [state, setState] = useState('');
  const [code, setCode] = useState('');
  const [oauthErr, setOauthErr] = useState<string | null>(null);
  const [oauthLoading, setOauthLoading] = useState(false);

  // Profiles state
  const [profiles, setProfiles] = useState<NodeProfile[]>([]);
  const [profilesLoading, setProfilesLoading] = useState(true);
  const [profilesErr, setProfilesErr] = useState<string | null>(null);
  const [importing, setImporting] = useState<Record<string, boolean>>({});
  const [importResults, setImportResults] = useState<Record<string, string>>({});

  // Load existing profiles
  const loadProfiles = useCallback(async () => {
    setProfilesLoading(true);
    setProfilesErr(null);
    try {
      const data = await listNodeProfiles(node.id);
      setProfiles(data);
    } catch (e) {
      setProfilesErr(e instanceof Error ? e.message : '加载失败');
    } finally {
      setProfilesLoading(false);
    }
  }, [node.id]);

  useEffect(() => {
    void loadProfiles();
  }, [loadProfiles]);

  async function handleStartOAuth() {
    setOauthLoading(true);
    setOauthErr(null);
    try {
      const res = await oauthStart(node.id);
      setAuthorizeUrl(res.authorizeUrl);
      setCodeVerifier(res.codeVerifier);
      setState(res.state);
      setStep('authorizing');
    } catch (e) {
      setOauthErr(e instanceof Error ? e.message : '启动失败');
    } finally {
      setOauthLoading(false);
    }
  }

  async function handleExchange() {
    if (!code.trim()) return;
    setOauthLoading(true);
    setOauthErr(null);
    try {
      await oauthExchange(node.id, { codeVerifier, state, code: code.trim() });
      setStep('done');
      onSuccess();
      void loadProfiles();
    } catch (e) {
      setOauthErr(e instanceof Error ? e.message : '换取失败');
    } finally {
      setOauthLoading(false);
    }
  }

  async function handleImport(profileId: string) {
    setImporting((prev) => ({ ...prev, [profileId]: true }));
    try {
      const res = await importNodeProfile(node.id, profileId);
      setImportResults((prev) => ({ ...prev, [profileId]: res.reused ? '已存在' : '导入成功' }));
      onSuccess();
    } catch (e) {
      setImportResults((prev) => ({ ...prev, [profileId]: e instanceof Error ? e.message : '失败' }));
    } finally {
      setImporting((prev) => ({ ...prev, [profileId]: false }));
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="bg-surface border border-line rounded-2xl w-full max-w-lg max-h-[90vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-line">
          <div>
            <h2 className="text-base font-semibold text-ink">授权向导</h2>
            <p className="text-xs text-muted mt-0.5">{node.name}</p>
          </div>
          <button
            onClick={onClose}
            className="text-muted hover:text-ink text-lg leading-none transition"
          >
            ×
          </button>
        </div>

        <div className="p-5 space-y-6">
          {/* --- OAuth section --- */}
          <div className="space-y-3">
            <h3 className="text-sm font-semibold text-ink">OAuth 授权</h3>

            {step === 'idle' && (
              <div className="space-y-2">
                <button
                  onClick={() => { void handleStartOAuth(); }}
                  disabled={oauthLoading}
                  className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                             hover:bg-accent/80 disabled:opacity-50 transition"
                >
                  {oauthLoading ? '生成中…' : '开始 OAuth 授权'}
                </button>
                {oauthErr && <p className="text-xs text-err">{oauthErr}</p>}
              </div>
            )}

            {step === 'authorizing' && (
              <div className="space-y-3">
                <p className="text-xs text-muted">
                  1. 点击下方链接在浏览器中完成授权，然后复制回调 code 粘贴到下方。
                </p>
                <a
                  href={authorizeUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-block text-xs text-accent hover:underline break-all"
                >
                  {authorizeUrl}
                </a>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    placeholder="粘贴授权 code…"
                    className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                               placeholder:text-muted focus:outline-none focus:border-accent transition"
                  />
                  <button
                    onClick={() => { void handleExchange(); }}
                    disabled={oauthLoading || !code.trim()}
                    className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                               hover:bg-accent/80 disabled:opacity-50 transition whitespace-nowrap"
                  >
                    {oauthLoading ? '换取中…' : '提交'}
                  </button>
                </div>
                {oauthErr && <p className="text-xs text-err">{oauthErr}</p>}
              </div>
            )}

            {step === 'done' && (
              <div className="space-y-2">
                <p className="text-xs text-ok font-medium">授权成功！</p>
                <button
                  onClick={() => {
                    setStep('idle');
                    setCode('');
                    setAuthorizeUrl('');
                    setOauthErr(null);
                  }}
                  className="text-xs text-accent hover:underline"
                >
                  再次授权
                </button>
              </div>
            )}
          </div>

          {/* --- Existing profiles section --- */}
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-ink">已登录账号</h3>
              <button
                onClick={() => { void loadProfiles(); }}
                className="text-xs text-accent hover:underline"
              >
                刷新
              </button>
            </div>

            {profilesLoading && (
              <p className="text-xs text-muted animate-pulse">加载中…</p>
            )}
            {profilesErr && (
              <p className="text-xs text-err">{profilesErr}</p>
            )}
            {!profilesLoading && !profilesErr && profiles.length === 0 && (
              <p className="text-xs text-muted">暂无账号</p>
            )}
            {!profilesLoading && profiles.length > 0 && (
              <div className="space-y-2">
                {profiles.map((p) => (
                  <div
                    key={p.id}
                    className="flex items-center justify-between gap-3 bg-bg border border-line rounded-lg px-3 py-2"
                  >
                    <div className="min-w-0">
                      <p className="text-xs font-medium text-ink truncate">
                        {p.email ?? p.name ?? p.id}
                      </p>
                      {p.subscriptionType && (
                        <p className="text-xs text-muted">{p.subscriptionType}</p>
                      )}
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      {p.loggedIn !== undefined && (
                        <span className={`text-xs ${p.loggedIn ? 'text-ok' : 'text-muted'}`}>
                          {p.loggedIn ? '已登录' : '未登录'}
                        </span>
                      )}
                      {importResults[p.id] ? (
                        <span className="text-xs text-ok">{importResults[p.id]}</span>
                      ) : (
                        <button
                          onClick={() => { void handleImport(p.id); }}
                          disabled={importing[p.id]}
                          className="px-2.5 py-1 text-xs font-medium bg-accent text-white rounded-lg
                                     hover:bg-accent/80 disabled:opacity-50 transition"
                        >
                          {importing[p.id] ? '导入中…' : '导入'}
                        </button>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// One-click provision wizard (一键开通)
// ------------------------------------------------------------------
interface ProvisionWizardProps {
  onSuccess: () => void;
}

function ProvisionWizard({ onSuccess }: ProvisionWizardProps) {
  const [open, setOpen] = useState(false);
  const [host, setHost] = useState('');
  const [user, setUser] = useState('root');
  const [password, setPassword] = useState('');
  const [ownerId, setOwnerId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [jobStatus, setJobStatus] = useState<string | null>(null);
  const [jobStep, setJobStep] = useState<string | null>(null);
  const [jobLog, setJobLog] = useState<string | null>(null);
  const [jobDone, setJobDone] = useState(false);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const logBoxRef = useRef<HTMLPreElement | null>(null);

  function clearTimer() {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }

  useEffect(() => {
    if (logBoxRef.current) {
      logBoxRef.current.scrollTop = logBoxRef.current.scrollHeight;
    }
  }, [jobLog]);

  useEffect(() => {
    return () => clearTimer();
  }, []);

  function handleClose() {
    clearTimer();
    setOpen(false);
    setHost('');
    setUser('root');
    setPassword('');
    setOwnerId('');
    setSubmitting(false);
    setErr(null);
    setJobStatus(null);
    setJobStep(null);
    setJobLog(null);
    setJobDone(false);
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!host.trim() || !password.trim()) return;
    setSubmitting(true);
    setErr(null);
    setJobStatus('pending');
    setJobStep(null);
    setJobLog('');
    setJobDone(false);
    try {
      const res = await startProvision({
        host: host.trim(),
        user: user.trim() || 'root',
        password: password,
        ...(ownerId.trim() ? { ownerId: ownerId.trim() } : {}),
      });
      const jobId = res.jobId;
      timerRef.current = setInterval(() => {
        void (async () => {
          try {
            const job = await getProvision(jobId);
            setJobStatus(job.status);
            setJobStep(job.step ?? null);
            setJobLog(job.log ?? null);
            if (job.status === 'success' || job.status === 'failed') {
              clearTimer();
              setJobDone(true);
              setSubmitting(false);
              if (job.status === 'success') {
                onSuccess();
              }
            }
          } catch (pollErr) {
            setErr(pollErr instanceof Error ? pollErr.message : '轮询失败');
            clearTimer();
            setSubmitting(false);
            setJobDone(true);
          }
        })();
      }, 2000);
    } catch (startErr) {
      setErr(startErr instanceof Error ? startErr.message : '开通失败');
      setSubmitting(false);
      setJobStatus(null);
    }
  }

  const statusColor =
    jobStatus === 'success' ? 'text-ok' :
    jobStatus === 'failed' ? 'text-err' :
    'text-muted';

  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between px-4 py-3 text-sm font-semibold text-ink hover:bg-line/20 transition"
      >
        <span>一键开通节点</span>
        <span className="text-muted text-xs">{open ? '收起 ▲' : '展开 ▼'}</span>
      </button>

      {open && (
        <div className="border-t border-line p-4 space-y-4">
          {!submitting && !jobDone && (
            <form onSubmit={(e) => { void handleSubmit(e); }} className="space-y-3">
              <div className="flex flex-col sm:flex-row gap-2">
                <input
                  type="text"
                  value={host}
                  onChange={(e) => setHost(e.target.value)}
                  placeholder="主机 IP / 域名 *"
                  required
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
                <input
                  type="text"
                  value={user}
                  onChange={(e) => setUser(e.target.value)}
                  placeholder="SSH 用户名（默认 root）"
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
              </div>
              <div className="flex flex-col sm:flex-row gap-2">
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="SSH 密码 *"
                  required
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
                <input
                  type="text"
                  value={ownerId}
                  onChange={(e) => setOwnerId(e.target.value)}
                  placeholder="归属用户 ID（选填）"
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
              </div>
              <div className="flex gap-2">
                <button
                  type="submit"
                  disabled={!host.trim() || !password.trim()}
                  className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                             hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
                >
                  开始开通
                </button>
                <button
                  type="button"
                  onClick={handleClose}
                  className="px-4 py-2 text-sm font-medium bg-bg border border-line text-muted rounded-lg hover:text-ink transition"
                >
                  取消
                </button>
              </div>
              {err && <p className="text-xs text-err">{err}</p>}
            </form>
          )}

          {(submitting || jobDone) && (
            <div className="space-y-3">
              <div className="flex items-center gap-3 flex-wrap">
                <span className="text-sm font-medium text-ink">进度</span>
                {jobStatus && (
                  <span className={`text-xs font-medium ${statusColor}`}>
                    状态: {jobStatus}
                  </span>
                )}
                {jobStep && (
                  <span className="text-xs text-muted">步骤: {jobStep}</span>
                )}
                {submitting && (
                  <span className="text-xs text-muted animate-pulse">轮询中…</span>
                )}
              </div>
              {jobLog !== null && (
                <pre
                  ref={logBoxRef}
                  className="bg-bg border border-line rounded-lg p-3 text-xs font-mono text-ink
                             max-h-48 overflow-y-auto whitespace-pre-wrap break-all"
                >
                  {jobLog || '(等待日志…)'}
                </pre>
              )}
              {err && <p className="text-xs text-err">{err}</p>}
              {jobDone && (
                <div className="flex gap-2">
                  {jobStatus === 'success' && (
                    <span className="text-xs text-ok font-medium">开通成功！</span>
                  )}
                  {jobStatus === 'failed' && (
                    <span className="text-xs text-err font-medium">开通失败</span>
                  )}
                  <button
                    type="button"
                    onClick={handleClose}
                    className="text-xs text-accent hover:underline"
                  >
                    关闭
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// Add-node form
// ------------------------------------------------------------------
interface AddNodeFormProps {
  onAdded: () => void;
}

function AddNodeForm({ onAdded }: AddNodeFormProps) {
  const [baseUrl, setBaseUrl] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [ownerId, setOwnerId] = useState('');
  const [kind, setKind] = useState<'meridian' | 'cpa'>('meridian');
  const [mgmtKey, setMgmtKey] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!baseUrl.trim()) return;
    setSubmitting(true);
    setErr(null);
    try {
      await createNode({
        baseUrl: baseUrl.trim(),
        kind,
        ...(apiKey.trim() ? { apiKey: apiKey.trim() } : {}),
        ...(ownerId.trim() ? { ownerId: ownerId.trim() } : {}),
        ...(kind === 'cpa' && mgmtKey.trim() ? { mgmtKey: mgmtKey.trim() } : {}),
      });
      setBaseUrl('');
      setApiKey('');
      setOwnerId('');
      setMgmtKey('');
      onAdded();
    } catch (error) {
      setErr(error instanceof Error ? error.message : '添加失败');
    } finally {
      setSubmitting(false);
    }
  }

  const inputCls =
    'flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink ' +
    'placeholder:text-muted focus:outline-none focus:border-accent transition';

  return (
    <form
      onSubmit={(e) => { void handleSubmit(e); }}
      className="bg-surface border border-line rounded-xl p-4"
    >
      <h2 className="text-sm font-semibold text-ink mb-3">添加节点</h2>
      <div className="flex flex-col sm:flex-row gap-2">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as 'meridian' | 'cpa')}
          className="bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink focus:outline-none focus:border-accent transition"
        >
          <option value="meridian">MERIDIAN</option>
          <option value="cpa">CPA</option>
        </select>
        <input
          type="text"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          placeholder={kind === 'cpa' ? '接入地址 * (如 http://ip:8317)' : '接入地址 * (如 http://ip:3456)'}
          required
          className={inputCls}
        />
        <input
          type="text"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={kind === 'cpa' ? 'CPA api-key（推理）' : 'API密钥（选填）'}
          className={inputCls}
        />
        <input
          type="text"
          value={ownerId}
          onChange={(e) => setOwnerId(e.target.value)}
          placeholder="归属用户 ID（选填）"
          className={inputCls}
        />
        <button
          type="submit"
          disabled={submitting || !baseUrl.trim()}
          className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                     hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition whitespace-nowrap"
        >
          {submitting ? '添加中…' : '+ 添加'}
        </button>
      </div>
      {kind === 'cpa' && (
        <div className="mt-2">
          <input
            type="text"
            value={mgmtKey}
            onChange={(e) => setMgmtKey(e.target.value)}
            placeholder="CPA 管理密钥 (management secret-key) — 用于读取号库与额度"
            className="w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition"
          />
          <p className="text-[11px] text-muted mt-1">CPA 节点会自动读取其下所有账户并显示在号库;调度时按账户单独路由(X-CLIProxy-Account)。</p>
        </div>
      )}
      {err && <p className="text-xs text-err mt-2">{err}</p>}
    </form>
  );
}

// ------------------------------------------------------------------
// Batch action bar
// ------------------------------------------------------------------
interface BatchBarProps {
  selectedCount: number;
  onEnableAll: () => void;
  onDisableAll: () => void;
  onRefreshAll: () => void;
  batchRunning: boolean;
  batchResult: string | null;
  batchHasError: boolean;
}

function BatchBar({
  selectedCount,
  onEnableAll,
  onDisableAll,
  onRefreshAll,
  batchRunning,
  batchResult,
  batchHasError,
}: BatchBarProps) {
  return (
    <div className="flex items-center gap-3 flex-wrap bg-accent/10 border border-accent/30 rounded-xl px-4 py-2.5">
      <span className="text-sm text-ink font-medium">已选 {selectedCount} 个节点</span>
      <div className="flex items-center gap-2 ml-auto flex-wrap">
        <button
          onClick={onEnableAll}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-ok text-white rounded-lg
                     hover:bg-ok/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          批量启用
        </button>
        <button
          onClick={onDisableAll}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-err text-white rounded-lg
                     hover:bg-err/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          批量停用
        </button>
        <button
          onClick={onRefreshAll}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-ink rounded-lg
                     hover:bg-line/30 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          批量刷新 token
        </button>
      </div>
      {batchRunning && (
        <span className="text-xs text-muted animate-pulse">处理中…</span>
      )}
      {batchResult && !batchRunning && (
        <span className={`text-xs ${batchHasError ? 'text-err' : 'text-ok'}`}>{batchResult}</span>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// Sort helpers
// ------------------------------------------------------------------
type SortKey = 'name' | 'createdAt' | 'status';
type SortDir = 'asc' | 'desc';

function statusOrder(node: NodeRecord): number {
  if (!node.enabled) return 2;
  if (node.loggedIn === false) return 1;
  return 0;
}

function sortNodes(nodes: NodeRecord[], key: SortKey, dir: SortDir): NodeRecord[] {
  const sorted = [...nodes].sort((a, b) => {
    let cmp = 0;
    if (key === 'name') {
      cmp = a.name.localeCompare(b.name, 'zh-CN');
    } else if (key === 'createdAt') {
      cmp = (a.createdAt ?? 0) - (b.createdAt ?? 0);
    } else if (key === 'status') {
      cmp = statusOrder(a) - statusOrder(b);
    }
    return dir === 'asc' ? cmp : -cmp;
  });
  return sorted;
}

// Sort header button
function SortTh({
  label,
  col,
  sortKey,
  sortDir,
  onSort,
  className,
}: {
  label: string;
  col: SortKey;
  sortKey: SortKey;
  sortDir: SortDir;
  onSort: (col: SortKey) => void;
  className?: string;
}) {
  const active = sortKey === col;
  return (
    <th className={`px-4 py-3 font-medium ${className ?? ''}`}>
      <button
        type="button"
        onClick={() => onSort(col)}
        className="flex items-center gap-1 text-xs text-muted uppercase tracking-wide hover:text-ink transition"
      >
        {label}
        <span className="text-xs">
          {active ? (sortDir === 'asc' ? '↑' : '↓') : '↕'}
        </span>
      </button>
    </th>
  );
}

// ------------------------------------------------------------------
// Node row (desktop table)
// ------------------------------------------------------------------
function NodeRow({
  node,
  selected,
  onSelect,
  onDelete,
  onToggleEnabled,
  onOpenOAuth,
}: {
  node: NodeRecord;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onDelete: (id: string) => void;
  onToggleEnabled: (node: NodeRecord) => void;
  onOpenOAuth: (node: NodeRecord) => void;
}) {
  const [deleting, setDeleting] = useState(false);
  const [toggling, setToggling] = useState(false);

  async function handleDelete() {
    if (!confirm(`确认删除节点 ${node.name}？`)) return;
    setDeleting(true);
    try {
      await deleteNode(node.id);
      onDelete(node.id);
    } catch {
      setDeleting(false);
    }
  }

  async function handleToggle() {
    setToggling(true);
    try {
      onToggleEnabled(node);
    } finally {
      setToggling(false);
    }
  }

  const version = node.liveVersion ?? node.version ?? '—';

  return (
    <tr className="border-t border-line hover:bg-line/30 transition">
      <td className="px-4 py-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={(e) => onSelect(node.id, e.target.checked)}
          className="rounded border-line accent-accent cursor-pointer"
        />
      </td>
      <td className="px-4 py-3 text-sm font-medium">
        <div className="flex items-center gap-2">
          <Link to={`/nodes/${node.id}`} className="text-accent hover:underline">
            {node.name}
          </Link>
          <NodeKindBadge kind={node.kind} />
        </div>
      </td>
      <td className="px-4 py-3 text-sm text-muted max-w-xs truncate">{node.baseUrl}</td>
      <td className="px-4 py-3 text-sm text-muted">{node.email ?? '—'}</td>
      <td className="px-4 py-3 text-sm text-muted">{fmtDate(node.createdAt)}</td>
      <td className="px-4 py-3 text-sm text-muted">{fmtDays(node.createdAt)}</td>
      <td className="px-4 py-3">
        <NodeStatusBadge node={node} />
      </td>
      <td className="px-4 py-3 text-xs text-muted font-mono">{version}</td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-3 flex-wrap">
          <button
            onClick={() => onOpenOAuth(node)}
            className="text-xs font-medium text-accent hover:text-accent/70 transition"
            title="授权向导"
          >
            加号
          </button>
          <Link
            to={`/nodes/${node.id}`}
            className="text-xs text-muted hover:text-ink transition"
          >
            详情
          </Link>
          <button type="button" onClick={() => openConsole(node)} className="text-xs text-muted hover:text-ink transition">控制台</button>
          <button
            onClick={() => { void handleToggle(); }}
            disabled={toggling}
            className={`text-xs disabled:opacity-50 transition ${
              node.enabled
                ? 'text-err hover:text-err/70'
                : 'text-ok hover:text-ok/70'
            }`}
          >
            {node.enabled ? '停用' : '启用'}
          </button>
          <button
            onClick={() => { void handleDelete(); }}
            disabled={deleting}
            className="text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
          >
            {deleting ? '…' : '删除'}
          </button>
        </div>
      </td>
    </tr>
  );
}

// ------------------------------------------------------------------
// Node card (mobile)
// ------------------------------------------------------------------
function NodeMobileCard({
  node,
  selected,
  onSelect,
  onDelete,
  onToggleEnabled,
  onOpenOAuth,
}: {
  node: NodeRecord;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onDelete: (id: string) => void;
  onToggleEnabled: (node: NodeRecord) => void;
  onOpenOAuth: (node: NodeRecord) => void;
}) {
  const [deleting, setDeleting] = useState(false);

  async function handleDelete() {
    if (!confirm(`确认删除节点 ${node.name}？`)) return;
    setDeleting(true);
    try {
      await deleteNode(node.id);
      onDelete(node.id);
    } catch {
      setDeleting(false);
    }
  }

  const version = node.liveVersion ?? node.version ?? '—';

  return (
    <div className={`bg-surface border rounded-xl p-4 space-y-2 ${selected ? 'border-accent' : 'border-line'}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-start gap-2 min-w-0">
          <input
            type="checkbox"
            checked={selected}
            onChange={(e) => onSelect(node.id, e.target.checked)}
            className="mt-0.5 rounded border-line accent-accent cursor-pointer shrink-0"
          />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <Link
                to={`/nodes/${node.id}`}
                className="text-sm font-semibold text-accent hover:underline truncate"
              >
                {node.name}
              </Link>
              <NodeKindBadge kind={node.kind} />
            </div>
            <p className="text-xs text-muted mt-0.5 truncate">{node.baseUrl}</p>
            {node.email && <p className="text-xs text-muted truncate">{node.email}</p>}
          </div>
        </div>
        <button
          onClick={() => { void handleDelete(); }}
          disabled={deleting}
          className="shrink-0 text-xs text-err hover:text-err/70 disabled:opacity-50 transition"
        >
          {deleting ? '…' : '删除'}
        </button>
      </div>

      <div className="flex items-center gap-4 text-xs text-muted pl-6 flex-wrap">
        <NodeStatusBadge node={node} />
        <span>加入: {fmtDate(node.createdAt)}</span>
        <span>运行: {fmtDays(node.createdAt)}</span>
        <span className="font-mono">{version}</span>
      </div>

      <div className="flex items-center gap-3 pl-6 flex-wrap">
        <button
          onClick={() => onOpenOAuth(node)}
          className="text-xs text-accent hover:text-accent/70 transition"
        >
          加号
        </button>
        <button
          onClick={() => onToggleEnabled(node)}
          className={`text-xs transition ${node.enabled ? 'text-err hover:text-err/70' : 'text-ok hover:text-ok/70'}`}
        >
          {node.enabled ? '停用' : '启用'}
        </button>
        <button type="button" onClick={() => openConsole(node)} className="text-xs text-muted hover:text-ink transition">控制台</button>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------
// Nodes page
// ------------------------------------------------------------------
const PAGE_SIZE = 12;

export default function Nodes() {
  const [nodes, setNodes] = useState<NodeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Search
  const [search, setSearch] = useState('');

  // Sort
  const [sortKey, setSortKey] = useState<SortKey>('name');
  const [sortDir, setSortDir] = useState<SortDir>('asc');

  // Pagination
  const [page, setPage] = useState(1);

  // Multi-select
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchRunning, setBatchRunning] = useState(false);
  const [batchResult, setBatchResult] = useState<string | null>(null);
  const [batchHasError, setBatchHasError] = useState(false);

  // OAuth modal
  const [oauthNode, setOauthNode] = useState<NodeRecord | null>(null);

  const fetchNodes = useCallback(async () => {
    try {
      const data = await listNodes();
      setNodes(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchNodes();
  }, [fetchNodes]);

  // Derived: filtered + sorted + paginated
  const filtered = nodes.filter((n) => {
    if (!search.trim()) return true;
    const q = search.trim().toLowerCase();
    return (
      n.name.toLowerCase().includes(q) ||
      n.baseUrl.toLowerCase().includes(q) ||
      (n.email ?? '').toLowerCase().includes(q)
    );
  });

  const sorted = sortNodes(filtered, sortKey, sortDir);
  const totalPages = Math.max(1, Math.ceil(sorted.length / PAGE_SIZE));
  const safePage = Math.min(page, totalPages);
  const pageItems = sorted.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE);

  // Reset to page 1 when filter changes
  useEffect(() => {
    setPage(1);
  }, [search, sortKey, sortDir]);

  function handleSort(col: SortKey) {
    if (sortKey === col) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortKey(col);
      setSortDir('asc');
    }
  }

  function handleDelete(id: string) {
    setNodes((prev) => prev.filter((n) => n.id !== id));
    setSelected((prev) => { const s = new Set(prev); s.delete(id); return s; });
  }

  function handleSelect(id: string, checked: boolean) {
    setSelected((prev) => {
      const s = new Set(prev);
      if (checked) s.add(id); else s.delete(id);
      return s;
    });
  }

  const allSelected = pageItems.length > 0 && pageItems.every((n) => selected.has(n.id));
  const someSelected = pageItems.some((n) => selected.has(n.id)) && !allSelected;

  function handleSelectAll(checked: boolean) {
    if (checked) {
      setSelected((prev) => {
        const s = new Set(prev);
        pageItems.forEach((n) => s.add(n.id));
        return s;
      });
    } else {
      setSelected((prev) => {
        const s = new Set(prev);
        pageItems.forEach((n) => s.delete(n.id));
        return s;
      });
    }
  }

  async function runBatch(op: (id: string) => Promise<void>, label: string) {
    setBatchRunning(true);
    setBatchResult(null);
    setBatchHasError(false);
    const ids = Array.from(selected);
    let ok = 0;
    let fail = 0;
    await Promise.allSettled(
      ids.map((id) =>
        op(id)
          .then(() => { ok++; })
          .catch(() => { fail++; }),
      ),
    );
    setBatchHasError(fail > 0);
    setBatchResult(`${label}: ${ok} 成功, ${fail} 失败`);
    setBatchRunning(false);
    void fetchNodes();
  }

  function handleBatchEnable() {
    void runBatch((id) => setNodeEnabled(id, true), '批量启用');
  }

  function handleBatchDisable() {
    void runBatch((id) => setNodeEnabled(id, false), '批量停用');
  }

  function handleBatchRefresh() {
    void runBatch((id) => refreshNode(id), '批量刷新');
  }

  function handleToggleEnabled(node: NodeRecord) {
    void setNodeEnabled(node.id, !node.enabled).then(() => {
      setNodes((prev) =>
        prev.map((n) => (n.id === node.id ? { ...n, enabled: !n.enabled } : n)),
      );
    });
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold text-ink">节点</h1>
        <button
          onClick={() => { void fetchNodes(); }}
          className="text-xs text-accent hover:underline"
        >
          刷新
        </button>
      </div>

      {/* Add node form */}
      <AddNodeForm onAdded={() => { void fetchNodes(); }} />

      {/* One-click provision wizard */}
      <ProvisionWizard onSuccess={() => { void fetchNodes(); }} />

      {/* Search */}
      <div className="flex items-center gap-2">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索节点名称 / 地址 / 邮箱…"
          className="w-full max-w-sm bg-surface border border-line rounded-lg px-3 py-2 text-sm text-ink
                     placeholder:text-muted focus:outline-none focus:border-accent transition"
        />
        {search && (
          <button
            onClick={() => setSearch('')}
            className="text-xs text-muted hover:text-ink transition"
          >
            清除
          </button>
        )}
      </div>

      {/* Batch action bar */}
      {selected.size > 0 && (
        <BatchBar
          selectedCount={selected.size}
          onEnableAll={handleBatchEnable}
          onDisableAll={handleBatchDisable}
          onRefreshAll={handleBatchRefresh}
          batchRunning={batchRunning}
          batchResult={batchResult}
          batchHasError={batchHasError}
        />
      )}

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center min-h-32">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}

      {/* Error */}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">
          {error}
        </div>
      )}

      {/* Empty */}
      {!loading && !error && filtered.length === 0 && (
        <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
          {search ? `没有匹配"${search}"的节点` : '暂无节点 — 使用上方表单添加新节点'}
        </div>
      )}

      {/* Desktop table */}
      {!loading && !error && pageItems.length > 0 && (
        <>
          {/* Table: hidden on mobile */}
          <div className="hidden md:block bg-surface border border-line rounded-xl overflow-hidden">
            <table className="w-full text-left">
              <thead>
                <tr className="text-xs text-muted uppercase tracking-wide">
                  <th className="px-4 py-3 font-medium w-8">
                    <input
                      type="checkbox"
                      checked={allSelected}
                      ref={(el) => {
                        if (el) el.indeterminate = someSelected;
                      }}
                      onChange={(e) => handleSelectAll(e.target.checked)}
                      className="rounded border-line accent-accent cursor-pointer"
                    />
                  </th>
                  <SortTh label="节点ID" col="name" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
                  <th className="px-4 py-3 font-medium text-xs text-muted uppercase tracking-wide">地址</th>
                  <th className="px-4 py-3 font-medium text-xs text-muted uppercase tracking-wide">邮箱</th>
                  <SortTh label="加入时间" col="createdAt" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
                  <th className="px-4 py-3 font-medium text-xs text-muted uppercase tracking-wide">已运行</th>
                  <SortTh label="状态" col="status" sortKey={sortKey} sortDir={sortDir} onSort={handleSort} />
                  <th className="px-4 py-3 font-medium text-xs text-muted uppercase tracking-wide">版本</th>
                  <th className="px-4 py-3 font-medium text-xs text-muted uppercase tracking-wide">操作</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((node) => (
                  <NodeRow
                    key={node.id}
                    node={node}
                    selected={selected.has(node.id)}
                    onSelect={handleSelect}
                    onDelete={handleDelete}
                    onToggleEnabled={handleToggleEnabled}
                    onOpenOAuth={(n) => setOauthNode(n)}
                  />
                ))}
              </tbody>
            </table>
          </div>

          {/* Cards: visible only on mobile */}
          <div className="md:hidden space-y-3">
            <div className="flex items-center gap-2 px-1">
              <input
                type="checkbox"
                checked={allSelected}
                ref={(el) => {
                  if (el) el.indeterminate = someSelected;
                }}
                onChange={(e) => handleSelectAll(e.target.checked)}
                className="rounded border-line accent-accent cursor-pointer"
              />
              <span className="text-xs text-muted">全选（本页）</span>
            </div>
            {pageItems.map((node) => (
              <NodeMobileCard
                key={node.id}
                node={node}
                selected={selected.has(node.id)}
                onSelect={handleSelect}
                onDelete={handleDelete}
                onToggleEnabled={handleToggleEnabled}
                onOpenOAuth={(n) => setOauthNode(n)}
              />
            ))}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between gap-3 pt-2">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={safePage === 1}
                className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-ink rounded-lg
                           hover:bg-line/30 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                上一页
              </button>
              <span className="text-xs text-muted">
                第 {safePage} / {totalPages} 页，共 {filtered.length} 条
              </span>
              <button
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                disabled={safePage === totalPages}
                className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-ink rounded-lg
                           hover:bg-line/30 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                下一页
              </button>
            </div>
          )}
        </>
      )}

      {/* OAuth wizard modal */}
      {oauthNode && (
        <OAuthWizardModal
          node={oauthNode}
          onClose={() => setOauthNode(null)}
          onSuccess={() => { void fetchNodes(); }}
        />
      )}
    </div>
  );
}
