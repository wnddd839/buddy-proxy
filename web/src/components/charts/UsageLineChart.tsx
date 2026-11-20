import { useId, useMemo, useState } from 'react';
import type { UsageChartPoint } from '@/types/api';
import { formatCompactNumber, formatHitRate } from '@/lib/format';

export type UsageChartRange = 'day' | 'week' | 'month';
type ChartSeriesMode = 'both' | 'tokens' | 'cache';

interface UsageLineChartProps {
  series: UsageChartPoint[];
  totalRequests: number;
  range?: UsageChartRange;
  height?: number;
  showModeToggle?: boolean;
  emptyAction?: React.ReactNode;
}

interface Point {
  x: number;
  y: number;
}

/**
 * 单调三次贝塞尔曲线（Fritsch-Carlson），平滑过渡且绝不下冲到基线以下。
 */
function buildMonotonePath(points: Point[]): string {
  if (!points.length) return '';
  if (points.length === 1) return `M${points[0].x.toFixed(1)} ${points[0].y.toFixed(1)}`;
  if (points.length === 2) {
    return `M${points[0].x.toFixed(1)} ${points[0].y.toFixed(1)} L${points[1].x.toFixed(1)} ${points[1].y.toFixed(1)}`;
  }

  const n = points.length;
  const dx: number[] = [];
  const dy: number[] = [];
  const slope: number[] = [];
  for (let i = 0; i < n - 1; i++) {
    const dxi = Math.max(1e-6, points[i + 1].x - points[i].x);
    const dyi = points[i + 1].y - points[i].y;
    dx.push(dxi);
    dy.push(dyi);
    slope.push(dyi / dxi);
  }

  const m: number[] = new Array(n).fill(0);
  m[0] = slope[0];
  m[n - 1] = slope[n - 2];
  for (let i = 1; i < n - 1; i++) {
    if (slope[i - 1] * slope[i] <= 0) {
      m[i] = 0;
    } else {
      const w1 = 2 * dx[i] + dx[i - 1];
      const w2 = dx[i] + 2 * dx[i - 1];
      m[i] = (w1 + w2) / (w1 / slope[i - 1] + w2 / slope[i]);
    }
  }

  let d = `M${points[0].x.toFixed(1)} ${points[0].y.toFixed(1)}`;
  for (let i = 0; i < n - 1; i++) {
    const h = dx[i] / 3;
    const cp1x = points[i].x + h;
    const cp1y = points[i].y + m[i] * h;
    const cp2x = points[i + 1].x - h;
    const cp2y = points[i + 1].y - m[i + 1] * h;
    d += ` C${cp1x.toFixed(1)} ${cp1y.toFixed(1)}, ${cp2x.toFixed(1)} ${cp2y.toFixed(1)}, ${points[i + 1].x.toFixed(1)} ${points[i + 1].y.toFixed(1)}`;
  }
  return d;
}

const EMPTY_AXIS_LABELS: Record<UsageChartRange, string[]> = {
  day: ['00:00', '06:00', '12:00', '18:00', '24:00'],
  week: ['6 天前', '4 天前', '2 天前', '昨日', '今日'],
  month: ['30 天前', '21 天前', '14 天前', '7 天前', '今日'],
};

export function UsageLineChart({
  series,
  totalRequests,
  range = 'day',
  height = 300,
  showModeToggle = true,
  emptyAction,
}: UsageLineChartProps) {
  const gradId = useId().replace(/:/g, '');
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const [mode, setMode] = useState<ChartSeriesMode>('both');

  const hasTokens = series.some((p) => (p.totalTokens || 0) > 0);
  const hasRate = series.some((p) => p.cacheHitRate != null && p.cacheHitRate >= 0);
  const isEmpty = !series.length || (!hasTokens && !hasRate && !totalRequests);

  const W = 960;
  const H = height;
  const pad = { l: 54, r: 48, t: 22, b: 32 };
  const iw = W - pad.l - pad.r;
  const ih = H - pad.t - pad.b;
  const baselineY = pad.t + ih;

  const {
    maxTok,
    yTicks,
    xCoords,
    tokenLinePath,
    tokenAreaPath,
    hitSegments,
    labelStep,
  } = useMemo(() => {
    const n = series.length;
    const peak = Math.max(
      1,
      series.reduce((m, p) => Math.max(m, p.totalTokens || 0), 0),
    );
    const niceMax = peak <= 10 ? 10 : Math.ceil(peak * 1.12);
    const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => Math.round(niceMax * f));
    const xs = series.map((_, i) =>
      n <= 1 ? pad.l + iw / 2 : pad.l + (i / (n - 1)) * iw,
    );

    const tokPts: Point[] = series.map((p, i) => ({
      x: xs[i],
      y: pad.t + ih - ((p.totalTokens || 0) / niceMax) * ih,
    }));
    const line = buildMonotonePath(tokPts);
    const area =
      tokPts.length >= 2
        ? `${line} L${tokPts[tokPts.length - 1].x.toFixed(1)} ${baselineY.toFixed(1)} L${tokPts[0].x.toFixed(1)} ${baselineY.toFixed(1)} Z`
        : '';

    const segments: Point[][] = [];
    let cur: Point[] = [];
    series.forEach((p, i) => {
      if (p.cacheHitRate != null && p.cacheHitRate >= 0) {
        const clamped = Math.max(0, Math.min(100, p.cacheHitRate));
        cur.push({ x: xs[i], y: pad.t + ih - (clamped / 100) * ih });
      } else if (cur.length) {
        segments.push(cur);
        cur = [];
      }
    });
    if (cur.length) segments.push(cur);

    return {
      maxTok: niceMax,
      yTicks: ticks,
      xCoords: xs,
      tokenLinePath: line,
      tokenAreaPath: area,
      hitSegments: segments,
      labelStep: n <= 9 ? 1 : Math.max(1, Math.ceil(n / 8)),
    };
  }, [series, iw, ih, pad.l, pad.t, baselineY]);

  const showTokens = mode === 'both' || mode === 'tokens';
  const showCache = mode === 'both' || mode === 'cache';
  const hovered = hoverIdx != null && !isEmpty ? series[hoverIdx] : null;
  const hoveredX = hoverIdx != null && !isEmpty ? xCoords[hoverIdx] : null;
  const slotW = series.length > 1 ? iw / (series.length - 1) : iw;

  return (
    <div className="space-y-3">
      {/* 图表顶栏：左实时读数 / 右图例与曲线切换 */}
      <div className="flex flex-wrap items-center justify-between gap-2.5 text-xs min-h-[24px]">
        <div className="text-[var(--fg-2)] min-w-0">
          {hovered ? (
            <span className="ds-mono inline-flex items-center flex-wrap gap-x-3 gap-y-1">
              <strong className="text-[var(--fg)]">{hovered.label}</strong>
              {showTokens && (
                <span>
                  Tokens{' '}
                  <strong className="text-[var(--fg)]">
                    {formatCompactNumber(hovered.totalTokens)}
                  </strong>
                </span>
              )}
              {showCache && (
                <span>
                  缓存命中{' '}
                  <strong className="text-[var(--ok)]">
                    {formatHitRate(hovered.cacheHitRate)}
                  </strong>
                </span>
              )}
            </span>
          ) : (
            <span className="text-[var(--fg-3)]">
              {isEmpty
                ? '等待首个代理请求进入，实时绘制折线走势'
                : '悬停折线查看任意时段 Token 与缓存命中率读数'}
            </span>
          )}
        </div>

        <div className="flex items-center gap-4 flex-wrap">
          <div className="flex items-center gap-3.5 text-[11px] text-[var(--fg-2)]">
            {showTokens && (
              <span className="inline-flex items-center gap-1.5">
                <i className="w-3.5 h-[2px] inline-block bg-[var(--accent)] rounded-full" />
                Token 消耗
              </span>
            )}
            {showCache && (
              <span className="inline-flex items-center gap-1.5">
                <i className="w-3.5 h-0 inline-block border-t-2 border-dashed border-[var(--ok)]" />
                缓存命中率
              </span>
            )}
          </div>

          {showModeToggle && (
            <div className="inline-flex items-center gap-1 text-[11px] text-[var(--fg-3)] border-l border-[var(--border)] pl-3">
              {(
                [
                  { id: 'both', label: '双轴' },
                  { id: 'tokens', label: 'Token' },
                  { id: 'cache', label: '命中率' },
                ] as const
              ).map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setMode(item.id)}
                  className={[
                    'px-1.5 py-0.5 rounded-[var(--radius-sm)] transition-colors cursor-pointer',
                    mode === item.id
                      ? 'bg-[var(--surface-2)] text-[var(--fg)] font-medium'
                      : 'hover:text-[var(--fg)]',
                  ].join(' ')}
                >
                  {item.label}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* 主画布 */}
      <div className={`relative ${isEmpty ? 'min-h-[180px] flex items-center' : ''}`}>
        <svg
          viewBox={`0 0 ${W} ${H}`}
          role="img"
          aria-label="Token 与缓存命中率折线趋势图"
          className="w-full h-auto block select-none"
          onMouseLeave={() => setHoverIdx(null)}
        >
          <defs>
            <linearGradient id={`area-${gradId}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.16" />
              <stop offset="100%" stopColor="var(--accent)" stopOpacity="0.0" />
            </linearGradient>
          </defs>

          {/* 水平网格线 + 左轴刻度 */}
          {(isEmpty ? [0, 0.33, 0.66, 1] : yTicks).map((val, idx, arr) => {
            const frac = isEmpty ? val : idx / (arr.length - 1);
            const y = baselineY - frac * ih;
            return (
              <g key={idx}>
                <line
                  x1={pad.l}
                  y1={y}
                  x2={W - pad.r}
                  y2={y}
                  stroke="var(--border)"
                  strokeDasharray={idx === 0 ? undefined : '3 4'}
                />
                {!isEmpty && showTokens && (
                  <text
                    x={pad.l - 10}
                    y={y + 3.5}
                    textAnchor="end"
                    fill="var(--fg-3)"
                    fontSize="10.5"
                    fontFamily="var(--font-mono)"
                  >
                    {val === 0 ? '0' : formatCompactNumber(val)}
                  </text>
                )}
              </g>
            );
          })}

          {/* 右轴缓存命中率刻度 */}
          {showCache && (
            <>
              <text
                x={W - pad.r + 10}
                y={pad.t + 4}
                fill={isEmpty ? 'var(--fg-3)' : 'var(--ok)'}
                opacity={isEmpty ? 0.55 : 0.9}
                fontSize="10.5"
                fontFamily="var(--font-mono)"
              >
                100%
              </text>
              <text
                x={W - pad.r + 10}
                y={pad.t + ih / 2 + 3.5}
                fill="var(--fg-3)"
                opacity={isEmpty ? 0.55 : 0.85}
                fontSize="10.5"
                fontFamily="var(--font-mono)"
              >
                50%
              </text>
              <text
                x={W - pad.r + 10}
                y={baselineY + 3.5}
                fill="var(--fg-3)"
                opacity={isEmpty ? 0.55 : 0.85}
                fontSize="10.5"
                fontFamily="var(--font-mono)"
              >
                0%
              </text>
            </>
          )}

          {isEmpty ? (
            <>
              {/* 空态优雅基线 + 时间轴坐标 */}
              <line
                x1={pad.l}
                y1={baselineY - 1}
                x2={W - pad.r}
                y2={baselineY - 1}
                stroke="var(--accent)"
                strokeWidth="1.5"
                opacity="0.22"
              />
              {EMPTY_AXIS_LABELS[range].map((lbl, idx, arr) => {
                const x = pad.l + (idx / (arr.length - 1)) * iw;
                return (
                  <text
                    key={lbl}
                    x={x}
                    y={H - 8}
                    textAnchor={idx === 0 ? 'start' : idx === arr.length - 1 ? 'end' : 'middle'}
                    fill="var(--fg-3)"
                    fontSize="10.5"
                    fontFamily="var(--font-mono)"
                  >
                    {lbl}
                  </text>
                );
              })}
            </>
          ) : (
            <>
              {/* 悬停垂直参考线 */}
              {hoveredX != null && (
                <line
                  x1={hoveredX}
                  y1={pad.t}
                  x2={hoveredX}
                  y2={baselineY}
                  stroke="var(--border-strong)"
                  strokeDasharray="3 3"
                />
              )}

              {/* Token 面积填充 + 折线 */}
              {showTokens && tokenAreaPath && (
                <path d={tokenAreaPath} fill={`url(#area-${gradId})`} />
              )}
              {showTokens && tokenLinePath && (
                <path
                  d={tokenLinePath}
                  fill="none"
                  stroke="var(--accent)"
                  strokeWidth="2.2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              )}

              {/* 缓存命中率折线 */}
              {showCache &&
                hitSegments.map((seg, idx) => (
                  <path
                    key={idx}
                    d={buildMonotonePath(seg)}
                    fill="none"
                    stroke="var(--ok)"
                    strokeWidth="1.8"
                    strokeDasharray="5 3"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                ))}

              {/* 数据节点：仅在悬停或点数较少时突出圆点，保持画面极度克制 */}
              {series.map((p, i) => {
                const isHov = hoverIdx === i;
                const cx = xCoords[i];
                const tokY = pad.t + ih - ((p.totalTokens || 0) / maxTok) * ih;
                const hasHit = p.cacheHitRate != null && p.cacheHitRate >= 0;
                const hitY = hasHit
                  ? pad.t + ih - (Math.max(0, Math.min(100, p.cacheHitRate)) / 100) * ih
                  : 0;
                const showBaseDot = series.length <= 12;
                return (
                  <g key={i}>
                    {showTokens && (isHov || showBaseDot) && (
                      <circle
                        cx={cx}
                        cy={tokY}
                        r={isHov ? 4.5 : 2.5}
                        fill="var(--surface)"
                        stroke="var(--accent)"
                        strokeWidth={isHov ? 2.2 : 1.75}
                      />
                    )}
                    {showCache && hasHit && (isHov || showBaseDot) && (
                      <circle
                        cx={cx}
                        cy={hitY}
                        r={isHov ? 4 : 2.2}
                        fill="var(--surface)"
                        stroke="var(--ok)"
                        strokeWidth={isHov ? 2 : 1.5}
                      />
                    )}
                  </g>
                );
              })}

              {/* X 轴时间刻度 */}
              {series.map((p, i) => {
                if (i % labelStep !== 0 && i !== series.length - 1) return null;
                return (
                  <text
                    key={i}
                    x={xCoords[i]}
                    y={H - 8}
                    textAnchor={
                      i === 0 ? 'start' : i === series.length - 1 ? 'end' : 'middle'
                    }
                    fill={hoverIdx === i ? 'var(--fg)' : 'var(--fg-3)'}
                    fontSize="10.5"
                    fontFamily="var(--font-mono)"
                  >
                    {p.label}
                  </text>
                );
              })}

              {/* 悬停感应热区 */}
              {series.map((_, i) => {
                const cx = xCoords[i];
                const x0 = Math.max(pad.l, cx - slotW / 2);
                const x1 = Math.min(W - pad.r, cx + slotW / 2);
                return (
                  <rect
                    key={i}
                    x={x0}
                    y={pad.t}
                    width={Math.max(8, x1 - x0)}
                    height={ih}
                    fill="transparent"
                    className="cursor-crosshair"
                    onMouseEnter={() => setHoverIdx(i)}
                  />
                );
              })}
            </>
          )}
        </svg>

        {isEmpty && (
          <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center pb-5 text-center px-4">
            <div className="text-xs font-medium text-[var(--fg-2)]">
              当前周期暂无请求统计
            </div>
            <p className="text-[11px] text-[var(--fg-3)] mt-1 max-w-sm m-0">
              通过本地 OpenAI 兼容入口发起调用后，此处将自动绘制 Token 消耗与缓存命中率折线走势。
            </p>
            {emptyAction && <div className="pointer-events-auto mt-3">{emptyAction}</div>}
          </div>
        )}
      </div>
    </div>
  );
}
