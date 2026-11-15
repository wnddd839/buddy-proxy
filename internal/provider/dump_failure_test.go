package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildToolCallDiagnosticsFindsBadArguments(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []any{
					map[string]any{
						"id":   "c1",
						"type": "function",
						"function": map[string]any{
							"name":      "bash",
							"arguments": `{"command":"ls"}`,
						},
					},
					map[string]any{
						"id":   "c2",
						"type": "function",
						"function": map[string]any{
							"name":      "edit",
							"arguments": `{"file":"a.go","content":"func main() {`,
						},
					},
					map[string]any{
						"id":   "c3",
						"type": "function",
						"function": map[string]any{
							"name":      "write",
							"arguments": "",
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": "ok"},
		},
		"tools": []any{map[string]any{"type": "function"}},
	}

	diag := buildToolCallDiagnostics(body)
	if diag["toolCallsTotal"] != 3 {
		t.Fatalf("total=%v want 3", diag["toolCallsTotal"])
	}
	if diag["toolCallsBad"] != 1 {
		t.Fatalf("bad=%v want 1（截断的 arguments）", diag["toolCallsBad"])
	}
	if diag["toolCallsEmpty"] != 1 {
		t.Fatalf("empty=%v want 1（空 arguments）", diag["toolCallsEmpty"])
	}

	problem, ok := diag["problemCalls"].([]map[string]any)
	if !ok || len(problem) != 2 {
		t.Fatalf("problemCalls=%v want 2 项（非法 + 空）", diag["problemCalls"])
	}
	// 合法的那条不该出现在问题列表里。
	for _, item := range problem {
		if item["id"] == "c1" {
			t.Fatal("合法调用 c1 不应进问题列表")
		}
	}
	var bad map[string]any
	for _, item := range problem {
		if item["id"] == "c2" {
			bad = item
		}
	}
	if bad == nil || bad["validJson"] != false {
		t.Fatalf("c2 应标记 validJson=false：%v", bad)
	}
	if _, ok := bad["error"]; !ok {
		t.Fatalf("c2 应带解析错误：%v", bad)
	}
}

func TestDumpUpstreamFailureWritesRequestAndDiagnostics(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dumps")
	body := map[string]any{
		"model": "deepseek-v4.1-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id": "c1", "type": "function",
						"function": map[string]any{"name": "bash", "arguments": `{"x":`},
					},
				},
			},
		},
	}
	base, err := dumpUpstreamFailure(dir, 400, body, "invalid function call parameters (code 11129)")
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Fatal("应写出转储文件")
	}

	raw, err := os.ReadFile(base + ".request.json")
	if err != nil {
		t.Fatalf("请求体未落盘：%v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("请求体不是合法 JSON：%v", err)
	}
	if round["model"] != "deepseek-v4.1-flash" {
		t.Fatalf("请求体内容不对：%v", round["model"])
	}

	diagRaw, err := os.ReadFile(base + ".diagnostics.json")
	if err != nil {
		t.Fatalf("诊断未落盘：%v", err)
	}
	var diag map[string]any
	if err := json.Unmarshal(diagRaw, &diag); err != nil {
		t.Fatalf("诊断不是合法 JSON：%v", err)
	}
	if diag["toolCallsBad"] != float64(1) {
		t.Fatalf("诊断未标出坏参数：%v", diag["toolCallsBad"])
	}
	if diag["status"] != float64(400) {
		t.Fatalf("诊断未记状态码：%v", diag["status"])
	}
}

// 目录为空时完全不写文件：默认关闭，不能因为没配开关就产生副作用。
func TestDumpUpstreamFailureDisabledByEmptyDir(t *testing.T) {
	body := map[string]any{"messages": []any{}}
	base, err := dumpUpstreamFailure("", 400, body, "x")
	if err != nil || base != "" {
		t.Fatalf("未配置目录时不应落盘：base=%q err=%v", base, err)
	}
}
