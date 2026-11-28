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
    const credit = labels.indexOf('Credit');
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
    for (const name of ['模型', 'System', '上游模型', '分词器比例', 'Credit']) {
      expect(labels.filter((l) => l === name), `列 ${name} 应恰好 1 个，实际 ${JSON.stringify(labels)}`).toHaveLength(1);
    }
    // 整表标签也不能撞名
    expect(new Set(labels).size).toBe(labels.length);
  });

  it('TestUsageCreditColumnIsDistinguishedFromBalance: Credit 列必须写明是单次扣费，不是余额', () => {
    const cell = thBlockByLabel(usageThead, 'Credit');
    expect(cell).toContain('单次扣费');
    expect(cell).toContain('不是余额');
    expect(cell).toMatch(/余额看/);
    // 01 概览的余额是另一个概念，两处标题不得混用
    expect(usage).toContain('Credits 累计');
  });

  it('TestUsageRowTotalTokensHasNoDeadBranch: 读 row.totalTokens 就必须在类型里声明该字段', () => {
    // lib/domain.ts 的 usageRowTotalTokens 在 totalTokens 缺省时回退到 p+c
    expect(domain).toContain('row.totalTokens ||');
    const entry = region(apiTypes, 'export interface UsageEntry {', '\n}');
    // 死分支的判据不是「有没有 ||」，而是「读的字段是否存在」
    expect(entry).toMatch(/totalTokens\?: number;/);
    expect(entry).toMatch(/promptTokens\?: number;/);
    expect(entry).toMatch(/completionTokens\?: number;/);
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
      [overview, '本次启动以来累计', '01 的成功/失败是进程内累计'],
      [overview, '重启清零', '同上'],
      [overview, '各分钟桶峰值的最大值', '并发峰值是 1h 内各分钟桶的最大值'],
      [overview, '限流事件次数，不是账号数', '1h 限流计数是事件数'],
      [overview, '只显示首个账号', '概览 Credits 余额只来自 primary 账号'],
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
