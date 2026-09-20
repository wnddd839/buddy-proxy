package openai

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// ---------------------------------------------------------------------------
// Responses API 出站：Response 对象
// ---------------------------------------------------------------------------

// ResponseObject 是 POST /v1/responses 非流式响应体，也内嵌在
// response.created / response.completed 流事件里。
type ResponseObject struct {
	ID                 string                `json:"id"`
	Object             string                `json:"object"`
	CreatedAt          int64                 `json:"created_at"`
	Status             string                `json:"status"`
	Error              *ResponseError        `json:"error"`
	IncompleteDetails  any                   `json:"incomplete_details"`
	Instructions       any                   `json:"instructions"`
	MaxOutputTokens    any                   `json:"max_output_tokens"`
	Model              string                `json:"model"`
	Output             []ResponsesOutputItem `json:"output"`
	ParallelToolCalls  bool                  `json:"parallel_tool_calls"`
	PreviousResponseID any                   `json:"previous_response_id"`
	Reasoning          *ResponsesReasonOut   `json:"reasoning"`
	Store              bool                  `json:"store"`
	Temperature        float64               `json:"temperature"`
	Text               *ResponsesTextOut     `json:"text"`
	ToolChoice         any                   `json:"tool_choice"`
	Tools              []any                 `json:"tools"`
	TopP               float64               `json:"top_p"`
	Truncation         string                `json:"truncation"`
	Usage              *ResponsesUsage       `json:"usage"`
	User               any                   `json:"user"`
	Metadata           map[string]string     `json:"metadata"`
}

type ResponsesReasonOut struct {
	Effort  string `json:"effort"`
	Summary any    `json:"summary"`
}

type ResponsesTextOut struct {
	Format map[string]any `json:"format"`
}

// NewResponseObject 创建 status=in_progress 的空 Response（流式起始帧用）。
func NewResponseObject(id, model string, req *ResponsesRequest) ResponseObject {
	return responseObject(id, model, req, "in_progress", nil, nil, nil)
}

// ResponseFromTurn 把内部 Turn 渲染为完整 Response（非流式与 completed 帧共用）。
func ResponseFromTurn(turn provider.Turn, id, model string, req *ResponsesRequest) ResponseObject {
	output := OutputItemsFromTurn(turn)
	usage := ResponsesUsageFromProvider(turn.Usage)
	return responseObject(id, model, req, "completed", output, &usage, nil)
}

// FailedResponseObject 生成 status=failed 的 Response（response.failed 帧）。
func FailedResponseObject(id, model string, req *ResponsesRequest, code, message string) ResponseObject {
	return responseObject(id, model, req, "failed", nil, nil, &ResponseError{Code: code, Message: message})
}

func responseObject(id, model string, req *ResponsesRequest, status string, output []ResponsesOutputItem, usage *ResponsesUsage, respErr *ResponseError) ResponseObject {
	if output == nil {
		output = []ResponsesOutputItem{}
	}
	obj := ResponseObject{
		ID:        id,
		Object:    "response",
		Status:    status,
		Error:     respErr,
		Model:     model,
		Output:    output,
		Usage:     usage,
		Tools:     []any{},
		Metadata:  map[string]string{},
		Text:      &ResponsesTextOut{Format: map[string]any{"type": "text"}},
		CreatedAt: time.Now().Unix(),
		Reasoning: &ResponsesReasonOut{Effort: "", Summary: nil},
	}
	if req == nil {
		obj.ParallelToolCalls = true
		obj.Store = true
		obj.Temperature = 1
		obj.TopP = 1
		obj.Truncation = "disabled"
		obj.ToolChoice = "auto"
		return obj
	}
	if req.Instructions != "" {
		obj.Instructions = req.Instructions
	}
	obj.ParallelToolCalls = true
	if req.ParallelToolCalls != nil {
		obj.ParallelToolCalls = *req.ParallelToolCalls
	}
	if req.MaxOutputTokens != nil {
		obj.MaxOutputTokens = *req.MaxOutputTokens
	}
	if req.PreviousResponseID != "" {
		obj.PreviousResponseID = req.PreviousResponseID
	}
	if req.Reasoning != nil {
		obj.Reasoning = &ResponsesReasonOut{Effort: req.Reasoning.Effort, Summary: req.Reasoning.Summary}
	}
	obj.Store = true
	if req.Store != nil {
		obj.Store = *req.Store
	}
	obj.Temperature = 1
	if req.Temperature != nil {
		obj.Temperature = *req.Temperature
	}
	obj.TopP = 1
	if req.TopP != nil {
		obj.TopP = *req.TopP
	}
	obj.Truncation = "disabled"
	if strings.TrimSpace(req.Truncation) != "" {
		obj.Truncation = req.Truncation
	}
	obj.ToolChoice = "auto"
	if req.ToolChoice != nil {
		obj.ToolChoice = req.ToolChoice
	}
	if req.User != "" {
		obj.User = req.User
	}
	if req.Metadata != nil {
		obj.Metadata = req.Metadata
	}
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, t.echoForResponse())
		}
		obj.Tools = tools
	}
	return obj
}

// echoForResponse 把请求里的 tool 回显到响应 tools 数组（Responses 扁平形态）。
func (t ResponsesTool) echoForResponse() map[string]any {
	out := map[string]any{"type": "function"}
	if typ := strings.ToLower(strings.TrimSpace(t.Type)); typ != "" {
		out["type"] = typ
	}
	name, desc := t.Name, t.Description
	params, strict := t.Parameters, t.Strict
	if t.Function != nil {
		out["type"] = "function"
		name, desc = t.Function.Name, t.Function.Description
		params, strict = t.Function.Parameters, t.Function.Strict
	}
	if name != "" {
		out["name"] = name
	}
	if desc != "" {
		out["description"] = desc
	}
	if len(params) > 0 {
		var parsed any
		if err := json.Unmarshal(params, &parsed); err == nil {
			out["parameters"] = parsed
		}
	}
	if strict != nil {
		out["strict"] = *strict
	}
	return out
}
