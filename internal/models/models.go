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
		InternetEnvironment: opts.InternetEnvironment,
		APIEndpoint:         opts.APIEndpoint,
		ChatCompletionsPath: opts.ChatCompletionsPath,
		Domain:              opts.Domain,
		EnterpriseID:        opts.EnterpriseID,
		TenantID:            opts.TenantID,
		DepartmentFullName:  opts.DepartmentFullName,
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
	headers := client.BuildProtocolDirectHeaders(chatOpts)
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
	if provider.RegionOf(opts) != "global" {
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
	headers := client.BuildProtocolDirectHeaders(opts)
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
	if len(merged) > 0 {
		return fetchResult{Models: merged}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("v3 config unavailable")
	}
	return fetchResult{}, lastErr
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
