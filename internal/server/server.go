package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/admin"
	"github.com/wnddd839/codebuddy-proxy/internal/billing"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/httputil"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
	"github.com/wnddd839/codebuddy-proxy/internal/openai"
	"github.com/wnddd839/codebuddy-proxy/internal/opencode"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
	"github.com/wnddd839/codebuddy-proxy/internal/version"
)

type Server struct {
	Svc  *gateway.Service
	HTTP *http.Server
}

func New(cfg config.Config, svc *gateway.Service) *Server {
	s := &Server{Svc: svc}
	mux := http.NewServeMux()
	// Go 1.22+ 方法感知路由（http_servemux_patterns）
	// 避免与方法相关的尾斜杠子树与更深层精确路由冲突。
	mux.HandleFunc("OPTIONS /{$}", s.handleOptions)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("HEAD /health", s.handleHealth)
	mux.HandleFunc("GET /v1/models", s.handleModelsAuth)
	mux.HandleFunc("GET /models", s.handleModelsAuth)
	mux.HandleFunc("GET /v1/model/info", s.handleModelInfoAuth)
	mux.HandleFunc("GET /model/info", s.handleModelInfoAuth)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatAuth)
	mux.HandleFunc("POST /chat/completions", s.handleChatAuth)
	mux.HandleFunc("GET /direct-admin", s.handleAdminPage)
	mux.HandleFunc("GET /direct-admin/{$}", s.handleAdminPage)
	mux.HandleFunc("HEAD /direct-admin", s.handleAdminPage)
	mux.HandleFunc("HEAD /direct-admin/{$}", s.handleAdminPage)
	mux.HandleFunc("GET /direct-admin/codebuddy/oauth/launch", s.handleOAuthLaunch)
	mux.HandleFunc("HEAD /direct-admin/codebuddy/oauth/launch", s.handleOAuthLaunch)
	mux.HandleFunc("GET /direct-admin/codebuddy/oauth/callback", s.handleOAuthCallback)
	mux.HandleFunc("HEAD /direct-admin/codebuddy/oauth/callback", s.handleOAuthCallback)
	mux.HandleFunc("/direct-admin/api/", s.handleAdminAPIEntry)
	mux.HandleFunc("/", s.handleFallback)
	s.HTTP = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           recoverHandler(svc.Log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

func (s *Server) ListenAndServe() error {
	return s.HTTP.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	err := s.HTTP.Shutdown(ctx)
	if s.Svc != nil {
		if cerr := s.Svc.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"provider":  "codebuddy",
		"transport": s.Svc.Config().Transport,
		"version":   version.Version,
	})
}

func (s *Server) handleModelsAuth(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPI(w, r) {
		return
	}
	s.handleModels(w, r)
}

func (s *Server) handleChatAuth(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPI(w, r) {
		return
	}
	s.handleChatCompletions(w, r)
}

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAdmin(w, r) {
		return
	}
	httputil.WriteHTML(w, http.StatusOK, admin.PageHTML())
}

func (s *Server) handleAdminAPIEntry(w http.ResponseWriter, r *http.Request) {
	path := httputil.NormalizePath(r.URL.Path)
	s.handleAdminAPI(w, r, path)
}

func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.handleOptions(w, r)
		return
	}
	path := httputil.NormalizePath(r.URL.Path)
	httputil.WriteJSON(w, http.StatusNotFound, openai.NewError(fmt.Sprintf("Unsupported route: %s", path), "not_found_error"))
}

func (s *Server) authorizeAPI(w http.ResponseWriter, r *http.Request) bool {
	cfg := s.Svc.Config()
	if !cfg.RequireAPIKey {
		return true
	}
	token := httputil.BearerToken(r)
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	if token == "" || !secretEqual(token, cfg.APIKey) {
		httputil.WriteJSON(w, http.StatusUnauthorized, openai.NewError("Missing or invalid API key", "authentication_error"))
		return false
	}
	return true
}

func (s *Server) authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	cfg := s.Svc.Config()
	if cfg.AdminPassword == "" {
		return true
	}
	user, pass, ok := r.BasicAuth()
	if ok && secretEqual(pass, cfg.AdminPassword) && (user == "" || user == "admin") {
		return true
	}
	token := httputil.BearerToken(r)
	if token != "" && (secretEqual(token, cfg.AdminPassword) || secretEqual(token, cfg.APIKey)) {
		return true
	}
	// 故意不支持 ?password= 查询参数鉴权：URL 中的密钥会进入代理日志、
	// 浏览器历史与 Referer。请用 Basic Auth 或 Bearer。
	w.Header().Set("WWW-Authenticate", `Basic realm="CodeBuddy Admin"`)
	httputil.WriteJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "admin auth required"})
	return false
}

func secretEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) handleModelInfoAuth(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPI(w, r) {
		return
	}
	s.handleModelInfo(w, r)
}

func (s *Server) handleModelInfo(w http.ResponseWriter, r *http.Request) {
	fresh := queryTruthy(r.URL.Query().Get("fresh"))
	listed, err := s.Svc.ListModels(r.Context(), fresh)
	models := listed.Models
	if err != nil || len(models) == 0 {
		models = s.Svc.ConfiguredModels()
	}
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if model.ID == "" {
			continue
		}
		data = append(data, opencode.LiteLLMModelInfoEntry(model))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	fresh := queryTruthy(r.URL.Query().Get("fresh"))
	listed, err := s.Svc.ListModels(r.Context(), fresh)
	models := listed.Models
	if err != nil || len(models) == 0 {
		models = s.Svc.ConfiguredModels()
	}
	created := time.Now().Unix()
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		item := map[string]any{
			"id":       model.ID,
			"object":   "model",
			"created":  created,
			"owned_by": strutil.First(model.OwnedBy, "codebuddy"),
		}
		if model.Credits != "" {
			item["credits"] = model.Credits
		}
		if model.CreditMultiplier != nil {
			item["credit_multiplier"] = *model.CreditMultiplier
		}
		if model.Free != nil {
			item["free"] = *model.Free
		}
		if model.Description != "" {
			item["description"] = model.Description
		}
		if model.SupportsReasoning {
			item["supportsReasoning"] = true
		}
		if model.OnlyReasoning {
			item["onlyReasoning"] = true
		}
		for key, value := range opencode.ModelListFields(model) {
			item[key] = value
		}
		data = append(data, item)
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model               string           `json:"model"`
		Messages            []map[string]any `json:"messages"`
		Stream              bool             `json:"stream"`
		Tools               any              `json:"tools"`
		ToolChoice          any              `json:"tool_choice"`
		Temperature         *float64         `json:"temperature"`
		TopP                *float64         `json:"top_p"`
		MaxTokens           *int             `json:"max_tokens"`
		MaxCompletionTokens *int             `json:"max_completion_tokens"`
		ReasoningEffort     string           `json:"reasoning_effort"`
		ReasoningEffortAlt  string           `json:"reasoningEffort"`
		Reasoning           map[string]any   `json:"reasoning"`
		Thinking            map[string]any   `json:"thinking"`
		PromptCacheKey      string           `json:"prompt_cache_key"`
	}
	if err := httputil.ReadJSON(r, &body); err != nil {
		if errors.Is(err, httputil.ErrBodyTooLarge) {
			httputil.WriteJSON(w, http.StatusRequestEntityTooLarge, openai.NewError("Request body exceeds 64MB. 1M-context requests need a larger JSON body.", "invalid_request_error"))
			return
		}
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("Invalid JSON body", "invalid_request_error"))
		return
	}
	providerModel := gateway.ResolveProviderModel(body.Model)
	promptChars := estimatePromptChars(body.Messages)
	chatStarted := time.Now()
	proxyRequestID := s.newProxyRequestID()
	finish := s.Svc.BeginRequest(providerModel.PublicModel, promptChars, body.Stream)
	maxTokens := 0
	if body.MaxCompletionTokens != nil && *body.MaxCompletionTokens > 0 {
		maxTokens = *body.MaxCompletionTokens
	} else if body.MaxTokens != nil && *body.MaxTokens > 0 {
		maxTokens = *body.MaxTokens
	}
	reasoningEffort := strutil.First(body.ReasoningEffort, body.ReasoningEffortAlt)
	completeOpts := gateway.CompleteOptions{
		Model:               providerModel.Model,
		Messages:            body.Messages,
		Tools:               body.Tools,
		ToolChoice:          body.ToolChoice,
		Temperature:         body.Temperature,
		TopP:                body.TopP,
		MaxCompletionTokens: maxTokens,
		ReasoningEffort:     reasoningEffort,
		Reasoning:           body.Reasoning,
		Thinking:            body.Thinking,
		SessionKey:          sessionpin.Key(r.Header, body.PromptCacheKey, body.Messages),
		SessionLabel:        sessionpin.SessionLabel(body.Messages),
	}

	if body.Stream {
		s.streamChat(w, r, providerModel, completeOpts, chatStarted, proxyRequestID, finish)
		return
	}

	completeOpts.Stream = false
	result, err := s.Svc.CompleteFromPool(r.Context(), completeOpts)
	if err != nil {
		if openai.IsClientCanceled(err) {
			// 客户端在非流式聚合前/中主动断开，不算网关故障。
			s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, true, "", provider.Usage{}, nil)
			finish(true, 0, 0, 0, 0, "", provider.Usage{})
			return
		}
		s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, false, err.Error(), provider.Usage{}, nil)
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		typ, code := openai.ClassifyUpstream(err)
		httputil.WriteJSON(w, http.StatusBadGateway, openai.NewErrorWithCode(err.Error(), typ, code))
		return
	}
	s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, true, "", result.Turn.Usage, &result)
	finish(true, len(result.Turn.Text), result.Bytes, int64(result.EventCount), int64(result.DeltaCount), "", result.Turn.Usage)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, http.StatusOK, openai.FromTurn(result.Turn, "", providerModel.PublicModel))
}

func (s *Server) streamChat(
	w http.ResponseWriter,
	r *http.Request,
	providerModel gateway.ProviderModel,
	opts gateway.CompleteOptions,
	chatStarted time.Time,
	proxyRequestID string,
	finish func(bool, int, int64, int64, int64, string, provider.Usage),
) {
	sse, ok := httputil.NewSSEStream(w, 16<<10)
	if !ok {
		httputil.WriteJSON(w, http.StatusInternalServerError, openai.NewError("streaming unsupported", "internal_error"))
		return
	}
	id := fmt.Sprintf("chatcmpl_codebuddy_%d", time.Now().UnixMilli())
	var (
		mu                sync.Mutex
		started           bool
		done              bool
		streamedToolCalls int
		streamedChars     int
		streamedThinking  int
	)
	writeLocked := func(fn func()) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return
		}
		fn()
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
		_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{Role: "assistant"}, nil))
		_ = sse.Flush()
	}

	// 立即打开 SSE，避免客户端把上游建连延迟误判为挂起；
	// 也让 keep-alive 注释有可写的响应流。
	writeLocked(startStream)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	keepAlive := s.Svc.Config().StreamKeepAlive
	if keepAlive <= 0 {
		// 频率足够高，避免 ZCode/NewAPI 把长 TTFB 误判为挂起。
		keepAlive = 5 * time.Second
	}
	keepAliveStop := make(chan struct{})
	go func() {
		safeCall(s.Svc.Log, "stream-keep-alive", func() {
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
						startStream()
						_ = sse.WriteComment("keep-alive")
					})
				}
			}
		})
	}()
	defer close(keepAliveStop)

	opts.Stream = true
	opts.OnEvent = func(event provider.Event) {
		if event.Type != "tool_use" {
			return
		}
		writeLocked(func() {
			startStream()
			streamedToolCalls++
			_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{
				ToolCalls: []openai.ToolCall{{
					Index: streamedToolCalls - 1,
					ID:    strutil.First(event.ID, fmt.Sprintf("call_%d", time.Now().UnixNano())),
					Type:  "function",
					Function: openai.ToolFunction{
						Name:      strutil.First(event.Name, "tool"),
						Arguments: mustJSON(event.Input),
					},
				}},
			}, nil))
		})
	}
	opts.OnDelta = func(delta string) {
		if delta == "" {
			return
		}
		writeLocked(func() {
			startStream()
			streamedChars += len(delta)
			_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{Content: delta}, nil))
		})
	}
	opts.OnThinkingDelta = func(delta string) {
		if delta == "" {
			return
		}
		writeLocked(func() {
			startStream()
			streamedThinking += len(delta)
			_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{ReasoningContent: delta}, nil))
		})
	}
	result, err := s.Svc.CompleteFromPool(ctx, opts)
	if err != nil {
		if openai.IsClientCanceled(err) {
			// ZCode/浏览器常因慢模型（hy4-preview）中止并重试——
			// 表现为 context canceled，而非上游宕机。
			s.recordChatJournal(chatStarted, proxyRequestID, opts, providerModel.PublicModel, true, true, "", provider.Usage{}, nil)
			finish(true, streamedChars, 0, 0, 0, "", provider.Usage{})
			writeLocked(func() { done = true })
			return
		}
		s.recordChatJournal(chatStarted, proxyRequestID, opts, providerModel.PublicModel, true, false, err.Error(), provider.Usage{}, nil)
		finish(false, streamedChars, 0, 0, 0, err.Error(), provider.Usage{})
		typ, code := openai.ClassifyUpstream(err)
		writeLocked(func() {
			if !started {
				httputil.WriteJSON(w, http.StatusBadGateway, openai.NewErrorWithCode(err.Error(), typ, code))
				done = true
				return
			}
			errObj := map[string]any{"message": err.Error(), "type": typ}
			if code != nil {
				errObj["code"] = code
			}
			_ = sse.WriteEvent(map[string]any{"error": errObj})
			_ = sse.WriteDone()
			done = true
		})
		return
	}
	s.recordChatJournal(chatStarted, proxyRequestID, opts, providerModel.PublicModel, true, true, "", result.Turn.Usage, &result)
	finish(true, len(result.Turn.Text), result.Bytes, int64(result.EventCount), int64(result.DeltaCount), "", result.Turn.Usage)
	writeLocked(func() {
		startStream()
		if streamedChars == 0 && result.Turn.Text != "" {
			_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{Content: result.Turn.Text}, nil))
		}
		if streamedThinking == 0 && result.Turn.Thinking != "" {
			_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{ReasoningContent: result.Turn.Thinking}, nil))
		}
		if len(result.Turn.ToolUses) > 0 && streamedToolCalls == 0 {
			for i, tool := range result.Turn.ToolUses {
				_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{
					ToolCalls: []openai.ToolCall{{
						Index: i,
						ID:    strutil.First(tool.ID, fmt.Sprintf("call_%d", time.Now().UnixNano())),
						Type:  "function",
						Function: openai.ToolFunction{
							Name:      strutil.First(tool.Name, "tool"),
							Arguments: mustJSON(tool.Input),
						},
					}},
				}, nil))
			}
		}
		finishReason := "stop"
		if len(result.Turn.ToolUses) > 0 {
			finishReason = "tool_calls"
		}
		_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{}, &finishReason))
		// OpenAI stream_options.include_usage 风格：收尾 chunk 带 usage、choices 为空。
		_ = sse.WriteEvent(openai.StreamUsageChunk(id, providerModel.PublicModel, openai.UsageFromProvider(result.Turn.Usage)))
		_ = sse.Flush() // 确保 usage 在 [DONE] 前落到客户端，避免下游提前断开丢统计
		_ = sse.WriteDone()
		done = true
	})
}

func (s *Server) handleAdminAPI(w http.ResponseWriter, r *http.Request, path string) {
	if !s.authorizeAdmin(w, r) {
		return
	}
	if !httputil.AdminMutationAllowed(r) {
		httputil.WriteJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "cross-origin admin mutation blocked"})
		return
	}
	publicOrigin := httputil.PublicOrigin(r, s.Svc.Config().PublicBaseURL)
	switch {
	case path == "/direct-admin/api/status" && r.Method == http.MethodGet:
		httputil.WriteJSON(w, http.StatusOK, s.Svc.Status())
		return
	case path == "/direct-admin/api/usage" && r.Method == http.MethodGet:
		rangeName := strings.TrimSpace(r.URL.Query().Get("range"))
		// Page size default 20; usagejournal.normalizePageSize caps at 100.
		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		offset := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
				offset = n
			}
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
			if page, err := strconv.Atoi(raw); err == nil && page > 1 {
				offset = (page - 1) * limit
			}
		}
		view := s.Svc.UsageQuery(usagejournal.Query{
			Range:   rangeName,
			Account: strings.TrimSpace(r.URL.Query().Get("account")),
			Model:   strings.TrimSpace(r.URL.Query().Get("model")),
			Limit:   limit,
			Offset:  offset,
		})
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"summary":       view.Summary,
			"series":        view.Series,
			"byModel":       view.ByModel,
			"accounts":      view.Accounts,
			"models":        view.Models,
			"requests":      view.Requests,
			"requestsTotal": view.RequestsTotal,
			"limit":         view.Limit,
			"offset":        view.Offset,
		})
		return
	case path == "/direct-admin/api/pool-site" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		var body struct {
			Site string `json:"site"`
		}
		_ = httputil.ReadJSON(r, &body)
		payload, err := s.Svc.SetPoolSite(body.Site)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/pool-product" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		var body struct {
			Product string `json:"product"`
		}
		_ = httputil.ReadJSON(r, &body)
		payload, err := s.Svc.SetPoolProduct(body.Product)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/client-config" && r.Method == http.MethodGet:
		httputil.WriteJSON(w, http.StatusOK, s.clientConfigPayload(publicOrigin))
		return
	case path == "/direct-admin/api/client-config/generate-key" && r.Method == http.MethodPost:
		key, err := GenerateProxyAPIKey()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		envPath := config.ResolveEnvFilePath()
		values := map[string]string{
			"CODEBUDDY_PROXY_API_KEY":         key,
			"CODEBUDDY_PROXY_REQUIRE_API_KEY": "true",
		}
		if err := config.UpsertEnvFile(envPath, values); err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{
				"ok":    false,
				"error": "API Key 已生成但写入 .env 失败: " + err.Error(),
			})
			return
		}
		s.Svc.SetAPIKey(key)
		payload := s.clientConfigPayload(publicOrigin)
		payload["generated"] = true
		payload["envFile"] = envPath
		payload["note"] = "新 API Key 已写入 " + envPath + "，重启后仍然有效。请同步更新 ZCode / NewAPI 等客户端里的 Key。"
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/codebuddy/status" && r.Method == http.MethodGet:
		store, err := s.Svc.Pool.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, accounts.SummarizeStore(store, s.Svc.Pool.Path()))
		return
	case path == "/direct-admin/api/codebuddy/oauth/session" && r.Method == http.MethodGet:
		httputil.WriteJSON(w, http.StatusOK, s.Svc.OAuthSession(publicOrigin))
		return
	case path == "/direct-admin/api/codebuddy/oauth/start" && r.Method == http.MethodPost:
		var body struct {
			Site          string `json:"site"`
			Label         string `json:"label"`
			ReuseExisting bool   `json:"reuseExisting"`
		}
		_ = httputil.ReadJSON(r, &body)
		payload, err := s.Svc.StartOAuth(r.Context(), body.Site, body.Label, publicOrigin, body.ReuseExisting)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/codebuddy/oauth/poll" && r.Method == http.MethodPost:
		payload, err := s.Svc.PollOAuth(r.Context(), publicOrigin)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/codebuddy/oauth/callback" && r.Method == http.MethodPost:
		payload, err := s.Svc.PollOAuth(r.Context(), publicOrigin)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/codebuddy/accounts" && r.Method == http.MethodGet:
		store, err := s.Svc.Pool.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, accounts.SummarizeStore(store, s.Svc.Pool.Path()))
		return
	case path == "/direct-admin/api/codebuddy/models" && r.Method == http.MethodGet:
		fresh := true
		if q := r.URL.Query().Get("fresh"); q != "" {
			fresh = queryTruthy(q)
		}
		listed, err := s.Svc.ListModels(r.Context(), fresh)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, listed)
		return
	case path == "/direct-admin/api/codebuddy/probe" && r.Method == http.MethodPost:
		listed, err := s.Svc.ListModels(r.Context(), true)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, listed)
		return
	// Batch check-in for enabled accounts. Defaults to the active pool site so the
	// button follows CODEBUDDY_SITE; optional body {"site":"domestic"} overrides it.
	case path == "/direct-admin/api/codebuddy/checkin" && r.Method == http.MethodPost:
		store, err := s.Svc.Pool.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		var body struct {
			Site string `json:"site"`
		}
		_ = httputil.ReadJSON(r, &body)
		poolSite := config.NormalizeSite(strutil.First(body.Site, s.Svc.ActivePoolSite()))
		result := billing.RunPoolCheckin(r.Context(), s.Svc.Provider, store, s.Svc.Config(), poolSite)
		httputil.WriteJSON(w, http.StatusOK, result)
		return
	}

	if strings.HasPrefix(path, "/direct-admin/api/codebuddy/accounts/") {
		s.handleAccountAction(w, r, path)
		return
	}
	httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "unknown admin api"})
}

func (s *Server) handleAccountAction(w http.ResponseWriter, r *http.Request, path string) {
	rest := strings.TrimPrefix(path, "/direct-admin/api/codebuddy/accounts/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing account id"})
		return
	}
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case r.Method == http.MethodDelete && action == "":
		store, err := s.Svc.Pool.Delete(id)
		if err != nil {
			httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, accounts.SummarizeStore(store, s.Svc.Pool.Path()))
		return
	case r.Method == http.MethodPost && (action == "enable" || action == "disable"):
		account, store, err := s.Svc.Pool.SetEnabled(id, action == "enable")
		if err != nil {
			httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"account":  accounts.SummarizeAccount(account),
			"accounts": accounts.SummarizeStore(store, s.Svc.Pool.Path()),
		})
		return
	case r.Method == http.MethodGet && action == "usage":
		store, err := s.Svc.Pool.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		var selected accounts.Account
		found := false
		for _, item := range store.Accounts {
			if item.ID == id {
				selected = item
				found = true
				break
			}
		}
		if !found {
			httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "account not found"})
			return
		}
		if !accounts.HasCredentials(selected) {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "account has no credentials"})
			return
		}
		usage, err := billing.FetchAccountUsage(r.Context(), s.Svc.Provider, selected, s.Svc.Config())
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		quotaState := billing.QuotaStateFromUsage(usage, time.Now())
		if updated, applyErr := s.Svc.Pool.ApplyQuotaState(selected.ID, quotaState.Remaining, quotaState.Unlimited, quotaState.ResetAt, quotaState.CheckedAt); applyErr == nil {
			selected = updated
		}
		payload := map[string]any{
			"ok":               usage.OK,
			"provider":         usage.Provider,
			"accountId":        usage.AccountID,
			"site":             usage.Site,
			"endpoint":         usage.Endpoint,
			"officialUsageUrl": usage.OfficialUsageURL,
			"note":             usage.Note,
			"credits":          usage.Credits,
			"notify":           usage.Notify,
			"quota":            quotaState,
			"account":          accounts.SummarizeAccount(selected),
		}
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case r.Method == http.MethodPost && action == "refresh-token":
		store, err := s.Svc.Pool.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		var selected accounts.Account
		found := false
		for _, item := range store.Accounts {
			if item.ID == id {
				selected = item
				found = true
				break
			}
		}
		if !found {
			httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "account not found"})
			return
		}
		token, err := s.Svc.OAuth.Refresh(r.Context(), oauth.RefreshOptions{
			Site:         strutil.First(selected.Site, s.Svc.Config().Site),
			BaseURL:      strutil.First(selected.BaseURL, s.Svc.Config().BaseURL),
			AccessToken:  selected.BearerToken,
			RefreshToken: selected.RefreshToken,
		})
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		updated := oauth.AccountFromTokenData(token, strutil.First(selected.Site, s.Svc.Config().Site), selected.Label)
		updated.ID = selected.ID
		updated.Enabled = selected.Enabled
		updated.BaseURL = selected.BaseURL
		updated.InternetEnvironment = selected.InternetEnvironment
		updated.APIEndpoint = selected.APIEndpoint
		updated.ChatCompletionsPath = selected.ChatCompletionsPath
		updated.Transport = selected.Transport
		updated.CreatedAt = selected.CreatedAt
		updated.SuccessRequests = selected.SuccessRequests
		updated.FailedRequests = selected.FailedRequests
		updated.LastUsedAt = selected.LastUsedAt
		updated.LastSelectedAt = selected.LastSelectedAt
		saved, store, err := s.Svc.Pool.ReplaceAccount(updated)
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"account":  accounts.SummarizeAccount(saved),
			"accounts": accounts.SummarizeStore(store, s.Svc.Pool.Path()),
		})
		return
	default:
		httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "unknown account action"})
	}
}

func (s *Server) handleOAuthLaunch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	setCookie := fmt.Sprintf("cursor_codebuddy_oauth=%s; Path=/; Max-Age=900; HttpOnly; SameSite=Lax", token)
	// 在 service 锁内鉴权；禁止对共享 *OAuthSession 产生竞态。
	if !s.Svc.OAuthLaunchAuthorized(id, token) {
		w.Header().Set("Set-Cookie", setCookie)
		httputil.WriteHTML(w, http.StatusForbidden, admin.LaunchPage("登录入口已失效或参数不正确，请回到管理台重新生成 CodeBuddy 登录入口。", false))
		return
	}
	session := s.Svc.CurrentOAuth()
	if session.URL != "" && session.Status == "waiting" {
		w.Header().Set("Set-Cookie", setCookie)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, session.URL, http.StatusFound)
		return
	}
	w.Header().Set("Set-Cookie", setCookie)
	httputil.WriteHTML(w, http.StatusConflict, admin.LaunchPage(strutil.First(session.Error, "请回到管理台重新发起 CodeBuddy OAuth 登录。"), false))
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	publicOrigin := httputil.PublicOrigin(r, s.Svc.Config().PublicBaseURL)
	setCookie := fmt.Sprintf("cursor_codebuddy_oauth=%s; Path=/; Max-Age=900; HttpOnly; SameSite=Lax", token)
	w.Header().Set("Set-Cookie", setCookie)
	if !s.Svc.OAuthLaunchAuthorized(id, token) {
		httputil.WriteHTML(w, http.StatusForbidden, admin.LaunchPage("登录回调无效，请回到管理台重试。", false))
		return
	}
	payload, err := s.Svc.PollOAuth(r.Context(), publicOrigin)
	if err != nil {
		httputil.WriteHTML(w, http.StatusBadGateway, admin.LaunchPage("CodeBuddy 登录回调失败："+err.Error(), false))
		return
	}
	ok, _ := payload["ok"].(bool)
	// poll 后重新读取值快照。
	liveAfter := s.Svc.CurrentOAuth()
	msg := "CodeBuddy 登录已确认，账号已导入账号池。"
	if !ok {
		msg = strutil.First(liveAfter.Error, "CodeBuddy 登录尚未完成。")
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusConflict
	}
	httputil.WriteHTML(w, status, admin.LaunchPage(msg, ok))
}

func (s *Server) clientConfigPayload(publicOrigin string) map[string]any {
	cfg := s.Svc.Config()
	apiBase := strings.TrimRight(publicOrigin, "/") + "/v1"
	key := strings.TrimSpace(cfg.APIKey)
	return map[string]any{
		"ok":                 true,
		"baseUrl":            apiBase,
		"apiBase":            apiBase,
		"apiBasePath":        "/v1",
		"chatCompletionsUrl": apiBase + "/chat/completions",
		"recommendedModel":   "auto",
		"requireApiKey":      cfg.RequireAPIKey,
		"apiKeyConfigured":   key != "",
		"apiKeyPreview":      strutil.MaskSecret(key, 6),
		"apiKey":             key,
		"transport":          cfg.Transport,
		"site":               s.Svc.ActivePoolSite(),
		"poolSite":           s.Svc.ActivePoolSite(),
		"product":            s.Svc.ActiveProduct(),
		"poolProduct":        s.Svc.ActiveProduct(),
	}
}

// GenerateProxyAPIKey 生成网关 API Key（cbp_... 前缀）。
func GenerateProxyAPIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cbp_" + hex.EncodeToString(buf), nil
}

func estimatePromptChars(messages []map[string]any) int {
	total := 0
	for _, message := range messages {
		switch v := message["content"].(type) {
		case string:
			total += len(v)
		default:
			raw, _ := json.Marshal(v)
			total += len(raw)
		}
	}
	return total
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

func queryTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
