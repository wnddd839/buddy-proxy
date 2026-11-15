package provider

import (
	"encoding/json"
	"testing"
)

// 真实故障形状：parameters 直接是属性字典，required 写在属性里。
// 上游对 automation_task_create 的这种定义返回 400 code 11129。
func TestSanitizeToolSchemasWrapsBarePropertyMap(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "automation_task_create",
				"parameters": map[string]any{
					"id":    map[string]any{"type": "string", "required": true},
					"title": map[string]any{"type": "string", "required": true},
					"note":  map[string]any{"type": "string"},
					"model": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"provider": map[string]any{"type": "string", "required": true},
							"model":    map[string]any{"type": "string", "required": true},
						},
						"required": true,
					},
				},
			},
		},
	}

	out, changed := SanitizeToolSchemas(tools)
	if len(changed) != 1 || changed[0] != "automation_task_create" {
		t.Fatalf("应报告 1 个工具被改写，实际 %v", changed)
	}

	params := out[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("根 type 应为 object，实际 %v", params["type"])
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("缺 properties：%v", params)
	}
	if len(props) != 4 {
		t.Fatalf("属性数应为 4，实际 %d", len(props))
	}
	// id/title 是必填；note 不是。
	req := toStringSlice(params["required"])
	if !contains(req, "id") || !contains(req, "title") {
		t.Fatalf("required 应含 id/title，实际 %v", req)
	}
	if contains(req, "note") {
		t.Fatalf("note 不该必填：%v", req)
	}

	// 嵌套层：model 的 provider/model 也要被上提，且属性内不再残留 required。
	model := props["model"].(map[string]any)
	if model["type"] != "object" {
		t.Fatalf("model 应为 object：%v", model)
	}
	inner := toStringSlice(model["required"])
	if !contains(inner, "provider") || !contains(inner, "model") {
		t.Fatalf("model.required 应含 provider/model，实际 %v", inner)
	}
	for name, raw := range model["properties"].(map[string]any) {
		child := raw.(map[string]any)
		if _, found := child["required"]; found {
			t.Fatalf("嵌套属性 %s 仍残留 required", name)
		}
	}
}

// 合法 schema 必须原样返回，连指针都不换：避免影响正在正常工作的 99 个工具。
func TestSanitizeToolSchemasLeavesValidSchemaUntouched(t *testing.T) {
	valid := map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "bash",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
				"required": []any{"command"},
			},
		},
	}
	out, changed := SanitizeToolSchemas([]any{valid})
	if len(changed) != 0 {
		t.Fatalf("合法 schema 不该被改写：%v", changed)
	}
	// 同一个对象引用（未复制、未修改）
	if out[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["type"] != "object" {
		t.Fatal("输出被意外改动")
	}
	raw, _ := json.Marshal(valid)
	if want := `{"function":{"name":"bash","parameters":{"properties":{"command":{"type":"string"}},"required":["command"],"type":"object"}},"type":"function"}`; string(raw) != want {
		t.Fatalf("原对象被就地修改：%s", raw)
	}
}

// 无参数工具（parameters 为空对象）补成空 object schema。
func TestSanitizeToolSchemasHandlesEmptyParameters(t *testing.T) {
	tools := []any{map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":       "list_agents",
			"parameters": map[string]any{},
		},
	}}
	out, changed := SanitizeToolSchemas(tools)
	if len(changed) != 1 {
		t.Fatalf("空 parameters 应被规整：%v", changed)
	}
	params := out[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("应为 object schema：%v", params)
	}
	if _, ok := params["properties"].(map[string]any); !ok {
		t.Fatalf("应带空 properties：%v", params)
	}
}

// 已经标准、但嵌套层仍有 required:true 的定义：只修嵌套层。
func TestSanitizeToolSchemasPromotesNestedRequiredInsideValidWrapper(t *testing.T) {
	tools := []any{map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "workflow",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"meta": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"tag": map[string]any{"type": "string", "required": true},
						},
					},
				},
			},
		},
	}}
	out, changed := SanitizeToolSchemas(tools)
	if len(changed) != 1 {
		t.Fatalf("嵌套 required 应被修：%v", changed)
	}
	props := out[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["properties"].(map[string]any)
	meta := props["meta"].(map[string]any)
	if got := toStringSlice(meta["required"]); !contains(got, "tag") {
		t.Fatalf("meta.required 应含 tag：%v", meta)
	}
	if _, found := meta["properties"].(map[string]any)["tag"].(map[string]any)["required"]; found {
		t.Fatal("tag 仍残留 required")
	}
}

// 混合场景：一批里只有一个是坏的，其余不受影响、顺序保持。
func TestSanitizeToolSchemasKeepsOrderAndOnlyTouchesBadOne(t *testing.T) {
	good1 := map[string]any{"type": "function", "function": map[string]any{
		"name": "read", "parameters": map[string]any{
			"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}},
		}}}
	bad := map[string]any{"type": "function", "function": map[string]any{
		"name": "automation_task_run", "parameters": map[string]any{
			"id": map[string]any{"type": "string", "required": true}}}}
	good2 := map[string]any{"type": "function", "function": map[string]any{
		"name": "write", "parameters": map[string]any{
			"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}},
		}}}

	out, changed := SanitizeToolSchemas([]any{good1, bad, good2})
	if len(out) != 3 {
		t.Fatalf("数量变了：%d", len(out))
	}
	names := []string{
		toString(out[0].(map[string]any)["function"].(map[string]any)["name"]),
		toString(out[1].(map[string]any)["function"].(map[string]any)["name"]),
		toString(out[2].(map[string]any)["function"].(map[string]any)["name"]),
	}
	if names[0] != "read" || names[1] != "automation_task_run" || names[2] != "write" {
		t.Fatalf("顺序被打乱：%v", names)
	}
	if len(changed) != 1 || changed[0] != "automation_task_run" {
		t.Fatalf("只应改写 1 个：%v", changed)
	}
	// 好的两个必须是原对象（未复制）
	if _, same := out[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)["required"]; same {
		t.Fatal("read 不该被加 required")
	}
}

// 非 function 工具（如内置类型）原样透传。
func TestSanitizeToolSchemasPassesThroughUnknownShapes(t *testing.T) {
	raw := map[string]any{"type": "web_search"}
	out, changed := SanitizeToolSchemas([]any{raw, "plain-string", 42})
	if len(changed) != 0 {
		t.Fatalf("非标准形状不该改：%v", changed)
	}
	if len(out) != 3 {
		t.Fatalf("数量变了：%d", len(out))
	}
}

func toStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, toString(item))
		}
		return out
	default:
		return nil
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
