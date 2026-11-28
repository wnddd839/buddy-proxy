package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// 「跟随当前」必须真的跟随线上生效档位。
//
// 回归背景：handleSystemDryRun 曾先 Normalize 再判空，而
// NormalizeSystemPromptMode 对空输入返回 auto，判空永不成立 ——
// 线上是 keep/fold/strip 时，mode 为空的试算结果仍然按 auto 计算。
// 单测只构造 Record 喂 registry 是绕过链路的假测试，所以这里走
// 完整的 HTTP 路径：先用管理 API 改线上档位，再发 mode 为空的试算。
func TestSystemDryRunEmptyModeFollowsLiveConfig(t *testing.T) {
	for _, live := range []string{"keep", "fold", "strip"} {
		t.Run(live, func(t *testing.T) {
			srv := systemPromptTestServer(t)

			rec := doAdminRequest(t, srv, http.MethodPost,
				"/direct-admin/api/system-prompt", `{"mode":"`+live+`"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("设置线上档位失败：%d %s", rec.Code, rec.Body.String())
			}

			rec = doAdminRequest(t, srv, http.MethodPost,
				"/direct-admin/api/system-dryrun",
				`{"system":"You are Claude Code, Anthropic's official CLI for Claude."}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("试算状态码 %d：%s", rec.Code, rec.Body.String())
			}
			var got struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("解析响应失败：%v", err)
			}
			if got.Mode != live {
				t.Fatalf("「跟随当前」失效：线上是 %s，试算却是 %s", live, got.Mode)
			}
		})
	}
}

// 显式档位必须优先于线上配置（不能反过来被「跟随当前」污染）。
func TestSystemDryRunExplicitModeWinsOverLiveConfig(t *testing.T) {
	srv := systemPromptTestServer(t)

	rec := doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-prompt", `{"mode":"fold"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("设置线上档位失败：%d %s", rec.Code, rec.Body.String())
	}

	rec = doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-dryrun",
		`{"system":"You are Claude Code.","mode":"keep"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("试算状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}
	if got.Mode != "keep" {
		t.Fatalf("显式档位被覆盖：期望 keep，实际 %s", got.Mode)
	}
}

// keep 档在生产里永不剥离（gateway.applyStripConfig 只在 auto/strip 生效），
// 试算必须与生产一致：即使显式 strip=true，也要原样直传。
func TestSystemDryRunKeepModeNeverStrips(t *testing.T) {
	srv := systemPromptTestServer(t)

	if rec := doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-prompt", `{"mode":"keep"}`); rec.Code != http.StatusOK {
		t.Fatalf("设置 keep 失败：%d %s", rec.Code, rec.Body.String())
	}

	const system = "You are Claude Code, Anthropic's official CLI for Claude."
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun",
		`{"system":`+strconv.Quote(system)+`,"strip":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("试算状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Mode         string `json:"mode"`
		StripEnabled bool   `json:"stripEnabled"`
		StripReport  struct {
			Stripped bool `json:"Stripped"`
		} `json:"stripReport"`
		Outbound []map[string]any `json:"outbound"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}
	if got.Mode != "keep" {
		t.Fatalf("档位应为 keep，实际 %s", got.Mode)
	}
	if got.StripEnabled {
		t.Fatal("keep 档不得剥离：生产 applyStripConfig 只在 auto/strip 生效")
	}
	if got.StripReport.Stripped {
		t.Fatal("keep 档的 stripReport.Stripped 必须为 false")
	}
	if len(got.Outbound) == 0 || got.Outbound[0]["role"] != "system" {
		t.Fatalf("keep 档 outbound 第一条应为 system：%v", got.Outbound)
	}
	if content, _ := got.Outbound[0]["content"].(string); content != system {
		t.Fatalf("keep 档 system 必须原样保留，实际 %q", content)
	}
}

// 纯空白的 mode 也必须走「跟随当前」，不能被当成显式档位。
func TestSystemDryRunWhitespaceModeFollowsLive(t *testing.T) {
	srv := systemPromptTestServer(t)
	if rec := doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-prompt", `{"mode":"fold"}`); rec.Code != http.StatusOK {
		t.Fatalf("设置 fold 失败：%d %s", rec.Code, rec.Body.String())
	}
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun",
		`{"system":"You are Claude Code.","mode":"   "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("试算状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}
	if got.Mode != "fold" {
		t.Fatalf("空白 mode 应跟随线上 fold，实际 %s", got.Mode)
	}
}

// dryRunView 是 system-dryrun 响应里本文件要断言的字段（名字取自实际 JSON）。
type dryRunView struct {
	OK           bool   `json:"ok"`
	Mode         string `json:"mode"`
	KeepSystem   bool   `json:"keepSystem"`
	StripEnabled bool   `json:"stripEnabled"`
	EchoEnabled  bool   `json:"echoEnabled"`
	StripReport  struct {
		Stripped    bool `json:"Stripped"`
		SystemBytes int  `json:"SystemBytes"`
	} `json:"stripReport"`
	Outbound []map[string]any `json:"outbound"`
}

// doDryRun 走与管理台完全相同的入口（handleAdminAPIEntry）发一次试算。
func doDryRun(t *testing.T, srv *Server, body string) dryRunView {
	t.Helper()
	rec := doAdminRequest(t, srv, http.MethodPost, "/direct-admin/api/system-dryrun", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("试算状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var got dryRunView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析试算响应失败：%v", err)
	}
	return got
}

// 「本页预览的 strip 开关」= 这次预览是否真的剥离，而不是「线上总开关开着」。
//
// 生产 gateway.applyStripConfig 只在 auto/strip 档、且总开关打开时挂 Strip；
// 关掉时退回「先直传、被 11128 拒再折叠」。预览必须给同一形态，
// 否则用户会以为「关掉剥离后 system 还会被削」。
func TestSystemDryRunStripFalseKeepsSystemIntact(t *testing.T) {
	srv := systemPromptTestServer(t)
	if rec := doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-prompt", `{"mode":"auto"}`); rec.Code != http.StatusOK {
		t.Fatalf("设置 auto 失败：%d %s", rec.Code, rec.Body.String())
	}

	const system = "You are Claude Code, Anthropic's official CLI for Claude."
	body := `{"system":` + strconv.Quote(system)

	// 基线：auto + 总开关默认开 ⇒ 这次试算确实会剥掉身份句。
	// 先证明输入真的会触发剥离，否则下一条断言「不剥」是空的。
	base := doDryRun(t, srv, body+`}`)
	if !base.KeepSystem || !base.StripEnabled {
		t.Fatalf("auto 档基线应是 keep + 剥离：%+v", base)
	}
	if !base.StripReport.Stripped || base.StripReport.SystemBytes == 0 {
		t.Fatalf("auto 档基线应剥掉 system 身份句：%+v", base.StripReport)
	}

	// strip:false 只影响这次预览：仍然直传 system，但不剥离。
	got := doDryRun(t, srv, body+`,"strip":false}`)
	if got.Mode != "auto" || !got.KeepSystem {
		t.Fatalf("strip:false 不应改动档位或直传语义：%+v", got)
	}
	if got.StripEnabled {
		t.Fatal("strip:false 时 stripEnabled 必须为 false（这次预览不剥）")
	}
	if got.StripReport.Stripped || got.StripReport.SystemBytes != 0 {
		t.Fatalf("strip:false 时不得剥离：%+v", got.StripReport)
	}
	if len(got.Outbound) == 0 || got.Outbound[0]["role"] != "system" {
		t.Fatalf("outbound 首条应为 system：%v", got.Outbound)
	}
	if content, _ := got.Outbound[0]["content"].(string); content != system {
		t.Fatalf("strip:false 时 system 必须原样保留，实际 %q", content)
	}
}

// echo 是「复制一份 system 到首条 user」，且显式值要能压过线上配置。
func TestSystemDryRunEchoFollowsBodyAndLiveConfig(t *testing.T) {
	srv := systemPromptTestServer(t)
	if rec := doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-prompt", `{"mode":"keep"}`); rec.Code != http.StatusOK {
		t.Fatalf("设置 keep 失败：%d %s", rec.Code, rec.Body.String())
	}

	const system = "SYSTEM-ECHO-MARKER"
	body := `{"system":` + strconv.Quote(system)

	on := doDryRun(t, srv, body+`,"echo":true}`)
	if !on.EchoEnabled {
		t.Fatal("echo:true 应开启回显")
	}
	if !on.KeepSystem {
		t.Fatal("keep 档应直传 system")
	}
	if len(on.Outbound) < 2 || on.Outbound[0]["role"] != "system" || on.Outbound[1]["role"] != "user" {
		t.Fatalf("outbound 应为 [system, user]：%v", on.Outbound)
	}
	// 回显是复制而不是搬走：system 位必须仍完整。
	if content, _ := on.Outbound[0]["content"].(string); content != system {
		t.Fatalf("echo 不得改动 system 位，实际 %q", content)
	}
	firstUser, _ := on.Outbound[1]["content"].(string)
	if !strings.HasPrefix(firstUser, "<client_instructions>") || !strings.Contains(firstUser, system) {
		t.Fatalf("echo:true 应把 system 包进首条 user，实际 %q", firstUser)
	}
	if !strings.HasSuffix(firstUser, "hi") {
		t.Fatalf("echo 不得吞掉原 user 正文，实际 %q", firstUser)
	}

	// 线上 echo 开着时：不给字段要跟随线上，显式 false 必须能关掉。
	if rec := doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-prompt", `{"echoUser":true}`); rec.Code != http.StatusOK {
		t.Fatalf("开启线上 echo 失败：%d %s", rec.Code, rec.Body.String())
	}
	if live := doDryRun(t, srv, body+`}`); !live.EchoEnabled {
		t.Fatal("未给 echo 字段时应跟随线上配置（true）")
	}
	off := doDryRun(t, srv, body+`,"echo":false}`)
	if off.EchoEnabled {
		t.Fatal("echo:false 必须能压过线上配置")
	}
	userContent, _ := off.Outbound[1]["content"].(string)
	if userContent != "hi" || strings.Contains(userContent, "<client_instructions>") {
		t.Fatalf("echo:false 时首条 user 应保持原文，实际 %q", userContent)
	}
}

// fold 档：不剥离，但必须真的把原 system 从 system 位搬进首条 user。
func TestSystemDryRunFoldRewritesWithoutStripping(t *testing.T) {
	srv := systemPromptTestServer(t)
	if rec := doAdminRequest(t, srv, http.MethodPost,
		"/direct-admin/api/system-prompt", `{"mode":"fold"}`); rec.Code != http.StatusOK {
		t.Fatalf("设置 fold 失败：%d %s", rec.Code, rec.Body.String())
	}

	const system = "You are Claude Code, Anthropic's official CLI for Claude."
	// 显式 strip:true：即使总开关打开，fold 也不得剥离
	// （生产 applyStripConfig 先看档位，keep/fold 永远不挂 Strip）。
	got := doDryRun(t, srv, `{"system":`+strconv.Quote(system)+`,"strip":true}`)
	if got.Mode != "fold" || got.KeepSystem {
		t.Fatalf("fold 档必须 keepSystem=false：%+v", got)
	}
	if got.StripEnabled || got.StripReport.Stripped {
		t.Fatalf("fold 档不得剥离：stripEnabled=%v report=%+v", got.StripEnabled, got.StripReport)
	}
	if len(got.Outbound) < 2 || got.Outbound[0]["role"] != "system" || got.Outbound[1]["role"] != "user" {
		t.Fatalf("fold 后应为 [system, user]：%v", got.Outbound)
	}
	// system 位换成内置人设（provider/client.go 的 canonicalUpstreamSystem）。
	const canonical = "You are a helpful coding assistant."
	if content, _ := got.Outbound[0]["content"].(string); content != canonical {
		t.Fatalf("fold 后 system 位应换成人设，实际 %q", content)
	}
	firstUser, _ := got.Outbound[1]["content"].(string)
	if !strings.Contains(firstUser, "<client_instructions>") || !strings.Contains(firstUser, system) {
		t.Fatalf("fold 应把原 system 折进首条 user（原文不得被剥），实际 %q", firstUser)
	}
}
