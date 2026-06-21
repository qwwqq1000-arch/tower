// ============================================================
// Tower SPA — 封号分析页 (Ban Analysis)
// GET /api/admin/ban-analysis → total + weekday/hour bar charts
// Pure CSS/div bars, no chart library. Responsive.
// ============================================================
import { useEffect, useState, useCallback } from 'react';
import { getBanAnalysis } from '../api';
import type { BanAnalysis, BanBucket } from '../api';

// ---- Weekday labels (0=周日 … 6=周六) ----
const WEEKDAY_LABELS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];

// ---- Bar chart component ----
interface BarChartProps {
  data: BanBucket[];
  labelFn: (bucket: number) => string;
  color?: string;
}

function BarChart({ data, labelFn, color = 'bg-accent' }: BarChartProps) {
  const maxCount = data.reduce((m, d) => Math.max(m, d.count), 0);

  if (maxCount === 0) {
    return (
      <p className="text-xs text-muted py-4 text-center">暂无数据</p>
    );
  }

  return (
    <div className="w-full overflow-x-auto">
      <div className="flex items-end gap-1 min-w-0" style={{ minHeight: '120px' }}>
        {data.map((d) => {
          const pct = maxCount > 0 ? (d.count / maxCount) * 100 : 0;
          return (
            <div
              key={d.bucket}
              className="flex flex-col items-center flex-1 min-w-0 gap-1 group"
              title={`${labelFn(d.bucket)}: ${d.count}`}
            >
              <span className="text-[10px] text-muted opacity-0 group-hover:opacity-100 transition whitespace-nowrap">
                {d.count}
              </span>
              <div className="w-full flex flex-col justify-end" style={{ height: '96px' }}>
                <div
                  className={`w-full rounded-t ${color} transition-all duration-300`}
                  style={{ height: `${pct}%`, minHeight: d.count > 0 ? '4px' : '0' }}
                />
              </div>
              <span className="text-[10px] text-muted truncate w-full text-center leading-none">
                {labelFn(d.bucket)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ---- Fill sparse data (ensure all slots present) ----
function fillWeekday(data: BanBucket[]): BanBucket[] {
  const map = new Map(data.map((d) => [d.bucket, d.count]));
  return Array.from({ length: 7 }, (_, i) => ({ bucket: i, count: map.get(i) ?? 0 }));
}

function fillHour(data: BanBucket[]): BanBucket[] {
  const map = new Map(data.map((d) => [d.bucket, d.count]));
  return Array.from({ length: 24 }, (_, i) => ({ bucket: i, count: map.get(i) ?? 0 }));
}

// ---- Page ----
export default function BanAnalysis() {
  const [data, setData] = useState<BanAnalysis | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const result = await getBanAnalysis();
      setData(result);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void fetchData(); }, [fetchData]);

  const weekdayData = data ? fillWeekday(data.byWeekday) : [];
  const hourData = data ? fillHour(data.byHour) : [];

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-ink">封号分析</h1>
          <p className="text-xs text-muted mt-1">封号时间分布（按星期 / 按小时）</p>
        </div>
        <button
          onClick={() => { void fetchData(); }}
          disabled={loading}
          className="shrink-0 px-3 py-1.5 text-xs font-medium border border-line rounded-lg
                     text-muted hover:text-ink hover:border-accent transition disabled:opacity-50"
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center min-h-40">
          <span className="text-muted animate-pulse">加载中…</span>
        </div>
      )}

      {/* Error */}
      {!loading && error && (
        <div className="bg-err/10 border border-err/30 rounded-xl p-4 text-err text-sm">{error}</div>
      )}

      {/* Empty state */}
      {!loading && !error && data && data.total === 0 && (
        <div className="bg-surface border border-line rounded-xl p-12 text-center space-y-2">
          <p className="text-4xl">🎉</p>
          <p className="text-ink font-medium">暂无封号记录</p>
          <p className="text-xs text-muted">所有账号运行正常，继续保持！</p>
        </div>
      )}

      {/* Content */}
      {!loading && !error && data && data.total > 0 && (
        <>
          {/* Total stat */}
          <div className="bg-surface border border-line rounded-xl p-5 flex items-center gap-4">
            <div className="w-12 h-12 rounded-full bg-err/10 flex items-center justify-center text-2xl shrink-0">
              ⚠
            </div>
            <div>
              <p className="text-xs text-muted uppercase tracking-wide">累计封号次数</p>
              <p className="text-3xl font-bold text-ink tabular-nums">{data.total.toLocaleString()}</p>
            </div>
          </div>

          {/* Weekday chart */}
          <div className="bg-surface border border-line rounded-xl p-5 space-y-3">
            <div>
              <h2 className="text-sm font-semibold text-ink">按星期分布</h2>
              <p className="text-xs text-muted">封号集中在哪些星期</p>
            </div>
            <BarChart
              data={weekdayData}
              labelFn={(b) => WEEKDAY_LABELS[b] ?? String(b)}
              color="bg-err"
            />
          </div>

          {/* Hour chart */}
          <div className="bg-surface border border-line rounded-xl p-5 space-y-3">
            <div>
              <h2 className="text-sm font-semibold text-ink">按小时分布</h2>
              <p className="text-xs text-muted">封号集中在哪些时段（0–23 时）</p>
            </div>
            <BarChart
              data={hourData}
              labelFn={(b) => String(b).padStart(2, '0')}
              color="bg-warn"
            />
          </div>
        </>
      )}
    </div>
  );
}
