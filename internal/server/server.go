package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accountimport"
	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/admin"
	"github.com/wnddd839/codebuddy-proxy/internal/billing"
	"github.com/wnddd839/codebuddy-proxy/internal/checkin"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/httputil"
	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
	modelspkg "github.com/wnddd839/codebuddy-proxy/internal/models"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
	"github.com/wnddd839/codebuddy-proxy/internal/openai"
	"github.com/wnddd839/codebuddy-proxy/internal/opencode"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/proxies"
	"github.com/wnddd839/codebuddy-proxy/internal/sessionpin"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
	"github.com/wnddd839/codebuddy-proxy/internal/update"
	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
	"github.com/wnddd839/codebuddy-proxy/internal/version"
)

type Server struct {
	Svc     *gateway.Service
	HTTP    *http.Server
	Updater *update.Manager
	// Proxies 是候选出口清单（proxies.json），账号绑定仍以 Account.Proxy 为准。
	Proxies *proxies.File
	// drainTimeout 是优雅关闭宽限期，0 时由 DrainTimeout 回退默认值。
	drainTimeout time.Duration
	// Checkin 是每账号自动签到规则与调度器（proxy-checkin.json）。
	Checkin *checkin.Runner
	// ModelRoutes 是跨区模型路由表（proxy-model-routes.json）。
	ModelRoutes      *modelroute.File
	maxJSONBodyBytes int64
}

func New(cfg config.Config, svc *gateway.Service) *Server {
	s := &Server{
		Svc:     svc,
		Updater: update.New(version.Version),
		Proxies: proxies.Open(filepath.Join(filepath.Dir(cfg.AccountsPath), "proxies.json")),
	}
	if cfg.ShutdownGrace > 0 {
		s.drainTimeout = cfg.ShutdownGrace
	}
	s.ModelRoutes = modelroute.Open(filepath.Join(filepath.Dir(cfg.AccountsPath), "proxy-model-routes.json"))
	svc.ModelRoutes = s.ModelRoutes
	// 积分阈值：选号时低于该值的账号直接跳过。
	svc.Pool.SetQuotaFloor(cfg.QuotaFloorCredits)
	s.Checkin = checkin.NewRunner(
		checkin.Open(filepath.Join(filepath.Dir(cfg.AccountsPath), "proxy-checkin.json")),
		checkin.OpenLog(filepath.Join(filepath.Dir(cfg.AccountsPath), "proxy-checkin-log.json")),
		svc.Pool, svc.Provider, svc.Log, svc.Config,
	)
	maxBodyMB := cfg.MaxBodyMB
	if maxBodyMB <= 0 || int64(maxBodyMB) > (1<<63-1)>>20 {
		maxBodyMB = config.DefaultMaxBodyMB
	}
	s.maxJSONBodyBytes = int64(maxBodyMB) << 20
	idleTimeout := cfg.ServerIdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = config.DefaultServerIdleTimeout
	}
	mux := http.NewServeMux()
	// Go 1.22+ 方法感知路由（http_servemux_patterns）
	// 避免与方法相关的尾斜杠子树与更深层精确路由冲突。
	mux.HandleFunc("OPTIONS /{$}", s.handleOptions)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("HEAD /health", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("HEAD /readyz", s.handleReadyz)
	mux.HandleFunc("GET /v1/models", s.handleModelsAuth)
	mux.HandleFunc("GET /models", s.handleModelsAuth)
	mux.HandleFunc("GET /v1/model/info", s.handleModelInfoAuth)
	mux.HandleFunc("GET /model/info", s.handleModelInfoAuth)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatAuth)
	mux.HandleFunc("POST /chat/completions", s.handleChatAuth)
	mux.HandleFunc("POST /v1/responses", s.handleResponsesAuth)
	mux.HandleFunc("POST /responses", s.handleResponsesAuth)
	mux.HandleFunc("POST /v1/images/generations", s.handleImagesAuth)
	mux.HandleFunc("POST /images/generations", s.handleImagesAuth)
	mux.HandleFunc("POST /v1/images/edits", s.handleImageEditsAuth)
	mux.HandleFunc("POST /images/edits", s.handleImageEditsAuth)
	mux.HandleFunc("POST /v1/search", s.handleSearchAuth)
	mux.HandleFunc("POST /search", s.handleSearchAuth)
	mux.HandleFunc("POST /v1/webfetch", s.handleWebFetchAuth)
	mux.HandleFunc("POST /webfetch", s.handleWebFetchAuth)
	mux.HandleFunc("POST /v1/agentic_search", s.handleAgenticSearchAuth)
	mux.HandleFunc("POST /agentic_search", s.handleAgenticSearchAuth)
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
		IdleTimeout:       idleTimeout,
	}
	return s
}

// DrainTimeout 返回优雅关闭宽限期：必须大于一个 agent 回合的常见时长，
// 否则每次部署都会硬切在途请求。默认 10 分钟，可用
// CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS 覆盖。
func (s *Server) DrainTimeout() time.Duration {
	if s != nil && s.drainTimeout > 0 {
		return s.drainTimeout
	}
	return config.DefaultShutdownGrace
}

func (s *Server) ListenAndServe() error {
	listener, err := net.Listen("tcp", s.HTTP.Addr)
	if err != nil {
		return err
	}
	if err := update.ReportReady(); err != nil {
		listener.Close()
		return err
	}
	return s.HTTP.Serve(listener)
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
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Site")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	deep := queryTruthy(r.URL.Query().Get("deep"))
	payload := map[string]any{
		"ok":        true,
		"provider":  "codebuddy",
		"transport": s.Svc.Config().Transport,
		"version":   version.Version,
	}
	if deep {
		upstream := s.Svc.ProbeUpstream(r.Context(), false)
		payload["upstream"] = upstream
		if !upstream.OK {
			payload["ok"] = false
			httputil.WriteJSON(w, http.StatusServiceUnavailable, payload)
			return
		}
	} else {
		payload["upstream"] = s.Svc.Status()["upstream"]
	}
	// watch 只作观测暴露，不改变 HTTP 状态码：真正的告警走 watch 自身的通知通道，
	// 避免外部探针因为“上游变慢”而把整个网关判死。
	if st := s.Svc.WatchStatus(); st.Enabled {
		payload["watch"] = st
	}
	httputil.WriteJSON(w, http.StatusOK, payload)
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	upstream := s.Svc.ProbeUpstream(r.Context(), false)
	payload := map[string]any{
		"ok":        upstream.OK,
		"provider":  "codebuddy",
		"transport": s.Svc.Config().Transport,
		"version":   version.Version,
		"upstream":  upstream,
	}
	status := http.StatusOK
	if !upstream.OK {
		status = http.StatusServiceUnavailable
	}
	httputil.WriteJSON(w, status, payload)
}

func (s *Server) handleModelsAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleModels(w, r, keySite)
}

func (s *Server) handleChatAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleChatCompletions(w, r, keySite)
}

// Responses API 的实现见 responses.go（handleResponsesAuth / handleResponses / streamResponses）。

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

func (s *Server) authorizeAPI(w http.ResponseWriter, r *http.Request) (bool, string) {
	cfg := s.Svc.Config()
	if !cfg.RequireAPIKey {
		return true, ""
	}
	token := httputil.BearerToken(r)
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	ok, binding := cfg.LookupAPIKeyBinding(token)
	if !ok {
		httputil.WriteJSON(w, http.StatusUnauthorized, openai.NewError("Missing or invalid API key", "authentication_error"))
		return false, ""
	}
	if binding.AccountID != "" {
		var err error
		binding, err = s.bindAccount(r, binding)
		if err != nil {
			httputil.WriteJSON(w, http.StatusServiceUnavailable, openai.NewError(err.Error(), "account_unavailable"))
			return false, ""
		}
	}
	return true, binding.Site
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
	// Only the admin password grants console access. The gateway API key is
	// distributed to every client; accepting it here would let any client
	// read /direct-admin/api/client-config (which returns every key in
	// plaintext) and mutate the account pool.
	if token != "" && secretEqual(token, cfg.AdminPassword) {
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
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleModelInfo(w, r, keySite)
}

func (s *Server) handleModelInfo(w http.ResponseWriter, r *http.Request, keySite string) {
	if !boundSiteAllowed(w, r, resolveRequestSite("", r.Header.Get("X-Site"), keySite)) {
		return
	}
	fresh := queryTruthy(r.URL.Query().Get("fresh"))
	listed, err := s.Svc.ListModelsForSite(r.Context(), resolveRequestSite("", r.Header.Get("X-Site"), keySite), fresh)
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

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request, keySite string) {
	if !boundSiteAllowed(w, r, resolveRequestSite("", r.Header.Get("X-Site"), keySite)) {
		return
	}
	fresh := queryTruthy(r.URL.Query().Get("fresh"))
	listed, err := s.Svc.ListModelsForSite(r.Context(), resolveRequestSite("", r.Header.Get("X-Site"), keySite), fresh)
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
		if len(model.Tags) > 0 {
			item["tags"] = model.Tags
		}
		if modelspkg.IsImageModel(model.Tags) {
			item["mode"] = "image_generation"
		}
		for key, value := range opencode.ModelListFields(model) {
			item[key] = value
		}
		data = append(data, item)
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func resolveRequestSite(modelSite, headerSite, keySite string) string {
	if site := config.OptionalSite(modelSite); site != "" {
		return site
	}
	if site := config.OptionalSite(headerSite); site != "" {
		return site
	}
	return config.OptionalSite(keySite)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request, keySite string) {
	var body struct {
		Model               string           `json:"model"`
		Messages            []map[string]any `json:"messages"`
		Stream              bool             `json:"stream"`
		StreamOptions       *streamOptions   `json:"stream_options"`
		Tools               any              `json:"tools"`
		ToolChoice          any              `json:"tool_choice"`
		WebSearchOptions    *struct{}        `json:"web_search_options"`
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
	if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
		if errors.Is(err, httputil.ErrBodyTooLarge) {
			httputil.WriteJSON(w, http.StatusRequestEntityTooLarge, openai.NewError(s.bodyTooLargeMessage(), "invalid_request_error"))
			return
		}
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("Invalid JSON body", "invalid_request_error"))
		return
	}
	tools, searchRequested, err := openai.ChatSearchTools(body.Tools, body.WebSearchOptions != nil, body.ToolChoice)
	if err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError(err.Error(), "invalid_request_error"))
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
	site := resolveRequestSite(providerModel.Site, r.Header.Get("X-Site"), keySite)
	// 只有模型名的 cn:/global: 前缀算「客户端硬钉站点」——前缀是最高优先级的
	// 单次指定，路由绝不覆盖它。X-Site 头不算：它只是选「从哪个区的号池进来」，
	// 规则本来就按源站点匹配，若把 X-Site 也当硬钉，国内来源的规则就永远触发不了。
	siteExplicit := config.OptionalSite(providerModel.Site) != ""
	if !boundSiteAllowed(w, r, site) {
		finish(false, 0, 0, 0, 0, "请求区域与绑定账号不一致", provider.Usage{})
		return
	}
	completeOpts := gateway.CompleteOptions{
		AccountID:           requestBinding(r).AccountID,
		FixedAccount:        requestBinding(r).AccountID != "",
		Model:               providerModel.Model,
		Messages:            body.Messages,
		Tools:               tools,
		ToolChoice:          body.ToolChoice,
		Temperature:         body.Temperature,
		TopP:                body.TopP,
		MaxCompletionTokens: maxTokens,
		ReasoningEffort:     reasoningEffort,
		Reasoning:           body.Reasoning,
		Thinking:            body.Thinking,
		SessionKey:          sessionpin.Key(r.Header, body.PromptCacheKey, body.Messages),
		SessionLabel:        sessionpin.SessionLabel(body.Messages),
		Site:                site,
		SiteExplicit:        siteExplicit,
	}

	if body.Stream {
		s.streamChat(w, r, providerModel, completeOpts, chatStarted, proxyRequestID, resolveIncludeUsage(body.StreamOptions), searchRequested, finish)
		return
	}

	completeOpts.Stream = false
	result, err := s.completeWithRequestedSearch(r.Context(), completeOpts, searchRequested)
	if err != nil {
		if openai.IsClientCanceled(err) {
			// 客户端在非流式聚合前/中主动断开，不算网关故障。
			s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, true, "", provider.Usage{}, nil)
			finish(true, 0, 0, 0, 0, "", provider.Usage{})
			return
		}
		s.recordChatJournal(chatStarted, proxyRequestID, completeOpts, providerModel.PublicModel, false, false, err.Error(), provider.Usage{}, nil)
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		s.writeChatError(w, http.StatusBadGateway, err)
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
	includeUsage bool,
	searchRequested bool,
	finish func(bool, int, int64, int64, int64, string, provider.Usage),
) {
	sse, ok := httputil.NewSSEStream(w, 16<<10)
	if !ok {
		httputil.WriteJSON(w, http.StatusInternalServerError, openai.NewError("streaming unsupported", "internal_error"))
		return
	}
	// SSE 连接是否强制 close，取决于是否经由隧道转发。
	//
	// 隧道下：服务端关闭连接的 FIN 未必能可靠送达客户端，客户端会把已失效的
	// 连接当作 keep-alive 复用，导致下一个请求挂起直到超时（opencode 默认
	// headersTimeout=60s，表现为 "Timed out waiting for response headers"）。
	// 所以隧道下必须 close，且要在 WriteHeader 之前决定。
	//
	// 直连（本机 / 局域网）没有这个问题，保持 keep-alive 可以省掉每次 SSE
	// 新建 TCP 的 60–90ms。是否走隧道由 ViaTunnel 判定（看 X-Forwarded-*
	// 与 Host），在 startStream 里设置响应头。
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
		// 隧道下强制 close（见函数开头说明），直连保持 keep-alive。
		if httputil.ViaTunnel(r) {
			r.Close = true
			w.Header().Set("Connection", "close")
		} else {
			w.Header().Set("Connection", "keep-alive")
		}
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
				ToolCalls: []openai.ToolCallDelta{{
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
	result, err := s.completeWithRequestedSearch(ctx, opts, searchRequested)
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
		cause := openai.ClassifyCause(err)
		retryAfter := openai.RetryAfter(err)
		writeLocked(func() {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			if !started {
				httputil.WriteJSON(w, http.StatusBadGateway, openai.NewErrorDetail(err.Error(), typ, code, cause, retryAfter))
				done = true
				return
			}
			errObj := map[string]any{"message": err.Error(), "type": typ, "cause": cause}
			if code != nil {
				errObj["code"] = code
			}
			if retryAfter != "" {
				errObj["retry_after"] = retryAfter
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
					ToolCalls: []openai.ToolCallDelta{{
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
		finishReason := openai.FinishReasonFromTurn(result.Turn)
		_ = sse.WriteEvent(openai.StreamChunkOf(id, providerModel.PublicModel, openai.Delta{}, &finishReason))
		// OpenAI stream_options.include_usage：空 choices + usage。
		// 显式 false 时跳过，避免严格客户端对 choices[0] 越界。
		if includeUsage {
			_ = sse.WriteEvent(openai.StreamUsageChunk(id, providerModel.PublicModel, openai.UsageFromProvider(result.Turn.Usage)))
			_ = sse.Flush() // 确保 usage 在 [DONE] 前落到客户端，避免下游提前断开丢统计
		}
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
	case strings.HasPrefix(path, "/direct-admin/api/update"):
		s.handleUpdate(w, r, path)
		return
	case path == "/direct-admin/api/status" && r.Method == http.MethodGet:
		fresh := queryTruthy(r.URL.Query().Get("fresh"))
		_ = s.Svc.ProbeUpstream(r.Context(), fresh)
		payload := s.Svc.Status()
		if fresh {
			usages := s.Svc.RefreshPoolUsage(r.Context(), "")
			payload = s.Svc.Status()
			payload["accountUsages"] = usages
			payload["creditsRefreshed"] = true
		}
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/concurrency" && r.Method == http.MethodGet:
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"concurrency": s.Svc.ConcurrencyStatus(),
		})
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
	case path == "/direct-admin/api/client-config/bound-keys" && r.Method == http.MethodPost:
		var body struct {
			Site      string `json:"site"`
			AccountID string `json:"accountId"`
		}
		if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "无效的绑定配置"})
			return
		}
		site := config.OptionalSite(body.Site)
		body.AccountID = strings.TrimSpace(body.AccountID)
		if body.AccountID != "" {
			binding, err := s.bindAccount(r, config.APIKeyBinding{AccountID: body.AccountID})
			if err != nil || (site != "" && site != binding.Site) {
				httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "绑定账号不可用或区域不一致"})
				return
			}
			site = binding.Site
		} else if site == "" {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "site 必须是 domestic 或 global"})
			return
		}
		key, err := GenerateProxyAPIKey()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cfg := s.Svc.Config()
		next := append(slices.Clone(cfg.APIKeys), config.APIKeyBinding{Key: key, Site: site, AccountID: body.AccountID})
		envPath, err := s.persistBoundAPIKeys(next)
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{
				"ok":    false,
				"error": "绑定 Key 已生成但写入 .env 失败: " + err.Error(),
			})
			return
		}
		payload := s.clientConfigPayload(publicOrigin)
		payload["generated"] = true
		payload["envFile"] = envPath
		payload["note"] = "绑定 Key 已写入 " + envPath + "，立即生效。"
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/client-config/bound-keys" && r.Method == http.MethodDelete:
		var body struct {
			Key string `json:"key"`
		}
		_ = httputil.ReadJSON(r, &body)
		target := strings.TrimSpace(body.Key)
		if target == "" {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "缺少要删除的绑定 Key"})
			return
		}
		cfg := s.Svc.Config()
		next := make([]config.APIKeyBinding, 0, len(cfg.APIKeys))
		found := false
		for _, binding := range cfg.APIKeys {
			if binding.Key == target {
				found = true
				continue
			}
			next = append(next, binding)
		}
		if !found {
			httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "绑定 Key 不存在"})
			return
		}
		envPath, err := s.persistBoundAPIKeys(next)
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{
				"ok":    false,
				"error": "删除绑定 Key 后写入 .env 失败: " + err.Error(),
			})
			return
		}
		payload := s.clientConfigPayload(publicOrigin)
		payload["envFile"] = envPath
		httputil.WriteJSON(w, http.StatusOK, payload)
		return
	case path == "/direct-admin/api/codebuddy/status" && r.Method == http.MethodGet:
		store, err := s.Svc.Pool.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, s.summarizeStore(store, s.Svc.Pool.Path()))
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
		httputil.WriteJSON(w, http.StatusOK, s.summarizeStore(store, s.Svc.Pool.Path()))
		return
	case path == "/direct-admin/api/codebuddy/models" && r.Method == http.MethodGet:
		fresh := true
		if q := r.URL.Query().Get("fresh"); q != "" {
			fresh = queryTruthy(q)
		}
		// site 缺省时沿用当前号池站点，保持老调用方行为不变。
		// 传 domestic / global 可分别拉取两站的模型编目（两站模型集不同）。
		site := config.NormalizeSite(strutil.First(r.URL.Query().Get("site"), s.Svc.ActivePoolSite()))
		listed, err := s.Svc.ListModelsForSite(r.Context(), site, fresh)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "site": site})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, listed)
		return
	case path == "/direct-admin/api/codebuddy/probe" && r.Method == http.MethodPost:
		site := config.NormalizeSite(strutil.First(r.URL.Query().Get("site"), s.Svc.ActivePoolSite()))
		listed, err := s.Svc.ListModelsForSite(r.Context(), site, true)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, listed)
		return
	// 一键导入：接受多种来源的凭据文本（CodeBuddy 客户端快照 / 账号池文件 /
	// account+auth 包装），逐条 upsert。放在 accounts/{id} 分支之前，避免
	// "import" 被当成账号 ID。
	case path == "/direct-admin/api/codebuddy/accounts/import" && r.Method == http.MethodPost:
		s.handleAccountsImport(w, r)
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
		// 手动签到也写结果日志：否则用户点完「每日签到」后徽章仍显示「未签到」，
		// 前端看不出这一轮到底成没成。
		s.recordCheckinBatch(result)
		httputil.WriteJSON(w, http.StatusOK, result)
		return
	// 跨区模型路由：列表（附当前可用模型，供前端下拉选择）。
	case path == "/direct-admin/api/model-routes" && r.Method == http.MethodGet:
		store, err := s.ModelRoutes.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		// 附带两站模型编目（缓存优先，不强制刷新），前端下拉直接用。
		catalog := map[string][]string{}
		for _, site := range []string{"domestic", "global"} {
			if listed, err := s.Svc.ListModelsForSite(r.Context(), site, false); err == nil {
				names := make([]string, 0, len(listed.Models))
				for _, m := range listed.Models {
					names = append(names, m.ModelID)
				}
				catalog[site] = names
			}
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "rules": store.Rules, "path": s.ModelRoutes.Path(), "catalog": catalog,
		})
		return
	case path == "/direct-admin/api/model-routes" && r.Method == http.MethodPost:
		var body struct {
			Rules []modelroute.Rule `json:"rules"`
		}
		if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		before, _ := s.ModelRoutes.Read()
		if err := s.ModelRoutes.Write(modelroute.Store{Rules: body.Rules}); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		// 路由表能改「请求打到哪个区、哪个模型」，属于高危配置：
		// 每次写入都留一条 WARN 级审计（默认日志级别下也会落盘），
		// 记下条数变化和每条规则的完整内容，事后可追溯。
		s.Svc.Log.Warn("model routes updated",
			"before", len(before.Rules), "after", len(body.Rules),
			"remote", r.RemoteAddr,
			"rules", modelroute.AuditLine(body.Rules))
		httputil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	// 签到结果查询：返回 48h 内的明细，供管理台渲染徽章与「48 小时 N/2」。
	case path == "/direct-admin/api/checkin-log" && r.Method == http.MethodGet:
		entries, err := s.Checkin.Log().Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "entries": entries})
		return
	// 每账号自动签到：总开关写 .env，规则写 proxy-checkin.json，保存后立即生效。
	case path == "/direct-admin/api/checkin-config" && r.Method == http.MethodGet:
		store, err := s.Checkin.Rules()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cfg := s.Svc.Config()
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "auto": s.Checkin.Auto(), "rules": store.Rules, "path": s.Checkin.Path(),
			"allowDirectFallback": cfg.CheckinAllowDirectFallback,
			"checkinRetryMin":     int(cfg.CheckinRetryInterval / time.Minute),
			"checkinRetryMax":     cfg.CheckinRetryMax,
		})
		return
	case path == "/direct-admin/api/checkin-config" && r.Method == http.MethodPost:
		var body struct {
			Auto  bool           `json:"auto"`
			Rules []checkin.Rule `json:"rules"`
			// 用指针区分「没传」与「传了 false」：只在真的传了时才写 .env，
			// 避免老前端不带这些字段就把开关静默关掉。
			AllowDirectFallback *bool `json:"allowDirectFallback"`
			// 重试参数：分钟 + 次数。0 分钟 = 不重试。
			CheckinRetryMin *int `json:"checkinRetryMin"`
			CheckinRetryMax *int `json:"checkinRetryMax"`
		}
		if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := s.Checkin.SetRules(checkin.Store{Rules: body.Rules}); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := s.Checkin.SetAuto(body.Auto); err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := s.saveBehaviorSwitches(body.AllowDirectFallback, body.CheckinRetryMin, body.CheckinRetryMax); err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "auto": body.Auto})
		return
	// 积分下限：独立端点，配独立的保存按钮。
	// 单独拆出来的原因：这是选号门槛，改错会让整池号瞬间不可用，
	// 放在签到表单里跟「保存签到」混在一起容易被误改、又不知道该不该点保存。
	case path == "/direct-admin/api/quota-floor" && r.Method == http.MethodGet:
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "quotaFloor": s.Svc.Config().QuotaFloorCredits,
		})
		return
	case path == "/direct-admin/api/quota-floor" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		var body struct {
			QuotaFloor *float64 `json:"quotaFloor"`
		}
		if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if body.QuotaFloor == nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "缺少 quotaFloor"})
			return
		}
		if *body.QuotaFloor < 0 {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "积分下限不能是负数"})
			return
		}
		cfg, err := s.setQuotaFloor(*body.QuotaFloor)
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "quotaFloor": cfg.QuotaFloorCredits,
		})
		return
	// Batch chat test for enabled accounts. Same-level path as checkin so it is not
	// swallowed by /accounts/{id}. Optional body: {"site":"...","model":"..."}.
	case path == "/direct-admin/api/codebuddy/test" && r.Method == http.MethodPost:
		var body struct {
			Site  string `json:"site"`
			Model string `json:"model"`
		}
		_ = httputil.ReadJSON(r, &body)
		poolSite := config.NormalizeSite(strutil.First(body.Site, s.Svc.ActivePoolSite()))
		model, err := s.Svc.ResolveChatTestModel(r.Context(), poolSite, body.Model)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		result := s.Svc.RunPoolChatTest(r.Context(), poolSite, model, gateway.RunPoolChatTestOptions{})
		httputil.WriteJSON(w, http.StatusOK, result)
		return
	case path == "/direct-admin/api/codebuddy/proxies" && r.Method == http.MethodGet:
		s.handleProxiesList(w, r)
		return
	case path == "/direct-admin/api/codebuddy/proxies/check" && r.Method == http.MethodPost:
		s.handleProxyCheck(w, r)
		return
	case path == "/direct-admin/api/codebuddy/proxies/candidates" && (r.Method == http.MethodPost || r.Method == http.MethodDelete):
		s.handleProxyCandidate(w, r)
		return
	case path == "/direct-admin/api/codebuddy/proxies/balance" && r.Method == http.MethodPost:
		s.handleProxyBalance(w, r)
		return
	}

	if strings.HasPrefix(path, "/direct-admin/api/codebuddy/accounts/") {
		s.handleAccountAction(w, r, path)
		return
	}
	httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "unknown admin api"})
}

// handleAccountsImport 解析并写入账号池。
//
// body: {"text":"<原始 JSON 文本>","site":"domestic|global","label":"可选前缀"}
// 返回逐条结果，便于前端展示「新增 / 更新 / 失败」。
func (s *Server) handleAccountsImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text  string `json:"text"`
		Site  string `json:"site"`
		Label string `json:"label"`
	}
	if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "text 不能为空"})
		return
	}

	parsed, err := accountimport.Parse([]byte(body.Text), accountimport.Options{
		Site:  config.OptionalSite(body.Site),
		Label: body.Label,
	})
	if err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	store, err := s.Svc.Pool.Read()
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	known := map[string]accounts.Account{}
	remember := func(item accounts.Account) {
		known[item.ID] = item
		if item.CredentialHash != "" {
			known["h:"+item.CredentialHash] = item
		}
		if item.AuthStatus.UserID != "" {
			known["u:"+item.AuthStatus.UserID+"|"+config.NormalizeSite(item.Site)] = item
		}
	}
	for _, item := range store.Accounts {
		remember(item)
	}

	type importResult struct {
		Index  int    `json:"index"`
		ID     string `json:"id,omitempty"`
		Label  string `json:"label,omitempty"`
		Site   string `json:"site,omitempty"`
		Status string `json:"status"` // added | updated | failed
		Error  string `json:"error,omitempty"`
	}
	results := make([]importResult, 0, len(parsed))
	added, updated, failed := 0, 0, 0
	var latest accounts.Store
	for _, candidate := range parsed {
		if candidate.Err != nil {
			failed++
			results = append(results, importResult{Index: candidate.Index, Status: "failed", Error: candidate.Err.Error()})
			continue
		}
		existing := known[candidate.ID]
		if existing.ID == "" && candidate.CredentialHash != "" {
			existing = known["h:"+candidate.CredentialHash]
		}
		if existing.ID == "" && candidate.AuthStatus.UserID != "" {
			existing = known["u:"+candidate.AuthStatus.UserID+"|"+config.NormalizeSite(candidate.Site)]
		}
		existed := existing.ID != ""
		var saved accounts.Account
		var snapshot accounts.Store
		var upsertErr error
		if existed {
			saved, snapshot, upsertErr = s.Svc.Pool.UpdateAccount(existing.ID, candidate.Merge)
		} else {
			saved, snapshot, upsertErr = s.Svc.Pool.Upsert(candidate.Account)
		}
		if upsertErr != nil {
			failed++
			results = append(results, importResult{Index: candidate.Index, Status: "failed", Error: upsertErr.Error()})
			continue
		}
		latest = snapshot
		status := "added"
		if existed {
			status = "updated"
			updated++
		} else {
			added++
		}
		remember(saved)
		results = append(results, importResult{
			Index:  candidate.Index,
			ID:     saved.ID,
			Label:  saved.Label,
			Site:   config.NormalizeSite(saved.Site),
			Status: status,
		})
	}

	if latest.Accounts == nil {
		latest, _ = s.Svc.Pool.Read()
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":       failed == 0,
		"added":    added,
		"updated":  updated,
		"failed":   failed,
		"results":  results,
		"accounts": s.summarizeStore(latest, s.Svc.Pool.Path()),
	})
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
		httputil.WriteJSON(w, http.StatusOK, s.summarizeStore(store, s.Svc.Pool.Path()))
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
			"accounts": s.summarizeStore(store, s.Svc.Pool.Path()),
		})
		return
	case r.Method == http.MethodPost && action == "proxy":
		var body struct {
			Proxy       string `json:"proxy"`
			CandidateID string `json:"candidateId"`
			Clear       bool   `json:"clear"`
		}
		if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		proxyURL := ""
		if !body.Clear {
			resolved, err := s.resolveProxyRef(body.Proxy, body.CandidateID)
			if err != nil {
				httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			proxyURL = resolved
		}
		account, store, err := s.Svc.Pool.SetProxy(id, proxyURL)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"account":  accounts.SummarizeAccount(account),
			"accounts": s.summarizeStore(store, s.Svc.Pool.Path()),
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
			HTTPClient:   s.Svc.RefreshHTTPFor(selected),
		})
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		saved, store, err := s.Svc.SaveRefreshedToken(selected, token)
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"account":  accounts.SummarizeAccount(saved),
			"accounts": s.summarizeStore(store, s.Svc.Pool.Path()),
		})
		return
	case r.Method == http.MethodPost && action == "test":
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
		var body struct {
			Model string `json:"model"`
		}
		_ = httputil.ReadJSON(r, &body)
		site := config.NormalizeSite(strutil.First(selected.Site, s.Svc.ActivePoolSite()))
		model, err := s.Svc.ResolveChatTestModel(r.Context(), site, body.Model)
		if err != nil {
			httputil.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		result := s.Svc.TestAccountChat(r.Context(), selected, model)
		httputil.WriteJSON(w, http.StatusOK, result)
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
	bound := make([]map[string]any, 0, len(cfg.APIKeys))
	for _, binding := range cfg.APIKeys {
		bound = append(bound, map[string]any{
			"site":       binding.Site,
			"accountId":  binding.AccountID,
			"preview":    strutil.MaskSecret(binding.Key, 6),
			"configured": true,
			"key":        binding.Key,
		})
	}
	return map[string]any{
		"ok":                 true,
		"baseUrl":            apiBase,
		"apiBase":            apiBase,
		"apiBasePath":        "/v1",
		"chatCompletionsUrl": apiBase + "/chat/completions",
		"responsesUrl":       apiBase + "/responses",
		"recommendedModel":   "auto",
		"requireApiKey":      cfg.RequireAPIKey,
		"apiKeyConfigured":   key != "",
		"apiKeyPreview":      strutil.MaskSecret(key, 6),
		"apiKey":             key,
		"apiKeys":            bound,
		"transport":          cfg.Transport,
		"site":               s.Svc.ActivePoolSite(),
		"poolSite":           s.Svc.ActivePoolSite(),
		"product":            s.Svc.ActiveProduct(),
		"poolProduct":        s.Svc.ActiveProduct(),
	}
}

func (s *Server) persistBoundAPIKeys(keys []config.APIKeyBinding) (string, error) {
	envPath := config.ResolveEnvFilePath()
	if err := config.UpsertEnvFile(envPath, map[string]string{
		"CODEBUDDY_PROXY_API_KEYS": config.FormatAPIKeys(keys),
	}); err != nil {
		return envPath, err
	}
	s.Svc.SetAPIKeys(keys)
	return envPath, nil
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

type streamOptions struct {
	IncludeUsage *bool `json:"include_usage"`
}

// resolveIncludeUsage 决定是否推送空 choices 的 usage 收尾 chunk。
// 缺省 true（保留历史代理行为，下游常依赖该分片做统计）；显式 false 时跳过。
func resolveIncludeUsage(opts *streamOptions) bool {
	if opts == nil || opts.IncludeUsage == nil {
		return true
	}
	return *opts.IncludeUsage
}

func (s *Server) writeChatError(w http.ResponseWriter, status int, err error) {
	typ, code := openai.ClassifyUpstream(err)
	cause := openai.ClassifyCause(err)
	retryAfter := openai.RetryAfter(err)
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	httputil.WriteJSON(w, status, openai.NewErrorDetail(err.Error(), typ, code, cause, retryAfter))
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

// summarizeStore 是 accounts.SummarizeStore 的包装，额外注入签到状态。
// 所有对外返回账号摘要的地方都走它，避免某个端点漏掉签到字段。
func (s *Server) summarizeStore(store accounts.Store, path string) accounts.StoreSummary {
	payload := accounts.SummarizeStore(store, path)
	s.injectCheckinSummary(&payload)
	return payload
}

// injectCheckinSummary 把签到状态注入账号摘要。
//
// 单独一个方法而不是塞进 accounts.SummarizeStore：accounts 包不该依赖 checkin 包，
// 且签到日志读失败时不能让整个账号列表 500（面板还要用它排障）。
func (s *Server) injectCheckinSummary(payload *accounts.StoreSummary) {
	if payload == nil || s.Checkin == nil {
		return
	}
	logStore := s.Checkin.Log()
	if logStore == nil {
		return
	}
	now := time.Now()
	day := now.In(checkinTimezone()).Format("2006-01-02")
	for i := range payload.Accounts {
		item := &payload.Accounts[i]
		item.CheckinCount48h = logStore.Count48h(item.ID, now)
		if next, ok := s.Checkin.NextRun(item.ID, now); ok {
			item.CheckinNextAt = next.UnixMilli()
		}
		entry, found := logStore.Today(item.ID, day)
		if !found {
			item.CheckinToday = "none"
			continue
		}
		switch {
		case entry.OK && entry.Already:
			item.CheckinToday = "already"
		case entry.OK:
			item.CheckinToday = "done"
		default:
			item.CheckinToday = "failed"
			item.CheckinLastErr = entry.Message
		}
	}
}

// checkinTimezone 与 checkin 包保持同一时区口径（UTC+8 固定偏移，
// 不依赖进程本地时区——容器默认是 UTC）。
func checkinTimezone() *time.Location { return time.FixedZone("UTC+8", 8*60*60) }

// saveBehaviorSwitches 持久化签到降级与故障转移开关。
//
// 每个开关都是 *bool：nil 表示请求没带这个字段（老前端），此时不动它。
// 值真正变化时才写 .env 并更新内存配置，避免每次保存都重写文件。
func (s *Server) saveBehaviorSwitches(allowDirectFallback *bool, retryMin, retryMax *int) error {
	type binding struct {
		envKey  string
		value   *bool
		current func(config.Config) bool
	}
	cfg := s.Svc.Config()
	bindings := []binding{
		{
			envKey:  "CODEBUDDY_PROXY_CHECKIN_ALLOW_DIRECT_FALLBACK",
			value:   allowDirectFallback,
			current: func(c config.Config) bool { return c.CheckinAllowDirectFallback },
		},
	}

	values := make(map[string]string, len(bindings))
	for _, b := range bindings {
		if b.value == nil || *b.value == b.current(cfg) {
			continue
		}
		values[b.envKey] = strconv.FormatBool(*b.value)
	}
	// 重试参数：分钟 → 毫秒存 env，与其它 CODEBUDDY_PROXY_*_MS 保持一致。
	retryChanged := false
	if retryMin != nil && *retryMin >= 0 {
		values["CODEBUDDY_PROXY_CHECKIN_RETRY_INTERVAL_MS"] = strconv.Itoa(*retryMin * 60 * 1000)
		retryChanged = true
	}
	if retryMax != nil && *retryMax >= 0 {
		values["CODEBUDDY_PROXY_CHECKIN_RETRY_MAX"] = strconv.Itoa(*retryMax)
		retryChanged = true
	}
	if len(values) == 0 {
		return nil
	}
	envPath := config.ResolveEnvFilePath()
	if err := config.UpsertEnvFile(envPath, values); err != nil {
		return fmt.Errorf("persist behavior switches: %w", err)
	}
	// 内存里先算出「应用后的完整开关集合」，再一次性写回，避免多次 CAS。
	next := gateway.BehaviorSwitches{
		CheckinAllowDirectFallback: cfg.CheckinAllowDirectFallback,
		QuotaFloorCredits:          cfg.QuotaFloorCredits,
		CheckinRetryInterval:       cfg.CheckinRetryInterval,
		CheckinRetryMax:            cfg.CheckinRetryMax,
	}
	if retryMin != nil && *retryMin >= 0 {
		next.CheckinRetryInterval = time.Duration(*retryMin) * time.Minute
	}
	if retryMax != nil && *retryMax >= 0 {
		next.CheckinRetryMax = *retryMax
	}
	for _, b := range bindings {
		if b.value == nil {
			continue
		}
		switch b.envKey {
		case "CODEBUDDY_PROXY_CHECKIN_ALLOW_DIRECT_FALLBACK":
			next.CheckinAllowDirectFallback = *b.value
		}
	}
	s.Svc.SetBehaviorSwitches(next)
	for _, b := range bindings {
		if b.value == nil {
			continue
		}
		_ = os.Setenv(b.envKey, strconv.FormatBool(*b.value))
	}
	if retryChanged {
		_ = os.Setenv("CODEBUDDY_PROXY_CHECKIN_RETRY_INTERVAL_MS", values["CODEBUDDY_PROXY_CHECKIN_RETRY_INTERVAL_MS"])
		_ = os.Setenv("CODEBUDDY_PROXY_CHECKIN_RETRY_MAX", values["CODEBUDDY_PROXY_CHECKIN_RETRY_MAX"])
	}
	return nil
}

// setQuotaFloor 保存并热应用积分下限（独立于签到表单）。
// 0 = 不启用；只有 >0 才会让余额不足的号退出选号。
func (s *Server) setQuotaFloor(floor float64) (config.Config, error) {
	if floor < 0 {
		return config.Config{}, fmt.Errorf("积分下限不能是负数")
	}
	envPath := config.ResolveEnvFilePath()
	if err := config.UpsertEnvFile(envPath, map[string]string{
		"CODEBUDDY_PROXY_QUOTA_FLOOR_CREDITS": strconv.FormatFloat(floor, 'f', -1, 64),
	}); err != nil {
		return config.Config{}, fmt.Errorf("persist quota floor: %w", err)
	}
	_ = os.Setenv("CODEBUDDY_PROXY_QUOTA_FLOOR_CREDITS", strconv.FormatFloat(floor, 'f', -1, 64))
	cfg := s.Svc.Config()
	next := gateway.BehaviorSwitches{
		CheckinAllowDirectFallback: cfg.CheckinAllowDirectFallback,
		QuotaFloorCredits:          floor,
		CheckinRetryInterval:       cfg.CheckinRetryInterval,
		CheckinRetryMax:            cfg.CheckinRetryMax,
	}
	return s.Svc.SetBehaviorSwitches(next), nil
}

// recordCheckinBatch 把一轮手动签到的结果逐条写进签到日志，
// 与自动调度的落盘格式保持一致（同一份 Entry 结构、同一套 48h 保留策略）。
func (s *Server) recordCheckinBatch(batch billing.CheckinBatchResult) {
	if s.Checkin == nil {
		return
	}
	logStore := s.Checkin.Log()
	if logStore == nil || len(batch.Results) == 0 {
		return
	}
	now := time.Now()
	day := now.In(checkinTimezone()).Format("2006-01-02")
	for _, item := range batch.Results {
		if item.AccountID == "" {
			continue
		}
		entry := checkin.Entry{
			AccountID: item.AccountID,
			Day:       day,
			At:        now.UnixMilli(),
			OK:        item.OK,
			Already:   item.AlreadyCheckedIn,
			Credits:   item.RewardCredits,
			Message:   item.Message,
			Egress:    s.accountEgress(item.AccountID),
		}
		if err := logStore.Append(entry); err != nil {
			s.Svc.Log.Warn("checkin: 手动签到结果落盘失败", "accountId", item.AccountID, "error", err.Error())
		}
	}
}

// accountEgress 查账号当前绑定的出口，仅用于日志留痕（查不到就留空）。
func (s *Server) accountEgress(accountID string) string {
	if s.Svc == nil || s.Svc.Pool == nil {
		return ""
	}
	store, err := s.Svc.Pool.Read()
	if err != nil {
		return ""
	}
	for _, item := range store.Accounts {
		if item.ID == accountID {
			return item.Proxy
		}
	}
	return ""
}

func (s *Server) bodyTooLargeMessage() string {
	return fmt.Sprintf("Request body exceeds %dMB.", s.maxJSONBodyBytes>>20)
}
