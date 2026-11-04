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
	if got := s.DrainTimeout(); got != config.DefaultShutdownGrace {
		t.Fatalf("DrainTimeout()=%s want %s", got, config.DefaultShutdownGrace)
	}
	if got := s.DrainTimeout(); got < 5*time.Minute {
		t.Fatalf("drain grace too small: %s (deploys will cut in-flight turns)", got)
	}
}

// 配置里的宽限期必须真正接到 Server 上。
func TestDrainTimeoutFromConfig(t *testing.T) {
	s := testServerCfg(t, config.Config{Site: "domestic", ShutdownGrace: 15 * time.Minute})
	if got := s.DrainTimeout(); got != 15*time.Minute {
		t.Fatalf("DrainTimeout()=%s want 15m", got)
	}
}

// 环境变量经 internal/config 解析，非法值安全回退默认（0 会让 Shutdown 立刻超时）。
func TestShutdownGraceEnv(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS", "900000")
	if got := config.Load().ShutdownGrace; got != 15*time.Minute {
		t.Fatalf("ShutdownGrace=%s want 15m", got)
	}

	t.Setenv("CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS", "not-a-number")
	if got := config.Load().ShutdownGrace; got != config.DefaultShutdownGrace {
		t.Fatalf("invalid value must fall back to default, got %s", got)
	}
}
