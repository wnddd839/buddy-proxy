package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
)

// dshDomesticOnlyIdentity 是只出现在国内站黑名单里的身份句
// （internal/provider/identity_strip.go 的 blockedIdentityPrefixes）。
// 国际站不拦它 —— 用它就能证明「试算真的换了规则表」，而不只是回显 site。
const dshDomesticOnlyIdentity = "You are an AI agent powered by DeepSeek Harness."

// dryRunSiteTestServer 与 checkinTestServer 相同，但能指定号池当前站点。
//
// 「跟随当前」的用例必须先让 cfg.Site 与显式 site 不同才有意义，所以不能
// 直接用 systemPromptTestServer（它把 Site 留空 ⇒ 归一成 global）。
func dryRunSiteTestServer(t *testing.T, site string) *Server {
	t.Helper()
	dir := t.TempDir()
	isolateEnvFile(t, dir)
	accountsPath := filepath.Join(dir, "proxy-accounts.json")
	raw, err := json.Marshal(accounts.Store{Accounts: []accounts.Account{checkinAccount("a1")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accountsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{AccountsPath: accountsPath, Site: site}
	svc := gateway.New(cfg, slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	// 必须收尾：gateway.New 会启动不变量执行器的写协程，不 Close 的话
	// TempDir 清理会撞上「directory not empty」（与 checkinTestServer 同因）。
	t.Cleanup(func() { _ = svc.Close() })
	return New(cfg, svc)
}

// dryRunSitePayload 只提本文件要断言的字段；字段名按后端真实 JSON 键。
type dryRunSitePayload struct {
	OK           bool   `json:"ok"`
	Site         string `json:"site"`
	Mode         string `json:"mode"`
	StripEnabled bool   `json:"stripEnabled"`
	InSystemLen  int    `json:"inSystemLen"`
	OutSystemLen int    `json:"outSystemLen"`
	StripReport  struct {
		Stripped    bool `json:"Stripped"`
		Bytes       int  `json:"Bytes"`
		SystemBytes int  `json:"SystemBytes"`
	} `json:"stripReport"`
	Error string `json:"error"`
}

func postDryRunSite(t *testing.T, srv *Server, body string) (*httptest.ResponseRecorder, dryRunSitePayload) {
	t.Helper()
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun", body)
	var got dryRunSitePayload
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
		}
	}
	return rec, got
}

// 不传 site = 旧行为：跟随当前号池站点（这里刻意设成 domestic）。
func TestSystemDryRunSiteDefaultsToConfig(t *testing.T) {
	srv := dryRunSiteTestServer(t, "domestic")
	rec, got := postDryRunSite(t, srv, `{"system":"hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	if !got.OK {
		t.Fatalf("ok 应为 true: %s", rec.Body.String())
	}
	if got.Site != "domestic" {
		t.Fatalf("缺省 site 应跟随 cfg.Site=domestic，实际 %q", got.Site)
	}
}

// 显式 global 必须覆盖 cfg.Site=domestic —— 否则这个字段等于没生效。
func TestSystemDryRunSiteExplicitGlobalOverridesConfig(t *testing.T) {
	srv := dryRunSiteTestServer(t, "domestic")
	rec, got := postDryRunSite(t, srv, `{"system":"hello","site":"global"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	if got.Site != "global" {
		t.Fatalf("显式 site=global 应回显 global，实际 %q", got.Site)
	}
	// 同一实例上再跑一次缺省请求，确认它没被上一次试算改掉。
	if _, again := postDryRunSite(t, srv, `{"system":"hello"}`); again.Site != "domestic" {
		t.Fatalf("试算不得改动 cfg.Site：缺省请求变成了 %q", again.Site)
	}
}

// 显式 domestic 必须覆盖 cfg.Site=global。
func TestSystemDryRunSiteExplicitDomesticOverridesConfig(t *testing.T) {
	srv := dryRunSiteTestServer(t, "global")
	rec, got := postDryRunSite(t, srv, `{"system":"hello","site":"domestic"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	if got.Site != "domestic" {
		t.Fatalf("显式 site=domestic 应回显 domestic，实际 %q", got.Site)
	}
}

// 垃圾值必须 400 —— 不能像 NormalizeSite 那样静默归成 global。
func TestSystemDryRunSiteRejectsGarbage(t *testing.T) {
	srv := dryRunSiteTestServer(t, "domestic")
	rec, _ := postDryRunSite(t, srv, `{"system":"hello","site":"garbage"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 site 应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("400 响应应是 JSON: %v\n%s", err, rec.Body.String())
	}
	if body.OK {
		t.Fatal("400 响应的 ok 应为 false")
	}
	if !strings.Contains(body.Error, "site") || !strings.Contains(body.Error, "domestic") {
		t.Fatalf("错误信息应点明 site 的合法取值，实际 %q", body.Error)
	}
}

// 站点不只是回显：剥离规则必须换成所选站点的那一张表。
func TestSystemDryRunSiteUsesChosenSiteStripRules(t *testing.T) {
	srv := dryRunSiteTestServer(t, "domestic")

	// 跟随当前（domestic）：国内站多拦的那条身份句必须被剥掉。
	_, domestic := postDryRunSite(t, srv, `{"system":"`+dshDomesticOnlyIdentity+`","mode":"strip"}`)
	if !domestic.StripEnabled {
		t.Fatal("strip 档应启用剥离")
	}
	if domestic.StripReport.SystemBytes == 0 || domestic.OutSystemLen != 0 {
		t.Fatalf("国内站应剥掉国内专属身份句：stripReport=%+v outSystemLen=%d",
			domestic.StripReport, domestic.OutSystemLen)
	}

	// 显式 global：同一条 system 国际站不拦，必须原样直传。
	_, global := postDryRunSite(t, srv, `{"system":"`+dshDomesticOnlyIdentity+`","site":"global","mode":"strip"}`)
	if global.Site != "global" {
		t.Fatalf("响应 site 应为 global，实际 %q", global.Site)
	}
	if global.StripReport.SystemBytes != 0 || global.StripReport.Stripped {
		t.Fatalf("国际站不该剥这条身份句：stripReport=%+v", global.StripReport)
	}
	if global.OutSystemLen == 0 {
		t.Fatal("国际站应保留原 system，实际出站 system 为空")
	}
}

// 试算（含显式 site）不得改动任何配置 —— 沿用旧用例的「读前 / 读后」比对。
func TestSystemDryRunSiteDoesNotChangeConfig(t *testing.T) {
	srv := dryRunSiteTestServer(t, "domestic")
	siteBefore := srv.Svc.Config().Site

	before := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	var st struct {
		Mode      string `json:"mode"`
		Strip     bool   `json:"strip"`
		EchoUser  bool   `json:"echoUser"`
		StripAsst bool   `json:"stripAssistant"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{
		`{"system":"hello","site":"global","mode":"fold"}`,
		`{"system":"hello","site":"domestic","strip":false,"echo":true}`,
		`{"system":"hello","site":"garbage"}`,
	} {
		postDryRunSite(t, srv, body)
	}

	after := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	var st2 struct {
		Mode      string `json:"mode"`
		Strip     bool   `json:"strip"`
		EchoUser  bool   `json:"echoUser"`
		StripAsst bool   `json:"stripAssistant"`
	}
	if err := json.Unmarshal(after.Body.Bytes(), &st2); err != nil {
		t.Fatal(err)
	}
	if st2.Mode != st.Mode {
		t.Fatalf("试算不得改动档位配置：%q → %q", st.Mode, st2.Mode)
	}
	if st2.Strip != st.Strip || st2.EchoUser != st.EchoUser || st2.StripAsst != st.StripAsst {
		t.Fatalf("试算不得改动开关配置：%+v → %+v", st, st2)
	}
	if siteAfter := srv.Svc.Config().Site; siteAfter != siteBefore {
		t.Fatalf("试算不得改动 cfg.Site：%q → %q", siteBefore, siteAfter)
	}
}
