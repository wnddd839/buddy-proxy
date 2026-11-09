package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// 重试参数要能通过 POST /checkin-config 写进 .env 并进内存。
func TestSaveRetrySettings(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "proxy.env")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envPath)

	srv, _, _ := checkinTestServer(t, nil)

	min := 30
	max := 2
	if err := srv.saveBehaviorSwitches(nil, &min, &max); err != nil {
		t.Fatalf("saveBehaviorSwitches: %v", err)
	}

	raw, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("读 .env: %v", err)
	}
	text := string(raw)
	if !contains(text, "CODEBUDDY_PROXY_CHECKIN_RETRY_INTERVAL_MS=1800000") {
		t.Fatalf(".env 应写入 30 分钟 = 1800000ms，实际:\n%s", text)
	}
	if !contains(text, "CODEBUDDY_PROXY_CHECKIN_RETRY_MAX=2") {
		t.Fatalf(".env 应写入 retry max=2，实际:\n%s", text)
	}

	cfg := srv.Svc.Config()
	if cfg.CheckinRetryInterval != 30*time.Minute {
		t.Fatalf("内存配置 interval want 30m, got %v", cfg.CheckinRetryInterval)
	}
	if cfg.CheckinRetryMax != 2 {
		t.Fatalf("内存配置 max want 2, got %d", cfg.CheckinRetryMax)
	}
}

// retryMin=0 表示不重试，必须能存成 0（不能被当成「没传」忽略）。
func TestSaveRetryDisabled(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "proxy.env")
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", envPath)

	srv, _, _ := checkinTestServer(t, nil)
	zero := 0
	if err := srv.saveBehaviorSwitches(nil, &zero, nil); err != nil {
		t.Fatal(err)
	}
	cfg := srv.Svc.Config()
	if cfg.CheckinRetryInterval != 0 {
		t.Fatalf("interval want 0（不重试），got %v", cfg.CheckinRetryInterval)
	}
}

// 不传这些字段时不能改动已有值（老前端兼容）。
func TestSaveRetryOmittedLeavesUnchanged(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEBUDDY_PROXY_ENV_FILE", filepath.Join(dir, "proxy.env"))

	srv, _, _ := checkinTestServer(t, nil)
	before := srv.Svc.Config().CheckinRetryInterval
	if err := srv.saveBehaviorSwitches(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	// 全部为 nil 时应直接返回 nil，不做任何持久化
	cfg := srv.Svc.Config()
	if cfg.CheckinRetryInterval != before {
		t.Fatalf("没传字段不该改配置，%v -> %v", before, cfg.CheckinRetryInterval)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

var _ = config.Config{}
