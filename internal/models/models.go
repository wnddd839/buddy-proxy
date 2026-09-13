package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

const upstreamConfigPath = "/v3/config"

// consoleModelsPath (LOCAL patch): the requestable chat catalog, as opposed to the
// IDE-plugin catalog served by /v3/config.
const consoleModelsPath = "/console/enterprises/personal/models"

var fallbackPaths = []string{
	"/v2/models",
	"/v2/plugin/models",
	"/v1/models",
	"/api/v1/models",
}

type Model struct {
	ID                string         `json:"id"`
	ModelID           string         `json:"modelId"`
	UpstreamID        string         `json:"upstreamId"`
	Name              string         `json:"name"`
	DisplayName       string         `json:"displayName"`
	Object            string         `json:"object"`
	OwnedBy           string         `json:"owned_by"`
	SupportsTools     bool           `json:"supportsTools"`
	SupportsImages    bool           `json:"supportsImages"`
	SupportsReasoning bool           `json:"supportsReasoning"`
	OnlyReasoning     bool           `json:"onlyReasoning,omitempty"`
	Reasoning         map[string]any `json:"reasoning,omitempty"`
	Credits           string         `json:"credits,omitempty"`
	CreditMultiplier  *float64       `json:"creditMultiplier,omitempty"`
	Free              *bool          `json:"free,omitempty"`
	Description       string         `json:"description,omitempty"`
	MaxInputTokens    int            `json:"maxInputTokens,omitzero"`
	MaxOutputTokens   int            `json:"maxOutputTokens,omitzero"`
	MaxAllowedSize    int            `json:"maxAllowedSize,omitzero"`
	Verified          bool           `json:"verified"`
	Source            string         `json:"source"`
}

// ContextLength 优先用上游 maxInputTokens，缺省回落到 maxAllowedSize。
func (m Model) ContextLength() int {
	return strutil.FirstPositiveInt(m.MaxInputTokens, m.MaxAllowedSize)
}

type ListResult struct {
	OK               bool    `json:"ok"`
	Site             string  `json:"site"`
	Models           []Model `json:"models"`
	ModelsSource     string  `json:"modelsSource"`
	UpstreamEndpoint string  `json:"upstreamEndpoint,omitempty"`
	Message          string  `json:"message,omitempty"`
}

type ListOptions struct {
	Site                string
	Product             string
	BaseURL             string
	InternetEnvironment string
	BearerToken         string
	UserID              string
	EnterpriseID        string
	TenantID            string
	DepartmentFullName  string
	Domain              string
	APIEndpoint         string
	ChatCompletionsPath string
}

func PublicModelID(upstreamID string) string {
	cleaned := strings.TrimSpace(upstreamID)
	lower := strings.ToLower(cleaned)
	if cleaned == "" || lower == "default" || lower == "codebuddy" || lower == "codebuddy/" || lower == "codebuddy:" {
		return "auto"
	}
	// 前缀为 ASCII，len(prefix) 与原大小写输入对齐。
	for _, prefix := range []string{"codebuddy/", "codebuddy:"} {
		if _, ok := strings.CutPrefix(lower, prefix); ok {
			rest := strings.TrimSpace(cleaned[len(prefix):])
			if rest == "" || strings.EqualFold(rest, "default") {
				return "auto"
			}
			return rest
		}
	}
	return cleaned
}

func ToAdminModels(rows []map[string]any, source string) []Model {
	out := make([]Model, 0, len(rows))
	allVerified := source == "upstream" || source == "v3_config" || source == "probe"
	for _, row := range rows {
		upstreamID := strutil.First(fmt.Sprint(row["id"]), fmt.Sprint(row["modelId"]))
		if upstreamID == "" || upstreamID == "<nil>" {
			continue
		}
		name := strutil.First(fmt.Sprint(row["name"]), fmt.Sprint(row["displayName"]), upstreamID)
		credits := strings.TrimSpace(fmt.Sprint(row["credits"]))
		if credits == "<nil>" {
			credits = ""
		}
		model := Model{
			ID:                PublicModelID(upstreamID),
			ModelID:           upstreamID,
			UpstreamID:        upstreamID,
			Name:              name,
			DisplayName:       name,
			Object:            "model",
			OwnedBy:           "codebuddy",
			SupportsTools:     truthy(row["supportsTools"]) || truthy(row["supportsToolCall"]),
			SupportsImages:    truthy(row["supportsImages"]) || truthy(row["supportsImage"]),
			SupportsReasoning: truthy(row["supportsReasoning"]),
			OnlyReasoning:     truthy(row["onlyReasoning"]),
			Credits:           credits,
			Description:       strutil.First(fmt.Sprint(row["description"]), fmt.Sprint(row["descriptionZh"]), fmt.Sprint(row["descriptionEn"])),
			MaxInputTokens:    strutil.PositiveInt(row["maxInputTokens"]),
			MaxOutputTokens:   strutil.PositiveInt(row["maxOutputTokens"]),
			MaxAllowedSize:    strutil.PositiveInt(row["maxAllowedSize"]),
			Verified:          allVerified || upstreamID == "auto",
			Source:            source,
		}
		if reasoning, ok := row["reasoning"].(map[string]any); ok && len(reasoning) > 0 {
			model.Reasoning = reasoning
		}
		if model.Description == "<nil>" {
			model.Description = ""
		}
		if credits != "" {
			if mult, ok := provider.ParseCreditMultiplier(credits); ok {
				model.CreditMultiplier = &mult
				free := mult == 0
				model.Free = &free
			}
		}
		out = append(out, model)
	}
	return out
}

func (c *Lister) List(ctx context.Context, client *provider.Client, opts ListOptions) ListResult {
	site := config.NormalizeSite(opts.Site)
	if strings.TrimSpace(opts.BearerToken) == "" {
		return ListResult{
			OK:           false,
			Site:         site,
			Models:       ToAdminModels([]map[string]any{{"id": "auto", "name": "Auto"}}, "fallback"),
			ModelsSource: "no_credentials",
			Message:      "Complete CodeBuddy OAuth login before listing models.",
		}
	}

	chatOpts := provider.ChatOptions{
		BearerToken:         opts.BearerToken,
		UserID:              opts.UserID,
		BaseURL:             opts.BaseURL,
		Site:                site,
		Product:             config.NormalizeProduct(opts.Product),
		InternetEnvironment: opts.InternetEnvironment,
		APIEndpoint:         opts.APIEndpoint,
		ChatCompletionsPath: opts.ChatCompletionsPath,
		Domain:              opts.Domain,
		EnterpriseID:        opts.EnterpriseID,
		TenantID:            opts.TenantID,
		DepartmentFullName:  opts.DepartmentFullName,
	}

	// LOCAL patch: prefer the console catalog. /v3/config is the IDE-plugin catalog and
	// mixes in completion/aux models (codewise-*, completion-gf, *-taco-completion) that
	// reject chat with 11102, while omitting actually-chat-able models (hy4-preview,
	// glm-5.3, kimi-k3-1...). The console catalog is the requestable set and carries
	// `credits`. Fall back to the original path on any failure.
	if result, err := c.fetchConsole(ctx, client, chatOpts); err == nil && len(result.Models) > 0 {
		return ListResult{
			OK:               true,
			Site:             site,
			Models:           ToAdminModels(result.Models, "console"),
			ModelsSource:     "console",
			UpstreamEndpoint: consoleModelsPath,
			Message:          fmt.Sprintf("Loaded %d model(s) from %s.", len(result.Models), consoleModelsPath),
		}
	}

	if result, err := c.fetchV3(ctx, client, chatOpts); err == nil && len(result.Models) > 0 {
		return ListResult{
			OK:               true,
			Site:             site,
			Models:           ToAdminModels(result.Models, "v3_config"),
			ModelsSource:     "v3_config",
			UpstreamEndpoint: upstreamConfigPath,
			Message:          fmt.Sprintf("Loaded %d model(s) from %s.", len(result.Models), upstreamConfigPath),
		}
	}

	base := provider.ResolveProtocolDirectBaseURL(chatOpts)
	headers, _ := client.BuildProtocolDirectHeaders(chatOpts)
	headers.Set("Accept", "application/json")
	for _, path := range fallbackPaths {
		rows, err := c.fetchJSONModels(ctx, client.HTTP, base+path, headers)
		if err != nil || len(rows) == 0 {
			continue
		}
		return ListResult{
			OK:               true,
			Site:             site,
			Models:           ToAdminModels(rows, "upstream"),
			ModelsSource:     "upstream",
			UpstreamEndpoint: path,
		}
	}

	return ListResult{
		OK:           true,
		Site:         site,
		Models:       ToAdminModels([]map[string]any{{"id": "auto", "name": "Auto"}}, "site_catalog"),
		ModelsSource: "site_catalog",
		Message:      "CodeBuddy model config unavailable; showing auto only.",
	}
}

type Lister struct{}

func NewLister() *Lister { return &Lister{} }

type fetchResult struct {
	Models []map[string]any
}

func v3ConfigCandidateBases(opts provider.ChatOptions) []string {
	primary := provider.ResolveProtocolDirectBaseURL(opts)
	if config.NormalizeProduct(opts.Product) == "workbuddy" || provider.RegionOf(opts) != "global" {
		return []string{primary}
	}
	// 国际站：www.codebuddy.ai/v3/config 有 Gemini/GPT 等，但不含 hy4；
	// copilot.tencent.com/v3/config 用同一 token 可读且含 hy4/glm 等，需合并。
	if strings.Contains(primary, "copilot.tencent.com") {
		return []string{primary}
	}
	return []string{primary, "https://copilot.tencent.com"}
}

func mergeModelsByID(batches ...[]map[string]any) []map[string]any {
	seen := make(map[string]struct{})
	out := make([]map[string]any, 0)
	for _, rows := range batches {
		for _, row := range rows {
			id := modelRowID(row)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, row)
		}
	}
	return out
}

func modelRowID(row map[string]any) string {
	id := strings.TrimSpace(fmt.Sprint(row["id"]))
	if id == "" || id == "<nil>" {
		id = strings.TrimSpace(fmt.Sprint(row["modelId"]))
	}
	if id == "" || id == "<nil>" {
		return ""
	}
	return id
}

func (c *Lister) fetchV3(ctx context.Context, client *provider.Client, opts provider.ChatOptions) (fetchResult, error) {
	candidates := v3ConfigCandidateBases(opts)
	headers, _ := client.BuildProtocolDirectHeaders(opts)
	headers.Set("Accept", "application/json")
	var (
		batches [][]map[string]any
		lastErr error
	)
	for _, base := range candidates {
		rows, err := c.fetchJSONModels(ctx, client.HTTP, provider.NormalizeBaseURL(base)+upstreamConfigPath, headers)
		if err != nil {
			lastErr = err
			continue
		}
		if len(rows) == 0 {
			lastErr = fmt.Errorf("empty models")
			continue
		}
		batches = append(batches, rows)
	}
	merged := mergeModelsByID(batches...)
	if len(merged) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("v3 config unavailable")
		}
		return fetchResult{}, lastErr
	}
	if config.NormalizeProduct(opts.Product) == "workbuddy" {
		return fetchResult{Models: merged}, nil
	}
	ideRows := c.fetchIDECatalog(ctx, client, opts, candidates)
	return fetchResult{Models: enrichModelsWithIDEReasoning(merged, ideRows)}, nil
}

// fetchConsole (LOCAL patch) loads the console catalog: the set of models that
// actually accept /v2/chat/completions, with credits metadata.
func (c *Lister) fetchConsole(ctx context.Context, client *provider.Client, opts provider.ChatOptions) (fetchResult, error) {
	bases := v3ConfigCandidateBases(opts)
	hosts := make([]string, 0, len(bases)+1)
	seen := make(map[string]struct{}, len(bases)+1)
	add := func(raw string) {
		base := provider.NormalizeBaseURL(raw)
		if base == "" {
			return
		}
		if _, ok := seen[base]; ok {
			return
		}
		seen[base] = struct{}{}
		hosts = append(hosts, base)
	}
	for _, base := range bases {
		add(base)
	}
	// The console route is served by the API host; workbuddy.cn may not expose it.
	add("https://copilot.tencent.com")

	ideOpts := opts
	if ideOpts.ExtraHeaders == nil {
		ideOpts.ExtraHeaders = map[string]string{}
	} else {
		cloned := make(map[string]string, len(opts.ExtraHeaders)+4)
		for k, v := range opts.ExtraHeaders {
			cloned[k] = v
		}
		ideOpts.ExtraHeaders = cloned
	}
	// The console endpoint rejects the WorkBuddy VSCode identity (400); use the CLI one.
	ideOpts.ExtraHeaders["X-IDE-Type"] = "CLI"
	ideOpts.ExtraHeaders["X-IDE-Name"] = "CLI"
	ideOpts.ExtraHeaders["X-IDE-Version"] = config.DefaultIDEVersion
	ideOpts.ExtraHeaders["User-Agent"] = fmt.Sprintf("CLI/%s CodeBuddy/%s", config.DefaultIDEVersion, config.DefaultIDEVersion)
	delete(ideOpts.ExtraHeaders, "X-Product-Version")
	delete(ideOpts.ExtraHeaders, "X-Env-ID")

	headers, _ := client.BuildProtocolDirectHeaders(ideOpts)
	headers.Set("Accept", "application/json")

	var lastErr error
	for _, base := range hosts {
		rows, err := c.fetchJSONModels(ctx, client.HTTP, base+consoleModelsPath, headers)
		if err != nil {
			lastErr = err
			continue
		}
		if len(rows) == 0 {
			lastErr = fmt.Errorf("empty models")
			continue
		}
		return fetchResult{Models: dedupeByPublicID(enrichConsoleReasoning(rows))}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("console catalog unavailable")
	}
	return fetchResult{}, lastErr
}

// dedupeByPublicID drops rows whose public ID already appeared. The console catalog
// contains both "auto" and "default"; PublicModelID maps "default" to "auto", which
// would otherwise surface two "auto" entries. First row wins.
func dedupeByPublicID(rows []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(rows))
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		id := PublicModelID(modelRowID(row))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, row)
	}
	return out
}

// enrichConsoleReasoning fills canDisableThinking when absent, matching
// enrichModelsWithIDEReasoning so clients do not reserve thinking budget
// unnecessarily.
func enrichConsoleReasoning(rows []map[string]any) []map[string]any {
	for _, row := range rows {
		ensureCanDisableThinking(row)
	}
	return rows
}

func (c *Lister) fetchIDECatalog(ctx context.Context, client *provider.Client, opts provider.ChatOptions, bases []string) []map[string]any {
	if len(bases) == 0 {
		return nil
	}
	ideOpts := opts
	if ideOpts.ExtraHeaders == nil {
		ideOpts.ExtraHeaders = map[string]string{}
	} else {
		cloned := make(map[string]string, len(opts.ExtraHeaders)+8)
		for k, v := range opts.ExtraHeaders {
			cloned[k] = v
		}
		ideOpts.ExtraHeaders = cloned
	}
	domain := "www.codebuddy.cn"
	if provider.RegionOf(opts) == "global" {
		domain = "www.codebuddy.ai"
	}
	ideOpts.ExtraHeaders["X-IDE-Type"] = "VSCode"
	ideOpts.ExtraHeaders["X-IDE-Name"] = "VSCode"
	ideOpts.ExtraHeaders["X-IDE-Version"] = "1.119.0"
	ideOpts.ExtraHeaders["X-Product-Version"] = "4.9.29177644"
	ideOpts.ExtraHeaders["X-Env-ID"] = "production"
	ideOpts.ExtraHeaders["User-Agent"] = "VSCode/1.119.0 CodeBuddy/4.9.29177644"
	ideOpts.ExtraHeaders["X-Domain"] = domain
	headers, _ := client.BuildProtocolDirectHeaders(ideOpts)
	headers.Set("Accept", "application/json")
	rows, err := c.fetchJSONModels(ctx, client.HTTP, provider.NormalizeBaseURL(bases[0])+upstreamConfigPath, headers)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return rows
}

// enrichModelsWithIDEReasoning copies WorkBuddy/IDE /v3/config reasoning fields onto the
// CLI catalog. CLI omits canDisableThinking / supportedEfforts, which makes clients treat
// DeepSeek Flash as "thinking always on" and reserve ~30% of the 1M window.
func enrichModelsWithIDEReasoning(cli, ide []map[string]any) []map[string]any {
	ideByID := make(map[string]map[string]any, len(ide))
	for _, row := range ide {
		if id := modelRowID(row); id != "" {
			ideByID[id] = row
		}
	}
	for _, row := range cli {
		if src, ok := ideByID[modelRowID(row)]; ok {
			copyReasoningMetadata(row, src)
		}
		ensureCanDisableThinking(row)
	}
	return cli
}

func copyReasoningMetadata(dst, src map[string]any) {
	srcReasoning, _ := src["reasoning"].(map[string]any)
	if len(srcReasoning) == 0 {
		return
	}
	dstReasoning, _ := dst["reasoning"].(map[string]any)
	if dstReasoning == nil {
		cloned := make(map[string]any, len(srcReasoning))
		for k, v := range srcReasoning {
			cloned[k] = v
		}
		dst["reasoning"] = cloned
		return
	}
	for _, key := range []string{"canDisableThinking", "supportedEfforts", "defaultEffort"} {
		if _, exists := dstReasoning[key]; exists {
			continue
		}
		if v, ok := srcReasoning[key]; ok {
			dstReasoning[key] = v
		}
	}
}

func ensureCanDisableThinking(row map[string]any) {
	reasoning, _ := row["reasoning"].(map[string]any)
	if len(reasoning) == 0 {
		return
	}
	if _, exists := reasoning["canDisableThinking"]; !exists {
		reasoning["canDisableThinking"] = true
	}
}

func (c *Lister) fetchJSONModels(ctx context.Context, httpClient *http.Client, endpoint string, headers http.Header) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return provider.NormalizeModels(payload), nil
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
