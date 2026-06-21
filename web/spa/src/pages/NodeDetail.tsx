// ============================================================
// Tower SPA — Node Detail page (/nodes/:id)
// Shows node info, SDK features (editable), refresh token,
// and enable/disable toggle.
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import { listNodes, getNodeFeatures, patchNodeFeatures, refreshNode, setNodeEnabled, getNodeTelemetry } from '../api';
import type { NodeRecord, NodeTelemetry } from '../types';

// ------------------------------------------------------------------
// Feature editor for a single adapter
// ------------------------------------------------------------------
interface AdapterEditorProps {
  nodeId: string;
  adapter: string;
  fields: Record<string, unknown>;
  onSaved: () => void;
}

function AdapterEditor({ nodeId, adapter, fields, onSaved }: AdapterEditorProps) {
  const [draft, setDraft] = useState<Record<string, unknown>>({ ...fields });
  const [saving, setSaving] = useState(false);
  const [saveErr, setSaveErr] = useState<string | null>(null);
  const [saveOk, setSaveOk] = useState(false);

  // Sync draft when fields change externally
  useEffect(() => {
    setDraft({ ...fields });
  }, [fields]);

  async function handleSave() {
    setSaving(true);
    setSaveErr(null);
    setSaveOk(false);
    try {
      await patchNodeFeatures(nodeId, adapter, draft);
      setSaveOk(true);
      setTimeout(() => setSaveOk(false), 2000);
      onSaved();
    } catch (err) {
      setSaveErr(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  }

  function handleChange(key: string, value: unknown) {
    setDraft((prev) => ({ ...prev, [key]: value }));
  }

  const keys = Object.keys(fields);

  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-3">
      <h3 className="text-sm font-semibold text-ink">{adapter}</h3>

      {keys.length === 0 && (
        <p className="text-xs text-muted">无可编辑字段</p>
      )}

      <div className="space-y-2">
        {keys.map((key) => {
          const val = draft[key];
          const fieldType = typeof val;

          return (
            <div key={key} className="flex items-center gap-3">
              <label className="text-xs text-muted w-40 shrink-0">{key}</label>
              {fieldType === 'boolean' ? (
                <button
                  type="button"
                  onClick={() => handleChange(key, !val)}
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                    val ? 'bg-accent' : 'bg-line'
                  }`}
                >
                  <span
                    className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform ${
                      val ? 'translate-x-4' : 'translate-x-0.5'
                    }`}
                  />
                </button>
              ) : (
                <input
                  type={fieldType === 'number' ? 'number' : 'text'}
                  value={String(val ?? '')}
                  onChange={(e) => {
                    const raw = e.target.value;
                    handleChange(key, fieldType === 'number' ? Number(raw) : raw);
                  }}
                  className="flex-1 bg-bg border border-line rounded-lg px-3 py-1.5 text-sm text-ink
                             placeholder:text-muted focus:outline-none focus:border-accent transition"
                />
              )}
            </div>
          );
        })}
      </div>

      {keys.length > 0 && (
        <div className="flex items-center gap-3 pt-1">
          <button
            onClick={() => { void handleSave(); }}
            disabled={saving}
            className="px-4 py-1.5 text-sm font-medium bg-accent text-white rounded-lg
                       hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {saving ? '保存中…' : '保存'}
          </button>
          {saveOk && <span className="text-xs text-ok">已保存</span>}
          {saveErr && <span className="text-xs text-err">{saveErr}</span>}
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------
// NodeDetail page
// ------------------------------------------------------------------
export default function NodeDetail() {
  const { id } = useParams<{ id: string }>();
  const [node, setNode] = useState<NodeRecord | null>(null);
  const [loadingNode, setLoadingNode] = useState(true);
  const [nodeErr, setNodeErr] = useState<string | null>(null);

  const [features, setFeatures] = useState<Record<string, Record<string, unknown>> | null>(null);
  const [loadingFeatures, setLoadingFeatures] = useState(false);
  const [featuresErr, setFeaturesErr] = useState<string | null>(null);

  const [refreshing, setRefreshing] = useState(false);
  const [refreshMsg, setRefreshMsg] = useState<string | null>(null);
  const [refreshError, setRefreshError] = useState(false);

  const [togglingEnabled, setTogglingEnabled] = useState(false);
  const [enableErr, setEnableErr] = useState<string | null>(null);

  const [telemetry, setTelemetry] = useState<NodeTelemetry | null>(null);
  const [loadingTelemetry, setLoadingTelemetry] = useState(false);
  const [telemetryErr, setTelemetryErr] = useState<string | null>(null);

  // ---- load node ----
  const fetchNode = useCallback(async () => {
    if (!id) return;
    setLoadingNode(true);
    setNodeErr(null);
    try {
      const nodes = await listNodes();
      const found = nodes.find((n) => n.id === id);
      if (!found) throw new Error(`找不到节点 ${id}`);
      setNode(found);
    } catch (err) {
      setNodeErr(err instanceof Error ? err.message : '加载失败');
    } finally {
      setLoadingNode(false);
    }
  }, [id]);

  // ---- load features ----
  const fetchFeatures = useCallback(async () => {
    if (!id) return;
    setLoadingFeatures(true);
    setFeaturesErr(null);
    try {
      const data = await getNodeFeatures(id);
      setFeatures(data);
    } catch (err) {
      setFeaturesErr(err instanceof Error ? err.message : '加载 SDK 设置失败');
    } finally {
      setLoadingFeatures(false);
    }
  }, [id]);

  // ---- load telemetry ----
  const fetchTelemetry = useCallback(async () => {
    if (!id) return;
    setLoadingTelemetry(true);
    setTelemetryErr(null);
    try {
      const data = await getNodeTelemetry(id);
      setTelemetry(data);
    } catch (err) {
      setTelemetryErr(err instanceof Error ? err.message : '加载遥测失败');
    } finally {
      setLoadingTelemetry(false);
    }
  }, [id]);

  useEffect(() => {
    void fetchNode();
    void fetchFeatures();
    void fetchTelemetry();
  }, [fetchNode, fetchFeatures, fetchTelemetry]);

  // ---- refresh token ----
  async function handleRefresh() {
    if (!id) return;
    setRefreshing(true);
    setRefreshMsg(null);
    setRefreshError(false);
    try {
      await refreshNode(id);
      setRefreshError(false);
      setRefreshMsg('Token 已刷新');
      setTimeout(() => setRefreshMsg(null), 3000);
    } catch (err) {
      setRefreshError(true);
      setRefreshMsg(err instanceof Error ? err.message : '刷新失败');
    } finally {
      setRefreshing(false);
    }
  }

  // ---- toggle enabled ----
  async function handleToggleEnabled() {
    if (!node || !id) return;
    setTogglingEnabled(true);
    setEnableErr(null);
    try {
      await setNodeEnabled(id, !node.enabled);
      setNode((prev) => prev ? { ...prev, enabled: !prev.enabled } : prev);
    } catch (err) {
      setEnableErr(err instanceof Error ? err.message : '操作失败');
    } finally {
      setTogglingEnabled(false);
    }
  }

  // ------------------------------------------------------------------
  if (loadingNode) {
    return (
      <div className="p-4 md:p-6 flex items-center justify-center min-h-64">
        <span className="text-muted animate-pulse">加载中…</span>
      </div>
    );
  }

  if (nodeErr || !node) {
    return (
      <div className="p-4 md:p-6 space-y-4">
        <Link to="/nodes" className="text-sm text-accent hover:underline">← 返回节点列表</Link>
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">
          {nodeErr ?? '节点未找到'}
        </div>
      </div>
    );
  }

  const statusLabel = node.status ? node.status : node.enabled ? '运行中' : '已停用';

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2">
        <Link to="/nodes" className="text-sm text-accent hover:underline">节点列表</Link>
        <span className="text-muted text-sm">/</span>
        <span className="text-sm text-ink font-medium">{node.name}</span>
      </div>

      {/* Node info card */}
      <div className="bg-surface border border-line rounded-xl p-5 space-y-4">
        <div className="flex items-start justify-between gap-4 flex-wrap">
          <div>
            <h1 className="text-xl font-semibold text-ink">{node.name}</h1>
            <p className="text-sm text-muted mt-0.5 break-all">{node.baseUrl}</p>
          </div>

          {/* Enable/Disable toggle */}
          <div className="flex flex-col items-end gap-1">
            <div className="flex items-center gap-3">
              <span className="text-sm text-muted">{node.enabled ? '启用' : '停用'}</span>
              <button
                type="button"
                onClick={() => { void handleToggleEnabled(); }}
                disabled={togglingEnabled}
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors disabled:opacity-50 ${
                  node.enabled ? 'bg-accent' : 'bg-line'
                }`}
              >
                <span
                  className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
                    node.enabled ? 'translate-x-6' : 'translate-x-1'
                  }`}
                />
              </button>
            </div>
            {enableErr && <span className="text-xs text-err">{enableErr}</span>}
          </div>
        </div>

        {/* Status row */}
        <div className="flex flex-wrap gap-6 text-sm">
          <div>
            <span className="text-xs text-muted uppercase tracking-wide">状态</span>
            <div className={`mt-0.5 flex items-center gap-1.5 ${node.enabled ? 'text-ok' : 'text-muted'}`}>
              <span className={`w-1.5 h-1.5 rounded-full ${node.enabled ? 'bg-ok' : 'bg-muted'}`} />
              {statusLabel}
            </div>
          </div>
          <div>
            <span className="text-xs text-muted uppercase tracking-wide">节点 ID</span>
            <div className="mt-0.5 text-ink font-mono text-xs">{node.id}</div>
          </div>
          {node.version && (
            <div>
              <span className="text-xs text-muted uppercase tracking-wide">版本</span>
              <div className="mt-0.5 text-ink text-xs">{node.version}</div>
            </div>
          )}
        </div>

        {/* Refresh token button + 控制台 */}
        <div className="flex items-center gap-3 pt-1 border-t border-line flex-wrap">
          <button
            onClick={() => { void handleRefresh(); }}
            disabled={refreshing}
            className="px-4 py-2 text-sm font-medium bg-surface border border-line rounded-lg
                       hover:bg-line/30 disabled:opacity-50 disabled:cursor-not-allowed transition text-ink"
          >
            {refreshing ? '刷新中…' : '刷新 token'}
          </button>
          <a
            href={node.baseUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="px-4 py-2 text-sm font-medium bg-surface border border-line rounded-lg
                       hover:bg-line/30 transition text-ink"
          >
            控制台
          </a>
          {refreshMsg && (
            <span className={`text-xs ${refreshError ? 'text-err' : 'text-ok'}`}>
              {refreshMsg}
            </span>
          )}
        </div>
      </div>

      {/* 遥测 / 健康 */}
      <div className="bg-surface border border-line rounded-xl p-5 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold text-ink uppercase tracking-wide">遥测 / 健康</h2>
          <button
            onClick={() => { void fetchTelemetry(); }}
            disabled={loadingTelemetry}
            className="px-3 py-1 text-xs font-medium bg-surface border border-line rounded-lg
                       hover:bg-line/30 disabled:opacity-50 disabled:cursor-not-allowed transition text-ink"
          >
            {loadingTelemetry ? '刷新中…' : '刷新'}
          </button>
        </div>

        {loadingTelemetry && (
          <div className="flex items-center justify-center min-h-20">
            <span className="text-muted animate-pulse text-sm">加载遥测数据…</span>
          </div>
        )}

        {!loadingTelemetry && telemetryErr && (
          <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">
            {telemetryErr}
          </div>
        )}

        {!loadingTelemetry && !telemetryErr && telemetry && (
          <div className="space-y-5">
            {/* Health row */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
              <div>
                <div className="text-xs text-muted uppercase tracking-wide">版本</div>
                <div className="mt-0.5 text-sm text-ink font-mono">{telemetry.health.version || '—'}</div>
              </div>
              <div>
                <div className="text-xs text-muted uppercase tracking-wide">登录态</div>
                <div className={`mt-0.5 text-sm font-medium ${telemetry.health.loggedIn ? 'text-ok' : 'text-err'}`}>
                  {telemetry.health.loggedIn ? '✓' : '✗'}
                  {telemetry.health.email && (
                    <span className="ml-1 text-xs text-muted font-normal truncate">{telemetry.health.email}</span>
                  )}
                </div>
              </div>
              <div>
                <div className="text-xs text-muted uppercase tracking-wide">订阅</div>
                <div className="mt-0.5 text-sm text-ink">{telemetry.health.subscriptionType || '—'}</div>
              </div>
              <div>
                <div className="text-xs text-muted uppercase tracking-wide">模式</div>
                <div className="mt-0.5 text-sm text-ink">{telemetry.health.mode || '—'}</div>
              </div>
            </div>

            {/* Telemetry stats */}
            {telemetry.telemetry === null ? (
              <div className="bg-line/20 border border-line rounded-lg p-4 text-center text-muted text-sm">
                节点遥测不可达
              </div>
            ) : (
              <div className="space-y-4">
                {/* Main stats grid */}
                <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4">
                  <div>
                    <div className="text-xs text-muted uppercase tracking-wide">近1h请求</div>
                    <div className="mt-0.5 text-lg font-semibold text-ink">{telemetry.telemetry.totalRequests.toLocaleString()}</div>
                  </div>
                  <div>
                    <div className="text-xs text-muted uppercase tracking-wide">RPM</div>
                    <div className="mt-0.5 text-lg font-semibold text-ink">{telemetry.telemetry.requestsPerMinute.toFixed(1)}</div>
                  </div>
                  <div>
                    <div className="text-xs text-muted uppercase tracking-wide">错误</div>
                    <div className="mt-0.5 text-lg font-semibold text-err">
                      {telemetry.telemetry.errorCount}
                      {telemetry.telemetry.totalRequests > 0 && (
                        <span className="ml-1 text-xs font-normal text-muted">
                          ({((telemetry.telemetry.errorCount / telemetry.telemetry.totalRequests) * 100).toFixed(1)}%)
                        </span>
                      )}
                    </div>
                  </div>
                  <div>
                    <div className="text-xs text-muted uppercase tracking-wide">首字延迟 p50/p95</div>
                    <div className="mt-0.5 text-sm font-medium text-ink">
                      {telemetry.telemetry.ttfb.p50 >= 1000
                        ? `${(telemetry.telemetry.ttfb.p50 / 1000).toFixed(2)}s`
                        : `${telemetry.telemetry.ttfb.p50}ms`}
                      {' / '}
                      {telemetry.telemetry.ttfb.p95 >= 1000
                        ? `${(telemetry.telemetry.ttfb.p95 / 1000).toFixed(2)}s`
                        : `${telemetry.telemetry.ttfb.p95}ms`}
                    </div>
                  </div>
                  <div>
                    <div className="text-xs text-muted uppercase tracking-wide">总耗时 p50/p95</div>
                    <div className="mt-0.5 text-sm font-medium text-ink">
                      {telemetry.telemetry.totalDuration.p50 >= 1000
                        ? `${(telemetry.telemetry.totalDuration.p50 / 1000).toFixed(2)}s`
                        : `${telemetry.telemetry.totalDuration.p50}ms`}
                      {' / '}
                      {telemetry.telemetry.totalDuration.p95 >= 1000
                        ? `${(telemetry.telemetry.totalDuration.p95 / 1000).toFixed(2)}s`
                        : `${telemetry.telemetry.totalDuration.p95}ms`}
                    </div>
                  </div>
                </div>

                {/* Token usage */}
                <div>
                  <div className="text-xs text-muted uppercase tracking-wide mb-2">Token 用量</div>
                  <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
                    <div className="bg-bg rounded-lg p-2.5">
                      <div className="text-xs text-muted">入</div>
                      <div className="text-sm font-medium text-ink mt-0.5">{telemetry.telemetry.tokenUsage.totalInputTokens.toLocaleString()}</div>
                    </div>
                    <div className="bg-bg rounded-lg p-2.5">
                      <div className="text-xs text-muted">出</div>
                      <div className="text-sm font-medium text-ink mt-0.5">{telemetry.telemetry.tokenUsage.totalOutputTokens.toLocaleString()}</div>
                    </div>
                    <div className="bg-bg rounded-lg p-2.5">
                      <div className="text-xs text-muted">缓存读</div>
                      <div className="text-sm font-medium text-ink mt-0.5">{telemetry.telemetry.tokenUsage.totalCacheReadTokens.toLocaleString()}</div>
                    </div>
                    <div className="bg-bg rounded-lg p-2.5">
                      <div className="text-xs text-muted">缓存写</div>
                      <div className="text-sm font-medium text-ink mt-0.5">{telemetry.telemetry.tokenUsage.totalCacheCreationTokens.toLocaleString()}</div>
                    </div>
                    <div className="bg-bg rounded-lg p-2.5">
                      <div className="text-xs text-muted">缓存命中率</div>
                      <div className="text-sm font-medium text-ok mt-0.5">{(telemetry.telemetry.tokenUsage.avgCacheHitRate * 100).toFixed(1)}%</div>
                    </div>
                  </div>
                </div>

                {/* By model table */}
                {Object.keys(telemetry.telemetry.byModel).length > 0 && (
                  <div>
                    <div className="text-xs text-muted uppercase tracking-wide mb-2">按模型</div>
                    <div className="overflow-x-auto rounded-lg border border-line">
                      <table className="w-full text-sm">
                        <thead>
                          <tr className="bg-surface border-b border-line">
                            <th className="text-left px-3 py-2 text-xs text-muted font-medium">模型</th>
                            <th className="text-right px-3 py-2 text-xs text-muted font-medium">请求数</th>
                            <th className="text-right px-3 py-2 text-xs text-muted font-medium">平均耗时</th>
                          </tr>
                        </thead>
                        <tbody>
                          {Object.entries(telemetry.telemetry.byModel).map(([model, stat]) => (
                            <tr key={model} className="border-b border-line last:border-0 hover:bg-surface/50">
                              <td className="px-3 py-2 font-mono text-xs text-ink">{model}</td>
                              <td className="px-3 py-2 text-right text-ink">{stat.count.toLocaleString()}</td>
                              <td className="px-3 py-2 text-right text-muted">
                                {stat.avgTotalMs >= 1000
                                  ? `${(stat.avgTotalMs / 1000).toFixed(2)}s`
                                  : `${stat.avgTotalMs.toFixed(0)}ms`}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>

      {/* SDK Features */}
      <div className="space-y-3">
        <h2 className="text-sm font-semibold text-ink uppercase tracking-wide">SDK 设置</h2>

        {loadingFeatures && (
          <div className="flex items-center justify-center min-h-20">
            <span className="text-muted animate-pulse text-sm">加载 SDK 设置…</span>
          </div>
        )}

        {!loadingFeatures && featuresErr && (
          <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">
            {featuresErr}
          </div>
        )}

        {!loadingFeatures && !featuresErr && features && (
          Object.keys(features).length === 0 ? (
            <div className="bg-surface border border-line rounded-xl p-6 text-center text-muted text-sm">
              暂无 SDK adapter 设置
            </div>
          ) : (
            <div className="space-y-3">
              {Object.entries(features).map(([adapter, adapterFields]) => (
                <AdapterEditor
                  key={adapter}
                  nodeId={node.id}
                  adapter={adapter}
                  fields={adapterFields}
                  onSaved={() => { void fetchFeatures(); }}
                />
              ))}
            </div>
          )
        )}
      </div>
    </div>
  );
}
