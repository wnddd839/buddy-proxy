package provider

import (
	"encoding/json"
	"maps"
	"strings"
)

// normalizeContent 保留图片块，并兼容 Responses 和常见客户端的图片字段。
// 这里只转换协议形状，不下载或解码图片，避免在代理内引入无界解码分配。
func normalizeContent(content any) any {
	parts := asAnySlice(content)
	if parts == nil {
		return flattenContent(content)
	}
	out := make([]any, 0, len(parts))
	structured := false
	for _, part := range parts {
		m, ok := part.(map[string]any)
		if !ok {
			out = append(out, part)
			structured = true
			continue
		}
		typ, _ := m["type"].(string)
		switch typ {
		case "reasoning":
			continue
		case "input_text", "output_text", "refusal":
			out = append(out, map[string]any{"type": "text", "text": m["text"]})
		default:
			if image, ok := imagePart(m); ok {
				out = append(out, image)
				structured = true
			} else {
				out = append(out, part)
				structured = structured || (typ != "" && typ != "text")
			}
		}
	}
	if !structured {
		return flattenContent(out)
	}
	return out
}

func imagePart(part map[string]any) (map[string]any, bool) {
	typ, _ := part["type"].(string)
	if typ != "" && typ != "image_url" && typ != "input_image" && typ != "image" {
		return nil, false
	}
	var ref map[string]any
	switch value := part["image_url"].(type) {
	case string:
		ref = map[string]any{"url": value}
	case map[string]any:
		ref = maps.Clone(value)
	}
	if typ == "image" && ref == nil {
		if url, ok := part["dataUrl"].(string); ok {
			ref = map[string]any{"url": url}
		} else if source, ok := part["source"].(map[string]any); ok {
			data, _ := source["data"].(string)
			mime, _ := source["media_type"].(string)
			if mime == "" {
				mime, _ = source["mediaType"].(string)
			}
			if data != "" && mime != "" && source["type"] == "base64" {
				ref = map[string]any{"url": "data:" + mime + ";base64," + data}
			}
		}
	}
	if ref == nil {
		return nil, false
	}
	url, _ := ref["url"].(string)
	if strings.TrimSpace(url) == "" {
		return nil, false
	}
	if detail, ok := part["detail"].(string); ok && detail != "" {
		ref["detail"] = detail
	}
	return map[string]any{"type": "image_url", "image_url": ref}, true
}

// NormalizeToolOutput 只解包明确的图片内容数组，不把普通 JSON 工具结果误当协议。
func NormalizeToolOutput(output any) any {
	value := output
	if text, ok := output.(string); ok {
		if !strings.HasPrefix(strings.TrimSpace(text), "[") {
			return text
		}
		var parts []any
		if json.Unmarshal([]byte(text), &parts) != nil || !containsImage(parts) {
			return text
		}
		value = parts
	}
	if parts := asAnySlice(value); parts != nil {
		if containsImage(parts) {
			return normalizeContent(parts)
		}
		var text strings.Builder
		for _, part := range parts {
			if m, ok := part.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					text.WriteString(s)
				}
			}
		}
		if text.Len() > 0 {
			return text.String()
		}
	}
	if output == nil {
		return ""
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return ""
	}
	return string(raw)
}

func containsImage(parts []any) bool {
	for _, part := range parts {
		if m, ok := part.(map[string]any); ok {
			if _, ok := imagePart(m); ok {
				return true
			}
		}
	}
	return false
}

// PromoteToolImages 保留工具回执，在整组连续工具结果结束后追加 user 图片消息。
// 上游的工具角色仅保证接受文本；不能在并行工具回执之间插入 user 消息。
func PromoteToolImages(messages []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	var images []any
	flush := func() {
		if len(images) > 0 {
			out = append(out, map[string]any{"role": "user", "content": images})
			images = nil
		}
	}
	for _, message := range messages {
		if message["role"] != "tool" {
			flush()
			out = append(out, message)
			continue
		}
		content := NormalizeToolOutput(message["content"])
		parts := asAnySlice(content)
		if !containsImage(parts) {
			out = append(out, message)
			continue
		}
		var text []any
		for _, part := range parts {
			if m, ok := part.(map[string]any); ok {
				if image, ok := imagePart(m); ok {
					images = append(images, image)
					continue
				}
			}
			text = append(text, part)
		}
		copy := maps.Clone(message)
		copy["content"] = normalizeContent(text)
		if messageContentEmpty(copy["content"]) {
			copy["content"] = "[工具返回图片，见后续图片消息]"
		}
		out = append(out, copy)
	}
	flush()
	return out
}

// ResponsesContent 将 Responses 消息内容转换为保留多模态信息的 Chat 内容。
func ResponsesContent(content any) any {
	return normalizeContent(content)
}

// SystemDigestText 把 system content 压成与 gateway.systemPromptText 逐字节一致的文本：
// nil -> ""；string 零拷贝原样返回；其它类型走 json.Marshal（键序稳定）。
//
// gateway.systemPromptDigest 用**原始** opts.Messages 里的 content 算指纹（见
// internal/gateway/systemprompt.go），而 fold 收集 <client_instructions> 若用
// normalizeContent 之后的形状再 fmt.Sprint，结构化（多模态数组）system 的文本
// 表示就会分叉：json.Marshal 得 "[{...}]"，fmt.Sprint 得 "[map[...]]"，同一份
// 输入在 keep / fold 两条事件里 SystemLen 对不上，"按指纹对照同一 system"被破坏。
// 此函数由 provider 导出，供 EnsureUpstreamMessagesWith 复用同一口径；gateway
// 包内小写同语义实现保持不变，两侧必须同步修改。
func SystemDigestText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}
