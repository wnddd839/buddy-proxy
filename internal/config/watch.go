package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/watch"
)

// loadWatchConfig 读取 CODEBUDDY_PROXY_WATCH_* 环境变量。
// 默认关闭：监测会产生额外内存与告警流量，需显式开启。
func loadWatchConfig() watch.Config {
	cfg := watch.DefaultConfig()
	cfg.Enabled = envBool("CODEBUDDY_PROXY_WATCH", "", false)

	cfg.Interval = envDurationText("CODEBUDDY_PROXY_WATCH_INTERVAL", cfg.Interval)
	cfg.Window = envPositiveInt("CODEBUDDY_PROXY_WATCH_WINDOW", cfg.Window)
	cfg.Baseline = envPositiveInt("CODEBUDDY_PROXY_WATCH_BASELINE", cfg.Baseline)
	cfg.MinSample = envPositiveInt("CODEBUDDY_PROXY_WATCH_MIN_SAMPLES", cfg.MinSample)
	cfg.ErrorRate = envRatio("CODEBUDDY_PROXY_WATCH_ERROR_RATE", cfg.ErrorRate)
	cfg.ReasoningMissRate = envRatio("CODEBUDDY_PROXY_WATCH_REASONING_MISS", cfg.ReasoningMissRate)
	cfg.LatencyFactor = envFloat("CODEBUDDY_PROXY_WATCH_LATENCY_FACTOR", cfg.LatencyFactor)
	cfg.Reminder = envDurationText("CODEBUDDY_PROXY_WATCH_REMINDER", cfg.Reminder)

	cfg.AdminURL = watchAdminURL()

	cfg.WebhookURL = firstEnv("CODEBUDDY_PROXY_WATCH_WEBHOOK")
	cfg.WebhookToken = firstEnv("CODEBUDDY_PROXY_WATCH_WEBHOOK_TOKEN")

	cfg.SMTP = watch.SMTPConfig{
		Addr:    firstEnv("CODEBUDDY_PROXY_WATCH_SMTP_ADDR"),
		User:    firstEnv("CODEBUDDY_PROXY_WATCH_SMTP_USER"),
		Pass:    firstEnv("CODEBUDDY_PROXY_WATCH_SMTP_PASS"),
		From:    firstEnv("CODEBUDDY_PROXY_WATCH_SMTP_FROM"),
		To:      splitList(firstEnv("CODEBUDDY_PROXY_WATCH_SMTP_TO")),
		Mode:    strings.ToLower(firstEnv("CODEBUDDY_PROXY_WATCH_SMTP_MODE")),
		Timeout: cfg.WebhookTimeout,
	}

	// 保险：窗口与基线都非法时回落到默认值，避免除零与空判定。
	if cfg.Window <= 0 {
		cfg.Window = watch.DefaultConfig().Window
	}
	if cfg.MinSample <= 0 {
		cfg.MinSample = watch.DefaultConfig().MinSample
	}
	return cfg
}

// watchAdminURL 给告警邮件里的排查入口用；优先公网地址，否则本机端口。
func watchAdminURL() string {
	if base := strings.TrimRight(firstEnv("CODEBUDDY_PROXY_PUBLIC_BASE_URL", "CURSOR_DIRECT_PUBLIC_BASE_URL"), "/"); base != "" {
		return base + "/direct-admin/"
	}
	port := envInt("CODEBUDDY_PROXY_PORT", "CURSOR_DIRECT_PORT", DefaultPort)
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/direct-admin/"
}

func envPositiveInt(key string, def int) int {
	n := envInt(key, "", def)
	if n <= 0 {
		return def
	}
	return n
}

func envFloat(key string, def float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// envRatio 读取 0~1 的比例；允许写成百分数（如 "30" 视为 30%）。
func envRatio(key string, def float64) float64 {
	v := envFloat(key, def)
	if v > 1 {
		v /= 100
	}
	if v <= 0 || v > 1 {
		return def
	}
	return v
}

// envDurationText 解析 "90s" / "6h" 这类可读时长，纯数字按秒处理。
func envDurationText(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return def
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
