package gateway

import (
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/concurrency"
)

// ConcurrencyStatus 返回并发观测快照，供管理台与外部监测读取。
func (s *Service) ConcurrencyStatus() concurrency.Snapshot {
	if s == nil || s.Concurrency == nil {
		return concurrency.Snapshot{GeneratedAt: time.Now().UnixMilli()}
	}
	return s.Concurrency.Snapshot()
}

// ConcurrencySummary 是 /status 的轻量视图：只带汇总数字。
// 完整按账号水位与限流事件在 /direct-admin/api/concurrency，避免 15 秒轮询
// 把最多 500 条事件反复塞进每个 /status 响应。
func (s *Service) ConcurrencySummary() map[string]any {
	snap := s.ConcurrencyStatus()
	return map[string]any{
		"generatedAt":    snap.GeneratedAt,
		"globalInFlight": snap.GlobalInFlight,
		"accounts":       len(snap.Accounts),
		"rateLimits":     len(snap.RateLimits),
		"totals":         snap.Totals,
	}
}
