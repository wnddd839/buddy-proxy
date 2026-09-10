package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

const (
	checkinActivityPath    = "/v2/billing/meter/checkin-activity-status"
	checkinActionPath      = "/v2/billing/meter/daily-checkin"
	checkinAlreadyCode     = 10001
	checkinRequestTimeout  = 20 * time.Second
	checkinBatchMaxTimeout = 5 * time.Minute
)

type CheckinActivity struct {
	Active         bool `json:"active,omitzero"`
	TodayCheckedIn bool `json:"todayCheckedIn,omitzero"`
	StreakDays     int  `json:"streakDays,omitzero"`
	DailyCredit    int  `json:"dailyCredit,omitzero"`
	TodayCredit    int  `json:"todayCredit,omitzero"`
	IsStreakDay    bool `json:"isStreakDay,omitzero"`
}

type CheckinResult struct {
	OK               bool   `json:"ok,omitzero"`
	AccountID        string `json:"accountId"`
	Label            string `json:"label,omitempty"`
	Site             string `json:"site"`
	Supported        bool   `json:"supported,omitzero"`
	AlreadyCheckedIn bool   `json:"alreadyCheckedIn,omitzero"`
	RewardCredits    int    `json:"rewardCredits,omitzero"`
	StreakDays       int    `json:"streakDays,omitzero"`
	IsStreakDay      bool   `json:"isStreakDay,omitzero"`
	Message          string `json:"message"`
	Endpoint         string `json:"endpoint,omitempty"`
}

type CheckinBatchResult struct {
	OK       bool            `json:"ok,omitzero"`
	PoolSite string          `json:"poolSite"`
	Results  []CheckinResult `json:"results"`
	Summary  CheckinSummary  `json:"summary"`
	Note     string          `json:"note,omitempty"`
}

type CheckinSummary struct {
	Total       int `json:"total,omitzero"`
	CheckedIn   int `json:"checkedIn,omitzero"`
	AlreadyDone int `json:"alreadyDone,omitzero"`
	Skipped     int `json:"skipped,omitzero"`
	Unsupported int `json:"unsupported,omitzero"`
	Failed      int `json:"failed,omitzero"`
}

func CheckinNoteForSite(site string) string {
	if config.NormalizeSite(site) == "global" {
		return "国际站若无签到活动将自动跳过；有活动的账号仍会尝试领取。"
	}
	return "国内站每日签到约 100 积分，连续第 7 天可达 1000 积分。"
}

// siteLabelForEmptyNote is the Chinese label used in "no eligible account" notes.
func siteLabelForEmptyNote(site string) string {
	if config.NormalizeSite(site) == "global" {
		return "国际"
	}
	return "国内"
}

func FetchCheckinActivity(ctx context.Context, client *provider.Client, account accounts.Account, cfg config.Config) (CheckinActivity, string, error) {
	ctx, cancel := withCheckinTimeout(ctx)
	defer cancel()

	site := strutil.First(account.Site, cfg.Site, "domestic")
	endpoint, headers, err := checkinRequest(client, account, cfg, site, checkinActivityPath)
	if err != nil {
		return CheckinActivity{}, endpoint, err
	}
	payload, err := postJSON(ctx, client.HTTP, endpoint, headers, map[string]any{})
	if err != nil {
		return CheckinActivity{}, endpoint, fmt.Errorf("checkin status failed: %w", err)
	}
	if code, ok := asNumber(payload["code"]); ok && code != 0 {
		msg := strutil.First(fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"]), "unknown error")
		return CheckinActivity{}, endpoint, fmt.Errorf("checkin status failed: %s", strutil.Truncate(msg, 240))
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = payload
	}
	return mapCheckinActivity(data), endpoint, nil
}

func DailyCheckinForAccount(ctx context.Context, client *provider.Client, account accounts.Account, cfg config.Config) CheckinResult {
	ctx, cancel := withCheckinTimeout(ctx)
	defer cancel()

	site := strutil.First(account.Site, cfg.Site, "domestic")
	normalizedSite := config.NormalizeSite(site)
	result := CheckinResult{
		AccountID: account.ID,
		Label:     account.Label,
		Site:      normalizedSite,
	}

	activity, statusEndpoint, err := FetchCheckinActivity(ctx, client, account, cfg)
	if err != nil {
		result.Supported = true
		result.Message = err.Error()
		result.Endpoint = statusEndpoint
		return result
	}
	result.Endpoint = statusEndpoint

	if !activity.Active {
		result.Supported = false
		result.Message = unsupportedCheckinMessage()
		return result
	}
	result.Supported = true

	if activity.TodayCheckedIn {
		result.OK = true
		result.AlreadyCheckedIn = true
		result.StreakDays = activity.StreakDays
		result.RewardCredits = activity.TodayCredit
		result.IsStreakDay = activity.IsStreakDay
		result.Message = "今日已签到"
		return result
	}

	actionEndpoint, headers, err := checkinRequest(client, account, cfg, site, checkinActionPath)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	payload, err := postJSON(ctx, client.HTTP, actionEndpoint, headers, map[string]any{})
	if err != nil {
		if isCheckinAlreadyDone(payload) {
			result.OK = true
			result.AlreadyCheckedIn = true
			result.Message = checkinMessage(payload)
			return result
		}
		result.Message = err.Error()
		result.Endpoint = actionEndpoint
		return result
	}

	code, hasCode := asNumber(payload["code"])
	if hasCode && code != 0 {
		if int(code) == checkinAlreadyCode {
			result.OK = true
			result.AlreadyCheckedIn = true
			result.Message = checkinMessage(payload)
			return result
		}
		msg := checkinMessage(payload)
		if strings.Contains(msg, "未开启") || strings.Contains(msg, "过期") {
			result.Supported = false
		}
		result.Message = msg
		return result
	}

	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = payload
	}
	result.OK = true
	result.RewardCredits = int(asNumberOr(data["credit"], data["today_credit"], data["todayCredit"], 0))
	result.StreakDays = int(asNumberOr(data["streak_days"], data["streakDays"], 0))
	result.IsStreakDay = triToBool(coalesceTriBool(data, []string{"is_streak_day", "isStreakDay"}, false))
	if result.RewardCredits > 0 {
		result.Message = fmt.Sprintf("签到成功，+%d 积分", result.RewardCredits)
	} else {
		result.Message = "签到成功"
	}
	return result
}

func RunPoolCheckin(ctx context.Context, client *provider.Client, store accounts.Store, cfg config.Config, poolSite string) CheckinBatchResult {
	poolSite = config.NormalizeSite(poolSite)
	if poolSite == "" {
		poolSite = config.NormalizeSite(cfg.Site)
	}
	if poolSite == "" {
		poolSite = "global"
	}

	eligible := eligibleCheckinAccounts(store, poolSite)
	out := CheckinBatchResult{
		OK:       true,
		PoolSite: poolSite,
		Note:     CheckinNoteForSite(poolSite),
	}

	batchCtx, batchCancel := context.WithTimeout(ctx, checkinBatchTimeout(len(eligible)))
	defer batchCancel()

	for _, account := range eligible {
		if batchCtx.Err() != nil {
			// Batch deadline exhausted: these accounts were never attempted.
			// Counted as skipped (not failed), but the batch is still a failure.
			out.addUnattempted(account, "批次超时，未执行签到")
			out.OK = false
			continue
		}
		reqCtx, cancel := context.WithTimeout(batchCtx, checkinRequestTimeout)
		item := DailyCheckinForAccount(reqCtx, client, account, cfg)
		cancel()
		out.add(item)
	}

	if out.Summary.Total == 0 {
		out.Note = siteLabelForEmptyNote(poolSite) + "号池没有可签到的启用账号。"
	}
	return out
}

// add appends a per-account result and folds it into the batch summary.
func (b *CheckinBatchResult) add(item CheckinResult) {
	b.Summary.Total++
	b.Results = append(b.Results, item)
	switch classifyCheckinResult(item) {
	case checkinAlreadyDone:
		b.Summary.AlreadyDone++
	case checkinCheckedIn:
		b.Summary.CheckedIn++
	case checkinUnsupported:
		b.Summary.Unsupported++
	case checkinSkipped:
		b.Summary.Skipped++
	default:
		b.Summary.Failed++
		b.OK = false
	}
}

// addUnattempted records an account that was never called because the batch
// deadline ran out. Skipped rather than failed: nothing actually broke.
func (b *CheckinBatchResult) addUnattempted(account accounts.Account, message string) {
	b.Summary.Total++
	b.Summary.Skipped++
	b.Results = append(b.Results, CheckinResult{
		AccountID: account.ID,
		Label:     account.Label,
		Site:      config.NormalizeSite(account.Site),
		Supported: true,
		Message:   message,
	})
}

func eligibleCheckinAccounts(store accounts.Store, poolSite string) []accounts.Account {
	poolSite = config.NormalizeSite(poolSite)
	out := make([]accounts.Account, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		if !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		if config.NormalizeSite(account.Site) != poolSite {
			continue
		}
		out = append(out, account)
	}
	return out
}

func checkinBatchTimeout(accountCount int) time.Duration {
	if accountCount <= 0 {
		return 0
	}
	return min(checkinRequestTimeout*time.Duration(accountCount), checkinBatchMaxTimeout)
}

func checkinRequest(client *provider.Client, account accounts.Account, cfg config.Config, site string, path string) (string, http.Header, error) {
	bearer := strings.TrimSpace(account.BearerToken)
	if bearer == "" {
		bearer = strings.TrimSpace(account.APIKey)
	}
	if bearer == "" {
		return "", nil, fmt.Errorf("account has no credentials: %s", account.ID)
	}
	userID := strutil.First(account.AuthStatus.UserID, account.AuthStatus.UserName)
	if userID == "" {
		return "", nil, fmt.Errorf("account missing user id: %s", account.ID)
	}

	chatOpts := provider.ChatOptions{
		Site:                site,
		InternetEnvironment: strutil.First(account.InternetEnvironment, cfg.InternetEnvironment),
		BaseURL:             strutil.First(account.BaseURL, cfg.BaseURL),
		// Account-level only: process cfg.APIEndpoint may target the wrong region (see gateway).
		APIEndpoint:        strings.TrimSpace(account.APIEndpoint),
		BearerToken:        bearer,
		UserID:             userID,
		EnterpriseID:       account.EnterpriseID,
		TenantID:           account.TenantID,
		DepartmentFullName: account.DepartmentFullName,
		Domain:             account.Domain,
	}
	endpoint := provider.ResolveProtocolDirectBillingEndpoint(chatOpts, path)
	headers := client.BuildProtocolDirectHeaders(chatOpts)
	return endpoint, headers, nil
}

func withCheckinTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, checkinRequestTimeout)
}

type triBool int

const (
	triUnset triBool = 0
	triFalse triBool = 1
	triTrue  triBool = 2
)

func parseTriBool(value any, allowCheckedInText bool) triBool {
	if value == nil {
		return triUnset
	}
	switch v := value.(type) {
	case bool:
		if v {
			return triTrue
		}
		return triFalse
	case string:
		s := strings.TrimSpace(v)
		if s == "" || s == "<nil>" {
			return triUnset
		}
		if allowCheckedInText && strings.Contains(s, "已签到") {
			return triTrue
		}
		switch strings.ToLower(s) {
		case "1", "true", "yes", "on", "active", "enabled":
			return triTrue
		case "0", "false", "no", "off", "inactive", "disabled":
			return triFalse
		}
		return triUnset
	case float64:
		if v == 0 {
			return triFalse
		}
		return triTrue
	case float32:
		if v == 0 {
			return triFalse
		}
		return triTrue
	case int:
		if v == 0 {
			return triFalse
		}
		return triTrue
	case int64:
		if v == 0 {
			return triFalse
		}
		return triTrue
	case json.Number:
		if i, err := v.Int64(); err == nil {
			if i == 0 {
				return triFalse
			}
			return triTrue
		}
		if f, err := v.Float64(); err == nil {
			if f == 0 {
				return triFalse
			}
			return triTrue
		}
	}
	return triUnset
}

func coalesceTriBool(data map[string]any, keys []string, allowCheckedInText bool) triBool {
	for _, key := range keys {
		if v, ok := data[key]; ok {
			t := parseTriBool(v, allowCheckedInText)
			if t != triUnset {
				return t
			}
		}
	}
	return triUnset
}

func triToBool(t triBool) bool {
	return t == triTrue
}

// parseCheckedInStatusText treats status text like "已签到" as today checked-in only.
// Numeric or "active" status values are activity flags, not user check-in state.
func parseCheckedInStatusText(value any) triBool {
	s := strings.TrimSpace(fmt.Sprint(value))
	if s == "" || s == "<nil>" {
		return triUnset
	}
	if strings.Contains(s, "已签到") {
		return triTrue
	}
	return triUnset
}

type checkinOutcome int

const (
	checkinFailed checkinOutcome = iota
	checkinAlreadyDone
	checkinCheckedIn
	checkinUnsupported
	checkinSkipped
)

func classifyCheckinResult(item CheckinResult) checkinOutcome {
	switch {
	case item.OK && item.AlreadyCheckedIn:
		return checkinAlreadyDone
	case item.OK:
		return checkinCheckedIn
	case !item.Supported:
		return checkinUnsupported
	case strings.TrimSpace(item.Message) == "":
		return checkinSkipped
	default:
		return checkinFailed
	}
}

func mapCheckinActivity(data map[string]any) CheckinActivity {
	active := coalesceTriBool(data, []string{"active", "enabled", "enable", "status"}, false)
	today := coalesceTriBool(data, []string{"today_checked_in", "todayCheckedIn", "checked_in", "checkedIn"}, false)
	if today == triUnset {
		today = parseCheckedInStatusText(data["status"])
	}
	return CheckinActivity{
		Active:         triToBool(active),
		TodayCheckedIn: triToBool(today),
		StreakDays:     int(asNumberOr(data["streak_days"], data["streakDays"], 0)),
		DailyCredit:    int(asNumberOr(data["daily_credit"], data["dailyCredit"], 0)),
		TodayCredit:    int(asNumberOr(data["today_credit"], data["todayCredit"], 0)),
		IsStreakDay:    triToBool(coalesceTriBool(data, []string{"is_streak_day", "isStreakDay"}, false)),
	}
}

func checkinMessage(payload map[string]any) string {
	if payload == nil {
		return "unknown error"
	}
	return strutil.First(
		fmt.Sprint(payload["msg"]),
		fmt.Sprint(payload["message"]),
		fmt.Sprint(payload["error"]),
		"unknown error",
	)
}

func isCheckinAlreadyDone(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	code, ok := asNumber(payload["code"])
	if ok && int(code) == checkinAlreadyCode {
		return true
	}
	msg := checkinMessage(payload)
	return strings.Contains(msg, "已签到") || strings.Contains(msg, "明天再来")
}

func unsupportedCheckinMessage() string {
	return "签到活动未开启或已过期"
}
