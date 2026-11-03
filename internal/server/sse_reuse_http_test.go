package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

// 核对 #51 的普通直连和反代链路；不把强制 close 本身当成隧道故障复现。
func TestSequentialSSERequestsOverHTTP(t *testing.T) {
	s := testServer(t, false, "", "")
	seedBothSites(t, s)
	s.Svc.Provider.HTTP = &http.Client{Transport: &responsesUpstreamTransport{}}
	backend := httptest.NewServer(s.HTTP.Handler)
	defer backend.Close()
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(target))
	defer proxy.Close()
	for _, srv := range []*httptest.Server{backend, proxy} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
			for i := range 2 {
				body := `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}],"input":"hi"}`
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				reused := false
				req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
					GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused },
				}))
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "Hello") || !strings.Contains(string(raw), "[DONE]") {
					t.Fatalf("%s: status=%d err=%v body=%s", path, resp.StatusCode, err, raw)
				}
				if i == 1 && !reused {
					t.Fatalf("%s: second request did not exercise keep-alive reuse", path)
				}
			}
		}
	}
}
