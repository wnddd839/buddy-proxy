package provider

import "testing"

// StripReport 必须能区分「删在 system」与「只删 assistant」。
// 这不是锦上添花：排查「我发的 system 有没有被改」时，只靠 Stripped=true
// 无法回答 —— 而这两件事对输出质量的含义完全不同。
func TestStripReportSeparatesSystemFromAssistant(t *testing.T) {
	cfg := &StripConfig{
		Enabled:  true,
		Rules:    RulesForSite("domestic"),
		Contains: ContainsForSite("domestic"),
	}
	const rule = "Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root"

	// 只有 assistant 含指纹：system 必须原样，计数字节只能落在 assistant 侧。
	report := &StripReport{}
	out := EnsureUpstreamMessagesWith([]map[string]any{
		{"role": "system", "content": "You are a helpful coding assistant."},
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "Sure. " + rule},
		{"role": "user", "content": "again"},
	}, true, cfg, report)

	if !report.Stripped {
		t.Fatal("应标记发生过剥离")
	}
	if report.SystemBytes != 0 {
		t.Fatalf("system 未改动，SystemBytes 应为 0，实际 %d", report.SystemBytes)
	}
	if report.AssistantBytes == 0 {
		t.Fatal("assistant 侧应记录字节")
	}
	if got := report.Scope(); got != "assistant" {
		t.Fatalf("Scope 应为 assistant，实际 %q", got)
	}
	if out[0]["content"] != "You are a helpful coding assistant." {
		t.Fatalf("system 内容不得改动：%v", out[0]["content"])
	}

	// system 含指纹：字节必须落在 system 侧。
	report2 := &StripReport{}
	EnsureUpstreamMessagesWith([]map[string]any{
		{"role": "system", "content": rule},
		{"role": "user", "content": "hi"},
	}, true, cfg, report2)
	if report2.SystemBytes == 0 {
		t.Fatal("system 命中时 SystemBytes 应大于 0")
	}
	if report2.AssistantBytes != 0 {
		t.Fatalf("没有 assistant，AssistantBytes 应为 0，实际 %d", report2.AssistantBytes)
	}
	if got := report2.Scope(); got != "system" {
		t.Fatalf("Scope 应为 system，实际 %q", got)
	}

	// 两侧都有：Scope=both。
	report3 := &StripReport{}
	EnsureUpstreamMessagesWith([]map[string]any{
		{"role": "system", "content": rule},
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": rule},
		{"role": "user", "content": "again"},
	}, true, cfg, report3)
	if got := report3.Scope(); got != "both" {
		t.Fatalf("Scope 应为 both，实际 %q", got)
	}
	if report3.SystemBytes == 0 || report3.AssistantBytes == 0 {
		t.Fatalf("两侧都应记录：sys=%d asst=%d", report3.SystemBytes, report3.AssistantBytes)
	}

	// 没命中：Scope 为空串，不能是 "system"。
	report4 := &StripReport{}
	EnsureUpstreamMessagesWith([]map[string]any{
		{"role": "system", "content": "plain"},
		{"role": "user", "content": "hi"},
	}, true, cfg, report4)
	if got := report4.Scope(); got != "" {
		t.Fatalf("未剥离时 Scope 应为空，实际 %q", got)
	}
}
