package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// DryRun 接受完整 messages（不传 system）时，应原样使用这些 messages。
//
// 用途：预览多轮历史在 fold / strip 下会变成什么样，
// 而不只是「单条 system + 一条 hi」的简单形态。
func TestSystemDryRunAcceptsMessagesOnly(t *testing.T) {
	srv := systemPromptTestServer(t)

	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun",
		`{"messages":[
			{"role":"system","content":"SYS"},
			{"role":"user","content":"u1"},
			{"role":"assistant","content":"a1"},
			{"role":"user","content":"u2"}
		],"mode":"fold"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}

	var got struct {
		Outbound    []map[string]any `json:"outbound"`
		InMessages  int              `json:"inMessages"`
		OutMessages int              `json:"outMessages"`
		Notes       []string         `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.InMessages != 4 {
		t.Fatalf("应原样使用 4 条 messages，实际 %d", got.InMessages)
	}
	if len(got.Outbound) == 0 {
		t.Fatal("outbound 不能为空")
	}
	// fold 档应出现 client_instructions（用 strings.Contains，内容前面有缩进）
	found := false
	for _, m := range got.Outbound {
		if s, ok := m["content"].(string); ok && strings.Contains(s, "<client_instructions>") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fold 档应把 system 折进 user：%+v", got.Outbound)
	}
}

// 只给 messages 时不得因为「没 system」就 400。
func TestSystemDryRunMessagesOnlyNo400(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun",
		`{"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("只给 messages 应 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
}
