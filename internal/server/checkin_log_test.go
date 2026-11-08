package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/checkin"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
)

// checkinTestServer 造一个最小可用的 Server，只用来测账号列表与签到端点。
func checkinTestServer(t *testing.T, accts []accounts.Account) (*Server, *checkin.Log, string) {
	t.Helper()
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "proxy-accounts.json")
	raw, err := json.Marshal(accounts.Store{Accounts: accts})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accountsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{AccountsPath: accountsPath}
	svc := gateway.New(cfg, slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	srv := New(cfg, svc)
	logStore := srv.Checkin.Log()
	if logStore == nil {
		t.Fatal("Server.Checkin.Log() 不应为 nil")
	}
	return srv, logStore, dir
}

func checkinAccount(id string) accounts.Account {
	return accounts.Account{
		ID:          id,
		Site:        "domestic",
		Enabled:     true,
		BearerToken: "tok",
		AuthStatus:  accounts.AuthStatus{UserID: "u-" + id},
	}
}

func doAdminGet(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.handleAdminAPIEntry(rec, req)
	return rec
}

func TestCheckinLogEndpointReturnsEntries(t *testing.T) {
	srv, logStore, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})

	day := time.Now().In(checkinTimezone()).Format("2006-01-02")
	for i, ok := range []bool{false, true} {
		if err := logStore.Append(checkin.Entry{
			AccountID: "a1", Day: day, At: time.Now().Add(time.Duration(i) * time.Second).UnixMilli(),
			OK: ok, Credits: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}

	rec := doAdminGet(t, srv, "/direct-admin/api/checkin-log")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK      bool            `json:"ok"`
		Entries []checkin.Entry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatal("ok 应为 true")
	}
	if len(body.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(body.Entries))
	}
}

func TestAccountSummaryIncludesCheckinFields(t *testing.T) {
	srv, logStore, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	day := time.Now().In(checkinTimezone()).Format("2006-01-02")
	if err := logStore.Append(checkin.Entry{
		AccountID: "a1", Day: day, At: time.Now().UnixMilli(), OK: true, Credits: 100,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminGet(t, srv, "/direct-admin/api/codebuddy/accounts")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Accounts []accounts.Summary `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(body.Accounts))
	}
	got := body.Accounts[0]
	if got.CheckinToday != "done" {
		t.Fatalf("checkinToday want done, got %q", got.CheckinToday)
	}
	if got.CheckinCount48h != 1 {
		t.Fatalf("checkinCount48h want 1, got %d", got.CheckinCount48h)
	}
}

func TestAccountSummaryCheckinNoneWhenNoEntry(t *testing.T) {
	srv, _, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})

	rec := doAdminGet(t, srv, "/direct-admin/api/codebuddy/accounts")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body struct {
		Accounts []accounts.Summary `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Accounts[0]
	if got.CheckinToday != "none" {
		t.Fatalf("checkinToday want none, got %q", got.CheckinToday)
	}
	if got.CheckinCount48h != 0 {
		t.Fatalf("checkinCount48h want 0, got %d", got.CheckinCount48h)
	}
}

func TestAccountSummaryCheckinFailedCarriesMessage(t *testing.T) {
	srv, logStore, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	day := time.Now().In(checkinTimezone()).Format("2006-01-02")
	if err := logStore.Append(checkin.Entry{
		AccountID: "a1", Day: day, At: time.Now().UnixMilli(),
		OK: false, Message: "checkin status failed: boom",
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminGet(t, srv, "/direct-admin/api/codebuddy/accounts")
	var body struct {
		Accounts []accounts.Summary `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Accounts[0]
	if got.CheckinToday != "failed" {
		t.Fatalf("checkinToday want failed, got %q", got.CheckinToday)
	}
	if got.CheckinLastErr == "" {
		t.Fatal("失败时 checkinLastError 应带上原因")
	}
}

// 签到日志里的孤儿记录（账号已从池里删掉）不能让列表 500 或 panic。
func TestAccountSummaryIgnoresOrphanCheckinEntries(t *testing.T) {
	srv, logStore, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	day := time.Now().In(checkinTimezone()).Format("2006-01-02")
	if err := logStore.Append(checkin.Entry{
		AccountID: "ghost", Day: day, At: time.Now().UnixMilli(), OK: true,
	}); err != nil {
		t.Fatal(err)
	}

	rec := doAdminGet(t, srv, "/direct-admin/api/codebuddy/accounts")
	if rec.Code != http.StatusOK {
		t.Fatalf("孤儿记录不该导致 %d", rec.Code)
	}
	var body struct {
		Accounts []accounts.Summary `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(body.Accounts))
	}
	// a1 没签到记录，不应因为 ghost 的记录而被算上
	if body.Accounts[0].CheckinCount48h != 0 {
		t.Fatalf("孤儿记录不应算到 a1 头上，got %d", body.Accounts[0].CheckinCount48h)
	}
}
