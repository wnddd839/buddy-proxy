package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code,omitempty"`
}

func NewError(message, typ string) ErrorBody {
	return ErrorBody{Error: ErrorDetail{Message: message, Type: typ}}
}

func NewErrorWithCode(message, typ string, code any) ErrorBody {
	return ErrorBody{Error: ErrorDetail{Message: message, Type: typ, Code: code}}
}

// ClassifyUpstream 将 CodeBuddy 上游错误映射为 OpenAI 风格 type/code，
// 便于 Orca/ZCode/NewAPI 等客户端区分策略错误与传输错误。
func ClassifyUpstream(err error) (typ string, code any) {
	if err == nil {
		return "upstream_error", nil
	}
	if IsClientCanceled(err) {
		return "client_disconnected", "context_canceled"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "11128"), strings.Contains(strings.ToLower(msg), "unapproved channel"):
		return "permission_error", 11128
	case strings.Contains(msg, "11101"):
		return "invalid_request_error", 11101
	case strings.Contains(msg, "11102"):
		return "invalid_request_error", 11102
	case strings.Contains(msg, "11140"), strings.Contains(strings.ToLower(msg), "request illegal"):
		return "invalid_request_error", 11140
	default:
		return "upstream_error", nil
	}
}

// IsClientCanceled 判断是否为客户端主动断开（浏览器/ZCode 关闭流）。
func IsClientCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "request canceled") ||
		strings.Contains(msg, "client disconnected")
}

type ChatCompletion struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Delta   `json:"delta,omitempty"`
	FinishReason *string  `json:"finish_reason"`
}

type Message struct {
	Role             string     `json:"role"`
	Content          any        `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

type Delta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCall 用于非流式 message.tool_calls；OpenAI 该路径不含 index。
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function"`
}

// ToolCallDelta 用于流式 delta.tool_calls。
// index 为规范必填（含 0），禁止 omitempty/omitzero，否则首个工具调用会被序列化丢弃。
type ToolCallDelta struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

type Usage struct {
	PromptTokens             int                      `json:"prompt_tokens"`
	CompletionTokens         int                      `json:"completion_tokens"`
	TotalTokens              int                      `json:"total_tokens"`
	PromptTokensDetails      *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails  *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
	CacheReadInputTokens     int                      `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int                      `json:"cache_creation_input_tokens,omitempty"`
	PromptCacheHitTokens     int                      `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens    int                      `json:"prompt_cache_miss_tokens,omitempty"`
	Source                   string                   `json:"usage_source,omitempty"`
}

func UsageFromProvider(u provider.Usage) Usage {
	out := Usage{
		PromptTokens:             u.PromptTokens,
		CompletionTokens:         u.CompletionTokens,
		TotalTokens:              u.TotalTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		PromptCacheHitTokens:     u.PromptCacheHitTokens,
		PromptCacheMissTokens:    u.PromptCacheMissTokens,
		Source:                   u.Source,
	}
	if u.CompletionTokensDetails != nil && u.CompletionTokensDetails.ReasoningTokens > 0 {
		out.CompletionTokensDetails = &CompletionTokensDetails{ReasoningTokens: u.CompletionTokensDetails.ReasoningTokens}
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	// 出站补齐常用 cache 别名，避免 CCSwitch / DeepSeek / Anthropic
	// 转换器只认其中一种字段时丢掉命中率。
	cached := u.CachedTokens()
	if cached > 0 {
		out.PromptTokensDetails = &PromptTokensDetails{CachedTokens: cached}
		if out.PromptCacheHitTokens == 0 {
			out.PromptCacheHitTokens = cached
		}
		if out.CacheReadInputTokens == 0 {
			out.CacheReadInputTokens = cached
		}
	}
	if out.PromptCacheMissTokens == 0 && out.PromptTokens > cached {
		out.PromptCacheMissTokens = out.PromptTokens - cached
	}
	return out
}

type StreamChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

func FromTurn(turn provider.Turn, id, model string) ChatCompletion {
	if id == "" {
		id = fmt.Sprintf("chatcmpl_%d", time.Now().UnixMilli())
	}
	toolCalls := make([]ToolCall, 0, len(turn.ToolUses))
	for _, tool := range turn.ToolUses {
		toolCalls = append(toolCalls, ToolCall{
			ID:   strutil.First(tool.ID, fmt.Sprintf("call_%d", time.Now().UnixNano())),
			Type: "function",
			Function: ToolFunction{
				Name:      strutil.First(tool.Name, "tool"),
				Arguments: mustJSON(tool.Input),
			},
		})
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
	}
	var content any = turn.Text
	if turn.Text == "" {
		content = nil
	}
	msg := &Message{Role: "assistant", Content: content}
	if turn.Thinking != "" {
		msg.ReasoningContent = turn.Thinking
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	return ChatCompletion{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: &finish,
		}},
		Usage: UsageFromProvider(turn.Usage),
	}
}

func StreamChunkOf(id, model string, delta Delta, finishReason *string) StreamChunk {
	return StreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{
			Index:        0,
			Delta:        &delta,
			FinishReason: finishReason,
		}},
	}
}

// StreamUsageChunk 发送 OpenAI include_usage 风格的收尾 chunk（空 choices + usage）。
func StreamUsageChunk(id, model string, usage Usage) StreamChunk {
	u := usage
	return StreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{},
		Usage:   &u,
	}
}

func mustJSON(value map[string]any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
