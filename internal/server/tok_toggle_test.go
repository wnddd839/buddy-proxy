package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 开关端点：POST 切到关 → 基线端点返回 enabled=false → 切回开。
func TestTokFingerprintToggle(t *testing.T) {
	srv, _ := tokBaselineTestServer(t)

	readEnabled := func() bool {
		rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/tok-baselines", "")
		var got struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Enabled == nil {
			t.Fatal("必须返回 enabled 字段")
		}
		return *got.Enabled
	}

	if !readEnabled() {
		t.Fatal("默认应为开启")
	}

	// 关掉
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/tok-fingerprint", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("关闭失败 %d: %s", rec.Code, rec.Body.String())
	}
	if readEnabled() {
		t.Fatal("关闭后 enabled 应为 false")
	}

	// 再打开
	rec = doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/tok-fingerprint", `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("开启失败 %d", rec.Code)
	}
	if !readEnabled() {
		t.Fatal("开启后 enabled 应为 true")
	}
}

// 参数缺失必须 400，而不是默默按默认值处理。
func TestTokFingerprintToggleRequiresEnabled(t *testing.T) {
	srv, _ := tokBaselineTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/tok-fingerprint", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 enabled 应 400，实际 %d", rec.Code)
	}
}

// 关掉后新请求不该写 Tok*（保证 journal 与 decision 两侧一致）。
func TestTokFieldsAreZeroWhenDisabled(t *testing.T) {
	/*
		这条在 gateway 层测（见 internal/gateway 的 tok_switch_test.go）：
		server 层构造不出带 Tok 的 RecordChat 输入。
	*/
}
