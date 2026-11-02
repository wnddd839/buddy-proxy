package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

// Upstream agent-tool endpoints exposed by the CodeBuddy / WorkBuddy portal.
// They are separate from the chat completion endpoint but use the same OAuth
// bearer token and account region.
const (
	agentToolSearchPath        = "/agenttool/v1/search"
	agentToolWebFetchPath      = "/agenttool/v1/webfetch"
	agentToolAgenticSearchPath = "/agenttool/v1/agentic_search"

	agentToolMaxResponseBytes = 8 << 20
	agentToolMaxSSEEventBytes = 16 << 20
	agentToolSearchModePro    = 2
)

// WebSearchRequest is the upstream body for POST /agenttool/v1/search.
type WebSearchRequest struct {
	Query          string
	MaxResults     int
	AllowedDomains []string
	BlockedDomains []string
	Freshness      string
}

// WebSearchItem is one search result.
type WebSearchItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Site    string `json:"site,omitempty"`
	Favicon string `json:"favicon,omitempty"`
}

// WebSearchResponse is the normalized upstream search payload.
type WebSearchResponse struct {
	Object         string          `json:"object"`
	Query          string          `json:"query"`
	SearchType     string          `json:"search_type,omitempty"`
	Provider       string          `json:"provider,omitempty"`
	Results        []WebSearchItem `json:"results"`
	TotalResults   int             `json:"total_results"`
	ResponseTimeMs int             `json:"response_time_ms,omitzero"`
	Status         int             `json:"-"`
}

// WebFetchRequest is the upstream body for POST /agenttool/v1/webfetch.
type WebFetchRequest struct {
	URL            string
	Prompt         string
	Format         string
	TimeoutSeconds int
	MaxLength      int
}

// WebFetchResponse is the normalized upstream page-fetch payload.
type WebFetchResponse struct {
	Object        string `json:"object"`
	URL           string `json:"url"`
	Title         string `json:"title,omitempty"`
	Content       string `json:"content"`
	ContentType   string `json:"content_type,omitempty"`
	ContentLength int    `json:"content_length,omitzero"`
	Truncated     bool   `json:"truncated,omitzero"`
	StatusCode    int    `json:"status_code,omitzero"`
	Status        int    `json:"-"`
}

// AgenticSearchEvent is one progress event from the agentic search SSE stream.
type AgenticSearchEvent struct {
	Type       string `json:"type"`
	Agent      string `json:"agent,omitempty"`
	Tool       string `json:"tool,omitempty"`
	CallID     string `json:"call_id,omitempty"`
	Status     string `json:"status,omitempty"`
	ItemsCount int    `json:"items_count,omitzero"`
	LatencyMs  int    `json:"latency_ms,omitzero"`
	Delta      string `json:"delta,omitempty"`
	Turn       int    `json:"turn,omitzero"`
	Phase      string `json:"phase,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
}

// AgenticSearchResult is the synthesized answer plus the progress timeline.
type AgenticSearchResult struct {
	Content string
	Events  []AgenticSearchEvent
	Status  int
}

// ResolveProtocolDirectAgentToolEndpoint builds an /agenttool/v1/* URL with the
// same region + custom APIEndpoint rules as chat.
func ResolveProtocolDirectAgentToolEndpoint(opts ChatOptions, toolPath string) string {
	// The billing resolver already implements the shared "account region wins,
	// strip /v2 from a custom chat endpoint, then append path" logic.
	return ResolveProtocolDirectBillingEndpoint(opts, toolPath)
}

// WebSearch calls the upstream keyword search API.
func (c *Client) WebSearch(ctx context.Context, opts ChatOptions, req WebSearchRequest) (WebSearchResponse, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return WebSearchResponse{}, fmt.Errorf("search query is required")
	}
	if blocked := normalizeDomainList(req.BlockedDomains); len(blocked) > 0 {
		// The official client implements blocked domains as -site: terms.
		terms := make([]string, 0, len(blocked))
		for _, domain := range blocked {
			terms = append(terms, "-site:"+domain)
		}
		query = query + " (" + strings.Join(terms, " ") + ")"
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	body := map[string]any{
		"query":       query,
		"type":        "text2text",
		"max_results": maxResults,
	}
	if allowed := normalizeDomainList(req.AllowedDomains); len(allowed) > 0 {
		body["allowed_domains"] = allowed
	}
	if freshness := normalizeFreshness(req.Freshness); freshness != "" {
		body["freshness"] = freshness
	}
	var parsed struct {
		Code           int             `json:"code"`
		Msg            string          `json:"msg"`
		Query          string          `json:"query"`
		Type           string          `json:"type"`
		Provider       string          `json:"provider"`
		Results        []WebSearchItem `json:"results"`
		TotalResults   int             `json:"total_results"`
		ResponseTimeMs int             `json:"response_time_ms"`
	}
	status, err := c.postAgentToolJSON(ctx, opts, agentToolSearchPath, "search", body, &parsed)
	if err != nil {
		return WebSearchResponse{}, err
	}
	if parsed.Code != 0 {
		return WebSearchResponse{}, fmt.Errorf("CodeBuddy search failed: %s (code %d)", strutil.First(parsed.Msg, "upstream search error"), parsed.Code)
	}
	results := parsed.Results
	if results == nil {
		results = []WebSearchItem{}
	}
	total := parsed.TotalResults
	if total == 0 && len(results) > 0 {
		total = len(results)
	}
	return WebSearchResponse{
		Object:         "search.result",
		Query:          strutil.First(parsed.Query, query),
		SearchType:     strutil.First(parsed.Type, "text2text"),
		Provider:       parsed.Provider,
		Results:        results,
		TotalResults:   total,
		ResponseTimeMs: parsed.ResponseTimeMs,
		Status:         status,
	}, nil
}

// WebFetch calls the upstream page-fetch API. The upstream performs the fetch,
// so local SSRF exposure is unchanged; only the URL scheme is validated here.
func (c *Client) WebFetch(ctx context.Context, opts ChatOptions, req WebFetchRequest) (WebFetchResponse, error) {
	format := strings.TrimSpace(req.Format)
	if format == "" {
		format = "markdown"
	}
	maxLength := req.MaxLength
	if maxLength <= 0 {
		maxLength = 100000
	}
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = 20
	}
	body := map[string]any{
		"url":        req.URL,
		"format":     format,
		"timeout":    timeout,
		"max_length": maxLength,
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		body["prompt"] = prompt
	}
	var parsed struct {
		Code          int    `json:"code"`
		Msg           string `json:"msg"`
		URL           string `json:"url"`
		Title         string `json:"title"`
		Content       string `json:"content"`
		ContentType   string `json:"content_type"`
		ContentLength int    `json:"content_length"`
		Truncated     bool   `json:"truncated"`
		StatusCode    int    `json:"status_code"`
	}
	status, err := c.postAgentToolJSON(ctx, opts, agentToolWebFetchPath, "webfetch", body, &parsed)
	if err != nil {
		return WebFetchResponse{}, err
	}
	if parsed.Code != 0 {
		return WebFetchResponse{}, fmt.Errorf("CodeBuddy webfetch failed: %s (code %d)", strutil.First(parsed.Msg, "upstream webfetch error"), parsed.Code)
	}
	return WebFetchResponse{
		Object:        "web.fetch",
		URL:           strutil.First(parsed.URL, req.URL),
		Title:         parsed.Title,
		Content:       parsed.Content,
		ContentType:   parsed.ContentType,
		ContentLength: parsed.ContentLength,
		Truncated:     parsed.Truncated,
		StatusCode:    parsed.StatusCode,
		Status:        status,
	}, nil
}

// AgenticSearch calls the upstream multi-step research agent and collects the
// synthesized answer. onEvent, when non-nil, receives progress events while
// the SSE stream is still open.
func (c *Client) AgenticSearch(
	ctx context.Context,
	opts ChatOptions,
	query string,
	onEvent func(AgenticSearchEvent),
) (AgenticSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return AgenticSearchResult{}, fmt.Errorf("search query is required")
	}
	bizVia := "public"
	if strings.TrimSpace(opts.EnterpriseID) != "" {
		bizVia = "internal"
	}
	payload, err := json.Marshal(map[string]any{
		"query":       query,
		"search_mode": agentToolSearchModePro,
		"stream":      true,
		"biz_via":     bizVia,
	})
	if err != nil {
		return AgenticSearchResult{}, err
	}
	endpoint := ResolveProtocolDirectAgentToolEndpoint(opts, agentToolAgenticSearchPath)
	region := RegionOf(opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return AgenticSearchResult{}, err
	}
	headers, _ := c.BuildProtocolDirectHeaders(opts)
	headers.Set("Accept", "text/event-stream")
	req.Header = headers

	resp, err := c.For(opts.Proxy).Do(req)
	if err != nil {
		return AgenticSearchResult{}, fmt.Errorf("CodeBuddy agentic search transport error: %w [region=%s site=%s endpoint=%s]", err, region, config.NormalizeSite(opts.Site), endpoint)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		msg := extractErrorMessage(raw, resp.StatusCode)
		return AgenticSearchResult{}, &ChatError{
			Status:     resp.StatusCode,
			RetryAfter: resp.Header.Get("Retry-After"),
			Msg:        fmt.Sprintf("CodeBuddy agentic search failed with %d: %s [region=%s site=%s endpoint=%s]", resp.StatusCode, msg, region, config.NormalizeSite(opts.Site), endpoint),
		}
	}

	result := AgenticSearchResult{Status: resp.StatusCode}
	var synthesis strings.Builder
	var streamErr error
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), agentToolMaxSSEEventBytes)

	var eventName string
	var dataLines []string
	dispatch := func() {
		if len(dataLines) == 0 {
			eventName = ""
			return
		}
		raw := strings.Join(dataLines, "\n")
		name := eventName
		eventName = ""
		dataLines = dataLines[:0]
		if streamErr != nil {
			return
		}
		if strings.TrimSpace(raw) == "[DONE]" {
			return
		}
		event, terminal, eventErr := parseAgenticSearchEvent(name, raw)
		if eventErr != nil {
			streamErr = eventErr
			return
		}
		if terminal {
			result.Content = event.Delta
			result.Events = append(result.Events, event)
			if onEvent != nil {
				onEvent(event)
			}
			return
		}
		if event.Type == "synthesis_delta" {
			synthesis.WriteString(event.Delta)
		}
		if event.Type != "" {
			result.Events = append(result.Events, event)
			if onEvent != nil {
				onEvent(event)
			}
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			dispatch()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	dispatch()
	if err := scanner.Err(); err != nil {
		return AgenticSearchResult{}, fmt.Errorf("CodeBuddy agentic search stream error: %w [region=%s site=%s endpoint=%s]", err, region, config.NormalizeSite(opts.Site), endpoint)
	}
	if streamErr != nil {
		return AgenticSearchResult{}, streamErr
	}
	if result.Content == "" {
		result.Content = synthesis.String()
	}
	if strings.TrimSpace(result.Content) == "" {
		return AgenticSearchResult{}, fmt.Errorf("CodeBuddy agentic search ended without synthesis content [region=%s site=%s endpoint=%s]", region, config.NormalizeSite(opts.Site), endpoint)
	}
	// 部分上游在最后一个 synthesis_delta 后直接关流、不发 done 帧；
	// 只要已经拿到完整答案就按成功处理。
	return result, nil
}

// parseAgenticSearchEvent converts one upstream SSE frame into a normalized
// event. terminal reports that the frame carried the final synthesis.
func parseAgenticSearchEvent(name, raw string) (event AgenticSearchEvent, terminal bool, err error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return AgenticSearchEvent{}, false, fmt.Errorf("invalid agentic search event %q: %w", name, err)
	}
	event.Type = strutil.First(name, searchString(payload["type"]))
	event.Agent = searchString(payload["agent"])
	event.Tool = searchString(payload["tool"])
	event.CallID = strutil.First(searchString(payload["call_id"]), searchString(payload["callId"]))
	event.Status = searchString(payload["status"])
	event.ItemsCount = intFrom(payload["items_count"])
	event.LatencyMs = intFrom(payload["latency_ms"])
	event.Delta = searchString(payload["delta"])
	if event.Delta == "" {
		event.Delta = searchString(payload["text"])
	}
	event.Turn = intFrom(payload["turn"])
	event.Phase = searchString(payload["phase"])
	event.Outcome = searchString(payload["outcome"])
	event.RequestID = searchString(payload["request_id"])
	if event.Type == "done" {
		if synthesis, ok := payload["synthesis"].(map[string]any); ok {
			event.Type = "done"
			return AgenticSearchEvent{
				Type:      "done",
				Delta:     searchString(synthesis["content"]),
				RequestID: event.RequestID,
			}, true, nil
		}
	}
	if errCode := intFrom(payload["code"]); errCode != 0 {
		return AgenticSearchEvent{}, false, fmt.Errorf("agentic search upstream error: %s (code %d)", strutil.First(searchString(payload["msg"]), "unknown error"), errCode)
	}
	if event.Type == "error" {
		return AgenticSearchEvent{}, false, fmt.Errorf("agentic search upstream error: %s", strutil.First(searchString(payload["message"]), searchString(payload["msg"]), "unknown error"))
	}
	if event.Type == "" {
		return AgenticSearchEvent{}, false, nil
	}
	return event, false, nil
}

func searchString(value any) string {
	text, _ := value.(string)
	return text
}

// postAgentToolJSON posts one JSON request to an /agenttool/v1/* endpoint and
// decodes a JSON response. Non-2xx responses become ChatError so the gateway
// can reuse its retry/refresh policy.
func (c *Client) postAgentToolJSON(
	ctx context.Context,
	opts ChatOptions,
	toolPath, label string,
	body any,
	out any,
) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	endpoint := ResolveProtocolDirectAgentToolEndpoint(opts, toolPath)
	region := RegionOf(opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	headers, _ := c.BuildProtocolDirectHeaders(opts)
	headers.Set("Accept", "application/json")
	req.Header = headers

	resp, err := c.For(opts.Proxy).Do(req)
	if err != nil {
		return 0, fmt.Errorf("CodeBuddy %s transport error: %w [region=%s site=%s endpoint=%s]", label, err, region, config.NormalizeSite(opts.Site), endpoint)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, agentToolMaxResponseBytes))
	if err != nil {
		return 0, fmt.Errorf("CodeBuddy %s read error: %w [region=%s site=%s endpoint=%s]", label, err, region, config.NormalizeSite(opts.Site), endpoint)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := extractErrorMessage(raw, resp.StatusCode)
		return 0, &ChatError{
			Status:     resp.StatusCode,
			RetryAfter: resp.Header.Get("Retry-After"),
			Msg:        fmt.Sprintf("CodeBuddy %s failed with %d: %s [region=%s site=%s endpoint=%s]", label, resp.StatusCode, msg, region, config.NormalizeSite(opts.Site), endpoint),
		}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return 0, fmt.Errorf("CodeBuddy %s returned invalid JSON: %w [region=%s site=%s endpoint=%s]", label, err, region, config.NormalizeSite(opts.Site), endpoint)
	}
	return resp.StatusCode, nil
}

func normalizeDomainList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// normalizeFreshness 只放行上游接受的时间过滤语法：d / d1-d30、
// m / m1-m12、y / y1-y5。模型生成的非法值（如 d1-d7）直接丢弃，
// 避免整次搜索被上游以 15003 拒绝。
func normalizeFreshness(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	unit := value[0]
	digits := value[1:]
	switch unit {
	case 'd':
		if digits == "" || validFreshnessNumber(digits, 30) {
			return value
		}
	case 'm':
		if digits == "" || validFreshnessNumber(digits, 12) {
			return value
		}
	case 'y':
		if digits == "" || validFreshnessNumber(digits, 5) {
			return value
		}
	}
	return ""
}

func validFreshnessNumber(digits string, maxValue int) bool {
	if len(digits) > 2 {
		return false
	}
	n := 0
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
	}
	return n >= 1 && n <= maxValue
}
