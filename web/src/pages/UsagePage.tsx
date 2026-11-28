import { useCallback, useEffect, useState } from 'react';
import { Copy, RefreshCw, TriangleAlert } from 'lucide-react';
import type { UsageEntry, UsageViewPayload } from '@/types/api';
import { usageRowHitRate, usageRowTotalTokens } from '@/lib/domain';
import {
  formatCompactNumber,
  formatHitRate,
  formatTime,
  shortId,
} from '@/lib/format';
import { apiRequest, copyToClipboard } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, PageHeader, StatCard } from '@/components/ui/Card';
import { ScopeNote } from '@/components/ui/ScopeNote';
import { SegmentedControl } from '@/components/ui/SegmentedControl';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { UsageLineChart, type UsageChartRange } from '@/components/charts/UsageLineChart';

interface UsagePageProps {
  active: boolean;
}

type RangeKey = UsageChartRange;

// 上游模型对照（I8）：normalizeModelName / isAutoFamily 与后端
// internal/gateway/usage_record.go 的判据保持一致 —— 后端已给出
// modelMismatch，这里只为旧条目（字段缺省）留一条本地回退路径。
const stripSitePrefix = (model: string) => model.trim().replace(/^(cn:|global:)/i, '');

const normalizeModelName = (model: string) =>
  stripSitePrefix(model).toLowerCase().replace(/[-_. /:]/g, '');

const isAutoFamily = (model: string) => {
  const m = stripSitePrefix(model).toLowerCase();
  return m === 'auto' || m.startsWith('auto-');
};

// 分词器比例（I8/I9/I10）：后端只会给出 cjk / digits / ascii / tiny / none，
// digit / alpha / sym 是历史与别名口径，一并映射，别让新桶名掉成英文。
const TOK_BUCKET_LABEL: Record<string, string> = {
  cjk: '中文',
  digit: '数字',
  digits: '数字',
  alpha: '英文',
  ascii: '英文',
  sym: '符号',
  symbol: '符号',
  tiny: '不参与',
  none: '不参与',
};

// 与 internal/invariant/tokbaseline.go 的 tokRatioMin/MaxPlausible 同值：
// 超出这个区间说明采样或分母出了问题，比例本身不可信。
const TOK_RATIO_MIN = 0.05;
const TOK_RATIO_MAX = 1.0;

export function UsagePage({ active }: UsagePageProps) {
  const { showToast } = useToast();
  const [range, setRange] = useState<RangeKey>('day');
  const [accountFilter, setAccountFilter] = useState('');
  const [modelFilter, setModelFilter] = useState('');
  const [page, setPage] = useState(1);
  const [data, setData] = useState<UsageViewPayload | null>(null);
  const [loading, setLoading] = useState(false);

  const pageSize = 20;

  const fetchUsage = useCallback(
    async (r: RangeKey, acc: string, mod: string, p: number) => {
      setLoading(true);
      try {
        const offset = (p - 1) * pageSize;
        let url = `/direct-admin/api/usage?range=${encodeURIComponent(
          r,
        )}&limit=${pageSize}&offset=${offset}`;
        if (acc) url += `&account=${encodeURIComponent(acc)}`;
        if (mod) url += `&model=${encodeURIComponent(mod)}`;
        const res = await apiRequest<UsageViewPayload>(url);
        setData(res);
      } catch (e: unknown) {
        showToast(e instanceof Error ? e.message : String(e), 'error');
      } finally {
        setLoading(false);
      }
    },
    [showToast],
  );

  useEffect(() => {
    if (!active) return;
    fetchUsage(range, accountFilter, modelFilter, page);
  }, [active, range, accountFilter, modelFilter, page, fetchUsage]);

  const sum = data?.summary;
  const total = data?.requestsTotal ?? data?.requests?.length ?? 0;
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const rows = data?.requests || [];

  const renderSystemCell = (row: UsageEntry) => {
    const form = row.systemForm || '';
    if (!form) return <span className="text-[var(--fg-3)]">—</span>;
    const label = row.systemDegraded ? `${form}(降级)` : form;
    const tone = row.systemDegraded ? 'warn' : form === 'keep' ? 'ok' : 'neutral';
    return (
      <StatusBadge
        tone={tone}
        title={row.systemFp ? `指纹 ${row.systemFp}` : undefined}
        className="!text-[10px]"
      >
        {label}
      </StatusBadge>
    );
  };

  // I8：上游模型回显。三种状态与旧面板一致 ——
  // 未观测「—」/ 一致（原样）/ 被替换（告警色 + 标记）/ auto 一族（上游解析，正常）。
  const renderUpstreamModelCell = (row: UsageEntry) => {
    const upstream = row.upstreamModel || '';
    if (!upstream) return <span className="text-[var(--fg-3)]">—</span>;
    const sent = row.routedModel || row.model || '';
    // 后端已算好 modelMismatch；旧条目没有该字段时按同一判据本地回退。
    const mismatch =
      row.modelMismatch ??
      (sent !== '' &&
        !isAutoFamily(sent) &&
        normalizeModelName(sent) !== normalizeModelName(upstream));
    if (mismatch) {
      return (
        <StatusBadge
          tone="warn"
          title={`发出去 ${sent}，上游回显 ${upstream} —— 模型被替换`}
          className="!text-[10px]"
        >
          <TriangleAlert className="w-2.5 h-2.5 shrink-0" />
          <span className="ds-mono">{upstream}</span>
        </StatusBadge>
      );
    }
    if (isAutoFamily(sent)) {
      return (
        <StatusBadge
          tone="neutral"
          title={`请求的是 ${sent}，由上游解析成 ${upstream}（正常）`}
          className="!text-[10px]"
        >
          <span className="ds-mono">{upstream}</span>
          <span className="opacity-70">/ 解析</span>
        </StatusBadge>
      );
    }
    return <span className="ds-mono text-[var(--fg)]">{upstream}</span>;
  };

  // 分词器比例（prompt_tokens ÷ 入站字符数）。未观测时不显示数字 ——
  // 显示 0.0000 会被误读成「比例为零」，那是假信号。
  const renderTokRatioCell = (row: UsageEntry) => {
    const bucket = String(row.tokBucket || '');
    const ratio = row.tokRatio ?? 0;
    const inactive = bucket === 'tiny' || bucket === 'none';
    const chars = row.tokChars || 0;
    const parts: string[] = [];
    if (row.tokCJK) parts.push(`中文 ${row.tokCJK}`);
    if (row.tokDigits) parts.push(`数字 ${row.tokDigits}`);
    if (row.tokASCII) parts.push(`ASCII ${row.tokASCII}`);
    const title = chars
      ? parts.length
        ? `字符 ${chars}（${parts.join(' / ')}）`
        : `字符 ${chars}`
      : undefined;

    // tiny/none 桶本来就不参与判定（短 prompt 模板开销占比过高 / 无文本），
    // 给出「不参与」说明原因，但不给比例 —— 它没有可比性。
    if (inactive) {
      return (
        <span className="inline-flex items-center gap-1.5 whitespace-nowrap" title={title}>
          <span className="text-[var(--fg-3)]">—</span>
          <StatusBadge tone="neutral" className="!text-[10px] !px-1.5 !py-0">
            {TOK_BUCKET_LABEL[bucket] ?? bucket}
          </StatusBadge>
        </span>
      );
    }
    if (!ratio) {
      return (
        <span className="text-[var(--fg-3)]" title={title}>
          —
        </span>
      );
    }
    const bucketLabel = TOK_BUCKET_LABEL[bucket] ?? bucket;
    // 旧面板注释里的「偏高/偏低配色」：与后端 tokRatioPlausible 同区间，
    // 超出即分母或采样可疑（实测 1.2MB 长上下文曾被算成 2.11）。
    const implausible = ratio < TOK_RATIO_MIN || ratio > TOK_RATIO_MAX;
    return (
      <span className="inline-flex items-center gap-1.5 whitespace-nowrap" title={title}>
        <span
          className={
            implausible
              ? 'ds-mono font-medium text-[var(--warn)]'
              : 'ds-mono text-[var(--fg)]'
          }
          title={
            implausible
              ? `比例超出物理可能区间 ${TOK_RATIO_MIN}~${TOK_RATIO_MAX}（分母或采样可疑），判定见 I9/I10/I11`
              : undefined
          }
        >
          {ratio.toFixed(4)}
        </span>
        {bucketLabel ? (
          <StatusBadge tone="neutral" className="!text-[10px] !px-1.5 !py-0">
            {bucketLabel}
          </StatusBadge>
        ) : null}
      </span>
    );
  };

  return (
    <div id="tab-usage" className="ds-page-enter space-y-5">
      <PageHeader
        title="Token 用量与缓存明细"
        subtitle="持久化保存最近约 400 条请求追踪与 90 日按日汇总（proxy-usage.json）；对比各模型 prompt 缓存命中效率与 Credits 消耗。"
        actions={
          <Button
            id="btnRefreshUsage"
            variant="ghost"
            size="sm"
            loading={loading}
            onClick={() => fetchUsage(range, accountFilter, modelFilter, page)}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
            刷新明细
          </Button>
        }
      />

      <ScopeNote>
        口径：请求数为落盘统计（保留 90 天，重启不清零）；命中率的分母口径为
        缓存 token ÷ prompt token，不是「缓存 ÷ 总 token」。
            </ScopeNote>

      {/* 周期与筛选工具条 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SegmentedControl<RangeKey>
          id="usageRangeSeg"
          ariaLabel="统计周期"
          value={range}
          onChange={(r) => {
            setRange(r);
            setPage(1);
          }}
          options={[
            { value: 'day', label: '今日', dataAttrs: { 'data-range': 'day' } },
            { value: 'week', label: '7 日', dataAttrs: { 'data-range': 'week' } },
            { value: 'month', label: '30 日', dataAttrs: { 'data-range': 'month' } },
            { value: 'all', label: '全部', dataAttrs: { 'data-range': 'all' } },
          ]}
        />

        <div className="flex items-center gap-2.5 flex-wrap text-xs">
          <select
            id="usageAccountFilter"
            aria-label="按账号筛选"
            value={accountFilter}
            onChange={(e) => {
              setAccountFilter(e.target.value);
              setPage(1);
            }}
            className="ds-select px-2.5 py-1.5 text-xs min-w-[140px]"
          >
            <option value="">全部账号</option>
            {(data?.accounts || []).map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>

          <select
            id="usageModelFilter"
            aria-label="按模型筛选"
            value={modelFilter}
            onChange={(e) => {
              setModelFilter(e.target.value);
              setPage(1);
            }}
            className="ds-select px-2.5 py-1.5 text-xs min-w-[160px]"
          >
            <option value="">全部模型</option>
            {(data?.models || []).map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* 5 格 KPI */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
        <StatCard label="请求数" valueId="uRequests" value={(sum?.requests || 0).toLocaleString()} />
        <StatCard
          label="失败"
          valueId="uFailed"
          value={(sum?.failed || 0).toLocaleString()}
          valueClassName={(sum?.failed || 0) > 0 ? 'text-[var(--err)]' : ''}
        />
        <StatCard
          label="Token 总量"
          valueId="uTotalTokens"
          value={formatCompactNumber(sum?.totalTokens || 0)}
          title={`精确总 Tokens：${(sum?.totalTokens || 0).toLocaleString()}`}
        />
        <StatCard
          label="缓存命中率"
          valueId="uCacheHit"
          value={formatHitRate(sum?.cacheHitRate)}
          valueClassName={
            (sum?.cacheHitRate ?? -1) >= 40
              ? 'text-[var(--ok)]'
              : (sum?.cacheHitRate ?? -1) >= 10
                ? 'text-[var(--info)]'
                : ''
          }
        />
        <StatCard
          label="Credits 累计"
          valueId="uCredits"
          value={
            (sum?.creditRows || 0) > 0
              ? (sum?.credits || 0).toFixed(4)
              : '—'
          }
          sub={(sum?.creditRows || 0) > 0 ? `${sum?.creditRows} 条` : undefined}
        />
      </div>

      {/* 交互式趋势图 + 按模型缓存对照 */}
      <div className="grid grid-cols-1 xl:grid-cols-12 gap-4">
        <Card id="usageChartBox" className="xl:col-span-7 p-5">
          <div className="text-sm font-medium text-[var(--fg)] mb-3">消耗与缓存命中趋势</div>
          <div id="usageChart">
            <UsageLineChart
              series={data?.series || []}
              totalRequests={sum?.requests || 0}
              range={range}
              height={220}
              showModeToggle={false}
            />
          </div>
        </Card>

        <Card id="usageByModel" className="xl:col-span-5 overflow-hidden flex flex-col">
          <div className="px-4 py-3 border-b border-[var(--border)] bg-[var(--surface-2)]/50 flex items-center justify-between">
            <span className="text-sm font-medium text-[var(--fg)]">按模型缓存命中对比</span>
            <span className="text-[11px] text-[var(--fg-3)]">cached ÷ prompt</span>
          </div>
          <div className="overflow-x-auto flex-1">
            {!(data?.byModel || []).length ? (
              <div className="py-12 text-center text-xs text-[var(--fg-3)]">暂无按模型统计</div>
            ) : (
              <table className="w-full text-left border-collapse text-xs" aria-label="按模型缓存命中">
                <thead>
                  <tr className="border-b border-[var(--border)] text-[11px] uppercase tracking-wider text-[var(--fg-3)]">
                    <th className="py-2 px-3.5 font-medium">模型</th>
                    <th className="py-2 px-2.5 font-medium">请求</th>
                    <th className="py-2 px-2.5 font-medium">Token</th>
                    <th className="py-2 px-3.5 font-medium">命中率</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--border)]">
                  {(data?.byModel || []).map((row) => {
                    const rate = row.cacheHitRate ?? -1;
                    const pct = rate > 0 ? Math.min(100, rate) : 0;
                    return (
                      <tr key={row.model}>
                        <td className="py-2 px-3.5 ds-mono text-[var(--fg)] truncate max-w-[160px]" title={row.model}>
                          {row.model}
                        </td>
                        <td className="py-2 px-2.5 ds-mono text-[var(--fg-2)]">{row.requests}</td>
                        <td className="py-2 px-2.5 ds-mono text-[var(--fg)]">
                          {formatCompactNumber(row.totalTokens)}
                        </td>
                        <td className="py-2 px-3.5 whitespace-nowrap">
                          <div className="flex items-center gap-2">
                            <div className="w-14 h-1.5 rounded-full bg-[var(--surface-2)] overflow-hidden">
                              <div
                                className="h-full rounded-full"
                                style={{
                                  width: `${pct}%`,
                                  background:
                                    rate >= 40
                                      ? 'var(--ok)'
                                      : rate >= 10
                                        ? 'var(--info)'
                                        : 'var(--warn)',
                                }}
                              />
                            </div>
                            <span
                              className={`ds-mono text-[11px] ${
                                rate < 5 ? 'text-[var(--fg-3)]' : 'text-[var(--fg)] font-medium'
                              }`}
                            >
                              {formatHitRate(rate)}
                            </span>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>
        </Card>
      </div>

      {/* 14 列请求追踪明细表 */}
      <Card className="overflow-hidden">
        <div className="px-4 py-3 border-b border-[var(--border)] bg-[var(--surface-2)]/50 flex flex-wrap items-center justify-between gap-2">
          <span className="text-sm font-medium text-[var(--fg)]">请求追踪明细</span>
          <span id="usageCreditHint" className="text-[11px] text-[var(--fg-3)]">
            点击任意请求 ID 即可复制；Credits 仅在上游 usage 返回 credit 时累加。
          </span>
        </div>

        <div className="overflow-x-auto max-h-[420px]">
          <table className="w-full text-left border-collapse text-xs min-w-[1240px]" aria-label="请求明细">
            <thead className="sticky top-0 z-10 bg-[var(--surface-2)]">
              <tr className="border-b border-[var(--border)] text-[11px] uppercase tracking-wider text-[var(--fg-3)]">
                <th className="py-2.5 px-3 font-medium">时间</th>
                <th className="py-2.5 px-3 font-medium">请求 ID</th>
                <th className="py-2.5 px-3 font-medium">上游 Request</th>
                <th className="py-2.5 px-3 font-medium">会话</th>
                <th className="py-2.5 px-3 font-medium">模型</th>
                <th className="py-2.5 px-3 font-medium">System</th>
                <th className="py-2.5 px-3 font-medium">Token 总量</th>
                <th className="py-2.5 px-3 font-medium">缓存命中</th>
                <th
                  className="py-2.5 px-3 font-medium"
                  title="上游 SSE 帧里回显的 model 字段。与「模型」列不一致即说明上游换了模型；auto 一族由上游解析，属正常"
                >
                  上游模型
                </th>
                <th
                  className="py-2.5 px-3 font-medium"
                  title="prompt_tokens ÷ 入站字符数。同一模型对同类文本应当稳定；突变说明实际跑的模型被换了。按内容成分分桶：中文 / 数字 / ASCII（不同类别的 token 密度差 2~3 倍，不可混算）"
                >
                  分词器比例
                </th>
                <th
                  className="py-2.5 px-3 font-medium"
                  title="本次请求上游返回的单次扣费（不是余额）。余额看 01 概览或 02 账号池"
                >
                  Credit
                </th>
                <th className="py-2.5 px-3 font-medium">耗时</th>
                <th className="py-2.5 px-3 font-medium">账号</th>
                <th className="py-2.5 px-3 font-medium">状态</th>
              </tr>
            </thead>
            <tbody id="usageRows" className="divide-y divide-[var(--border)]">
              {!rows.length ? (
                <tr>
                  <td colSpan={14} className="py-10 text-center text-[var(--fg-3)]">
                    暂无请求明细记录
                  </td>
                </tr>
              ) : (
                rows.map((row, idx) => {
                  const totalTok = usageRowTotalTokens(row);
                  const hit = formatHitRate(usageRowHitRate(row));
                  const account = row.accountLabel || shortId(row.accountId);
                  return (
                    <tr
                      key={`${row.proxyRequestId || 'r'}-${row.at}-${idx}`}
                      className="hover:bg-[var(--surface-2)]/50"
                    >
                      <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)] whitespace-nowrap">
                        {formatTime(row.at)}
                      </td>
                      <td className="py-2.5 px-3 ds-mono whitespace-nowrap">
                        {row.proxyRequestId ? (
                          <button
                            type="button"
                            onClick={() =>
                              copyToClipboard(row.proxyRequestId!)
                                .then(() => showToast('请求 ID 已复制'))
                                .catch(() => {})
                            }
                            className="inline-flex items-center gap-1 text-[var(--fg)] hover:underline underline-offset-2 cursor-pointer"
                          >
                            <span>{shortId(row.proxyRequestId)}</span>
                            <Copy className="w-2.5 h-2.5 opacity-40" />
                          </button>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="py-2.5 px-3 ds-mono whitespace-nowrap">
                        {row.upstreamConversationRequestId ? (
                          <button
                            type="button"
                            onClick={() =>
                              copyToClipboard(row.upstreamConversationRequestId!)
                                .then(() => showToast('上游 Request ID 已复制'))
                                .catch(() => {})
                            }
                            className="inline-flex items-center gap-1 text-[var(--fg-2)] hover:text-[var(--fg)] hover:underline cursor-pointer"
                          >
                            <span>{shortId(row.upstreamConversationRequestId)}</span>
                          </button>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="py-2.5 px-3 text-[var(--fg-2)]">
                        {row.sessionLabel || shortId(row.sessionKey)}
                      </td>
                      <td className="py-2.5 px-3 ds-mono text-[var(--fg)]">{row.model || '—'}</td>
                      <td className="py-2.5 px-3">{renderSystemCell(row)}</td>
                      <td
                        className="py-2.5 px-3 ds-mono text-[var(--fg)]"
                        title={`Prompt ${row.promptTokens || 0} + Completion ${row.completionTokens || 0}`}
                      >
                        {formatCompactNumber(totalTok)}
                      </td>
                      <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">{hit}</td>
                      <td className="py-2.5 px-3">{renderUpstreamModelCell(row)}</td>
                      <td className="py-2.5 px-3">{renderTokRatioCell(row)}</td>
                      <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                        {row.credit != null ? String(row.credit) : '—'}
                      </td>
                      <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)] whitespace-nowrap">
                        {row.durationMs != null ? `${row.durationMs}ms` : '—'}
                      </td>
                      <td className="py-2.5 px-3 text-[var(--fg)]">{account || '—'}</td>
                      <td className="py-2.5 px-3 whitespace-nowrap">
                        {row.ok ? (
                          <StatusBadge tone="ok">OK</StatusBadge>
                        ) : (
                          <StatusBadge tone="err" title={row.error || ''}>
                            FAIL
                          </StatusBadge>
                        )}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        <div className="px-4 py-2.5 border-t border-[var(--border)] bg-[var(--surface-2)]/40 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--fg-2)]">
          <span id="usagePagerMeta" className="ds-mono">
            {total
              ? `第 ${page} / ${pages} 页 · 本页 ${rows.length} 条 · 共 ${total} 条`
              : '暂无明细'}
          </span>
          <div className="flex items-center gap-1.5">
            <Button
              id="btnUsagePrev"
              variant="ghost"
              size="xs"
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              上一页
            </Button>
            <Button
              id="btnUsageNext"
              variant="ghost"
              size="xs"
              disabled={page >= pages || total === 0}
              onClick={() => setPage((p) => p + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}
