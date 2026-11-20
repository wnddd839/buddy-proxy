import { useMemo, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  BookOpen,
  Code2,
  Copy,
  RefreshCw,
  ShieldAlert,
  Sparkles,
} from 'lucide-react';
import type {
  AccountSummary,
  AccountUsageItem,
  StatusPayload,
} from '@/types/api';
import {
  computeAccountStates,
  computeCreditLevel,
  computeExpirySummary,
  type ConcurrencySummary,
} from '@/lib/domain';
import {
  formatCompactNumber,
  formatPercent,
  normalizeSite,
  siteLabel,
} from '@/lib/format';
import { copyToClipboard } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, PageHeader, StatCard } from '@/components/ui/Card';
import { SlideDrawer } from '@/components/ui/SlideDrawer';
import { StatusBadge } from '@/components/ui/StatusBadge';
import type { TabId } from '@/components/layout/AppShell';

interface OverviewPageProps {
  status: StatusPayload | null;
  activeSite: string;
  usageByAccount: Record<string, AccountUsageItem>;
  concurrency: ConcurrencySummary;
  onNavigate: (tab: TabId) => void;
  onAccountAction: (
    accountId: string,
    action: 'test' | 'usage' | 'refresh' | 'disable',
  ) => Promise<void>;
}

export function OverviewPage({
  status,
  activeSite,
  usageByAccount,
  concurrency,
  onNavigate,
  onAccountAction,
}: OverviewPageProps) {
  const { showToast } = useToast();
  const [helpOpen, setHelpOpen] = useState(false);
  const [diagOpen, setDiagOpen] = useState(false);
  const [busyKey, setBusyKey] = useState('');

  const allAccounts = status?.accounts?.accounts || [];
  const site = normalizeSite(activeSite);
  const siteAccounts = useMemo(
    () => allAccounts.filter((a) => normalizeSite(a.site) === site),
    [allAccounts, site],
  );

  // 汇总当前号池所有启用账号的 Credits 快照与 7 天内将过期积分
  const poolCredits = useMemo(() => {
    let remaining = 0;
    let total = 0;
    let unlimited = false;
    let withSnapshot = 0;
    let expiring7d = 0;

    for (const a of siteAccounts) {
      if (!a.enabled) continue;
      const u = usageByAccount[a.id];
      if (u?.credits) {
        withSnapshot++;
        if (u.credits.unlimited) {
          unlimited = true;
        } else {
          if (u.credits.remaining != null) remaining += Number(u.credits.remaining);
          if (u.credits.total != null) total += Number(u.credits.total);
        }
        const exp = computeExpirySummary(u);
        expiring7d += exp.expiringAmount7d;
      } else if (a.quotaRemaining != null) {
        withSnapshot++;
        remaining += Number(a.quotaRemaining);
      }
    }
    return { remaining, total, unlimited, withSnapshot, expiring7d };
  }, [siteAccounts, usageByAccount]);

  // 账号健康度分类计数
  const healthBreakdown = useMemo(() => {
    let healthy = 0;
    let cooling = 0;
    let problem = 0;
    let disabled = 0;
    for (const a of siteAccounts) {
      if (!a.enabled) {
        disabled++;
        continue;
      }
      const states = computeAccountStates(a);
      if (!(a.loggedIn && a.hasCredentials) || a.tokenExpired || states.some((s) => s.tone === 'err')) {
        problem++;
      } else if (states.length > 0) {
        cooling++;
      } else {
        healthy++;
      }
    }
    return { healthy, cooling, problem, disabled, enabled: healthy + cooling + problem };
  }, [siteAccounts]);

  // 需要关注列表
  const attentionRows = useMemo(() => {
    return siteAccounts
      .filter((a) => a.enabled)
      .map((a) => {
        const reasons = computeAccountStates(a);
        if (!(a.loggedIn && a.hasCredentials)) {
          reasons.unshift({ tone: 'err', text: '未登录' });
        }
        if (a.tokenExpired) {
          reasons.unshift({ tone: 'err', text: 'Token 过期' });
        }
        const usage = usageByAccount[a.id];
        if (usage?.credits) {
          const lvl = computeCreditLevel(usage);
          if (lvl !== 'good') {
            reasons.push({
              tone: lvl === 'bad' ? 'err' : 'warn',
              text: lvl === 'bad' ? '余额告急' : '余额偏低',
            });
          }
        }
        if (a.lastError) {
          reasons.push({ tone: 'neutral', text: '最近报错' });
        }
        return { account: a, reasons };
      })
      .filter((r) => r.reasons.length > 0);
  }, [siteAccounts, usageByAccount]);

  const stats = status?.stats || {};
  const watch = status?.watch;
  const totalTokens = stats.totalTokens || 0;
  const cachedTokens = stats.totalCachedTokens || 0;
  const creditRatio =
    poolCredits.total > 0
      ? Math.min(100, Math.max(0, (poolCredits.remaining / poolCredits.total) * 100))
      : 0;
  const loggedIn = !!status?.accounts?.loggedIn;

  const runQuick = async (
    account: AccountSummary,
    act: 'test' | 'usage' | 'refresh' | 'disable',
  ) => {
    const key = `${account.id}:${act}`;
    setBusyKey(key);
    try {
      await onAccountAction(account.id, act);
    } finally {
      setBusyKey('');
    }
  };

  const totalBarDenom = Math.max(1, healthBreakdown.enabled);

  return (
    <div id="tab-overview" className="ds-page-enter space-y-5">
      <PageHeader
        title="网关概览"
        subtitle={`当前聚焦「${siteLabel(site)}站」号池 · 实时掌握账号健康、聚合余额、在途并发与待处理告警。`}
        actions={
          <>
            <Button variant="ghost" size="sm" onClick={() => setDiagOpen(true)}>
              <Code2 className="w-3.5 h-3.5" />
              运行快照 JSON
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setHelpOpen(true)}>
              <BookOpen className="w-3.5 h-3.5" />
              调度与熔断说明
            </Button>
          </>
        }
      />

      {/* 4 格核心 KPI */}
      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3.5">
        <StatCard
          label="号池总剩余 Credits"
          valueId="mCredits"
          value={
            poolCredits.unlimited
              ? '不限量'
              : poolCredits.withSnapshot > 0
                ? formatCompactNumber(Math.round(poolCredits.remaining))
                : loggedIn
                  ? '待查询'
                  : '—'
          }
          sub={
            !poolCredits.unlimited && poolCredits.total > 0
              ? `/ ${formatCompactNumber(Math.round(poolCredits.total))}`
              : undefined
          }
          badge={
            <StatusBadge
              tone={
                !loggedIn
                  ? 'neutral'
                  : poolCredits.unlimited || creditRatio >= 25 || poolCredits.total === 0
                    ? 'ok'
                    : creditRatio >= 10
                      ? 'warn'
                      : 'err'
              }
            >
              {!loggedIn ? '未登录' : poolCredits.unlimited ? '不限量' : `${poolCredits.withSnapshot} 号快照`}
            </StatusBadge>
          }
          footer={
            <div>
              <div className="h-1.5 w-full rounded-full overflow-hidden bg-[var(--surface-2)]">
                <div
                  className="h-full rounded-full transition-all duration-300"
                  style={{
                    width: `${poolCredits.unlimited ? 100 : creditRatio}%`,
                    background:
                      creditRatio < 10 && !poolCredits.unlimited && poolCredits.total > 0
                        ? 'var(--err)'
                        : creditRatio < 25 && !poolCredits.unlimited && poolCredits.total > 0
                          ? 'var(--warn)'
                          : 'var(--ok)',
                  }}
                />
              </div>
              <div className="mt-2 flex items-center justify-between text-[11px] text-[var(--fg-3)]">
                <span>
                  {poolCredits.expiring7d > 0
                    ? `7 日内到期 ${poolCredits.expiring7d.toFixed(0)} 分`
                    : loggedIn
                      ? '登录态正常'
                      : '请先添加并授权账号'}
                </span>
                {poolCredits.total > 0 && !poolCredits.unlimited && (
                  <span className="ds-mono">{creditRatio.toFixed(1)}%</span>
                )}
              </div>
            </div>
          }
        />

        <StatCard
          label={`可用账号（${siteLabel(site)}站）`}
          valueId="mEnabled"
          value={healthBreakdown.healthy}
          sub={`/ ${healthBreakdown.enabled} 启用${healthBreakdown.disabled ? ` · ${healthBreakdown.disabled} 禁用` : ''}`}
          badge={
            healthBreakdown.enabled === 0 ? (
              <StatusBadge tone="neutral">暂无启用</StatusBadge>
            ) : healthBreakdown.problem > 0 ? (
              <StatusBadge tone="err">{healthBreakdown.problem} 异常</StatusBadge>
            ) : healthBreakdown.cooling > 0 ? (
              <StatusBadge tone="warn">{healthBreakdown.cooling} 冷却</StatusBadge>
            ) : (
              <StatusBadge tone="ok">全部健康</StatusBadge>
            )
          }
          footer={
            <div>
              <div className="h-1.5 w-full rounded-full overflow-hidden bg-[var(--surface-2)] flex gap-0.5">
                {healthBreakdown.healthy > 0 && (
                  <div
                    style={{
                      width: `${(healthBreakdown.healthy / totalBarDenom) * 100}%`,
                      background: 'var(--ok)',
                    }}
                  />
                )}
                {healthBreakdown.cooling > 0 && (
                  <div
                    style={{
                      width: `${(healthBreakdown.cooling / totalBarDenom) * 100}%`,
                      background: 'var(--warn)',
                    }}
                  />
                )}
                {healthBreakdown.problem > 0 && (
                  <div
                    style={{
                      width: `${(healthBreakdown.problem / totalBarDenom) * 100}%`,
                      background: 'var(--err)',
                    }}
                  />
                )}
              </div>
              <div className="mt-2 flex items-center gap-3 text-[11px] text-[var(--fg-3)]">
                <span className="inline-flex items-center gap-1">
                  <i className="w-1.5 h-1.5 rounded-full bg-[var(--ok)]" />
                  正常 {healthBreakdown.healthy}
                </span>
                <span className="inline-flex items-center gap-1">
                  <i className="w-1.5 h-1.5 rounded-full bg-[var(--warn)]" />
                  冷却 {healthBreakdown.cooling}
                </span>
                <span className="inline-flex items-center gap-1">
                  <i className="w-1.5 h-1.5 rounded-full bg-[var(--err)]" />
                  异常 {healthBreakdown.problem}
                </span>
              </div>
            </div>
          }
        />

        <StatCard
          label="实时并发与限流"
          valueId="ccInFlight"
          value={concurrency.globalInFlight}
          sub="全局在途"
          badge={
            <StatusBadge tone={concurrency.rateLimits > 0 ? 'warn' : 'info'}>
              {concurrency.rateLimits > 0 ? `限流 ${concurrency.rateLimits}` : '平稳'}
            </StatusBadge>
          }
          footer={
            <div
              id="concurrencyBar"
              className="grid grid-cols-2 gap-2 pt-2.5 border-t border-[var(--border)] text-[11px]"
            >
              <div>
                <span className="text-[var(--fg-3)]">1m 峰值</span>
                <strong id="ccPeak" className="ds-mono text-[var(--fg)] ml-1.5">
                  {concurrency.peakInFlight}
                </strong>
              </div>
              <div>
                <span className="text-[var(--fg-3)]">1h 限流</span>
                <strong
                  id="ccLimited"
                  className={`ds-mono ml-1.5 ${
                    concurrency.rateLimits > 0 ? 'text-[var(--warn)]' : 'text-[var(--fg)]'
                  }`}
                >
                  {concurrency.rateLimits}
                </strong>
              </div>
            </div>
          }
        />

        <StatCard
          label="累计流量与 Tokens"
          valueId="mTokens"
          value={formatCompactNumber(totalTokens)}
          title={`精确总 Tokens：${totalTokens.toLocaleString()}，缓存命中：${cachedTokens.toLocaleString()}`}
          sub="Tokens"
          badge={
            <StatusBadge tone={(stats.failedRequests || 0) > 0 ? 'warn' : 'ok'}>
              {stats.successRequests || 0} / {stats.failedRequests || 0}
            </StatusBadge>
          }
          footer={
            <div className="grid grid-cols-2 gap-2 pt-2.5 border-t border-[var(--border)] text-[11px]">
              <div>
                <span className="text-[var(--fg-3)]">成功/失败</span>
                <strong id="mSF" className="ds-mono text-[var(--fg)] ml-1.5">
                  {stats.successRequests || 0}/{stats.failedRequests || 0}
                </strong>
              </div>
              <div>
                <span className="text-[var(--fg-3)]">缓存</span>
                <strong className="ds-mono text-[var(--fg)] ml-1.5">
                  {formatCompactNumber(cachedTokens)}
                </strong>
              </div>
            </div>
          }
        />
      </div>

      {/* 主次双栏：左侧「需要关注（行内快捷处理）」 + 右侧「波动监测与号池分布」 */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">
        <Card className="lg:col-span-8 p-5">
          <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
            <div>
              <h2 className="ds-heading text-base text-[var(--fg)] m-0 flex items-center gap-2">
                <AlertTriangle className="w-4 h-4 text-[var(--warn)]" />
                需要关注
              </h2>
              <p className="text-xs text-[var(--fg-3)] mt-0.5">
                汇总当前号池中未登录、Token 过期、冷却、余额告急或最近报错的启用账号，支持直接行内处置。
              </p>
            </div>
            <Button
              id="btnGoAccounts"
              variant="ghost"
              size="xs"
              onClick={() => onNavigate('tab-pool')}
            >
              管理全部账号
              <ArrowRight className="w-3 h-3" />
            </Button>
          </div>

          <div id="attentionList">
            {!allAccounts.length ? (
              <EmptyState
                title="号池为空"
                description="尚未添加任何 CodeBuddy / WorkBuddy 账号，请前往账号池完成 OAuth 授权或批量导入。"
                action={
                  <Button variant="primary" size="sm" onClick={() => onNavigate('tab-pool')}>
                    前往添加账号
                  </Button>
                }
              />
            ) : !attentionRows.length ? (
              <EmptyState
                title="当前号池一切正常"
                description={`${siteLabel(site)}站所有已启用账号均处于健康可调度状态，无冷却、过期或余额预警。`}
              />
            ) : (
              <div className="divide-y divide-[var(--border)]">
                {attentionRows.map(({ account, reasons }) => (
                  <div
                    key={account.id}
                    className="py-3.5 first:pt-1 last:pb-1 flex flex-wrap items-center justify-between gap-3"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="font-medium text-sm text-[var(--fg)]">
                          {account.label || account.id}
                        </span>
                        {reasons.map((r, idx) => (
                          <StatusBadge key={idx} tone={r.tone} dot={r.tone === 'err' || r.tone === 'warn'}>
                            {r.text}
                          </StatusBadge>
                        ))}
                      </div>
                      {account.lastError && (
                        <p
                          title={account.lastError}
                          className="ds-mono text-[11px] text-[var(--fg-3)] mt-1 truncate m-0"
                        >
                          {account.lastError}
                        </p>
                      )}
                    </div>
                    <div className="flex items-center gap-1.5 shrink-0">
                      {account.tokenExpired && (
                        <Button
                          variant="primary"
                          size="xs"
                          loading={busyKey === `${account.id}:refresh`}
                          onClick={() => runQuick(account, 'refresh')}
                        >
                          刷新 Token
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="xs"
                        loading={busyKey === `${account.id}:usage`}
                        onClick={() => runQuick(account, 'usage')}
                      >
                        查余额
                      </Button>
                      <Button
                        variant="ghost"
                        size="xs"
                        loading={busyKey === `${account.id}:test`}
                        onClick={() => runQuick(account, 'test')}
                      >
                        测活
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </Card>

        <div className="lg:col-span-4 space-y-4">
          {/* 波动监测卡片 */}
          <Card id="watchBar" className="p-5">
            <div className="flex items-center justify-between mb-3">
              <h2 className="ds-heading text-base text-[var(--fg)] m-0 flex items-center gap-2">
                <Activity className="w-4 h-4 text-[var(--info)]" />
                波动监测
              </h2>
              {watch?.enabled ? (
                <StatusBadge id="watchState" tone={watch.ok ? 'ok' : 'err'} dot>
                  {watch.ok ? '基线正常' : watch.reason || '异常'}
                </StatusBadge>
              ) : (
                <StatusBadge tone="neutral">未启用</StatusBadge>
              )}
            </div>

            {watch?.enabled ? (
              <div className="space-y-2 text-xs">
                <div className="flex items-center justify-between py-1.5 border-b border-[var(--border)]">
                  <span className="text-[var(--fg-2)]">滚动错误率</span>
                  <span id="watchErr" className="ds-mono font-medium text-[var(--fg)]">
                    {formatPercent(watch.metrics?.errorRate)}
                  </span>
                </div>
                <div className="flex items-center justify-between py-1.5 border-b border-[var(--border)]">
                  <span className="text-[var(--fg-2)]">推理 Token 占比</span>
                  <span id="watchMiss" className="ds-mono font-medium text-[var(--fg)]">
                    {(watch.metrics?.reasoningAsked || 0) > 0
                      ? `${formatPercent(1 - (watch.metrics?.reasoningMissRate || 0))} · ${
                          (watch.metrics?.reasoningAsked || 0) -
                          (watch.metrics?.reasoningMissed || 0)
                        }/${watch.metrics?.reasoningAsked}`
                      : '无样本'}
                  </span>
                </div>
                <div className="flex items-center justify-between py-1.5 border-b border-[var(--border)]">
                  <span className="text-[var(--fg-2)]">延迟 P50</span>
                  <span id="watchP50" className="ds-mono font-medium text-[var(--fg)]">
                    {watch.metrics?.latencyP50 || 0}ms
                  </span>
                </div>
                <div className="flex items-center justify-between pt-1">
                  <span className="text-[var(--fg-2)]">统计样本</span>
                  <span id="watchSamples" className="ds-mono font-medium text-[var(--fg)]">
                    {watch.metrics?.samples || 0}
                  </span>
                </div>
              </div>
            ) : (
              <p className="text-xs text-[var(--fg-3)] leading-relaxed m-0">
                波动监测默认关闭。在 <code className="ds-mono text-[var(--fg-2)]">.env</code> 中设置{' '}
                <code className="ds-mono text-[var(--fg-2)]">CODEBUDDY_PROXY_WATCH=true</code>{' '}
                即可自动基线监测错误率、推理占比与 P50 耗时，并支持 Webhook / SMTP 告警。
              </p>
            )}
          </Card>

          {/* 站点号池分布总览 */}
          <Card className="p-5">
            <h2 className="ds-heading text-base text-[var(--fg)] m-0 mb-3 flex items-center gap-2">
              <Sparkles className="w-4 h-4 text-[var(--fg-2)]" />
              双区号池分布
            </h2>
            <div className="grid grid-cols-2 gap-2.5">
              <div className="ds-surface-2 p-3">
                <div className="text-[11px] text-[var(--fg-3)]">国内站账号</div>
                <div className="ds-mono text-lg font-medium text-[var(--fg)] mt-1">
                  {status?.accounts?.domesticCount ??
                    allAccounts.filter((a) => normalizeSite(a.site) === 'domestic').length}
                </div>
              </div>
              <div className="ds-surface-2 p-3">
                <div className="text-[11px] text-[var(--fg-3)]">国际站账号</div>
                <div className="ds-mono text-lg font-medium text-[var(--fg)] mt-1">
                  {status?.accounts?.globalCount ??
                    allAccounts.filter((a) => normalizeSite(a.site) === 'global').length}
                </div>
              </div>
            </div>
            <div className="mt-3 text-[11px] text-[var(--fg-3)] flex items-center justify-between">
              <span id="mLogin" className="truncate">
                {loggedIn
                  ? `已登录 · ${
                      status?.accounts?.primary?.userNickname ||
                      status?.accounts?.primary?.userName ||
                      status?.accounts?.primary?.label ||
                      '号池就绪'
                    }`
                  : '未登录'}
              </span>
              <button
                type="button"
                onClick={() => onNavigate('tab-client')}
                className="text-[var(--fg)] underline underline-offset-2 cursor-pointer shrink-0 ml-2"
              >
                接入客户端
              </button>
            </div>
          </Card>
        </div>
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
              在本地实时扣减余额快照，使选号门槛（Quota Floor）即时生效；点击顶栏「刷新状态」可随时向官网拉取精确最新余额并解除已恢复账号的额度冷却。
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
