package billing

import (
	"strings"
	"time"
)

// QuotaState 是从上游 billing 接口提炼的配额快照，用于号池冷却与选号。
type QuotaState struct {
	Remaining  *float64 `json:"remaining"`
	Unlimited  bool     `json:"unlimited"`
	ResetAt    int64    `json:"resetAt"` // unix millis，配额窗口结束时间
	CheckedAt  int64    `json:"checkedAt"`
	NotifyCode int      `json:"notifyCode"`
}

func (q QuotaState) Exhausted(now time.Time) bool {
	if q.Unlimited {
		return false
	}
	if q.Remaining != nil && *q.Remaining > 0 {
		return false
	}
	nowMs := now.UnixMilli()
	if q.ResetAt > 0 && q.ResetAt <= nowMs {
		return false
	}
	if q.Remaining != nil && *q.Remaining <= 0 {
		return true
	}
	return q.NotifyCode == 3
}

func (q QuotaState) CooldownDuration(now time.Time) time.Duration {
	if !q.Exhausted(now) {
		return 0
	}
	if q.ResetAt <= now.UnixMilli() {
		return 0
	}
	return time.Duration(q.ResetAt-now.UnixMilli()) * time.Millisecond
}

func QuotaStateFromUsage(usage UsageResult, now time.Time) QuotaState {
	state := QuotaState{
		Remaining: copyFloatPtr(usage.Credits.Remaining),
		Unlimited: usage.Credits.Unlimited,
		ResetAt:   creditsNextResetAt(usage.Credits, now),
		CheckedAt: now.UnixMilli(),
	}
	if usage.Notify != nil {
		state.NotifyCode = usage.Notify.DosageNotifyCode
	}
	if state.ResetAt == 0 && state.Exhausted(now) {
		// 上游未返回结束时间但已耗尽：保守等待 5 小时（海外滚动窗口常见值）。
		state.ResetAt = now.Add(5 * time.Hour).UnixMilli()
	}
	return state
}

func IsQuotaExhaustedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"quota", "credit", "额度", "用量", "exhaust", "insufficient",
		"不足", "耗尽", "dosage", "resource exhausted",
		"no remaining", "out of credits", "out of quota",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func creditsNextResetAt(credits Credits, now time.Time) int64 {
	nowMs := now.UnixMilli()
	best := int64(0)
	for _, candidate := range []string{credits.CycleEndTime} {
		if ms := parseBillingTimeMillis(candidate); ms > nowMs && (best == 0 || ms < best) {
			best = ms
		}
	}
	for _, pkg := range credits.Packages {
		for _, candidate := range []string{pkg.CycleEndTime, pkg.SlicePeriodEndTime} {
			if ms := parseBillingTimeMillis(candidate); ms > nowMs && (best == 0 || ms < best) {
				best = ms
			}
		}
	}
	return best
}

func parseBillingTimeMillis(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "<nil>" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UnixMilli()
		}
	}
	loc := billingLocalLocation()
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t.UnixMilli()
		}
	}
	if n, ok := asNumber(raw); ok && n > 1_000_000_000_000 {
		return int64(n)
	}
	if n, ok := asNumber(raw); ok && n > 1_000_000_000 {
		return int64(n * 1000)
	}
	return 0
}

func billingLocalLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Local
	}
	return loc
}

func copyFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
