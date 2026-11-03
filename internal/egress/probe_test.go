package egress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 直连探针（proxyURL 为空）应当拿到回显里的 IP。
func TestProbeDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"198.51.100.7"}`))
	}))
	defer srv.Close()
	res := ProbeEndpointURL(context.Background(), "", srv.URL)
	if !res.OK {
		t.Fatalf("直连探针失败: %+v", res)
	}
	if res.IP != "198.51.100.7" {
		t.Fatalf("IP 解析错误: %+v", res)
	}
}

// 纯文本回显（不少服务直接吐 IP）也要认。
func TestProbePlainTextEcho(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.9\n"))
	}))
	defer srv.Close()
	res := ProbeEndpointURL(context.Background(), "", srv.URL)
	if !res.OK || res.IP != "203.0.113.9" {
		t.Fatalf("纯文本 IP 解析失败: %+v", res)
	}
}

// 非法出口要给出可读错误，而不是静默直连。
func TestProbeInvalidProxy(t *testing.T) {
	res := ProbeEndpointURL(context.Background(), "ftp://x:1", "http://127.0.0.1:1")
	if res.OK {
		t.Fatalf("非法出口不该成功: %+v", res)
	}
	if !strings.Contains(res.Error, "unsupported scheme") {
		t.Fatalf("错误信息不明确: %s", res.Error)
	}
}

func TestProbeUsesConfiguredProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "echo.invalid" {
			t.Errorf("unexpected proxy target: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"ip":"203.0.113.9"}`))
	}))
	defer proxy.Close()
	// 目标域名不可能直连成功；只有实际经过指定 HTTP / SOCKS 代理才能读到回显。
	socks := fakeSocks(t, false, "", "", "HTTP/1.0 200 OK\r\n\r\n"+`{"ip":"203.0.113.9"}`)
	for _, raw := range []string{proxy.URL, "socks5h://" + socks} {
		res := ProbeEndpointURL(t.Context(), raw, "http://echo.invalid")
		if !res.OK || res.IP != "203.0.113.9" {
			t.Fatalf("probe did not use %s: %+v", raw, res)
		}
	}
}

// 回显不是 IP 时要报错，避免把错误页当出口 IP。
func TestProbeRejectsNonIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>oops</html>"))
	}))
	defer srv.Close()
	res := ProbeEndpointURL(context.Background(), "", srv.URL)
	if res.OK {
		t.Fatalf("非 IP 响应不该算成功: %+v", res)
	}
}
