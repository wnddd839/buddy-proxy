package gateway

import (
	"context"
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
)

func newAltPool(t *testing.T) *accounts.Pool {
	t.Helper()
	pool := accounts.NewPool(filepath.Join(t.TempDir(), "accounts.json"))
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func addAltAccount(t *testing.T, pool *accounts.Pool, label, site, token string) accounts.Account {
	t.Helper()
	account, _, err := pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label:       label,
		Site:        site,
		BearerToken: token,
		Enabled:     true,
	}))
	if err != nil {
		t.Fatalf("upsert %s: %v", label, err)
	}
	return account
}

func editAltAccount(t *testing.T, pool *accounts.Pool, id string, mutate func(*accounts.Account)) {
	t.Helper()
	if _, _, err := pool.UpdateAccount(id, func(account accounts.Account) accounts.Account {
		mutate(&account)
		return account
	}); err != nil {
		t.Fatalf("update %s: %v", id, err)
	}
}

func TestHasAlternativeAccount(t *testing.T) {
	tests := []struct {
		name string
		site string
		// setup 在当前账号之外追加候选；返回值作为 exclude 透传。
		setup func(t *testing.T, pool *accounts.Pool) []string
		want  bool
	}{
		{
			name:  "仅当前账号",
			site:  "global",
			setup: func(*testing.T, *accounts.Pool) []string { return nil },
			want:  false,
		},
		{
			name: "第二个同站点账号可用",
			site: "global",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				addAltAccount(t, pool, "second", "global", "token-second")
				return nil
			},
			want: true,
		},
		{
			name: "第二个账号被禁用",
			site: "global",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				second := addAltAccount(t, pool, "second", "global", "token-second")
				editAltAccount(t, pool, second.ID, func(a *accounts.Account) { a.Enabled = false })
				return nil
			},
			want: false,
		},
		{
			name: "第二个账号冷却中",
			site: "global",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				second := addAltAccount(t, pool, "second", "global", "token-second")
				editAltAccount(t, pool, second.ID, func(a *accounts.Account) {
					a.CooldownUntil = time.Now().Add(time.Hour).UnixMilli()
				})
				return nil
			},
			want: false,
		},
		{
			name: "第二个账号冷却已过期",
			site: "global",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				second := addAltAccount(t, pool, "second", "global", "token-second")
				editAltAccount(t, pool, second.ID, func(a *accounts.Account) {
					a.CooldownUntil = time.Now().Add(-time.Minute).UnixMilli()
				})
				return nil
			},
			want: true,
		},
		{
			name: "第二个账号额度耗尽未到重置时间",
			site: "global",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				second := addAltAccount(t, pool, "second", "global", "token-second")
				editAltAccount(t, pool, second.ID, func(a *accounts.Account) {
					zero := 0.0
					a.QuotaRemaining = &zero
					a.QuotaResetAt = time.Now().Add(time.Hour).UnixMilli()
					a.QuotaCheckedAt = time.Now().UnixMilli()
				})
				return nil
			},
			want: false,
		},
		{
			name: "第二个账号在另一站点",
			site: "global",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				addAltAccount(t, pool, "domestic", "domestic", "token-domestic")
				return nil
			},
			want: false,
		},
		{
			name: "候选已列在 exclude 里",
			site: "global",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				second := addAltAccount(t, pool, "second", "global", "token-second")
				return []string{second.ID}
			},
			want: false,
		},
		{
			name: "站点别名归一化后再比较",
			site: "intl",
			setup: func(t *testing.T, pool *accounts.Pool) []string {
				addAltAccount(t, pool, "second", "international", "token-second")
				return nil
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := newAltPool(t)
			current := addAltAccount(t, pool, "current", tc.site, "token-current")
			exclude := tc.setup(t, pool)
			svc := &Service{Pool: pool}
			if got := svc.hasAlternativeAccount(tc.site, current.ID, exclude); got != tc.want {
				t.Fatalf("hasAlternativeAccount(site=%s, current=%s, exclude=%v)=%v want %v",
					tc.site, current.ID, exclude, got, tc.want)
			}
		})
	}
}

func TestHasAlternativeAccountWithoutPool(t *testing.T) {
	var nilSvc *Service
	if nilSvc.hasAlternativeAccount("global", "id", nil) {
		t.Fatal("nil service must not report an alternative account")
	}
	if (&Service{}).hasAlternativeAccount("global", "id", nil) {
		t.Fatal("service without pool must not report an alternative account")
	}
}

// eofOnceTransport 让第一次 chat 请求在响应头之前断开（复现机场出口的
// "Post ...: unexpected EOF"），之后交给 inner 正常应答；其它路径直接透传。
type eofOnceTransport struct {
	inner http.RoundTripper

	mu       sync.Mutex
	attempts int
	failed   bool
}

func (t *eofOnceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "chat/completions") {
		t.mu.Lock()
		t.attempts++
		fail := !t.failed
		t.failed = true
		t.mu.Unlock()
		if fail {
			return nil, io.ErrUnexpectedEOF
		}
	}
	return t.inner.RoundTrip(req)
}

func (t *eofOnceTransport) chatAttempts() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attempts
}

func newTransportRetryService(t *testing.T) *Service {
	t.Helper()
	svc := New(config.Config{Site: "global", AccountsPath: filepath.Join(t.TempDir(), "accounts.json")},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestCompleteFromPoolRetriesSameAccountAfterTransportEOF(t *testing.T) {
	svc := newTransportRetryService(t)
	only, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "only", Site: "global", BearerToken: "token-global", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}

	scripted := &scriptedChatTransport{}
	transport := &eofOnceTransport{inner: scripted}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("同一账号重连应当吃掉一次 transport EOF，实际返回 %v", err)
	}
	if got := transport.chatAttempts(); got != 2 {
		t.Fatalf("chat attempts=%d want 2（首连 EOF + 同号重连）", got)
	}
	if result.AccountID != only.ID {
		t.Fatalf("AccountID=%s want %s（必须还是同一个账号）", result.AccountID, only.ID)
	}
	if result.Turn.Text != "ok" {
		t.Fatalf("text=%q want ok", result.Turn.Text)
	}
	chat, _ := scripted.snapshot()
	if len(chat) != 1 || chat[0] != "token-global" {
		t.Fatalf("inner chat calls=%v want [token-global]", chat)
	}
}

func TestCompleteFromPoolSwitchesAccountWhenAlternativeExists(t *testing.T) {
	svc := newTransportRetryService(t)
	first, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "first", Site: "global", BearerToken: "token-first", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "second", Site: "global", BearerToken: "token-second", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}

	scripted := &scriptedChatTransport{}
	transport := &eofOnceTransport{inner: scripted}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	result, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("有备用账号时 transport EOF 应换号重试，实际返回 %v", err)
	}
	if result.AccountID == first.ID {
		t.Fatalf("首连 EOF 后应换到另一账号，仍是 %s", first.ID)
	}
	if result.AccountID != second.ID {
		t.Fatalf("AccountID=%s want %s", result.AccountID, second.ID)
	}
	chat, _ := scripted.snapshot()
	if len(chat) != 1 || chat[0] != "token-second" {
		t.Fatalf("inner chat calls=%v want [token-second]", chat)
	}
}
