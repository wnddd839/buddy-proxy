package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// headerCapturingTransport 记录每个账号的 X-Conversation-ID，其余返回 200 SSE。
type headerCapturingTransport struct {
	onChat func(*http.Request)
}

func (t *headerCapturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, "chat") {
		// billing / dosage 探测：返回无额度 fixture，避免干扰选号。
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{"code":1,"msg":"no fixture"}`)),
			Request:    req,
		}, nil
	}
	if t.onChat != nil {
		t.onChat(req)
	}
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func trimBearer(value string) string {
	return strings.TrimPrefix(value, "Bearer ")
}

// newConversationTestService 建一个双账号 + 脚本化上游的网关。
func newConversationTestService(t *testing.T, transport http.RoundTripper) (*Service, string, string) {
	t.Helper()
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	a, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "b", Site: "domestic", BearerToken: "token-b", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: transport}
	return svc, a.ID, b.ID
}

func TestCompleteFromPoolReusesUpstreamConversationID(t *testing.T) {
	seen := map[string]string{} // bearer token -> X-Conversation-ID
	transport := &headerCapturingTransport{onChat: func(req *http.Request) {
		token := req.Header.Get("Authorization")
		token = trimBearer(token)
		if _, ok := seen[token]; !ok {
			seen[token] = req.Header.Get("X-Conversation-ID")
		}
	}}
	svc, _, _ := newConversationTestService(t, transport)
	ctx := t.Context()
	first, err := svc.CompleteFromPool(ctx, CompleteOptions{
		Model: "auto", SessionKey: "conv-1",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CompleteFromPool(ctx, CompleteOptions{
		Model: "auto", SessionKey: "conv-1",
		Messages: []map[string]any{{"role": "user", "content": "hi"}, {"role": "assistant", "content": "ok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Trace.ConversationID == "" || first.Trace.ConversationID != second.Trace.ConversationID {
		t.Fatalf("same session must reuse upstream conversation id: %s vs %s", first.Trace.ConversationID, second.Trace.ConversationID)
	}
	if got := seen["token-a"]; got != first.Trace.ConversationID {
		t.Fatalf("upstream header %q must equal trace id %q", got, first.Trace.ConversationID)
	}
	// 无会话键（退化 key）：不同消息内容 → 不同键，conversation ID 逐会话随机。
	bare, err := svc.CompleteFromPool(ctx, CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "one-off"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.CompleteFromPool(ctx, CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "one-off-2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bare.Trace.ConversationID == "" || bare.Trace.ConversationID == again.Trace.ConversationID {
		t.Fatalf("content-distinct sessionless requests must not share ids: %s vs %s", bare.Trace.ConversationID, again.Trace.ConversationID)
	}
}

func TestCompleteFromPoolRotatesConversationIDAfterAccountSwitch(t *testing.T) {
	transport := &headerCapturingTransport{}
	svc, aID, bID := newConversationTestService(t, transport)
	// 预置会话钉在 a，a 对该会话返回 429 → 松钉换 b。
	svc.Pins.Remember(SessionPinKey("conv-rot", "domestic"), aID)
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{byAuth: map[string]int{"token-a": http.StatusTooManyRequests}}}
	switched, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", SessionKey: "conv-rot",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if switched.AccountID != bID {
		t.Fatalf("expected failover to b, got %s", switched.AccountID)
	}
	firstID := switched.Trace.ConversationID
	if firstID == "" {
		t.Fatal("expected upstream conversation id on switched account")
	}
	// 换回 scripted 200 上游，同会话继续：ID 必须稳定。
	svc.Provider.HTTP = &http.Client{Transport: transport}
	next, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", SessionKey: "conv-rot",
		Messages: []map[string]any{{"role": "user", "content": "hi"}, {"role": "assistant", "content": "ok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.AccountID != bID || next.Trace.ConversationID != firstID {
		t.Fatalf("stable account must keep conversation id: account %s id %s vs %s", next.AccountID, next.Trace.ConversationID, firstID)
	}
}

func TestCompleteFromPoolSeparatesConversationIDPerSessionAndAccount(t *testing.T) {
	ids := map[string]string{} // token|conversation header -> value
	transport := &headerCapturingTransport{onChat: func(req *http.Request) {
		ids[trimBearer(req.Header.Get("Authorization"))+"|"+req.Header.Get("X-Session-Id")] = req.Header.Get("X-Conversation-ID")
	}}
	svc, _, _ := newConversationTestService(t, transport)
	ctx := t.Context()
	r1, err := svc.CompleteFromPool(ctx, CompleteOptions{
		Model: "auto", SessionKey: "conv-1",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc.CompleteFromPool(ctx, CompleteOptions{
		Model: "auto", SessionKey: "conv-2",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Trace.ConversationID == r2.Trace.ConversationID {
		t.Fatal("different sessions must not share an upstream conversation id")
	}
}
