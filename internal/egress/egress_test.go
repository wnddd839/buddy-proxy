package egress

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		raw    string
		want   string // scheme/host:port，空表示 nil
		user   string
		remote bool
		err    bool
	}{
		{raw: "", want: ""},
		{raw: "  ", want: ""},
		{raw: "http://127.0.0.1:19120", want: "http/127.0.0.1:19120"},
		{raw: "socks5://10.0.0.1:1080", want: "socks5/10.0.0.1:1080"},
		{raw: "socks5h://10.0.0.1", want: "socks5h/10.0.0.1:1080", remote: true},
		{raw: "socks5://u:p@10.0.0.1:1080", want: "socks5/10.0.0.1:1080", user: "u"},
		{raw: "ftp://10.0.0.1:21", err: true},
		{raw: "http://", err: true},
		{raw: "http://host:notaport", err: true},
	}
	for _, tc := range cases {
		got, err := Parse(tc.raw)
		if tc.err {
			if err == nil {
				t.Fatalf("Parse(%q) 期望报错", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.raw, err)
		}
		if tc.want == "" {
			if got != nil {
				t.Fatalf("Parse(%q) 期望 nil", tc.raw)
			}
			continue
		}
		if got == nil {
			t.Fatalf("Parse(%q) 返回 nil", tc.raw)
		}
		if key := got.Scheme + "/" + got.Addr(); key != tc.want {
			t.Fatalf("Parse(%q) = %s, want %s", tc.raw, key, tc.want)
		}
		if got.User != tc.user {
			t.Fatalf("Parse(%q) user=%q want %q", tc.raw, got.User, tc.user)
		}
		if got.RemoteDNS != tc.remote {
			t.Fatalf("Parse(%q) remoteDNS=%v want %v", tc.raw, got.RemoteDNS, tc.remote)
		}
	}
}

func TestMaskHidesPassword(t *testing.T) {
	if got := Mask("socks5://alice:s3cret@127.0.0.1:17083"); strings.Contains(got, "s3cret") {
		t.Fatalf("Mask 泄露密码: %s", got)
	} else if got != "socks5://alice:***@127.0.0.1:17083" {
		t.Fatalf("Mask 格式不对: %s", got)
	}
	if got := Mask("http://127.0.0.1:19120"); got != "http://127.0.0.1:19120" {
		t.Fatalf("无密码时不该改写: %s", got)
	}
}

// fakeSocks 起一个最小 SOCKS5 服务，握手成功后回写 payload。
func fakeSocks(t *testing.T, requireAuth bool, user, pass, payload string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		head := make([]byte, 2)
		if _, err := io.ReadFull(conn, head); err != nil {
			return
		}
		methods := make([]byte, int(head[1]))
		if _, err := io.ReadFull(conn, methods); err != nil {
			return
		}
		if requireAuth {
			if _, err := conn.Write([]byte{0x05, 0x02}); err != nil {
				return
			}
			// RFC1929: VER, ULEN, UNAME, PLEN, PASSWD
			ver := make([]byte, 2)
			if _, err := io.ReadFull(conn, ver); err != nil {
				return
			}
			uname := make([]byte, int(ver[1]))
			if _, err := io.ReadFull(conn, uname); err != nil {
				return
			}
			plen := make([]byte, 1)
			if _, err := io.ReadFull(conn, plen); err != nil {
				return
			}
			pwd := make([]byte, int(plen[0]))
			if _, err := io.ReadFull(conn, pwd); err != nil {
				return
			}
			ok := string(uname) == user && string(pwd) == pass
			status := byte(0x00)
			if !ok {
				status = 0x01
			}
			if _, err := conn.Write([]byte{0x01, status}); err != nil || !ok {
				return
			}
		} else {
			if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
				return
			}
		}
		// CONNECT 请求：VER CMD RSV ATYP + addr + port
		req := make([]byte, 4)
		if _, err := io.ReadFull(conn, req); err != nil {
			return
		}
		switch req[3] {
		case 0x01:
			if _, err := io.ReadFull(conn, make([]byte, 4)); err != nil {
				return
			}
		case 0x04:
			if _, err := io.ReadFull(conn, make([]byte, 16)); err != nil {
				return
			}
		case 0x03:
			l := make([]byte, 1)
			if _, err := io.ReadFull(conn, l); err != nil {
				return
			}
			if _, err := io.ReadFull(conn, make([]byte, int(l[0]))); err != nil {
				return
			}
		}
		if _, err := io.ReadFull(conn, make([]byte, 2)); err != nil {
			return
		}
		// 成功响应 + 绑定地址 0.0.0.0:0
		if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			return
		}
		if strings.HasPrefix(payload, "HTTP/") {
			// HTTP 探针需先发请求；提前写响应会被 Transport 判成 unsolicited response。
			req, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				return
			}
			req.Body.Close()
		}
		_, _ = conn.Write([]byte(payload))
	}()
	return ln.Addr().String()
}

func TestDialContextNoAuth(t *testing.T) {
	addr := fakeSocks(t, false, "", "", "pong")
	p, err := Parse("socks5h://" + addr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("payload=%q", buf)
	}
}

func TestDialContextWithAuth(t *testing.T) {
	addr := fakeSocks(t, true, "alice", "s3cret", "ok")
	p, err := Parse("socks5://alice:s3cret@" + addr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.DialContext(ctx, "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ok" {
		t.Fatalf("payload=%q", buf)
	}
}

func TestDialContextAuthRejected(t *testing.T) {
	addr := fakeSocks(t, true, "alice", "s3cret", "never")
	p, err := Parse("socks5://alice:wrong@" + addr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.DialContext(ctx, "tcp", "example.com:443"); err == nil {
		t.Fatal("密码错误应当报错")
	}
}

func TestTransportUsesProxyForHTTP(t *testing.T) {
	p, err := Parse("http://127.0.0.1:19120")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	base := &http.Transport{}
	tr, err := p.Transport(base)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if tr.Proxy == nil {
		t.Fatal("HTTP 出口应设置 Proxy")
	}
	if tr == base {
		t.Fatal("不应复用同一个 Transport 指针")
	}
}

func TestTransportSOCKSUsesDialer(t *testing.T) {
	p, err := Parse("socks5://127.0.0.1:17083")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	base := &http.Transport{}
	tr, err := p.Transport(base)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if tr.Proxy != nil {
		t.Fatal("SOCKS 出口不应设置 Proxy")
	}
	if tr.DialContext == nil {
		t.Fatal("SOCKS 出口应设置 DialContext")
	}
}
