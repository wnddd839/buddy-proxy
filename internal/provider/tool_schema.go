package provider

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// SanitizeToolSchemas 修正上游会以 11129「invalid function call parameters」
// 拒收的工具定义。
//
// 实测触发条件（2026-11-15，真实失败请求 + 逐项对照实验）：
//
//	"parameters": {                      ← 缺 {"type":"object","properties":{...}} 外壳
//	    "id": {"type":"string","required":true}   ← required 写在属性里（应为根级数组）
//	}
//
// 只有 automation_task_create 会真的让上游 400；同形状但没有必填项的
// delete/list/run 上游能接受。这里对**所有**工具做规整，因为形状本身不合法，
// 而规整后是它唯一正确的表达，不会改变原意。
//
// 合法 schema 一字不动（只读检查后原样返回），避免影响已经在正常工作的工具。
// 返回值 sanitized 表示是否改写过，供日志用。
func SanitizeToolSchemas(tools []any) (out []any, sanitized []string) {
	if len(tools) == 0 {
		return tools, nil
	}
	out = make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		params, ok := fn["parameters"].(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		fixed, changed := normalizeParameterSchema(params)
		if !changed {
			out = append(out, raw)
			continue
		}
		clone := make(map[string]any, len(tool))
		for k, v := range tool {
			clone[k] = v
		}
		fnClone := make(map[string]any, len(fn))
		for k, v := range fn {
			fnClone[k] = v
		}
		fnClone["parameters"] = fixed
		clone["function"] = fnClone
		out = append(out, clone)
		sanitized = append(sanitized, strings.TrimSpace(toString(fn["name"])))
	}
	return out, sanitized
}

// normalizeParameterSchema 把 parameters 规整成合法 JSON Schema。
// 返回新对象与是否发生改写；无改写时返回原对象（调用方据此跳过复制）。
func normalizeParameterSchema(params map[string]any) (map[string]any, bool) {
	// 已有标准外壳：只做嵌套层的 required 上提（深拷贝后再改）。
	if strings.EqualFold(toString(params["type"]), "object") && params["properties"] != nil {
		if _, ok := params["properties"].(map[string]any); ok {
			clone := deepCloneMap(params)
			if promoteNestedRequired(clone) {
				return clone, true
			}
			return params, false
		}
	}

	// 裸属性字典：顶层每个 key 都是一个属性。
	props := make(map[string]any, len(params))
	required := make([]string, 0, 4)
	changed := false
	for key, value := range params {
		// 这几个是 schema 关键字而非属性名；出现时说明它其实已是标准 schema，
		// 只是 type 写错或缺失，按标准结构补 type 即可。
		// ⚠️ 不能把 "title" 列为关键字：它同时也是合法属性名（automation_task_create
		// 的必填字段之一），曾因此把 title 属性整个丢掉。
		switch key {
		case "type", "properties", "required", "additionalProperties", "description", "$schema", "definitions", "$defs":
			changed = true
			continue
		}
		child, ok := value.(map[string]any)
		if !ok {
			props[key] = value
			continue
		}
		childClone := deepCloneMap(child)
		if isTrue(childClone["required"]) {
			delete(childClone, "required")
			required = append(required, key)
			changed = true
		}
		if promoteNestedRequired(childClone) {
			changed = true
		}
		props[key] = childClone
	}

	// 没有任何属性、也没有 schema 关键字 ⇒ 空参数工具，给个空 object 即可。
	if len(props) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}, true
	}

	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	// 裸字典一定不是标准形状，算改写。
	return schema, true || changed
}

// promoteNestedRequired 递归把子属性里的 required:true 提升为父级 required 数组。
// 返回是否改动。
func promoteNestedRequired(node map[string]any) bool {
	props, ok := node["properties"].(map[string]any)
	if !ok {
		// 数组节点也要往下走。
		if items, ok := node["items"].(map[string]any); ok {
			return promoteNestedRequired(items)
		}
		return false
	}
	changed := false
	required := make([]string, 0, 4)
	if existing, ok := node["required"].([]any); ok {
		for _, v := range existing {
			required = append(required, toString(v))
		}
	} else if existing, ok := node["required"].([]string); ok {
		required = append(required, existing...)
	}
	for key, raw := range props {
		child, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if isTrue(child["required"]) {
			delete(child, "required")
			required = append(required, key)
			changed = true
		}
		if promoteNestedRequired(child) {
			changed = true
		}
	}
	if changed || len(required) > 0 {
		if len(required) > 0 {
			node["required"] = dedupeStrings(required)
		}
	}
	return changed
}

func isTrue(value any) bool {
	b, ok := value.(bool)
	return ok && b
}

func toString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func deepCloneMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		switch typed := v.(type) {
		case map[string]any:
			out[k] = deepCloneMap(typed)
		case []any:
			list := make([]any, len(typed))
			for i, item := range typed {
				if m, ok := item.(map[string]any); ok {
					list[i] = deepCloneMap(m)
				} else {
					list[i] = item
				}
			}
			out[k] = list
		default:
			out[k] = v
		}
	}
	return out
}

// logSanitizedToolSchemas 只记工具名，不记 schema 内容。
func logSanitizedToolSchemas(logger *slog.Logger, names []string) {
	if logger == nil || len(names) == 0 {
		return
	}
	logger.Warn("sanitized non-conforming tool schemas",
		"count", len(names),
		"tools", strings.Join(names, ","))
}
