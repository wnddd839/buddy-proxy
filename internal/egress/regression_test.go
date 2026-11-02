package egress

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMalformedProxyDoesNotExposeCredentials(t *testing.T) {
	for _, raw := range []string{
		"http://alice:private-password@127.0.0.1:bad",
		"http://alice:private-password@%zz:80",
	} {
		_, err := Parse(raw)
		if err == nil || strings.Contains(err.Error(), "private-password") ||
			strings.Contains(Mask(raw), "private-password") {
			t.Fatalf("malformed proxy was accepted or exposed credentials: err=%v", err)
		}
	}
}

func TestSOCKSHandshakeHonorsCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		_, _ = io.Copy(io.Discard, conn)
	}()
	p, err := Parse("socks5h://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		conn, err := p.DialContext(ctx, "tcp", "target.invalid:443")
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()
	select {
	case <-accepted:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("proxy was not contacted")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stalled handshake unexpectedly succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SOCKS handshake ignored cancellation")
	}
}

func TestDirectProbeIgnoresEnvironmentProxy(t *testing.T) {
	if os.Getenv("CBP_DIRECT_PROBE_TEST") == "1" {
		result := ProbeEndpointURL(t.Context(), "", "http://direct-probe.invalid")
		if result.OK {
			t.Fatal("DIRECT probe used the environment proxy")
		}
		return
	}
	// ProxyFromEnvironment caches its first result, so use a fresh test process.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ip":"203.0.113.9"}`)
	}))
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDirectProbeIgnoresEnvironmentProxy$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CBP_DIRECT_PROBE_TEST=1",
		"HTTP_PROXY="+proxy.URL, "http_proxy="+proxy.URL, "NO_PROXY=", "no_proxy=")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated direct probe failed: %v\n%s", err, output)
	}
}
