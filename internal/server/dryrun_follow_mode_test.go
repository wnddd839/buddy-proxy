package server

import (
	"encoding/json"
	"net/http"
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
