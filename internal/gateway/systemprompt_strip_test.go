package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// 2026-11-17：auto 档必须在首试前就剥离已知指纹。
//
// 背景：上游按「被 11128 拒收的次数」累计账号风险分。此前 auto 的策略是
// 「先原样传，被拒再折叠」，等于每个请求都主动交一次 11128，账号风险分
// 会持续上涨。现有证据（identity_strip.go 注释）表明黑名单只有精确的几条，
// 首试前删掉它们不会误伤正常内容，漏掉的未知指纹仍有折叠兜底。
func TestApplyStripConfigEnablesKnownFingerprintStripForAutoAndStrip(t *testing.T) {
	for _, mode := range []string{config.SystemPromptModeAuto, config.SystemPromptModeStrip} {
		var opts provider.ChatOptions
		applyStripConfig(&opts, mode, "domestic", true, true, nil)
		if opts.Strip == nil || !opts.Strip.Enabled {
			t.Fatalf("%s: Strip 未启用", mode)
		}
		if len(opts.Strip.Rules) == 0 {
			t.Fatalf("%s: 缺开头身份句规则", mode)
		}
		if len(opts.Strip.Contains) == 0 {
			t.Fatalf("%s: 缺任意位置指纹规则", mode)
		}
		if opts.Strip.SkipAssistant {
			t.Fatalf("%s: assistant 剥离应默认开启", mode)
		}
	}
}

// keep 是「严格原样」档，绝不能偷偷改内容；fold 本就折叠，不需要剥离。
func TestApplyStripConfigLeavesKeepAndFoldUntouched(t *testing.T) {
	for _, mode := range []string{config.SystemPromptModeKeep, config.SystemPromptModeFold} {
		var opts provider.ChatOptions
		applyStripConfig(&opts, mode, "domestic", true, true, nil)
		if opts.Strip != nil {
			t.Fatalf("%s: 不应挂 Strip", mode)
		}
	}
}

// 总开关关掉 ⇒ 退回 v0.7.6 行为（不挂 Strip，先直传、被拒再折叠）。
func TestApplyStripConfigHonorsTotalSwitchOff(t *testing.T) {
	for _, mode := range []string{config.SystemPromptModeAuto, config.SystemPromptModeStrip} {
		var opts provider.ChatOptions
		applyStripConfig(&opts, mode, "domestic", false, true, nil)
		if opts.Strip != nil {
			t.Fatalf("%s: 总开关关掉后不应挂 Strip", mode)
		}
	}
}

// assistant 子开关只影响 assistant，system 仍照剥（Strip 仍然挂着）。
func TestApplyStripConfigAssistantSwitchOnlySkipsAssistant(t *testing.T) {
	var opts provider.ChatOptions
	applyStripConfig(&opts, config.SystemPromptModeAuto, "domestic", true, false, nil)
	if opts.Strip == nil || !opts.Strip.Enabled {
		t.Fatal("关 assistant 不应连 system 剥离一起关掉")
	}
	if !opts.Strip.SkipAssistant {
		t.Fatal("关掉 assistant 剥离时应置 SkipAssistant")
	}
}
