package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// system-dryrun 必须给出「最终 outbound + 说明 + 警告」三样，
// 且用的是当前生效配置（不写 .env、不发请求）。
func TestSystemDryRunAPI(t *testing.T) {
	srv := systemPromptTestServer(t)

	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun",
		`{"system":"You are Claude Code, Anthropic's official CLI for Claude."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}

	var got struct {
		OK           bool             `json:"ok"`
		Mode         string           `json:"mode"`
		ModeLabel    string           `json:"modeLabel"`
		KeepSystem   bool             `json:"keepSystem"`
		StripEnabled bool             `json:"stripEnabled"`
		EchoEnabled  bool             `json:"echoEnabled"`
		Outbound     []map[string]any `json:"outbound"`
		InMessages   int              `json:"inMessages"`
		OutMessages  int              `json:"outMessages"`
		InSystemLen  int              `json:"inSystemLen"`
		OutSystemLen int              `json:"outSystemLen"`
		Notes        []string         `json:"notes"`
		Warnings     []string         `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK {
		t.Fatal("ok 应为 true")
	}
	if len(got.Outbound) == 0 {
		t.Fatal("outbound 不能为空 —— 这是预览的核心输出")
	}
	if got.ModeLabel == "" {
		t.Fatal("必须给出档位说明（用户不知道自己当前是哪档）")
	}
	if got.InMessages == 0 {
		t.Fatal("inMessages 必须给出，否则看不到条数变化")
	}
	if got.Notes == nil {
		t.Fatal("notes 应为数组而不是 null")
	}
	if got.Warnings == nil {
		t.Fatal("warnings 应为数组而不是 null")
	}
}

// 指定档位试算不得改动配置。
func TestSystemDryRunDoesNotChangeConfig(t *testing.T) {
	srv := systemPromptTestServer(t)

	before := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	var st struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(before.Body.Bytes(), &st)

	// 用 fold 试算
	if rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun",
		`{"system":"hello","mode":"fold"}`); rec.Code != http.StatusOK {
		t.Fatalf("试算失败 %d", rec.Code)
	}

	after := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	var st2 struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(after.Body.Bytes(), &st2)
	if st2.Mode != st.Mode {
		t.Fatalf("试算不得改动配置：%q → %q", st.Mode, st2.Mode)
	}
}

// 空输入必须 400，而不是悄悄返回一个空预览。
func TestSystemDryRunRequiresInput(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空输入应 400，实际 %d", rec.Code)
	}
}

// fold 档必须把 system 折进 user，且预览里能看到 client_instructions。
func TestSystemDryRunFoldShowsClientInstructions(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun",
		`{"system":"MYCUSTOMSYSTEM","mode":"fold"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "client_instructions") {
		t.Fatal("fold 档的预览里应能看到 client_instructions 包裹")
	}
	if !strings.Contains(rec.Body.String(), "MYCUSTOMSYSTEM") {
		t.Fatal("原 system 内容应被保留在包裹里")
	}
}
