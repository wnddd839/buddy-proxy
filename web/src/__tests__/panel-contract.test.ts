import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// 面板契约（源码级）
//
// 上游 v0.8.0 把 4618 行的单文件 admin 页换成了 React + Vite。旧分支里
// internal/admin/*_test.go 有 18 个测试直接对 PageHTML() 的 HTML 字符串做
// 断言（`<th>模型</th>`、`id="tokTable"` 之类）。React 产物里 JSX 序列化成了
// `n.jsx("th",{...})`、id 变成 JS 属性，字符串断言必然失效 —— 但断言背后的
// *意图*（列序、列数守恒、口径文案、判定兜底）与实现语言无关。
//
// 因此这些契约下沉到这里：直接读页面源码文本，一个 it() 对应一个旧 Go 测试，
// 标题里保留旧测试名，便于 grep 追溯。
//
// 迁移原则：只断言源码里真实存在的写法；无法在现有 React 页面里表达的
// 契约不在这里弱化成看似通过的断言，而是留在 Go 测试里（并在交接报告里点名）。
const here = dirname(fileURLToPath(import.meta.url));
const src = (p: string) => readFileSync(join(here, '..', p), 'utf8');

const usage = src('pages/UsagePage.tsx');
const tok = src('pages/TokPage.tsx');
const overview = src('pages/OverviewPage.tsx');
const pool = src('pages/PoolPage.tsx');
const models = src('pages/ModelsPage.tsx');
const checkin = src('pages/CheckinPage.tsx');
const client = src('pages/ClientPage.tsx');
const systemPrompt = src('pages/SystemPromptPage.tsx');
const invariants = src('pages/InvariantsPage.tsx');
const app = src('App.tsx');
const apiLib = src('lib/api.ts');
const apiTypes = src('types/api.ts');
const domain = src('lib/domain.ts');

/** 取 text 中 start..end（含端点）的片段；找不到即测试失败。 */
function region(text: string, start: string, end: string): string {
  const i = text.indexOf(start);
  expect(i, `找不到起始标记 ${start}`).toBeGreaterThanOrEqual(0);
  const j = text.indexOf(end, i + start.length);
  expect(j, `找不到结束标记 ${end}`).toBeGreaterThanOrEqual(0);
  return text.slice(i, j + end.length);
}

/** 抽出片段里每个 <th> 的纯文本标签（去掉内部标签与多余空白）。 */
function thLabels(block: string): string[] {
  return [...block.matchAll(/<th(?=[\s>])[^>]*>([\s\S]*?)<\/th>/g)].map((m) =>
    m[1]
      .replace(/<[^>]+>/g, '')
      .replace(/\{[^}]*\}/g, '')
      .replace(/\s+/g, ' ')
      .trim(),
  );
}

/** 取某个表头单元格的完整源码（含 title 等属性）。 */
function thBlockByLabel(block: string, label: string): string {
  const match = [...block.matchAll(/<th(?=[\s>])[^>]*>[\s\S]*?<\/th>/g)].find(
    (m) =>
      m[0]
        .replace(/<[^>]+>/g, '')
        .replace(/\s+/g, ' ')
        .trim() === label,
  );
  expect(match, `找不到列 ${label}`).toBeDefined();
  return match![0];
}

// 07 用量页「请求追踪明细」表：从 aria-label 到 </table>。
const usageTable = region(usage, 'aria-label="请求明细"', '</table>');
const usageThead = region(usageTable, '<thead', '</thead>');

describe('usage table (旧 internal/admin/usage_table_cols_test.go)', () => {
  it('TestUpstreamModelColumnOrder: 列序必须是 模型 → 上游模型 → Credit', () => {
    const labels = thLabels(usageThead);
    const model = labels.indexOf('模型');
    const upstream = labels.indexOf('上游模型');
    const credit = labels.indexOf('单次 Credit');
    expect(model, `表头未找齐: ${JSON.stringify(labels)}`).toBeGreaterThanOrEqual(0);
    expect(upstream).toBeGreaterThanOrEqual(0);
    expect(credit).toBeGreaterThanOrEqual(0);
    // 客户端要的 / 上游回显 / 扣费 三列必须相邻可对照
    expect(model).toBeLessThan(upstream);
    expect(upstream).toBeLessThan(credit);
    expect(upstream - model).toBe(4);
  });

  it('TestUsageTableColumnCountConsistent: 表头 / 数据行 / 空态 colspan 三处列数一致', () => {
    const thCount = (usageThead.match(/<th(?=[\s>])/g) || []).length;
    // 整表里的 <td>：数据行 14 个 + 空态 1 个（空态用 colSpan 占满）
    const tdCount = (usageTable.match(/<td(?=[\s>])/g) || []).length;
    const span = /colSpan=\{(\d+)\}/.exec(usageTable);
    expect(span, '空态缺少 colSpan').not.toBeNull();
    const colSpan = Number(span![1]);

    expect(thCount).toBeGreaterThanOrEqual(14);
    // 唯一不是数据单元格的那个 td 就是空态
    expect(thCount).toBe(tdCount - 1);
    expect(colSpan).toBe(thCount);
  });

  it('TestUsageTableNoDuplicateColumns: 关键列不得重复（历史事故：重复的「上游模型」让整表错位）', () => {
    const labels = thLabels(usageThead);
    for (const name of ['模型', 'System', '上游模型', '分词器比例', '单次 Credit']) {
      expect(labels.filter((l) => l === name), `列 ${name} 应恰好 1 个，实际 ${JSON.stringify(labels)}`).toHaveLength(1);
    }
    // 整表标签也不能撞名
    expect(new Set(labels).size).toBe(labels.length);
  });

  it('TestUsageCreditColumnIsDistinguishedFromBalance: Credit 列必须写明是单次扣费，不是余额', () => {
    const cell = thBlockByLabel(usageThead, '单次 Credit');
    expect(cell).toContain('单次扣费');
    expect(cell).toContain('不是余额');
    expect(cell).toMatch(/余额看/);
    // 01 概览的余额是另一个概念，两处标题不得混用
    expect(usage).toContain('Credits 累计');
  });

  it('TestUsageRowTotalTokensHasNoDeadBranch: 不得读后端不存在的 totalTokens 字段', () => {
    // usagejournal.Entry 没有 totalTokens（只有 Summary / ModelStat /
    // ChartPoint 有）。一旦回到 `row.totalTokens ||` 就是死分支 ——
    // 那也是这条测试在旧面板上要防的原始 bug。
    expect(domain).not.toContain('row.totalTokens');
    const entry = region(apiTypes, 'export interface UsageEntry {', '\n}');
    expect(entry).not.toContain('totalTokens');
  });
});

describe('上游模型回显（旧 internal/admin/model_echo_panel_test.go）', () => {
  it('TestModelEchoHasThreeBuckets: 05 必须有「一致 / 不一致 / 上游解析」三个分类计数，07 单元格四态可分', () => {
    // 05 的三个分类：只报总数无法区分「没被换」与「auto 本来就不同」
    for (const id of ['echoMatch', 'echoMismatch', 'echoAuto']) {
      expect(models, `缺少分类计数 ${id}`).toContain(`id="${id}"`);
    }
    const cell = region(usage, 'const renderUpstreamModelCell', 'const renderTokRatioCell');
    // 未观测
    expect(cell).toMatch(/if \(!upstream\)/);
    // 被替换（告警色 + 说清差在哪）
    expect(cell).toMatch(/if \(mismatch\)/);
    expect(cell).toContain('模型被替换');
    // auto 一族：上游解析，属正常，不得算替换
    expect(cell).toContain('if (isAutoFamily(sent))');
    expect(cell).toContain('/ 解析');
    // 一致：原样显示，不加告警
    expect(cell).toMatch(/return <span className="ds-mono text-\[var\(--fg\)\]">\{upstream\}<\/span>;/);
  });
});

describe('分词器指纹 + 词表身份鉴定（旧 internal/admin/tok_panel_test.go / tokid_panel_test.go）', () => {
  // 区块标题（JSX 里的可见文本），别命中上面的状态注释
  const fingerprintCard = region(tok, '>分词器指纹基线</div>', '>词表身份鉴定</div>');
  const baselineHead = region(tok, 'aria-label="分词器指纹基线"', '</thead>');

  it('TestTokFingerprintPanelExplainsNoInstall: 必须说清「不需要装 tokenizer」以及数字从哪来', () => {
    for (const needle of [
      '分词器指纹基线',
      '不需要安装任何 tokenizer',
      'token 数来自上游返回的',
      'prompt_tokens',
      '字符成分在网关本地按字符类别数',
      '按内容成分分桶',
      '0.4552',
      '0.6018',
      'id="tokEnable"',
      'id="tokTable"',
      'id="btnRefreshTok"',
    ]) {
      expect(fingerprintCard, `缺少 ${needle}`).toContain(needle);
    }
  });

  it('TestTokBaselineTableColumns: 基线表必须有 快基线 / 慢基线 / 当前值 / 样本数', () => {
    const labels = thLabels(baselineHead);
    for (const name of ['快基线', '慢基线', '当前值', '样本数']) {
      expect(labels, `基线表缺少列 ${name}，实际 ${JSON.stringify(labels)}`).toContain(name);
    }
  });

  it('TestI9RegisteredWithWhy: I9 是分词器比例，不能与 I8 上游模型混为一谈', () => {
    expect(tok).toContain('分词器');
    expect(thLabels(usageThead)).toContain('分词器比例');
    expect(thLabels(usageThead)).toContain('上游模型');
  });

  it('TestTokIdentityPanelWiring: 判据 / 权重无法验证 / 成本三件事都要写出来', () => {
    for (const needle of [
      '词表身份鉴定',
      'prompt_tokens</code> 与官方',
      '差值恒为 4',
      '差值各不相同',
      '权重（模型本体）无法验证',
      'logprobs',
      '蒸馏 / 微调模型可以继承词表',
      'id="btnTokIdCheck"',
      'id="tokIdTable"',
      '0.02 credit',
      '不设定时任务',
    ]) {
      expect(tok, `缺少 ${needle}`).toContain(needle);
    }
  });

  it('TestTokIdentityTableColumns: 结果表必须分得清 词表 / 权重 / 模板公式 / 样本明细', () => {
    const labels = thLabels(region(tok, 'aria-label="词表身份鉴定结果"', '</thead>'));
    expect(labels).toContain('词表');
    expect(labels).toContain('权重');
    const identityCard = region(tok, 'aria-label="词表身份鉴定结果"', '</table>');
    expect(identityCard).toContain('模板公式');
    expect(identityCard).toContain('样本明细');
  });

  it('TestTokIdentityVerdictFallback: 校准失败不得显示「通过」，必须落到「无法判定 / 无法验证」', () => {
    expect(tok).toContain('无法判定');
    expect(tok).toContain('无法验证');
    // 兜底不是文案摆设：词表徽标在未知值时必须原样露出「无法判定」
    expect(tok).toMatch(/vocabulary \|\| '无法判定'/);
    // 权重列缺省时必须显式写「无法验证（上游不提供 logprobs）」
    expect(tok).toContain('无法验证（上游不提供 logprobs）');
  });
});

describe('01 概览（旧 internal/admin/panel_semantics2_test.go）', () => {
  it('TestNoMisleadingPeakLabel: 并发峰值不得命名为「1m 峰值」', () => {
    const idx = overview.indexOf('id="ccPeak"');
    expect(idx, '找不到 ccPeak').toBeGreaterThan(0);
    // 标签在 <strong id="ccPeak"> 之前；这段里不得出现分钟级口径
    const label = overview.slice(Math.max(0, idx - 400), idx);
    expect(label).toContain('峰值');
    expect(label).not.toMatch(/1m|1 分钟|分钟/);
  });

  it('TestConcurrencyLabelsMatchBackendSemantics: 并发标签必须是「1h 峰值」，不是「1m 峰值」', () => {
    // 后端返回的是最近 1 小时内各分钟桶峰值的最大值
    expect(overview).toContain('在途 · 1h 峰值 <strong id="ccPeak"');
    expect(overview).toContain('各分钟桶峰值的最大值');
  });
});

describe('口径说明（旧 internal/admin/panel_semantics_test.go / panel_semantics2_test.go）', () => {
  it('TestPanelMetricScopesAreDocumented: 每个跨页面口径差异都必须写明', () => {
    const expected: Array<[string, string, string]> = [
      [overview, '落盘统计，随上方所选区间变化', '01 的请求/Token 优先用落盘统计'],
      [overview, '回退到进程内累计', '落盘不可用时回退到进程内累计'],
      [overview, '重启清零', '进程内累计重启清零'],
      [overview, '各分钟桶峰值的最大值', '并发峰值是 1h 内各分钟桶的最大值'],
      [overview, '限流事件次数，不是账号数', '1h 限流计数是事件数'],
      [overview, '有余额快照账号的合计', 'Credits 是快照账号合计，不是首个账号'],
      [usage, '缓存 token ÷ prompt token', '07 命中率的分母口径'],
      [systemPrompt, 'UTF-8 字节数', '06 的 system 长度是字节数（Go 的 len(string)）'],
      [systemPrompt, '中文 system 的字节数约为字符数的 3 倍', '说明字节与字符的关系，避免误读'],
    ];
    const missing = expected.filter(([text, needle]) => !text.includes(needle)).map(([, n]) => n);
    expect(missing).toEqual([]);
  });

  it('TestPanelSemanticsRound2: 02/03/04/05 的口径差异都必须写明', () => {
    const expected: Array<[string, string]> = [
      [pool, '该账号<strong>历史累计</strong>'],
      [pool, '重启不清零'],
      [pool, '只表示'],
      [pool, '快照 6 小时内有效'],
      [checkin, '北京时间'],
      [checkin, '本地草稿'],
      [checkin, '保存始终包含两站全部规则'],
      [client, '并列的三个端点'],
      [models, '本页三块<strong>互不依赖</strong>'],
      [models, '从两个站各拉一份'],
      [models, '你<strong>额外配置</strong>的改写规则'],
    ];
    const missing = expected.filter(([text, needle]) => !text.includes(needle)).map(([, n]) => n);
    expect(missing).toEqual([]);
  });
});

describe('06 System Prompt（旧 internal/admin/panel_semantics_test.go / panel_semantics2_test.go）', () => {
  it('TestSystemPromptShowsStrippedCount: Stats.Stripped 必须在界面上可见', () => {
    // 加了剥离开关却看不到剥离次数 —— 表格里的「已剥离」形态徽标只说明单条，
    // 这里要的是汇总计数卡。
    expect(systemPrompt).toContain('valueId="spStripped"');
    expect(systemPrompt).toContain('label="已剥离"');
    expect(systemPrompt).toContain('stats.stripped');
  });

  it('TestSystemLenColumnSaysChars: 长度列不得命名为「system 字节」，且必须说明是 UTF-8 字节数', () => {
    expect(systemPrompt).not.toContain('system 字节');
    expect(systemPrompt).toContain('system 长度');
    expect(systemPrompt).toContain('UTF-8 字节数');
  });
});

describe('05 模型页与标题改写（旧 internal/admin/model_echo_panel_test.go / title_panel_test.go）', () => {
  it('TestModelEchoPanelWiring: 07 的列 + 05 的对照区块 + 三态渲染必须齐全', () => {
    // 07 明细列（口径写在表头 title 里）
    expect(usage).toContain('上游模型');
    expect(usage).toContain('上游 SSE 帧里回显的 model 字段');
    // 05 对照区块：三分类计数 + 刷新入口 + 归一化说明
    for (const needle of [
      '上游模型回显对照',
      '归一化后比较',
      'auto 一族由上游解析',
      'id="echoMismatch"',
      'id="echoTotal"',
      'id="btnRefreshEcho"',
      'upstreamModelCell',
      '模型被替换',
    ]) {
      expect(models, `缺少 ${needle}`).toContain(needle);
    }
  });

  it('TestTitleRoutePanelExplainsUnconfigured: 开关开着但没配目标时必须说明「实际不改写」', () => {
    for (const needle of [
      '沿用请求模型（不改写）',
      '已启用但未配置目标',
      '实际不改写，标题仍由主模型生成',
    ]) {
      expect(models, `缺少 ${needle}`).toContain(needle);
    }
  });
});

describe('05 模型页（旧 internal/admin/title_panel_test.go）', () => {
  /** 收集 src 下所有 .ts/.tsx（跳过测试目录自身，避免自匹配）。 */
  function walk(dir = join(here, '..')): string[] {
    return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
      const p = join(dir, e.name);
      if (e.isDirectory()) return e.name === '__tests__' ? [] : walk(p);
      return /\.tsx?$/.test(e.name) ? [p] : [];
    });
  }

  it('TestTitleRoutePanelHasNoWbLite: wb-lite 已删除，源码里不得再有它的常量或下拉项', () => {
    const banned = ['TITLE_LITE_MODEL', 'wb-lite', '隐藏轻量模型'];
    const offenders: string[] = [];
    for (const file of walk()) {
      const text = readFileSync(file, 'utf8');
      for (const bad of banned) {
        if (text.includes(bad)) offenders.push(`${file}: ${bad}`);
      }
    }
    expect(offenders).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// 静默失效不变式（本轮评审新增）：四条源码级契约，对应四类历史事故。
// 状态竞态本身无法在源码字符串里断言，这里只钉住「守卫/兜底写法必须存在」。
// ---------------------------------------------------------------------------
describe('静默失效：乱序响应与后台刷新失败必须留痕', () => {
  it('TestPollDoesNotSwallowErrors: 5s 轮询失败不得被空 catch 吞掉', () => {
    const poll = region(systemPrompt, 'window.setInterval(() => {', '}, 5000)');
    expect(poll).toContain('setOverviewError');
    expect(poll).not.toMatch(/\.catch\(\(\) => \{\}\)/);
  });

  it('TestPollDoesNotRefetchHistory: 轮询只刷概览，不带渲染时的旧页码去抢历史表格', () => {
    const poll = region(systemPrompt, 'window.setInterval(() => {', '}, 5000)');
    expect(poll).toContain('fetchOverviewOnly');
    expect(poll).not.toContain('fetchOverviewAndHistory');
  });

  it('TestStaleResponseGuards: 日期/翻页/筛选/区间必须有「只认最后一次响应」的守卫', () => {
    // 06：概览与历史各自一条序号
    expect(systemPrompt).toContain('if (seq !== overviewSeqRef.current) return null;');
    expect(systemPrompt).toContain('if (seq !== historySeqRef.current) return;');
    // 07：筛选/翻页
    expect(usage).toContain('if (seq !== reqSeqRef.current) return;');
    // 01：区间切换
    expect(overview).toContain('if (seq !== usageSeqRef.current) return;');
  });

  it('TestOverviewUsageErrorIsVisible: 落盘统计读不到时必须说出来，并标明回退口径', () => {
    expect(overview).toContain('setUsageError(');
    expect(overview).toContain('落盘统计读取失败');
    expect(overview).toContain('上面指标已回退为进程内累计');
  });

  it('TestApiRejectsNonJson200: 200 却不是 JSON（网关错误页）必须算失败，不能当成空数据', () => {
    const apiRequestFn = region(apiLib, 'export async function apiRequest', 'export async function updateFetch');
    expect(apiRequestFn).toContain('响应不是 JSON');
    expect(apiRequestFn).not.toContain('data = { raw: text };');
    // 更新接口同样不能把错误页当成功
    expect(region(apiLib, 'export async function updateFetch', 'export async function copyToClipboard')).toContain(
      '响应不是 JSON',
    );
  });

  it('TestStaleLoadsAreLabelled: 刷新失败时旧数据必须被标注为「上一次成功加载的」', () => {
    expect(systemPrompt).toContain('下面显示的是上一次成功加载的结果');
    expect(invariants).toContain('下面仍是上一次成功加载的体检结果');
    expect(models).toContain('下面的计数仍是上一次成功加载的结果');
    expect(usage).toContain('下面不显示上一次查询的旧数据');
  });
});

describe('静默失效：未知不得被显示成零', () => {
  it('TestSystemPromptStatsUseDashUntilLoaded: 八个统计卡都走 statValue，未加载时显示 —', () => {
    expect(systemPrompt).toMatch(
      /const statValue = \(v\?: number \| null\) => \(overview && v != null \? v : '—'\);/,
    );
    expect((systemPrompt.match(/value=\{statValue\(/g) || []).length).toBeGreaterThanOrEqual(8);
    // 旧写法：字段缺省 → 0，前端看不出「还没加载」还是「真的零次」
    expect(systemPrompt).not.toMatch(/value=\{stats\.\w+ \?\? 0\}/);
  });

  it('TestSystemPromptStatsScopeDoesNotGuess: 概览未加载时不得断言「按今日落盘统计」', () => {
    expect(systemPrompt).toContain("'—（概览尚未加载）'");
    // 历史总数未知时也不能写 0 条
    expect(systemPrompt).toContain('historyResp?.total ?? null');
    expect(systemPrompt).not.toMatch(/historyResp\?\.total \|\| 0/);
  });

  it('TestUsageKpisUseDashUntilLoaded: 请求/失败/Token 三个 KPI 不得用 0 兜底', () => {
    expect(usage).toContain('const kpi = (v?: number | null');
    expect((usage.match(/value=\{kpi\(/g) || []).length).toBeGreaterThanOrEqual(3);
    expect(usage).not.toContain('value={(sum?.requests || 0).toLocaleString()}');
    expect(usage).not.toContain('value={formatCompactNumber(sum?.totalTokens || 0)}');
  });

  it('TestUsageDoesNotShowOtherQueryRows: 当前查询没拿到数据时不能展示上一个筛选的行', () => {
    expect(usage).toContain('const view = data && loadedKey === queryKey ? data : null;');
    expect(usage).toContain('const rows = view?.requests || [];');
  });

  it('TestOverviewFallbackIsLabelledByScope: 回退进程内累计时指标标签必须改口径', () => {
    expect(overview).toContain('const uSum = usageForRange === range ? usageData?.summary : undefined;');
    expect(overview).toContain('const rangeScopeLabel = usingFallback');
    // 区间切换、新数据未到时必须写「加载中」，不能冒充进程内累计。
    expect(overview).toContain('加载中…（暂显进程内累计）');
    expect(overview).toContain('rangeSwitching');
  });

  it('TestOverviewPoolNotZeroBeforeStatus: 状态未到不得显示「0/0 启用」或「尚未添加账号」', () => {
    expect(overview).toContain("{statusKnown ? poolSummary.healthy : '—'}");
    expect(overview).toContain("{statusKnown ? poolSummary.enabled : '—'} 启用");
    expect(domain).toContain('known: false');
  });

  it('TestModelsEmptyStateDistinguishesFailure: 拉取失败 / 筛选无命中 / 真的没数据 三种文案必须分开', () => {
    expect(models).toContain('模型编目加载失败');
    expect(models).toContain('没有匹配筛选条件的模型');
    expect(models).toContain('拉取失败不等于没有模型');
    expect(models).toContain('modelsError');
  });

  it('TestAutoFamilyPredicateSharedWithBackend: auto 判据必须是 auto / auto-*，不得把 autopilot 当成 auto', () => {
    // 后端 isAutoFamily：m == "auto" || HasPrefix(m, "auto-")
    expect(domain).toContain("m === 'auto' || m.startsWith('auto-')");
    expect(models).toContain('isAutoFamilyModel(sent)');
    expect(usage).toContain('isAutoFamilyModel as isAutoFamily');
    expect(models).not.toMatch(/indexOf\('auto'\) === 0/);
  });

  it('TestEchoMismatchNotDefaultedToMatch: 旧条目缺 modelMismatch 时按判据本地算，不得默认「一致」', () => {
    expect(models).toContain('r.modelMismatch ??');
    expect(models).toContain('normalizeModelName(r.upstreamModel');
  });
});

describe('静默失效：写入失败与被覆盖的草稿', () => {
  it('TestTitleSaveKeepsRouteDrafts: 父级载荷回写不得冲掉未保存的路由草稿', () => {
    expect(models).toContain('prevSnap && JSON.stringify(prev) !== prevSnap ? prev : normalized');
    expect(models).toContain('savedSnapshotRef.current = snap;');
  });

  it('TestModelsScopeOnlyAfterSuccess: 编目范围是「已加载成功的那一份」，不是用户刚点的那个', () => {
    const fn = region(app, 'const refreshModels = useCallback', '\n  }, []);');
    expect(fn).toMatch(/setModelsScope\(target\);\n\s*setModelsError\(''\);/);
    expect(app).toContain('modelsError={modelsError}');
    // 范围切换不再由子页面乐观设置
    expect(models).not.toContain('onSetModelsScope');
    expect(app).not.toContain('onSetModelsScope');
  });

  it('TestBothSitesFetchFailsLoudly: 两站合并时单站失败不得静默只显示一站', () => {
    const fn = region(app, 'const refreshModels = useCallback', '\n  }, []);');
    expect(fn).toContain('if (failures.length) throw new Error(failures.join');
  });

  it('TestStripSwitchGuardsDoubleSubmit: 剥离开关写入期间不得二次提交', () => {
    expect(systemPrompt).toContain('if (switchingStrip) return;');
  });
});

describe('旧面板对账补齐（2026-11-28 第二轮）', () => {
  const shell = src('components/layout/AppShell.tsx');
  const invariants = src('pages/InvariantsPage.tsx');

  it('P0 静默失效命中必须在导航与概览首屏可见', () => {
    // 旧面板把命中数摆在 01 首屏；移植后不能只剩一个入口。
    expect(shell).toContain("id: 'tab-invariants'");
    expect(shell).toContain('invariantHits');
    expect(overview).toContain('id="ovInvariantHits"');
    expect(overview).toContain("onNavigate('tab-invariants')");
    // 未知与 0 必须区分
    expect(overview).toContain('尚未加载（未知 ≠ 无命中）');
  });

  it('P2 invList 容器 id 必须存在', () => {
    expect(invariants).toContain('id="invList"');
  });

  it('P1 事件表 system 长度列必须带 UTF-8 字节 tooltip', () => {
    expect(systemPrompt).toContain('system 长度');
    expect(systemPrompt).toContain('UTF-8 字节数');
    expect(systemPrompt).not.toContain('>大小<');
  });

  it('P1 标题改写三句安全口径必须写出来', () => {
    for (const needle of [
      'fail-closed',
      '只改模型名',
      'reasoning_effort',
      '自动停止识别',
    ]) {
      expect(models, `缺少 ${needle}`).toContain(needle);
    }
  });
});

describe('终审修复', () => {
  const shell = src('components/layout/AppShell.tsx');
  const app = src('App.tsx');

  it('静默失效命中必须区分「证据丢弃」与「过期」', () => {
    // 丢弃 / 写盘失败时不能渲染成「无命中」；接口失败时旧值要标过期。
    expect(app).toContain('invariantDropped');
    expect(app).toContain('setInvariantStale(true)');
    expect(shell).toContain('invariantDropped + invariantWriteErrors');
    expect(overview).toContain('已丢弃');
    expect(overview).toContain('（已过期）');
    expect(overview).toContain('未知 ≠ 无命中');
  });

  it('模型替换判据必须先采信后端 modelMismatch', () => {
    const iMismatch = models.indexOf('r.modelMismatch === true');
    const iAuto = models.indexOf('isAutoFamilyModel(sent)');
    expect(iMismatch, '缺少后端判定优先').toBeGreaterThan(0);
    expect(iAuto, '缺少 auto 分类').toBeGreaterThan(iMismatch);
  });

  it('非 JSON 响应必须带上 HTTP 状态码', () => {
    const api = src('lib/api.ts');
    expect(api).toContain('HTTP ${res.status}（响应不是 JSON');
  });
});

describe('剩余项（超时 / 草稿 / 切换中标签）', () => {
  it('apiRequest 必须有超时出口', () => {
    const api = src('lib/api.ts');
    expect(api).toContain('AbortController');
    expect(api).toContain('请求超时');
    expect(api).toContain('window.clearTimeout(timer)');
  });

  it('标题草稿必须可放弃，且 payload 更新不覆盖未保存草稿', () => {
    expect(models).toContain('titleSavedRef');
    expect(models).toContain('放弃标题草稿');
    expect(models).toContain('handleResetTitleRoute');
  });

  it('区间切换中必须明说加载中，而不是冒充进程内累计', () => {
    expect(overview).toContain('加载中…（暂显进程内累计）');
    expect(overview).toContain('rangeSwitching');
  });
});
