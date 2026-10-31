package provider

import (
	"net/http"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestClientForRouting(t *testing.T) {
	client := NewClient(config.Config{})
	if client.HTTP == nil {
		t.Fatal("HTTP client 未初始化")
	}
	if got := client.For(""); got != client.HTTP {
		t.Fatal("空出口应返回直连客户端")
	}
	viaWarp := client.For("socks5h://u:p@127.0.0.1:1080")
	if viaWarp == client.HTTP {
		t.Fatal("绑定出口后不该复用直连客户端")
	}
	if viaWarp.Transport == client.HTTP.Transport {
		t.Fatal("出口 Transport 必须独立，否则连接池会串")
	}
	if again := client.For("socks5h://u:p@127.0.0.1:1080"); again != viaWarp {
		t.Fatal("同一出口应复用同一个 client")
	}
	if other := client.For("socks5h://u:p@127.0.0.1:1081"); other == viaWarp {
		t.Fatal("不同出口不该共用 client")
	}
	// 非法出口回落直连，保证配置写错时不会把请求发丢。
	if got := client.For("ftp://x:1"); got != client.HTTP {
		t.Fatal("非法出口应回落直连")
	}
}

// 字面量构造的 Client（没走 NewClient）也不能因为 base 为 nil 而崩。
func TestClientForLiteralClientWithoutBase(t *testing.T) {
	transport := &http.Transport{}
	client := &Client{HTTP: &http.Client{Transport: transport}}
	got := client.For("socks5h://u:p@127.0.0.1:1080")
	if got == nil || got == client.HTTP {
		t.Fatal("应基于 HTTP.Transport 兜底生成出口客户端")
	}
}
