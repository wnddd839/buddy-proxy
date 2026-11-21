package gateway

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/invariant"
)

// 观测体系的容量与保留。都取得很小：这是旁路观测，不允许影响主路径。
const (
	invariantRingSize     = 200
	invariantQueueSize    = 1024
	invariantSessionCap   = 512
	invariantErrorCap     = 256
	decisionRetentionDays = 30
)

// startInvariants 装配在线执行器：开存储 → 建 Sink → 回扫 → 接线。
//
// 失败时只 WARN 并返回 nil：观测绝不能拦住服务启动。
func (s *Service) startInvariants(accountsPath string) *invariant.Sink {
	if s == nil {
		return nil
	}
	dir := ""
	if trimmed := strings.TrimSpace(accountsPath); trimmed != "" {
		dir = filepath.Dir(trimmed)
	}
	if dir == "" {
		if s.Log != nil {
			s.Log.Warn("invariant sink disabled: no accounts dir")
		}
		return nil
	}

	store, err := decision.OpenStore(dir, decisionRetentionDays*24*time.Hour)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("decision store unavailable; invariant sink disabled", "dir", dir, "error", err.Error())
		}
		return nil
	}

	sink := invariant.NewSink(store,
		invariant.DefaultRegistry(),
		invariant.NewState(invariantSessionCap, invariantErrorCap),
		invariantRingSize, invariantQueueSize)
	if s.Log != nil {
		sink.SetLogger(func(msg string, fields map[string]any) {
			// 结构化 WARN：journalctl -u buddy-proxy | grep 'invariant hit' 就能查到
			attrs := make([]any, 0, len(fields)*2)
			for _, k := range []string{"invariant", "severity", "subject", "detail"} {
				if v, ok := fields[k]; ok {
					attrs = append(attrs, k, v)
				}
			}
			s.Log.Warn(msg, attrs...)
		})
	}
	sink.Start()
	if err := sink.Backfill(time.Now()); err != nil && s.Log != nil {
		s.Log.Warn("invariant backfill failed", "error", err.Error())
	}

	s.SetInvariants(sink)
	if s.Pool != nil {
		s.Pool.SetObserver(&InvariantObserver{Sink: sink})
	}
	if s.Log != nil {
		s.Log.Info("invariant sink enabled", "dir", dir, "invariants", len(invariant.DefaultRegistry().Specs()))
	}
	return sink
}
