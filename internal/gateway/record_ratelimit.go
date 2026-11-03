package gateway

import (
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/billing"
)

// recordRateLimit 把一次上游限流（429 / code 6004 / rate-model）写入并发观测器。
//
// 只在「确实是限流」时才记，避免把普通失败混进 429 统计：
//   - billing.IsModelRateLimitError 覆盖 6004 / rate-model；
//   - 文本里出现 429 / too many requests / rate limit。
//
// resetAt 用 billing.ModelRateLimitCooldown 反推（它已处理好日期形状与时区），
// 解析不到就留 0——事件仍然有价值，只是无法判断「频率限制 vs 日配额」。
func (s *Service) recordRateLimit(account accounts.Account, model, site string, err error) {
	if s == nil || s.Concurrency == nil || err == nil {
		return
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	limited := billing.IsModelRateLimitError(err) ||
		strings.Contains(lower, "429") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "rate limit")
	if !limited {
		return
	}
	var resetAt int64
	now := time.Now()
	if d, ok := billing.ModelRateLimitCooldown(err, now); ok && d > 0 {
		resetAt = now.Add(d).UnixMilli()
	}
	s.Concurrency.RecordRateLimit(account.ID, account.Label, model, site, msg, resetAt)
}
