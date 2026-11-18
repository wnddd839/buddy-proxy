package provider

import (
	"strings"
	"testing"
)

// assistant 剥离是可选开关：SkipAssistant=true 时只剥 system，
// assistant 历史必须原样保留（这是「不动模型历史」的那一档）。
func TestStripSkipAssistantLeavesAssistantUntouched(t *testing.T) {
	const rule = "Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root"
	in := []map[string]any{
		{"role": "system", "content": rule},
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "Sure. " + rule},
		{"role": "user", "content": "again"},
	}
	cfg := &StripConfig{
		Enabled:       true,
		Rules:         RulesForSite("domestic"),
		Contains:      ContainsForSite("domestic"),
		SkipAssistant: true,
	}
	report := &StripReport{}
	out := EnsureUpstreamMessagesWith(in, true, cfg, report)

	// system 仍照剥。
	sys, _ := out[0]["content"].(string)
	if strings.Contains(sys, "Tokens prefixed with @") {
		t.Fatalf("system 应仍被剥离：%q", sys)
	}
	// assistant 原样保留 —— 这正是关掉该开关的意图。
	asst, _ := out[2]["content"].(string)
	if !strings.Contains(asst, rule) {
		t.Fatalf("SkipAssistant 时 assistant 不得改动：%q", asst)
	}
	if !report.Stripped {
		t.Fatal("system 剥过了，report 应标记 stripped")
	}
}

// 零值 = 连 assistant 一起剥。这条锁住「忘了设字段也不会漏剥 assistant」：
// 上游会扫 assistant，漏剥会让被污染的会话永久 502。
func TestStripZeroValueStillStripsAssistant(t *testing.T) {
	const rule = "Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root"
	cfg := &StripConfig{
		Enabled:  true,
		Rules:    RulesForSite("domestic"),
		Contains: ContainsForSite("domestic"),
	}
	if cfg.SkipAssistant {
		t.Fatal("零值必须是「不跳过」，否则默认会漏剥")
	}
	out := EnsureUpstreamMessagesWith([]map[string]any{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": rule},
		{"role": "user", "content": "again"},
	}, true, cfg, &StripReport{})
	asst, _ := out[1]["content"].(string)
	if strings.Contains(asst, "Tokens prefixed with @") {
		t.Fatalf("零值配置下 assistant 也应被剥：%q", asst)
	}
}
