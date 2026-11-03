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
)

const (
	searchUpstreamTimeout         = 45 * time.Second
	webFetchUpstreamTimeout       = 90 * time.Second
	agenticSearchUpstreamTimeout  = 5 * time.Minute
	responsesSearchOverallTimeout = 5 * time.Minute
	maxSearchResults              = 50
)

type searchRequestBody struct {
	Model          string   `json:"model"`
	Query          string   `json:"query"`
	MaxResults     int      `json:"max_results"`
	AllowedDomains []string `json:"allowed_domains"`
	BlockedDomains []string `json:"blocked_domains"`
	Freshness      string   `json:"freshness"`
}

type webFetchRequestBody struct {
	Model          string `json:"model"`
	URL            string `json:"url"`
	Prompt         string `json:"prompt"`
	Format         string `json:"format"`
	TimeoutSeconds int    `json:"timeout"`
	MaxLength      int    `json:"max_length"`
}

type agenticSearchRequestBody struct {
	Model  string `json:"model"`
	Query  string `json:"query"`
	Stream bool   `json:"stream"`
}

func (s *Server) handleSearchAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleSearch(w, r, keySite)
}

func (s *Server) handleWebFetchAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleWebFetch(w, r, keySite)
}

func (s *Server) handleAgenticSearchAuth(w http.ResponseWriter, r *http.Request) {
	ok, keySite := s.authorizeAPI(w, r)
	if !ok {
		return
	}
	s.handleAgenticSearch(w, r, keySite)
}

// handleSearch 转发上游 /agenttool/v1/search（关键词搜索）。
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, keySite string) {
	var body searchRequestBody
	if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
		s.writeSearchBodyError(w, err)
		return
	}
	query := strings.TrimSpace(body.Query)
	if query == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("query is required", "invalid_request_error"))
		return
	}
	if body.MaxResults < 0 || body.MaxResults > maxSearchResults {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("max_results must be between 1 and 50", "invalid_request_error"))
		return
	}
	providerModel := gateway.ResolveProviderModel(body.Model)
	site := resolveRequestSite(providerModel.Site, r.Header.Get("X-Site"), keySite)
	if !boundSiteAllowed(w, r, site) {
		return
	}
	started := time.Now()
	proxyRequestID := s.newProxyRequestID()
	finish := s.Svc.BeginRequest("search", len(query), false)
	ctx, cancel := context.WithTimeout(r.Context(), searchUpstreamTimeout)
	defer cancel()

	result, meta, err := s.Svc.WebSearchFromPool(ctx, gateway.SearchOptions{
		AccountID:    requestBinding(r).AccountID,
		FixedAccount: requestBinding(r).AccountID != "",
		Site:         site,
	}, provider.WebSearchRequest{
		Query:          query,
		MaxResults:     body.MaxResults,
		AllowedDomains: body.AllowedDomains,
		BlockedDomains: body.BlockedDomains,
		Freshness:      body.Freshness,
	})
	if err != nil {
		s.finishSearchRequest(started, proxyRequestID, "search", query, false, err, gateway.SearchResult{})
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		s.writeChatError(w, http.StatusBadGateway, err)
		return
	}
	s.finishSearchRequest(started, proxyRequestID, "search", query, true, nil, meta)
	finish(true, 0, 0, 0, 0, "", provider.Usage{})
	payload := map[string]any{
		"object":           result.Object,
		"query":            result.Query,
		"search_type":      result.SearchType,
		"provider":         result.Provider,
		"results":          result.Results,
		"total_results":    result.TotalResults,
		"response_time_ms": result.ResponseTimeMs,
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, http.StatusOK, payload)
}

// handleWebFetch 转发上游 /agenttool/v1/webfetch（网页正文抓取）。
func (s *Server) handleWebFetch(w http.ResponseWriter, r *http.Request, keySite string) {
	var body webFetchRequestBody
	if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
		s.writeSearchBodyError(w, err)
		return
	}
	target := strings.TrimSpace(body.URL)
	if target == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("url is required", "invalid_request_error"))
		return
	}
	lower := strings.ToLower(target)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("url must start with http:// or https://", "invalid_request_error"))
		return
	}
	format := strings.ToLower(strings.TrimSpace(body.Format))
	switch format {
	case "", "markdown", "text":
	default:
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("format must be markdown or text", "invalid_request_error"))
		return
	}
	providerModel := gateway.ResolveProviderModel(body.Model)
	site := resolveRequestSite(providerModel.Site, r.Header.Get("X-Site"), keySite)
	if !boundSiteAllowed(w, r, site) {
		return
	}
	started := time.Now()
	proxyRequestID := s.newProxyRequestID()
	finish := s.Svc.BeginRequest("webfetch", len(target), false)
	ctx, cancel := context.WithTimeout(r.Context(), webFetchUpstreamTimeout)
	defer cancel()

	result, meta, err := s.Svc.WebFetchFromPool(ctx, gateway.SearchOptions{
		AccountID:    requestBinding(r).AccountID,
		FixedAccount: requestBinding(r).AccountID != "",
		Site:         site,
	}, provider.WebFetchRequest{
		URL:            target,
		Prompt:         body.Prompt,
		Format:         body.Format,
		TimeoutSeconds: body.TimeoutSeconds,
		MaxLength:      body.MaxLength,
	})
	if err != nil {
		s.finishSearchRequest(started, proxyRequestID, "webfetch", target, false, err, gateway.SearchResult{})
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		s.writeChatError(w, http.StatusBadGateway, err)
		return
	}
	s.finishSearchRequest(started, proxyRequestID, "webfetch", target, true, nil, meta)
	finish(true, len(result.Content), 0, 0, 0, "", provider.Usage{})
	payload := map[string]any{
		"object":         result.Object,
		"url":            result.URL,
		"title":          result.Title,
		"content":        result.Content,
		"content_type":   result.ContentType,
		"content_length": result.ContentLength,
		"truncated":      result.Truncated,
		"status_code":    result.StatusCode,
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, http.StatusOK, payload)
}

// handleAgenticSearch 转发上游 /agenttool/v1/agentic_search。
// 上游本身是 SSE；stream=false 时聚合成 JSON，stream=true 时逐事件回放。
func (s *Server) handleAgenticSearch(w http.ResponseWriter, r *http.Request, keySite string) {
	var body agenticSearchRequestBody
	if err := httputil.ReadJSONLimited(r, &body, s.maxJSONBodyBytes); err != nil {
		s.writeSearchBodyError(w, err)
		return
	}
	query := strings.TrimSpace(body.Query)
	if query == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("query is required", "invalid_request_error"))
		return
	}
	providerModel := gateway.ResolveProviderModel(body.Model)
	site := resolveRequestSite(providerModel.Site, r.Header.Get("X-Site"), keySite)
	if !boundSiteAllowed(w, r, site) {
		return
	}
	started := time.Now()
	proxyRequestID := s.newProxyRequestID()
	finish := s.Svc.BeginRequest("agentic_search", len(query), body.Stream)
	ctx, cancel := context.WithTimeout(r.Context(), agenticSearchUpstreamTimeout)
	defer cancel()

	if body.Stream {
		s.streamAgenticSearch(w, r, ctx, query, site, proxyRequestID, started, finish)
		return
	}
	result, err := s.Svc.AgenticSearchFromPool(ctx, gateway.AgenticSearchOptions{
		SearchOptions: gateway.SearchOptions{
			AccountID:    requestBinding(r).AccountID,
			FixedAccount: requestBinding(r).AccountID != "",
			Site:         site,
		},
		Query: query,
	})
	if err != nil {
		s.finishSearchRequest(started, proxyRequestID, "agentic_search", query, false, err, gateway.SearchResult{})
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		s.writeChatError(w, http.StatusBadGateway, err)
		return
	}
	s.finishSearchRequest(started, proxyRequestID, "agentic_search", query, true, nil, result.SearchResult)
	finish(true, len(result.Content), 0, 0, 0, "", provider.Usage{})
	w.Header().Set("Access-Control-Allow-Origin", "*")
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"object":  "agentic_search.result",
		"query":   query,
		"content": result.Content,
		"events":  result.Events,
	})
}

// streamAgenticSearch 把上游 SSE 进度与最终答案透传给下游。
func (s *Server) streamAgenticSearch(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	query, site, proxyRequestID string,
	started time.Time,
	finish func(bool, int, int64, int64, int64, string, provider.Usage),
) {
	sse, ok := httputil.NewSSEStream(w, 16<<10)
	if !ok {
		finish(false, 0, 0, 0, 0, "streaming unsupported", provider.Usage{})
		httputil.WriteJSON(w, http.StatusInternalServerError, openai.NewError("streaming unsupported", "internal_error"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	write := func(fn func() error) {
		mu.Lock()
		defer mu.Unlock()
		if err := fn(); err != nil {
			cancel()
		}
	}
	write(func() error { return sse.WriteComment("connected") })
	interval := s.Svc.Config().StreamKeepAlive
	if interval <= 0 {
		interval = 5 * time.Second
	}
	stop := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				write(func() error { return sse.WriteComment("keep-alive") })
			}
		}
	})
	defer func() {
		close(stop)
		workers.Wait()
	}()

	emit := func(event provider.AgenticSearchEvent) {
		// done 事件携带最终全文，统一在聚合完成后用 synthesis 事件输出一次，
		// 避免下游拿到两份完整答案。
		if event.Type == "done" {
			return
		}
		write(func() error { return sse.WriteNamedEvent(event.Type, event) })
	}
	result, err := s.Svc.AgenticSearchFromPool(ctx, gateway.AgenticSearchOptions{
		SearchOptions: gateway.SearchOptions{
			AccountID:    requestBinding(r).AccountID,
			FixedAccount: requestBinding(r).AccountID != "",
			Site:         site,
		},
		Query:   query,
		OnEvent: emit,
	})
	if err != nil {
		if openai.IsClientCanceled(err) {
			s.finishSearchRequest(started, proxyRequestID, "agentic_search", query, true, nil, gateway.SearchResult{})
			finish(true, 0, 0, 0, 0, "", provider.Usage{})
			return
		}
		s.finishSearchRequest(started, proxyRequestID, "agentic_search", query, false, err, gateway.SearchResult{})
		finish(false, 0, 0, 0, 0, err.Error(), provider.Usage{})
		typ, _ := openai.ClassifyUpstream(err)
		write(func() error {
			return sse.WriteNamedEvent("error", map[string]any{"type": "error", "code": typ, "message": err.Error()})
		})
		write(sse.WriteDone)
		return
	}
	s.finishSearchRequest(started, proxyRequestID, "agentic_search", query, true, nil, result.SearchResult)
	finish(true, len(result.Content), 0, 0, 0, "", provider.Usage{})
	if result.Content != "" {
		write(func() error {
			return sse.WriteNamedEvent("synthesis", map[string]any{"type": "synthesis", "content": result.Content})
		})
	}
	write(sse.WriteDone)
}

func (s *Server) finishSearchRequest(
	started time.Time,
	proxyRequestID, model, label string,
	ok bool,
	err error,
	meta gateway.SearchResult,
) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	var result *gateway.CompleteResult
	if meta.AccountID != "" {
		result = &gateway.CompleteResult{Account: meta.Account, AccountID: meta.AccountID}
	}
	s.recordChatJournal(started, proxyRequestID, gateway.CompleteOptions{
		Model:        model,
		SessionLabel: label,
	}, model, false, ok, errMsg, provider.Usage{}, result)
}

func (s *Server) writeSearchBodyError(w http.ResponseWriter, err error) {
	if errors.Is(err, httputil.ErrBodyTooLarge) {
		httputil.WriteJSON(w, http.StatusRequestEntityTooLarge, openai.NewError(s.bodyTooLargeMessage(), "invalid_request_error"))
		return
	}
	httputil.WriteJSON(w, http.StatusBadRequest, openai.NewError("Invalid JSON body", "invalid_request_error"))
}
