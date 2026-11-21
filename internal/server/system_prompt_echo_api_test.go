package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

type echoState struct {
	OK             bool   `json:"ok"`
	Mode           string `json:"mode"`
	Strip          *bool  `json:"strip"`
	StripAssistant *bool  `json:"stripAssistant"`
	EchoUser       *bool  `json:"echoUser"`
}

func readEchoState(t *testing.T, srv *Server) echoState {
	t.Helper()
	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var got echoState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// echo 开关的 API 往返：POST 写入 → GET 读回。
func TestSystemPromptEchoUserAPIRoundTrip(t *testing.T) {
	srv := systemPromptTestServer(t)

	got := readEchoState(t, srv)
	if got.EchoUser == nil || *got.EchoUser {
		t.Fatalf("默认应关闭，实际 %v", got.EchoUser)
	}

	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt", `{"echoUser":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST 失败 %d: %s", rec.Code, rec.Body.String())
	}

	got = readEchoState(t, srv)
	if got.EchoUser == nil || !*got.EchoUser {
		t.Fatalf("开启后 GET 应读到 true，实际 %v", got.EchoUser)
	}

	if rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt",
		`{"echoUser":false}`); rec.Code != http.StatusOK {
		t.Fatalf("关闭失败 %d", rec.Code)
	}
	got = readEchoState(t, srv)
	if got.EchoUser == nil || *got.EchoUser {
		t.Fatalf("关闭后应为 false，实际 %v", got.EchoUser)
	}
}

// 只改 echoUser 不能影响 mode / strip / stripAssistant。
func TestSystemPromptEchoUserKeepsOtherSwitches(t *testing.T) {
	srv := systemPromptTestServer(t)

	if rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt",
		`{"mode":"keep","strip":true,"stripAssistant":false}`); rec.Code != http.StatusOK {
		t.Fatalf("预设失败 %d: %s", rec.Code, rec.Body.String())
	}

	if rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-prompt",
		`{"echoUser":true}`); rec.Code != http.StatusOK {
		t.Fatalf("改 echoUser 失败 %d", rec.Code)
	}

	got := readEchoState(t, srv)
	if got.Mode != "keep" {
		t.Errorf("mode 被覆写成 %q", got.Mode)
	}
	if got.Strip == nil || !*got.Strip {
		t.Errorf("strip 被覆写成 %v", got.Strip)
	}
	if got.StripAssistant == nil || *got.StripAssistant {
		t.Errorf("stripAssistant 被覆写成 %v", got.StripAssistant)
	}
	if got.EchoUser == nil || !*got.EchoUser {
		t.Errorf("echoUser 未生效 %v", got.EchoUser)
	}
}
