package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/models"
	"github.com/wnddd839/codebuddy-proxy/internal/openai"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestResolveProviderModel(t *testing.T) {
	tests := []struct {
		in     string
		model  string
		public string
		site   string
	}{
		{"", "auto", "auto", ""},
		{"codebuddy", "auto", "auto", ""},
		{"codebuddy:deepseek-v4", "deepseek-v4", "deepseek-v4", ""},
		{"codebuddy/deepseek-v4", "deepseek-v4", "deepseek-v4", ""},
		{"deepseek-v4-flash", "deepseek-v4-flash", "deepseek-v4-flash", ""},
		{"global:deepseek-v4.1-flash", "deepseek-v4.1-flash", "global:deepseek-v4.1-flash", "global"},
		{"cn:glm-5.3-flash", "glm-5.3-flash", "cn:glm-5.3-flash", "domestic"},
		{"codebuddy:global:auto", "auto", "global:auto", "global"},
	}
	for _, tc := range tests {
		got := ResolveProviderModel(tc.in)
		if got.Model != tc.model || got.PublicModel != tc.public || got.Site != tc.site {
			t.Fatalf("ResolveProviderModel(%q) = %+v, want model=%q public=%q site=%q", tc.in, got, tc.model, tc.public, tc.site)
		}
	}
}

type scriptedChatTransport struct {
	mu            sync.Mutex
	calls         []string
	chatCalls     []string
	billingCalls  []string
	byAuth        map[string]int
	creditsByAuth map[string]float64
}

func (t *scriptedChatTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	path := req.URL.Path
	t.mu.Lock()
	t.calls = append(t.calls, token)
	if strings.Contains(path, "get-user-resource") {
		t.billingCalls = append(t.billingCalls, token)
		remaining, ok := t.creditsByAuth[token]
		t.mu.Unlock()
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		body := `{"code":1,"msg":"no fixture"}`
		if ok {
			body = fmt.Sprintf(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CapacityRemain":%g,"CapacitySize":1000,"CapacityUsed":0,"CapacityType":1}]}}}}`, remaining)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
	if strings.Contains(path, "get-dosage-notify") {
		t.mu.Unlock()
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"dosageNotifyCode":0}}`)),
			Request:    req,
		}, nil
	}
	t.chatCalls = append(t.chatCalls, token)
	status := t.byAuth[token]
	t.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	header := make(http.Header)
	var body string
	if status >= 200 && status < 300 {
		header.Set("Content-Type", "text/event-stream")
		body = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	} else {
		header.Set("Content-Type", "application/json")
		body = `{"error":"too many requests"}`
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (t *scriptedChatTransport) snapshot() (chat, billing []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string{}, t.chatCalls...), append([]string{}, t.billingCalls...)
}

func TestCompleteFromPoolRetriesNextAccountAfter429(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/accounts.json"
	svc := New(config.Config{Site: "domestic", AccountsPath: path}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
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

	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-a": http.StatusTooManyRequests,
		"token-b": http.StatusOK,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("expected second account to succeed, got %v", err)
	}
	if result.AccountID != b.ID {
		t.Fatalf("AccountID=%s want %s (account B)", result.AccountID, b.ID)
	}
	if result.Turn.Text != "ok" {
		t.Fatalf("text=%q want ok", result.Turn.Text)
	}
	chat, _ := transport.snapshot()
	if len(chat) != 2 {
		t.Fatalf("chat calls=%v want token-a then token-b", chat)
	}
	if chat[0] != "token-a" || chat[1] != "token-b" {
		t.Fatalf("chat calls=%v want [token-a token-b]; first account was %s", chat, a.ID)
	}
}

func TestCompleteFromPoolPreserves429WhenAllAccountsFail(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/accounts.json"
	svc := New(config.Config{Site: "domestic", AccountsPath: path}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	for _, token := range []string{"token-a", "token-b"} {
		if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
			Label: token, Site: "domestic", BearerToken: token, Enabled: true,
		})); err != nil {
			t.Fatal(err)
		}
	}
	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-a": http.StatusTooManyRequests,
		"token-b": http.StatusTooManyRequests,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatal("expected upstream 429")
	}
	if strings.Contains(err.Error(), "重试深度") {
		t.Fatalf("retry budget must not mask original 429: %v", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("unexpected error: %v", err)
	}
	chat, _ := transport.snapshot()
	if len(chat) != 2 {
		t.Fatalf("chat calls=%d want 2, got %v", len(chat), chat)
	}
}

func TestCompleteFromPoolPinsSessionToOneAccount(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	a, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "b", Site: "domestic", BearerToken: "token-b", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}

	first, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-1",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-1",
		Messages: []map[string]any{{"role": "user", "content": "hi"}, {"role": "assistant", "content": "ok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.AccountID != second.AccountID {
		t.Fatalf("session pin broke: %s vs %s", first.AccountID, second.AccountID)
	}
	other, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-2",
		Messages: []map[string]any{{"role": "user", "content": "other"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if other.AccountID == first.AccountID {
		t.Fatalf("new session should be able to use another account, both %s (first was %s)", other.AccountID, a.ID)
	}
}

func TestCompleteFromPoolPinnedAccountSwitchesAfter429(t *testing.T) {
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
	transport := &scriptedChatTransport{byAuth: map[string]int{"token-a": http.StatusTooManyRequests}}
	svc.Provider.HTTP = &http.Client{Transport: transport}
	svc.Pins.Remember(SessionPinKey("conv-sticky", "domestic"), a.ID)

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-sticky",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("expected failover after pinned 429, got %v", err)
	}
	if result.AccountID != b.ID {
		t.Fatalf("AccountID=%s want %s after unpin", result.AccountID, b.ID)
	}
	if _, still := svc.Pins.Lookup(SessionPinKey("conv-sticky", "domestic")); still && result.AccountID == a.ID {
		t.Fatal("failed pin must not keep the exhausted account")
	}
}

func TestCompleteFromPoolPinnedAccountHealsAfterSiteMismatch(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	global, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "global", Site: "global", BearerToken: "token-global", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	domestic, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "domestic", Site: "domestic", BearerToken: "token-domestic", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}
	// 模拟：会话在 SITE=国际 时钉到国际号，随后管理台切到国内。
	svc.Pins.Remember(SessionPinKey("conv-site-switch", "domestic"), global.ID)

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-site-switch",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("expected site-mismatch pin to self-heal, got %v", err)
	}
	if result.AccountID != domestic.ID {
		t.Fatalf("AccountID=%s want domestic %s after SITE switch", result.AccountID, domestic.ID)
	}
	if pinned, ok := svc.Pins.Lookup(SessionPinKey("conv-site-switch", "domestic")); !ok || pinned != domestic.ID {
		t.Fatalf("pin=%q ok=%v want domestic %s", pinned, ok, domestic.ID)
	}
}

func TestIsPinnedAccountUnusable(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		want   bool
		reason string
	}{
		{
			name:   "wrapped no credentials",
			err:    fmt.Errorf("%w: x", accounts.ErrNoCredentials),
			want:   true,
			reason: "no_credentials",
		},
		{
			name:   "wrapped disabled",
			err:    fmt.Errorf("%w: acc-a", accounts.ErrAccountDisabled),
			want:   true,
			reason: "disabled",
		},
		{
			name:   "wrapped not found",
			err:    fmt.Errorf("%w: acc-a", accounts.ErrAccountNotFound),
			want:   true,
			reason: "not_found",
		},
		{
			name:   "site mismatch",
			err:    errors.New("account acc-a site mismatch: global != domestic"),
			want:   true,
			reason: "site_mismatch",
		},
		{
			name: "plain 429 is not pin-unusable",
			err:  errors.New("429 too many requests"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPinnedAccountUnusable(tc.err); got != tc.want {
				t.Fatalf("isPinnedAccountUnusable(%v)=%v want %v", tc.err, got, tc.want)
			}
			if got := pinnedAccountUnusableReason(tc.err); got != tc.reason {
				t.Fatalf("pinnedAccountUnusableReason(%v)=%q want %q", tc.err, got, tc.reason)
			}
		})
	}
}

func TestCompleteFromPoolPinnedAccountHealsAfterDisabled(t *testing.T) {
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
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}
	svc.Pins.Remember(SessionPinKey("conv-disabled", "domestic"), a.ID)
	if _, _, err := svc.Pool.SetEnabled(a.ID, false); err != nil {
		t.Fatal(err)
	}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-disabled",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("expected disabled pin to self-heal, got %v", err)
	}
	if result.AccountID != b.ID {
		t.Fatalf("AccountID=%s want %s after disable", result.AccountID, b.ID)
	}
	if pinned, ok := svc.Pins.Lookup(SessionPinKey("conv-disabled", "domestic")); !ok || pinned == a.ID {
		t.Fatalf("pin=%q ok=%v must not still point at disabled %s", pinned, ok, a.ID)
	}
}

func TestCompleteFromPoolPinnedAccountHealsAfterDeleted(t *testing.T) {
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
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}
	svc.Pins.Remember(SessionPinKey("conv-deleted", "domestic"), a.ID)
	if _, err := svc.Pool.Delete(a.ID); err != nil {
		t.Fatal(err)
	}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-deleted",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("expected deleted pin to self-heal, got %v", err)
	}
	if result.AccountID != b.ID {
		t.Fatalf("AccountID=%s want %s after delete", result.AccountID, b.ID)
	}
	if pinned, ok := svc.Pins.Lookup(SessionPinKey("conv-deleted", "domestic")); !ok || pinned == a.ID {
		t.Fatalf("pin=%q ok=%v must not still point at deleted %s", pinned, ok, a.ID)
	}
}

func TestCompleteFromPoolPinnedDisabledLastAccountReturnsPoolError(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	a, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "only", Site: "domestic", BearerToken: "token-only", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}
	svc.Pins.Remember(SessionPinKey("conv-last", "domestic"), a.ID)
	if _, _, err := svc.Pool.SetEnabled(a.ID, false); err != nil {
		t.Fatal(err)
	}

	_, err = svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-last",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatal("expected pool error when no other account remains")
	}
	if errors.Is(err, errRetryDepthExceeded) {
		t.Fatalf("must not recurse to retry-depth exceeded, got %v", err)
	}
	if !errors.Is(err, accounts.ErrAccountDisabled) && !errors.Is(err, accounts.ErrNoAccounts) {
		t.Fatalf("want original pool error, got %v", err)
	}
	if got := openai.ClassifyCause(err); got != "pool_state" {
		t.Fatalf("ClassifyCause=%q want pool_state", got)
	}
}

func TestResolveFailureCooldownHonors6004ResetTime(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{}

	localReset := time.Now().In(loc).Add(3 * time.Hour).Truncate(time.Second)
	localErr := fmt.Errorf("failed with 429: 6004 rate-model will reset at %s", localReset.Format("2006-01-02 15:04:05"))
	got := svc.resolveFailureCooldown(context.Background(), accounts.Account{}, localErr)
	want := time.Until(localReset)
	if got <= 0 || got > 48*time.Hour {
		t.Fatalf("6004 local cooldown=%s want (0, 48h]", got)
	}
	if diff := got - want; diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("6004 local cooldown=%s want ~%s (parsed %s)", got, want, localReset.Format(time.RFC3339))
	}

	rfcReset := time.Now().Add(90 * time.Minute).Truncate(time.Second)
	rfcErr := fmt.Errorf("failed with 429: 6004 will reset at %s", rfcReset.UTC().Format(time.RFC3339))
	got = svc.resolveFailureCooldown(context.Background(), accounts.Account{}, rfcErr)
	want = time.Until(rfcReset)
	if diff := got - want; diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("6004 rfc3339 cooldown=%s want ~%s", got, want)
	}

	farErr := errors.New("failed with 429: 6004 rate-model will reset at 2099-01-01 00:00:00")
	got = svc.resolveFailureCooldown(context.Background(), accounts.Account{}, farErr)
	if got != 48*time.Hour {
		t.Fatalf("6004 far-future cooldown=%s want 48h clamp", got)
	}

	chatErr := errors.New("CodeBuddy chat completion failed with 429: 6004 rate-model will reset at 2099-01-01 00:00:00 UTC+8 [region=global site=global endpoint=https://www.codebuddy.ai/v2/chat/completions domain=www.codebuddy.ai model=DS-V4.1-Flash]")
	got = svc.resolveFailureCooldown(context.Background(), accounts.Account{}, chatErr)
	if got != 48*time.Hour {
		t.Fatalf("6004 ChatError envelope cooldown=%s want 48h clamp, not 2m", got)
	}

	utc8BeforeRegion := errors.New("failed with 429: 6004 rate-model will reset at 2099-01-01 00:00:00 UTC+8 [region=global site=global]")
	got = svc.resolveFailureCooldown(context.Background(), accounts.Account{}, utc8BeforeRegion)
	if got != 48*time.Hour {
		t.Fatalf("6004 UTC+8 before [region=] cooldown=%s want 48h clamp, not 2m", got)
	}
}

func TestResolveFailureCooldown6004RealWorldTail(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{}
	reset := time.Now().In(loc).Add(13 * time.Hour).Truncate(time.Second)
	// 生产实收文案：时间戳后紧跟 ", alternatively…"，旧截断集（\n;)][）拦不住它。
	realWorld := fmt.Errorf("CodeBuddy chat completion failed with 429: usage exceeds frequency limit, but don't worry, your usage will reset at %s UTC+8, alternatively, you can switch to the other models to continue using it. (code 6004) [region=global site=global endpoint=https://www.workbuddy.ai/v2/chat/completions domain=www.workbuddy.ai model=deepseek-v4.1-flash]", reset.Format("2006-01-02 15:04:05"))
	got := svc.resolveFailureCooldown(context.Background(), accounts.Account{}, realWorld)
	if got == 2*time.Minute {
		t.Fatalf("real-world 6004 fell back to fixed 2m cooldown; parse miss")
	}
	want := time.Until(reset)
	if diff := got - want; diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("real-world 6004 cooldown=%s want ~%s", got, want)
	}
}

func TestResolveFailureCooldown6004WithoutTimeStaysTwoMinutes(t *testing.T) {
	svc := &Service{}
	got := svc.resolveFailureCooldown(context.Background(), accounts.Account{}, errors.New("failed with 429: 6004 rate-model (no reset clock)"))
	if got != 2*time.Minute {
		t.Fatalf("6004 without time cooldown=%s want 2m", got)
	}
}

func TestResolveFailureCooldownQuotaDoesNotUse6004Parser(t *testing.T) {
	svc := &Service{}
	got := svc.resolveFailureCooldown(context.Background(), accounts.Account{}, errors.New("quota exhausted will reset at 2099-01-01 00:00:00"))
	if got == 48*time.Hour {
		t.Fatal("quota exhausted must not take the 6004 error-text cooldown")
	}
}

func TestPoolSelectSkipsUntil6004Reset(t *testing.T) {
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { _ = pool.Close() })
	one, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "one", BearerToken: "token-one", Site: "domestic",
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "two", BearerToken: "token-two", Site: "domestic",
	}))
	if err != nil {
		t.Fatal(err)
	}
	sel, err := pool.Select(accounts.SelectOptions{Site: "domestic", AccountID: one.ID})
	if err != nil {
		t.Fatal(err)
	}

	reset := time.Now().Add(1200 * time.Millisecond)
	msg := fmt.Sprintf("failed with 429: 6004 will reset at %s", reset.Format(time.RFC3339Nano))
	cooldown := (&Service{}).resolveFailureCooldown(context.Background(), sel.Account, errors.New(msg))
	if cooldown <= 0 || cooldown > time.Second+500*time.Millisecond {
		t.Fatalf("expected short parsed cooldown, got %s", cooldown)
	}
	if err := pool.MarkResult(sel, false, msg, cooldown); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		picked, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
		if err != nil {
			t.Fatal(err)
		}
		if picked.Account.ID == one.ID {
			t.Fatalf("expected 6004 cooldown account skipped, got %s on attempt %d", one.ID, i+1)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		picked, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
		if err != nil {
			t.Fatal(err)
		}
		if picked.Account.ID == one.ID {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected account to rejoin pool after 6004 reset")
}

func TestCompleteFromPoolRoutesByModelSitePrefix(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	global, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "global", Site: "global", BearerToken: "token-global", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	domestic, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "domestic", Site: "domestic", BearerToken: "token-domestic", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}

	local, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if local.AccountID != domestic.ID {
		t.Fatalf("default SITE=domestic picked %s want %s", local.AccountID, domestic.ID)
	}

	resolved := ResolveProviderModel("global:auto")
	remote, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    resolved.Model,
		Site:     resolved.Site,
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("global: prefix should pick international account in one process, got %v", err)
	}
	if remote.AccountID != global.ID {
		t.Fatalf("global: prefix picked %s want %s (no second process)", remote.AccountID, global.ID)
	}
}

func TestListModelsOmitsSiteAliasesAndStaysOnOneSite(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "global", Product: "codebuddy", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "global", Site: "global", BearerToken: "token-global", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "domestic", Site: "domestic", BearerToken: "token-domestic", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: catalogByHostTransport{}}

	listed, err := svc.ListModels(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	ids := modelIDs(listed.Models)
	for _, id := range ids {
		if strings.Contains(id, ":") {
			t.Fatalf("GET catalog must not inject site aliases, got %v", ids)
		}
	}
	if !containsID(ids, "deepseek-v4.1-flash") || !containsID(ids, "gpt-5") {
		t.Fatalf("default SITE=global catalog=%v", ids)
	}
	if containsID(ids, "glm-5.3-flash") {
		t.Fatalf("domestic-only model leaked into default catalog: %v", ids)
	}

	cn, err := svc.ListModelsForSite(context.Background(), "domestic", true)
	if err != nil {
		t.Fatal(err)
	}
	cnIDs := modelIDs(cn.Models)
	if containsID(cnIDs, "gpt-5") {
		t.Fatalf("global-only model leaked into domestic catalog: %v", cnIDs)
	}
	if !containsID(cnIDs, "glm-5.3-flash") || !containsID(cnIDs, "deepseek-v4.1-flash") {
		t.Fatalf("domestic catalog=%v", cnIDs)
	}
	if cn.Site != "domestic" {
		t.Fatalf("site=%q", cn.Site)
	}
}

func modelIDs(models []models.Model) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

type catalogByHostTransport struct{}

func (catalogByHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	host := req.URL.Host
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	ids := []string{"deepseek-v4.1-flash", "gpt-5"}
	if token == "token-domestic" || strings.Contains(host, ".cn") {
		ids = []string{"deepseek-v4.1-flash", "glm-5.3-flash"}
	}
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id, "name": id})
	}
	body, _ := json.Marshal(map[string]any{"models": rows})
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}, nil
}

func TestSessionPinsAreIsolatedBySite(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	global, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "global", Site: "global", BearerToken: "token-global", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	domestic, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "domestic", Site: "domestic", BearerToken: "token-domestic", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}
	const session = "same-client-session"
	cn, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", Site: "domestic", SessionKey: session,
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	intl, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", Site: "global", SessionKey: session,
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cn.AccountID != domestic.ID || intl.AccountID != global.ID {
		t.Fatalf("pins leaked across sites: cn=%s intl=%s", cn.AccountID, intl.AccountID)
	}
}

func TestProbeCacheKeyedByProductAndClearedOnSwitch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", filepath.Join(dir, ".env"))
	svc := New(config.Config{Site: "global", Product: "codebuddy", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	now := time.Now().UnixMilli()
	svc.probeCache[svc.probeKey("global")] = provider.Reachability{OK: false, Site: "global", Cause: "upstream_infra", CheckedAt: now}
	got := svc.probeSite(context.Background(), "global", false)
	if got.OK || got.Cause != "upstream_infra" {
		t.Fatalf("expected cached codebuddy failure, got %+v", got)
	}
	svc.updateConfig(func(c *config.Config) { c.Product = "workbuddy" })
	svc.invalidateProbeCache()
	svc.Provider.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("Authorization Required")),
			Request:    req,
		}, nil
	})}
	fresh := svc.probeSite(context.Background(), "global", false)
	if !fresh.OK {
		t.Fatalf("workbuddy should not reuse codebuddy probe cache: %+v", fresh)
	}
}

func TestProbeUpstreamSetsProbed(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "global", Product: "codebuddy", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	svc.Provider.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("Authorization Required")),
			Request:    req,
		}, nil
	})}
	got := svc.ProbeUpstream(context.Background(), true)
	if !got.Probed {
		t.Fatalf("ProbeUpstream must set probed=true for /readyz JSON, got %+v", got)
	}
	if !got.OK {
		t.Fatalf("401 auth layer should be reachable: %+v", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCompleteFromPoolSwitchPicksLiveHighestQuota(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	cachedLow, cachedHigh := 10.0, 100.0
	a, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "b", Site: "domestic", BearerToken: "token-b", Enabled: true, QuotaRemaining: &cachedLow,
	}))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "c", Site: "domestic", BearerToken: "token-c", Enabled: true, QuotaRemaining: &cachedHigh,
	}))
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedChatTransport{
		byAuth:        map[string]int{"token-a": http.StatusTooManyRequests},
		creditsByAuth: map[string]float64{"token-b": 90, "token-c": 5},
	}
	svc.Provider.HTTP = &http.Client{Transport: transport}
	svc.Pins.Remember(SessionPinKey("conv-quota", "domestic"), a.ID)

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "conv-quota",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != b.ID {
		t.Fatalf("live quota should pick B (90) over cached-high C (5 live / 100 cached); got %s (B=%s C=%s)", result.AccountID, b.ID, c.ID)
	}
}

func TestCompleteFromPoolNewSessionDoesNotProbeQuota(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	high, mid := 80.0, 20.0
	checked := time.Now().UnixMilli()
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "high", Site: "domestic", BearerToken: "token-high", Enabled: true, QuotaRemaining: &high, QuotaCheckedAt: checked,
	})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "mid", Site: "domestic", BearerToken: "token-mid", Enabled: true, QuotaRemaining: &mid, QuotaCheckedAt: checked,
	})); err != nil {
		t.Fatal(err)
	}
	transport := &scriptedChatTransport{creditsByAuth: map[string]float64{"token-high": 1, "token-mid": 99}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.Label != "high" {
		t.Fatalf("new session should use cached max, got %s", result.Account.Label)
	}
	_, billing := transport.snapshot()
	if len(billing) != 0 {
		t.Fatalf("new session must not hit billing, got %v", billing)
	}
}

func TestCompleteFromPoolColdPoolProbesQuotaOnce(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "low", Site: "domestic", BearerToken: "token-low", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	high, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "high", Site: "domestic", BearerToken: "token-high", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedChatTransport{creditsByAuth: map[string]float64{"token-low": 10, "token-high": 90}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	first, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "sess-1",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.AccountID != high.ID {
		t.Fatalf("cold pool should probe then pick high, got %s want %s", first.Account.Label, high.Label)
	}
	_, billing1 := transport.snapshot()
	if len(billing1) == 0 {
		t.Fatal("cold pool must probe credits once")
	}

	second, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto", SessionKey: "sess-2",
		Messages: []map[string]any{{"role": "user", "content": "other"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.AccountID != high.ID {
		t.Fatalf("cached quota should keep picking high, got %s", second.Account.Label)
	}
	_, billing2 := transport.snapshot()
	if len(billing2) != len(billing1) {
		t.Fatalf("second new session must not probe again: first=%v second=%v", billing1, billing2)
	}
}

func TestCompleteFromPoolPartialSnapshotProbesMissingAccount(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	cached := 10.0
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "known", Site: "domestic", BearerToken: "token-known", Enabled: true,
		QuotaRemaining: &cached, QuotaCheckedAt: time.Now().UnixMilli(),
	})); err != nil {
		t.Fatal(err)
	}
	fresh, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "fresh", Site: "domestic", BearerToken: "token-fresh", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedChatTransport{creditsByAuth: map[string]float64{"token-known": 10, "token-fresh": 90}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != fresh.ID {
		t.Fatalf("missing snapshot should be probed, got %s want fresh", result.Account.Label)
	}
	_, billing := transport.snapshot()
	if len(billing) != 1 || billing[0] != "token-fresh" {
		t.Fatalf("should only probe the account without snapshot, got %v", billing)
	}
}

func TestCompleteFromPoolStaleSnapshotRefreshesBeforeSelect(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	staleHigh, freshLow := 100.0, 20.0
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "stale", Site: "domestic", BearerToken: "token-stale", Enabled: true,
		QuotaRemaining: &staleHigh, QuotaCheckedAt: time.Now().Add(-10 * time.Minute).UnixMilli(),
	})); err != nil {
		t.Fatal(err)
	}
	thin, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "thin", Site: "domestic", BearerToken: "token-thin", Enabled: true,
		QuotaRemaining: &freshLow, QuotaCheckedAt: time.Now().UnixMilli(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedChatTransport{creditsByAuth: map[string]float64{"token-stale": 5, "token-thin": 20}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != thin.ID {
		t.Fatalf("stale high snapshot should refresh then lose to thin, got %s", result.Account.Label)
	}
	_, billing := transport.snapshot()
	if len(billing) != 1 || billing[0] != "token-stale" {
		t.Fatalf("should only refresh the stale account, got %v", billing)
	}
}

func TestQuotaSnapshotFresh(t *testing.T) {
	now := time.Now().UnixMilli()
	rem := 10.0
	if quotaSnapshotFresh(accounts.Account{QuotaUnlimited: true}, now) != true {
		t.Fatal("unlimited should be fresh")
	}
	if quotaSnapshotFresh(accounts.Account{QuotaRemaining: &rem}, now) {
		t.Fatal("remaining without checkedAt is stale")
	}
	if quotaSnapshotFresh(accounts.Account{QuotaRemaining: &rem, QuotaCheckedAt: now}, now) != true {
		t.Fatal("fresh snapshot")
	}
	if quotaSnapshotFresh(accounts.Account{QuotaRemaining: &rem, QuotaCheckedAt: now - quotaSnapshotTTL.Milliseconds() - 1}, now) {
		t.Fatal("expired snapshot")
	}
}

func TestCompleteFromPoolRetryDepthExceeded(t *testing.T) {
	cfg := config.Config{
		Host:         "127.0.0.1",
		Port:         32126,
		AccountsPath: t.TempDir() + "/accounts.json",
		Site:         "domestic",
	}
	svc := New(cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:      "auto",
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		RetryDepth: defaultMaxAccountRetries,
	})
	if err == nil {
		t.Fatal("expected retry depth error")
	}
	if !errors.Is(err, errRetryDepthExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShouldRetryNextAccount(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, slog.Default())
	sel := accounts.Selection{Account: accounts.Account{ID: "acc-1", RefreshToken: "rt"}}
	base := CompleteOptions{Model: "auto"}

	tests := []struct {
		name string
		err  string
		opts CompleteOptions
		want bool
	}{
		{name: "429", err: "CodeBuddy chat completion failed with 429: too many requests", want: true},
		{name: "503", err: "CodeBuddy chat completion failed with 503: service unavailable", want: true},
		{name: "rate limit text", err: "upstream rate limit exceeded", want: true},
		{name: "quota exhausted", err: "CodeBuddy chat completion failed: quota exhausted", want: true},
		{name: "11140 no retry", err: "request illegal 11140", want: false},
		{name: "11128 no retry", err: "unapproved channel 11128", want: false},
		{name: "11101 no retry", err: "tool_choice unmarshal 11101", want: false},
		{name: "401 refresh not switch", err: "failed with 401: unauthorized", want: false},
		{name: "session-bound account still switches", err: "failed with 429", opts: CompleteOptions{AccountID: "acc-1"}, want: true},
		{name: "already excluded", err: "failed with 429", opts: CompleteOptions{ExcludeIDs: []string{"acc-1"}}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := base
			if tc.opts.AccountID != "" || len(tc.opts.ExcludeIDs) > 0 {
				opts = tc.opts
			}
			got := svc.shouldRetryNextAccount(errors.New(tc.err), sel, opts)
			if got != tc.want {
				t.Fatalf("shouldRetryNextAccount(%q)=%v want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestPoolSelectRoundRobin(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/accounts.json"
	pool := accounts.NewPool(path)
	t.Cleanup(func() { _ = pool.Close() })

	mk := func(label string) accounts.Account {
		return accounts.CreateAccount(accounts.Account{
			Label:       label,
			Site:        "domestic",
			BearerToken: "token-" + label,
			Enabled:     true,
		})
	}
	a1, _, err := pool.Upsert(mk("a1"))
	if err != nil {
		t.Fatal(err)
	}
	a2, _, err := pool.Upsert(mk("a2"))
	if err != nil {
		t.Fatal(err)
	}

	s1, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := pool.Select(accounts.SelectOptions{Site: "domestic"})
	if err != nil {
		t.Fatal(err)
	}
	if s1.Account.ID == s2.Account.ID {
		t.Fatalf("expected round-robin to pick different accounts, got %s twice", s1.Account.ID)
	}

	s3, err := pool.Select(accounts.SelectOptions{Site: "domestic", ExcludeIDs: []string{s1.Account.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if s3.Account.ID != s2.Account.ID && s3.Account.ID != a1.ID && s3.Account.ID != a2.ID {
		t.Fatalf("unexpected account %s", s3.Account.ID)
	}
	if s3.Account.ID == s1.Account.ID {
		t.Fatalf("exclude should skip %s", s1.Account.ID)
	}
}

func TestResolveFailureCooldownSkipsProbeOn429(t *testing.T) {
	svc := &Service{}
	got := svc.resolveFailureCooldown(t.Context(), accounts.Account{}, errors.New("429 too many requests"))
	if got != 2*time.Minute {
		t.Fatalf("resolveFailureCooldown=%s want 2m without probe", got)
	}
}

func TestFailureCooldown(t *testing.T) {
	tests := []struct {
		err  string
		want time.Duration
	}{
		{err: "request illegal 11140", want: 5 * time.Minute},
		{err: "429 too many", want: 2 * time.Minute},
		{err: "503 unavailable", want: 30 * time.Second},
		{err: "context canceled", want: 0},
	}
	for _, tc := range tests {
		got := failureCooldown(errors.New(tc.err))
		if got != tc.want {
			t.Fatalf("failureCooldown(%q)=%s want %s", tc.err, got, tc.want)
		}
	}
}

func TestChatOptionsFromAccountUsesProcessProduct(t *testing.T) {
	svc := New(config.Config{Site: "global", Product: "workbuddy"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	opts := svc.chatOptionsFromAccount(accounts.Account{
		Site:        "global",
		BaseURL:     "https://www.codebuddy.ai",
		APIEndpoint: "https://www.codebuddy.ai/v2/chat/completions",
		BearerToken: "tok",
	}, CompleteOptions{Model: "auto"})
	if opts.Product != "workbuddy" {
		t.Fatalf("product=%q", opts.Product)
	}
	if got := provider.ResolveProtocolDirectEndpoint(opts); got != "https://www.workbuddy.ai/v2/chat/completions" {
		t.Fatalf("endpoint=%s", got)
	}
}

func TestSetPoolProductPersistsAndSwitchesUpstream(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envFile)
	path := filepath.Join(t.TempDir(), "accounts.json")
	svc := New(config.Config{
		Site:         "global",
		Product:      "codebuddy",
		BaseURL:      "https://www.codebuddy.ai",
		AccountsPath: path,
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	payload, err := svc.SetPoolProduct("workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if svc.Config().Product != "workbuddy" {
		t.Fatalf("product=%q", svc.Config().Product)
	}
	if svc.Config().BaseURL != "https://www.workbuddy.ai" {
		t.Fatalf("baseURL=%q", svc.Config().BaseURL)
	}
	if payload["product"] != "workbuddy" && payload["poolProduct"] != "workbuddy" {
		t.Fatalf("payload=%v", payload)
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "CODEBUDDY_PRODUCT=workbuddy") {
		t.Fatalf("env file missing product: %s", text)
	}
	if !strings.Contains(text, "CODEBUDDY_BASE_URL=https://www.workbuddy.ai") {
		t.Fatalf("env file missing workbuddy base: %s", text)
	}

	if _, err := svc.SetPoolSite("domestic"); err != nil {
		t.Fatal(err)
	}
	if svc.Config().Product != "workbuddy" {
		t.Fatalf("switching site must keep product, got %q", svc.Config().Product)
	}
	if svc.Config().BaseURL != "https://www.workbuddy.cn" {
		t.Fatalf("domestic workbuddy base=%q", svc.Config().BaseURL)
	}
}

func BenchmarkResolveProviderModel(b *testing.B) {
	models := []string{"auto", "codebuddy:deepseek-v4", "codebuddy/deepseek-v4-flash", "deepseek-v4"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ResolveProviderModel(models[i%len(models)])
	}
}
