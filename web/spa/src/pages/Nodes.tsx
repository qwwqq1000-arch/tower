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
  setNodePassthrough,
  refreshNode,
  updateNodeCode,
  getNodeConsoleUrl,
  startProvision,
  getProvision,
  oauthStart,
  oauthExchange,
  listNodeProfiles,
  importNodeProfile,
  listUsers,
  updateNode,
  batchAutoImport,
  getNodeProxy,
  testNodeProxy,
  setNodeProxy,
} from '../api';
import type { NodeRecord, NodeProfile, UserRow } from '../types';

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
  // cpa nodes: the CLIProxyAPI management UI lives at <baseUrl>/management.html#/
  // (log in there with the secret-key). Open it directly — no server round-trip.
  if ((node.kind ?? 'meridian').toLowerCase() === 'cpa') {
    const base = node.baseUrl.replace(/\/+$/, '');
    window.open(`${base}/management.html#/`, '_blank', 'noopener,noreferrer');
    return;
  }
  getNodeConsoleUrl(node.id)
    .then(({ url }) => { window.open(url, '_blank', 'noopener,noreferrer'); })
    .catch(() => {});
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

// Node status is derived from enabled/banned/loggedIn in ONE place so the badge, the
// status sort, and the status filter never drift apart (tenant-shared-codepath rule).
type NodeStatus = 'ok' | 'banned' | 'noauth' | 'disabled';

function nodeStatusValue(node: NodeRecord): NodeStatus {
  if (!node.enabled) return 'disabled';
  if (node.banned) return 'banned';
  if (node.loggedIn === false) return 'noauth';
  return 'ok';
}

// order drives the 状态 sort: asc = 正常 first, desc surfaces 封号 first.
const NODE_STATUS_META: Record<NodeStatus, { label: string; dot: string; text: string; order: number }> = {
  ok: { label: '正常', dot: 'bg-ok', text: 'text-ok', order: 0 },
  disabled: { label: '停用', dot: 'bg-muted', text: 'text-muted', order: 1 },
  noauth: { label: '未上传凭证', dot: 'bg-orange-400', text: 'text-orange-400', order: 2 },
  banned: { label: '封号', dot: 'bg-err', text: 'text-err', order: 3 },
};

function NodeStatusBadge({ node }: { node: NodeRecord }) {
  const m = NODE_STATUS_META[nodeStatusValue(node)];
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${m.text}`}>
      <span className={`w-1.5 h-1.5 rounded-full ${m.dot}`} />
      {m.label}
    </span>
  );
}

// ------------------------------------------------------------------
// Node edit modal (接入地址 + API密钥)
// ------------------------------------------------------------------
function NodeEditModal({ node, onClose, onSuccess }: { node: NodeRecord; onClose: () => void; onSuccess: () => void }) {
  const [baseUrl, setBaseUrl] = useState(node.baseUrl);
  const [apiKey, setApiKey] = useState('');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function handleSave() {
    setSaving(true);
    setErr(null);
    try {
      await updateNode(node.id, {
        ...(baseUrl !== node.baseUrl ? { baseUrl } : {}),
        ...(apiKey.trim() ? { apiKey: apiKey.trim() } : {}),
      });
      onSuccess();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '保存失败');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div className="bg-surface border border-line rounded-2xl w-full max-w-md shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-line">
          <h2 className="text-base font-semibold text-ink">编辑节点 — {node.name}</h2>
          <button onClick={onClose} className="text-muted hover:text-ink text-lg leading-none transition">x</button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="text-xs text-muted font-medium">接入地址</label>
            <input
              type="text"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder="http://ip:3456"
              className="mt-1 w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition"
            />
          </div>
          <div>
            <label className="text-xs text-muted font-medium">API 密钥（留空不修改）</label>
            <input
              type="text"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="留空保持不变"
              className="mt-1 w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink placeholder:text-muted focus:outline-none focus:border-accent transition"
            />
          </div>
          {err && <p className="text-xs text-err">{err}</p>}
          <button
            onClick={() => { void handleSave(); }}
            disabled={saving}
            className="w-full py-2 text-sm font-medium bg-accent text-white rounded-lg hover:bg-accent/80 disabled:opacity-50 transition"
          >
            {saving ? '保存中…' : '保存'}
          </button>
        </div>
      </div>
    </div>
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

// maskProxy 脱敏展示代理串(隐藏账密与主机中段),仅用于非编辑态显示。
function maskProxy(raw: string): string {
  if (!raw) return '';
  const scheme = raw.match(/^([a-z0-9]+):\/\//i)?.[1] ?? 'socks5';
  const parts = raw.replace(/^[a-z0-9]+:\/\//i, '').split(':');
  const host = parts[0] ?? '';
  const port = parts[1] ?? '';
  const maskedHost =
    host.length > 6 ? host.slice(0, 3) + '***' + host.slice(-2) : host.replace(/.(?=.)/g, '*');
  const cred = parts.length > 2 ? ':***:***' : '';
  return `${scheme}://${maskedHost}:${port}${cred}`;
}

// copyToClipboard works over plain HTTP too: navigator.clipboard is undefined in
// non-secure contexts (the tower is served at http://ip:port), so fall back to a
// hidden <textarea> + execCommand('copy').
function copyToClipboard(text: string) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      void navigator.clipboard.writeText(text).catch(() => fallbackCopy(text));
      return;
    }
  } catch { /* fall through */ }
  fallbackCopy(text);
}
// nodeIP pulls the host (IP or hostname) out of a node baseUrl like
// "http://23.134.76.52:3456/". Falls back to a regex if the URL fails to parse.
function nodeIP(baseUrl: string): string {
  try {
    return new URL(baseUrl).hostname;
  } catch {
    const m = baseUrl.replace(/^\w+:\/\//, '').match(/^([^:/]+)/);
    return m ? m[1] : '';
  }
}
function fallbackCopy(text: string) {
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.focus();
  ta.select();
  try { document.execCommand('copy'); } catch { /* ignore */ }
  document.body.removeChild(ta);
}

function OAuthWizardModal({ node, onClose, onSuccess }: OAuthWizardModalProps) {
  // OAuth flow state
  const [step, setStep] = useState<'idle' | 'authorizing' | 'exchange' | 'done'>('idle');
  const [authorizeUrl, setAuthorizeUrl] = useState('');
  const [copied, setCopied] = useState(false);
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

  // Egress proxy state (节点级出口代理,透传节点 /settings/api/proxy)
  const [proxyRaw, setProxyRaw] = useState('');
  const [proxyLoaded, setProxyLoaded] = useState(''); // 服务端当前值(脱敏显示 + 清除按钮显隐)
  const [proxyErr, setProxyErr] = useState<string | null>(null);
  const [proxyMsg, setProxyMsg] = useState<string | null>(null);
  const [proxyBusy, setProxyBusy] = useState(false);
  const [proxyTested, setProxyTested] = useState(false);
  const [proxyRestarting, setProxyRestarting] = useState(false);
  const [cpaBlocked, setCpaBlocked] = useState(false);

  // Load existing profiles (backend auto-imports logged-in ones)
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
  }, [node.id, onSuccess]);

  useEffect(() => {
    void loadProfiles();
  }, [loadProfiles]);

  // Load current egress proxy (409 = CPA node → block the section).
  const loadProxy = useCallback(async () => {
    setProxyErr(null);
    try {
      const data = await getNodeProxy(node.id);
      setProxyRaw(data.proxy ?? '');
      setProxyLoaded(data.proxy ?? '');
      setCpaBlocked(false);
    } catch (e) {
      const msg = e instanceof Error ? e.message : '加载失败';
      if (/not applicable/i.test(msg)) setCpaBlocked(true);
      else setProxyErr(msg);
    }
  }, [node.id]);

  useEffect(() => {
    void loadProxy();
  }, [loadProxy]);

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

  async function handleTestProxy() {
    setProxyBusy(true);
    setProxyErr(null);
    setProxyMsg(null);
    try {
      const r = await testNodeProxy(node.id, proxyRaw.trim());
      if (r.ok) {
        setProxyTested(true);
        setProxyMsg(`代理可用，出口 IP: ${r.egressIp ?? '未知'}`);
      } else {
        setProxyTested(false);
        setProxyErr(r.error ?? '代理测试失败');
      }
    } catch (e) {
      setProxyTested(false);
      setProxyErr(e instanceof Error ? e.message : '测试失败');
    } finally {
      setProxyBusy(false);
    }
  }

  async function handleSaveProxy(raw: string) {
    setProxyBusy(true);
    setProxyErr(null);
    setProxyMsg(null);
    try {
      const r = await setNodeProxy(node.id, raw);
      setProxyLoaded(raw);
      if (r.restarting) {
        setProxyRestarting(true);
        setProxyMsg('已保存，节点正在重启激活代理(约 10–20s),起来后再开始 OAuth 授权。');
      } else {
        setProxyMsg(raw ? '已保存。' : '已清除。');
      }
      onSuccess();
    } catch (e) {
      setProxyErr(e instanceof Error ? e.message : '保存失败');
    } finally {
      setProxyBusy(false);
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
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
          {/* --- Egress proxy section (节点级出口代理) --- */}
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-ink">🌐 出口代理</h3>
              {proxyLoaded && !cpaBlocked && (
                <span className="text-xs text-muted">当前: {maskProxy(proxyLoaded)}</span>
              )}
            </div>
            {cpaBlocked ? (
              <p className="text-xs text-muted">CPA 节点不适用（账号经 CPA 管理 API 管理）。</p>
            ) : (
              <div className="space-y-2">
                <p className="text-xs text-muted">
                  粘贴 <code className="text-ink">socks5://host:port:user:pass</code>
                  。保存后该节点所有出站（含 OAuth 换 token、后续 API）都走此代理，出口 IP 固定。先测试通过再保存。
                </p>
                <textarea
                  value={proxyRaw}
                  onChange={(e) => {
                    setProxyRaw(e.target.value);
                    setProxyTested(false);
                  }}
                  placeholder="socks5://host:port:user:pass"
                  rows={2}
                  className="w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink font-mono
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => {
                      void handleTestProxy();
                    }}
                    disabled={proxyBusy || !proxyRaw.trim()}
                    className="px-3 py-1.5 text-xs font-medium border border-accent text-accent rounded-lg
                               hover:bg-accent/10 disabled:opacity-50 transition"
                  >
                    {proxyBusy ? '…' : '测试'}
                  </button>
                  <button
                    onClick={() => {
                      void handleSaveProxy(proxyRaw.trim());
                    }}
                    disabled={proxyBusy || !proxyTested || !proxyRaw.trim()}
                    title={!proxyTested ? '先测试通过才能保存' : ''}
                    className="px-3 py-1.5 text-xs font-medium bg-accent text-white rounded-lg
                               hover:bg-accent/80 disabled:opacity-50 transition"
                  >
                    保存
                  </button>
                  {proxyLoaded && (
                    <button
                      onClick={() => {
                        setProxyRaw('');
                        void handleSaveProxy('');
                      }}
                      disabled={proxyBusy}
                      className="px-3 py-1.5 text-xs font-medium border border-line text-muted rounded-lg
                                 hover:text-ink disabled:opacity-50 transition"
                    >
                      清除
                    </button>
                  )}
                </div>
                {proxyErr && <p className="text-xs text-err">{proxyErr}</p>}
                {proxyMsg && <p className="text-xs text-ok">{proxyMsg}</p>}
                {proxyRestarting && (
                  <button
                    onClick={() => {
                      setProxyRestarting(false);
                      void loadProxy();
                      onSuccess();
                    }}
                    className="text-xs text-accent hover:underline"
                  >
                    刷新状态
                  </button>
                )}
              </div>
            )}
          </div>

          {/* --- OAuth section --- */}
          <div className="space-y-3">
            <h3 className="text-sm font-semibold text-ink">OAuth 授权</h3>

            {step === 'idle' && (
              <div className="space-y-2">
                <button
                  onClick={() => { void handleStartOAuth(); }}
                  disabled={oauthLoading || !proxyLoaded.trim()}
                  title={!proxyLoaded.trim() ? '请先配置并保存出口代理' : undefined}
                  className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                             hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
                >
                  {oauthLoading ? '生成中…' : '开始 OAuth 授权'}
                </button>
                {!proxyLoaded.trim() && (
                  <p className="text-xs text-amber-400">请先在上方配置并保存出口代理，才能开始 OAuth 授权。</p>
                )}
                {oauthErr && <p className="text-xs text-err">{oauthErr}</p>}
              </div>
            )}

            {step === 'authorizing' && (
              <div className="space-y-3">
                <p className="text-xs text-muted">
                  1. 复制下方链接到浏览器打开完成授权，然后把回调 code 粘贴到下方。
                </p>
                <div className="flex items-start gap-2">
                  <code className="flex-1 text-xs text-accent break-all select-all bg-bg border border-line rounded-lg px-3 py-2 leading-relaxed">
                    {authorizeUrl}
                  </code>
                  <button
                    type="button"
                    onClick={() => { copyToClipboard(authorizeUrl); setCopied(true); setTimeout(() => setCopied(false), 1500); }}
                    className="shrink-0 text-xs px-3 py-2 rounded-lg bg-accent/15 text-accent hover:bg-accent/25 transition"
                  >
                    {copied ? '已复制' : '复制'}
                  </button>
                </div>
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
                      {p.imported || importResults[p.id] === '导入成功' || importResults[p.id] === '已存在' ? (
                        <span className="text-xs text-ok">已导入</span>
                      ) : importResults[p.id] ? (
                        <span className="text-xs text-err">{importResults[p.id]}</span>
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

// One row per target host in a batch provision run.
interface ProvisionRow {
  host: string;
  user: string;
  password: string;
  jobId: string | null;
  status: 'pending' | 'running' | 'success' | 'failed' | 'error';
  step: string;
  log: string;
  error: string | null;
}

function ProvisionWizard({ onSuccess }: ProvisionWizardProps) {
  const [open, setOpen] = useState(false);
  const [hostsText, setHostsText] = useState('');
  const [defaultUser, setDefaultUser] = useState('root');
  const [defaultPassword, setDefaultPassword] = useState('');
  const [ownerId, setOwnerId] = useState('');
  const [rows, setRows] = useState<ProvisionRow[]>([]);
  const [started, setStarted] = useState(false);
  const [running, setRunning] = useState(false);
  const [parseErr, setParseErr] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const rowsRef = useRef<ProvisionRow[]>([]);
  const succeededRef = useRef(false);

  function clearTimer() {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }

  // Keep rowsRef in sync so the poll interval always reads the latest rows.
  useEffect(() => { rowsRef.current = rows; }, [rows]);

  useEffect(() => {
    return () => clearTimer();
  }, []);

  // When every row reaches a terminal state, stop polling and (once) refresh the
  // node list if at least one host was provisioned successfully.
  useEffect(() => {
    if (!started || rows.length === 0) return;
    const active = rows.some((r) => r.status === 'running' || r.status === 'pending');
    if (!active) {
      clearTimer();
      setRunning(false);
      if (!succeededRef.current && rows.some((r) => r.status === 'success')) {
        succeededRef.current = true;
        onSuccess();
      }
    }
  }, [rows, started, onSuccess]);

  // Parse the textarea into rows. Each non-empty line is one host; fields are
  // separated by whitespace or comma:
  //   ip                 → uses the default user + default password
  //   ip 密码            → host + password (user = default)
  //   ip 用户名 密码     → all three
  function parseHosts(): ProvisionRow[] | null {
    const lines = hostsText.split('\n').map((l) => l.trim()).filter(Boolean);
    if (lines.length === 0) { setParseErr('请至少输入一台主机'); return null; }
    const du = defaultUser.trim() || 'root';
    const out: ProvisionRow[] = [];
    const seen = new Set<string>();
    for (const line of lines) {
      const parts = line.split(/[\s,]+/).filter(Boolean);
      let host = '';
      let user = du;
      let password = defaultPassword;
      if (parts.length === 1) {
        host = parts[0];
      } else if (parts.length === 2) {
        host = parts[0];
        password = parts[1];
      } else {
        host = parts[0];
        user = parts[1];
        password = parts.slice(2).join(' ');
      }
      if (!host) continue;
      if (seen.has(host)) continue; // dedupe repeated IPs
      seen.add(host);
      if (!password) {
        setParseErr(`主机 ${host} 缺少密码（该行未写密码，且未设置默认密码）`);
        return null;
      }
      out.push({ host, user: user || 'root', password, jobId: null, status: 'pending', step: '', log: '', error: null });
    }
    if (out.length === 0) { setParseErr('未解析到有效主机'); return null; }
    return out;
  }

  async function handleStart(e: React.FormEvent) {
    e.preventDefault();
    setParseErr(null);
    const parsed = parseHosts();
    if (!parsed) return;
    setRows(parsed);
    setStarted(true);
    setRunning(true);
    succeededRef.current = false;

    const owner = ownerId.trim();
    const withJobs = await Promise.all(
      parsed.map(async (r): Promise<ProvisionRow> => {
        try {
          const res = await startProvision({
            host: r.host,
            user: r.user,
            password: r.password,
            ...(owner ? { ownerId: owner } : {}),
          });
          return { ...r, jobId: res.jobId, status: 'running' };
        } catch (startErr) {
          return { ...r, status: 'error', error: startErr instanceof Error ? startErr.message : '提交失败' };
        }
      }),
    );
    setRows(withJobs);
    rowsRef.current = withJobs;

    if (!withJobs.some((r) => r.jobId && r.status === 'running')) {
      setRunning(false);
      return;
    }

    timerRef.current = setInterval(() => {
      void pollAll();
    }, 2000);
  }

  async function pollAll() {
    const cur = rowsRef.current;
    const pending = cur
      .map((r, i) => ({ r, i }))
      .filter(({ r }) => r.jobId && (r.status === 'running' || r.status === 'pending'));
    if (pending.length === 0) { clearTimer(); return; }
    const updates = await Promise.all(
      pending.map(async ({ r, i }) => {
        try {
          const job = await getProvision(r.jobId as string);
          return { i, patch: { status: job.status, step: job.step ?? '', log: job.log ?? '' } as Partial<ProvisionRow> };
        } catch (pollErr) {
          return { i, patch: { status: 'error', error: pollErr instanceof Error ? pollErr.message : '轮询失败' } as Partial<ProvisionRow> };
        }
      }),
    );
    setRows((prev) => {
      const next = [...prev];
      for (const u of updates) next[u.i] = { ...next[u.i], ...u.patch };
      return next;
    });
  }

  function handleReset() {
    clearTimer();
    setRows([]);
    setStarted(false);
    setRunning(false);
    setParseErr(null);
    setExpanded(new Set());
    succeededRef.current = false;
  }

  function handleClose() {
    handleReset();
    setOpen(false);
    setHostsText('');
    setDefaultUser('root');
    setDefaultPassword('');
    setOwnerId('');
  }

  function toggleExpanded(i: number) {
    setExpanded((prev) => {
      const s = new Set(prev);
      if (s.has(i)) s.delete(i); else s.add(i);
      return s;
    });
  }

  const doneCount = rows.filter((r) => r.status === 'success').length;
  const failCount = rows.filter((r) => r.status === 'failed' || r.status === 'error').length;
  const runCount = rows.filter((r) => r.status === 'running' || r.status === 'pending').length;

  function rowStatusText(r: ProvisionRow): string {
    switch (r.status) {
      case 'success': return '成功';
      case 'failed': return '失败';
      case 'error': return '错误';
      case 'running': return '进行中';
      default: return '等待';
    }
  }
  function rowStatusColor(r: ProvisionRow): string {
    if (r.status === 'success') return 'text-ok';
    if (r.status === 'failed' || r.status === 'error') return 'text-err';
    return 'text-muted';
  }

  return (
    <div className="bg-surface border border-line rounded-xl overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between px-4 py-3 text-sm font-semibold text-ink hover:bg-line/20 transition"
      >
        <span>一键开通节点（支持批量）</span>
        <span className="text-muted text-xs">{open ? '收起 ▲' : '展开 ▼'}</span>
      </button>

      {open && (
        <div className="border-t border-line p-4 space-y-4">
          {!started && (
            <form onSubmit={(e) => { void handleStart(e); }} className="space-y-3">
              <div>
                <label className="block text-xs text-muted mb-1">主机列表（每行一台，可批量粘贴）</label>
                <textarea
                  value={hostsText}
                  onChange={(e) => setHostsText(e.target.value)}
                  rows={6}
                  placeholder={'每行一台，支持以下格式（空格或逗号分隔）：\nIP                    → 用下方默认用户名与密码\nIP 密码               → 该行单独密码\nIP 用户名 密码        → 该行单独用户名+密码\n\n例：\n1.2.3.4\n5.6.7.8 mypassword\n9.9.9.9 ubuntu mypassword'}
                  className="w-full bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink font-mono
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
              </div>
              <div className="flex flex-col sm:flex-row gap-2">
                <input
                  type="text"
                  value={defaultUser}
                  onChange={(e) => setDefaultUser(e.target.value)}
                  placeholder="默认 SSH 用户名（默认 root）"
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
                <input
                  type="password"
                  value={defaultPassword}
                  onChange={(e) => setDefaultPassword(e.target.value)}
                  placeholder="默认 SSH 密码（未在行内指定时使用）"
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
                <input
                  type="text"
                  value={ownerId}
                  onChange={(e) => setOwnerId(e.target.value)}
                  placeholder="归属用户 ID（选填，作用于全部）"
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
              </div>
              <div className="flex gap-2">
                <button
                  type="submit"
                  disabled={!hostsText.trim()}
                  className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                             hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
                >
                  开始批量开通
                </button>
                <button
                  type="button"
                  onClick={handleClose}
                  className="px-4 py-2 text-sm font-medium bg-bg border border-line text-muted rounded-lg hover:text-ink transition"
                >
                  取消
                </button>
              </div>
              {parseErr && <p className="text-xs text-err">{parseErr}</p>}
            </form>
          )}

          {started && (
            <div className="space-y-3">
              <div className="flex items-center gap-3 flex-wrap">
                <span className="text-sm font-medium text-ink">批量进度</span>
                <span className="text-xs text-muted">共 {rows.length} 台</span>
                <span className="text-xs text-ok">成功 {doneCount}</span>
                <span className="text-xs text-err">失败 {failCount}</span>
                <span className="text-xs text-muted">进行中 {runCount}</span>
                {running && <span className="text-xs text-muted animate-pulse">轮询中…</span>}
              </div>

              <div className="space-y-2 max-h-96 overflow-y-auto">
                {rows.map((r, i) => (
                  <div key={r.host} className="border border-line rounded-lg bg-bg">
                    <div className="flex items-center gap-2 px-3 py-2 flex-wrap">
                      <span className="text-xs font-mono text-ink">{r.host}</span>
                      <span className={`text-xs font-medium ${rowStatusColor(r)}`}>{rowStatusText(r)}</span>
                      {r.step && <span className="text-xs text-muted">· {r.step}</span>}
                      {r.error && <span className="text-xs text-err">· {r.error}</span>}
                      {(r.log || r.error) && (
                        <button
                          type="button"
                          onClick={() => toggleExpanded(i)}
                          className="ml-auto text-xs text-accent hover:underline"
                        >
                          {expanded.has(i) ? '收起日志' : '日志'}
                        </button>
                      )}
                    </div>
                    {expanded.has(i) && (
                      <pre className="border-t border-line px-3 py-2 text-[11px] font-mono text-ink
                                      max-h-48 overflow-y-auto whitespace-pre-wrap break-all">
                        {r.log || r.error || '(无日志)'}
                      </pre>
                    )}
                  </div>
                ))}
              </div>

              {!running && (
                <div className="flex gap-3 items-center">
                  <span className="text-xs font-medium text-ink">
                    全部完成：{doneCount} 成功，{failCount} 失败
                  </span>
                  <button
                    type="button"
                    onClick={handleReset}
                    className="text-xs text-accent hover:underline"
                  >
                    再装一批
                  </button>
                  <button
                    type="button"
                    onClick={handleClose}
                    className="text-xs text-muted hover:text-ink transition"
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
  const [users, setUsers] = useState<UserRow[]>([]);
  const [kind, setKind] = useState<'meridian' | 'cpa'>('meridian');
  const [mgmtKey, setMgmtKey] = useState('');
  const [passthrough, setPassthrough] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    listUsers().then((us) => {
      setUsers(us);
      const t = us.find((u) => u.username === 'yanghao');
      if (t) setOwnerId(t.id);
    }).catch(() => setUsers([]));
  }, []);

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
        ...(ownerId.trim() ? { accountOwnerId: ownerId.trim() } : {}),
        ...(kind === 'cpa' && mgmtKey.trim() ? { mgmtKey: mgmtKey.trim() } : {}),
        ...(kind === 'cpa' && passthrough ? { passthrough: true } : {}),
      });
      setBaseUrl('');
      setApiKey('');
      setOwnerId('');
      setMgmtKey('');
      setPassthrough(true);
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
        <select
          value={ownerId}
          onChange={(e) => setOwnerId(e.target.value)}
          className={inputCls}
          title="号库归属用户"
        >
          <option value="">超级管理员（全局）</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>{u.username}</option>
          ))}
        </select>
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
          <label className="flex items-center gap-2 mt-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={passthrough}
              onChange={(e) => setPassthrough(e.target.checked)}
              className="w-3.5 h-3.5 accent-accent"
            />
            <span className="text-[11px] text-ink">透传(不钉账户·仅 cpa)</span>
          </label>
          <p className="text-[11px] text-muted mt-0.5">cpa 节点开启后不发送 X-CLIProxy-Account，由 CLIProxyAPI 自行轮换号池</p>
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
  onUpdateCode: () => void;
  onCopyIPs: () => void;
  onDeleteAll: () => void;
  batchRunning: boolean;
  batchResult: string | null;
  batchHasError: boolean;
}

function BatchBar({
  selectedCount,
  onEnableAll,
  onDisableAll,
  onRefreshAll,
  onUpdateCode,
  onCopyIPs,
  onDeleteAll,
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
        <button
          onClick={onUpdateCode}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-accent rounded-lg
                     hover:bg-line/30 disabled:opacity-50 disabled:cursor-not-allowed transition"
          title="SSH 进选中节点 git pull + 重建到最新代码"
        >
          更新代码
        </button>
        <button
          onClick={onCopyIPs}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-ink rounded-lg
                     hover:bg-line/30 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          复制 IP
        </button>
        <button
          onClick={() => { if (confirm(`确认删除选中的 ${selectedCount} 个节点？此操作不可撤回。`)) onDeleteAll(); }}
          disabled={batchRunning}
          className="px-3 py-1.5 text-xs font-medium bg-red-700 text-white rounded-lg
                     hover:bg-red-800 disabled:opacity-50 disabled:cursor-not-allowed transition"
        >
          批量删除
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
  return NODE_STATUS_META[nodeStatusValue(node)].order;
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
  onTogglePassthrough,
  onOpenOAuth,
  onRefresh,
}: {
  node: NodeRecord;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onDelete: (id: string) => void;
  onToggleEnabled: (node: NodeRecord) => void;
  onTogglePassthrough: (node: NodeRecord) => void;
  onOpenOAuth: (node: NodeRecord) => void;
  onRefresh: () => void;
}) {
  const [deleting, setDeleting] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [showEdit, setShowEdit] = useState(false);

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
            onClick={() => setShowEdit(true)}
            className="text-xs font-medium text-cyan-400 hover:text-cyan-300 transition"
          >
            编辑
          </button>
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
          {node.kind?.toLowerCase() === 'cpa' && (
            <button
              onClick={() => onTogglePassthrough(node)}
              className={`text-xs transition ${
                node.passthrough
                  ? 'text-ok hover:text-ok/70'
                  : 'text-muted hover:text-ink'
              }`}
              title={node.passthrough ? '透传已开' : '透传已关'}
            >
              透传
            </button>
          )}
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
        {showEdit && (
          <NodeEditModal node={node} onClose={() => setShowEdit(false)} onSuccess={() => { setShowEdit(false); onRefresh(); }} />
        )}
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
  onTogglePassthrough,
  onOpenOAuth,
}: {
  node: NodeRecord;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onDelete: (id: string) => void;
  onToggleEnabled: (node: NodeRecord) => void;
  onTogglePassthrough: (node: NodeRecord) => void;
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
        {node.kind?.toLowerCase() === 'cpa' && (
          <button
            onClick={() => onTogglePassthrough(node)}
            className={`text-xs transition ${
              node.passthrough
                ? 'text-ok hover:text-ok/70'
                : 'text-muted hover:text-ink'
            }`}
            title={node.passthrough ? '透传已开' : '透传已关'}
          >
            透传
          </button>
        )}
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
const PAGE_SIZE_OPTIONS = [100, 200, 500, 1000];

export default function Nodes() {
  const [nodes, setNodes] = useState<NodeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Search
  const [search, setSearch] = useState('');

  // Status filter ('' = all)
  const [statusFilter, setStatusFilter] = useState<NodeStatus | ''>('');

  // Sort
  const [sortKey, setSortKey] = useState<SortKey>('name');
  const [sortDir, setSortDir] = useState<SortDir>('asc');

  // Pagination
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE_OPTIONS[0]);

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
    if (statusFilter && nodeStatusValue(n) !== statusFilter) return false;
    if (!search.trim()) return true;
    const q = search.trim().toLowerCase();
    return (
      n.name.toLowerCase().includes(q) ||
      n.baseUrl.toLowerCase().includes(q) ||
      (n.email ?? '').toLowerCase().includes(q)
    );
  });

  const sorted = sortNodes(filtered, sortKey, sortDir);
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize));
  const safePage = Math.min(page, totalPages);
  const pageItems = sorted.slice((safePage - 1) * pageSize, safePage * pageSize);

  // Reset to page 1 when filter or page size changes
  useEffect(() => {
    setPage(1);
  }, [search, statusFilter, sortKey, sortDir, pageSize]);

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

  // Banned nodes across the whole list (not just current page) — powers the
  // "选择封号" quick-select so batch ops can target every banned node at once.
  const bannedNodes = nodes.filter((n) => nodeStatusValue(n) === 'banned');
  const allBannedSelected =
    bannedNodes.length > 0 && bannedNodes.every((n) => selected.has(n.id));

  function handleSelectBanned() {
    setSelected((prev) => {
      const s = new Set(prev);
      if (allBannedSelected) {
        // toggle off: clear the banned selection
        bannedNodes.forEach((n) => s.delete(n.id));
      } else {
        bannedNodes.forEach((n) => s.add(n.id));
      }
      return s;
    });
  }

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

  function handleBatchUpdateCode() {
    const password = window.prompt('输入选中节点的 SSH 密码（用户名默认 root）：\n将 SSH 进每台 git pull + 重建到最新代码（约 1-2 分钟/台）');
    if (!password) return;
    void runBatch((id) => updateNodeCode(id, { user: 'root', password }).then(() => undefined), '更新代码');
  }

  function handleCopyIPs() {
    const ips = nodes
      .filter((n) => selected.has(n.id))
      .map((n) => nodeIP(n.baseUrl))
      .filter(Boolean);
    if (ips.length === 0) {
      setBatchHasError(true);
      setBatchResult('未提取到 IP');
      return;
    }
    copyToClipboard(ips.join('\n'));
    setBatchHasError(false);
    setBatchResult(`已复制 ${ips.length} 个 IP`);
  }

  function handleBatchDelete() {
    void runBatch(async (id) => {
      await deleteNode(id);
      handleDelete(id);
    }, '批量删除');
    setSelected(new Set());
  }

  function handleToggleEnabled(node: NodeRecord) {
    void setNodeEnabled(node.id, !node.enabled).then(() => {
      setNodes((prev) =>
        prev.map((n) => (n.id === node.id ? { ...n, enabled: !n.enabled } : n)),
      );
    });
  }

  function handleTogglePassthrough(node: NodeRecord) {
    void setNodePassthrough(node.id, !node.passthrough).then(() => {
      setNodes(prev => prev.map(n => n.id === node.id ? { ...n, passthrough: !node.passthrough } : n));
    });
  }

  const [importing, setImporting] = useState(false);
  const [importMsg, setImportMsg] = useState<string | null>(null);

  async function handleBatchImport() {
    setImporting(true);
    setImportMsg(null);
    try {
      const res = await batchAutoImport();
      setImportMsg(`导入 ${res.imported} 个, 失败 ${res.failed} 个`);
      void fetchNodes();
    } catch (e) {
      setImportMsg(e instanceof Error ? e.message : '导入失败');
    } finally {
      setImporting(false);
    }
  }

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold text-ink">节点</h1>
        <div className="flex items-center gap-3">
          <button
            onClick={() => { void handleBatchImport(); }}
            disabled={importing}
            className="px-3 py-1.5 text-xs font-medium bg-accent text-white rounded-lg hover:bg-accent/80 disabled:opacity-50 transition"
          >
            {importing ? '导入中…' : '一键导入'}
          </button>
          {importMsg && <span className="text-xs text-muted">{importMsg}</span>}
          <button
            onClick={() => { void fetchNodes(); }}
            className="text-xs text-accent hover:underline"
          >
            刷新
          </button>
        </div>
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
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as NodeStatus | '')}
          className="bg-surface border border-line rounded-lg px-3 py-2 text-sm text-ink focus:outline-none focus:border-accent transition"
        >
          <option value="">全部状态</option>
          <option value="ok">正常</option>
          <option value="banned">封号</option>
          <option value="noauth">未上传凭证</option>
          <option value="disabled">停用</option>
        </select>
        {bannedNodes.length > 0 && (
          <button
            onClick={handleSelectBanned}
            title="一键选中/取消选中所有封号节点"
            className={`px-3 py-2 text-xs font-medium rounded-lg border transition whitespace-nowrap
                        ${allBannedSelected
                          ? 'bg-err text-white border-err hover:bg-err/80'
                          : 'bg-surface text-err border-err/40 hover:bg-err/10'}`}
          >
            {allBannedSelected ? '取消选中封号' : `选择封号 (${bannedNodes.length})`}
          </button>
        )}
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
          onUpdateCode={handleBatchUpdateCode}
          onCopyIPs={handleCopyIPs}
          onDeleteAll={handleBatchDelete}
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
                    onTogglePassthrough={handleTogglePassthrough}
                    onOpenOAuth={(n) => setOauthNode(n)}
                    onRefresh={() => { void fetchNodes(); }}
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
                onTogglePassthrough={handleTogglePassthrough}
                onOpenOAuth={(n) => setOauthNode(n)}
              />
            ))}
          </div>

          {/* Pagination */}
          <div className="flex items-center justify-between gap-3 pt-2">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted">每页</span>
              <select
                value={pageSize}
                onChange={(e) => setPageSize(Number(e.target.value))}
                className="bg-surface border border-line rounded-lg px-2 py-1.5 text-xs text-ink
                           focus:outline-none focus:border-accent transition"
              >
                {PAGE_SIZE_OPTIONS.map((n) => (
                  <option key={n} value={n}>{n}</option>
                ))}
              </select>
              <span className="text-xs text-muted">条</span>
            </div>
            <span className="text-xs text-muted">
              第 {safePage} / {totalPages} 页，共 {filtered.length} 条
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={safePage === 1}
                className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-ink rounded-lg
                           hover:bg-line/30 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                上一页
              </button>
              <button
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                disabled={safePage === totalPages}
                className="px-3 py-1.5 text-xs font-medium bg-surface border border-line text-ink rounded-lg
                           hover:bg-line/30 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                下一页
              </button>
            </div>
          </div>
        </>
      )}

      {/* OAuth wizard modal */}
      {oauthNode && (
        <OAuthWizardModal
          node={oauthNode}
          onClose={() => { const cid = oauthNode.id; setOauthNode(null); void (async () => { try { await refreshNode(cid); } catch { /* best-effort */ } await fetchNodes(); })(); }}
          onSuccess={() => { const id = oauthNode.id; void (async () => { try { await refreshNode(id); } catch { /* probe best-effort */ } await fetchNodes(); })(); }}
        />
      )}
    </div>
  );
}
