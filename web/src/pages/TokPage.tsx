import { Fragment, useCallback, useEffect, useState } from 'react';
import { RefreshCw, ScanSearch, TriangleAlert } from 'lucide-react';
import type {
  TokBaseline,
  TokBaselinesPayload,
  TokBucket,
  TokIdentityPayload,
  TokIdentityResult,
  TokTogglePayload,
} from '@/types/tok';
import { apiRequest } from '@/lib/api';
import { useToast } from '@/hooks/useToast';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, PageHeader } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';

interface TokPageProps {
  active: boolean;
}

// 中文桶名。服务端（internal/tokfp）发的是 cjk / digits / ascii，契约里
// 另外写了 digit / alpha 两个别名，两套都要认；sym 目前只出现在参考比例里，
// 但基线行同样可能出现。tiny / none 是「太短」「无文本」，不参与判定。
const BUCKET_LABEL: Record<TokBucket, string> = {
  cjk: '中文',
  digit: '数字',
  digits: '数字',
  alpha: '英文',
  ascii: '英文',
  sym: '符号',
  tiny: '不参与',
  none: '不参与',
};

function bucketLabel(bucket: string): string {
  return BUCKET_LABEL[bucket as TokBucket] || bucket || '—';
}

function formatRatio(value?: number | null): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(4) : '—';
}

function siteText(site?: string): string {
  if (!site) return '—';
  // 与全站口径一致；未知值原样返回，不强行归一成「国际」。
  if (site === 'domestic' || site === 'cn') return '国内';
  if (site === 'global' || site === 'intl' || site === 'international') return '国际';
  return site;
}

function vocabularyBadge(vocabulary?: string) {
  if (vocabulary === '官方 DeepSeek 词表') {
    return (
      <StatusBadge tone="ok" title="官方 DeepSeek 词表">
        官方 DeepSeek
      </StatusBadge>
    );
  }
  if (vocabulary === '非 DeepSeek 词表') {
    return (
      <StatusBadge tone="err" title="非 DeepSeek 词表">
        非 DeepSeek
      </StatusBadge>
    );
  }
  // 无法判定必须原样露出 —— 这是「这次没测出来」，不是「词表正常」。
  return <StatusBadge tone="warn">{vocabulary || '无法判定'}</StatusBadge>;
}

function templateBadge(ok?: boolean) {
  if (ok === true) {
    return <StatusBadge tone="ok">符合</StatusBadge>;
  }
  if (ok === false) {
    return <StatusBadge tone="err">不符</StatusBadge>;
  }
  return <StatusBadge tone="neutral">未测</StatusBadge>;
}

// 模板公式只有「词表判定通过」时才真的测过：后端只在 VerdictPass 分支调
// checkTemplateFormula（internal/gateway/tokidentity.go），其余情况
// TemplateOK 都是零值 false。不能把它直接渲染成红色「不符」——
// 「无法判定」说明探测没拿到可用结论，模板校验根本没跑。
function templateState(row: TokIdentityResult): boolean | undefined {
  if (row.vocabulary !== '官方 DeepSeek 词表') return undefined;
  return row.templateOK;
}

function sampleEvidence(row: TokIdentityResult): string {
  return (row.samples || [])
    .map((s) => {
      const observed = s.observed == null ? '?' : String(s.observed);
      const delta = s.delta == null ? '' : ` (${s.delta >= 0 ? '+' : ''}${s.delta})`;
      return `${s.name} ${observed}/${s.official}${delta}`;
    })
    .join(' · ');
}

function templateDetailText(row: TokIdentityResult): string {
  const details = row.templateDetail || [];
  if (!details.length) return '';
  return details
    .map((d) => {
      const observed = d.observed == null ? '?' : String(d.observed);
      return `${d.name} ${observed}/${d.predicted} ${d.ok ? '符合' : '不符'}`;
    })
    .join(' · ');
}

export function TokPage({ active }: TokPageProps) {
  const { showToast } = useToast();

  // 分词器指纹基线
  const [enabled, setEnabled] = useState(false);
  // 开关状态是否真的取到过：接口挂了还照常渲染一个「未勾选」的框，
  // 用户点一下就是在盲切真实配置。
  const [enabledKnown, setEnabledKnown] = useState(false);
  const [baselines, setBaselines] = useState<TokBaseline[] | null>(null);
  const [baselineError, setBaselineError] = useState('');
  const [loadingBaselines, setLoadingBaselines] = useState(false);
  const [toggling, setToggling] = useState(false);

  // 词表身份鉴定
  const [identityResults, setIdentityResults] = useState<TokIdentityResult[] | null>(null);
  const [identityRunning, setIdentityRunning] = useState(false);
  const [identityError, setIdentityError] = useState('');

  const fetchBaselines = useCallback(async () => {
    setLoadingBaselines(true);
    try {
      const data = await apiRequest<TokBaselinesPayload>('/direct-admin/api/tok-baselines?limit=100');
      // HTTP 200 + ok:false 也必须当失败 —— 否则会渲染成「没有基线」。
      if (!data.ok) throw new Error('tok-baselines 返回 ok=false');
      setBaselines(data.baselines || []);
      setEnabled(data.enabled !== false);
      setEnabledKnown(true);
      setBaselineError('');
    } catch (e: unknown) {
      // 不能吞错：接口挂了却显示一张空表，与「确实没有基线」不可区分。
      setBaselineError(e instanceof Error ? e.message : String(e));
      throw e;
    } finally {
      setLoadingBaselines(false);
    }
  }, []);

  useEffect(() => {
    if (!active) return;
    fetchBaselines().catch(() => {});
  }, [active, fetchBaselines]);

  const handleRefreshBaselines = async () => {
    try {
      await fetchBaselines();
    } catch (e: unknown) {
      showToast(
        `分词器基线加载失败：${e instanceof Error ? e.message : String(e)}`,
        'error',
      );
    }
  };

  const handleToggleEnabled = async (next: boolean) => {
    if (toggling) return;
    const prev = enabled;
    setEnabled(next);
    setToggling(true);
    try {
      const data = await apiRequest<TokTogglePayload>('/direct-admin/api/tok-fingerprint', {
        method: 'POST',
        body: JSON.stringify({ enabled: next }),
      });
      // 200 + ok:false 不能当成功：会把「没切过去」显示成已切换。
      if (!data.ok) throw new Error(data.error || '切换失败（接口返回 ok=false）');
      const applied = data.enabled !== false;
      setEnabled(applied);
      showToast(`分词器指纹检测已${applied ? '开启' : '关闭'}`, 'success');
      // 刷新失败不改写开关结果：POST 已经生效，回退勾选框反而是假话。
      await fetchBaselines().catch(() => {});
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setEnabled(prev);
      showToast(`切换失败：${msg}`, 'error');
    } finally {
      setToggling(false);
    }
  };

  const handleIdentityCheck = async () => {
    if (identityRunning) return;
    setIdentityRunning(true);
    setIdentityError('');
    setIdentityResults(null);
    try {
      const data = await apiRequest<TokIdentityPayload>('/direct-admin/api/tok-identity-check', {
        method: 'POST',
        body: JSON.stringify({}),
      });
      // 200 + ok:false 也是失败：不能渲染成「0 个结果 = 没事」。
      if (!data.ok) throw new Error(`校准失败（ok=false），count=${data.count ?? 0}`);
      setIdentityResults(data.results || []);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setIdentityError(msg);
      showToast(msg, 'error');
    } finally {
      setIdentityRunning(false);
    }
  };

  const rows = baselines || [];
  const undetermined = (identityResults || []).filter((r) => r.vocabulary === '无法判定');
  const identityCount = identityResults === null ? null : identityResults.length;
  const identityStateText = identityRunning
    ? '校准中…（约 0.02 credit / 模型）'
    : identityError
      ? '失败'
      : identityCount === null
        ? '—'
        : identityCount
          ? `${identityCount} 个模型已校准`
          : '没有可校准的模型';

  return (
    <div id="tab-tok" className="ds-page-enter space-y-5">
      <PageHeader
        title="分词器指纹与词表身份鉴定"
        subtitle="同一模型对同类文本的 token 密度应当稳定；词表鉴定则回答「上游到底用谁的词表理解我们发出去的东西」。两项都不改写请求内容，只在网关本地做字符计数与最小探测。"
      />

      {/* ── 分词器指纹 ─────────────────────────────────── */}
      <Card className="overflow-hidden">
        <div className="px-4 py-3.5 border-b border-[var(--border)] bg-[var(--surface-2)]/50 flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-2">
            <div className="text-sm font-medium text-[var(--fg)]">分词器指纹基线</div>
            <p className="text-xs text-[var(--fg-2)] leading-relaxed max-w-3xl m-0">
              同一模型对<strong className="text-[var(--fg)]">同类文本</strong>切出的 token 数应当稳定 ——
              这是改不了的物理证据。上游如果把请求悄悄路由到别的模型，即使回显字段仍写原名字，分词器比例也会变。
              <br />
              按内容成分分桶（<code className="ds-mono text-[var(--fg)]">中文 / 数字 / ASCII</code>）：
              不同类别的 token 密度差 2~3 倍，混算会把差异淹没。数字密集文本最敏感 —— 实测{' '}
              <code className="ds-mono text-[var(--fg)]">deepseek-v4.1-flash</code> 为{' '}
              <code className="ds-mono text-[var(--fg)]">0.4552</code>、
              <code className="ds-mono text-[var(--fg)]">glm-5.3-flash</code> 为{' '}
              <code className="ds-mono text-[var(--fg)]">0.6018</code>（差 32%）。
              <br />
              <strong className="text-[var(--fg)]">不需要安装任何 tokenizer</strong>：token 数来自上游返回的{' '}
              <code className="ds-mono text-[var(--fg)]">prompt_tokens</code>，
              字符成分在网关本地按字符类别数（纯算术，零依赖）。
            </p>
          </div>
          <div className="flex items-center gap-2.5 flex-wrap shrink-0">
            <label
              title="关掉后完全不观测分词器比例；开启成本约为每请求一次字符遍历"
              className="inline-flex items-center gap-1.5 text-xs text-[var(--fg-2)] cursor-pointer select-none"
            >
              <input
                id="tokEnable"
                type="checkbox"
                checked={enabled}
                disabled={toggling || !enabledKnown}
                onChange={(e) => handleToggleEnabled(e.target.checked)}
                className="accent-[var(--accent)]"
              />
              启用分词器指纹检测
            </label>
            <Button
              id="btnRefreshTok"
              variant="ghost"
              size="sm"
              loading={loadingBaselines}
              onClick={handleRefreshBaselines}
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loadingBaselines ? 'animate-spin' : ''}`} />
              刷新基线
            </Button>
          </div>
        </div>

        <div id="tokTable">
          {baselineError && (
            <div className="mx-3 mt-3 rounded-[var(--radius-sm)] border border-[var(--err)] bg-[var(--err-bg)]/40 px-3 py-2 text-[11px] leading-relaxed text-[var(--err)]">
              基线加载失败：{baselineError}
              {enabledKnown ? '' : ' · 开关状态未知，已禁用开关以免盲切'}
            </div>
          )}

          {baselines === null ? (
            baselineError ? (
              <div className="px-4 py-4 text-center text-xs text-[var(--fg-3)]">
                未取到基线数据 · 点「刷新基线」重试
              </div>
            ) : (
              <div className="px-4 py-6 text-center text-xs text-[var(--fg-3)]">加载中…</div>
            )
          ) : !rows.length ? (
            <EmptyState
              className="m-3"
              title="还没有基线数据（需要至少 3 个样本）"
              description="每对（模型, 分桶）需要 3 次观测才能建立快/慢线；样本不足时只累计、不判偏离。"
            />
          ) : (
            <div className="overflow-x-auto max-h-[460px]">
              <table
                className="w-full text-left border-collapse text-xs min-w-[820px]"
                aria-label="分词器指纹基线"
              >
                <thead className="sticky top-0 z-10 bg-[var(--surface-2)]">
                  <tr className="border-b border-[var(--border)] text-[11px] uppercase tracking-wider text-[var(--fg-3)]">
                    <th className="py-2.5 px-3 font-medium">模型</th>
                    <th
                      className="py-2.5 px-3 font-medium"
                      title="按内容成分分桶；不同桶的 token 密度差 2~3 倍，不可混算"
                    >
                      分桶
                    </th>
                    <th
                      className="py-2.5 px-3 font-medium"
                      title="快线：观测到的最小比例（指数平滑）"
                    >
                      快基线
                    </th>
                    <th
                      className="py-2.5 px-3 font-medium"
                      title="慢线：观测到的最大比例（指数平滑）"
                    >
                      慢基线
                    </th>
                    <th className="py-2.5 px-3 font-medium" title="当前：最近一次观测的比例">
                      当前值
                    </th>
                    <th className="py-2.5 px-3 font-medium" title="样本：超出快/慢线 8% 才判异常">
                      样本数
                    </th>
                    <th className="py-2.5 px-3 font-medium">状态</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--border)]">
                  {rows.map((row) => {
                    const insufficient = (row.samples ?? 0) < 3;
                    return (
                      <tr
                        key={`${row.model}·${row.bucket}`}
                        className={
                          row.deviating
                            ? 'bg-[var(--warn-bg)]/35'
                            : 'hover:bg-[var(--surface-2)]/50'
                        }
                      >
                        <td className="py-2.5 px-3 ds-mono text-[var(--fg)] whitespace-nowrap">
                          {row.model || '—'}
                        </td>
                        <td className="py-2.5 px-3">
                          <StatusBadge tone="neutral">{bucketLabel(row.bucket)}</StatusBadge>
                        </td>
                        <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                          {formatRatio(row.fast)}
                        </td>
                        <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                          {formatRatio(row.slow)}
                        </td>
                        <td
                          className={`py-2.5 px-3 ds-mono ${
                            row.deviating
                              ? 'text-[var(--warn)] font-semibold'
                              : 'text-[var(--fg-2)]'
                          }`}
                        >
                          {formatRatio(row.lastRatio)}
                        </td>
                        <td className="py-2.5 px-3 ds-mono text-[var(--fg-2)]">
                          {row.samples ?? 0}
                        </td>
                        <td className="py-2.5 px-3 whitespace-nowrap">
                          {row.deviating === true ? (
                            <StatusBadge
                              tone="warn"
                              dot
                              title="当前值已超出快/慢线 8% 以上"
                            >
                              已偏离区间
                            </StatusBadge>
                          ) : row.deviating === false ? (
                            insufficient ? (
                              <StatusBadge
                                tone="neutral"
                                title="样本不足 3 次，还不判偏离"
                              >
                                样本不足
                              </StatusBadge>
                            ) : (
                              <StatusBadge tone="ok" dot title="当前值在快/慢线之内">
                                区间内
                              </StatusBadge>
                            )
                          ) : (
                            // deviating 缺失（旧数据 / 字段被吞）不能读成「区间内」。
                            <StatusBadge
                              tone="neutral"
                              title="服务端未给出 deviating 判定，无法判断是否偏离"
                            >
                              未判定
                            </StatusBadge>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </Card>

      {/* ── 词表身份鉴定 ───────────────────────────────── */}
      <Card className="overflow-hidden">
        <div className="px-4 py-3.5 border-b border-[var(--border)] bg-[var(--surface-2)]/50 flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-2">
            <div className="text-sm font-medium text-[var(--fg)]">词表身份鉴定</div>
            <p className="text-xs text-[var(--fg-2)] leading-relaxed max-w-3xl m-0">
              回答一个 system 面板最该回答的问题：{' '}
              <strong className="text-[var(--fg)]">我们发出去的东西，上游到底用什么词表理解的？</strong>
              这条判据不看上游回显的模型名 —— 名字可以照抄，词表计数不会。
              <br />
              做法：把 3 条固定样本不改动地发给上游，看它返回的{' '}
              <code className="ds-mono text-[var(--fg)]">prompt_tokens</code> 与官方 DeepSeek
              词表的精确计数是否只差一个<strong className="text-[var(--fg)]">恒定的模板开销</strong>。
              差值恒为 4（单条 user）⇒ 词表就是官方 DeepSeek。差值各不相同 ⇒ 换了词表。
              <br />
              <strong className="text-[var(--fg)]">权重（模型本体）无法验证</strong>：上游不返回{' '}
              <code className="ds-mono text-[var(--fg)]">logprobs</code>（实测三个模型都没有），
              而词表相同不代表权重相同 —— 蒸馏 / 微调模型可以继承词表。这一栏如实写「无法验证」，不留白。
            </p>
          </div>
          <div className="flex items-center gap-2.5 flex-wrap shrink-0">
            <span
              id="tokIdState"
              role="status"
              aria-live="polite"
              className={`ds-mono text-[11px] ${
                identityError
                  ? 'text-[var(--err)]'
                  : identityRunning
                    ? 'text-[var(--warn)]'
                    : 'text-[var(--fg-3)]'
              }`}
            >
              {identityStateText}
            </span>
            <Button
              id="btnTokIdCheck"
              variant="ghost"
              size="sm"
              loading={identityRunning}
              disabled={identityRunning}
              title="会消耗约 0.02 credit（7 次请求，每次 max_tokens=1）"
              onClick={handleIdentityCheck}
            >
              <ScanSearch className="w-3.5 h-3.5" />
              {identityRunning ? '校准中…' : '立即校准'}
            </Button>
          </div>
        </div>

        <div className="px-4 py-2.5 border-b border-[var(--border)] bg-[var(--surface-2)]/40 text-[11px] text-[var(--fg-3)] leading-relaxed">
          <strong className="text-[var(--fg-2)]">成本</strong>：约{' '}
          <code className="ds-mono text-[var(--fg)]">0.02 credit</code>{' '}
          / 模型（3 条词表样本 + 4 种模板结构，每次只输出 1 token）。
          只做手动触发，不设定时任务 —— 词表不会天天变，持续烧额度没有意义。
        </div>

        <div id="tokIdTable">
          {identityError ? (
            <div className="px-4 py-4">
              <p className="m-0 text-xs leading-relaxed text-[var(--err)]">
                校准失败：{identityError}
              </p>
              <p className="m-0 mt-1.5 text-[11px] leading-relaxed text-[var(--fg-3)]">
                接口整体失败时没有任何结果可比对，同样不能读成「词表正常」。
              </p>
            </div>
          ) : identityRunning ? (
            <div className="px-4 py-6 text-center text-xs text-[var(--fg-3)]">
              正在请求上游…
            </div>
          ) : identityResults === null ? (
            <EmptyState
              className="m-3"
              title="点「立即校准」开始（未校准时不做任何请求）"
              description="每次校准对在役模型各发 7 次最小请求（3 条词表样本 + 4 种模板结构），只手动触发。"
            />
          ) : !identityResults.length ? (
            <EmptyState className="m-3" title="没有在役模型（账号池为空或全部冷却）。" />
          ) : (
            <>
              {undetermined.length > 0 && (
                <div className="mx-3 mt-3 flex items-start gap-2 rounded-[var(--radius-sm)] border border-[var(--warn)] bg-[var(--warn-bg)]/40 px-3 py-2.5">
                  <TriangleAlert className="w-3.5 h-3.5 shrink-0 text-[var(--warn)] mt-0.5" />
                  <div className="text-[11px] leading-relaxed text-[var(--fg)]">
                    <strong className="text-[var(--warn)]">
                      {undetermined.length} 个模型的词表判定为「无法判定」
                    </strong>
                    ：这次探测没拿到可用结论，<strong>不代表词表正常</strong>。
                    上游未返回 prompt_tokens、样本缺失或模板开销对不上都会落到这一档，具体原因见下方「备注」（通常带「校准失败：…」）。
                    <span className="ds-mono text-[var(--fg-2)]">
                      {' '}
                      （{undetermined.map((r) => r.model).join('、')}）
                    </span>
                  </div>
                </div>
              )}

              <div className="overflow-x-auto max-h-[520px]">
                <table
                  className="w-full text-left border-collapse text-xs min-w-[1040px]"
                  aria-label="词表身份鉴定结果"
                >
                  <thead className="sticky top-0 z-10 bg-[var(--surface-2)]">
                    <tr className="border-b border-[var(--border)] text-[11px] uppercase tracking-wider text-[var(--fg-3)]">
                      <th className="py-2.5 px-3 font-medium">模型</th>
                      <th className="py-2.5 px-3 font-medium">站点</th>
                      <th
                        className="py-2.5 px-3 font-medium"
                        title="三项固定样本的差值是否一致且等于模板开销"
                      >
                        词表
                      </th>
                      <th
                        className="py-2.5 px-3 font-medium"
                        title="词表相同不代表权重相同；上游不提供 logprobs，无从验证"
                      >
                        权重
                      </th>
                      <th
                        className="py-2.5 px-3 font-medium"
                        title="四种消息结构的模板开销是否符合实测公式"
                      >
                        模板公式
                      </th>
                      <th className="py-2.5 px-3 font-medium">样本明细</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--border)]">
                    {identityResults.map((r, idx) => {
                      const evidence = sampleEvidence(r);
                      const tplDetail = templateDetailText(r);
                      return (
                        <Fragment key={`${r.model}·${r.site || ''}·${idx}`}>
                          <tr
                            className={
                              r.vocabulary === '无法判定'
                                ? 'bg-[var(--warn-bg)]/25'
                                : 'hover:bg-[var(--surface-2)]/50'
                            }
                          >
                            <td className="py-2.5 px-3 ds-mono text-[var(--fg)] whitespace-nowrap">
                              {r.model || '—'}
                            </td>
                            <td className="py-2.5 px-3 text-[var(--fg-2)] whitespace-nowrap">
                              {siteText(r.site)}
                            </td>
                            <td className="py-2.5 px-3">{vocabularyBadge(r.vocabulary)}</td>
                            <td className="py-2.5 px-3">
                              <StatusBadge
                                tone="neutral"
                                title="上游不返回 logprobs，词表相同也不能证明权重相同"
                              >
                                {r.weight || '无法验证（上游不提供 logprobs）'}
                              </StatusBadge>
                            </td>
                            <td className="py-2.5 px-3 align-top">
                              {templateBadge(templateState(r))}
                              {tplDetail && (
                                <div className="mt-1 ds-mono text-[10px] leading-relaxed text-[var(--fg-3)]">
                                  {tplDetail}
                                </div>
                              )}
                            </td>
                            <td className="py-2.5 px-3 align-top ds-mono text-[10px] leading-relaxed text-[var(--fg-2)]">
                              {evidence || <span className="text-[var(--fg-3)]">—</span>}
                            </td>
                          </tr>
                          {r.note && (
                            // 备注没有独立列：整行一条，不把长文案挤进任何单元格，
                            // 也保证 note 永远可见（不是只能悬浮看的 tooltip）。
                            <tr className="bg-[var(--surface-2)]/30">
                              <td
                                colSpan={6}
                                className="px-3 pb-2 pt-0 text-[11px] leading-relaxed text-[var(--fg-2)]"
                              >
                                <span className="text-[var(--fg-3)]">备注 </span>
                                {r.note}
                              </td>
                            </tr>
                          )}
                        </Fragment>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      </Card>
    </div>
  );
}
