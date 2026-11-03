package server

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// 优雅关闭宽限期默认必须远大于旧值 10s：单回合实测可达数分钟，
// 10s 会在每次部署时硬切在途 agent 请求。
func TestDrainTimeoutDefaultIsGenerous(t *testing.T) {
	s := &Server{}
	if got := s.DrainTimeout(); got < 5*time.Minute {
		t.Fatalf("drain grace too small: %s (deploys will cut in-flight turns)", got)
	}
}

// 环境变量可覆盖（运维需要时能缩短/加长）。
func TestDrainTimeoutOverride(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS", "900000")
	s := testServerCfg(t, config.Config{Site: "domestic"})
	if got := s.DrainTimeout(); got != 15*time.Minute {
		t.Fatalf("override not applied: %s", got)
	}
}

// 非法值必须安全回退到默认，不能变成 0（0 会让 Shutdown 立刻超时）。
func TestDrainTimeoutInvalidFallsBack(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS", "not-a-number")
	s := testServerCfg(t, config.Config{Site: "domestic"})
	if got := s.DrainTimeout(); got != 10*time.Minute {
		t.Fatalf("invalid value must fall back to default, got %s", got)
	}
}
