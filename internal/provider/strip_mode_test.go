package provider

import (
	"strings"
	"testing"
)

// 本文件验证 strip 档在「消息规整」层面的行为，覆盖三种档位的差异。
//
// 三档语义（改动后仍必须成立）：
//   keep  : system 原样，剥都不剥
//   fold  : system 折叠进 user 的 <client_instructions>
//   strip : system 原位保留，只删掉开头的身份句
//
// 关键边界：剥空的 system 必须**保留为空消息**，不能丢弃。
// 丢弃会让 global 站触发「首条不是 system → 补 canonical」的兜底注入，
// 等于网关偷偷改了客户端语义。

func TestStripModeRemovesIdentityKeepsRest(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": testDSHSentence + "\n\nYou have access to tools."},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	report := &StripReport{}

	out := EnsureUpstreamMessagesWith(in, true, cfg, report)

	if len(out) != 2 {
		t.Fatalf("应保留 2 条消息，实际 %d", len(out))
	}
	if out[0]["role"] != "system" {
		t.Fatalf("首条仍应是 system，实际 %v", out[0]["role"])
	}
	got := out[0]["content"].(string)
	if got != "You have access to tools." {
		t.Fatalf("应只删身份句，实际 %q", got)
	}
	if !report.Stripped {
		t.Fatal("report 应标记已剥离")
	}
	if report.Bytes != len(testDSHSentence)+2 {
		t.Fatalf("删除字节数应为 %d，实际 %d", len(testDSHSentence)+2, report.Bytes)
	}
}

// TestStripModeKeepsEmptySystemMessage 是最重要的一条边界：
// 剥空的 system 必须保留（content 为空串），不能整条丢弃。
func TestStripModeKeepsEmptySystemMessage(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": testDSHSentence},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	out := EnsureUpstreamMessagesWith(in, true, cfg, &StripReport{})

	if len(out) != 2 {
		t.Fatalf("剥空的 system 不应被丢弃，应保留 2 条，实际 %d: %v", len(out), out)
	}
	if out[0]["role"] != "system" {
		t.Fatalf("首条应是空 system，实际 %v", out[0]["role"])
	}
	if got, _ := out[0]["content"].(string); got != "" {
		t.Fatalf("content 应为空串，实际 %q", got)
	}
}

// TestStripModeLeavesOtherModesUntouched 校验 keep 档不做剥离。
func TestStripModeLeavesOtherModesUntouched(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": testClaudeSentence},
		{"role": "user", "content": "hi"},
	}
	// keep 档：cfg 为 nil
	out := EnsureUpstreamMessagesWith(in, true, nil, nil)
	if out[0]["content"] != testClaudeSentence {
		t.Fatalf("keep 档不得改动 system，实际 %q", out[0]["content"])
	}
	// strip 配置存在但 Enabled=false 时也不得剥离
	out2 := EnsureUpstreamMessagesWith(in, true, &StripConfig{Enabled: false, Rules: RulesForSite("domestic")}, nil)
	if out2[0]["content"] != testClaudeSentence {
		t.Fatalf("Enabled=false 时不得剥离，实际 %q", out2[0]["content"])
	}
}

// TestStripModeOnlyTouchesSystem 校验 user/assistant 里的同一句不动。
func TestStripModeOnlyTouchesSystem(t *testing.T) {
	in := []map[string]any{
		{"role": "user", "content": testClaudeSentence},
		{"role": "assistant", "content": testClaudeSentence},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	report := &StripReport{}
	out := EnsureUpstreamMessagesWith(in, true, cfg, report)
	if report.Stripped {
		t.Fatal("user/assistant 不应被剥离")
	}
	if out[0]["content"] != testClaudeSentence || out[1]["content"] != testClaudeSentence {
		t.Fatal("user/assistant 内容不得改动")
	}
}

// TestStripModeHandlesMultipleSystems 校验任意一条 system 命中都会被剥。
func TestStripModeHandlesMultipleSystems(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": "Always be concise."},
		{"role": "system", "content": testDSHSentence},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	out := EnsureUpstreamMessagesWith(in, true, cfg, &StripReport{})

	if len(out) != 3 {
		t.Fatalf("应保留 3 条，实际 %d", len(out))
	}
	if out[0]["content"] != "Always be concise." {
		t.Fatalf("未命中的 system 不得改动，实际 %q", out[0]["content"])
	}
	if got, _ := out[1]["content"].(string); got != "" {
		t.Fatalf("命中的 system 应被剥空，实际 %q", got)
	}
}

// TestStripModeDeveloperRole 校验 developer 被归一成 system 后同样剥离。
func TestStripModeDeveloperRole(t *testing.T) {
	in := []map[string]any{
		{"role": "developer", "content": testDSHSentence},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	out := EnsureUpstreamMessagesWith(in, true, cfg, &StripReport{})
	if out[0]["role"] != "system" {
		t.Fatalf("developer 应归一为 system，实际 %v", out[0]["role"])
	}
	if got, _ := out[0]["content"].(string); got != "" {
		t.Fatalf("developer 内容也应被剥离，实际 %q", got)
	}
}

// TestStripModeStructuredContent 校验数组形态只动 text part，图片不动。
func TestStripModeStructuredContent(t *testing.T) {
	imgPart := map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": "data:image/png;base64,AAA"},
	}
	in := []map[string]any{
		{
			"role": "system",
			"content": []any{
				map[string]any{"type": "text", "text": testDSHSentence},
				imgPart,
			},
		},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	out := EnsureUpstreamMessagesWith(in, true, cfg, &StripReport{})

	parts, ok := out[0]["content"].([]any)
	if !ok {
		t.Fatalf("结构化 content 应保持数组形态，实际 %T", out[0]["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("part 数应不变（2），实际 %d", len(parts))
	}
	textPart := parts[0].(map[string]any)
	if got, _ := textPart["text"].(string); got != "" {
		t.Fatalf("text part 应被剥空，实际 %q", got)
	}
	if parts[1].(map[string]any)["type"] != "image_url" {
		t.Fatal("图片 part 不得改动")
	}
}

// TestStripModeSiteRouting 校验站点分流在规整层也成立。
func TestStripModeSiteRouting(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": testDSHSentence},
		{"role": "user", "content": "hi"},
	}
	// global 站不该剥 DeepSeek 句
	cfgGlobal := &StripConfig{Enabled: true, Rules: RulesForSite("global")}
	reportGlobal := &StripReport{}
	outGlobal := EnsureUpstreamMessagesWith(in, true, cfgGlobal, reportGlobal)
	if reportGlobal.Stripped {
		t.Fatal("global 站不应剥 DeepSeek 身份句")
	}
	if outGlobal[0]["content"] != testDSHSentence {
		t.Fatalf("global 站内容不得改动，实际 %q", outGlobal[0]["content"])
	}

	// domestic 站该剥
	cfgDom := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	reportDom := &StripReport{}
	EnsureUpstreamMessagesWith(in, true, cfgDom, reportDom)
	if !reportDom.Stripped {
		t.Fatal("domestic 站应剥 DeepSeek 身份句")
	}
}

// TestStripModeIdempotentAcrossCalls 校验同一输入多次规整得到同样结果
// （幂等性保证会话键与上游 cache 稳定）。
func TestStripModeIdempotentAcrossCalls(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": testClaudeSentence + "\n\nrules"},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	a := EnsureUpstreamMessagesWith(in, true, cfg, &StripReport{})
	b := EnsureUpstreamMessagesWith(in, true, cfg, &StripReport{})
	if a[0]["content"] != b[0]["content"] {
		t.Fatalf("幂等性破坏：%q != %q", a[0]["content"], b[0]["content"])
	}
}

// TestStripModeDoesNotMutateInput 校验不修改调用方传入的原始 messages
// （sessionpin 用原始内容算会话键，若被就地改写会导致键漂移）。
func TestStripModeDoesNotMutateInput(t *testing.T) {
	original := testDSHSentence + "\n\nrules"
	in := []map[string]any{
		{"role": "system", "content": original},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	EnsureUpstreamMessagesWith(in, true, cfg, &StripReport{})
	if in[0]["content"] != original {
		t.Fatalf("不得就地改写调用方入参，实际 %q", in[0]["content"])
	}
}

// TestStripModeDoesNotMutateStructuredInput 校验多模态结构化 []any（text + image）
// 场景下绝不原地修改调用方切片或底层 part map。
func TestStripModeDoesNotMutateStructuredInput(t *testing.T) {
	originalText := testDSHSentence + "\n\nrules"
	textPart := map[string]any{"type": "text", "text": originalText}
	imgPart := map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAA"}}
	parts := []any{textPart, imgPart}
	in := []map[string]any{
		{"role": "system", "content": parts},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
	report := &StripReport{}
	out := EnsureUpstreamMessagesWith(in, true, cfg, report)
	if !report.Stripped {
		t.Fatal("结构化 system 应命中剥离")
	}
	// 原入参 map 与切片必须原封不动
	if textPart["text"] != originalText {
		t.Fatalf("不得原地修改入参 textPart，实际 %q", textPart["text"])
	}
	if origSlice, ok := in[0]["content"].([]any); !ok || origSlice[0].(map[string]any)["text"] != originalText {
		t.Fatalf("不得原地修改入参 []any 切片，实际 %#v", in[0]["content"])
	}
	// 返回的副本应已剥离且保留图片 part
	outParts, ok := out[0]["content"].([]any)
	if !ok || len(outParts) != 2 {
		t.Fatalf("返回结构异常：%#v", out[0]["content"])
	}
	if outParts[0].(map[string]any)["text"] != "rules" {
		t.Fatalf("剥离后文本应为 rules，实际 %q", outParts[0].(map[string]any)["text"])
	}
	if outParts[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("非文本 part 应保留：%#v", outParts[1])
	}
}

// TestStripModeReportBytesMatchesActualRemoval 校验报告的字节数与实际差值一致。
func TestStripModeReportBytesMatchesActualRemoval(t *testing.T) {
	cases := []string{
		testDSHSentence,
		testDSHSentence + "\n\nrest",
		testClaudeSentence + "\nrest",
	}
	for _, content := range cases {
		in := []map[string]any{
			{"role": "system", "content": content},
			{"role": "user", "content": "hi"},
		}
		cfg := &StripConfig{Enabled: true, Rules: RulesForSite("domestic")}
		report := &StripReport{}
		out := EnsureUpstreamMessagesWith(in, true, cfg, report)
		got, _ := out[0]["content"].(string)
		if report.Bytes != len(content)-len(got) {
			t.Fatalf("报告字节数 %d != 实际差值 %d (输入 %q)", report.Bytes, len(content)-len(got), content)
		}
	}
}

// TestStripModeEmptyRulesDisables 校验空规则不会误伤（防御性）。
func TestStripModeEmptyRulesDisables(t *testing.T) {
	in := []map[string]any{
		{"role": "system", "content": testClaudeSentence},
		{"role": "user", "content": "hi"},
	}
	cfg := &StripConfig{Enabled: true, Rules: nil}
	report := &StripReport{}
	out := EnsureUpstreamMessagesWith(in, true, cfg, report)
	if report.Stripped {
		t.Fatal("空规则时不得剥离")
	}
	if !strings.Contains(out[0]["content"].(string), "Claude Code") {
		t.Fatal("内容不得改动")
	}
}
