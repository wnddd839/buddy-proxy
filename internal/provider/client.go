package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

type Client struct {
	HTTP       *http.Client
	IDEVersion string
	// Log 上游失败排障日志；为空时回退 slog.Default()。网关 New 时注入进程 logger。
	Log *slog.Logger
}

func NewClient(cfg config.Config) *Client {
	// 重要：protocol_direct 聊天不要设置 http.Client.Timeout。
	// CodeBuddy 始终上游流式；总超时会截断长 agent 回合/慢模型 SSE，导致客户端反复重连。
	headerTimeout := cfg.HTTPTimeout
	if headerTimeout <= 0 {
		// 慢模型（hy4-preview）首字节可能超过 60s。
		headerTimeout = 180 * time.Second
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Client{
		HTTP: &http.Client{
			Timeout:   0,
			Transport: transport,
		},
		IDEVersion: strutil.First(cfg.IDEVersion, config.DefaultIDEVersion),
		Log:        slog.Default(),
	}
}

type ChatOptions struct {
	Model               string
	Messages            []map[string]any
	Stream              bool
	Tools               any
	ToolChoice          any
	Temperature         *float64
	TopP                *float64
	MaxCompletionTokens int
	ReasoningEffort     string
	Reasoning           map[string]any
	Thinking            map[string]any
	BearerToken         string
	UserID              string
	BaseURL             string
	Site                string
	Product             string
	InternetEnvironment string
	APIEndpoint         string
	ChatCompletionsPath string
	Domain              string
	EnterpriseID        string
	TenantID            string
	DepartmentFullName  string
	ExtraHeaders        map[string]string
	OnDelta             func(string)
	OnThinkingDelta     func(string)
	OnEvent             func(Event)
}

type Event struct {
	Type           string
	Text           string
	ID             string
	Name           string
	Input          map[string]any
	ArgumentsDelta string
	Index          int
	StopReason     string
	Message        string
	Source         string
	Usage          *Usage
}

type Turn struct {
	Text       string
	Thinking   string
	ToolUses   []ToolUse
	Errors     []string
	StopReason string
	Usage      Usage
}

type ToolUse struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Input  map[string]any `json:"input"`
	Source string         `json:"source"`
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
	// Source 区分上游真实值与本地估算：upstream | estimated。
	Source string `json:"usage_source,omitempty"`
}

func (u Usage) CachedTokens() int {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	if u.CacheReadInputTokens > 0 {
		return u.CacheReadInputTokens
	}
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	return 0
}

type Result struct {
	Turn       Turn
	DurationMs int64
	Bytes      int64
	EventCount int
	DeltaCount int
	Status     int
	Model      string
}

func NormalizeBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func IsDomestic(site, internetEnvironment, baseURL string) bool {
	site = config.NormalizeSite(site)
	env := strings.ToLower(strings.TrimSpace(internetEnvironment))
	host := strings.ToLower(baseURL)
	return site == "domestic" || env == "domestic" || env == "cn" || env == "china" || env == "internal" ||
		strings.Contains(host, "codebuddy.cn") || strings.Contains(host, "workbuddy.cn") || strings.Contains(host, "copilot.tencent.com")
}

// RegionOf 返回 protocol_direct 路由用的 "domestic" 或 "global"。
// 账号 site / internetEnvironment 优先于反代进程所在主机
// （海外 VPS/VPN 不得把国内账号打到海外）。
func RegionOf(opts ChatOptions) string {
	if IsDomestic(opts.Site, opts.InternetEnvironment, "") {
		return "domestic"
	}
	// 仅当 site/env 未设置或模糊时，才回退到 BaseURL 主机判断。
	if strings.TrimSpace(opts.Site) == "" && strings.TrimSpace(opts.InternetEnvironment) == "" &&
		IsDomestic("", "", opts.BaseURL) {
		return "domestic"
	}
	return "global"
}

func ResolveProtocolDirectBaseURL(opts ChatOptions) string {
	if RegionOf(opts) == "domestic" {
		if config.NormalizeProduct(opts.Product) == "workbuddy" {
			return "https://www.workbuddy.cn"
		}
		return "https://copilot.tencent.com"
	}
	if config.NormalizeProduct(opts.Product) == "workbuddy" {
		return "https://www.workbuddy.ai"
	}
	configured := NormalizeBaseURL(opts.BaseURL)
	if configured != "" && !IsDomestic("", "", configured) {
		return configured
	}
	return "https://www.codebuddy.ai"
}

func AlignAPIEndpoint(product, endpoint string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return ""
	}
	lower := strings.ToLower(endpoint)
	if config.NormalizeProduct(product) == "workbuddy" {
		if strings.Contains(lower, "workbuddy") {
			return endpoint
		}
		return ""
	}
	if strings.Contains(lower, "workbuddy") {
		return ""
	}
	return endpoint
}

func endpointMatchesRegion(endpoint, region string) bool {
	lower := strings.ToLower(endpoint)
	domestic := strings.Contains(lower, "copilot.tencent.com") || strings.Contains(lower, "codebuddy.cn") || strings.Contains(lower, "workbuddy.cn")
	if region == "domestic" {
		return domestic
	}
	return !domestic && endpoint != ""
}

func ResolveProtocolDirectEndpoint(opts ChatOptions) string {
	region := RegionOf(opts)
	if endpoint := strings.TrimRight(strings.TrimSpace(opts.APIEndpoint), "/"); endpoint != "" {
		// 仅当显式 APIEndpoint 与账号区域一致时才采用。
		if endpointMatchesRegion(endpoint, region) {
			return endpoint
		}
	}
	base := ResolveProtocolDirectBaseURL(opts)
	path := strings.TrimSpace(opts.ChatCompletionsPath)
	if path == "" {
		path = config.DefaultChatCompletionsPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasSuffix(base, "/v2") && strings.HasPrefix(path, "/v2/") {
		path = path[3:]
	}
	return base + path
}

// ResolveProtocolDirectBillingEndpoint builds a /v2/billing/meter/* URL with the same
// region + custom APIEndpoint rules as chat, including /v2 prefix deduplication.
func ResolveProtocolDirectBillingEndpoint(opts ChatOptions, meterPath string) string {
	meterPath = strings.TrimSpace(meterPath)
	if !strings.HasPrefix(meterPath, "/") {
		meterPath = "/" + meterPath
	}
	region := RegionOf(opts)
	base := strings.TrimRight(ResolveProtocolDirectBaseURL(opts), "/")
	if endpoint := strings.TrimRight(strings.TrimSpace(opts.APIEndpoint), "/"); endpoint != "" && endpointMatchesRegion(endpoint, region) {
		base = strings.TrimRight(protocolDirectHostBase(endpoint), "/")
	}
	if strings.HasSuffix(base, "/v2") && strings.HasPrefix(meterPath, "/v2/") {
		meterPath = meterPath[3:]
	}
	return base + meterPath
}

func protocolDirectHostBase(endpoint string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	for _, marker := range []string{"/v2/", "/v3/"} {
		if idx := strings.Index(endpoint, marker); idx > 0 {
			return endpoint[:idx]
		}
	}
	return endpoint
}

// ResolveProtocolDirectDomain 返回 chat 请求的 X-Domain。
// 官方 CLI 从 chat endpoint 主机推导，而非账号上的 portal 登录域（常为 www.codebuddy.cn）。
func ResolveProtocolDirectDomain(opts ChatOptions) string {
	if host := authorityHost(ResolveProtocolDirectEndpoint(opts)); host != "" {
		return host
	}
	if domain := strings.TrimSpace(opts.Domain); domain != "" && !isPortalDomain(domain) {
		return domain
	}
	if IsDomestic(opts.Site, opts.InternetEnvironment, opts.BaseURL) {
		return "copilot.tencent.com"
	}
	return "www.codebuddy.ai"
}

func authorityHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	for _, prefix := range []string{"https://", "http://"} {
		if rest, ok := strings.CutPrefix(endpoint, prefix); ok {
			host, _, _ := strings.Cut(rest, "/")
			return strings.TrimSpace(host)
		}
	}
	return ""
}

func isPortalDomain(domain string) bool {
	host := strings.ToLower(strings.TrimSpace(domain))
	switch host {
	case "www.codebuddy.cn", "codebuddy.cn", "www.codebuddy.ai", "codebuddy.ai",
		"www.workbuddy.cn", "workbuddy.cn", "www.workbuddy.ai", "workbuddy.ai":
		return true
	default:
		return false
	}
}

// NormalizeToolChoice 将 OpenAI 风格 tool_choice 转为 CodeBuddy protocol_direct 接受的字符串。
// 对象形式会导致上游 11101（tool_choice 类型不匹配）。
// ApplyReasoningFields 将客户端思考档位写入上游 body。
// 实测仅传 reasoning_effort 不会触发 reasoning_content；需 reasoning.effort。
func ApplyReasoningFields(body map[string]any, opts ChatOptions) {
	if opts.Reasoning != nil {
		body["reasoning"] = opts.Reasoning
	} else if effort := strings.TrimSpace(opts.ReasoningEffort); effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
		body["reasoning_effort"] = effort
	}
	if opts.Thinking != nil {
		body["thinking"] = opts.Thinking
	}
}

func NormalizeToolChoice(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || trimmed == "<nil>" {
			return nil
		}
		return trimmed
	case map[string]any:
		typ := strings.ToLower(strings.TrimSpace(fmt.Sprint(v["type"])))
		switch typ {
		case "auto", "none", "required":
			return typ
		case "function":
			// 上游不接受 forced-function 对象；回退 auto 以保持 tools 可用。
			return "auto"
		default:
			if typ != "" && typ != "<nil>" {
				return typ
			}
		}
	}
	return nil
}

func randomUUID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func (c *Client) BuildProtocolDirectHeaders(opts ChatOptions) http.Header {
	requestID := strutil.RandomHex(16)
	messageID := strutil.RandomHex(16)
	ideVersion := strutil.First(c.IDEVersion, config.DefaultIDEVersion)
	headers := http.Header{}
	headers.Set("Accept", "text/event-stream, application/json")
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Requested-With", "XMLHttpRequest")
	headers.Set("X-Agent-Intent", "craft")
	headers.Set("X-IDE-Type", "CLI")
	headers.Set("X-IDE-Name", "CLI")
	headers.Set("X-IDE-Version", ideVersion)
	headers.Set("X-Domain", ResolveProtocolDirectDomain(opts))
	// 上游 /v3/config 拒绝纯 "CLI/<ver>"（12403：UA 版本解析失败）。
	// 官方格式同时包含 CLI 与 CodeBuddy 版本段。
	headers.Set("User-Agent", fmt.Sprintf("CLI/%s CodeBuddy/%s", ideVersion, ideVersion))
	headers.Set("X-Product", "SaaS")
	headers.Set("X-User-Id", strutil.First(opts.UserID, "anonymous"))
	// 官方 CLI 会话 ID 使用大写 UUID。
	headers.Set("X-Conversation-ID", randomUUID())
	headers.Set("X-Conversation-Request-ID", requestID)
	headers.Set("X-Conversation-Message-ID", messageID)
	headers.Set("X-Request-ID", messageID)
	if config.NormalizeProduct(opts.Product) == "workbuddy" {
		headers.Set("X-IDE-Type", "VSCode")
		headers.Set("X-IDE-Name", "VSCode")
		headers.Set("X-IDE-Version", config.DefaultWorkBuddyIDEVersion)
		headers.Set("X-Product-Version", config.DefaultWorkBuddyProductVer)
		headers.Set("X-Env-ID", "production")
		headers.Set("User-Agent", fmt.Sprintf("VSCode/%s WorkBuddy/%s", config.DefaultWorkBuddyIDEVersion, config.DefaultWorkBuddyProductVer))
	}
	if opts.EnterpriseID != "" {
		headers.Set("X-Enterprise-Id", opts.EnterpriseID)
		headers.Set("X-Tenant-Id", strutil.First(opts.TenantID, opts.EnterpriseID))
	}
	if opts.DepartmentFullName != "" {
		headers.Set("X-Department-Info", opts.DepartmentFullName)
	}
	if token := strings.TrimSpace(opts.BearerToken); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	for k, v := range opts.ExtraHeaders {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		headers.Set(k, v)
	}
	return headers
}

func NormalizeMessageRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "", "human", "ai":
		return "user"
	case "developer":
		// OpenAI/ZCode 的 "developer" 角色会被 CodeBuddy 上游以 11128 拒绝
		// ("unapproved channel"). Map to system instructions.
		return "system"
	default:
		return role
	}
}

func EnsureUpstreamMessages(messages []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role := NormalizeMessageRole(fmt.Sprint(message["role"]))
		item := map[string]any{"role": role}
		if toolCalls, ok := message["tool_calls"]; ok {
			item["tool_calls"] = toolCalls
		}
		if id, ok := message["tool_call_id"]; ok {
			item["tool_call_id"] = id
		}
		if name, ok := message["name"]; ok {
			item["name"] = name
		}
		item["content"] = sanitizeUpstreamContent(role, flattenContent(message["content"]))
		hasToolCalls := false
		if tc, ok := item["tool_calls"].([]any); ok && len(tc) > 0 {
			hasToolCalls = true
		}
		contentEmpty := false
		switch v := item["content"].(type) {
		case string:
			contentEmpty = strings.TrimSpace(v) == ""
		case nil:
			contentEmpty = true
		}
		if contentEmpty && !hasToolCalls && item["tool_call_id"] == nil {
			continue
		}
		out = append(out, item)
	}
	return out
}

// sanitizeUpstreamContent 改写已知触发上游 11128 的 system 指纹。
// 上游风控对 "Main branch (you will usually use this for PRs):" 整串敏感
// （ZCode System Context 注入），缺括号/冒号/值任一件即放行。
// 改写保留分支名语义，仅去括号注解。仅处理 system：用户原文与工具
// 结果必须原样透传，避免静默改变用户请求语义。
func sanitizeUpstreamContent(role string, content any) any {
	text, ok := content.(string)
	if !ok || role != "system" {
		return content
	}
	if !strings.Contains(text, "Main branch") || !strings.Contains(text, "for PRs") {
		return content
	}
	return strings.ReplaceAll(text, " (you will usually use this for PRs)", "")
}

func flattenContent(content any) any {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		structured := false
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				parts = append(parts, fmt.Sprint(part))
				continue
			}
			typ, _ := m["type"].(string)
			if typ != "" && typ != "text" {
				structured = true
				break
			}
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		if structured {
			return v
		}
		return strings.Join(parts, "")
	default:
		return fmt.Sprint(v)
	}
}

func (c *Client) Complete(ctx context.Context, opts ChatOptions) (Result, error) {
	started := time.Now()
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = "auto"
	}
	// CodeBuddy protocol_direct 拒绝非流式 chat（11101）。始终上游流式；
	// 需要 JSON 的调用方仍通过 readSSE 聚合成 Turn。
	messages := EnsureUpstreamMessages(opts.Messages)
	if len(messages) == 0 {
		return Result{}, fmt.Errorf("CodeBuddy chat completion failed: no valid messages after normalization")
	}
	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	body["stream_options"] = map[string]any{"include_usage": true}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.MaxCompletionTokens > 0 {
		body["max_completion_tokens"] = opts.MaxCompletionTokens
	}
	if opts.Tools != nil {
		body["tools"] = opts.Tools
	}
	if choice := NormalizeToolChoice(opts.ToolChoice); choice != nil {
		body["tool_choice"] = choice
	}
	ApplyReasoningFields(body, opts)
	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	endpoint := ResolveProtocolDirectEndpoint(opts)
	domain := ResolveProtocolDirectDomain(opts)
	region := RegionOf(opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header = c.BuildProtocolDirectHeaders(opts)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("CodeBuddy chat completion transport error: %w [region=%s site=%s endpoint=%s domain=%s model=%s]", err, region, config.NormalizeSite(opts.Site), endpoint, domain, model)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		msg := extractErrorMessage(raw, resp.StatusCode)
		c.logUpstreamFailure(msg, resp.StatusCode, body, opts, endpoint, domain, region, model)
		return Result{}, fmt.Errorf("CodeBuddy chat completion failed with %d: %s [region=%s site=%s endpoint=%s domain=%s model=%s]", resp.StatusCode, msg, region, config.NormalizeSite(opts.Site), endpoint, domain, model)
	}

	result, err := c.readSSE(resp.Body, opts)
	if err != nil {
		return Result{}, err
	}
	if len(result.Turn.Errors) > 0 {
		c.logUpstreamFailure(result.Turn.Errors[0], resp.StatusCode, body, opts, endpoint, domain, region, model)
		return Result{}, fmt.Errorf("%s", result.Turn.Errors[0])
	}
	result.DurationMs = time.Since(started).Milliseconds()
	result.Status = resp.StatusCode
	result.Model = model
	return result, nil
}

// logUpstreamFailure 在上游拒绝（非 2xx）或流中报错时输出归一化请求指纹（WARN）。
// 指纹脱敏：不含 token、无完整 prompt，只有字段形态与单条消息前 200 字预览；下游错误串保持不变。
func (c *Client) logUpstreamFailure(upstreamMsg string, status int, body map[string]any, opts ChatOptions, endpoint, domain, region, model string) {
	logger := slog.Default()
	if c != nil && c.Log != nil {
		logger = c.Log
	}
	logger.Warn("codebuddy upstream failure",
		"status", status,
		"upstreamMessage", strutil.Truncate(upstreamMsg, 300),
		"region", region,
		"site", config.NormalizeSite(opts.Site),
		"endpoint", endpoint,
		"domain", domain,
		"model", model,
		"request", DescribeUpstreamBody(body),
	)
}

// DescribeUpstreamBody 返回归一化上游请求体的脱敏指纹。
// 上游 4xx/5xx（11128/11101 等）排障时，直接看出可疑字段与消息形态。
func DescribeUpstreamBody(body map[string]any) map[string]any {
	fp := map[string]any{}
	if len(body) == 0 {
		return fp
	}
	fp["model"] = fmt.Sprint(body["model"])
	if v, ok := body["temperature"]; ok && v != nil {
		fp["temperature"] = fmt.Sprint(v)
	}
	if v, ok := body["top_p"]; ok && v != nil {
		fp["top_p"] = fmt.Sprint(v)
	}
	if v, ok := body["max_completion_tokens"]; ok && v != nil {
		fp["max_completion_tokens"] = fmt.Sprint(v)
	}
	if choice, ok := body["tool_choice"]; ok && choice != nil {
		fp["tool_choice"] = strutil.Truncate(fmt.Sprint(choice), 80)
	}
	if reasoning, ok := body["reasoning"]; ok && reasoning != nil {
		fp["reasoning"] = strutil.Truncate(fmt.Sprint(reasoning), 200)
	}
	if effort, ok := body["reasoning_effort"]; ok && effort != nil {
		fp["reasoning_effort"] = fmt.Sprint(effort)
	}
	if thinking, ok := body["thinking"]; ok && thinking != nil {
		fp["thinking"] = strutil.Truncate(fmt.Sprint(thinking), 200)
	}
	if tools, ok := body["tools"]; ok && tools != nil {
		names := toolNames(tools)
		fp["toolsCount"] = len(names)
		if len(names) > 0 {
			fp["toolNames"] = names
		}
		if raw, err := json.Marshal(tools); err == nil {
			fp["toolsJSONLen"] = len(raw)
		}
	}
	msgs := toMessageList(body["messages"])
	roles := make([]string, 0, len(msgs))
	details := make([]map[string]any, 0, len(msgs))
	totalChars := 0
	for _, msg := range msgs {
		role := fmt.Sprint(msg["role"])
		roles = append(roles, role)
		detail := map[string]any{"role": role}
		switch content := msg["content"].(type) {
		case string:
			totalChars += len(content)
			detail["contentKind"] = "string"
			detail["contentLen"] = len(content)
			if preview := strutil.Truncate(content, 200); preview != "" {
				detail["preview"] = preview
			}
		case []any:
			detail["contentKind"] = "structured_array"
			detail["contentLen"] = len(content)
			detail["partTypes"] = partTypes(content)
		case nil:
			detail["contentKind"] = "nil"
		default:
			detail["contentKind"] = fmt.Sprintf("%T", content)
		}
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			detail["toolCallsCount"] = len(tc)
			detail["toolCallNames"] = toolNames(tc)
		}
		if id, ok := msg["tool_call_id"]; ok && strutil.First(fmt.Sprint(id)) != "" {
			detail["hasToolCallID"] = true
		}
		if name, ok := msg["name"]; ok && strutil.First(fmt.Sprint(name)) != "" {
			detail["name"] = strutil.Truncate(fmt.Sprint(name), 80)
		}
		details = append(details, detail)
	}
	fp["messageCount"] = len(msgs)
	fp["roles"] = roles
	fp["messages"] = details
	fp["totalChars"] = totalChars
	return fp
}

func toMessageList(messages any) []map[string]any {
	switch v := messages.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if m, ok := raw.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

// toolNames 提取 tools / tool_calls 中的 function 名（只记名，不记参数）。
func toolNames(items any) []string {
	arr, ok := items.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if fn, ok := m["function"].(map[string]any); ok {
			if name := strutil.First(fmt.Sprint(fn["name"])); name != "" {
				names = append(names, name)
				continue
			}
		}
		if name := strutil.First(fmt.Sprint(m["name"])); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func partTypes(parts []any) []string {
	types := make([]string, 0, len(parts))
	for _, part := range parts {
		if m, ok := part.(map[string]any); ok {
			types = append(types, strutil.First(fmt.Sprint(m["type"]), "map"))
			continue
		}
		types = append(types, fmt.Sprintf("%T", part))
	}
	return types
}

func (c *Client) readSSE(body io.Reader, opts ChatOptions) (Result, error) {
	acc := newAccumulator()
	reader := bufio.NewReaderSize(body, 64*1024)
	var (
		eventCount int
		deltaCount int
		bytesRead  int64
		dataBuf    strings.Builder
	)
	flush := func() {
		data := strings.TrimSpace(dataBuf.String())
		dataBuf.Reset()
		if data == "" || data == "[DONE]" {
			return
		}
		raw := []byte(data)
		// 热路径优先 typed OpenAI chunk，减少 map[string]any 分配；非 OpenAI 形态再回退。
		var events []Event
		var chunk openAISSEChunk
		if err := json.Unmarshal(raw, &chunk); err == nil && chunk.looksOpenAI() {
			events = eventsFromOpenAIChunk(chunk)
		} else {
			var payload any
			if err := json.Unmarshal(raw, &payload); err != nil {
				return
			}
			events = MapSSEEvent(payload)
		}
		for _, event := range events {
			eventCount++
			acc.push(event)
			if opts.OnEvent != nil {
				opts.OnEvent(event)
			}
			if event.Type == "text_delta" && event.Text != "" {
				deltaCount++
				if opts.OnDelta != nil {
					opts.OnDelta(event.Text)
				}
			}
			if event.Type == "thinking_delta" && event.Text != "" && opts.OnThinkingDelta != nil {
				opts.OnThinkingDelta(event.Text)
			}
		}
	}
	for {
		line, err := reader.ReadString('\n')
		bytesRead += int64(len(line))
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				flush()
			} else if payload, ok := strings.CutPrefix(trimmed, "data:"); ok {
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.WriteString(strings.TrimSpace(payload))
			}
		}
		if err == io.EOF {
			flush()
			break
		}
		if err != nil {
			return Result{}, err
		}
	}
	return Result{
		// 上游已给 usage 时跳过整段 messages 估算，避免长上下文白烧 CPU。
		Turn: acc.snapshot(func() string {
			return estimatePromptText(opts.Messages)
		}),
		Bytes:      bytesRead,
		EventCount: eventCount,
		DeltaCount: deltaCount,
	}, nil
}

// openAISSEChunk 覆盖 protocol_direct 主流式帧，避免每事件 Unmarshal(map)。
type openAISSEChunk struct {
	Choices []openAISSEChoice `json:"choices"`
	Usage   map[string]any    `json:"usage"`
}

type openAISSEChoice struct {
	Delta        *openAISSEDelta `json:"delta"`
	Message      *openAISSEDelta `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type openAISSEDelta struct {
	Content          any                 `json:"content"`
	Text             any                 `json:"text"`
	ReasoningContent any                 `json:"reasoning_content"`
	ToolCalls        []openAISSEToolCall `json:"tool_calls"`
}

type openAISSEToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (c openAISSEChunk) looksOpenAI() bool {
	return len(c.Choices) > 0 || c.Usage != nil
}

func eventsFromOpenAIChunk(chunk openAISSEChunk) []Event {
	events := make([]Event, 0, 2)
	if chunk.Usage != nil {
		if usageEvent := usageEventFromPayload(map[string]any{"usage": chunk.Usage}); usageEvent != nil {
			events = append(events, *usageEvent)
		}
	}
	if len(chunk.Choices) == 0 {
		return events
	}
	choice := chunk.Choices[0]
	delta := choice.Delta
	if delta == nil {
		delta = choice.Message
	}
	if delta != nil {
		if text := firstText(delta.Content, delta.Text); text != "" {
			events = append(events, Event{Type: "text_delta", Text: text, Source: "codebuddy_openai"})
		}
		if text := firstText(delta.ReasoningContent); text != "" {
			events = append(events, Event{Type: "thinking_delta", Text: text, Source: "codebuddy_openai"})
		}
		for _, tc := range delta.ToolCalls {
			events = append(events, Event{
				Type:           "tool_call_delta",
				Index:          tc.Index,
				ID:             tc.ID,
				Name:           tc.Function.Name,
				ArgumentsDelta: tc.Function.Arguments,
				Source:         "codebuddy_openai",
			})
		}
	}
	if finish := strings.TrimSpace(choice.FinishReason); finish != "" {
		stop := finish
		if finish == "tool_calls" {
			stop = "tool_use"
		}
		events = append(events, Event{Type: "turn_ended", StopReason: stop, Source: "codebuddy_openai"})
	}
	return events
}

func MapSSEEvent(payload any) []Event {
	data := unwrap(payload)
	if data == nil {
		if text := firstText(payload); text != "" {
			return []Event{{Type: "text_delta", Text: text, Source: "codebuddy_sse"}}
		}
		return nil
	}
	obj, ok := data.(map[string]any)
	if !ok {
		if text := firstText(data); text != "" {
			return []Event{{Type: "text_delta", Text: text, Source: "codebuddy_sse"}}
		}
		return nil
	}
	if usageEvent := usageEventFromPayload(obj); usageEvent != nil {
		events := []Event{*usageEvent}
		if _, hasChoices := obj["choices"]; hasChoices {
			events = append(events, mapOpenAIDelta(obj)...)
		}
		return events
	}
	if _, hasChoices := obj["choices"]; hasChoices {
		return mapOpenAIDelta(obj)
	}
	kind := strutil.First(
		fmt.Sprint(obj["sessionUpdate"]),
		fmt.Sprint(obj["type"]),
		fmt.Sprint(obj["event"]),
	)
	status := strutil.First(fmt.Sprint(obj["status"]), fmt.Sprint(obj["state"]))
	if kind != "tool_result" && (obj["error"] != nil || kind == "error" || isErrorStatus(status)) {
		return []Event{{Type: "upstream_error", Message: describeError(obj), Source: "codebuddy_sse"}}
	}
	switch kind {
	case "agent_message_chunk", "message", "text_delta", "delta":
		if text := firstText(obj["content"], obj["text"], obj["delta"], obj["message"]); text != "" {
			return []Event{{Type: "text_delta", Text: text, Source: "codebuddy_sse"}}
		}
	case "agent_thought_chunk", "thinking_delta":
		if text := firstText(obj["content"], obj["text"], obj["delta"], obj["thought"]); text != "" {
			return []Event{{Type: "thinking_delta", Text: text, Source: "codebuddy_sse"}}
		}
	case "tool_call", "tool_use":
		return []Event{{
			Type:   "tool_use",
			ID:     strutil.First(fmt.Sprint(obj["toolCallId"]), fmt.Sprint(obj["tool_call_id"]), fmt.Sprint(obj["id"]), "call_"+strutil.RandomHex(8)),
			Name:   strutil.First(fmt.Sprint(obj["name"]), fmt.Sprint(obj["title"]), fmt.Sprint(obj["toolName"]), fmt.Sprint(obj["tool"])),
			Input:  normalizeToolInput(obj["rawInput"], obj["input"], obj["arguments"], obj["args"]),
			Source: "codebuddy_sse",
		}}
	case "tool_call_delta":
		args, _ := obj["argumentsDelta"].(string)
		return []Event{{
			Type:           "tool_call_delta",
			Index:          intFrom(obj["index"]),
			ID:             strutil.First(fmt.Sprint(obj["toolCallId"]), fmt.Sprint(obj["tool_call_id"]), fmt.Sprint(obj["id"])),
			Name:           strutil.First(fmt.Sprint(obj["name"]), fmt.Sprint(obj["title"]), fmt.Sprint(obj["toolName"])),
			ArgumentsDelta: args,
			Input:          normalizeToolInput(obj["input"], obj["rawInput"]),
			Source:         "codebuddy_sse",
		}}
	case "session_end", "done", "completed", "agent_message_end":
		return []Event{{Type: "turn_ended", Source: "codebuddy_sse"}}
	}
	return nil
}

func mapOpenAIDelta(payload map[string]any) []Event {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		delta, _ = choice["message"].(map[string]any)
	}
	events := make([]Event, 0, 2)
	if text := firstText(delta["content"], delta["text"]); text != "" {
		events = append(events, Event{Type: "text_delta", Text: text, Source: "codebuddy_openai"})
	}
	if text := firstText(delta["reasoning_content"], delta["reasoning"]); text != "" {
		events = append(events, Event{Type: "thinking_delta", Text: text, Source: "codebuddy_openai"})
	}
	if toolCalls, ok := delta["tool_calls"].([]any); ok {
		for _, raw := range toolCalls {
			tc, _ := raw.(map[string]any)
			fn, _ := tc["function"].(map[string]any)
			args, _ := fn["arguments"].(string)
			events = append(events, Event{
				Type:           "tool_call_delta",
				Index:          intFrom(tc["index"]),
				ID:             fmt.Sprint(tc["id"]),
				Name:           fmt.Sprint(fn["name"]),
				ArgumentsDelta: args,
				Source:         "codebuddy_openai",
			})
		}
	}
	if finish := strutil.First(fmt.Sprint(choice["finish_reason"]), fmt.Sprint(choice["finishReason"])); finish != "" && finish != "<nil>" {
		stop := finish
		if finish == "tool_calls" {
			stop = "tool_use"
		}
		events = append(events, Event{Type: "turn_ended", StopReason: stop, Source: "codebuddy_openai"})
	}
	return events
}

type accumulator struct {
	text       string
	thinking   string
	toolUses   []ToolUse
	fragments  map[int]*ToolUse
	errors     []string
	stopReason string
	usage      Usage
	hasUsage   bool
}

func newAccumulator() *accumulator {
	return &accumulator{fragments: map[int]*ToolUse{}}
}

func (a *accumulator) push(event Event) {
	switch event.Type {
	case "text_delta":
		a.text += event.Text
	case "thinking_delta":
		a.thinking += event.Text
	case "tool_use":
		a.toolUses = append(a.toolUses, ToolUse{
			ID:     strutil.First(event.ID, "toolu_"+strutil.RandomHex(8)),
			Name:   event.Name,
			Input:  event.Input,
			Source: event.Source,
		})
	case "tool_call_delta":
		frag := a.fragments[event.Index]
		if frag == nil {
			frag = &ToolUse{Input: map[string]any{}}
			a.fragments[event.Index] = frag
		}
		if event.ID != "" {
			frag.ID = event.ID
		}
		if event.Name != "" {
			frag.Name = event.Name
		}
		if event.ArgumentsDelta != "" {
			if existing, ok := frag.Input["__args"].(string); ok {
				frag.Input["__args"] = existing + event.ArgumentsDelta
			} else {
				frag.Input["__args"] = event.ArgumentsDelta
			}
		}
		for k, v := range event.Input {
			frag.Input[k] = v
		}
	case "upstream_error":
		a.errors = append(a.errors, strutil.First(event.Message, "upstream error"))
		a.stopReason = "error"
	case "turn_ended":
		if event.StopReason != "" {
			a.stopReason = event.StopReason
		}
	case "usage":
		if event.Usage != nil {
			a.usage = *event.Usage
			if a.usage.Source == "" {
				a.usage.Source = "upstream"
			}
			a.hasUsage = true
		}
	}
}

func (a *accumulator) snapshot(promptFn func() string) Turn {
	tools := append([]ToolUse{}, a.toolUses...)
	for _, frag := range a.fragments {
		input := map[string]any{}
		if args, ok := frag.Input["__args"].(string); ok && args != "" {
			input = normalizeToolInput(args)
		} else {
			for k, v := range frag.Input {
				if k == "__args" {
					continue
				}
				input[k] = v
			}
		}
		tools = append(tools, ToolUse{
			ID:     strutil.First(frag.ID, "toolu_"+strutil.RandomHex(8)),
			Name:   frag.Name,
			Input:  input,
			Source: "provider_delta",
		})
	}
	stop := a.stopReason
	if stop == "" {
		if len(a.errors) > 0 {
			stop = "error"
		} else if len(tools) > 0 {
			stop = "tool_use"
		} else {
			stop = "end_turn"
		}
	}
	var usage Usage
	if a.hasUsage {
		usage = a.usage
		if usage.Source == "" {
			usage.Source = "upstream"
		}
		if usage.TotalTokens == 0 {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
	} else {
		prompt := ""
		if promptFn != nil {
			prompt = promptFn()
		}
		usage = estimateUsage(prompt, a.text)
	}
	return Turn{
		Text:       a.text,
		Thinking:   a.thinking,
		ToolUses:   tools,
		Errors:     append([]string{}, a.errors...),
		StopReason: stop,
		Usage:      usage,
	}
}

func estimatePromptText(messages []map[string]any) string {
	var b strings.Builder
	for _, message := range messages {
		switch v := message["content"].(type) {
		case string:
			b.WriteString(v)
		default:
			raw, _ := json.Marshal(v)
			b.Write(raw)
		}
	}
	return b.String()
}

func usageEventFromPayload(obj map[string]any) *Event {
	raw, ok := obj["usage"].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	usage := ParseUsage(raw)
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 && usage.CachedTokens() == 0 {
		return nil
	}
	return &Event{Type: "usage", Usage: &usage, Source: "codebuddy_openai"}
}

// ParseUsage 解析 OpenAI / Anthropic / DeepSeek 兼容的 token 字段。
func ParseUsage(raw map[string]any) Usage {
	if raw == nil {
		return Usage{}
	}
	usage := Usage{
		PromptTokens:             intFrom(raw["prompt_tokens"], raw["input_tokens"]),
		CompletionTokens:         intFrom(raw["completion_tokens"], raw["output_tokens"]),
		TotalTokens:              intFrom(raw["total_tokens"]),
		CacheReadInputTokens:     intFrom(raw["cache_read_input_tokens"]),
		CacheCreationInputTokens: intFrom(raw["cache_creation_input_tokens"]),
		PromptCacheHitTokens:     intFrom(raw["prompt_cache_hit_tokens"], raw["cached_tokens"]),
		PromptCacheMissTokens:    intFrom(raw["prompt_cache_miss_tokens"]),
	}
	for _, key := range []string{"prompt_tokens_details", "input_tokens_details"} {
		details, ok := raw[key].(map[string]any)
		if !ok || details == nil {
			continue
		}
		cached := intFrom(details["cached_tokens"], details["cache_read_input_tokens"], details["cache_hit_tokens"])
		if cached > 0 {
			usage.PromptTokensDetails = &PromptTokensDetails{CachedTokens: cached}
			break
		}
	}
	if details, ok := raw["completion_tokens_details"].(map[string]any); ok && details != nil {
		reasoning := intFrom(details["reasoning_tokens"])
		if reasoning > 0 {
			usage.CompletionTokensDetails = &CompletionTokensDetails{ReasoningTokens: reasoning}
		}
	}
	// 仅存在厂商别名时，归一化为 OpenAI prompt_tokens_details.cached_tokens。
	if usage.PromptTokensDetails == nil {
		if cached := usage.CachedTokens(); cached > 0 {
			usage.PromptTokensDetails = &PromptTokensDetails{CachedTokens: cached}
		}
	}
	if usage.TotalTokens == 0 && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func estimateUsage(prompt, output string) Usage {
	promptTokens := estimateTokenCount(prompt)
	completionTokens := estimateTokenCount(output)
	return Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Source:           "estimated",
	}
}

// estimateTokenCount 按字符估算 token：ASCII ≈4 字符/token，CJK ≈1 字/token。
// 勿用 len([]byte)：UTF-8 中文 3 字节/字，会系统性低估 50%+。
func estimateTokenCount(s string) int {
	if s == "" {
		return 1
	}
	var ascii, cjk, other int
	for _, r := range s {
		switch {
		case r <= 0x7F:
			ascii++
		case isCJKRune(r):
			cjk++
		default:
			other++
		}
	}
	return max(1, (ascii+3)/4+cjk+(other+1)/2)
}

func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

func NormalizeModels(input any) []map[string]any {
	root := unwrapMap(input)
	modelRoot := root
	if nested, ok := root["models"].(map[string]any); ok {
		modelRoot = nested
	}
	var rows []any
	switch v := modelRoot["models"].(type) {
	case []any:
		rows = v
	default:
		if arr, ok := modelRoot["availableModels"].([]any); ok {
			objectRows := make([]any, 0)
			for _, item := range arr {
				if _, ok := item.(map[string]any); ok {
					objectRows = append(objectRows, item)
				}
			}
			if len(objectRows) > 0 {
				rows = objectRows
			}
		}
		if rows == nil {
			if arr, ok := input.([]any); ok {
				rows = arr
			}
		}
	}
	allowed := map[string]struct{}{}
	if arr, ok := modelRoot["availableModels"].([]any); ok {
		onlyStrings := true
		for _, item := range arr {
			if _, ok := item.(map[string]any); ok {
				onlyStrings = false
				break
			}
		}
		if onlyStrings {
			for _, item := range arr {
				id := strings.TrimSpace(fmt.Sprint(item))
				if id != "" {
					allowed[id] = struct{}{}
				}
			}
		}
	}
	// 线上 /v3/config 的 data.models 可能是裸数组（非嵌套 map）。
	if rows == nil {
		if arr, ok := root["models"].([]any); ok {
			rows = arr
		}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		id := strutil.First(fmt.Sprint(m["id"]), fmt.Sprint(m["modelId"]))
		if id == "" || id == "<nil>" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[id]; !ok {
				continue
			}
		}
		credits := strings.TrimSpace(fmt.Sprint(m["credits"]))
		if credits == "<nil>" {
			credits = ""
		}
		item := map[string]any{
			"id":             id,
			"object":         "model",
			"name":           strutil.First(fmt.Sprint(m["name"]), fmt.Sprint(m["label"]), fmt.Sprint(m["displayName"]), id),
			"owned_by":       strutil.First(fmt.Sprint(m["vendor"]), fmt.Sprint(m["provider"]), "codebuddy"),
			"supportsTools":  truthy(m["supportsToolCall"]) || truthy(m["supportsTools"]),
			"supportsImages": truthy(m["supportsImages"]) || truthy(m["supportsImage"]),
		}
		if truthy(m["supportsReasoning"]) {
			item["supportsReasoning"] = true
		}
		if truthy(m["onlyReasoning"]) {
			item["onlyReasoning"] = true
		}
		if reasoning, ok := m["reasoning"].(map[string]any); ok && len(reasoning) > 0 {
			item["reasoning"] = reasoning
		}
		if credits != "" {
			item["credits"] = credits
			if mult, ok := ParseCreditMultiplier(credits); ok {
				item["creditMultiplier"] = mult
				item["free"] = mult == 0
			}
		}
		if desc := strutil.First(fmt.Sprint(m["descriptionZh"]), fmt.Sprint(m["descriptionEn"])); desc != "" && desc != "<nil>" {
			item["description"] = desc
		}
		if n := strutil.PositiveInt(m["maxInputTokens"]); n > 0 {
			item["maxInputTokens"] = n
		}
		if n := strutil.PositiveInt(m["maxOutputTokens"]); n > 0 {
			item["maxOutputTokens"] = n
		}
		if n := strutil.PositiveInt(m["maxAllowedSize"]); n > 0 {
			item["maxAllowedSize"] = n
		}
		out = append(out, item)
	}
	return out
}

// ParseCreditMultiplier 解析上游标签，如 "x0.29 credits" / "x0.00 credits"。
func ParseCreditMultiplier(raw string) (float64, bool) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" || text == "<nil>" {
		return 0, false
	}
	text = strings.TrimPrefix(text, "x")
	text = strings.TrimSpace(text)
	for _, suffix := range []string{" credits", " credit", "credits", "credit"} {
		if strings.HasSuffix(text, suffix) {
			text = strings.TrimSpace(strings.TrimSuffix(text, suffix))
			break
		}
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func extractErrorMessage(raw []byte, status int) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		if len(text) > 400 {
			return text[:400]
		}
		return text
	}
	if errObj, ok := payload["error"].(map[string]any); ok {
		return strutil.First(fmt.Sprint(errObj["message"]), fmt.Sprint(errObj["code"]), text)
	}
	if msg := strutil.First(fmt.Sprint(payload["message"]), fmt.Sprint(payload["msg"]), fmt.Sprint(payload["error"])); msg != "" && msg != "<nil>" {
		if code, ok := payload["code"]; ok {
			return fmt.Sprintf("%s (code %v)", msg, code)
		}
		return msg
	}
	if len(text) > 400 {
		return text[:400]
	}
	return text
}

func unwrap(value any) any {
	m, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if data, exists := m["data"]; exists && data != nil {
		if nested, ok := data.(map[string]any); ok {
			return nested
		}
		return data
	}
	return m
}

func unwrapMap(value any) map[string]any {
	if m, ok := unwrap(value).(map[string]any); ok {
		return m
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func firstText(values ...any) string {
	for _, value := range values {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(v) != "" {
				return v
			}
		case map[string]any:
			if text, ok := v["text"].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
			if text, ok := v["content"].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func normalizeToolInput(values ...any) map[string]any {
	for _, value := range values {
		switch v := value.(type) {
		case map[string]any:
			return v
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				continue
			}
			var parsed any
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				if m, ok := parsed.(map[string]any); ok {
					return m
				}
			}
			return map[string]any{"input": trimmed}
		}
	}
	return map[string]any{}
}

func describeError(obj map[string]any) string {
	if text := firstText(obj["message"], obj["msg"], obj["reason"], obj["detail"], obj["description"]); text != "" {
		return text
	}
	if code, ok := obj["code"]; ok {
		return fmt.Sprintf("CodeBuddy upstream error (%v)", code)
	}
	return "CodeBuddy upstream error"
}

func isErrorStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "error" || s == "failed" || s == "failure"
}

func intFrom(values ...any) int {
	for _, value := range values {
		switch v := value.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case json.Number:
			n, err := v.Int64()
			if err == nil {
				return int(n)
			}
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err == nil {
				return n
			}
		}
	}
	return 0
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	case float64:
		return v != 0
	default:
		return false
	}
}
