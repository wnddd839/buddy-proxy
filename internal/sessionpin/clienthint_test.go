package sessionpin_test

import (
	"net/http"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
)

// 本文件固化各 agent 的「请求头特征 → 客户端类型」识别规则。
//
// 所有 UA 格式与专属头均来自源码/二进制实证（2026-11-16）：
//   opencode      UA: opencode/<channel>/<version>/<name>
//                 头: x-opencode-client / x-opencode-session / x-opencode-project ...
//   deepseek-harness UA: deepseek-harness/<version> (+https://github.com/deepseek-ai/deepseek-harness)
//                 头: x-deepseek-harness-session-id / x-dsh-auth-token / x-deepseek-harness-user-id
//   claude-code   UA: claude-cli/<version> (external, cli)
//                 头: x-app: cli + anthropic-version + anthropic-beta
//
// 关键陷阱：dsh 也发 anthropic-version: 2023-06-01 和 anthropic-beta
// （源码 dsh-llm-deepseek/lib/index.js:222-223）。因此「有 anthropic-version
// 就是 Claude Code」是错的，必须让专属头/专属 UA 先判。

func TestClientHintDeepSeekHarness(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
	}{
		{
			"专属会话头",
			http.Header{"X-Deepseek-Harness-Session-Id": []string{"sess-1"}},
		},
		{
			"专属鉴权头",
			http.Header{"X-Dsh-Auth-Token": []string{"tok"}},
		},
		{
			"专属用户头",
			http.Header{"X-Deepseek-Harness-User-Id": []string{"u1"}},
		},
		{
			"官方 UA",
			http.Header{"User-Agent": []string{"deepseek-harness/0.1.7-alpha.1 (+https://github.com/deepseek-ai/deepseek-harness)"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sessionpin.ClientHint(c.header); got != sessionpin.ClientDeepSeekHarness {
				t.Fatalf("got %q want %q", got, sessionpin.ClientDeepSeekHarness)
			}
		})
	}
}

// TestClientHintDeepSeekHarnessNotMistakenForClaudeCode 是最重要的一条：
// dsh 同时发 anthropic-version/anthropic-beta 与自己的专属头，
// 必须识别成 dsh，而不是 claude-code。
func TestClientHintDeepSeekHarnessNotMistakenForClaudeCode(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-Session-Id", "sess-1")
	h.Set("Anthropic-Version", "2023-06-01")
	h.Set("Anthropic-Beta", "files-api-2025-04-14")
	h.Set("User-Agent", "deepseek-harness/0.1.7-alpha.1 (+https://github.com/deepseek-ai/deepseek-harness)")
	if got := sessionpin.ClientHint(h); got != sessionpin.ClientDeepSeekHarness {
		t.Fatalf("dsh 被误判为 %q（应为 %q）", got, sessionpin.ClientDeepSeekHarness)
	}
}

func TestClientHintOpencode(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
	}{
		{"专属 client 头", http.Header{"X-Opencode-Client": []string{"tui"}}},
		{"专属 session 头", http.Header{"X-Opencode-Session": []string{"ses_abc"}}},
		{"专属 UA", http.Header{"User-Agent": []string{"opencode/stable/2.2.6/tui"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sessionpin.ClientHint(c.header); got != sessionpin.ClientOpencode {
				t.Fatalf("got %q want %q", got, sessionpin.ClientOpencode)
			}
		})
	}
}

func TestClientHintClaudeCode(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
	}{
		{"官方 UA", http.Header{"User-Agent": []string{"claude-cli/2.1.0 (external, cli)"}}},
		{
			"x-app + anthropic-version",
			http.Header{
				"X-App":             []string{"cli"},
				"Anthropic-Version": []string{"2023-06-01"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sessionpin.ClientHint(c.header); got != sessionpin.ClientClaudeCode {
				t.Fatalf("got %q want %q", got, sessionpin.ClientClaudeCode)
			}
		})
	}
}

// TestClientHintAnthropicVersionAloneIsNotClaudeCode 校验弱信号不能单独成立：
// 只有 anthropic-version 没有 x-app: cli 时，不应判成 claude-code
// （很多 Anthropic 兼容网关都会发这个头）。
func TestClientHintAnthropicVersionAloneIsNotClaudeCode(t *testing.T) {
	h := http.Header{}
	h.Set("Anthropic-Version", "2023-06-01")
	got := sessionpin.ClientHint(h)
	if got == sessionpin.ClientClaudeCode {
		t.Fatalf("仅 anthropic-version 不应判为 claude-code，得到 %q", got)
	}
}

func TestClientHintUnknownAndEmpty(t *testing.T) {
	// 完全无头 → unknown
	if got := sessionpin.ClientHint(http.Header{}); got != sessionpin.ClientUnknown {
		t.Fatalf("无 UA 应为 %q，得到 %q", sessionpin.ClientUnknown, got)
	}
	// 不认识的 UA → 空串（不污染事件）
	h := http.Header{"User-Agent": []string{"some-random-client/1.0"}}
	if got := sessionpin.ClientHint(h); got != "" {
		t.Fatalf("未知 UA 应返回空串，得到 %q", got)
	}
}

// TestClientHintIsCaseInsensitive 校验 UA 匹配大小写无关。
func TestClientHintIsCaseInsensitive(t *testing.T) {
	h := http.Header{"User-Agent": []string{"DeepSeek-Harness/0.1.7 (+https://x)"}}
	if got := sessionpin.ClientHint(h); got != sessionpin.ClientDeepSeekHarness {
		t.Fatalf("UA 匹配应大小写无关，得到 %q", got)
	}
}

// TestClientHintDoesNotAffectStripDecision 是一条防回归断言：
// ClientHint 只用于观测，剥离决策必须只看 system 内容。
// 这里只校验「不同 clientHint 下，rulesFor 的结果不受影响」——
// 通过 provider 包的测试间接保证（此处只记录设计意图）。
func TestClientHintDoesNotAffectStripDecision(t *testing.T) {
	// 该断言在 provider 包的 TestStripBlockedIdentityRulesForSite 中实现：
	// rulesFor(site) 不接受 client 参数，从签名上就杜绝了按客户端做减法。
	t.Skip("由 provider.TestStripBlockedIdentityRulesForSite 覆盖")
}
