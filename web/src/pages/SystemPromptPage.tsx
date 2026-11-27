import { useCallback, useEffect, useState } from 'react';
import { BookOpen, CheckCircle2, Copy, RefreshCw, ShieldCheck } from 'lucide-react';
import type {
  SystemPromptEvent,
  SystemPromptHistoryResponse,
  SystemPromptMode,
  SystemPromptOverview,
} from '@/types/api';
import { resolveLocalDateList, spFormInfo, spResultInfo } from '@/lib/domain';
import { formatBytes, formatShortClock, formatTime, shortId } from '@/lib/format';
import { apiRequest, copyToClipboard } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, PageHeader, StatCard } from '@/components/ui/Card';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { SlideDrawer } from '@/components/ui/SlideDrawer';
import { StatusBadge } from '@/components/ui/StatusBadge';

interface SystemPromptPageProps {
  active: boolean;
  onModeSynced: (mode: SystemPromptMode) => void;
}

const MODE_CARDS: Array<{
  mode: SystemPromptMode;
  tag: string;
  headline: string;
  desc: string;
}> = [
  {
    mode: 'auto',
    tag: '推荐默认',
    headline: '先直传，被拒再改写',
    desc: '首轮直传 system；遇到 11128 策略拦截时自动折叠重放，兼顾高权重与零失败。',
  },
  {
    mode: 'strip',
    tag: '精确剥离',
    headline: '删掉身份句，其余直传',
    desc: '仅剔除触发 11128 的精确身份句，完整保留 AGENTS.md 与规则的高优先级。',
  },
  {
    mode: 'keep',
    tag: '严格直传',
    headline: '直传 system，绝不改写',
    desc: '遇到上游拦截直接报错不重试，适合排查上游真实拦截规则与行为。',
  },
  {
    mode: 'fold',
    tag: '始终稳妥',
    headline: '改写 system，全部折叠',
    desc: '每次均将客户端指令移入首条 user 消息，适合批量自动化任务。',
  },
];

export function SystemPromptPage({ active, onModeSynced }: SystemPromptPageProps) {
  const { showToast } = useToast();
  const [overview, setOverview] = useState<SystemPromptOverview | null>(null);
  const [mode, setMode] = useState<SystemPromptMode>('auto');
  const [switchingMode, setSwitchingMode] = useState(false);

  const [viewTab, setViewTab] = useState<'realtime' | 'history'>('realtime');
  const [onlyProblems, setOnlyProblems] = useState(false);
  const [guideOpen, setGuideOpen] = useState(false);

  // 历史分页状态
  const [selectedDate, setSelectedDate] = useState('');
  const [historyPage, setHistoryPage] = useState(1);
  const [historyResp, setHistoryResp] = useState<SystemPromptHistoryResponse | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const historyPageSize = 100;

  const fetchHistory = useCallback(
    async (dateStr: string, pageNum: number, problemsOnly: boolean) => {
      if (!dateStr) return;
      let url = `/direct-admin/api/system-prompt/events?date=${encodeURIComponent(
        dateStr,
      )}&page=${pageNum}&size=${historyPageSize}`;
      if (problemsOnly) url += '&onlyProblems=1';
      const data = await apiRequest<SystemPromptHistoryResponse>(url);
      setHistoryResp(data);
    },
    [],
  );

  const fetchOverviewAndHistory = useCallback(
    async (explicitDate?: string, explicitPage?: number, explicitProblems?: boolean) => {
      const data = await apiRequest<SystemPromptOverview>('/direct-admin/api/system-prompt');
      setOverview(data);
      const nextMode = (data.mode || 'auto') as SystemPromptMode;
      setMode(nextMode);
      onModeSynced(nextMode);

      const availableDates = resolveLocalDateList(data.dates || [], explicitDate || selectedDate);
      const targetDate =
        explicitDate ||
        selectedDate ||
        (availableDates.length ? availableDates[availableDates.length - 1] : '');
      if (targetDate && targetDate !== selectedDate) {
        setSelectedDate(targetDate);
      }
      if (targetDate) {
        await fetchHistory(
          targetDate,
          explicitPage ?? historyPage,
          explicitProblems ?? onlyProblems,
        );
      }
    },
    [fetchHistory, historyPage, onModeSynced, onlyProblems, selectedDate],
  );

  // 切换到本页时立刻刷新
  useEffect(() => {
    if (!active) return;
    fetchOverviewAndHistory().catch((e) =>
      showToast(e instanceof Error ? e.message : String(e), 'error'),
    );
  }, [active]);

  // 每 5 秒轮询（页面隐藏或未激活本页时暂停）
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'hidden') return;
      fetchOverviewAndHistory().catch(() => {});
    }, 5000);
    return () => window.clearInterval(timer);
  }, [active, fetchOverviewAndHistory]);

  const handleSwitchMode = async (next: SystemPromptMode) => {
    if (next === mode || switchingMode) return;
    setSwitchingMode(true);
    try {
      const data = await apiRequest<{ mode?: SystemPromptMode }>('/direct-admin/api/system-prompt', {
        method: 'POST',
        body: JSON.stringify({ mode: next }),
      });
      const applied = (data.mode || next) as SystemPromptMode;
      setMode(applied);
      onModeSynced(applied);
      showToast(`System Prompt 模式已切换：${applied}`, 'success');
      await fetchOverviewAndHistory();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setSwitchingMode(false);
    }
  };

  const handleManualRefresh = async () => {
    setRefreshing(true);
    try {
      await fetchOverviewAndHistory();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setRefreshing(false);
    }
  };

  const dateList = resolveLocalDateList(overview?.dates || [], selectedDate);
  const stats = overview?.stats || {};
  const realtimeRows = (overview?.recent || []).filter((e) => {
    if (!onlyProblems) return true;
    return (e.status ?? 0) === 0 || (e.status ?? 0) >= 400 || !!e.errorCode || !!e.degraded;
  });
  const historyRows = historyResp?.events || [];

  const renderEventTable = (
    rows: SystemPromptEvent[],
    ariaLabel: string,
    tbodyId: string,
    emptyText: string,
  ) => (
    <div className="overflow-x-auto max-h-[440px]">
      <table className="w-full text-left border-collapse text-xs min-w-[960px]" aria-label={ariaLabel}>
        <thead className="sticky top-0 z-10 bg-[var(--surface-2)]">
          <tr className="border-b border-[var(--border)] text-[11px] uppercase tracking-wider text-[var(--fg-3)]">
            <th className="py-2.5 px-3 font-medium">时间</th>
            <th className="py-2.5 px-3 font-medium">请求 ID</th>
            <th className="py-2.5 px-3 font-medium">模式</th>
            <th
              className="py-2.5 px-3 font-medium"
              title="keep=直传 system；fold=改写 system；keep(已剥离)=strip 档删过身份句；带(降级重放)/(被拦截)的是同一次 11128 降级请求的两条事件"
            >
              形态
            </th>
            <th
              className="py-2.5 px-3 font-medium"
              title="从请求头推断的客户端类型与会话键来源"
            >
              客户端
            </th>
            <th className="py-2.5 px-3 font-medium">指纹</th>
            <th className="py-2.5 px-3 font-medium">大小</th>
            <th className="py-2.5 px-3 font-medium">账号</th>
            <th className="py-2.5 px-3 font-medium">模型</th>
            <th className="py-2.5 px-3 font-medium">耗时</th>
            <th className="py-2.5 px-3 font-medium">结果</th>
          </tr>
        </thead>
        <tbody id={tbodyId} className="divide-y divide-[var(--border)]">
          {!rows.length ? (
            <tr>
              <td colSpan={11} className="py-10 text-center text-[var(--fg-3)]">
                {emptyText}
              </td>
            </tr>
          ) : (
            rows.map((e, idx) => {
              const resInfo = spResultInfo(e);
              const formInfo = spFormInfo(e);
              const rowClass = formInfo.isBlocked
                ? 'sp-blocked-row bg-[var(--err-bg)]/45'
                : formInfo.isReplay
                  ? 'sp-replay-row bg-[var(--warn-bg)]/35'
                  : 'hover:bg-[var(--surface-2)]/50';
              const sessTitle = e.sessionSource
                ? `会话键来源：${e.sessionSource}${
                    e.sessionSource === 'content'
                      ? '（靠 system+首条 user 哈希）'
                      : '（客户端显式提供）'
                  }`
                : '无会话信号';

              return (
                <tr key={`${e.requestId || 'ev'}-${e.at}-${idx}`} className={rowClass}>
                  <td
                    title={formatTime(e.at)}
                    className="py-2.5 px-3 ds-mono text-[var(--fg-2)] whitespace-nowrap"
                  >
                    {formatShortClock(e.at)}
                  </td>
                  <td className="py-2.5 px-3 ds-mono whitespace-nowrap">
                    {e.requestId ? (
                      <button
                        type="button"
                        onClick={() =>
                          copyToClipboard(e.requestId!)
                            .then(() => showToast('请求 ID 已复制'))
                            .catch(() => {})
                        }
                        className="inline-flex items-center gap-1 text-[var(--fg)] hover:underline underline-offset-2 cursor-pointer"
                      >
                        {formInfo.isReplay && <span className="text-[var(--warn)]">↳</span>}
                        <span>{shortId(e.requestId)}</span>
                        <Copy className="w-2.5 h-2.5 opacity-40" />
                      </button>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">{e.mode || '—'}</td>
                  <td className="py-2.5 px-3">
                    <StatusBadge tone={formInfo.tone}>{formInfo.label}</StatusBadge>
                  </td>
                  <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]" title={sessTitle}>
                    {e.clientHint || '—'}
                    {e.sessionSource ? (
                      <span className="text-[10px] text-[var(--fg-3)] ml-1">
                        · {e.sessionSource}
                      </span>
                    ) : null}
                  </td>
                  <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                    {e.fingerprint || '—'}
                  </td>
                  <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)] whitespace-nowrap">
                    {formatBytes(e.systemLen)}
                  </td>
                  <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                    {e.accountId ? shortId(e.accountId) : '—'}
                  </td>
                  <td className="py-2.5 px-3 ds-mono text-[var(--fg)]">{e.model || '—'}</td>
                  <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)] whitespace-nowrap">
                    {e.durationMs != null ? `${e.durationMs}ms` : '—'}
                  </td>
                  <td className="py-2.5 px-3 whitespace-nowrap">
                    <StatusBadge
                      tone={resInfo.tone}
                      title={
                        formInfo.isReplay
                          ? '与上方同 requestId 的被拦截事件是同一次请求'
                          : resInfo.title
                      }
                    >
                      {resInfo.label}
                    </StatusBadge>
                  </td>
                </tr>
              );
            })
          )}
        </tbody>
      </table>
    </div>
  );

  const totalHist = historyResp?.total || 0;
  const fromIdx = totalHist === 0 ? 0 : (historyPage - 1) * historyPageSize + 1;
  const toIdx = Math.min(historyPage * historyPageSize, totalHist);

  return (
    <div id="tab-system-prompt" className="ds-page-enter space-y-5">
      <PageHeader
        title="System Prompt 开发者面板"
        subtitle="四档模式热切换与实时观测；仅记录 SHA-256 前 8 位指纹与字节数，绝不落盘原文，历史按天保留 7 天。"
        actions={
          <>
            <Button variant="ghost" size="sm" onClick={() => setGuideOpen(true)}>
              <BookOpen className="w-3.5 h-3.5" />
              四档模式原理与示例
            </Button>
            <Button
              id="btnRefreshSp"
              variant="ghost"
              size="sm"
              loading={refreshing}
              onClick={handleManualRefresh}
            >
              <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} />
              刷新
            </Button>
          </>
        }
      />

      {/* 4 张清晰直观的模式卡片（取代 700 字首屏文档墙） */}
      <div id="spModeSeg" role="group" aria-label="system 提示词处理模式" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        {MODE_CARDS.map((card) => {
          const isActive = mode === card.mode;
          return (
            <button
              key={card.mode}
              type="button"
              data-mode={card.mode}
              disabled={switchingMode}
              onClick={() => handleSwitchMode(card.mode)}
              className={[
                'ds-surface p-3.5 text-left transition-all cursor-pointer flex flex-col justify-between gap-2',
                isActive
                  ? 'active border-[var(--accent)] ring-1 ring-[var(--accent)]'
                  : 'hover:border-[var(--border-strong)]',
              ].join(' ')}
            >
              <div>
                <div className="flex items-center justify-between gap-2">
                  <span className="ds-mono text-sm font-semibold text-[var(--fg)]">
                    {card.mode}
                  </span>
                  {isActive ? (
                    <StatusBadge tone="ok" className="!text-[10px]">
                      <CheckCircle2 className="w-3 h-3" />
                      当前生效
                    </StatusBadge>
                  ) : (
                    <span className="text-[11px] text-[var(--fg-3)]">{card.tag}</span>
                  )}
                </div>
                <div className="text-xs font-medium text-[var(--fg)] mt-1.5">{card.headline}</div>
                <p className="text-[11px] text-[var(--fg-3)] mt-1 leading-relaxed m-0">
                  {card.desc}
                </p>
              </div>
            </button>
          );
        })}
      </div>

      {/* 整齐对称的统计指标 */}
      <div className="space-y-2">
        <div className="text-[11px] text-[var(--fg-3)] flex flex-wrap items-center justify-between gap-2">
          <span>
            统计口径：
            <strong id="spStatsScope" className="text-[var(--fg-2)] font-medium ml-1">
              {overview?.statsScope === 'memory'
                ? '进程内近 24h（重启会清零）'
                : overview?.statsScope
                  ? `按 ${overview.statsScope} 落盘事件统计（重启不清零）`
                  : '按今日落盘事件统计（重启不清零）'}
            </strong>
          </span>
          <span className="inline-flex items-center gap-1">
            <ShieldCheck className="w-3.5 h-3.5 text-[var(--ok)]" />
            零原文落盘隐私保护
          </span>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-4 xl:grid-cols-7 gap-2.5">
          <StatCard
            label={overview?.statsScope === 'memory' ? '近 24h 事件' : '今日事件'}
            labelId="spTotalLabel"
            valueId="spTotal"
            value={stats.total ?? 0}
            className="p-3"
          />
          <StatCard label="keep 直传" valueId="spKeep" value={stats.keep ?? 0} className="p-3" />
          <StatCard label="fold 改写" valueId="spFold" value={stats.fold ?? 0} className="p-3" />
          <StatCard
            label="11128 降级"
            title="degraded:true 的事件数，即被 11128 拦截的失败尝试次数"
            valueId="spDegraded"
            value={stats.degraded ?? 0}
            valueClassName={(stats.degraded ?? 0) > 0 ? 'text-[var(--warn)]' : ''}
            className="p-3"
          />
          <StatCard
            label="被拦截"
            valueId="spBlocked"
            value={stats.blocked ?? 0}
            valueClassName={(stats.blocked ?? 0) > 0 ? 'text-[var(--err)]' : ''}
            className="p-3"
          />
          <StatCard
            label="其它失败"
            valueId="spFailed"
            value={stats.failed ?? 0}
            valueClassName={(stats.failed ?? 0) > 0 ? 'text-[var(--err)]' : ''}
            className="p-3"
          />
          <StatCard
            label="降级重放成功"
            valueId="spRecovered"
            value={stats.recovered == null ? '—' : stats.recovered}
            valueClassName={(stats.recovered ?? 0) > 0 ? 'text-[var(--ok)]' : ''}
            className="p-3"
          />
        </div>
      </div>

      {/* 统一的实时流 / 按日历史回溯面板 */}
      <Card className="overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--border)] bg-[var(--surface-2)]/50 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3 flex-wrap">
            <SegmentedControl<'realtime' | 'history'>
              ariaLabel="事件流视图"
              size="xs"
              value={viewTab}
              onChange={setViewTab}
              options={[
                { value: 'realtime', label: '实时事件流 (5s)', badge: realtimeRows.length },
                { value: 'history', label: '按日历史回溯', badge: totalHist },
              ]}
            />
            <label
              title="含传输失败、被拦截、降级、其它错误"
              className="inline-flex items-center gap-1.5 text-xs text-[var(--fg-2)] cursor-pointer select-none"
            >
              <input
                id="spOnlyProblems"
                type="checkbox"
                checked={onlyProblems}
                onChange={(e) => {
                  const checked = e.target.checked;
                  setOnlyProblems(checked);
                  setHistoryPage(1);
                  fetchHistory(selectedDate, 1, checked).catch(() => {});
                }}
                className="accent-[var(--accent)]"
              />
              只看问题（拦截 / 降级 / 失败）
            </label>
          </div>

          {viewTab === 'history' ? (
            <div className="flex items-center gap-2 flex-wrap text-xs">
              <label className="inline-flex items-center gap-1.5 text-[var(--fg-2)]">
                <span>日期</span>
                <select
                  id="spDateSelect"
                  aria-label="选择日期"
                  value={selectedDate}
                  onChange={(e) => {
                    const d = e.target.value;
                    setSelectedDate(d);
                    setHistoryPage(1);
                    fetchHistory(d, 1, onlyProblems).catch(() => {});
                  }}
                  className="ds-select ds-mono px-2 py-1 text-xs"
                >
                  {dateList.map((d) => {
                    const pretty =
                      d.length === 8 ? `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6, 8)}` : d;
                    return (
                      <option key={d} value={d}>
                        {pretty}
                      </option>
                    );
                  })}
                </select>
              </label>
              <Button
                id="btnSpPrev"
                variant="ghost"
                size="xs"
                disabled={historyPage <= 1}
                onClick={() => {
                  const p = Math.max(1, historyPage - 1);
                  setHistoryPage(p);
                  fetchHistory(selectedDate, p, onlyProblems).catch(() => {});
                }}
              >
                上一页
              </Button>
              <Button
                id="btnSpNext"
                variant="ghost"
                size="xs"
                disabled={!historyResp?.hasMore}
                onClick={() => {
                  const p = historyPage + 1;
                  setHistoryPage(p);
                  fetchHistory(selectedDate, p, onlyProblems).catch(() => {});
                }}
              >
                下一页
              </Button>
            </div>
          ) : (
            <span className="text-[11px] text-[var(--fg-3)]">
              左侧红/琥珀竖条自动串联同次 11128 拦截与重放
            </span>
          )}
        </div>

        {viewTab === 'realtime' ? (
          renderEventTable(
            realtimeRows,
            'system 提示词实时事件',
            'spRealtimeRows',
            '暂无实时事件（只有带 system 消息的请求才会记录）',
          )
        ) : (
          <>
            {renderEventTable(
              historyRows,
              'system 提示词历史事件',
              'spHistoryRows',
              '这一天没有匹配的落盘事件',
            )}
            <div className="px-4 py-2.5 border-t border-[var(--border)] bg-[var(--surface-2)]/40 text-xs text-[var(--fg-3)] flex items-center justify-between">
              <span id="spPagerMeta" className="ds-mono">
                {totalHist} 条 · {fromIdx}–{toIdx} · 第 {historyPage} 页 ·{' '}
                {historyResp?.date || selectedDate || '—'}
              </span>
            </div>
          </>
        )}
      </Card>

      {/* 右侧抽屉：四档模式详细原理、改写前后对比与隐私自检 */}
      <SlideDrawer
        open={guideOpen}
        onClose={() => setGuideOpen(false)}
        title="System Prompt 四档模式原理与示例"
        subtitle="完整说明网关转发上游时的 system 处理差异与隐私保护机制。"
        widthClass="max-w-xl"
      >
        <div id="spModeHint" className="space-y-4 text-xs leading-relaxed text-[var(--fg-2)]">
          <p className="m-0">
            客户端（Claude Code / OpenCode 等）发来的请求里包含一条{' '}
            <code className="ds-mono text-[var(--fg)]">system</code> 消息。此开关只决定
            <strong className="text-[var(--fg)]">网关转发给上游时这一条消息怎么带过去</strong>
            ——工具定义、空消息、图片等照常规整。
          </p>

          <pre className="ds-surface-2 ds-mono text-[11px] p-3.5 overflow-x-auto text-[var(--fg)] m-0 leading-relaxed">{`直传（keep / auto 首次）
  [ { role: system, content: "You are Claude Code..." },
    { role: user,   content: "帮我写个函数" } ]

改写（fold / auto 被拒后重试）
  [ { role: system, content: "You are a helpful coding assistant." },
    { role: user,   content: "<client_instructions>\\nYou are Claude Code...\\n</client_instructions>\\n\\n帮我写个函数" } ]`}</pre>

          <div className="ds-surface-2 p-3.5 space-y-2">
            <div className="font-medium text-[var(--fg)]">strip 模式精确剥离哪两句？</div>
            <p className="m-0">
              上游只拦截两条精确整句：
              <code className="ds-mono text-[var(--fg)]">
                You are Claude Code, Anthropic&apos;s official CLI for Claude.
              </code>
              （两区均拦）与{' '}
              <code className="ds-mono text-[var(--fg)]">
                You are an AI agent powered by DeepSeek Harness.
              </code>
              （仅国内区拦）。剥离其余内容（AGENTS.md、工具说明、项目规则）一字不改。
            </p>
          </div>

          <div className="ds-surface-2 p-3.5 space-y-2">
            <div className="font-medium text-[var(--fg)]">关于 auto 降级的两条关联事件</div>
            <p className="m-0">
              一次 11128 自动降级会产生两条同 <code className="ds-mono">requestId</code> 的事件：
              <code className="ds-mono">keep(被拦截)</code> +{' '}
              <code className="ds-mono">fold(降级重放)</code>。它们属于同一次用户请求，表格用左侧红/琥珀竖条将两者串联显示。
            </p>
          </div>

          <div className="ds-surface-2 p-3.5 space-y-1.5">
            <div className="font-medium text-[var(--fg)]">隐私自检命令</div>
            <p className="m-0">
              <code className="ds-mono text-[var(--fg)] break-all">
                grep -c &quot;You are Claude Code&quot; proxy-systemprompt-*.jsonl
              </code>{' '}
              应恒为 <code className="ds-mono">0</code>。指纹仅用于判断同一 system 出现频次，无法反推原文。
            </p>
          </div>
        </div>
      </SlideDrawer>
    </div>
  );
}
