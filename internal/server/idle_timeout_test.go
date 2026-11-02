package server

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// 服务端空闲超时必须可配置：短连接场景下，长时间的 keep-alive 空闲连接
// 只会增加「死连接被复用」的概率（见 SSE 连接复用修复）。
func TestServerIdleTimeoutConfigurable(t *testing.T) {
	cases := []struct {
		name string
		cfg  time.Duration
		want time.Duration
	}{
		{"显式配置 30s", 30 * time.Second, 30 * time.Second},
		{"显式配置 5s", 5 * time.Second, 5 * time.Second},
		{"零值回退默认 120s", 0, 120 * time.Second},
		{"负值回退默认 120s", -1, 120 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := testServerCfg(t, config.Config{
				ServerIdleTimeout: c.cfg,
			})
			if srv.HTTP == nil {
				t.Fatal("HTTP server 未初始化")
			}
			if got := srv.HTTP.IdleTimeout; got != c.want {
				t.Fatalf("IdleTimeout 期望 %v，实际 %v", c.want, got)
			}
		})
	}
}
