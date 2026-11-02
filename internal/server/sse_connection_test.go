package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
)

// SSE 是一次性流：响应结束后连接必须关闭，不能回到 keep-alive 池。
//
// 背景：经由反向代理/隧道转发时，服务端关闭连接的 FIN 未必能可靠送达客户端，
// 客户端会把已失效的连接当作 keep-alive 复用，导致下一个请求挂起直到
// headersTimeout（openai-compatible 客户端常见 60s），表现为
// "Timed out waiting for response headers"；空转重发即可恢复。
//
// 本文件用真实 HTTP 连接（httptest.Server）验证三条 SSE 路径都声明
// `Connection: close` 并且确实不会被复用，不依赖外部上游。

func doSSERequest(t *testing.T, client *http.Client, baseURL, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s request failed: %v", path, err)
	}
	return resp
}

// 三条流式路径（chat / responses / agentic_search）都必须关闭连接。
func TestStreamingResponsesSendConnectionClose(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		rt   http.RoundTripper
	}{
		{
			name: "chat",
			path: "/v1/chat/completions",
			body: `{"model":"auto","stream":true,"max_tokens":8,` +
				`"messages":[{"role":"user","content":"hi"}]}`,
			rt: roundTripOKChat{},
		},
		{
			name: "responses",
			path: "/v1/responses",
			body: `{"model":"auto","input":"hi","stream":true}`,
			rt:   &responsesUpstreamTransport{},
		},
		{
			name: "agentic_search",
			path: "/v1/agentic_search",
			body: `{"query":"研究","stream":true}`,
			rt:   &agentToolUpstream{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServer(t, false, "", "")
			seedBothSites(t, srv)
			srv.Svc.Provider.HTTP = &http.Client{Transport: tc.rt}
			ts := httptest.NewServer(srv.HTTP.Handler)
			defer ts.Close()

			client := &http.Client{Transport: &http.Transport{}}
			defer client.CloseIdleConnections()
			resp := doSSERequest(t, client, ts.URL, tc.path, tc.body)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
				t.Fatalf("content-type=%q", ct)
			}
			if !resp.Close {
				t.Fatalf("SSE 响应必须关闭连接（否则客户端可能复用已失效连接导致挂起）；"+
					"resp.Close=false, Connection=%q", resp.Header.Get("Connection"))
			}
			_, _ = io.Copy(io.Discard, resp.Body)
		})
	}
}

// 流式响应发完之后，同一个客户端的下一个请求必须建立新连接。
// 用 httptrace.GotConn 的 Reused 标志判定，避免依赖时序或 sleep。
func TestStreamingResponseDoesNotReuseConnection(t *testing.T) {
	srv := testServer(t, false, "", "")
	seedBothSites(t, srv)
	srv.Svc.Provider.HTTP = &http.Client{Transport: roundTripOKChat{}}
	ts := httptest.NewServer(srv.HTTP.Handler)
	defer ts.Close()

	client := &http.Client{Transport: &http.Transport{MaxIdleConns: 4}}
	defer client.CloseIdleConnections()

	var mu sync.Mutex
	var reused []bool

	send := func() error {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", strings.NewReader(
			`{"model":"auto","stream":true,"max_tokens":4,"messages":[{"role":"user","content":"hi"}]}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			reused = append(reused, info.Reused)
			mu.Unlock()
		}}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, raw)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := send(); err != nil {
		t.Fatalf("第一个请求失败: %v", err)
	}
	if err := send(); err != nil {
		t.Fatalf("第二个请求失败（可能是复用了已关闭的连接）: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(reused) < 2 {
		t.Fatalf("GotConn 调用次数=%d want >=2", len(reused))
	}
	for i, r := range reused {
		if r {
			t.Fatalf("第 %d 次连接尝试复用了旧连接（SSE 应禁止 keep-alive）", i+1)
		}
	}
}
