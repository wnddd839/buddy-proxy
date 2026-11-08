package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

type scriptedImageTransport struct {
	mu            sync.Mutex
	imageCalls    []string
	imagePaths    []string
	billingCalls  []string
	refreshCalls  int
	byAuth        map[string]int
	creditsByAuth map[string]float64
}

func (t *scriptedImageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	path := req.URL.Path
	t.mu.Lock()
	defer t.mu.Unlock()

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	if strings.Contains(path, "get-user-resource") {
		t.billingCalls = append(t.billingCalls, token)
		remaining, ok := t.creditsByAuth[token]
		body := `{"code":1,"msg":"no fixture"}`
		if ok {
			body = fmt.Sprintf(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CapacityRemain":%g,"CapacitySize":1000,"CapacityUsed":0,"CapacityType":1}]}}}}`, remaining)
		}
		return jsonResponse(req, http.StatusOK, header, body)
	}
	if strings.Contains(path, "get-dosage-notify") {
		return jsonResponse(req, http.StatusOK, header, `{"code":0,"data":{"dosageNotifyCode":0}}`)
	}
	if strings.Contains(path, "token/refresh") {
		t.refreshCalls++
		return jsonResponse(req, http.StatusOK, header, `{"code":0,"data":{"accessToken":"token-new","refreshToken":"rt-new","expiresIn":3600}}`)
	}

	t.imageCalls = append(t.imageCalls, token)
	t.imagePaths = append(t.imagePaths, path)
	status := t.byAuth[token]
	if status == 0 {
		status = http.StatusOK
	}
	if status >= 200 && status < 300 {
		header.Set("Content-Type", "text/event-stream")
		return jsonResponse(req, status, header, `{"code":0,"msg":"OK","data":{"created":1,"data":[{"url":"https://example.test/out.png"}]}}`)
	}
	return jsonResponse(req, status, header, `{"error":"too many requests"}`)
}

func (t *scriptedImageTransport) snapshot() (images []string, refresh int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string{}, t.imageCalls...), t.refreshCalls
}

func jsonResponse(req *http.Request, status int, header http.Header, body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func newImageService(t *testing.T, transport *scriptedImageTransport) *Service {
	t.Helper()
	svc := New(config.Config{Site: "domestic", AccountsPath: t.TempDir() + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })
	client := &http.Client{Transport: transport}
	svc.Provider.HTTP = client
	svc.OAuth.HTTP = client
	return svc
}

func TestImageFromPoolDoesNotSwitchAccountOn429(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
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
		"token-b": http.StatusOK,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	// 图片请求的站内换号重试同样已删除。
	_, err := svc.ImageFromPool(context.Background(), ImageOptions{
		Model:  "hunyuan-image-alpha",
		Prompt: "a cat",
	})
	if err == nil {
		t.Fatal("429 应直接返回错误，不应换号")
	}
	calls, _ := transport.snapshot()
	if len(calls) != 1 {
		t.Fatalf("只应调用一次，实际 %v", calls)
	}
}
func TestImageFromPoolPreserves429(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
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

	_, err := svc.ImageFromPool(context.Background(), ImageOptions{
		Model:  "hunyuan-image-alpha",
		Prompt: "a cat",
	})
	if err == nil {
		t.Fatal("expected 429")
	}
	calls, _ := transport.snapshot()
	if len(calls) != 1 {
		t.Fatalf("calls=%d want 1 (不换号), got %v", len(calls), calls)
	}
}
func TestImageFromPoolRefreshesSameAccountAfter401(t *testing.T) {
	transport := &scriptedImageTransport{byAuth: map[string]int{
		"token-old": http.StatusUnauthorized,
		"token-new": http.StatusOK,
	}}
	svc := newImageService(t, transport)
	acc, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-old", RefreshToken: "rt", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ImageFromPool(context.Background(), ImageOptions{
		Model:  "hunyuan-image-alpha",
		Prompt: "a red circle",
	})
	if err != nil {
		t.Fatalf("expected refresh retry to succeed, got %v", err)
	}
	if result.AccountID != acc.ID {
		t.Fatalf("AccountID=%s want %s", result.AccountID, acc.ID)
	}
	images, refresh := transport.snapshot()
	if refresh != 1 {
		t.Fatalf("refresh calls=%d want 1", refresh)
	}
	if len(images) != 2 || images[0] != "token-old" || images[1] != "token-new" {
		t.Fatalf("image calls=%v want [token-old token-new]", images)
	}
}
