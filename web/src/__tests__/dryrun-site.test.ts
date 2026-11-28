import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// 「试算站点」契约（源码级）
//
// 背景：生产的剥离规则按**选中账号的站点**（gateway.applyStripConfig）生效，
// 而试算曾固定用全局 cfg.Site —— 混站号池里会预览另一个站点的规则。
// 后端加了可选 site 字段后，前端必须做到：
//   1. 「跟随当前」= 请求体里**没有** site 字段（服务端才会读实时配置），
//      绝不能发空串（空串是不合法值，会被 400 拒掉）；
//   2. 站点进「结果过期」签名，切站点后旧结果必须标过期；
//   3. 结果区显示实际生效的站点，并标注是「跟随当前」还是「本次指定」。
//
// 实现语言是 TSX，字符串断言只能读源码文本 —— 与 panel-contract.test.ts
// 同一套做法。
const here = dirname(fileURLToPath(import.meta.url));
const src = (p: string) => readFileSync(join(here, '..', p), 'utf8');

const page = src('pages/SystemPromptPage.tsx');
const apiTypes = src('types/api.ts');

/** 取 text 中 start..end（含端点）的片段；找不到即测试失败。 */
function region(text: string, start: string, end: string): string {
  const i = text.indexOf(start);
  expect(i, `找不到起始标记 ${start}`).toBeGreaterThanOrEqual(0);
  const j = text.indexOf(end, i + start.length);
  expect(j, `找不到结束标记 ${end}`).toBeGreaterThanOrEqual(0);
  return text.slice(i, j + end.length);
}

const handleDryRun = region(
  page,
  'const handleDryRun = async () => {',
  '\n  const dateList =',
);

describe('system 试算站点', () => {
  it('「跟随当前」不发送 site 字段，更不发空串', () => {
    // 唯一允许的写法：只有非空才写进请求体。
    expect(handleDryRun).toContain("if (drySite !== '') body.site = drySite;");
    expect(handleDryRun).not.toContain("body.site = ''");
    expect(handleDryRun).not.toMatch(/body\.site\s*=\s*['"`]\s*['"`]/);

    // 请求体字面量本身不带 site —— 否则「跟随当前」会变成显式 ""。
    const bodyLiteral = region(
      handleDryRun,
      'const body: SystemDryRunRequest',
      'mode: dryMode }',
    );
    expect(bodyLiteral).not.toMatch(/\bsite\b/);
  });

  it('显式站点写进请求体', () => {
    expect(handleDryRun).toMatch(/if \(drySite !== ''\) body\.site = drySite;/);
    expect(page).toContain(
      "const [drySite, setDrySite] = useState<'' | 'domestic' | 'global'>('');",
    );
  });

  it('站点进了「结果过期」签名（请求时与比对时都要有）', () => {
    // 请求成功后写入的签名（单行字面量）：必须含 site。
    expect(handleDryRun).toContain(
      'JSON.stringify({ system, mode: dryMode, strip: dryStrip, echo: dryEcho, site: drySite })',
    );

    // 渲染时比对的签名（多行对象）：同样必须含 site。
    const staleCheck = region(page, 'const dryStale', 'const renderDryToggle');
    expect(staleCheck).toContain('site: drySite');

    // 两处签名必须都带 site，否则切站点不会标过期。
    expect(page.match(/site: drySite/g) ?? []).toHaveLength(2);
  });

  it('结果区显示实际生效站点，跟随时标注「跟随当前」', () => {
    const badge = region(page, '{dryResult.site && (', '</StatusBadge>');
    expect(badge).toContain('siteLabel(dryResult.site)');
    expect(badge).toContain('（跟随当前）');
    expect(badge).toContain('dryRanSite');
  });

  it('站点选择器有三个选项：跟随当前 / 国内站 / 国际站', () => {
    const selector = region(page, 'id="drySiteSeg"', ']}');
    expect(selector).toContain('ariaLabel="试算站点"');
    expect(selector).toContain("value: ''");
    expect(selector).toContain("label: '跟随当前'");
    expect(selector).toContain("value: 'domestic'");
    expect(selector).toContain("label: '国内站'");
    expect(selector).toContain("value: 'global'");
    expect(selector).toContain("label: '国际站'");
  });

  it('请求/响应类型都接受 site', () => {
    const req = region(apiTypes, 'export interface SystemDryRunRequest {', '\n}');
    expect(req).toContain("site?: 'domestic' | 'global' | string;");
    const resp = region(apiTypes, 'export interface SystemDryRunResponse {', '\n}');
    expect(resp).toContain("site?: 'domestic' | 'global' | string;");
  });
});
