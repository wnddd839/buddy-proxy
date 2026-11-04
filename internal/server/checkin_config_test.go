package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 端对端：管理台保存每账号签到规则 → 写文件 → 读回；非法时间被拒绝。
func TestAdminCheckinConfigRoundTrip(t *testing.T) {
	srv := testServer(t, false, "", "")
	post := func(body string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32126/direct-admin/api/checkin-config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:32126")
		req.Host = "127.0.0.1:32126"
		rec := httptest.NewRecorder()
		srv.HTTP.Handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(`{"auto":true,"rules":[{"accountId":"a1","enabled":true,"start":"09:00","end":"11:00"},{"accountId":"a2","enabled":false,"start":"23:30"}]}`); code != http.StatusOK {
		t.Fatalf("save status=%d", code)
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/api/checkin-config", nil)
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK    bool `json:"ok"`
		Auto  bool `json:"auto"`
		Rules []struct {
			AccountID string `json:"accountId"`
			Enabled   bool   `json:"enabled"`
			Start     string `json:"start"`
			End       string `json:"end"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.Auto || len(got.Rules) != 2 || got.Rules[0].End != "11:00" || got.Rules[1].Enabled {
		t.Fatalf("roundtrip=%s", rec.Body.String())
	}
	if code := post(`{"auto":true,"rules":[{"accountId":"a1","enabled":true,"start":"09:00","end":"08:00"}]}`); code != http.StatusBadRequest {
		t.Fatalf("反向窗口应被拒绝: status=%d", code)
	}
	// 拒绝后原规则仍在，不会被半成品覆盖。
	rec = httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:32126/direct-admin/api/checkin-config", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Rules) != 2 {
		t.Fatalf("规则被非法写入破坏: %s err=%v", rec.Body.String(), err)
	}
}
