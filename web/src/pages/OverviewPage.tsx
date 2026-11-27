import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowRight,
  BookOpen,
  Code2,
  Copy,
  RefreshCw,
  ShieldAlert,
} from 'lucide-react';
import type {
  AccountUsageItem,
  StatusPayload,
  UsageViewPayload,
} from '@/types/api';
import {
  computeAccountStates,
  computeCreditLevel,
  type ConcurrencySummary,
} from '@/lib/domain';
import {
  formatCompactNumber,
  formatHitRate,
  formatPercent,
  normalizeSite,
  siteLabel,
} from '@/lib/format';
import { apiRequest, copyToClipboard } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, PageHeader } from '@/components/ui/Card';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { SlideDrawer } from '@/components/ui/SlideDrawer';
import { StatusBadge } from '@/components/ui/StatusBadge';
import {
  UsageLineChart,
  type UsageChartRange,
} from '@/components/charts/UsageLineChart';
import type { TabId } from '@/components/layout/AppShell';

interface OverviewPageProps {
  active: boolean;
  refreshTick: number;
  status: StatusPayload | null;
  activeSite: string;
  usageByAccount: Record<string, AccountUsageItem>;
  concurrency: ConcurrencySummary;
  onNavigate: (tab: TabId) => void;
  onNavigatePoolIssues: () => void;
}

export function OverviewPage({
  active,
  refreshTick,
  status,
  activeSite,
  usageByAccount,
  concurrency,
  onNavigate,
  onNavigatePoolIssues,
}: OverviewPageProps) {
  const { showToast } = useToast();
  const [range, setRange] = useState<UsageChartRange>('day');
  const [usageData, setUsageData] = useState<UsageViewPayload | null>(null);
  const [helpOpen, setHelpOpen] = useState(false);
  const [diagOpen, setDiagOpen] = useState(false);

  const fetchUsageOverview = useCallback(async (r: UsageChartRange) => {
    try {
      const data = await apiRequest<UsageViewPayload>(
        `/direct-admin/api/usage?range=${encodeURIComponent(r)}&limit=5&offset=0`,
      );
      setUsageData(data);
    } catch {
      // 首页统计图表加载失败时回退使用 status 汇总，不阻塞页面
    }
  }, []);

  useEffect(() => {
    if (!active) return;
    fetchUsageOverview(range);
  }, [active, range, refreshTick, fetchUsageOverview]);

  const allAccounts = status?.accounts?.accounts || [];
  const site = normalizeSite(activeSite);
  const siteAccounts = useMemo(
    () => allAccounts.filter((a) => normalizeSite(a.site) === site),
    [allAccounts, site],
  );

  const poolSummary = useMemo(() => {
    let healthy = 0;
    let attention = 0;
    let enabled = 0;
    let remaining = 0;
    let unlimited = false;
    let withSnapshot = 0;

    for (const a of siteAccounts) {
      if (!a.enabled) continue;
      enabled++;
      const states = computeAccountStates(a);
      const usage = usageByAccount[a.id];
      const creditLvl = usage?.credits ? computeCreditLevel(usage) : 'good';
      const hasIssue =
        !(a.loggedIn && a.hasCredentials) ||
        a.tokenExpired ||
        !!a.lastError ||
        states.length > 0 ||
        creditLvl !== 'good';
      if (hasIssue) attention++;
      else healthy++;

      if (usage?.credits) {
        withSnapshot++;
        if (usage.credits.unlimited) unlimited = true;
        else if (usage.credits.remaining != null) remaining += Number(usage.credits.remaining);
      } else if (a.quotaRemaining != null) {
        withSnapshot++;
        remaining += Number(a.quotaRemaining);
      }
    }
    return { healthy, attention, enabled, remaining, unlimited, withSnapshot };
  }, [siteAccounts, usageByAccount]);

  const stats = status?.stats || {};
  const watch = status?.watch;
  const uSum = usageData?.summary;

  const displayTokens = uSum?.totalTokens ?? stats.totalTokens ?? 0;
  const displayRequests =
    uSum?.requests ?? (stats.successRequests || 0) + (stats.failedRequests || 0);
  const displayFailed = uSum?.failed ?? stats.failedRequests ?? 0;
  const successRate =
    displayRequests > 0 ? ((displayRequests - displayFailed) / displayRequests) * 100 : null;
  const topModels = (usageData?.byModel || []).slice(0, 5);
  const maxModelTokens = Math.max(
    1,
    topModels.reduce((m, row) => Math.max(m, row.totalTokens || 0), 0),
  );

  const domesticCount =
    status?.accounts?.domesticCount ??
    allAccounts.filter((a) => normalizeSite(a.site) === 'domestic').length;
  const globalCount =
    status?.accounts?.globalCount ??
    allAccounts.filter((a) => normalizeSite(a.site) === 'global').length;

  const rangeLabel =
    range === 'day' ? '今日' : range === 'week' ? '近 7 日' : range === 'month' ? '近 30 日' : '全部历史';

  return (
    <div id="tab-overview" className="ds-page-enter space-y-6">
      <PageHeader
        title="统计总览"
        subtitle={`${rangeLabel}代理流量、缓存效率与实时运行态势 · 号池维护已整合至左侧「账号池」。`}
        actions={
          <>
            <SegmentedControl<UsageChartRange>
              id="overviewRangeSeg"
              ariaLabel="首页统计周期"
              size="xs"
              value={range}
              onChange={setRange}
              options={[
                { value: 'day', label: '今日' },
                { value: 'week', label: '7 日' },
                { value: 'month', label: '30 日' },
                { value: 'all', label: '全部' },
              ]}
            />
            <Button variant="ghost" size="xs" onClick={() => setDiagOpen(true)}>
              <Code2 className="w-3 h-3" />
              快照
            </Button>
            <Button variant="ghost" size="xs" onClick={() => setHelpOpen(true)}>
              <BookOpen className="w-3 h-3" />
              说明
            </Button>
          </>
        }
      />

      {/* 扁平极简指标带（纯细线分割，彻底告别堆叠方块） */}
      <div className="border-y border-[var(--border)] grid grid-cols-2 lg:grid-cols-4 divide-y sm:divide-y-0 lg:divide-x divide-[var(--border)]">
        <div className="py-4 pr-4 lg:px-5 lg:first:pl-0">
          <div className="text-[11px] text-[var(--fg-3)]">Token 消耗（{rangeLabel}）</div>
          <div className="mt-1.5 flex items-baseline gap-2">
            <span id="mTokens" className="ds-mono text-[26px] font-medium leading-none text-[var(--fg)]">
              {formatCompactNumber(displayTokens)}
            </span>
            <span className="ds-mono text-xs text-[var(--ok)]">
              {uSum?.cacheHitRate != null && uSum.cacheHitRate >= 0
                ? `缓存 ${formatHitRate(uSum.cacheHitRate)}`
                : `缓存 ${formatCompactNumber(stats.totalCachedTokens || 0)}`}
            </span>
          </div>
        </div>

        <div className="py-4 pl-4 lg:px-5">
          <div className="text-[11px] text-[var(--fg-3)]">请求与成功率</div>
          <div className="mt-1.5 flex items-baseline gap-2">
            <span id="mSF" className="ds-mono text-[26px] font-medium leading-none text-[var(--fg)]">
              {displayRequests.toLocaleString()}
            </span>
            <span
              className={`ds-mono text-xs ${
                displayFailed > 0 ? 'text-[var(--warn)]' : 'text-[var(--fg-3)]'
              }`}
            >
              {successRate != null
                ? `${successRate.toFixed(1)}% 成功${displayFailed > 0 ? ` · ${displayFailed} 失败` : ''}`
                : '暂无失败'}
            </span>
          </div>
        </div>

        <div className="py-4 pr-4 lg:px-5" id="concurrencyBar">
          <div className="text-[11px] text-[var(--fg-3)]">实时并发与限流</div>
          <div className="mt-1.5 flex items-baseline gap-2">
            <span id="ccInFlight" className="ds-mono text-[26px] font-medium leading-none text-[var(--fg)]">
              {concurrency.globalInFlight}
            </span>
            <span className="text-xs text-[var(--fg-3)]">
              在途 · 峰值 <strong id="ccPeak" className="ds-mono text-[var(--fg-2)] font-medium">{concurrency.peakInFlight}</strong>
              {concurrency.rateLimits > 0 && (
                <>
                  {' · '}
                  <strong id="ccLimited" className="ds-mono text-[var(--warn)] font-medium">
                    限流 {concurrency.rateLimits}
                  </strong>
                </>
              )}
            </span>
          </div>
        </div>

        <div className="py-4 pl-4 lg:px-5 lg:last:pr-0">
          <div className="text-[11px] text-[var(--fg-3)]">
            活跃号池（{siteLabel(site)}站）
          </div>
          <div className="mt-1.5 flex items-baseline justify-between gap-2">
            <div className="flex items-baseline gap-1.5 min-w-0">
              <span id="mEnabled" className="ds-mono text-[26px] font-medium leading-none text-[var(--fg)]">
                {poolSummary.healthy}
              </span>
              <span className="ds-mono text-xs text-[var(--fg-3)]">
                / {poolSummary.enabled} 启用
              </span>
            </div>
            {poolSummary.attention > 0 ? (
              <button
                type="button"
                onClick={onNavigatePoolIssues}
                className="text-[11px] text-[var(--warn)] hover:underline underline-offset-2 cursor-pointer whitespace-nowrap"
              >
                {poolSummary.attention} 需处理 →
              </button>
            ) : (
              <span id="mCredits" className="ds-mono text-xs text-[var(--fg-3)] truncate">
                {poolSummary.unlimited
                  ? '不限量'
                  : poolSummary.withSnapshot > 0
                    ? `${formatCompactNumber(Math.round(poolSummary.remaining))} 分`
                    : '—'}
              </span>
            )}
          </div>
        </div>
      </div>

      {/* 视觉主角：全宽大折线统计图 */}
      <Card id="overviewHeroChart" className="p-5 sm:p-6">
        <div className="flex flex-wrap items-baseline justify-between gap-2 mb-3">
          <div>
            <h2 className="ds-heading text-base text-[var(--fg)] m-0">
              Token 消耗与缓存命中率趋势
            </h2>
          </div>
          {uSum && (uSum.creditRows || 0) > 0 && (
            <span className="ds-mono text-[11px] text-[var(--fg-3)]">
              周期 Credits 累计 <strong className="text-[var(--fg)]">{(uSum.credits || 0).toFixed(4)}</strong>
            </span>
          )}
        </div>
        <UsageLineChart
          series={usageData?.series || []}
          totalRequests={displayRequests}
          range={range}
          height={310}
          emptyAction={
            <Button variant="ghost" size="xs" onClick={() => onNavigate('tab-client')}>
              配置客户端接入
              <ArrowRight className="w-3 h-3" />
            </Button>
          }
        />
      </Card>

      {/* 底部极简双列纯分割线摘要（无卡片盒墙） */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 pt-1">
        {/* 左 7 栏：按模型消耗分布 */}
        <section className="lg:col-span-7 space-y-3">
          <div className="flex items-center justify-between border-b border-[var(--border)] pb-2.5">
            <h3 className="text-xs font-medium uppercase tracking-[0.08em] text-[var(--fg-2)] m-0">
              热门模型消耗（{rangeLabel}）
            </h3>
            <button
              type="button"
              onClick={() => onNavigate('tab-usage')}
              className="text-xs text-[var(--fg-2)] hover:text-[var(--fg)] inline-flex items-center gap-1 cursor-pointer transition-colors"
            >
              全部请求明细
              <ArrowRight className="w-3 h-3" />
            </button>
          </div>

          {!topModels.length ? (
            <div className="py-6 text-xs text-[var(--fg-3)]">
              当前周期暂无按模型统计记录。
            </div>
          ) : (
            <div className="divide-y divide-[var(--border)]">
              {topModels.map((row) => {
                const barPct = Math.max(4, Math.round(((row.totalTokens || 0) / maxModelTokens) * 100));
                const hit = row.cacheHitRate ?? -1;
                return (
                  <div key={row.model} className="py-2.5 flex items-center gap-4 text-xs">
                    <div className="w-40 sm:w-48 ds-mono text-[var(--fg)] truncate shrink-0" title={row.model}>
                      {row.model}
                    </div>
                    <div className="flex-1 h-1.5 rounded-full bg-[var(--surface-2)] overflow-hidden min-w-[60px]">
                      <div
                        className="h-full rounded-full bg-[var(--accent)] opacity-60"
                        style={{ width: `${barPct}%` }}
                      />
                    </div>
                    <div className="ds-mono text-[var(--fg)] w-16 text-right shrink-0">
                      {formatCompactNumber(row.totalTokens)}
                    </div>
                    <div className="ds-mono text-[11px] w-14 text-right shrink-0 text-[var(--fg-3)]">
                      {hit >= 0 ? (
                        <span className={hit >= 40 ? 'text-[var(--ok)]' : 'text-[var(--fg-2)]'}>
                          {formatHitRate(hit)}
                        </span>
                      ) : (
                        '—'
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </section>

        {/* 右 5 栏：极简运行摘要（纯细线键值行） */}
        <section className="lg:col-span-5 space-y-3">
          <div className="flex items-center justify-between border-b border-[var(--border)] pb-2.5">
            <h3 className="text-xs font-medium uppercase tracking-[0.08em] text-[var(--fg-2)] m-0">
              运行摘要
            </h3>
            <button
              id="btnGoAccounts"
              type="button"
              onClick={() => onNavigate('tab-pool')}
              className="text-xs text-[var(--fg-2)] hover:text-[var(--fg)] inline-flex items-center gap-1 cursor-pointer transition-colors"
            >
              管理账号池
              <ArrowRight className="w-3 h-3" />
            </button>
          </div>

          <div className="divide-y divide-[var(--border)] text-xs">
            <div className="py-2.5 flex items-center justify-between gap-3">
              <span className="text-[var(--fg-3)]">双区账号规模</span>
              <span className="ds-mono text-[var(--fg)]">
                国内 {domesticCount} · 国际 {globalCount}
              </span>
            </div>

            <div className="py-2.5 flex items-center justify-between gap-3">
              <span className="text-[var(--fg-3)]">号池健康</span>
              {poolSummary.attention > 0 ? (
                <button
                  type="button"
                  onClick={onNavigatePoolIssues}
                  className="inline-flex items-center gap-1.5 text-[var(--warn)] font-medium hover:underline underline-offset-2 cursor-pointer"
                >
                  {poolSummary.attention} 个账号需关注
                  <ArrowRight className="w-3 h-3" />
                </button>
              ) : poolSummary.enabled === 0 ? (
                <button
                  type="button"
                  onClick={() => onNavigate('tab-pool')}
                  className="text-[var(--fg-2)] hover:text-[var(--fg)] underline underline-offset-2 cursor-pointer"
                >
                  尚未添加账号 · 前往授权
                </button>
              ) : (
                <span className="text-[var(--ok)]">全部启用账号运行正常</span>
              )}
            </div>

            <div id="watchBar" className="py-2.5 flex items-center justify-between gap-3">
              <span className="text-[var(--fg-3)]">基线波动监测</span>
              {watch?.enabled ? (
                <span className="inline-flex items-center gap-2">
                  <span className="ds-mono text-[var(--fg-2)]">
                    错率 {formatPercent(watch.metrics?.errorRate)} · P50 {watch.metrics?.latencyP50 || 0}ms
                  </span>
                  <StatusBadge id="watchState" tone={watch.ok ? 'ok' : 'err'} dot>
                    {watch.ok ? '正常' : watch.reason || '异常'}
                  </StatusBadge>
                </span>
              ) : (
                <span
                  className="text-[var(--fg-3)]"
                  title="在 .env 设置 CODEBUDDY_PROXY_WATCH=true 开启基线监测"
                >
                  未启用
                </span>
              )}
            </div>

            <div className="py-2.5 flex items-center justify-between gap-3">
              <span className="text-[var(--fg-3)]">客户端接入</span>
              <button
                type="button"
                onClick={() => onNavigate('tab-client')}
                className="text-[var(--fg-2)] hover:text-[var(--fg)] inline-flex items-center gap-1 cursor-pointer transition-colors"
              >
                复制 Base URL 与 Key
                <ArrowRight className="w-3 h-3" />
              </button>
            </div>
          </div>
        </section>
      </div>

      {/* 右侧滑出：调度与熔断机制说明（不占首屏） */}
      <SlideDrawer
        open={helpOpen}
        onClose={() => setHelpOpen(false)}
        title="调度、限频隔离与思考链透传"
        subtitle="了解网关在多账号轮换、冷却自愈与推理字段透传上的默认工程行为。"
      >
        <div className="space-y-4 text-xs leading-relaxed text-[var(--fg-2)]">
          <div className="ds-surface-2 p-4 space-y-2">
            <div className="font-medium text-sm text-[var(--fg)] flex items-center gap-2">
              <ShieldAlert className="w-4 h-4 text-[var(--warn)]" />
              429 限频隔离与自动熔断
            </div>
            <p className="m-0">
              同一会话默认软钉在一个账号上以最大化上游 prompt 缓存命中率。新会话只补缺失或超过 5
              分钟的额度快照，再按剩余最大选号。
            </p>
            <p className="m-0">
              纯 429 / rate limit 触发 2 分钟短冷却，松钉后并行刷新候选额度并切换至剩余最大账号；6004 /
              配额类错误会查询官方余额并对齐至 CycleEnd / SlicePeriod 结束，且只冷却对应模型不误伤同号其他模型。111xx
              策略码为 5 分钟，部分 5xx 为 30 秒。
            </p>
          </div>
          <div className="ds-surface-2 p-4 space-y-2">
            <div className="font-medium text-sm text-[var(--fg)] flex items-center gap-2">
              <RefreshCw className="w-4 h-4 text-[var(--info)]" />
              本地实时扣减余额与积分下限
            </div>
            <p className="m-0">
              每次请求结束后会按上游返回的实际 <code className="ds-mono">credit</code>{' '}
              在本地实时扣减余额快照，使选号门槛（Quota Floor）即时生效；点击顶栏「刷新」可随时向官网拉取精确最新余额并解除已恢复账号的额度冷却。
            </p>
          </div>
          <div className="ds-surface-2 p-4 space-y-2">
            <div className="font-medium text-sm text-[var(--fg)]">Reasoning 思考链透传</div>
            <p className="m-0">
              支持客户端 <code className="ds-mono">reasoning_effort</code> /{' '}
              <code className="ds-mono">reasoning</code> 入站映射；流式与非流式均可透传{' '}
              <code className="ds-mono">reasoning_content</code>（视上游模型是否开启思考模式）。
            </p>
          </div>
        </div>
      </SlideDrawer>

      {/* 右侧滑出：完整运行快照 JSON 诊断 */}
      <SlideDrawer
        open={diagOpen}
        onClose={() => setDiagOpen(false)}
        title="运行快照诊断"
        subtitle="当前 /direct-admin/api/status 原始 JSON 载荷，便于排障与反馈 Issue。"
        widthClass="max-w-2xl"
      >
        <div className="space-y-3">
          <div className="flex justify-end">
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                copyToClipboard(JSON.stringify(status || {}, null, 2))
                  .then(() => showToast('完整状态快照已复制'))
                  .catch((e) => showToast(String(e), 'error'));
              }}
            >
              <Copy className="w-3 h-3" />
              复制 JSON
            </Button>
          </div>
          <pre
            id="statusRaw"
            className="ds-surface-2 ds-mono text-[11px] leading-relaxed p-4 overflow-auto max-h-[72vh] text-[var(--fg-2)] m-0 whitespace-pre-wrap break-all"
          >
            {status ? JSON.stringify(status, null, 2) : 'loading…'}
          </pre>
        </div>
      </SlideDrawer>
    </div>
  );
}
