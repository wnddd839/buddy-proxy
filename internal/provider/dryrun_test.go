package provider

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

const claudeIdentity = "You are Claude Code, Anthropic's official CLI for Claude."

func claudeMessages() []map[string]any {
	return []map[string]any{
		{"role": "system", "content": claudeIdentity},
		{"role": "user", "content": "帮我写个函数"},
	}
}

// enabledStrip 返回一个真正会生效的 StripConfig（必须带规则）。
//
// 空的 StripConfig{Enabled:true} 不会剥离任何东西 —— 剥离靠规则表驱动。
func enabledStrip() *StripConfig {
	return &StripConfig{
		Enabled:  true,
		Rules:    RulesForSite(config.NormalizeSite("domestic")),
		Contains: ContainsForSite(config.NormalizeSite("domestic")),
	}
}

// keep 档：system 直传、不剥离。
func TestDryRunKeep(t *testing.T) {
	res := DryRun(DryRunOptions{
		Messages:   claudeMessages(),
		Site:       "domestic",
		KeepSystem: true,
	})
	if res.OutSystemLen == 0 {
		t.Fatalf("keep 档 system 应保留：%+v", res)
	}
	if res.Report.Stripped {
		t.Fatal("没挂 Strip 时不该剥离")
	}
	if res.InMessages != 2 || res.OutMessages != 2 {
		t.Fatalf("条数不该变：%+v", res)
	}
}

// keep + strip 档：system 仍在，但已知指纹被删。
func TestDryRunKeepStrip(t *testing.T) {
	res := DryRun(DryRunOptions{
		Messages:   claudeMessages(),
		Site:       "domestic",
		KeepSystem: true,
		Strip:      enabledStrip(),
	})
	if !res.Report.Stripped {
		t.Fatalf("应剥离已知身份句：%+v", res.Report)
	}
	if res.Report.SystemBytes == 0 {
		t.Fatalf("剥离应记在 SystemBytes 上：%+v", res.Report)
	}
	if res.OutSystemLen >= res.InSystemLen {
		t.Fatalf("剥离后应更短：in=%d out=%d", res.InSystemLen, res.OutSystemLen)
	}
	// 模型看到的 system 里不该再有那句人设
	for _, m := range res.Outbound {
		if fmtStr(m["role"]) != "system" {
			continue
		}
		if s, ok := m["content"].(string); ok && strings.Contains(s, "Anthropic's official CLI") {
			t.Fatalf("system 里仍有身份句：%q", s)
		}
	}
}

// fold 档：system 被折进首条 user，system 位换成内置人设。
func TestDryRunFold(t *testing.T) {
	res := DryRun(DryRunOptions{
		Messages:   claudeMessages(),
		Site:       "domestic",
		KeepSystem: false,
	})
	if !containsNote(res.Notes, "fold") {
		t.Fatalf("应说明走 fold：%+v", res.Notes)
	}
	if res.OutSystemLen == 0 {
		t.Fatal("fold 后 system 位应有内置人设")
	}
	found := false
	for _, m := range res.Outbound {
		if fmtStr(m["role"]) == "user" {
			if s, ok := m["content"].(string); ok && strings.Contains(s, "<client_instructions>") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("fold 后原 system 应在 user 的 client_instructions 里：%+v", res.Outbound)
	}
}

// echo 档：system 位不变，同时复制一份进首条 user。
func TestDryRunEcho(t *testing.T) {
	res := DryRun(DryRunOptions{
		Messages:         claudeMessages(),
		Site:             "domestic",
		KeepSystem:       true,
		EchoSystemToUser: true,
	})
	if res.OutSystemLen != res.InSystemLen {
		t.Fatalf("echo 不改变 system 位：in=%d out=%d", res.InSystemLen, res.OutSystemLen)
	}
	for _, m := range res.Outbound {
		if fmtStr(m["role"]) == "user" {
			if s, ok := m["content"].(string); ok && strings.Contains(s, "<client_instructions>") {
				return
			}
		}
	}
	t.Fatalf("echo 后 user 里应有 system 副本：%+v", res.Outbound)
}

// system 被剥离时必须给警告（res.Report.SystemBytes > 0）。
func TestDryRunWarnsSystemStripped(t *testing.T) {
	res := DryRun(DryRunOptions{
		Messages:   claudeMessages(),
		Site:       "domestic",
		KeepSystem: true,
		Strip:      enabledStrip(),
	})
	if !containsNote(res.Warnings, "system 内容被剥离") {
		t.Fatalf("应警告 system 被剥离：%+v", res.Warnings)
	}
}

// 剥空 system 时（身份句是全部内容）要额外警告「出站 system 为空」。
func TestDryRunWarnsSystemEmptied(t *testing.T) {
	// 构造一条只有身份句、剥完就空的 system
	res := DryRun(DryRunOptions{
		Messages: []map[string]any{
			{"role": "system", "content": claudeIdentity},
			{"role": "user", "content": "hi"},
		},
		Site:       "domestic",
		KeepSystem: true,
		Strip:      enabledStrip(),
	})
	if res.Report.SystemBytes > 0 && res.OutSystemLen == 0 {
		if !containsNote(res.Warnings, "出站 system 为空") {
			t.Fatalf("应警告 system 归零：%+v", res.Warnings)
		}
	}
}

// global 站缺 system 时必须注入内置 system，并说明。
func TestDryRunGlobalInjectsSystem(t *testing.T) {
	res := DryRun(DryRunOptions{
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		Site:       "global",
		KeepSystem: true,
	})
	if !containsNote(res.Notes, "global 注入") {
		t.Fatalf("应说明注入：%+v", res.Notes)
	}
	if res.OutMessages != 2 {
		t.Fatalf("应多出一条 system：%+v", res)
	}
}

// 预览绝不能修改输入切片 —— 否则调用方会拿到被改过的请求。
func TestDryRunDoesNotMutateInput(t *testing.T) {
	in := claudeMessages()
	DryRun(DryRunOptions{
		Messages:   in,
		Site:       "domestic",
		KeepSystem: false,
		Strip:      enabledStrip(),
	})
	if s, ok := in[0]["content"].(string); ok && s != claudeIdentity {
		t.Fatalf("输入的 system 被改写：%q", s)
	}
	if fmtStr(in[0]["role"]) != "system" {
		t.Fatalf("输入的 role 被改写：%q", fmtStr(in[0]["role"]))
	}
}

func containsNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}
