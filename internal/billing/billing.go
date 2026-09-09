package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

type Credits struct {
	Remaining      *float64  `json:"remaining"`
	Total          *float64  `json:"total"`
	Used           *float64  `json:"used"`
	Percent        *int      `json:"percent"`
	Unlimited      bool      `json:"unlimited"`
	Unit           string    `json:"unit"`
	Packages       []Package `json:"packages"`
	Label          string    `json:"label"`
	Display        string    `json:"display"`
	PackageName    string    `json:"packageName,omitempty"`
	CycleStartTime string    `json:"cycleStartTime,omitempty"`
	CycleEndTime   string    `json:"cycleEndTime,omitempty"`
}

type Package struct {
	PackageCode          string   `json:"packageCode"`
	PackageName          string   `json:"packageName"`
	ResourceID           string   `json:"resourceId"`
	Status               any      `json:"status"`
	CapacityType         int      `json:"capacityType"`
	Unit                 string   `json:"unit"`
	Remaining            *float64 `json:"remaining"`
	Total                *float64 `json:"total"`
	Used                 *float64 `json:"used"`
	CycleStartTime       string   `json:"cycleStartTime,omitempty"`
	CycleEndTime         string   `json:"cycleEndTime,omitempty"`
	SlicePeriodEndTime   string   `json:"slicePeriodEndTime,omitempty"`
}

type Notify struct {
	DosageNotifyCode int    `json:"dosageNotifyCode"`
	DosageNotifyZh   string `json:"dosageNotifyZh,omitempty"`
	DosageNotifyEn   string `json:"dosageNotifyEn,omitempty"`
	SkipURL          string `json:"skipUrl,omitempty"`
	Level            string `json:"level"`
	Label            string `json:"label"`
	Hint             string `json:"hint"`
}

type UsageResult struct {
	OK               bool           `json:"ok"`
	Provider         string         `json:"provider"`
	AccountID        string         `json:"accountId"`
	Site             string         `json:"site"`
	Endpoint         string         `json:"endpoint"`
	OfficialUsageURL string         `json:"officialUsageUrl"`
	Note             string         `json:"note"`
	Credits          Credits        `json:"credits"`
	Notify           *Notify        `json:"notify"`
	Raw              map[string]any `json:"raw,omitempty"`
}

func OfficialUsageURL(site string) string {
	if config.NormalizeSite(site) == "global" {
		return "https://www.codebuddy.ai/profile/plan"
	}
	return "https://www.codebuddy.cn/profile/plan"
}

func BillingBaseURL(site string) string {
	if configured := strings.TrimSpace(os.Getenv("CODEBUDDY_BILLING_BASE_URL")); configured != "" {
		return provider.NormalizeBaseURL(configured)
	}
	if config.NormalizeSite(site) == "global" {
		return "https://www.codebuddy.ai"
	}
	return "https://www.codebuddy.cn"
}

func FetchAccountUsage(ctx context.Context, client *provider.Client, account accounts.Account, cfg config.Config) (UsageResult, error) {
	site := strutil.First(account.Site, cfg.Site, "domestic")
	billingBase := BillingBaseURL(site)
	protocolBase := provider.ResolveProtocolDirectBaseURL(provider.ChatOptions{
		Site:                site,
		InternetEnvironment: strutil.First(account.InternetEnvironment, cfg.InternetEnvironment),
		BaseURL:             strutil.First(account.BaseURL, cfg.BaseURL),
		APIEndpoint:         strutil.First(account.APIEndpoint, cfg.APIEndpoint),
	})

	bearer := strings.TrimSpace(account.BearerToken)
	if bearer == "" {
		bearer = strings.TrimSpace(account.APIKey)
	}
	if bearer == "" {
		return UsageResult{}, fmt.Errorf("CodeBuddy account has no credentials: %s", account.ID)
	}

	billingHeaders := http.Header{}
	billingHeaders.Set("Accept", "application/json")
	billingHeaders.Set("Content-Type", "application/json")
	billingHeaders.Set("Authorization", "Bearer "+bearer)
	billingHeaders.Set("X-Requested-With", "XMLHttpRequest")
	if account.EnterpriseID != "" {
		billingHeaders.Set("X-Enterprise-Id", account.EnterpriseID)
	}

	resourceEndpoint := strings.TrimRight(billingBase, "/") + "/billing/meter/get-user-resource"
	resourceBody := map[string]any{
		"PageNumber":      1,
		"PageSize":        200,
		"ProductCode":     "p_tcaca",
		"Status":          []int{0, 3},
		"OnlyValidPeriod": true,
	}
	resourcePayload, err := postJSON(ctx, client.HTTP, resourceEndpoint, billingHeaders, resourceBody)
	if err != nil {
		return UsageResult{}, fmt.Errorf("CodeBuddy credits query failed: %w", err)
	}
	if code, ok := asNumber(resourcePayload["code"]); ok && code != 0 {
		msg := strutil.First(fmt.Sprint(resourcePayload["msg"]), fmt.Sprint(resourcePayload["message"]), fmt.Sprint(resourcePayload["error"]), "unknown error")
		return UsageResult{}, fmt.Errorf("CodeBuddy credits query failed: %s", strutil.Truncate(msg, 240))
	}

	accountsRaw := digSlice(resourcePayload, "data", "Response", "Data", "Accounts")
	if accountsRaw == nil {
		accountsRaw = digSlice(resourcePayload, "data", "Accounts")
	}
	if accountsRaw == nil {
		accountsRaw = digSlice(resourcePayload, "Accounts")
	}
	credits := summarizeResourceAccounts(accountsRaw)

	var notify *Notify
	notifyEndpoint := strings.TrimRight(protocolBase, "/") + "/v2/billing/meter/get-dosage-notify"
	notifyHeaders := client.BuildProtocolDirectHeaders(provider.ChatOptions{
		Site:                site,
		InternetEnvironment: strutil.First(account.InternetEnvironment, cfg.InternetEnvironment),
		BaseURL:             strutil.First(account.BaseURL, cfg.BaseURL),
		APIEndpoint:         strutil.First(account.APIEndpoint, cfg.APIEndpoint),
		BearerToken:         bearer,
		UserID:              strutil.First(account.AuthStatus.UserID, "anonymous"),
		EnterpriseID:        account.EnterpriseID,
		TenantID:            account.TenantID,
		DepartmentFullName:  account.DepartmentFullName,
		Domain:              account.Domain,
	})
	if notifyPayload, notifyErr := postJSON(ctx, client.HTTP, notifyEndpoint, notifyHeaders, map[string]any{}); notifyErr == nil {
		if code, ok := asNumber(notifyPayload["code"]); !ok || code == 0 {
			data, _ := notifyPayload["data"].(map[string]any)
			if data == nil {
				data = notifyPayload
			}
			n := mapDosageNotify(data)
			notify = &n
		}
	}

	remainingLabel := "-"
	totalLabel := "-"
	if credits.Unlimited {
		remainingLabel = "不限量"
		totalLabel = "不限量"
	} else {
		if credits.Remaining != nil {
			remainingLabel = formatNumber(*credits.Remaining)
		}
		if credits.Total != nil {
			totalLabel = formatNumber(*credits.Total)
		}
	}
	primary := Package{}
	if len(credits.Packages) > 0 {
		primary = credits.Packages[0]
	}
	credits.Label = remainingLabel + " / " + totalLabel
	if credits.Unlimited {
		credits.Display = "剩余 不限量 Credits"
	} else {
		credits.Display = "剩余 " + remainingLabel + " / " + totalLabel + " Credits"
	}
	credits.PackageName = primary.PackageName
	credits.CycleStartTime = primary.CycleStartTime
	credits.CycleEndTime = primary.CycleEndTime

	raw := map[string]any{}
	if data, ok := resourcePayload["data"]; ok {
		raw["resource"] = data
	} else {
		raw["resource"] = resourcePayload
	}
	if notify != nil {
		raw["notify"] = notify
	}

	return UsageResult{
		OK:               true,
		Provider:         "codebuddy",
		AccountID:        account.ID,
		Site:             config.NormalizeSite(site),
		Endpoint:         resourceEndpoint,
		OfficialUsageURL: OfficialUsageURL(site),
		Note:             "剩余 Credits 来自官网套餐接口 /billing/meter/get-user-resource。",
		Credits:          credits,
		Notify:           notify,
		Raw:              raw,
	}, nil
}

func summarizeResourceAccounts(items []any) Credits {
	packages := make([]Package, 0, len(items))
	var remaining, total, used float64
	hasUnlimited := false

	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		capacityType := int(asNumberOr(item["CapacityType"], item["capacityType"], 0))
		var left, size, usedAmount *float64
		slicePeriodEnd := ""
		if capacityType == 4 {
			if details, ok := item["SlicePeriodUsageDetails"].([]any); ok && len(details) > 0 {
				if slice, ok := details[0].(map[string]any); ok {
					left = toFinitePtr(firstAny(slice, "SlicePeriodCapacityRemainPrecise", "SlicePeriodCapacityRemain"))
					size = toFinitePtr(firstAny(slice, "SlicePeriodCapacitySizePrecise", "SlicePeriodCapacitySize"))
					usedAmount = toFinitePtr(firstAny(slice, "SlicePeriodCapacityUsedPrecise", "SlicePeriodCapacityUsed"))
					slicePeriodEnd = strings.TrimSpace(fmt.Sprint(firstAny(
						slice,
						"SlicePeriodEndTime", "slicePeriodEndTime",
						"PeriodEndTime", "periodEndTime",
						"EndTime", "endTime",
					)))
					if slicePeriodEnd == "<nil>" {
						slicePeriodEnd = ""
					}
				}
			}
		}
		if left == nil && size == nil && usedAmount == nil {
			left = toFinitePtr(firstAny(item, "CycleCapacityRemainPrecise", "CycleCapacityRemain", "CapacityRemainPrecise", "CapacityRemain"))
			size = toFinitePtr(firstAny(item, "CycleCapacitySizePrecise", "CycleCapacitySize", "CapacitySizePrecise", "CapacitySize"))
			usedAmount = toFinitePtr(firstAny(item, "CycleCapacityUsedPrecise", "CycleCapacityUsed", "CapacityUsedPrecise", "CapacityUsed"))
		}
		if (size != nil && *size == -1) || (left != nil && *left == -1) {
			hasUnlimited = true
		}
		if left != nil && *left >= 0 {
			remaining += *left
		}
		if size != nil && *size >= 0 {
			total += *size
		}
		if usedAmount != nil && *usedAmount >= 0 {
			used += *usedAmount
		} else if left != nil && size != nil && *size >= 0 && *left >= 0 {
			used += math.Max(0, *size-*left)
		}
		packages = append(packages, Package{
			PackageCode:    strings.TrimSpace(fmt.Sprint(firstAny(item, "PackageCode", "packageCode"))),
			PackageName:    strings.TrimSpace(fmt.Sprint(firstAny(item, "PackageName", "packageName"))),
			ResourceID:     strings.TrimSpace(fmt.Sprint(firstAny(item, "ResourceId", "resourceId"))),
			Status:         firstAny(item, "Status", "status"),
			CapacityType:   capacityType,
			Unit:           strutil.First(strings.TrimSpace(fmt.Sprint(firstAny(item, "CapacityUnit", "OriginUnit"))), "credits"),
			Remaining:      left,
			Total:          size,
			Used:           usedAmount,
			CycleStartTime:     strings.TrimSpace(fmt.Sprint(firstAny(item, "CycleStartTime", "cycleStartTime"))),
			CycleEndTime:       strings.TrimSpace(fmt.Sprint(firstAny(item, "CycleEndTime", "cycleEndTime"))),
			SlicePeriodEndTime: slicePeriodEnd,
		})
	}

	if used <= 0 && total > 0 && remaining >= 0 {
		used = math.Max(0, total-remaining)
	}
	out := Credits{
		Unlimited: hasUnlimited,
		Unit:      "credits",
		Packages:  packages,
	}
	if !hasUnlimited {
		out.Remaining = floatPtr(remaining)
		out.Total = floatPtr(total)
		out.Used = floatPtr(used)
		if total > 0 {
			pct := int(math.Min(100, math.Round((used/total)*100)))
			out.Percent = &pct
		}
	}
	return out
}

func mapDosageNotify(data map[string]any) Notify {
	code := int(asNumberOr(data["dosageNotifyCode"], data["code"], 0))
	preset := map[int]Notify{
		0: {Level: "ok", Label: "用量正常", Hint: "当前未触发用量告警"},
		1: {Level: "warn", Label: "用量提醒", Hint: "接近额度，建议关注官网套餐与用量"},
		2: {Level: "bad", Label: "用量不足", Hint: "额度可能不足，请到官网查看套餐与用量"},
		3: {Level: "bad", Label: "用量耗尽", Hint: "额度可能已耗尽，请到官网充值或升级"},
	}
	mapped, ok := preset[code]
	if !ok {
		mapped = Notify{
			Level: "warn",
			Label: fmt.Sprintf("通知码 %d", code),
			Hint:  strutil.First(fmt.Sprint(data["dosageNotifyZh"]), fmt.Sprint(data["dosageNotifyEn"]), "已收到用量通知"),
		}
		if code == 0 {
			mapped.Level = "ok"
		}
	}
	mapped.DosageNotifyCode = code
	mapped.DosageNotifyZh = strings.TrimSpace(fmt.Sprint(data["dosageNotifyZh"]))
	mapped.DosageNotifyEn = strings.TrimSpace(fmt.Sprint(data["dosageNotifyEn"]))
	mapped.SkipURL = strings.TrimSpace(fmt.Sprint(data["skipUrl"]))
	return mapped
}

func postJSON(ctx context.Context, httpClient *http.Client, endpoint string, headers http.Header, body any) (map[string]any, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	text, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	var payload map[string]any
	if len(text) > 0 {
		if err := json.Unmarshal(text, &payload); err != nil {
			payload = map[string]any{"raw": string(text)}
		}
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := strutil.First(fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"]), fmt.Sprint(payload["error"]), fmt.Sprintf("HTTP %d", res.StatusCode))
		return payload, fmt.Errorf("%s", strutil.Truncate(msg, 240))
	}
	return payload, nil
}

func digSlice(root map[string]any, keys ...string) []any {
	var cur any = root
	for _, key := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[key]
	}
	out, _ := cur.([]any)
	return out
}

func firstAny(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok && v != nil {
			return v
		}
	}
	return nil
}

func toFinitePtr(value any) *float64 {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" || v == "-" {
			return nil
		}
	}
	num, ok := asNumber(value)
	if !ok || math.IsNaN(num) || math.IsInf(num, 0) {
		return nil
	}
	return &num
}

func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

func asNumberOr(values ...any) float64 {
	for _, value := range values[:len(values)-1] {
		if n, ok := asNumber(value); ok {
			return n
		}
	}
	if n, ok := asNumber(values[len(values)-1]); ok {
		return n
	}
	return 0
}

func floatPtr(v float64) *float64 { return new(v) }

func formatNumber(v float64) string {
	if math.Mod(v, 1) == 0 {
		return fmt.Sprintf("%.0f", v)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}
