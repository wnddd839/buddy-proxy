package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// 端对端：上游以 FIN 收尾却没给终止帧时，不能再把半截正文当完整回答。
// 非流式（首内容未下发）应换号重放，流式（已下发）应显式给客户端错误。
func TestUpstreamFINWithoutTerminalFrame(t *testing.T) {
	const complete = "data: {\"choices\":[{\"delta\":{\"content\":\"完整回答\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	const truncated = "data: {\"choices\":[{\"delta\":{\"content\":\"半句话\"}}]}\n\n"

	cases := []struct {
		name     string
		stream   bool
		wantBody string
		wantErr  string
	}{
		{"非流式半截换号重放", false, "完整回答", ""},
		{"正常收尾不受影响", false, "完整回答", ""},
		{"流式半截显式报错", true, "半句话", "truncated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testServerCfg(t, config.Config{Site: "domestic"})
			seedTwoDomestic(t, s)
			var calls atomic.Int32
			tr := &terminalFrameTransport{body: complete, truncated: truncated, calls: &calls}
			s.Svc.Provider.HTTP = &http.Client{Transport: tr}
			srv := httptest.NewServer(s.HTTP.Handler)
			defer srv.Close()
			srv.Client().Timeout = 5 * time.Second

			if tc.name == "正常收尾不受影响" {
				tr.firstComplete = true
			}
			body := `{"model":"auto","stream":false,"messages":[{"role":"user","content":"hi"}]}`
			if tc.stream {
				body = `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`
			}
			resp, err := srv.Client().Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			text := string(raw)
			if !strings.Contains(text, tc.wantBody) {
				t.Fatalf("正文未到达客户端: status=%d body=%s", resp.StatusCode, text)
			}
			if tc.wantErr != "" && !strings.Contains(text, tc.wantErr) {
				t.Fatalf("半截流没有显式报错: status=%d body=%s", resp.StatusCode, text)
			}
			if tc.wantErr == "" && strings.Contains(text, "\"error\"") {
				t.Fatalf("正常/重放路径不该报错: body=%s", text)
			}
			wantCalls := int32(2)
			if tc.name == "正常收尾不受影响" {
				wantCalls = 1
			}
			if tc.stream {
				wantCalls = 1 // 已下发正文不得重放
			}
			if calls.Load() != wantCalls {
				t.Fatalf("上游请求次数=%d want=%d body=%s", calls.Load(), wantCalls, text)
			}
		})
	}
}

// 端对端：停滞时未下发内容可换号重放，已下发的流式正文不得重放。
func TestUpstreamStallRetriesOnlyBeforeDelivery(t *testing.T) {
	const complete = "data: {\"choices\":[{\"delta\":{\"content\":\"恢复\"}}]}\n\n" +
		"data: [DONE]\n\n"
	const partial = "data: {\"choices\":[{\"delta\":{\"content\":\"半句话\"}}]}\n\n"
	for _, tc := range []struct {
		name      string
		initial   string
		stream    bool
		wantCalls int32
	}{
		{"首内容前停滞换号", "", false, 2},
		{"非流式缓存正文后停滞换号", partial, false, 2},
		{"流式已下发正文不得重放", partial, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServerCfg(t, config.Config{Site: "domestic"})
			seedTwoDomestic(t, s)
			var calls atomic.Int32
			s.Svc.Provider.StallTimeout = 60 * time.Millisecond
			s.Svc.Provider.HTTP = &http.Client{Transport: &stallTransport{calls: &calls, complete: complete, initial: tc.initial}}
			srv := httptest.NewServer(s.HTTP.Handler)
			defer srv.Close()
			srv.Client().Timeout = 5 * time.Second

			body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`
			if tc.stream {
				body = `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`
			}
			start := time.Now()
			resp, err := srv.Client().Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("看门狗未及时触发: %s", elapsed)
			}
			text := string(raw)
			if calls.Load() != tc.wantCalls {
				t.Fatalf("上游请求次数=%d want=%d body=%s", calls.Load(), tc.wantCalls, text)
			}
			if tc.stream {
				if !strings.Contains(text, "半句话") || !strings.Contains(text, "stalled after delivering content") || strings.Contains(text, "恢复") {
					t.Fatalf("已下发正文的停滞流应显式失败且不得重放: %s", text)
				}
			} else if resp.StatusCode != http.StatusOK || !strings.Contains(text, "恢复") || strings.Contains(text, "半句话") || strings.Contains(text, "\"error\"") {
				t.Fatalf("未下发正文的停滞请求应换号恢复: status=%d body=%s", resp.StatusCode, text)
			}
		})
	}
}

func seedTwoDomestic(t *testing.T, s *Server) {
	t.Helper()
	for _, label := range []string{"first", "second"} {
		if _, _, err := s.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
			Label: label, Site: "domestic", BearerToken: "token-" + label, Enabled: true,
		})); err != nil {
			t.Fatal(err)
		}
	}
}

type terminalFrameTransport struct {
	body          string
	truncated     string
	calls         *atomic.Int32
	firstComplete bool
}

func (tr *terminalFrameTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, "chat") {
		return (&responsesUpstreamTransport{}).RoundTrip(req)
	}
	n := tr.calls.Add(1)
	payload := tr.body
	if n == 1 && !tr.firstComplete {
		payload = tr.truncated
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    req,
	}, nil
}

type stallTransport struct {
	calls    *atomic.Int32
	complete string
	initial  string
}

func (tr *stallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, "chat") {
		return (&responsesUpstreamTransport{}).RoundTrip(req)
	}
	if tr.calls.Add(1) == 1 {
		pr, pw := io.Pipe()
		// 可先写部分正文，再保持连接不关；看门狗关闭 pr 后 Read 返回错误。
		go func() {
			defer pw.Close()
			_, _ = io.WriteString(pw, tr.initial)
			<-req.Context().Done()
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       pr,
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(tr.complete)),
		Request:    req,
	}, nil
}
