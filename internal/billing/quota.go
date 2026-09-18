package billing

import (
	"strings"
	"sync"
	"time"
)

// QuotaState 是从上游 billing 接口提炼的配额快照，用于号池冷却与选号。
type QuotaState struct {
	Remaining  *float64 `json:"remaining"`
	Unlimited  bool     `json:"unlimited,omitzero"`
	ResetAt    int64    `json:"resetAt,omitzero"`
	CheckedAt  int64    `json:"checkedAt,omitzero"`
	NotifyCode int      `json:"notifyCode,omitzero"`
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
	if !q.Exhausted(now) || q.ResetAt <= now.UnixMilli() {
		return 0
	}
	return time.Duration(q.ResetAt-now.UnixMilli()) * time.Millisecond
}

func QuotaStateFromUsage(usage UsageResult, now time.Time) QuotaState {
	state := QuotaState{
		Remaining: cloneFloatPtr(usage.Credits.Remaining),
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
	consider := func(raw string) {
		ms := ParseBillingTimeMillis(raw)
		if ms <= nowMs {
			return
		}
		if best == 0 || ms < best {
			best = ms
		}
	}
	consider(credits.CycleEndTime)
	for _, pkg := range credits.Packages {
		consider(pkg.CycleEndTime)
		consider(pkg.SlicePeriodEndTime)
	}
	return best
}

func parseBillingTimeMillis(raw string) int64 {
	return ParseBillingTimeMillis(raw)
}

const maxModelRateLimitCooldown = 48 * time.Hour

// ModelRateLimitCooldown 从 6004 / rate-model 的 "will reset at" 文案解析冷却时长。
// 解析失败或已过期时返回 ok=false，由调用方回退固定表。
func ModelRateLimitCooldown(err error, now time.Time) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	has6004 := strings.Contains(lower, "6004")
	hasRateModel := strings.Contains(lower, "rate-model") || strings.Contains(lower, "rate model")
	if !has6004 && !hasRateModel {
		return 0, false
	}
	if !strings.Contains(lower, "will reset at") {
		return 0, false
	}
	ms := ParseBillingTimeMillis(extractWillResetAt(msg))
	if ms <= 0 {
		return 0, false
	}
	d := time.Duration(ms-now.UnixMilli()) * time.Millisecond
	if d <= 0 {
		return 0, false
	}
	if d > maxModelRateLimitCooldown {
		d = maxModelRateLimitCooldown
	}
	return d, true
}

func extractWillResetAt(msg string) string {
	lower := strings.ToLower(msg)
	idx := strings.Index(lower, "will reset at")
	if idx < 0 {
		return ""
	}
	raw := strings.TrimSpace(msg[idx+len("will reset at"):])
	if cut := strings.IndexAny(raw, "\n;)]["); cut >= 0 {
		raw = strings.TrimSpace(raw[:cut])
	}
	lowerRaw := strings.ToLower(raw)
	for _, suffix := range []string{" utc+8", " utc+08", " utc+08:00", " gmt+8", " gmt+08"} {
		if strings.HasSuffix(lowerRaw, suffix) {
			return strings.TrimSpace(raw[:len(raw)-len(suffix)])
		}
	}
	return raw
}

// ParseBillingTimeMillis 解析上游时间戳。无时区按 Asia/Shanghai。
func ParseBillingTimeMillis(raw string) int64 {
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

var billingLocalLocation = sync.OnceValue(func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Local
	}
	return loc
})

func cloneFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return new(*v)
}
