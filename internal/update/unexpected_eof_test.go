package update

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 端对端：模拟 Release 的 SHA256SUMS.txt 响应体中途断开，检查更新仍应成功。
func TestCheckRetriesUnexpectedEOFOnChecksumFile(t *testing.T) {
	var attempts atomic.Int32
	data := []byte("synthetic executable")
	name := "codebuddy-proxy-windows-x64.exe"
	sum := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), name)
	ts := newUpdateTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/latest"):
			http.Redirect(w, r, releaseTagBase+"v0.6", http.StatusFound)
		case r.Method == http.MethodHead && strings.Contains(r.URL.Path, "/releases/tag/"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "SHA256SUMS.txt"):
			if attempts.Add(1) == 1 {
				// 声明长度大于实发字节，让客户端读到 unexpected EOF。
				w.Header().Set("Content-Length", fmt.Sprint(len(sum)+10))
				_, _ = io.WriteString(w, sum[:5])
				return
			}
			_, _ = io.WriteString(w, sum)
		case strings.HasSuffix(r.URL.Path, name):
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	m := newUpdateTestManager(t, ts)
	state, err := m.Check(t.Context())
	if err != nil {
		t.Fatalf("瞬断不应导致检查失败: %v", err)
	}
	if !state.Available || state.Latest != "v0.6" {
		t.Fatalf("state=%+v", state)
	}
	if attempts.Load() != 2 {
		t.Fatalf("checksum 请求次数=%d want 2", attempts.Load())
	}
}

// 持续中断时不能无限重试，也不能返回裸 EOF 让管理台无从判断。
func TestCheckGivesUpAfterRetries(t *testing.T) {
	ts := newUpdateTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/latest") {
			http.Redirect(w, r, releaseTagBase+"v0.6", http.StatusFound)
			return
		}
		if r.Method == http.MethodHead && strings.Contains(r.URL.Path, "/releases/tag/") {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	})
	m := newUpdateTestManager(t, ts)
	_, err := m.Check(t.Context())
	if err == nil || !strings.Contains(err.Error(), "已重试") {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("应保留原始中断原因: %v", err)
	}
}

func newUpdateTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

func newUpdateTestManager(t *testing.T, ts *httptest.Server) *Manager {
	t.Helper()
	m := New("v0.5.5.1")
	m.exe = filepath.Join(t.TempDir(), "proxy.exe")
	if err := os.WriteFile(m.exe, []byte("original"), 0o700); err != nil {
		t.Fatal(err)
	}
	m.asset = "codebuddy-proxy-windows-x64.exe"
	m.state.Message = ""
	m.supported = true
	m.client = &http.Client{Timeout: 3 * time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		c := r.Clone(r.Context())
		c.URL.Scheme = "http"
		c.URL.Host = strings.TrimPrefix(ts.URL, "http://")
		resp, err := http.DefaultTransport.RoundTrip(c)
		if resp != nil {
			resp.Request = r
		}
		return resp, err
	})}
	return m
}
