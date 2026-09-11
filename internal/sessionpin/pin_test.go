package sessionpin_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
)

func TestKeyPrefersSessionHeader(t *testing.T) {
	h := make(http.Header)
	h.Set("X-Session-Id", "codex-abc")
	got := sessionpin.Key(h, "ignored", []map[string]any{{"role": "user", "content": "hello"}})
	if got != "hdr:codex-abc" {
		t.Fatalf("key=%q", got)
	}
}

func TestKeyUsesPromptCacheKey(t *testing.T) {
	got := sessionpin.Key(nil, "pc-1", []map[string]any{{"role": "user", "content": "hello"}})
	if got != "hdr:pc-1" {
		t.Fatalf("key=%q", got)
	}
}

func TestKeyStableFromFirstUserMessage(t *testing.T) {
	first := []map[string]any{
		{"role": "system", "content": "you are a bot"},
		{"role": "user", "content": "fix the bug"},
	}
	later := []map[string]any{
		{"role": "system", "content": "you are a bot"},
		{"role": "user", "content": "fix the bug"},
		{"role": "assistant", "content": "ok"},
		{"role": "user", "content": "also tests"},
	}
	a := sessionpin.Key(nil, "", first)
	b := sessionpin.Key(nil, "", later)
	if a == "" || a != b {
		t.Fatalf("expected stable conversation key, got %q vs %q", a, b)
	}
	other := sessionpin.Key(nil, "", []map[string]any{{"role": "user", "content": "different"}})
	if other == a {
		t.Fatal("different first user message must not collide")
	}
}

func TestTableRememberLookupForget(t *testing.T) {
	pins := sessionpin.New(time.Hour)
	if _, ok := pins.Lookup("s1"); ok {
		t.Fatal("empty table")
	}
	pins.Remember("s1", "acc-1")
	id, ok := pins.Lookup("s1")
	if !ok || id != "acc-1" {
		t.Fatalf("lookup=%s ok=%v", id, ok)
	}
	pins.Forget("s1")
	if _, ok := pins.Lookup("s1"); ok {
		t.Fatal("forgot key still present")
	}
}

func TestTableExpires(t *testing.T) {
	pins := sessionpin.New(time.Millisecond)
	pins.Remember("s1", "acc-1")
	time.Sleep(5 * time.Millisecond)
	if _, ok := pins.Lookup("s1"); ok {
		t.Fatal("expired pin still present")
	}
}
