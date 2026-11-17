package sessionpin_test

import (
	"net/http"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
)

// 本文件固化「客户端会话头 → sessionKey」的实测结论。
//
// 背景：会话亲和靠三级回退
//   ① 客户端显式会话头（各 agent 的专属头）
//   ② 请求体 prompt_cache_key
//   ③ system + 首条 user 的内容哈希（msg:）
//
// 2026-11-16 实测发现：DeepSeek Harness 发的是
// x-deepseek-harness-session-id，但它不在候选列表里 → 落到 msg: 内容哈希。
// 这会让 dsh 的账号钉随 system 内容变化而失效（尤其 strip 档会改 system）。
//
// 各 agent 的专属头（源码实证）：
//   opencode      : x-opencode-session / x-opencode-client
//   deepseek-harness: x-deepseek-harness-session-id / x-dsh-auth-token
//   claude-code   : x-app: cli + anthropic-version（无会话头）

// TestKeyUsesDeepSeekHarnessCompositeKey 校验 dsh 用 user+session 复合键。
//
// 注意：这里刻意不用「只有 session-id」的用例 —— dsh 的 session-id 是
// 进程内自增序号（session-1、session-2…），重启后归零会重复，
// 单独作键会让不同会话撞车。详见 harness_key_test.go 的说明。
func TestKeyUsesDeepSeekHarnessCompositeKey(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-User-Id", "user-uuid-abc")
	h.Set("X-Deepseek-Harness-Session-Id", "session-1")
	key, source := sessionpin.Key(h, "", nil)
	want := "dsh:user-uuid-abc\x1esession-1"
	if key != want {
		t.Fatalf("应使用 dsh 复合键，得到 %q want %q", key, want)
	}
	if source != sessionpin.KeySourceHeader {
		t.Fatalf("来源应为 header，得到 %q", source)
	}
}

// TestKeyDeepSeekHarnessSessionOnlyNotUsedAsKey 防回归：只有 session-id 时
// 必须回落内容哈希，不得直接拿 session-1 当键。
func TestKeyDeepSeekHarnessSessionOnlyNotUsedAsKey(t *testing.T) {
	h := http.Header{}
	h.Set("X-Deepseek-Harness-Session-Id", "sess-dsh-001")
	key, source := sessionpin.Key(h, "", nil)
	if key == "hdr:sess-dsh-001" || key == "dsh:sess-dsh-001" {
		t.Fatalf("不得单独用 session-id 作键，得到 %q", key)
	}
	if source != sessionpin.KeySourceNone {
		t.Fatalf("无可用信号时来源应为 none，得到 %q", source)
	}
}

func TestKeyUsesOpencodeSessionHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-Opencode-Session", "ses_abc123")
	key, source := sessionpin.Key(h, "", nil)
	if key != "hdr:ses_abc123" {
		t.Fatalf("应使用 opencode 会话头，得到 %q", key)
	}
	if source != sessionpin.KeySourceHeader {
		t.Fatalf("来源应为 header，得到 %q", source)
	}
}

func TestKeyFallsBackToPromptCacheKey(t *testing.T) {
	key, source := sessionpin.Key(http.Header{}, "cache-key-xyz", nil)
	if key != "hdr:cache-key-xyz" {
		t.Fatalf("应使用 prompt_cache_key，得到 %q", key)
	}
	if source != sessionpin.KeySourceCacheKey {
		t.Fatalf("来源应为 cachekey，得到 %q", source)
	}
}

func TestKeyFallsBackToContentHash(t *testing.T) {
	msgs := []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "hello"},
	}
	key, source := sessionpin.Key(http.Header{}, "", msgs)
	if key == "" || key[:4] != "msg:" {
		t.Fatalf("应回落内容哈希，得到 %q", key)
	}
	if source != sessionpin.KeySourceContent {
		t.Fatalf("来源应为 content，得到 %q", source)
	}
}

// TestKeyContentHashIsStableAcrossTurns 校验同一段对话的多轮请求得到同一个 key
// （system + 首条 user 不变，后续工具轮次不改变 key）。
func TestKeyContentHashIsStableAcrossTurns(t *testing.T) {
	base := []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "hello"},
	}
	first, _ := sessionpin.Key(http.Header{}, "", base)
	// 追加一轮 assistant + tool 后，system 与首条 user 未变
	more := append(append([]map[string]any{}, base...),
		map[string]any{"role": "assistant", "content": "ok"},
		map[string]any{"role": "user", "content": "again"},
	)
	second, _ := sessionpin.Key(http.Header{}, "", more)
	if first != second {
		t.Fatalf("多轮应保持同一 key：%q != %q", first, second)
	}
}

// TestKeyEmptyWhenNoSignal 校验完全无信号时返回空（调用方回落随机 ID）。
func TestKeyEmptyWhenNoSignal(t *testing.T) {
	key, source := sessionpin.Key(http.Header{}, "", nil)
	if key != "" {
		t.Fatalf("无信号应返回空，得到 %q", key)
	}
	if source != sessionpin.KeySourceNone {
		t.Fatalf("来源应为 none，得到 %q", source)
	}
}

// TestKeyHeaderPriority 校验多个头同时存在时的优先级。
func TestKeyHeaderPriority(t *testing.T) {
	h := http.Header{}
	h.Set("X-Session-Id", "explicit")
	h.Set("X-Opencode-Session", "opencode")
	h.Set("X-Deepseek-Harness-Session-Id", "dsh")
	key, _ := sessionpin.Key(h, "cachekey", nil)
	// X-Session-Id 排最前：它是通用显式约定，优先级最高。
	if key != "hdr:explicit" {
		t.Fatalf("X-Session-Id 应优先，得到 %q", key)
	}
}
