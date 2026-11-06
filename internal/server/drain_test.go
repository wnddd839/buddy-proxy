package server

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestDrainTimeoutDefaultsAndConfig(t *testing.T) {
	if got := (&Server{}).DrainTimeout(); got != config.DefaultShutdownGrace {
		t.Fatalf("default=%s want %s", got, config.DefaultShutdownGrace)
	}
	s := testServerCfg(t, config.Config{Site: "domestic", ShutdownGrace: 15 * time.Minute})
	if got := s.DrainTimeout(); got != 15*time.Minute {
		t.Fatalf("config=%s want 15m", got)
	}
}

func TestShutdownGraceEnvFallsBack(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS", "not-a-number")
	if got := config.Load().ShutdownGrace; got != config.DefaultShutdownGrace {
		t.Fatalf("invalid env=%s want default", got)
	}
}
