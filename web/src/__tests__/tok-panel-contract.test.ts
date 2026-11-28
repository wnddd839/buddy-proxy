import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// 词表身份鉴定面板契约（源码级）
//
// 权威来源是旧面板 docs/superpowers/handoff/legacy-panel-page.go.txt：
//   ~1395–1425  「词表身份鉴定」区块说明 + 成本文案
//   ~2722–2762  renderTokIdTable()：结果表 6 列 + 每行模板/样本证据
// React 迁移后这张表一度退化成 5 列，把「模板公式 / 样本明细」两列独立证据
// 塞进「备注」单元格 —— 列还在，但对照关系没了。这里把 6 列与证据渲染钉死，
// 防止再次退化。断言全部对着真实源码文本（与 panel-contract.test.ts 同风格）。
const here = dirname(fileURLToPath(import.meta.url));
const read = (p: string) => readFileSync(join(here, '..', p), 'utf8');

const tok = read('pages/TokPage.tsx');
const tokTypes = read('types/tok.ts');

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

const identityTable = region(tok, 'aria-label="词表身份鉴定结果"', '</table>');
const identityHead = region(identityTable, '<thead', '</thead>');
const identityBody = region(identityTable, '<tbody', '</tbody>');

describe('词表身份鉴定结果表（旧 renderTokIdTable 的 6 列）', () => {
  it('TestTokIdentityTableColumns: 必须是 6 列，列序 模型→站点→词表→权重→模板公式→样本明细', () => {
    const labels = thLabels(identityHead);
    // 旧面板 renderTokIdTable 的 thead 顺序（legacy 第 2724–2729 行）
    expect(labels).toEqual(['模型', '站点', '词表', '权重', '模板公式', '样本明细']);
    // 6 列是「四个判定 + 两个证据」；少一列就等于把证据合并进别的单元格
    expect((identityHead.match(/<th(?=[\s>])/g) || []).length).toBe(6);
    // 迁移退化版把这两列塞进了「备注」；表头不得再有「备注」列
    expect(labels).not.toContain('备注');
    // 每行 6 个 <td>；note 行整行铺满，colSpan 必须等于列数
    expect(identityTable).toContain('colSpan={6}');
  });

  it('TestTokIdentityRowRendersEvidence: 每行都要渲染模板明细与样本明细，不能只留徽标', () => {
    const row = region(identityBody, '{identityResults.map((r, idx) => {', '</tbody>');
    // 模板公式单元格：判定徽标 + 逐结构明细（模板不符时能直接看到差在哪）
    expect(row).toContain('templateBadge(templateState(r))');
    expect(row).toContain('templateDetail');
    expect(row).toContain('{tplDetail &&');
    // 样本明细单元格：name observed/official (+delta)
    expect(row).toContain('const evidence = sampleEvidence(r)');
    expect(row).toContain('{evidence ||');
    // 证据来自后端真实的两个数组字段，不是前端编的
    expect(tok).toMatch(/row\.templateDetail \|\| \[\]/);
    expect(tok).toMatch(/\(row\.samples \|\| \[\]\)/);
    // 与旧面板逐字同格式：`name observed/official (+delta)`，缺观测值写 ?
    expect(tok).toContain('`${s.name} ${observed}/${s.official}${delta}`');
    expect(tok).toContain("s.delta == null ? '' : ` (${s.delta >= 0 ? '+' : ''}${s.delta})`");
  });

  it('TestTokIdentityTemplateUnmeasured: 没跑过模板校验只能显示「未测」，不得显示「不符」', () => {
    // 后端 TemplateOK 是 bool（非指针）：没跑过时是零值 false。
    // 只有「官方 DeepSeek 词表」才代表模板校验真的执行过。
    const state = region(tok, '// 模板公式只有', 'return row.templateOK;');
    const guard = state.indexOf("'官方 DeepSeek 词表'");
    const readTOK = state.indexOf('return row.templateOK');
    expect(guard, '缺少「词表判定通过」前置条件').toBeGreaterThanOrEqual(0);
    expect(guard).toBeLessThan(readTOK);
    // 无法判定（校准失败）时探测没拿到结论 —— 注释与本文件的行高亮都点名这一档
    expect(state).toContain('无法判定');
    // 除 templateState() 外，页面不得直接拿 templateOK 当判定（那会绕过前置条件）
    expect(tok).not.toMatch(/\br\.templateOK/);
    // 三态文案齐全
    expect(tok).toContain('符合');
    expect(tok).toContain('未测');
    expect(tokTypes).toContain('templateOK?: boolean');
  });

  it('TestTokIdentityVerdictFallback: 校准失败不得显示「通过」，必须落到「无法判定 / 无法验证」', () => {
    expect(tok).toContain('无法判定');
    expect(tok).toContain('无法验证');
    // 词表徽标对未知值原样露出「无法判定」
    expect(tok).toMatch(/vocabulary \|\| '无法判定'/);
    // 权重列缺省时显式写「无法验证（上游不提供 logprobs）」
    expect(tok).toContain('无法验证（上游不提供 logprobs）');
    // 整体校准失败（含 HTTP 200 + ok:false）走错误分支，不渲染成「没有结果 = 正常」
    expect(tok).toContain('if (!data.ok) throw new Error(`校准失败（ok=false）');
    expect(tok).toContain('校准失败：{identityError}');
    // 无法判定的行必须能被一眼看出（告警底色 + 顶部提示），不是静默通过
    expect(tok).toContain("r.vocabulary === '无法判定'");
    expect(tok).toContain('个模型的词表判定为「无法判定」');
  });
});

describe('分词器指纹基线（分桶别名与成本口径）', () => {
  it('TestTokBucketAliases: cjk / digits / ascii 必须映射成中文名，contract 的 digit / alpha 也别丢', () => {
    const labels = region(tok, 'const BUCKET_LABEL', '};');
    const expected: Array<[string, string]> = [
      ['cjk', '中文'],
      ['digits', '数字'],
      ['ascii', '英文'],
      // 面板契约里写的别名（服务端实际可能发哪一套不确定，两套都认）
      ['digit', '数字'],
      ['alpha', '英文'],
    ];
    const missing = expected
      .filter(([key, name]) => !labels.includes(`${key}: '${name}'`))
      .map(([key]) => key);
    expect(missing).toEqual([]);
    // 漏映射时兜底不能把原始英文键当成可用结论
    expect(tok).toContain("BUCKET_LABEL[bucket as TokBucket] || bucket || '—'");
  });

  it('TestTokIdentityCostWarning: 校准成本与「只手动触发」必须写在界面上', () => {
    expect(tok).toMatch(/0\.02 credit/);
    expect(tok).toContain('title="会消耗约 0.02 credit（7 次请求，每次 max_tokens=1）"');
    expect(tok).toContain('/ 模型（3 条词表样本 + 4 种模板结构，每次只输出 1 token）');
    expect(tok).toContain('不设定时任务');
    expect(tok).toContain('校准中…（约 0.02 credit / 模型）');
  });
});

describe('分词器指纹的既有加固不得回退', () => {
  it('TestTokBaselineFailureHardening: ok:false + HTTP 200 当失败；基线没取到时开关禁用', () => {
    // 三条接口都必须在 ok:false 时抛错，而不是把空结果当成正常
    expect(tok).toContain("if (!data.ok) throw new Error('tok-baselines 返回 ok=false');");
    expect(tok).toContain('if (!data.ok) throw new Error(data.error');
    // 开关状态未知时禁用，避免盲切真实配置
    expect(tok).toContain('disabled={toggling || !enabledKnown}');
    expect(tok).toContain('开关状态未知，已禁用开关以免盲切');
    // 加载失败必须显示错误，不能留一张看起来「确实没有基线」的空表
    expect(tok).toContain('基线加载失败：{baselineError}');
  });

  it('TestTokIdentityNoteVisible: 每行 note 必须可见（长文案不能藏进 tooltip）', () => {
    const row = region(identityBody, '{identityResults.map((r, idx) => {', '</tbody>');
    expect(row).toContain('{r.note && (');
    // 备注行用独立一整行铺满 6 列，避免把长文案挤进任何单元格
    expect(row).toContain('<span className="text-[var(--fg-3)]">备注 </span>');
  });
});
