import { useCallback, useEffect, useState } from 'react';
import {
  BookOpen,
  CheckCircle2,
  Copy,
  FlaskConical,
  RefreshCw,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react';
import type {
  SystemDryRunRequest,
  SystemDryRunResponse,
  SystemPromptEvent,
  SystemPromptHistoryResponse,
  SystemPromptMode,
  SystemPromptOverview,
} from '@/types/api';
import { resolveLocalDateList, spFormInfo, spResultInfo } from '@/lib/domain';
import { formatBytes, formatShortClock, formatTime, shortId, siteLabel } from '@/lib/format';
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
  // 线上剥离开关正在提交哪一个（旧面板的 spStripEnabled /
  // spStripAssistant / spEchoUser）。提交中禁用全部开关，避免连点
  // 造成「界面显示 A、服务端实际 B」的分叉。
  const [switchingStrip, setSwitchingStrip] = useState<
    'strip' | 'stripAssistant' | 'echoUser' | null
  >(null);

  const [viewTab, setViewTab] = useState<'realtime' | 'history'>('realtime');
  const [onlyProblems, setOnlyProblems] = useState(false);
  const [guideOpen, setGuideOpen] = useState(false);

  // 历史分页状态
  const [selectedDate, setSelectedDate] = useState('');
  const [historyPage, setHistoryPage] = useState(1);
  const [historyResp, setHistoryResp] = useState<SystemPromptHistoryResponse | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  // system 预览（不发请求）——纯本地试算，不改配置、不碰请求路径。
  // 档位留空 = 跟随当前生效配置；strip / echo 用 null 表示「未指定」，
  // 只有用户真的动过勾选框才会带上布尔值（带 false 也会覆盖线上配置）。
  const [drySystem, setDrySystem] = useState('');
  const [dryMode, setDryMode] = useState<'' | 'auto' | 'keep' | 'fold' | 'strip'>('');
  const [dryStrip, setDryStrip] = useState<boolean | null>(null);
  const [dryEcho, setDryEcho] = useState<boolean | null>(null);
  const [dryRunning, setDryRunning] = useState(false);
  const [dryResult, setDryResult] = useState<SystemDryRunResponse | null>(null);
  const [dryError, setDryError] = useState('');
  // 上一次真正跑过的输入签名：输入改了但没重跑时，结果要标成过期，
  // 否则操作者会把旧结果当成新文本的预览（本分支最忌讳的静默失效）。
  const [dryRanSignature, setDryRanSignature] = useState('');

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

  // 线上配置的剥离开关。只发被点的那一个字段 —— 后端按
  // 「只改开关、不动模式」处理，不会顺带覆写档位。
  const stripState = overview?.strip;
  const stripAssistantState = overview?.stripAssistant;
  const echoUserState = overview?.echoUser;

  const handleSwitchStrip = async (
    which: 'strip' | 'stripAssistant' | 'echoUser',
    value: boolean,
  ) => {
    setSwitchingStrip(which);
    try {
      const data = await apiRequest<{
        mode?: SystemPromptMode;
        strip?: boolean;
        stripAssistant?: boolean;
        echoUser?: boolean;
      }>('/direct-admin/api/system-prompt', {
        method: 'POST',
        body: JSON.stringify({ [which]: value }),
      });
      // 以服务端回显为准，别让界面停在本地推测值上。
      setOverview((prev) =>
        prev
          ? {
              ...prev,
              strip: data.strip ?? prev.strip,
              stripAssistant: data.stripAssistant ?? prev.stripAssistant,
              echoUser: data.echoUser ?? prev.echoUser,
            }
          : prev,
      );
      if (which === 'echoUser') {
        showToast(
          value
            ? 'system 复制进 user 已开启（每次请求多消耗约 system 长度的 token）'
            : 'system 复制进 user 已关闭',
          'success',
        );
      } else if (which === 'strip') {
        showToast(`首试前剥离已${value ? '开启' : '关闭'}`, 'success');
      } else {
        showToast(`assistant 剥离已${value ? '开启' : '关闭'}`, 'success');
      }
      await fetchOverviewAndHistory();
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : String(e), 'error');
      // 失败时回读服务端真值，把复选框拉回实际状态。
      await fetchOverviewAndHistory();
    } finally {
      setSwitchingStrip(null);
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

  // 跑一次本地试算。请求体里只带用户显式指定的字段：
  const handleDryRun = async () => {
    const system = drySystem;
    if (!system.trim()) {
      setDryError('先在上面贴一段 system。');
      setDryResult(null);
      return;
    }
    setDryRunning(true);
    setDryError('');
    // 与旧面板一致：开跑即清掉上一次结果并显示「计算中…」，
    // 不让旧 outbound 在等待期间冒充新结果。
    setDryResult(null);
    setDryRanSignature('');
    try {
      // mode 始终显式带上：`跟随当前` 就是空串（按契约由服务端解释）。
      // strip / echo 则只有动过勾选框才出现 —— 传 false 同样会覆盖线上配置。
      const body: SystemDryRunRequest = { system, mode: dryMode };
      if (dryStrip !== null) body.strip = dryStrip;
      if (dryEcho !== null) body.echo = dryEcho;
      const data = await apiRequest<SystemDryRunResponse>('/direct-admin/api/system-dryrun', {
        method: 'POST',
        body: JSON.stringify(body),
      });
      setDryResult(data);
      setDryRanSignature(JSON.stringify({ system, mode: dryMode, strip: dryStrip, echo: dryEcho }));
    } catch (e: unknown) {
      setDryResult(null);
      setDryRanSignature('');
      setDryError(e instanceof Error ? e.message : String(e));
    } finally {
      setDryRunning(false);
    }
  };

  const dateList = resolveLocalDateList(overview?.dates || [], selectedDate);
  const stats = overview?.stats || {};
  const realtimeRows = (overview?.recent || []).filter((e) => {
    if (!onlyProblems) return true;
    return (e.status ?? 0) === 0 || (e.status ?? 0) >= 400 || !!e.errorCode || !!e.degraded;
  });
  const historyRows = historyResp?.events || [];

  // 预览结果里的派生值：条数 / system 长度变化要在渲染时直接可读。
  const dryInLen = dryResult?.inSystemLen ?? 0;
  const dryOutLen = dryResult?.outSystemLen ?? 0;
  const dryLenDelta = dryOutLen - dryInLen;
  const dryWarnings = dryResult?.warnings ?? [];
  const dryNotes = dryResult?.notes ?? [];
  const dryModeLabelText = dryResult?.modeLabel || dryResult?.mode || '—';

  // formatBytes(0) 会回 '—'，但「出站 system 被剥成 0」正是需要看见的事实，
  // 所以零值单独写 0 B，不拿破折号糊过去。
  const fmtLen = (n: number) => (n > 0 ? formatBytes(n) : '0 B');

  // 勾选框的显示值：未指定时跟随后端当前配置（overview 里的 strip / echoUser）。
  const dryStripChecked = dryStrip ?? !!(overview?.strip ?? false);
  const dryEchoChecked = dryEcho ?? !!(overview?.echoUser ?? false);

  // 输入改过但没重跑：结果仍在屏幕上，必须显式标过期。
  const dryStale =
    !!dryResult &&
    dryRanSignature !==
      JSON.stringify({ system: drySystem, mode: dryMode, strip: dryStrip, echo: dryEcho });

  const renderDryToggle = (opts: {
    id: string;
    label: string;
    hint: string;
    checked: boolean;
    live: boolean | undefined;
    overridden: boolean;
    onToggle: (next: boolean) => void;
    onFollow: () => void;
  }) => (
    <div className="inline-flex items-center gap-2 text-xs text-[var(--fg-2)]" title={opts.hint}>
      <label className="inline-flex items-center gap-1.5 cursor-pointer select-none">
        <input
          id={opts.id}
          type="checkbox"
          checked={opts.checked}
          // overview 未加载时不知道线上值，先禁用；此时请求也不会发送该字段。
          disabled={!overview}
          onChange={(e) => opts.onToggle(e.target.checked)}
          className="accent-[var(--accent)]"
        />
        {opts.label}
      </label>
      <span className="text-[10px] text-[var(--fg-3)]">
        {opts.overridden
          ? `本次显式${opts.checked ? '开' : '关'}`
          : `跟随当前：${opts.live === undefined ? '—' : opts.live ? '开' : '关'}`}
      </span>
      {opts.overridden && (
        <button
          type="button"
          onClick={opts.onFollow}
          className="text-[10px] text-[var(--fg-3)] hover:text-[var(--fg)] underline underline-offset-2 cursor-pointer"
          title="取消本次显式指定，回到「不发送该字段、由服务端跟随当前配置」"
        >
          恢复跟随
        </button>
      )}
    </div>
  );

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

      {/* 线上生效的剥离 / echo 开关（旧面板 spStripEnabled /
          spStripAssistant / spEchoUser）。点一下立刻热更新，不重启。 */}
      <Card className="p-3.5 space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-[11px] text-[var(--fg-3)]">
            指纹剥离开关 · 点一下立刻生效（热更新，不重启）
          </span>
          <span id="spStripState" className="text-[11px] text-[var(--fg-3)]">
            {!overview
              ? '—'
              : `${stripState === false ? '剥离已关闭' : '剥离已开启'} · assistant ${
                  stripAssistantState === false ? '不剥' : '同剥'
                } · echo ${echoUserState ? '开' : '关'}`}
          </span>
        </div>
        <div className="flex flex-col sm:flex-row flex-wrap gap-x-5 gap-y-2">
          <label
            className="inline-flex items-center gap-2 text-xs text-[var(--fg-2)] cursor-pointer"
            title="总开关。关掉后 auto/strip 退回 v0.7.6 行为：先直传原 system，被 11128 拒了才折叠重试 —— 每个请求都会先吃一次 400"
          >
            <input
              id="spStripEnabled"
              type="checkbox"
              className="accent-[var(--accent)]"
              checked={stripState !== false}
              disabled={!overview || switchingStrip !== null}
              onChange={(e) => handleSwitchStrip('strip', e.target.checked)}
            />
            首试前剥离已知指纹
          </label>
          <label
            className="inline-flex items-center gap-2 text-xs text-[var(--fg-2)] cursor-pointer"
            title="只控制 assistant 正文这一路。关掉后 system 仍照剥，但历史里带指纹的会话会永久 502（折叠救不了）"
          >
            <input
              id="spStripAssistant"
              type="checkbox"
              className="accent-[var(--accent)]"
              checked={stripAssistantState !== false}
              disabled={!overview || stripState === false || switchingStrip !== null}
              onChange={(e) => handleSwitchStrip('stripAssistant', e.target.checked)}
            />
            连 assistant 正文一起剥
          </label>
          <label
            className="inline-flex items-center gap-2 text-xs text-[var(--fg-2)] cursor-pointer"
            title="实验开关。system 仍留在 system 位不动，同时把同一份内容复制一份到第一条 user 开头（client_instructions 包裹）。用于对照验证模型是靠 system 位还是 user 副本更能遵循指令。开启后每次请求多消耗约等于 system 长度的 prompt tokens"
          >
            <input
              id="spEchoUser"
              type="checkbox"
              className="accent-[var(--accent)]"
              checked={echoUserState === true}
              disabled={!overview || switchingStrip !== null}
              onChange={(e) => handleSwitchStrip('echoUser', e.target.checked)}
            />
            额外把 system 复制进 user（实验）
          </label>
        </div>
      </Card>

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

        <div className="grid grid-cols-2 sm:grid-cols-4 xl:grid-cols-8 gap-2.5">
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
            label="已剥离"
            valueId="spStripped"
            title="首试前剥离（或剥离后直传）发生的事件数；关掉剥离开关后该值会停止增长"
            value={stats.stripped ?? 0}
            className="p-3"
          />
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

      {/* system 预览：纯本地试算，零网络、零 credit */}
      <Card id="spDryRun" className="p-5 space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-[0.16em] text-[var(--fg-3)]">
              System Dry Run · Zero Cost
            </div>
            <h2 className="ds-heading text-lg text-[var(--fg)] mt-1 mb-0">system 预览（不发请求）</h2>
            <p className="text-xs text-[var(--fg-2)] mt-1.5 mb-0 max-w-3xl leading-relaxed">
              贴一段 system，立刻看到<strong className="text-[var(--fg)]">网关会把它变成什么</strong>再发给上游。纯本地计算，
              <strong className="text-[var(--fg)]">零网络、零 credit</strong>；用的是当前生效的档位与开关，也可以临时指定档位试算（不改配置）。
            </p>
          </div>
        </div>

        <div>
          <label htmlFor="drySystem" className="block text-[11px] text-[var(--fg-3)] mb-1">
            客户端会发的 system
          </label>
          <textarea
            id="drySystem"
            rows={6}
            spellCheck={false}
            value={drySystem}
            onChange={(e) => setDrySystem(e.target.value)}
            placeholder="You are Claude Code, Anthropic's official CLI for Claude."
            className="ds-input ds-mono w-full p-3 text-[11px] leading-relaxed resize-y"
          />
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[11px] text-[var(--fg-3)]">试算档位</span>
            <span
              className="inline-flex"
              title="留空 = 用线上当前生效档位；结果区会显示实际生效的档位。档位只是试算参数，不会写回配置。"
            >
              <SegmentedControl<'' | 'auto' | 'keep' | 'fold' | 'strip'>
                id="dryModeSeg"
                ariaLabel="试算档位"
                size="sm"
                value={dryMode}
                onChange={setDryMode}
                options={[
                  { value: '', label: '跟随当前', dataAttrs: { 'data-drymode': '' } },
                  { value: 'auto', label: 'auto', dataAttrs: { 'data-drymode': 'auto' } },
                  { value: 'keep', label: 'keep', dataAttrs: { 'data-drymode': 'keep' } },
                  { value: 'fold', label: 'fold', dataAttrs: { 'data-drymode': 'fold' } },
                  { value: 'strip', label: 'strip', dataAttrs: { 'data-drymode': 'strip' } },
                ]}
              />
            </span>
          </div>
          <Button
            id="btnDryRun"
            variant="primary"
            size="sm"
            loading={dryRunning}
            disabled={!drySystem.trim()}
            onClick={handleDryRun}
          >
            <FlaskConical className="w-3.5 h-3.5" />
            预览
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          {renderDryToggle({
            id: 'dryStrip',
            label: '强制剥离指纹',
            hint: '对应 strip 开关：开 = 首试前剥掉已知身份句 / 段落指纹。不动它就跟随后端当前配置，不发送该字段。',
            checked: dryStripChecked,
            live: overview?.strip,
            overridden: dryStrip !== null,
            onToggle: setDryStrip,
            onFollow: () => setDryStrip(null),
          })}
          {renderDryToggle({
            id: 'dryEcho',
            label: '额外把 system 复制进 user',
            hint: '对应 echo 开关：system 仍留在 system 位，另一份内容以 client_instructions 包裹复制到首条 user 开头，会多消耗约 system 长度的 prompt tokens。',
            checked: dryEchoChecked,
            live: overview?.echoUser,
            overridden: dryEcho !== null,
            onToggle: setDryEcho,
            onFollow: () => setDryEcho(null),
          })}
        </div>

        {!drySystem.trim() && (
          <p className="text-xs text-[var(--fg-3)] m-0">先在上面贴一段 system。</p>
        )}

        {dryError && (
          <div
            role="alert"
            className="ds-surface-2 px-3.5 py-3 text-xs leading-relaxed text-[var(--err)] bg-[var(--err-bg)] border-l-2 border-[var(--err)] whitespace-pre-wrap break-words"
          >
            {dryError}
          </div>
        )}

        {/* 容器常驻（沿用旧面板的 #dryResult），没有结果时为空，便于外部探针判断有无数据 */}
        <div id="dryResult" className="space-y-3">
          {dryRunning && (
            <div className="ds-surface-2 px-3.5 py-3 text-xs text-[var(--fg-3)]">计算中…</div>
          )}
          {dryResult && (
            <div className="space-y-3 border-t border-[var(--border)] pt-4">
              {dryStale && (
                <div className="ds-surface-2 px-3.5 py-2.5 text-xs leading-relaxed text-[var(--warn)] bg-[var(--warn-bg)] border-l-2 border-[var(--warn)]">
                  下面的结果是上一次输入的预览，system 文本或档位 / 开关已经改过 —— 点「预览」重算后再信。
                </div>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <StatusBadge tone="info" title="本次试算实际使用的档位说明">
                  {dryModeLabelText}
                </StatusBadge>
                <StatusBadge
                  tone={dryResult.keepSystem ? 'ok' : 'warn'}
                  title="keepSystem=true：system 留在 system 位直传；false：fold 进首条 user"
                >
                  <span className="ds-mono">keepSystem</span>
                  {dryResult.keepSystem ? '开' : '关'}
                </StatusBadge>
                <StatusBadge
                  tone={dryResult.stripEnabled ? 'ok' : 'neutral'}
                  title="本次试算是否启用了已知指纹剥离"
                >
                  剥离{dryResult.stripEnabled ? '开' : '关'}
                </StatusBadge>
                <StatusBadge
                  tone={dryResult.echoEnabled ? 'warn' : 'neutral'}
                  title={
                    !dryResult.keepSystem && dryResult.echoEnabled
                      ? 'echo 开关是开的，但 fold 档不直传 system，复制进 user 这一步不会发生（后端 DryRun 只在 keepSystem 时执行 echo）'
                      : '本次试算是否把 system 复制进首条 user'
                  }
                >
                  echo{dryResult.echoEnabled ? '开' : '关'}
                  {!dryResult.keepSystem && dryResult.echoEnabled ? '（fold 下不生效）' : ''}
                </StatusBadge>
                {dryResult.site && (
                  <StatusBadge tone="neutral" title="剥离规则按站点区分：国内区多拦一条 DeepSeek Harness 身份句">
                    站点 · {siteLabel(dryResult.site)}
                  </StatusBadge>
                )}
              </div>

              <div className="ds-surface-2 flex flex-wrap items-stretch divide-x divide-[var(--border)] text-xs">
                <div className="px-3.5 py-2.5">
                  <span className="text-[var(--fg-3)]">条数</span>
                  <span id="dryMsgCount" className="ds-mono text-[var(--fg)] ml-2">
                    {dryResult.inMessages ?? 0} → {dryResult.outMessages ?? 0}
                  </span>
                </div>
                <div
                  className="px-3.5 py-2.5"
                  title="后端统计的 system 长度（Go 的 len(string)，即 UTF-8 字节数；中文 system 的字节数约为字符数的 3 倍）"
                >
                  <span className="text-[var(--fg-3)]">system 长度</span>
                  <span id="drySysLen" className="ds-mono text-[var(--fg)] ml-2">
                    {fmtLen(dryInLen)} → {fmtLen(dryOutLen)}
                  </span>
                  <span className="ds-mono text-[10px] text-[var(--fg-3)] ml-2">
                    （{dryInLen} → {dryOutLen} 字节）
                  </span>
                  <span
                    className={[
                      'ds-mono ml-2',
                      dryLenDelta === 0
                        ? 'text-[var(--fg-3)]'
                        : dryLenDelta < 0
                          ? 'text-[var(--warn)]'
                          : 'text-[var(--info)]',
                    ].join(' ')}
                  >
                    {dryLenDelta === 0
                      ? '（不变）'
                      : `（${dryLenDelta > 0 ? '+' : '-'}${formatBytes(Math.abs(dryLenDelta))}）`}
                  </span>
                </div>
                <div className="px-3.5 py-2.5">
                  <span className="text-[var(--fg-3)]">剥离</span>
                  <span id="dryStripSummary" className="ds-mono text-[var(--fg)] ml-2">
                    {dryResult.stripReport?.Stripped
                      ? `${dryResult.stripReport.Bytes} 字节`
                      : '无'}
                  </span>
                </div>
              </div>

              {/* 警告放在最上面：它们存在的意义就是提示操作者「有东西不对」 */}
              {dryWarnings.length > 0 && (
                <div
                  role="alert"
                  className="ds-surface-2 px-3.5 py-3 bg-[var(--err-bg)] border-l-2 border-[var(--err)] space-y-1.5"
                >
                  <div className="flex items-center gap-1.5 text-[11px] font-medium text-[var(--err)]">
                    <TriangleAlert className="w-3.5 h-3.5" />
                    警告（{dryWarnings.length}）
                  </div>
                  <ul className="m-0 pl-4 list-disc space-y-1 text-xs leading-relaxed text-[var(--err)]">
                    {dryWarnings.map((w, i) => (
                      <li key={`dry-warn-${i}`}>{w}</li>
                    ))}
                  </ul>
                </div>
              )}

              {dryNotes.length > 0 && (
                <div className="ds-surface-2 px-3.5 py-3 space-y-1.5">
                  <div className="text-[11px] font-medium text-[var(--fg-2)]">说明（{dryNotes.length}）</div>
                  <ul className="m-0 pl-4 list-disc space-y-1 text-xs leading-relaxed text-[var(--fg-2)]">
                    {dryNotes.map((n, i) => (
                      <li key={`dry-note-${i}`}>{n}</li>
                    ))}
                  </ul>
                </div>
              )}

              {dryResult.stripReport && (
                <div className="ds-surface-2 px-3.5 py-3">
                  <div className="text-[11px] font-medium text-[var(--fg-2)]">剥离明细</div>
                  <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs ds-mono text-[var(--fg-2)]">
                    <span>
                      Stripped
                      <b className="text-[var(--fg)] ml-1.5">
                        {dryResult.stripReport.Stripped ? 'true' : 'false'}
                      </b>
                    </span>
                    <span title="本次删掉的总字节数（Go 的 len(string)）">
                      Bytes
                      <b className="text-[var(--fg)] ml-1.5">{dryResult.stripReport.Bytes}</b>
                    </span>
                    <span title="其中来自 system 的字节数">
                      SystemBytes
                      <b className="text-[var(--fg)] ml-1.5">{dryResult.stripReport.SystemBytes}</b>
                    </span>
                    <span title="其中来自 assistant 历史的字节数">
                      AssistantBytes
                      <b className="text-[var(--fg)] ml-1.5">{dryResult.stripReport.AssistantBytes}</b>
                    </span>
                  </div>
                </div>
              )}

              <div>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <div className="text-sm font-medium text-[var(--fg)]">最终发给上游的 messages</div>
                    <p className="text-[11px] text-[var(--fg-3)] mt-0.5 mb-0">
                      这就是模型会看到的内容
                    </p>
                  </div>
                  <Button
                    id="btnDryCopy"
                    variant="ghost"
                    size="xs"
                    onClick={() => {
                      copyToClipboard(JSON.stringify(dryResult.outbound ?? [], null, 2))
                        .then(() => showToast('最终 outbound 已复制', 'success'))
                        .catch((e: unknown) =>
                          showToast(
                            `复制失败：${e instanceof Error ? e.message : String(e)}`,
                            'error',
                          ),
                        );
                    }}
                  >
                    <Copy className="w-3 h-3" />
                    复制 JSON
                  </Button>
                </div>
                <pre
                  id="dryOutbound"
                  className="ds-surface-2 ds-mono text-[11px] leading-relaxed p-3.5 mt-2 overflow-auto max-h-[420px] text-[var(--fg-2)] m-0 whitespace-pre"
                >
                  {JSON.stringify(dryResult.outbound ?? [], null, 2)}
                </pre>
              </div>
            </div>
          )}
        </div>
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
