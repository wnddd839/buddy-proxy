package config

import "testing"

func TestNormalizeSystemPromptMode(t *testing.T) {
	cases := map[string]string{
		"":            SystemPromptModeAuto,
		"auto":        SystemPromptModeAuto,
		"AUTO":        SystemPromptModeAuto,
		"  Auto  ":    SystemPromptModeAuto,
		"garbage":     SystemPromptModeAuto,
		"keep":        SystemPromptModeKeep,
		"KEEP":        SystemPromptModeKeep,
		"preserve":    SystemPromptModeKeep,
		"passthrough": SystemPromptModeKeep,
		"1":           SystemPromptModeKeep,
		"true":        SystemPromptModeKeep,
		"fold":        SystemPromptModeFold,
		"FOLD":        SystemPromptModeFold,
		"0":           SystemPromptModeFold,
		"false":       SystemPromptModeFold,
		"  keep  ":    SystemPromptModeKeep,
	}
	for in, want := range cases {
		if got := NormalizeSystemPromptMode(in); got != want {
			t.Fatalf("NormalizeSystemPromptMode(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSystemPromptModeIsValid(t *testing.T) {
	for _, ok := range []string{"auto", "keep", "fold"} {
		if !IsValidSystemPromptMode(ok) {
			t.Fatalf("%q 应合法", ok)
		}
	}
	for _, bad := range []string{"", "AUTO", " preserve ", "nonsense"} {
		if IsValidSystemPromptMode(bad) {
			t.Fatalf("%q 不应合法（区分大小写，不做归一）", bad)
		}
	}
}

func TestLoadSystemPromptModeDefaultsToAuto(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE", "")
	if got := Load().SystemPromptMode; got != SystemPromptModeAuto {
		t.Fatalf("未设置时应默认 auto，实际 %q", got)
	}
}

func TestLoadSystemPromptModeReadsEnv(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE", " keep ")
	if got := Load().SystemPromptMode; got != SystemPromptModeKeep {
		t.Fatalf("应读到 keep，实际 %q", got)
	}
	t.Setenv("CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE", "nonsense")
	if got := Load().SystemPromptMode; got != SystemPromptModeAuto {
		t.Fatalf("垃圾值应归一为 auto，实际 %q", got)
	}
}
