package sessionpin_test

import (
	"net/http"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
)

// 本文件固化 DeepSeek Harness 的会话键规则。
//
// 源码实证（2026-11-16）：
//
//	dsh-session/lib/index.js:1554
//	  sessionId = brandString(`session-${++this.counter}`)
//	  SessionStore.counter = 0        ← 实例字段，进程重启归零
//
//	dsh-anonymous-user-id/lib/index.js:50
//	  getOrCreateAnonymousUserId() 读写 $DSH_HOME/.anonymous-user-id（持久 UUID）
//
// 结论：session-id 单独用会跨重启/跨实例重复，必须与持久化的 user-id 组合。

const (
	dshUserA    = "f93bd21f-f8f9-4291-8a00-12819b97cb23"
	dshUserB    = "11111111-2222-3333-4444-555555555555"
	dshSession1 = "session-1"
	dshSession2 = "session-2"
)

// TestHarnessKeyCombinesUserAndSession 校验两者都有时用复合键。
func TestHarnessKeyCombinesUserAndSession(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-User-Id", dshUserA)
	h.Set("X-Deepseek-Harness-Session-Id", dshSession1)
	key, source := sessionpin.Key(h, "", nil)

	want := "dsh:" + dshUserA + "\x1e" + dshSession1
	if key != want {
		t.Fatalf("got %q want %q", key, want)
	}
	if source != sessionpin.KeySourceHeader {
		t.Fatalf("来源应为 header，得到 %q", source)
	}
}

// TestHarnessKeyOnlyUserIsStable 校验只有 user-id 时仍能得到稳定键。
func TestHarnessKeyOnlyUserIsStable(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-User-Id", dshUserA)
	key, source := sessionpin.Key(h, "", nil)
	if key != "dsh:"+dshUserA {
		t.Fatalf("got %q", key)
	}
	if source != sessionpin.KeySourceHeader {
		t.Fatalf("来源应为 header，得到 %q", source)
	}
}

// TestHarnessKeySessionOnlyFallsBackToContent 是最重要的一条：
// 只有 session-id 时**不得**用它当键，必须回落内容哈希。
//
// 理由：session-1 会在 dsh 重启后再次出现，单独用会让不同会话撞键，
// 导致两个会话共用账号钉与上游 ConversationID（上下文串味）。
// 回落内容哈希虽然粗，但稳定且不会串。
func TestHarnessKeySessionOnlyFallsBackToContent(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-Session-Id", dshSession1)
	msgs := []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "hello"},
	}
	key, source := sessionpin.Key(h, "", msgs)
	if source != sessionpin.KeySourceContent {
		t.Fatalf("只有 session-id 时应回落内容哈希，实际来源 %q", source)
	}
	if len(key) < 4 || key[:4] != "msg:" {
		t.Fatalf("应得到 msg: 键，实际 %q", key)
	}
}

// TestHarnessKeyDistinguishesParallelSessions 校验同一 user 的两个并发会话
// 得到不同的键（否则会互相抢账号钉）。
func TestHarnessKeyDistinguishesParallelSessions(t *testing.T) {
	mk := func(sess string) string {
		h := http.Header{}
		h.Set("X-Deepseek-Harness-User-Id", dshUserA)
		h.Set("X-Deepseek-Harness-Session-Id", sess)
		k, _ := sessionpin.Key(h, "", nil)
		return k
	}
	if mk(dshSession1) == mk(dshSession2) {
		t.Fatal("同一 user 的不同 session 必须得到不同键")
	}
}

// TestHarnessKeyDistinguishesRestartedInstances 校验「重启后 session-1 复用」
// 不会撞到另一台机器上的 session-1（因为 user-id 不同）。
func TestHarnessKeyDistinguishesRestartedInstances(t *testing.T) {
	mk := func(user string) string {
		h := http.Header{}
		h.Set("X-Deepseek-Harness-User-Id", user)
		h.Set("X-Deepseek-Harness-Session-Id", dshSession1) // 两台机器都是 session-1
		k, _ := sessionpin.Key(h, "", nil)
		return k
	}
	if mk(dshUserA) == mk(dshUserB) {
		t.Fatal("不同 user 的相同 session 名必须得到不同键（防跨实例撞键）")
	}
}

// TestHarnessKeyStableAcrossTurns 校验同一会话的多轮请求键不变。
func TestHarnessKeyStableAcrossTurns(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-User-Id", dshUserA)
	h.Set("X-Deepseek-Harness-Session-Id", dshSession1)

	first, _ := sessionpin.Key(h, "", []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "turn1"},
	})
	// 第二轮内容不同，但会话头不变 → 键必须相同
	second, _ := sessionpin.Key(h, "", []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "turn1"},
		{"role": "assistant", "content": "ok"},
		{"role": "user", "content": "turn2"},
	})
	if first != second {
		t.Fatalf("同一会话多轮键必须稳定：%q != %q", first, second)
	}
}

// TestHarnessKeyNotOverriddenByGenericHeaders 校验通用会话头优先级更高
// （若客户端同时发了 X-Session-Id，它是明确约定，应当优先）。
func TestHarnessKeyNotOverriddenByGenericHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-Session-Id", "explicit-session")
	h.Set("X-Deepseek-Harness-User-Id", dshUserA)
	h.Set("X-Deepseek-Harness-Session-Id", dshSession1)
	key, _ := sessionpin.Key(h, "", nil)
	if key != "hdr:explicit-session" {
		t.Fatalf("通用显式头应优先，得到 %q", key)
	}
}

// TestHarnessKeyClientHintStillRecognizes 校验加入复合键后，
// ClientHint 仍能仅凭 session 头识别出 dsh（观测不受影响）。
func TestHarnessKeyClientHintStillRecognizes(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-Session-Id", dshSession1)
	if got := sessionpin.ClientHint(h); got != sessionpin.ClientDeepSeekHarness {
		t.Fatalf("仅 session 头也应识别为 dsh，得到 %q", got)
	}
}
