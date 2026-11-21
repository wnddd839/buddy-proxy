package gateway

import (
	"path/filepath"
	"strings"
	"sync/atomic"
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

// invariantsPrunedAt 记录上次清理决策日志的时刻（毫秒）。
//
// 惰性清理而不是后台 goroutine：省掉生命周期管理，也不会空转占资源。
// 与 systemprompt 的做法一致。
var invariantsPrunedAt atomic.Int64

const invariantsPruneInterval = time.Hour

// maybePruneInvariants 最多每小时清一次过期决策文件（30 天保留）。
//
// 没有它的话 proxy-decisions-*.jsonl 会无限增长：修好「输入也落盘」之后
// 一天就是几万行，不清理迟早顶满磁盘。
func (s *Service) maybePruneInvariants(sink *invariant.Sink) {
	if s == nil || sink == nil {
		return
	}
	now := time.Now()
	last := invariantsPrunedAt.Load()
	if now.UnixMilli()-last < invariantsPruneInterval.Milliseconds() {
		return
	}
	if !invariantsPrunedAt.CompareAndSwap(last, now.UnixMilli()) {
		return
	}
	if err := sink.Prune(now); err != nil && s.Log != nil {
		s.Log.Warn("decision log prune failed", "error", err.Error())
	}
}

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
			// 结构化 WARN：journalctl -u buddy-proxy | grep 'invariant hit' 就能查到。
			// 这里在写协程里执行，允许 I/O。
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
	s.maybePruneInvariants(sink)

	s.SetInvariants(sink)
	if s.Pool != nil {
		s.Pool.SetObserver(&InvariantObserver{Sink: sink})
	}
	if s.Log != nil {
		s.Log.Info("invariant sink enabled", "dir", dir, "invariants", len(invariant.DefaultRegistry().Specs()))
	}
	return sink
}
