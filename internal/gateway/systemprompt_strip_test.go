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
		applyStripConfig(&opts, mode, "domestic", nil)
		if opts.Strip == nil || !opts.Strip.Enabled {
			t.Fatalf("%s: Strip 未启用", mode)
		}
		if len(opts.Strip.Rules) == 0 {
			t.Fatalf("%s: 缺开头身份句规则", mode)
		}
		if len(opts.Strip.Contains) == 0 {
			t.Fatalf("%s: 缺任意位置指纹规则", mode)
		}
	}
}

// keep 是「严格原样」档，绝不能偷偷改内容；fold 本就折叠，不需要剥离。
func TestApplyStripConfigLeavesKeepAndFoldUntouched(t *testing.T) {
	for _, mode := range []string{config.SystemPromptModeKeep, config.SystemPromptModeFold} {
		var opts provider.ChatOptions
		applyStripConfig(&opts, mode, "domestic", nil)
		if opts.Strip != nil {
			t.Fatalf("%s: 不应挂 Strip", mode)
		}
	}
}
