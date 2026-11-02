package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 请求体上限必须可配置：超长会话历史重放可能超过默认 64MiB。
func TestSetMaxJSONBodyBytes(t *testing.T) {
	orig := MaxJSONBodyBytes
	defer func() { MaxJSONBodyBytes = orig }()

	SetMaxJSONBodyBytes(256 << 20)
	if got := MaxJSONBodyBytes; got != 256<<20 {
		t.Fatalf("期望 256MiB，实际 %d", got)
	}

	// 非正值应被忽略（防止误配把上限归零）
	SetMaxJSONBodyBytes(0)
	if got := MaxJSONBodyBytes; got != 256<<20 {
		t.Fatalf("n<=0 不应修改上限，实际 %d", got)
	}
	SetMaxJSONBodyBytes(-1)
	if got := MaxJSONBodyBytes; got != 256<<20 {
		t.Fatalf("n<=0 不应修改上限，实际 %d", got)
	}
}

// 错误文案必须反映实际上限，不能写死 "64MB"，否则排查时会被误导。
func TestBodyTooLargeMessageReflectsLimit(t *testing.T) {
	orig := MaxJSONBodyBytes
	defer func() { MaxJSONBodyBytes = orig }()

	SetMaxJSONBodyBytes(128 << 20)
	if got := BodyTooLargeMessage(); !strings.Contains(got, "128MB") {
		t.Fatalf("文案应含 128MB，实际 %q", got)
	}
	if got := BodyTooLargeMessageSentence(); !strings.Contains(got, "128MB") {
		t.Fatalf("文案应含 128MB，实际 %q", got)
	}
}

// 超过上限时应返回 ErrBodyTooLarge，而不是静默截断。
func TestReadJSONExceedsLimit(t *testing.T) {
	orig := MaxJSONBodyBytes
	defer func() { MaxJSONBodyBytes = orig }()

	SetMaxJSONBodyBytes(1 << 10) // 1KiB

	big := `{"a":"` + strings.Repeat("x", 4096) + `"}`
	var dst map[string]any
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(big))
	err := ReadJSON(req, &dst)
	if err == nil {
		t.Fatal("超限请求应报错")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("应返回体积超限错误，实际 %q", err)
	}
}
