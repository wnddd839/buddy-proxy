package sessionpin

import (
	"net/http"
	"testing"
)

// 用 opencode 2.2.6 实抓的请求头原文，验证 ClientHint 判定。
//
// 实抓（2026-11-18，opencode v2.2.6 → workbuddy-cn provider）：
//
//	User-Agent: opencode/latest/2.2.6/cli
//	x-opencode-client: cli
//	x-opencode-project: 7fc025facb5a13ae0f3529454f122c018c8d16d9
//	x-opencode-session: ses_e118f3460ffeSCE5ck6hQMLsPd
//	x-opencode-session-id: ses_e118f3460ffeSCE5ck6hQMLsPd
//	x-session-affinity / x-session-id: 同 session
func TestClientHintMatchesRealOpencodeHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "opencode/latest/2.2.6/cli")
	h.Set("X-Opencode-Client", "cli")
	h.Set("X-Opencode-Project", "7fc025facb5a13ae0f3529454f122c018c8d16d9")
	h.Set("X-Opencode-Session", "ses_e118f3460ffeSCE5ck6hQMLsPd")
	h.Set("X-Opencode-Session-Id", "ses_e118f3460ffeSCE5ck6hQMLsPd")
	h.Set("X-Session-Affinity", "ses_e118f3460ffeSCE5ck6hQMLsPd")
	h.Set("Content-Type", "application/json")

	if got := ClientHint(h); got != ClientOpencode {
		t.Fatalf("实抓 header 应判定为 opencode，实际 %q", got)
	}
}

// 只有 UA 也要认（有的转发层可能剥离自定义头）。
func TestClientHintOpencodeByUserAgentOnly(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "opencode/latest/2.2.6/cli")
	if got := ClientHint(h); got != ClientOpencode {
		t.Fatalf("仅凭 UA 也应判定 opencode，实际 %q", got)
	}
}

// dsh 的专属头必须优先于 opencode 判定 —— 两者都可能带 UA 噪声。
func TestClientHintHarnessBeatsOpencode(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "opencode/latest/2.2.6/cli")
	h.Set("X-Opencode-Client", "cli")
	h.Set("X-Deepseek-Harness-User-Id", "uuid-1")
	if got := ClientHint(h); got != ClientDeepSeekHarness {
		t.Fatalf("dsh 头应优先，实际 %q", got)
	}
}

// 网关自己转发给上游时用的是 CodeBuddy 头（UA 被覆盖成 CLI/x CodeBuddy/x）。
// 这条锁住「上游看到的不是 opencode」这一事实，防止有人误以为 clientHint 来自上游。
func TestClientHintDoesNotMatchGatewayUpstreamUA(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "CLI/2.2.6 CodeBuddy/2.2.6")
	h.Set("X-IDE-Type", "CLI")
	h.Set("X-Product", "SaaS")
	if got := ClientHint(h); got == ClientOpencode {
		t.Fatalf("网关转发的上游头不应被认成 opencode，实际 %q", got)
	}
}
