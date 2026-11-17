package admin

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// execAdminScript 抽取页面内的一段 JS，拼上 assert 前言后交给 node -e 执行。
// 沿用 expiry_test.go 的思路：直接跑页面里的真实函数，避免测试副本与实现分叉。
// env 用于给 Date 相关测试固定时区。
func execAdminScript(t *testing.T, startMarker, endMarker, prelude, body string, env ...string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("可选的管理台脚本测试需要 Node.js")
	}
	page := PageHTML()
	start := strings.Index(page, startMarker)
	if start < 0 {
		t.Fatalf("脚本缺少 %q", startMarker)
	}
	end := strings.Index(page[start:], endMarker)
	if end < 0 {
		t.Fatalf("脚本缺少结束标记 %q", endMarker)
	}
	script := `const assert = require('node:assert/strict');
const escapeHtml = s => String(s).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;');
` + prelude + "\n" + page[start:start+end] + "\n" + body
	cmd := exec.CommandContext(t.Context(), node, "-e", script)
	if len(env) > 0 {
		base := make([]string, 0, len(os.Environ()))
		for _, kv := range os.Environ() {
			key := kv
			if i := strings.IndexByte(kv, '='); i >= 0 {
				key = kv[:i]
			}
			duplicate := false
			for _, e := range env {
				if strings.HasPrefix(e, key+"=") {
					duplicate = true
					break
				}
			}
			if !duplicate {
				base = append(base, kv)
			}
		}
		cmd.Env = append(base, env...)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("管理台脚本测试失败: %v\n%s", err, output)
	}
}

// 回归：auto 模式降级产生两条事件 ——
//
//	{degraded:true, status:400, errorCode:'11128', form:'keep'}  ← 被拦截的失败尝试
//	{degraded:false, status:200, form:'fold'}                     ← 折叠后重放成功
//
// 失败事件绝不能标「降级成功」；「降级成功」只由 recovered:true + mode:'auto' 判定，
// fold 模式下的普通 fold/200 正常请求必须显示 OK。
func TestSpResultLabelDegradedFailureIsNotSuccess(t *testing.T) {
	execAdminScript(t, "function spResultLabel(", "\nfunction spRowsHtml(", "", `
const fail = spResultLabel({degraded:true, status:400, errorCode:'11128', form:'keep'});
assert.ok(!fail.includes('降级成功'), '失败事件不得显示「降级成功」，实际: ' + fail);
assert.match(fail, /被拦截 · 11128/);
// 折叠后重放成功（recovered:true、mode:auto、200）才标「降级成功」。
const recovered = spResultLabel({degraded:false, status:200, form:'fold', recovered:true, mode:'auto'});
assert.match(recovered, /降级成功/);
// 普通 keep 成功仍是 OK，不能冒充降级成功。
const ok = spResultLabel({degraded:false, status:200, form:'keep'});
assert.match(ok, /OK/);
assert.ok(!ok.includes('降级成功'));
// 其它失败/异常事件也不得声称降级成功。
for (const e of [
  {degraded:true, status:0, errorCode:'read: connection reset'},
  {degraded:true, status:502, errorCode:''},
  {degraded:true, status:200, errorCode:'some_error'},
]) {
  assert.ok(!spResultLabel(e).includes('降级成功'), '失败事件不得显示「降级成功」: ' + JSON.stringify(e));
}
`)
}

// 锁死判断顺序：status / errorCode 必须排在其它判断之前，
// 否则成功标签会抢先命中，把 400/11128 的失败事件误标成降级成功。
func TestSpResultLabelChecksStatusBeforeDegraded(t *testing.T) {
	page := PageHTML()
	start := strings.Index(page, "function spResultLabel(")
	if start < 0 {
		t.Fatal("脚本缺少 spResultLabel")
	}
	end := strings.Index(page[start:], "\nfunction spRowsHtml(")
	if end < 0 {
		t.Fatal("脚本缺少 spRowsHtml")
	}
	body := page[start : start+end]
	// 只认 return 语句里的「降级成功」标签，别被注释里的同名文字干扰。
	returnLabel := strings.Index(body, "return '<span class=\"badge warn\"")
	if returnLabel < 0 {
		t.Fatal("spResultLabel 缺少「降级成功」的 return 分支")
	}
	for _, want := range []string{
		"e.status >= 400",
		"e.errorCode === '11128'",
		"e.status === 0",
	} {
		i := strings.Index(body, want)
		if i < 0 {
			t.Fatalf("spResultLabel 缺少 %q 判断", want)
		}
		if i > returnLabel {
			t.Fatalf("%q 判断必须出现在「降级成功」return 分支之前", want)
		}
	}
}

// 回归：首次进入 tab（openChapterHash 与 tab 按钮 click）都应加载历史，
// 且必须在 refreshSystemPrompt() 成功后再调 refreshSpHistory()（后者依赖 spHistory.date）。
func TestSystemPromptTabEntriesLoadHistory(t *testing.T) {
	page := PageHTML()
	chain := "refreshSystemPrompt().then(function(){ return refreshSpHistory(); })"
	if n := strings.Count(page, chain); n < 3 {
		t.Fatalf("两处 tab 入口与刷新按钮都应串联加载历史，实际串联次数 %d", n)
	}
	entries := []struct{ start, end string }{
		{"function openChapterHash(", "document.querySelectorAll('.tab-link').forEach(function(btn)"},
		{"document.querySelectorAll('.tab-link').forEach(function(btn)", "window.addEventListener('hashchange'"},
	}
	for _, e := range entries {
		s := strings.Index(page, e.start)
		if s < 0 {
			t.Fatalf("缺少入口 %q", e.start)
		}
		rel := strings.Index(page[s:], e.end)
		if rel < 0 {
			t.Fatalf("缺少入口结束标记 %q", e.end)
		}
		if !strings.Contains(page[s:s+rel], chain) {
			t.Fatalf("入口 %q 未在 refreshSystemPrompt 后串联 refreshSpHistory", e.start)
		}
	}
}

// 回归：无事件文件时日期兜底必须用本地日期（后端 eventDate 用 time.Local），
// 不能用 toISOString()（UTC）——本地 00:00–08:00 会算出「昨天」。
func TestFillSpDatesUsesLocalDate(t *testing.T) {
	body := `
const RealDate = Date;
class FakeDate extends RealDate {
  constructor(...args){ if (!args.length) super(2026,0,1,3,0,0); else super(...args); }
  static now(){ return new RealDate(2026,0,1,3,0,0).getTime(); }
}
const spDateNode = {innerHTML:'', value:''};
const $ = (id) => spDateNode;
let spHistory = {date:''};
global.Date = FakeDate;
fillSpDates([]);
assert.match(spDateNode.innerHTML, /value="20260101"/, '空列表兜底应为本地日期 20260101，实际: ' + spDateNode.innerHTML);
assert.ok(!spDateNode.innerHTML.includes('20251231'), '不得使用 UTC 日期（昨天），实际: ' + spDateNode.innerHTML);
`
	execAdminScript(t, "function fillSpDates(", "\nasync function refreshSystemPrompt(", "", body, "TZ=Asia/Shanghai")
}

func TestSystemPromptTabRendered(t *testing.T) {
	page := PageHTML()
	for _, want := range []string{
		`data-tab="tab-system-prompt"`,
		`id="tab-system-prompt"`,
		"System Prompt",
		`id="spModeSeg"`,
		`id="spRealtimeRows"`,
		`id="spHistoryRows"`,
		`id="spDateSelect"`,
		`data-mode="auto"`,
		`data-mode="keep"`,
		`data-mode="fold"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
}

func TestUsageTabIndexShiftedTo07(t *testing.T) {
	page := PageHTML()
	if !strings.Contains(page, `07 /</span> 用量与明细`) {
		t.Fatal("用量 tab 应顺延为 07")
	}
	if !strings.Contains(page, `06 /</span> System Prompt`) {
		t.Fatal("06 / 应分配给 System Prompt")
	}
}

func TestSystemPromptTabIsTopLevel(t *testing.T) {
	page := PageHTML()
	models := strings.Index(page, `id="tab-models"`)
	sp := strings.Index(page, `id="tab-system-prompt"`)
	usage := strings.Index(page, `id="tab-usage"`)
	if models < 0 || sp < 0 || usage < 0 {
		t.Fatalf("panel 缺失 models=%d sp=%d usage=%d", models, sp, usage)
	}
	if !(models < sp && sp < usage) {
		t.Fatalf("顺序应为 models < system-prompt < usage，实际 %d/%d/%d", models, sp, usage)
	}
}

func TestSystemPromptTabScriptWiring(t *testing.T) {
	page := PageHTML()
	for _, want := range []string{
		"function refreshSystemPrompt(",
		"function spSwitchMode(",
		"function refreshSpHistory(",
		"function spResultLabel(",
		"/direct-admin/api/system-prompt/events",
		"/direct-admin/api/system-prompt",
		"visibilityState",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("脚本缺少 %q", want)
		}
	}
}

func TestUsageTableHasSystemColumn(t *testing.T) {
	page := PageHTML()
	if !strings.Contains(page, "<th>System</th>") {
		t.Fatal("用量明细表应新增 System 列")
	}
	// 用量表（07）应为 12 列：12 个 <th> 对应 colspan="12" 空态。
	// 注意 06 事件表现在是 11 列（原 10 列 + 客户端），所以不能再用
	// 「页面里不得出现 colspan=11」这种全局断言 —— 会误伤 06。
	if !strings.Contains(page, `colspan="12"`) {
		t.Fatal("用量表空态应为 colspan=12（12 列）")
	}
	if !strings.Contains(page, "systemForm") {
		t.Fatal("用量表应渲染 systemForm")
	}
}

func TestSystemPromptModeDescriptionsAreConcrete(t *testing.T) {
	page := PageHTML()
	if strings.Contains(page, "原样") {
		t.Fatal("模式说明不得再使用含糊的「原样」")
	}
	for _, want := range []string{
		"先直传，被拒再改写",
		"直传 system",
		"改写 system",
		"只决定",
		"role: system",
		"11128",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("模式说明缺少 %q", want)
		}
	}
	for _, want := range []string{`data-mode="auto"`, `data-mode="keep"`, `data-mode="fold"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("缺少技术名 %q", want)
		}
	}
}
