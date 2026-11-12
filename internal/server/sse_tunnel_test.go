package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 经隧道/反代的 SSE 必须关闭连接；直连保持 keep-alive，不给本地用户增加建连开销。
func TestSSEConnectionCloseOnlyViaTunnel(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		body   string
		header http.Header
		want   bool
	}{
		{"chat direct", "/v1/chat/completions", `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`, nil, false},
		{"chat tunnel", "/v1/chat/completions", `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`, http.Header{"X-Forwarded-For": {"1.2.3.4"}}, true},
		{"responses direct", "/v1/responses", `{"model":"auto","input":"hi","stream":true}`, nil, false},
		{"responses tunnel", "/v1/responses", `{"model":"auto","input":"hi","stream":true}`, http.Header{"X-Forwarded-Host": {"proxy.example"}}, true},
		{"agentic tunnel", "/v1/agentic_search", `{"query":"q","stream":true}`, http.Header{"Forwarded": {"for=1.2.3.4"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServer(t, false, "", "")
			seedBothSites(t, srv)
			srv.Svc.Provider.HTTP = &http.Client{Transport: roundTripOKChat{}}
			ts := httptest.NewServer(srv.HTTP.Handler)
			defer ts.Close()

			req, err := http.NewRequest(http.MethodPost, ts.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			for k, values := range tc.header {
				for _, v := range values {
					req.Header.Add(k, v)
				}
			}
			resp, err := (&http.Client{}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.Close != tc.want {
				t.Fatalf("resp.Close=%v want %v; Connection=%q status=%d",
					resp.Close, tc.want, resp.Header.Get("Connection"), resp.StatusCode)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", resp.StatusCode)
			}
		})
	}
}
