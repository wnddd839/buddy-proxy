package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/billing"
)

// 手动签到（POST /codebuddy/checkin）必须也落日志，
// 否则用户点完按钮后徽章仍显示「未签到」。
func TestManualCheckinWritesLog(t *testing.T) {
	srv, logStore, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})

	// 直接调落盘逻辑，避开真实上游请求
	srv.recordCheckinBatch(billing.CheckinBatchResult{
		OK:       true,
		PoolSite: "domestic",
		Results: []billing.CheckinResult{
			{AccountID: "a1", OK: true, AlreadyCheckedIn: true, RewardCredits: 100, Message: "今日已签到"},
		},
	})

	day := time.Now().In(checkinTimezone()).Format("2006-01-02")
	entry, ok := logStore.Today("a1", day)
	if !ok {
		t.Fatal("手动签到应写日志")
	}
	if !entry.OK || !entry.Already {
		t.Fatalf("entry 应记录 OK+Already，got %+v", entry)
	}
	if entry.Credits != 100 {
		t.Fatalf("Credits want 100, got %d", entry.Credits)
	}
	if entry.Egress != "" {
		t.Fatalf("无出口账号 Egress 应为空，got %q", entry.Egress)
	}
}

// 手动签到结果也要反应到账号摘要里。
func TestManualCheckinReflectedInAccountSummary(t *testing.T) {
	srv, _, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	srv.recordCheckinBatch(billing.CheckinBatchResult{
		OK: true,
		Results: []billing.CheckinResult{
			{AccountID: "a1", OK: true, RewardCredits: 50, Message: ""},
		},
	})

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
	if got.CheckinToday != "done" {
		t.Fatalf("checkinToday want done, got %q", got.CheckinToday)
	}
	if got.CheckinCount48h != 1 {
		t.Fatalf("checkinCount48h want 1, got %d", got.CheckinCount48h)
	}
}

// 空 accountId 的结果（未尝试的账号）不该写入日志。
func TestManualCheckinSkipsEmptyAccountID(t *testing.T) {
	srv, logStore, _ := checkinTestServer(t, []accounts.Account{checkinAccount("a1")})
	srv.recordCheckinBatch(billing.CheckinBatchResult{
		Results: []billing.CheckinResult{
			{AccountID: "", OK: false, Message: "skipped"},
			{AccountID: "a1", OK: true},
		},
	})
	entries, err := logStore.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("应只写 1 条（跳过空 accountId），got %d", len(entries))
	}
	if entries[0].AccountID != "a1" {
		t.Fatalf("写入的应是 a1，got %q", entries[0].AccountID)
	}
}

// 出口信息要留痕，便于事后判断是出口问题还是账号问题。
func TestManualCheckinRecordsEgress(t *testing.T) {
	acct := checkinAccount("a1")
	acct.Proxy = "socks5h://user:pass@1.2.3.4:1080"
	srv, logStore, _ := checkinTestServer(t, []accounts.Account{acct})
	srv.recordCheckinBatch(billing.CheckinBatchResult{
		Results: []billing.CheckinResult{{AccountID: "a1", OK: true}},
	})
	day := time.Now().In(checkinTimezone()).Format("2006-01-02")
	entry, ok := logStore.Today("a1", day)
	if !ok {
		t.Fatal("应写日志")
	}
	if entry.Egress == "" {
		t.Fatal("有出口的账号应记录 egress")
	}
}

var _ = httptest.NewRecorder
