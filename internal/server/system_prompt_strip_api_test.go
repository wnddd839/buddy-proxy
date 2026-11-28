package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

type stripState struct {
	OK             bool   `json:"ok"`
	Mode           string `json:"mode"`
	Strip          *bool  `json:"strip"`
	StripAssistant *bool  `json:"stripAssistant"`
}

// readStripState 读一次 GET，把三个字段解出来。
func readStripState(t *testing.T, srv *Server) stripState {
	t.Helper()
	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var got stripState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// GET 必须把两个开关的**当前值**告诉前端，否则面板勾选框会显示错的。
func TestSystemPromptAPIExposesStripSwitches(t *testing.T) {
	srv := systemPromptTestServer(t)
	got := readStripState(t, srv)
	if got.Strip == nil || !*got.Strip {
		t.Fatalf("默认应变剥离开启：%+v", got)
	}
	if got.StripAssistant == nil || !*got.StripAssistant {
		t.Fatalf("默认应连 assistant 一起剥：%+v", got)
	}
}

// 关掉总开关 ⇒ 立刻生效、并落到 .env（重启后仍是关）。
func TestSystemPromptAPIStripSwitchOffPersists(t *testing.T) {
	srv := systemPromptTestServer(t)

	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"strip":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	if cfg := srv.Svc.Config(); !cfg.SystemStripDisabled {
		t.Fatal("总开关应已置为「跳过」")
	}
	got := readStripState(t, srv)
	if got.Strip == nil || *got.Strip {
		t.Fatalf("GET 应回报 strip=false：%+v", got)
	}
	raw, err := os.ReadFile(os.Getenv("CODEBUDDY_PROXY_ENV_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "CODEBUDDY_PROXY_SYSTEM_STRIP_OFF=1") {
		t.Fatalf(".env 未持久化总开关：%s", raw)
	}
}

// 只关 assistant：总开关仍开着，system 照剥。
func TestSystemPromptAPIAssistantSwitchIsIndependent(t *testing.T) {
	srv := systemPromptTestServer(t)

	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"stripAssistant":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	cfg := srv.Svc.Config()
	if cfg.SystemStripDisabled {
		t.Fatal("关 assistant 不应连总开关一起关掉")
	}
	if !cfg.SystemStripAssistantDisabled {
		t.Fatal("assistant 跳过开关应已置位")
	}
	got := readStripState(t, srv)
	if got.Strip == nil || !*got.Strip {
		t.Fatalf("strip 应仍为 true：%+v", got)
	}
	if got.StripAssistant == nil || *got.StripAssistant {
		t.Fatalf("stripAssistant 应为 false：%+v", got)
	}
}

// 只改开关不能顺手改模式；只改模式不能顺手改开关。
func TestSystemPromptSwitchesDoNotLeakIntoEachOther(t *testing.T) {
	srv := systemPromptTestServer(t)
	if rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"mode":"fold"}`); rec.Code != http.StatusOK {
		t.Fatalf("切模式失败：%d %s", rec.Code, rec.Body.String())
	}
	got := readStripState(t, srv)
	if got.Mode != "fold" {
		t.Fatalf("模式应为 fold：%+v", got)
	}
	if got.Strip == nil || !*got.Strip || got.StripAssistant == nil || !*got.StripAssistant {
		t.Fatalf("只切模式不应改动剥离开关：%+v", got)
	}

	if rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"strip":false}`); rec.Code != http.StatusOK {
		t.Fatalf("切开关失败：%d %s", rec.Code, rec.Body.String())
	}
	got = readStripState(t, srv)
	if got.Mode != "fold" {
		t.Fatalf("只切开关不应改动模式：%+v", got)
	}
}

// 一个字段都不给 ⇒ 400，避免「点了一下什么都没变」还被当作成功。
func TestSystemPromptAPIRejectsEmptyBody(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 body 应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
}
