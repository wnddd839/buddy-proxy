package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/concurrency"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// 这些测试固定住「埋点接在哪」这件事：
// 只要有人把 recordRateLimit 的判定改坏，这里就会失败。

func newConcurrencyTestService(t *testing.T) *Service {
	t.Helper()
	svc := &Service{Concurrency: concurrency.New("", time.Hour)}
	t.Cleanup(func() { _ = svc.Concurrency.Close() })
	return svc
}

// newWiredTestService 返回带真实号池与并发观测的 Service，用于行为测试。
func newWiredTestService(t *testing.T) *Service {
	t.Helper()
	svc := New(config.Config{Site: "domestic", AccountsPath: filepath.Join(t.TempDir(), "accounts.json")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = svc.Close() })
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Site: "domestic", BearerToken: "a", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRecordRateLimitIgnoresNonRateLimitError(t *testing.T) {
	svc := newConcurrencyTestService(t)

	svc.recordRateLimit(accounts.Account{ID: "a"}, "m", "domestic", errString("connection refused"))
	svc.recordRateLimit(accounts.Account{ID: "a"}, "m", "domestic", errString("11140 request illegal"))
	svc.recordRateLimit(accounts.Account{ID: "a"}, "m", "domestic", nil)

	if got := len(svc.ConcurrencyStatus().RateLimits); got != 0 {
		t.Fatalf("非限流错误不应入账，实际 rateLimits=%d", got)
	}
}

func TestRecordRateLimitDetectsPlain429(t *testing.T) {
	svc := newConcurrencyTestService(t)

	svc.recordRateLimit(accounts.Account{ID: "a", Label: "账号A"}, "m", "domestic",
		errString("upstream returned 429 Too Many Requests"))
	events := svc.ConcurrencyStatus().RateLimits
	if len(events) != 1 {
		t.Fatalf("普通 429 应入账，rateLimits=%d want 1", len(events))
	}
	if events[0].AccountID != "a" || events[0].Label != "账号A" {
		t.Fatalf("事件字段=%+v", events[0])
	}
}

func TestRecordRateLimitDetectsCode6004(t *testing.T) {
	svc := newConcurrencyTestService(t)

	svc.recordRateLimit(accounts.Account{ID: "a"}, "m", "domestic",
		errString("CodeBuddy chat completion failed with 429: 您的使用量已超出频率限制 (code 6004)"))
	if got := len(svc.ConcurrencyStatus().RateLimits); got != 1 {
		t.Fatalf("code 6004 应入账，rateLimits=%d want 1", got)
	}
}

func TestConcurrencyStatusNilServiceIsSafe(t *testing.T) {
	var svc *Service
	snap := svc.ConcurrencyStatus()
	if snap.GeneratedAt == 0 {
		t.Fatalf("nil service 应返回带时间戳的空快照")
	}
	if got := svc.ConcurrencySummary(); len(got) == 0 {
		t.Fatalf("nil service 的汇总视图不应为空")
	}
}

func TestConcurrencyStatusNilTrackerIsSafe(t *testing.T) {
	svc := &Service{}
	snap := svc.ConcurrencyStatus()
	if snap.GeneratedAt == 0 {
		t.Fatalf("nil tracker 应返回带时间戳的空快照")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// 行为验证：成功与失败路径都必须把 in-flight 归零（Begin/End 必须成对）。
// 这类泄漏在线上表现为在途数单调上涨，靠真实请求很难稳定复现。
func TestCompleteFromPoolAlwaysEndsFlight(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "success", status: http.StatusOK},
		{name: "permanent-error", status: http.StatusTeapot, body: `{"error":"teapot"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newWiredTestService(t)
			svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{
				byAuth:          map[string]int{"a": tc.status},
				errorBodyByAuth: map[string]string{"a": tc.body},
			}}
			_, _ = svc.CompleteFromPool(t.Context(), CompleteOptions{
				Model:    "auto",
				Messages: []map[string]any{{"role": "user", "content": "hi"}},
			})
			if got := svc.ConcurrencyStatus().GlobalInFlight; got != 0 {
				t.Fatalf("in-flight 泄漏: %d", got)
			}
		})
	}
}

// 行为验证：chat / image / search 三条上游路径都必须计入并发观测。
// 只要某条路径漏了 Begin，totals.requests 就不会增长。
func TestAllUpstreamPathsRecordUsage(t *testing.T) {
	svc := newWiredTestService(t)
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}

	assertRequestsGrow := func(label string, call func()) {
		t.Helper()
		before := svc.ConcurrencyStatus().Totals.Requests
		call()
		after := svc.ConcurrencyStatus().Totals.Requests
		if after <= before {
			t.Fatalf("%s 路径未计入并发观测: requests %d -> %d", label, before, after)
		}
		if got := svc.ConcurrencyStatus().GlobalInFlight; got != 0 {
			t.Fatalf("%s 路径 in-flight 泄漏: %d", label, got)
		}
	}

	assertRequestsGrow("chat", func() {
		_, _ = svc.CompleteFromPool(t.Context(), CompleteOptions{
			Model:    "auto",
			Messages: []map[string]any{{"role": "user", "content": "hi"}},
		})
	})
	assertRequestsGrow("image", func() {
		_, _ = svc.ImageFromPool(t.Context(), ImageOptions{Model: "image", Prompt: "p"})
	})
	assertRequestsGrow("search", func() {
		_, _, _ = svc.WebSearchFromPool(t.Context(), SearchOptions{}, provider.WebSearchRequest{Query: "q"})
	})
}

// 行为验证：429 现场必须在 endFlight 之前记录，否则读到的并发永远减掉了本次请求。
func TestRateLimitRecordedBeforeFlightEnds(t *testing.T) {
	svc := newWiredTestService(t)
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{
		byAuth:          map[string]int{"a": http.StatusTooManyRequests},
		errorBodyByAuth: map[string]string{"a": `{"error":"429 too many requests"}`},
	}}
	_, _ = svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model:    "auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	events := svc.ConcurrencyStatus().RateLimits
	if len(events) == 0 {
		t.Fatal("429 未记录")
	}
	ev := events[len(events)-1]
	if ev.AccountInFlight < 1 {
		t.Fatalf("429 记录发生在请求结束之后: accountInFlight=%d", ev.AccountInFlight)
	}
}
