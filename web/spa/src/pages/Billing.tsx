// ============================================================
// Tower SPA — Billing page (计费)
// Input tenantId → "结算" POST /api/admin/settle → show invoice
// GET /api/admin/ledger?tenantId= → ledger table
// ============================================================
import { useState, useCallback } from 'react';
import { settle, getLedger } from '../api';
import type { SettleResult, LedgerEntry } from '../types';

// ---- helpers ----
function fmtTime(ms: number): string {
  if (!ms) return '—';
  return new Date(ms).toLocaleString('zh-CN', {
    month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

function fmtUsd(amount: number): string {
  return `$${amount.toFixed(6)}`;
}

// ---- Invoice card ----
function InvoiceCard({ result }: { result: SettleResult }) {
  const ok = result.status === 'ok' || result.status === 'settled';
  return (
    <div className={`border rounded-xl p-5 space-y-3 ${ok ? 'bg-ok/5 border-ok/30' : 'bg-surface border-line'}`}>
      <div className="flex items-center gap-2">
        <span className={`w-2 h-2 rounded-full ${ok ? 'bg-ok' : 'bg-warn'}`} />
        <h3 className="text-sm font-semibold text-ink">结算单</h3>
        <span className={`ml-auto text-xs font-medium px-2 py-0.5 rounded-full ${ok ? 'bg-ok/10 text-ok' : 'bg-warn/10 text-warn'}`}>
          {result.status}
        </span>
      </div>
      <div className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <p className="text-xs text-muted">账单 ID</p>
          <p className="font-mono text-ink mt-0.5 truncate text-xs">{result.id || '—'}</p>
        </div>
        <div>
          <p className="text-xs text-muted">租户</p>
          <p className="font-mono text-ink mt-0.5 truncate text-xs">{result.tenantId || '—'}</p>
        </div>
        <div className="col-span-2">
          <p className="text-xs text-muted">结算金额</p>
          <p className="text-2xl font-bold text-ink mt-0.5">{fmtUsd(result.gross ?? 0)}</p>
        </div>
      </div>
    </div>
  );
}

// ---- Ledger row ----
function LedgerRow({ row }: { row: LedgerEntry }) {
  const isDebit = row.amount < 0;
  return (
    <tr className="border-t border-line hover:bg-line/20 transition text-sm">
      <td className="px-3 py-2 text-xs text-muted whitespace-nowrap font-mono">{fmtTime(row.ts)}</td>
      <td className="px-3 py-2 text-ink">{row.type || '—'}</td>
      <td className={`px-3 py-2 font-mono text-xs font-medium ${isDebit ? 'text-err' : 'text-ok'}`}>
        {isDebit ? '' : '+'}{fmtUsd(row.amount)}
      </td>
      <td className="px-3 py-2 text-xs text-muted font-mono truncate max-w-[100px]" title={row.ref}>{row.ref || '—'}</td>
      <td className="px-3 py-2 text-xs text-muted truncate max-w-[160px]" title={row.note}>{row.note || '—'}</td>
    </tr>
  );
}

// ---- Ledger card (mobile) ----
function LedgerCard({ row }: { row: LedgerEntry }) {
  const isDebit = row.amount < 0;
  return (
    <div className="bg-surface border border-line rounded-xl p-4 space-y-1.5 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="text-ink font-medium">{row.type || '—'}</span>
        <span className={`font-mono text-sm font-semibold ${isDebit ? 'text-err' : 'text-ok'}`}>
          {isDebit ? '' : '+'}{fmtUsd(row.amount)}
        </span>
      </div>
      <p className="text-xs text-muted">{fmtTime(row.ts)}</p>
      {row.note && <p className="text-xs text-muted truncate">{row.note}</p>}
    </div>
  );
}

// ---- Page ----
export default function Billing() {
  const [tenantId, setTenantId] = useState('');
  const [settling, setSettling] = useState(false);
  const [invoice, setInvoice] = useState<SettleResult | null>(null);
  const [settleErr, setSettleErr] = useState<string | null>(null);

  const [ledger, setLedger] = useState<LedgerEntry[]>([]);
  const [loadingLedger, setLoadingLedger] = useState(false);
  const [ledgerErr, setLedgerErr] = useState<string | null>(null);
  const [ledgerTenant, setLedgerTenant] = useState('');

  const handleSettle = useCallback(async () => {
    if (!tenantId.trim()) return;
    setSettling(true);
    setSettleErr(null);
    setInvoice(null);
    try {
      const result = await settle({ tenantId: tenantId.trim() });
      setInvoice(result);
    } catch (e) {
      setSettleErr(e instanceof Error ? e.message : '结算失败');
    } finally {
      setSettling(false);
    }
  }, [tenantId]);

  const handleLoadLedger = useCallback(async () => {
    if (!tenantId.trim()) return;
    setLoadingLedger(true);
    setLedgerErr(null);
    setLedger([]);
    const tid = tenantId.trim();
    setLedgerTenant(tid);
    try {
      const data = await getLedger(tid);
      setLedger(Array.isArray(data) ? data : []);
    } catch (e) {
      setLedgerErr(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoadingLedger(false);
    }
  }, [tenantId]);

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-semibold text-ink">计费</h1>
        <p className="text-xs text-muted mt-1">输入租户 ID，结算或查询账本</p>
      </div>

      {/* Tenant input + actions */}
      <div className="bg-surface border border-line rounded-xl p-5 space-y-4">
        <div>
          <label className="block text-xs font-medium text-muted mb-1.5">租户 ID</label>
          <input
            type="text"
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
            placeholder="tenant_xxx"
            className="w-full max-w-sm bg-bg border border-line rounded-lg px-3 py-2 text-sm text-ink
                       placeholder:text-muted focus:outline-none focus:border-accent transition font-mono"
          />
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            onClick={() => { void handleSettle(); }}
            disabled={settling || !tenantId.trim()}
            className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg
                       hover:bg-accent/80 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {settling ? '结算中…' : '结算'}
          </button>
          <button
            onClick={() => { void handleLoadLedger(); }}
            disabled={loadingLedger || !tenantId.trim()}
            className="px-4 py-2 text-sm font-medium border border-accent text-accent rounded-lg
                       hover:bg-accent/10 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {loadingLedger ? '加载中…' : '查询账本'}
          </button>
        </div>
        {settleErr && (
          <div className="bg-err/10 border border-err/30 rounded-lg p-3 text-err text-sm">{settleErr}</div>
        )}
      </div>

      {/* Invoice */}
      {invoice && <InvoiceCard result={invoice} />}

      {/* Ledger */}
      {(ledger.length > 0 || ledgerErr || loadingLedger) && (
        <div className="space-y-3">
          <h2 className="text-sm font-semibold text-ink">
            账本 — <span className="font-mono text-muted">{ledgerTenant}</span>
          </h2>

          {loadingLedger && (
            <div className="flex items-center justify-center min-h-20">
              <span className="text-muted animate-pulse">加载中…</span>
            </div>
          )}

          {!loadingLedger && ledgerErr && (
            <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{ledgerErr}</div>
          )}

          {!loadingLedger && !ledgerErr && ledger.length === 0 && (
            <div className="bg-surface border border-line rounded-xl p-8 text-center text-muted text-sm">
              暂无账本记录
            </div>
          )}

          {!loadingLedger && !ledgerErr && ledger.length > 0 && (
            <>
              {/* Desktop table */}
              <div className="hidden md:block bg-surface border border-line rounded-xl overflow-x-auto">
                <table className="w-full text-left min-w-[520px]">
                  <thead>
                    <tr className="text-xs text-muted uppercase tracking-wide">
                      <th className="px-3 py-3 font-medium">时间</th>
                      <th className="px-3 py-3 font-medium">类型</th>
                      <th className="px-3 py-3 font-medium">金额 (USD)</th>
                      <th className="px-3 py-3 font-medium">引用</th>
                      <th className="px-3 py-3 font-medium">备注</th>
                    </tr>
                  </thead>
                  <tbody>
                    {ledger.map((row, i) => <LedgerRow key={i} row={row} />)}
                  </tbody>
                </table>
              </div>

              {/* Mobile cards */}
              <div className="md:hidden space-y-3">
                {ledger.map((row, i) => <LedgerCard key={i} row={row} />)}
              </div>

              <p className="text-xs text-muted text-right">{ledger.length} 条记录</p>
            </>
          )}
        </div>
      )}
    </div>
  );
}
