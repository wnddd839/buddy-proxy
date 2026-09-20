package openai

import (
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

// ---------------------------------------------------------------------------
// Responses API 出站：usage
// ---------------------------------------------------------------------------

// ResponsesUsage 是 Responses API 的 usage 形态。
type ResponsesUsage struct {
	InputTokens         int                           `json:"input_tokens"`
	InputTokensDetails  *ResponsesInputTokensDetails  `json:"input_tokens_details"`
	OutputTokens        int                           `json:"output_tokens"`
	OutputTokensDetails *ResponsesOutputTokensDetails `json:"output_tokens_details"`
	TotalTokens         int                           `json:"total_tokens"`
}

type ResponsesInputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type ResponsesOutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// ResponsesUsageFromProvider 把内部 Usage 转成 Responses usage。
func ResponsesUsageFromProvider(u provider.Usage) ResponsesUsage {
	out := ResponsesUsage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	reasoning := 0
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	out.InputTokensDetails = &ResponsesInputTokensDetails{CachedTokens: u.CachedTokens()}
	out.OutputTokensDetails = &ResponsesOutputTokensDetails{ReasoningTokens: reasoning}
	return out
}

// ---------------------------------------------------------------------------
// Responses API 出站：output items
// ---------------------------------------------------------------------------

// ResponseError 是 Responses API 的错误对象（response.failed / status=failed）。
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ResponsesOutputContent 是 message item 的 content 分片。
type ResponsesOutputContent struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

// ResponsesOutputItem 是 output 数组的元素：
// reasoning / message / function_call 复用同一结构，omitempty 裁剪。
type ResponsesOutputItem struct {
	ID        string                   `json:"id,omitempty"`
	Type      string                   `json:"type"`
	Status    string                   `json:"status,omitempty"`
	Role      string                   `json:"role,omitempty"`
	Content   []ResponsesOutputContent `json:"content,omitempty"`
	Summary   []any                    `json:"summary,omitempty"`
	CallID    string                   `json:"call_id,omitempty"`
	Name      string                   `json:"name,omitempty"`
	Arguments string                   `json:"arguments,omitempty"`
}

// OutputItemsFromTurn 把内部 Turn 渲染为 Responses output 数组。
// 顺序：reasoning → message → function_call（与 OpenAI 实际输出一致）。
func OutputItemsFromTurn(turn provider.Turn) []ResponsesOutputItem {
	items := make([]ResponsesOutputItem, 0, 3)
	if turn.Thinking != "" {
		items = append(items, ResponsesOutputItem{
			ID:      NewItemID("rs"),
			Type:    "reasoning",
			Status:  "completed",
			Summary: reasoningSummary(turn.Thinking),
		})
	}
	if turn.Text != "" || len(turn.ToolUses) == 0 {
		items = append(items, ResponsesOutputItem{
			ID:     NewItemID("msg"),
			Type:   "message",
			Status: "completed",
			Role:   "assistant",
			Content: []ResponsesOutputContent{{
				Type:        "output_text",
				Text:        turn.Text,
				Annotations: []any{},
			}},
		})
	}
	for _, tool := range turn.ToolUses {
		items = append(items, FunctionCallOutputItem(tool))
	}
	return items
}

// FunctionCallOutputItem 渲染单个 function_call item。
func FunctionCallOutputItem(tool provider.ToolUse) ResponsesOutputItem {
	callID := tool.ID
	if callID == "" {
		callID = NewItemID("call")
	}
	name := strings.TrimSpace(tool.Name)
	if name == "" {
		name = "tool"
	}
	return ResponsesOutputItem{
		ID:        NewItemID("fc"),
		Type:      "function_call",
		Status:    "completed",
		CallID:    callID,
		Name:      name,
		Arguments: mustJSON(tool.Input),
	}
}

// NewItemID 生成 Responses 风格的 item id（rs_/msg_/fc_/call_ 前缀）。
// 用随机字节，避免 Windows 上 UnixNano 分辨率不够导致同一响应内撞 id。
func NewItemID(prefix string) string {
	return prefix + "_" + strutil.RandomHex(8)
}

func reasoningSummary(text string) []any {
	if strings.TrimSpace(text) == "" {
		return []any{}
	}
	return []any{map[string]any{"type": "summary_text", "text": text}}
}
