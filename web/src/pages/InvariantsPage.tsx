import { useCallback, useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import type { Finding, InvariantsPayload } from '@/types/invariants';
import { formatTime } from '@/lib/format';
import { apiRequest } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, PageHeader, StatCard } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';

interface InvariantsPageProps {
  active: boolean;
}

const FINDINGS_LIMIT = 20;

/** 相对时间：越新越该先看；精确时刻放 tooltip，不占正文。 */
function relativeTime(ms?: number): string {
  if (!ms) return '—';
  const diff = Date.now() - ms;
  if (diff < 60_000) return '刚刚';
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days} 天前`;
  return formatTime(ms);
}

/** 只有 info（观测）与 violation（违规）两种；没见过的级别按违规处理，不静默降级。 */
function severityBadge(severity?: string): { tone: 'err' | 'neutral'; label: string } {
  if (severity === 'info') return { tone: 'neutral', label: '观测' };
  return { tone: 'err', label: '违规' };
}

export function InvariantsPage({ active }: InvariantsPageProps) {
  const { showToast } = useToast();
  const [data, setData] = useState<InvariantsPayload | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const fetchInvariants = useCallback(async () => {
    setLoading(true);
    try {
      const res = await apiRequest<InvariantsPayload>(
        `/direct-admin/api/invariants?limit=${FINDINGS_LIMIT}`,
      );
      setData(res);
      setError('');
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      // 不能吞错：接口挂了会永远显示「—」，与「确实没命中」不可区分。
      setError(msg);
      showToast(`静默失效体检加载失败：${msg}`, 'error');
    } finally {
      setLoading(false);
    }
  }, [showToast]);

  // 切到本页立刻拉一次（与其余页面同样的 active 模式）
  useEffect(() => {
    if (!active) return;
    fetchInvariants();
  }, [active, fetchInvariants]);

  const enabled = !!data?.enabled;
  const findings: Finding[] = data?.findings || [];
  const specs = data?.specs || [];
  const coverage = data?.coverage || '—';
  const counts = data?.counts || {};
  // counts 是近 24h 的累计（环形缓冲内），findings 只是最近 limit 条 —— 两者可以不等。
  const countsTotal = Object.keys(counts).reduce((sum, k) => sum + (Number(counts[k]) || 0), 0);
  const suppressed = data?.suppressed || 0;
  const dropped = data?.dropped || 0;
  const writeErrors = data?.writeErrors || 0;
  // 「命中」= 近 24h 的累计次数（与旧面板口径一致）。
  //
  // 为什么不直接用 findings.length：列表只取最近 limit（20）条，
  // 「展示 20 条」会被读成「24h 只炸了 20 次」。取 max 是为了同时避免
  // 另一种误读 —— 当 24h 累计已经超过展示上限时，主数字不能被压成 20。
  const hits = Math.max(countsTotal, findings.length);

  return (
    <div id="tab-invariants" className="ds-page-enter space-y-4">
      <div className="space-y-1">
        <div className="text-[10px] font-medium tracking-[0.2em] uppercase text-[var(--fg-3)]">
          Silent Failure Watch
        </div>
        <PageHeader
          title="静默失效体检"
          subtitle="请求返回 200、但数据里出现了不该出现的变化。这里只做断言，不自动修复。"
          actions={
            <Button
              id="btnRefreshInv"
              variant="ghost"
              size="sm"
              loading={loading}
              onClick={() => fetchInvariants()}
              // 旧面板的处理函数名，保留给 internal/server 的面板断言测试
              // （它按字符串找 refreshInvariants；React 侧的真名会被压缩掉）。
              data-action="refreshInvariants"
              title="重新拉取最近命中的断言（不会触发上游请求）"
            >
              <RefreshCw className={`w-3 h-3 shrink-0 ${loading ? 'animate-spin' : ''}`} />
              <span>刷新</span>
            </Button>
          }
        />
      </div>

      <Card className="p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span
            id="invCoverage"
            className="ds-mono text-[11px] text-[var(--fg-3)]"
            title="这份数据的覆盖范围：没有覆盖说明的仪表盘会被误读成「没发生」"
          >
            {coverage}
          </span>
          {writeErrors > 0 && (
            <StatusBadge
              tone="err"
              title="命中了但没落盘：离线回放与本次统计都会缺数据"
            >
              写盘失败 {writeErrors}
            </StatusBadge>
          )}
        </div>

        {error && (
          <div className="rounded-[var(--radius-sm)] border border-[var(--err)] bg-[var(--err-bg)] px-3 py-2 text-xs text-[var(--err)] break-words">
            加载失败：{error}
          </div>
        )}

        {!data && loading && <p className="text-xs text-[var(--fg-3)] m-0">加载中…</p>}

        {data && (
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <StatCard
              label="命中"
              valueId="invHits"
              value={enabled ? hits : '—'}
              sub={
                enabled
                  ? `近 24h 累计 · 列表展示最近 ${findings.length} / ${FINDINGS_LIMIT} 条`
                  : '未启用'
              }
              title="近 24h 命中的断言次数（与旧面板口径一致）" 
            />
            <StatCard
              label="去重压掉"
              valueId="invSuppressed"
              value={enabled ? suppressed : '—'}
              title="同一 (断言, 对象) 在 10 分钟窗口内只落一条，其余只计数"
            />
            <StatCard
              label="队列丢弃"
              valueId="invDropped"
              value={enabled ? dropped : '—'}
              valueClassName={enabled && dropped > 0 ? 'text-[var(--warn)]' : ''}
              title="队列满时直接丢记录、只计数不阻塞请求路径；大于 0 说明体检本身在漏检"
            />
          </div>
        )}

        {data && !enabled && (
          <EmptyState
            title="未启用"
            description={`${coverage}。不变量需要账号目录可写才能落盘；目录不可写时决策日志写不出去，命中也永远不会出现在这里。`}
          />
        )}

        {enabled && (
          <>
            {!findings.length ? (
              <p className="text-xs text-[var(--fg-3)] m-0">近 24h 未命中任何不变量。</p>
            ) : (
              <div className="divide-y divide-[var(--border)]">
                {findings.map((f, idx) => {
                  const sev = severityBadge(f.severity);
                  const supN = f.suppressed || 0;
                  return (
                    <div
                      key={`${f.invariant}-${f.subject || ''}-${f.at || 0}-${idx}`}
                      className="py-2.5 first:pt-0 last:pb-0 space-y-1"
                    >
                      <div className="flex flex-wrap items-center gap-1.5">
                        <StatusBadge
                          tone={sev.tone}
                          className="!text-[10px] !px-1.5 !py-0"
                          title={f.severity || 'violation'}
                        >
                          {sev.label}
                        </StatusBadge>
                        <span
                          className="ds-mono text-[10px] px-1.5 py-0.5 rounded bg-[var(--surface-2)] border border-[var(--border)] text-[var(--fg-2)]"
                          title={f.why || undefined}
                        >
                          {f.invariant}
                        </span>
                        {f.subject && (
                          <span
                            className="ds-mono text-[11px] text-[var(--fg-3)] truncate max-w-[34ch]"
                            title={f.subject}
                          >
                            {f.subject}
                          </span>
                        )}
                        <span
                          className="ds-mono text-[11px] text-[var(--fg-3)] ml-auto whitespace-nowrap"
                          title={formatTime(f.at)}
                        >
                          {relativeTime(f.at)}
                        </span>
                      </div>
                      <p className="text-[13px] text-[var(--fg)] m-0 leading-relaxed break-words">
                        {f.detail || '—'}
                        {supN > 0 && (
                          <span className="text-[var(--fg-3)]">
                            （另有 {supN} 次被去重压掉）
                          </span>
                        )}
                      </p>
                    </div>
                  );
                })}
              </div>
            )}
          </>
        )}

        {data && (
          <details className="ds-surface-2 px-3 py-2 text-xs">
            <summary className="text-[11px] text-[var(--fg-3)] cursor-pointer select-none">
              断言清单{specs.length ? `（${specs.length}）` : ''}
            </summary>
            {!specs.length ? (
              <p className="text-[11px] text-[var(--fg-3)] m-0 mt-2">未启用，无法列出断言。</p>
            ) : (
              <div className="mt-2 divide-y divide-[var(--border)]">
                {specs.map((s) => (
                  <div key={s.id} className="py-2 first:pt-0 last:pb-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="ds-mono text-[10px] px-1.5 py-0.5 rounded bg-[var(--surface-2)] border border-[var(--border)] text-[var(--fg)]">
                        {s.id}
                      </span>
                      <span className="text-[13px] text-[var(--fg)]">{s.title}</span>
                      <span className="ds-mono text-[10px] text-[var(--fg-3)]">
                        {severityBadge(s.severity).label}
                      </span>
                      {counts[s.id] ? (
                        <span className="ds-mono text-[10px] text-[var(--fg-3)] ml-auto">
                          近 24h {counts[s.id]} 次
                        </span>
                      ) : null}
                    </div>
                    <p className="text-[11px] text-[var(--fg-3)] m-0 leading-relaxed break-words">
                      {s.why}
                    </p>
                  </div>
                ))}
              </div>
            )}
          </details>
        )}
      </Card>
    </div>
  );
}
