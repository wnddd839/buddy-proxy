package config

import (
	"testing"
	"time"
)

func TestLoadWatchConfigDefaults(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_WATCH", "")
	cfg := loadWatchConfig()
	if cfg.Enabled {
		t.Fatal("watch must be off by default")
	}
	if cfg.Interval <= 0 || cfg.Window <= 0 || cfg.MinSample <= 0 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadWatchConfigFromEnv(t *testing.T) {
	t.Setenv("CODEBUDDY_PROXY_WATCH", "on")
	t.Setenv("CODEBUDDY_PROXY_WATCH_INTERVAL", "90s")
	t.Setenv("CODEBUDDY_PROXY_WATCH_WINDOW", "40")
	t.Setenv("CODEBUDDY_PROXY_WATCH_ERROR_RATE", "30") // 百分数写法
	t.Setenv("CODEBUDDY_PROXY_WATCH_REMINDER", "2h")
	t.Setenv("CODEBUDDY_PROXY_WATCH_SMTP_ADDR", "smtp.example.com:465")
	t.Setenv("CODEBUDDY_PROXY_WATCH_SMTP_TO", "a@example.com, b@example.com")

	cfg := loadWatchConfig()
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Interval != 90*time.Second {
		t.Fatalf("interval=%v", cfg.Interval)
	}
	if cfg.Window != 40 {
		t.Fatalf("window=%d", cfg.Window)
	}
	if cfg.ErrorRate != 0.3 {
		t.Fatalf("errorRate=%v", cfg.ErrorRate)
	}
	if cfg.Reminder != 2*time.Hour {
		t.Fatalf("reminder=%v", cfg.Reminder)
	}
	if len(cfg.SMTP.To) != 2 || cfg.SMTP.To[1] != "b@example.com" {
		t.Fatalf("smtp to=%v", cfg.SMTP.To)
	}
}

func TestEnvRatioClamps(t *testing.T) {
	t.Setenv("TEST_RATIO", "0")
	if got := envRatio("TEST_RATIO", 0.25); got != 0.25 {
		t.Fatalf("zero must fall back, got %v", got)
	}
	t.Setenv("TEST_RATIO", "abc")
	if got := envRatio("TEST_RATIO", 0.25); got != 0.25 {
		t.Fatalf("garbage must fall back, got %v", got)
	}
	t.Setenv("TEST_RATIO", "0.5")
	if got := envRatio("TEST_RATIO", 0.25); got != 0.5 {
		t.Fatalf("got %v", got)
	}
}

func TestEnvDurationTextFallsBackToSeconds(t *testing.T) {
	t.Setenv("TEST_DUR", "30")
	if got := envDurationText("TEST_DUR", time.Minute); got != 30*time.Second {
		t.Fatalf("got %v", got)
	}
	t.Setenv("TEST_DUR", "45s")
	if got := envDurationText("TEST_DUR", time.Minute); got != 45*time.Second {
		t.Fatalf("got %v", got)
	}
	t.Setenv("TEST_DUR", "-1s")
	if got := envDurationText("TEST_DUR", time.Minute); got != time.Minute {
		t.Fatalf("negative must fall back, got %v", got)
	}
}
