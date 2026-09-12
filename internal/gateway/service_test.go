package gateway

import (
	"context"
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
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestResolveProviderModel(t *testing.T) {
	tests := []struct {
		in     string
		model  string
		public string
	}{
		{"", "auto", "auto"},
		{"codebuddy", "auto", "auto"},
		{"codebuddy:deepseek-v4", "deepseek-v4", "deepseek-v4"},
		{"codebuddy/deepseek-v4", "deepseek-v4", "deepseek-v4"},
		{"deepseek-v4-flash", "deepseek-v4-flash", "deepseek-v4-flash"},
	}
	for _, tc := range tests {
		got := ResolveProviderModel(tc.in)
		if got.Model != tc.model || got.PublicModel != tc.public {
			t.Fatalf("ResolveProviderModel(%q) = %+v, want model=%q public=%q", tc.in, got, tc.model, tc.public)
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
	svc.Pins.Remember("conv-sticky", a.ID)

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
	if _, still := svc.Pins.Lookup("conv-sticky"); still && result.AccountID == a.ID {
		t.Fatal("failed pin must not keep the exhausted account")
	}
}

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
	svc.Pins.Remember("conv-quota", a.ID)

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
