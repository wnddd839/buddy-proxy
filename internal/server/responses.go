package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/httputil"
	"github.com/wnddd839/codebuddy-proxy/internal/openai"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

// handleResponsesAuth 对 /v1/responses 做 API Key 鉴权后分发。
func (s *Server) handleResponsesAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleResponses(w, r, keySite)
}

// writeResponsesError 以 Responses API 的错误形态返回错误。
// Responses 错误结构与 chat 不同：顶层 {error:{code,message}}，无 type 字段。
func (s *Server) writeResponsesError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, status, map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request, keySite string) {
	var req openai.ResponsesRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		if errors.Is(err, httputil.ErrBodyTooLarge) {
			s.writeResponsesError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds 64MB.")
			return
		}
		s.writeResponsesError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		s.writeResponsesError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	// background 需要异步任务存储，本代理无服务端状态，明确拒绝。
	if req.Background {
		s.writeResponsesError(w, http.StatusBadRequest, "unsupported_parameter", "background mode is not supported by this proxy")
		return
	}

	messages, tools, toolChoice := req.ToMessages()
	if len(messages) == 0 {
		s.writeResponsesError(w, http.StatusBadRequest, "invalid_request", "input or instructions is required")
		return
	}

	providerModel := gateway.ResolveProviderModel(req.Model)
	promptChars := estimatePromptChars(messages)
	chatStarted := time.Now()
	proxyRequestID := s.newProxyRequestID()
	finish := s.Svc.BeginRequest(providerModel.PublicModel, promptChars, req.Stream)
	site := resolveRequestSite(providerModel.Site, r.Header.Get("X-Site"), keySite)

	// session 钉号：Codex 传 prompt_cache_key，或按消息指纹。
	sessionKey := sessionpin.Key(r.Header, req.PromptCacheKey, messages)
	if req.PreviousResponseID != "" && sessionKey == "" {
		sessionKey = "resp_prev_" + req.PreviousResponseID
	}

	completeOpts := gateway.CompleteOptions{
		Model:               providerModel.Model,
		Messages:            messages,
		Tools:               tools,
		ToolChoice:          toolChoice,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		MaxCompletionTokens: req.MaxTokens(),
		ReasoningEffort:     req.ReasoningEffort(),
		SessionKey:          sessionKey,
		SessionLabel:        sessionpin.SessionLabel(messages),
		Site:                site,
	}

	responseID := "resp_" + strutil.RandomHex(12)

	if req.Stream {
		s.streamResponses(w, r, &req, responseID, providerModel, completeOpts, chatStarted, proxyRequestID, finish)
		return
	}

	completeOpts.Stream = false
	result, err := s.Svc.CompleteFromPool(r.Context(), completeOpts)
	if err != nil {
		if openai.IsClientCanceled(err) {
			s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, true, "", provider.Usage{}, nil)
			finish(true, 0, 0, 0, 0, "", provider.Usage{})
			return
		}
		s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, false, err.Error(), provider.Usage{}, nil)
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		typ, _ := openai.ClassifyUpstream(err)
		s.writeResponsesError(w, http.StatusBadGateway, typ, err.Error())
		return
	}
	s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, true, "", result.Turn.Usage, &result)
	finish(true, len(result.Turn.Text), result.Bytes, int64(result.EventCount), int64(result.DeltaCount), "", result.Turn.Usage)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, http.StatusOK, openai.ResponseFromTurn(result.Turn, responseID, providerModel.PublicModel, &req))
}

// streamResponses 驱动 Responses API 的 SSE 流（Codex 兼容）。
func (s *Server) streamResponses(
	w http.ResponseWriter,
	r *http.Request,
	req *openai.ResponsesRequest,
	responseID string,
	providerModel gateway.ProviderModel,
	opts gateway.CompleteOptions,
	chatStarted time.Time,
	proxyRequestID string,
	finish func(bool, int, int64, int64, int64, string, provider.Usage),
) {
	sse, ok := httputil.NewSSEStream(w, 16<<10)
	if !ok {
		s.writeResponsesError(w, http.StatusInternalServerError, "internal_error", "streaming unsupported")
		return
	}
	builder := openai.NewResponseStreamBuilder(responseID, providerModel.PublicModel, req)
	var (
		mu               sync.Mutex
		started          bool
		done             bool
		streamedChars    int
		streamedThinking int
		toolCallsSeen    int
	)
	writeLocked := func(fn func()) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return
		}
		fn()
	}
	emit := func(events []openai.ResponseStreamEvent) {
		for _, ev := range events {
			_ = sse.WriteNamedEvent(ev.Type, ev)
		}
	}
	startStream := func() {
		if started {
			return
		}
		started = true
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		emit(builder.CreatedEvents())
		_ = sse.Flush()
	}

	// 立即打开 SSE 并发出 created/in_progress，Codex 据此确认请求已被接受。
	writeLocked(startStream)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	keepAlive := s.Svc.Config().StreamKeepAlive
	if keepAlive <= 0 {
		keepAlive = 5 * time.Second
	}
	keepAliveStop := make(chan struct{})
	go func() {
		safeCall(s.Svc.Log, "responses-stream-keep-alive", func() {
			ticker := time.NewTicker(keepAlive)
			defer ticker.Stop()
			for {
				select {
				case <-keepAliveStop:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					writeLocked(func() {
						_ = sse.WriteComment("keep-alive")
					})
				}
			}
		})
	}()
	defer close(keepAliveStop)

	opts.Stream = true
	// 上游事件回调：tool_use 完整到达 / arguments 增量。
	opts.OnEvent = func(event provider.Event) {
		writeLocked(func() {
			switch event.Type {
			case "tool_use":
				// codebuddy_sse 风格：完整 tool_use 一次性到达。
				toolCallsSeen++
				emit(builder.ToolCallStart(event.ID, event.Name))
				if len(event.Input) > 0 {
					emit(builder.ToolCallDelta(mustJSON(event.Input)))
					emit(builder.ToolCallDone())
				}
			case "tool_call_delta":
				// OpenAI 风格上游：分片到达。首个分片携带 id/name，需要先开 item。
				if event.ID != "" || event.Name != "" {
					toolCallsSeen++
					emit(builder.ToolCallStart(event.ID, event.Name))
				}
				if event.ArgumentsDelta != "" {
					if !builder.ToolOpen() {
						emit(builder.ToolCallStart("", ""))
					}
					emit(builder.ToolCallDelta(event.ArgumentsDelta))
				}
			}
		})
	}
	opts.OnDelta = func(delta string) {
		if delta == "" {
			return
		}
		writeLocked(func() {
			if !builder.MessageOpen() {
				emit(builder.MessageStart())
			}
			streamedChars += len(delta)
			emit(builder.MessageDelta(delta))
		})
	}
	opts.OnThinkingDelta = func(delta string) {
		if delta == "" {
			return
		}
		writeLocked(func() {
			if !builder.ReasoningOpen() {
				emit(builder.ReasoningStart())
			}
			streamedThinking += len(delta)
			emit(builder.ReasoningDelta(delta))
		})
	}

	result, err := s.Svc.CompleteFromPool(ctx, opts)
	if err != nil {
		if openai.IsClientCanceled(err) {
			s.recordChatJournal(chatStarted, proxyRequestID, opts, providerModel.PublicModel, true, true, "", provider.Usage{}, nil)
			finish(true, streamedChars, 0, 0, 0, "", provider.Usage{})
			writeLocked(func() { done = true })
			return
		}
		s.recordChatJournal(chatStarted, proxyRequestID, opts, providerModel.PublicModel, true, false, err.Error(), provider.Usage{}, nil)
		finish(false, streamedChars, 0, 0, 0, err.Error(), provider.Usage{})
		typ, _ := openai.ClassifyUpstream(err)
		retryAfter := openai.RetryAfter(err)
		writeLocked(func() {
			if retryAfter != "" && !started {
				w.Header().Set("Retry-After", retryAfter)
			}
			if !started {
				s.writeResponsesError(w, http.StatusBadGateway, typ, err.Error())
				done = true
				return
			}
			emit([]openai.ResponseStreamEvent{builder.Failed(typ, err.Error())})
			_ = sse.WriteDone()
			done = true
		})
		return
	}
	s.recordChatJournal(chatStarted, proxyRequestID, opts, providerModel.PublicModel, true, true, "", result.Turn.Usage, &result)
	finish(true, len(result.Turn.Text), result.Bytes, int64(result.EventCount), int64(result.DeltaCount), "", result.Turn.Usage)
	writeLocked(func() {
		// 补偿非流式上游：回调未触发时用 Turn 补发完整内容。
		if streamedThinking == 0 && result.Turn.Thinking != "" {
			emit(builder.ReasoningStart())
			emit(builder.ReasoningDelta(result.Turn.Thinking))
		}
		if streamedChars == 0 && result.Turn.Text != "" {
			if !builder.MessageOpen() {
				emit(builder.MessageStart())
			}
			emit(builder.MessageDelta(result.Turn.Text))
		}
		if len(result.Turn.ToolUses) > 0 && toolCallsSeen == 0 {
			for _, tool := range result.Turn.ToolUses {
				emit(builder.ToolCallStart(tool.ID, tool.Name))
				emit(builder.ToolCallDelta(mustJSON(tool.Input)))
				emit(builder.ToolCallDone())
			}
		}
		emit(builder.Completed(result.Turn))
		_ = sse.WriteDone()
		done = true
	})
}
