package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/httputil"
	"github.com/wnddd839/codebuddy-proxy/internal/openai"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// prepareBuiltinSearch 执行「模型决定搜索 → 代理调用官方搜索 → 模型作答」。
//
// 上游 chat 不支持 OpenAI 内置 web_search，因此 ToMessages 会把它桥接成内部
// function 工具。这里只允许一轮搜索：模型首轮若调用 web_search，代理用账号池
// token 调官方 /agenttool/v1/search，把结果回填后再跑一轮，并在这一轮移掉
// web_search 工具，防止模型无休止搜索。客户端只会看到最终答案。
func (s *Server) prepareBuiltinSearch(
	ctx context.Context,
	opts gateway.CompleteOptions,
	site string,
) (gateway.CompleteOptions, *gateway.CompleteResult, error) {
	roundOpts := opts
	roundOpts.OnDelta = nil
	roundOpts.OnThinkingDelta = nil
	roundOpts.OnEvent = nil
	// 每轮用独立的 SystemOutcome：CompleteFromPool 每轮都会覆写 Form/Fingerprint，
	// 而 Degraded 只置 true 不清零；两轮共享一个指针会把「最后一轮形态 + 期间
	// 是否降级过」混成一个 06 面板里根本不存在的组合值。调用方的 07 用量明细
	// 由 completeWithRequestedSearch 把本函数返回的最终形态写回共享指针，
	// 成功 / 失败路径都能拿到合理值。
	if opts.SystemOutcome != nil {
		roundOpts.SystemOutcome = &gateway.SystemPromptOutcome{}
	}
	first, err := s.Svc.CompleteFromPool(ctx, roundOpts)
	if err != nil {
		return roundOpts, nil, err
	}
	searchCalls := onlyWebSearchCalls(first.Turn.ToolUses)
	if len(searchCalls) == 0 {
		return roundOpts, &first, nil
	}

	assistant := map[string]any{
		"role":    "assistant",
		"content": first.Turn.Text,
	}
	toolCalls := make([]map[string]any, 0, len(searchCalls))
	for _, call := range searchCalls {
		toolCalls = append(toolCalls, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": mustJSON(call.Input),
			},
		})
	}
	assistant["tool_calls"] = toolCalls
	messages := append(append([]map[string]any{}, opts.Messages...), assistant)
	for _, call := range searchCalls {
		content := s.runBuiltinWebSearch(ctx, site, first.AccountID, opts.FixedAccount, call.Input)
		messages = append(messages, map[string]any{
			"role":         "tool",
			"tool_call_id": call.ID,
			"content":      content,
		})
	}
	s.Svc.Log.Info("responses builtin web_search executed",
		"calls", len(searchCalls),
		"accountId", first.AccountID,
	)

	roundOpts.Messages = messages
	roundOpts.Tools = openai.WithoutBuiltinWebSearch(opts.Tools)
	if roundOpts.Tools == nil {
		roundOpts.ToolChoice = nil
	} else if choice, ok := roundOpts.ToolChoice.(string); ok && choice == "required" {
		// 首轮已执行 required 的内置工具，末轮不再强制调用客户端工具。
		roundOpts.ToolChoice = "auto"
	}
	// 第二轮用全新 outcome，不带入第一轮的 Degraded / Form。
	if opts.SystemOutcome != nil {
		roundOpts.SystemOutcome = &gateway.SystemPromptOutcome{}
	}
	final, err := s.Svc.CompleteFromPool(ctx, roundOpts)
	if err != nil {
		return roundOpts, nil, err
	}
	final.Turn.Usage = sumSearchUsage(first.Turn.Usage, final.Turn.Usage)
	final.Bytes += first.Bytes
	final.EventCount += first.EventCount
	final.DeltaCount += first.DeltaCount
	final.DurationMs += first.DurationMs
	// 部分模型即使工具已被移除仍会幻觉出 web_search 调用。客户端不认识这个
	// 代理内部工具，必须过滤，否则 Codex 会尝试自行执行。
	final.Turn.ToolUses = filterBuiltinSearchCalls(final.Turn.ToolUses)
	if strings.TrimSpace(final.Turn.Text) == "" && len(final.Turn.ToolUses) == 0 {
		final.Turn.Text = fallbackSearchSummary(searchCalls)
	}
	return roundOpts, &final, nil
}

func sumSearchUsage(first, final provider.Usage) provider.Usage {
	result := provider.Usage{
		PromptTokens:     first.PromptTokens + final.PromptTokens,
		CompletionTokens: first.CompletionTokens + final.CompletionTokens,
		TotalTokens:      first.TotalTokens + final.TotalTokens,
		Source:           final.Source,
	}
	if cached := first.CachedTokens() + final.CachedTokens(); cached > 0 {
		result.PromptTokensDetails = &provider.PromptTokensDetails{CachedTokens: cached}
	}
	for _, usage := range []provider.Usage{first, final} {
		if usage.CompletionTokensDetails != nil {
			if result.CompletionTokensDetails == nil {
				result.CompletionTokensDetails = &provider.CompletionTokensDetails{}
			}
			result.CompletionTokensDetails.ReasoningTokens += usage.CompletionTokensDetails.ReasoningTokens
		}
		if usage.Source == "estimated" {
			result.Source = "estimated"
		}
	}
	// 缺失扣费值不估算，也不把另一轮的扣费误报成整个请求的费用。
	if first.Credit != nil && final.Credit != nil {
		result.Credit = new(*first.Credit + *final.Credit)
	}
	return result
}

func filterBuiltinSearchCalls(calls []provider.ToolUse) []provider.ToolUse {
	out := calls[:0]
	for _, call := range calls {
		if strings.EqualFold(strings.TrimSpace(call.Name), openai.BuiltinWebSearchToolName) {
			continue
		}
		out = append(out, call)
	}
	return out
}

// fallbackSearchSummary 在模型只吐工具调用、没有正文时给出可读兜底，
// 避免客户端收到一个空 message。
func fallbackSearchSummary(calls []provider.ToolUse) string {
	var query string
	for _, call := range calls {
		if value, ok := call.Input["query"].(string); ok && strings.TrimSpace(value) != "" {
			query = strings.TrimSpace(value)
			break
		}
	}
	if query == "" {
		return "模型未返回可读结论；请使用 /v1/search 检索并检查官方接口返回。"
	}
	return "模型未能根据搜索回执生成可读结论（查询：" + query + "）；可使用 /v1/search 检查官方接口返回。"
}

// onlyWebSearchCalls 只保留内置搜索调用，忽略客户端自己的工具调用。
func onlyWebSearchCalls(calls []provider.ToolUse) []provider.ToolUse {
	out := make([]provider.ToolUse, 0, len(calls))
	for _, call := range calls {
		if strings.EqualFold(strings.TrimSpace(call.Name), openai.BuiltinWebSearchToolName) {
			out = append(out, call)
		}
	}
	return out
}

// runBuiltinWebSearch 执行一次内置搜索并返回给模型的工具结果文本。
// 搜索失败不让整轮对话失败：把错误文本回填给模型，由模型决定改述或放弃。
func (s *Server) runBuiltinWebSearch(ctx context.Context, site, accountID string, fixedAccount bool, input map[string]any) string {
	query, _ := input["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return `{"error":"query is required"}`
	}
	req := provider.WebSearchRequest{Query: query}
	if raw, ok := input["allowed_domains"].([]any); ok {
		for _, item := range raw {
			if domain, ok := item.(string); ok && strings.TrimSpace(domain) != "" {
				req.AllowedDomains = append(req.AllowedDomains, domain)
			}
		}
	}
	if freshness, ok := input["freshness"].(string); ok {
		req.Freshness = strings.TrimSpace(freshness)
	}
	searchCtx, cancel := context.WithTimeout(ctx, searchUpstreamTimeout)
	defer cancel()
	result, _, err := s.Svc.WebSearchFromPool(searchCtx, gateway.SearchOptions{
		Site:         site,
		AccountID:    accountID,
		FixedAccount: fixedAccount,
	}, req)
	if err != nil {
		s.Svc.Log.Warn("responses builtin web_search failed", "accountId", accountID, "error", err.Error())
		payload, _ := json.Marshal(map[string]any{"error": err.Error()})
		return string(payload)
	}
	payload, err := json.Marshal(map[string]any{
		"query":         result.Query,
		"total_results": result.TotalResults,
		"results":       result.Results,
	})
	if err != nil {
		return `{"error":"failed to encode search results"}`
	}
	return string(payload)
}

// handleResponsesWithSearch 处理带内置 web_search 的 Responses 请求。
// 搜索闭环在服务端完成：非流式返回一个完整 response 对象；流式则按
// Responses 命名事件把最终答案回放给客户端。
func (s *Server) handleResponsesWithSearch(
	w http.ResponseWriter,
	r *http.Request,
	req *openai.ResponsesRequest,
	responseID string,
	providerModel gateway.ProviderModel,
	opts gateway.CompleteOptions,
	started time.Time,
	proxyRequestID string,
	finish func(bool, int, int64, int64, int64, string, provider.Usage),
) {
	if req.Stream {
		s.streamResponses(w, r, req, responseID, providerModel, opts, started, proxyRequestID, finish)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), responsesSearchOverallTimeout)
	defer cancel()
	roundOpts, result, err := s.prepareBuiltinSearch(ctx, opts, opts.Site)
	if err != nil {
		if openai.IsClientCanceled(err) {
			s.recordChatJournal(started, proxyRequestID, roundOpts, providerModel.PublicModel, req.Stream, true, "", provider.Usage{}, nil)
			finish(true, 0, 0, 0, 0, "", provider.Usage{})
			return
		}
		s.recordChatJournal(started, proxyRequestID, roundOpts, providerModel.PublicModel, req.Stream, false, err.Error(), provider.Usage{}, nil)
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		typ, _ := openai.ClassifyUpstream(err)
		s.writeResponsesError(w, http.StatusBadGateway, typ, err.Error())
		return
	}
	roundOpts.Stream = req.Stream
	s.recordChatJournal(started, proxyRequestID, roundOpts, providerModel.PublicModel, false, true, "", result.Turn.Usage, result)
	finish(true, len(result.Turn.Text), result.Bytes, int64(result.EventCount), int64(result.DeltaCount), "", result.Turn.Usage)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, http.StatusOK, openai.ResponseFromTurn(result.Turn, responseID, providerModel.PublicModel, req))
}
