package admin

import (
	"strings"
	"testing"
)

// execAdminScript 沿用现有 helper：直接跑页面里的真实函数，避免测试副本与实现分叉。
// 这里锁住 4 项展示修复的行为与文案。

// ---------------------------------------------------------------------------
//  1. 修真实误报：只有 recovered:true + mode:'auto' + 2xx 才显示「降级成功」。
//     fold 模式下每一次正常请求（form=fold/status=200/mode=fold）必须显示 OK。
//
// ---------------------------------------------------------------------------
func TestSpResultLabelRecoveredGating(t *testing.T) {
	body := `
// fold 模式正常请求：绝不能误标「降级成功」。
const foldOk = spResultLabel({form:'fold', status:200, mode:'fold', recovered:false});
assert.match(foldOk, /OK/, 'fold 模式正常请求应显示 OK，实际: ' + foldOk);
assert.ok(!foldOk.includes('降级成功'), 'fold 模式正常请求不得显示「降级成功」，实际: ' + foldOk);
// 降级重放成功：recovered:true + auto + 2xx -> 降级成功。
const replay = spResultLabel({form:'fold', status:200, mode:'auto', recovered:true});
assert.match(replay, /降级成功/, '降级重放成功应显示「降级成功」，实际: ' + replay);
// 被拦截的失败事件：显示 11128，不得含「降级成功」。
const blocked = spResultLabel({degraded:true, status:400, errorCode:'11128', form:'keep', mode:'auto'});
assert.match(blocked, /被拦截 · 11128/, '被拦截事件应显示「被拦截 · 11128」，实际: ' + blocked);
assert.ok(!blocked.includes('降级成功'), '被拦截事件不得含「降级成功」，实际: ' + blocked);
// 传输失败。
const transport = spResultLabel({status:0});
assert.match(transport, /传输失败/, 'status:0 应显示「传输失败」，实际: ' + transport);
// 后端字段缺失的保险：仅有 recovered 但 mode 不是 auto（异常数据）也不得误报。
const stray = spResultLabel({form:'fold', status:200, mode:'fold', recovered:true});
assert.ok(!stray.includes('降级成功'), 'mode 非 auto 时不得误报「降级成功」，实际: ' + stray);
`
	execAdminScript(t, "function spResultLabel(", "\nfunction spRowsHtml(", "", body)
}

// ---------------------------------------------------------------------------
// 2. 一次降级的两条事件关联展示：spRowsHtml 的形态徽标与行 class。
// ---------------------------------------------------------------------------
func TestSpRowsHtmlReplayCorrelation(t *testing.T) {
	prelude := `
const formatTime = (x) => String(x == null ? '—' : x);
const shortId = (x) => String(x).slice(0, 8);
`
	body := `
const rows = [
  {requestId:'REQ-A', degraded:true,  status:400, errorCode:'11128', form:'keep', mode:'auto', at:'t1'},
  {requestId:'REQ-A', degraded:false, status:200, recovered:true,   form:'fold', mode:'auto', at:'t2'},
  {requestId:'REQ-B', degraded:false, status:200, recovered:false,  form:'fold', mode:'fold', at:'t3'},
];
const html = spRowsHtml(rows, 'empty');
const trs = html.split('<tr').slice(1); // 3 行
assert.equal(trs.length, 3, '应渲染 3 行，实际 ' + trs.length);
// 被拦截行：形态含「被拦截」，且是降级关联行。
assert.match(trs[0], /keep\(被拦截\)/, '被拦截行形态徽标应含「被拦截」，实际: ' + trs[0]);
assert.ok(trs[0].includes('sp-replay-row'), '被拦截行应带 sp-replay-row class');
// 重放成功行：形态含「降级重放」，带行 class 与 title 说明。
assert.match(trs[1], /fold\(降级重放\)/, '重放成功行形态徽标应含「降级重放」，实际: ' + trs[1]);
assert.ok(trs[1].includes('sp-replay-row'), '重放成功行应带 sp-replay-row class');
assert.match(trs[1], /同 requestId 的被拦截事件是同一次请求/, '重放成功行结果列应有同请求说明 title');
assert.match(trs[1], /降级成功/, '重放成功行结果应为「降级成功」');
// 普通 fold/200/mode:fold 行：不含「降级重放」，不带关联 class，结果为 OK。
assert.ok(!trs[2].includes('降级重放'), '普通 fold 行不得含「降级重放」，实际: ' + trs[2]);
assert.ok(!trs[2].includes('sp-replay-row'), '普通 fold 行不得带关联 class');
assert.ok(!trs[2].includes('降级成功'), '普通 fold 行不得显示「降级成功」');
assert.match(trs[2], /OK/, '普通 fold 行结果应为 OK');
`
	execAdminScript(t, "function spResultLabel(", "\nfunction paintSpMode(", prelude, body)
}

// ---------------------------------------------------------------------------
// 3. 指标卡口径说明 + 新卡片 + refreshSystemPrompt 读取 statsScope。
// ---------------------------------------------------------------------------
func TestSystemPromptMetricsScopeAndRecovered(t *testing.T) {
	page := PageHTML()
	for _, want := range []string{
		`id="spStatsScope"`,
		`id="spTotalLabel"`,
		"今日事件",
		"降级重放成功",
		`id="spRecovered"`,
		"指标口径：优先按今天落盘事件统计（重启不清零）",
		`statsScope="memory"`,
		"降级/被拦截次数",
		"degraded:true 的事件数",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
	// refreshSystemPrompt 必须读取 data.statsScope 并动态更新口径文字。
	start := strings.Index(page, "async function refreshSystemPrompt(")
	if start < 0 {
		t.Fatal("缺少 refreshSystemPrompt")
	}
	rel := strings.Index(page[start:], "\nasync function refreshSpHistory(")
	if rel < 0 {
		t.Fatal("缺少 refreshSpHistory 结束标记")
	}
	fn := page[start : start+rel]
	for _, want := range []string{
		"data.statsScope",
		"spStatsScope",
		"spTotalLabel",
		"stats.recovered",
		"spRecovered",
	} {
		if !strings.Contains(fn, want) {
			t.Fatalf("refreshSystemPrompt 缺少 %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. 「只看问题」/「形态」列/面板顶部说明文案（字符串锁）。
// ---------------------------------------------------------------------------
func TestSystemPromptDisplayHints(t *testing.T) {
	page := PageHTML()
	for _, want := range []string{
		"含传输失败、被拦截、降级、其它错误",
		"keep=直传 system；fold=改写 system",
		"带(降级重放)/(被拦截)的是同一次 11128 降级请求的两条事件",
		"auto 降级会产生两条事件",
		"用同一请求 ID 关联",
		"别当成两个请求",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("页面缺少说明文案 %q", want)
		}
	}
	// 两个「形态」表头都要带 title。
	if n := strings.Count(page, `title="keep=直传 system`); n < 2 {
		t.Fatalf("实时/历史两个形态表头都应有 title，实际 %d", n)
	}
}

// ---------------------------------------------------------------------------
// 新增 CSS 用独立 class，不改全局规则。
// ---------------------------------------------------------------------------
func TestSpReplayRowCssIsIndependent(t *testing.T) {
	page := PageHTML()
	if !strings.Contains(page, ".log-table tr.sp-replay-row td{border-left:2px solid var(--fg-20)}") {
		t.Fatal("缺少 sp-replay-row 独立 CSS 规则")
	}
}
