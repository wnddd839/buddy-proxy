package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestFixedKeyAcrossChatAndResponses(t *testing.T) {
	srv := testServerCfg(t, config.Config{RequireAPIKey: true, APIKey: "main", Site: "global"})
	account, _, err := srv.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Site: "domestic", Label: "fixed", BearerToken: "token-fixed", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	srv.Svc.SetAPIKeys([]config.APIKeyBinding{{Key: "fixed", AccountID: account.ID}})
	transport := &recordingUpstreamTransport{}
	srv.Svc.Provider.HTTP = &http.Client{Transport: transport}
	for _, test := range []struct {
		path, body string
		want       int
	}{
		{"/v1/chat/completions", `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`, 200},
		{"/v1/responses", `{"model":"auto","input":"hi"}`, 200},
		{"/v1/responses", `{"model":"global:auto","input":"hi"}`, 400},
		{"/v1/chat/completions", `{"model":"global:auto","messages":[{"role":"user","content":"hi"}]}`, 400},
		{"/v1/images/generations", `{"model":"global:auto","prompt":"hi"}`, 400},
	} {
		req := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		req.Header.Set("Authorization", "Bearer fixed")
		rec := httptest.NewRecorder()
		srv.HTTP.Handler.ServeHTTP(rec, req)
		if rec.Code != test.want {
			t.Fatalf("%s status=%d: %s", test.path, rec.Code, rec.Body.String())
		}
	}
	for _, token := range transport.chatTokens() {
		if token != "token-fixed" {
			t.Fatal("请求使用了其他账号")
		}
	}
	if _, _, err := srv.Svc.Pool.SetEnabled(account.ID, false); err != nil {
		t.Fatal(err)
	}
	disabled := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"auto","input":"hi"}`))
	disabled.Header.Set("Authorization", "Bearer fixed")
	disabledResponse := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(disabledResponse, disabled)
	if disabledResponse.Code != http.StatusServiceUnavailable {
		t.Fatal("禁用账号未明确失败")
	}
	if _, err := srv.Svc.Pool.Delete(account.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer fixed")
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("账号删除后未明确失败: %d", rec.Code)
	}
}

func TestCreateAccountBindingPersistsAndReloads(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "proxy.env")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envPath)
	srv := testServer(t, true, "", "main")
	account, _, err := srv.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Site: "domestic", BearerToken: "fixture", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"accountId": account.ID})
	req := httptest.NewRequest("POST", "/direct-admin/api/client-config/bound-keys", strings.NewReader(string(raw)))
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("创建失败: %d", rec.Code)
	}
	keys := srv.Svc.Config().APIKeys
	if len(keys) != 1 || keys[0].AccountID != account.ID {
		t.Fatal("运行时绑定丢失")
	}
	env, err := os.ReadFile(envPath)
	if err != nil || !strings.Contains(string(env), ":account="+account.ID) {
		t.Fatal("绑定未落盘")
	}
	reloaded := config.ParseAPIKeys(config.FormatAPIKeys(keys))
	if len(reloaded) != 1 || reloaded[0].AccountID != account.ID {
		t.Fatal("重启后的绑定约束丢失")
	}
	// 相同账号显式指定冲突区域时，不应创建第二把 Key。
	raw, _ = json.Marshal(map[string]any{"accountId": account.ID, "site": "global"})
	req = httptest.NewRequest("POST", "/direct-admin/api/client-config/bound-keys", strings.NewReader(string(raw)))
	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != 400 || len(srv.Svc.Config().APIKeys) != 1 {
		t.Fatal("区域冲突未拒绝或产生了配置副作用")
	}
}

// 生成绑定 Key 是「读 APIKeys → 追加 → 落盘 + 写内存」的读改写。
// 两个并发请求若各拿一份旧快照，后落盘的会把前一个绑定静默覆盖掉，
// 而两边都收到 200 —— 用户看到的新 Key 其实已经不在配置里了。
//
// 不靠并发时序赌概率：直接持住 envMu，断言请求会等这把锁。
// 没加锁的实现会在持锁期间就写完（本测试会立刻失败）。
func TestBoundKeyCreateWaitsForEnvLock(t *testing.T) {
	srv := testServer(t, true, "", "main")
	account, _, err := srv.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Site: "domestic", BearerToken: "fixture", Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"accountId": account.ID})

	srv.envMu.Lock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/direct-admin/api/client-config/bound-keys", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.HTTP.Handler.ServeHTTP(rec, req)
		done <- rec
	}()

	select {
	case rec := <-done:
		srv.envMu.Unlock()
		t.Fatalf("生成绑定 Key 未受 envMu 保护：持锁期间就完成了写入（%d）", rec.Code)
	case <-time.After(100 * time.Millisecond):
	}

	srv.envMu.Unlock()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("解锁后请求应成功：%d %s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("解锁后请求仍未完成")
	}
}

// 两个并发的「生成绑定 Key」必须都留下。没有 envMu 时它们会读到同一份
// APIKeys 快照，后落盘的覆盖前一个，而两边都返回 200 —— 静默丢绑定。
func TestConcurrentBoundKeyCreatesDoNotLoseBindings(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "proxy.env")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envPath)
	srv := testServer(t, true, "", "main")

	ids := make([]string, 2)
	for i := range ids {
		acc, _, err := srv.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
			Site: "domestic", BearerToken: "fixture", Enabled: true,
		}))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = acc.ID
	}

	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"accountId": ids[i]})
			req := httptest.NewRequest(http.MethodPost,
				"/direct-admin/api/client-config/bound-keys", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.HTTP.Handler.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("并发创建失败：%v", codes)
	}
	if got := len(srv.Svc.Config().APIKeys); got != 2 {
		t.Fatalf("并发创建丢失绑定：期望 2 条，实际 %d 条", got)
	}
}
