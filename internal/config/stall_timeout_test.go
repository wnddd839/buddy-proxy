package config

import (
	"testing"
	"time"
)

// TestLoadStallTimeoutDefaults 锁住用户要求：默认从 3 分钟提到 10 分钟，
// 且首字节后阈值也放宽（不再是写死 3 分钟）。
func TestLoadStallTimeoutDefaults(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_STALL_TIMEOUT_MS", "")
	t.Setenv("CODEBUDDY_PROXY_STALL_AFTER_FIRST_BYTE_MS", "")
	cfg := Load()
	if cfg.StallTimeout != 10*time.Minute {
		t.Fatalf("首字节前默认应为 10 分钟，实际 %s", cfg.StallTimeout)
	}
	if cfg.StallTimeoutAfterFirstByte != 10*time.Minute {
		t.Fatalf("首字节后默认应为 10 分钟，实际 %s", cfg.StallTimeoutAfterFirstByte)
	}
	// ShutdownGrace 明确不动（保护在途长回合）。
	if cfg.ShutdownGrace != DefaultShutdownGrace {
		t.Fatalf("ShutdownGrace 不应改变，实际 %s", cfg.ShutdownGrace)
	}
}

// TestLoadStallTimeoutFromEnv 锁住两个 env 可独立配置。
func TestLoadStallTimeoutFromEnv(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_STALL_TIMEOUT_MS", "60000")
	t.Setenv("CODEBUDDY_PROXY_STALL_AFTER_FIRST_BYTE_MS", "1800000")
	cfg := Load()
	if cfg.StallTimeout != time.Minute {
		t.Fatalf("StallTimeout=%s want 1m", cfg.StallTimeout)
	}
	if cfg.StallTimeoutAfterFirstByte != 30*time.Minute {
		t.Fatalf("StallTimeoutAfterFirstByte=%s want 30m", cfg.StallTimeoutAfterFirstByte)
	}
}

// TestLoadStallTimeoutInvalidFallsBack 锁住垃圾值回落默认，不产生 0（0 会关闭看门狗）。
func TestLoadStallTimeoutInvalidFallsBack(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_STALL_TIMEOUT_MS", "abc")
	t.Setenv("CODEBUDDY_PROXY_STALL_AFTER_FIRST_BYTE_MS", "-1")
	cfg := Load()
	if cfg.StallTimeout != 10*time.Minute {
		t.Fatalf("非法值应回落 10 分钟，实际 %s", cfg.StallTimeout)
	}
	if cfg.StallTimeoutAfterFirstByte <= 0 {
		t.Fatalf("非法值不得让看门狗关闭，实际 %s", cfg.StallTimeoutAfterFirstByte)
	}
}
