package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
)

type rateLimitTransport struct {
	calls atomic.Int32
	reset string
}

func (tr *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, "chat") {
		return (&responsesUpstreamTransport{}).RoundTrip(req)
	}
	tr.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Request: req,
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":6004,"message":%q}`, tr.reset))),
	}, nil
}

func TestModelCooldownIsNotBypassedOverHTTP(t *testing.T) {
	for _, tc := range []struct {
		name string
		wait time.Duration
		text string
	}{
		{"english-short", 20 * time.Minute, "usage will reset at %s, alternatively, switch models"},
		{"chinese-long", 21 * time.Hour, "您的使用量已超出频率限制，将在 %s 重置，您也可以切换其他模型继续使用。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t, false, "", "")
			seedBothSites(t, s)
			if _, _, err := s.Svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
				Label: "second", Site: "domestic", BearerToken: "token-second", Enabled: true,
			})); err != nil {
				t.Fatal(err)
			}
			tr := &rateLimitTransport{reset: fmt.Sprintf(tc.text, time.Now().Add(tc.wait).UTC().Format(time.RFC3339))}
			s.Svc.Provider.HTTP = &http.Client{Transport: tr}
			srv := httptest.NewServer(s.HTTP.Handler)
			defer srv.Close()
			srv.Client().Timeout = 5 * time.Second
			send := func(model string, stream bool) (*http.Response, string) {
				t.Helper()
				body := fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, model, stream)
				resp, err := srv.Client().Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				return resp, string(raw)
			}
			resp, raw := send("auto", false)
			if resp.StatusCode != http.StatusBadGateway || !strings.Contains(raw, "6004") || tr.calls.Load() != 2 {
				t.Fatalf("initial failure lost: status=%d calls=%d body=%s", resp.StatusCode, tr.calls.Load(), raw)
			}
			resp, raw = send("auto", false)
			if tr.calls.Load() != 2 {
				t.Fatalf("all-cooling fallback hit upstream before reset: calls=%d body=%s", tr.calls.Load(), raw)
			}
			seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
			if err != nil || seconds < int((tc.wait-time.Minute)/time.Second) || !strings.Contains(raw, "rate_limited") {
				t.Fatalf("reset advice missing: header=%v body=%s", resp.Header, raw)
			}
			_, raw = send("auto", true)
			if tr.calls.Load() != 2 || !strings.Contains(raw, "retry_after") || !strings.Contains(raw, "[DONE]") {
				t.Fatalf("stream cooldown lost: calls=%d body=%s", tr.calls.Load(), raw)
			}
			s.Svc.Provider.HTTP = &http.Client{Transport: &responsesUpstreamTransport{}}
			resp, raw = send("other-model", false)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("other model incorrectly blocked: status=%d body=%s", resp.StatusCode, raw)
			}
			store, err := s.Svc.Pool.Read()
			if err != nil {
				t.Fatal(err)
			}
			for _, account := range store.Accounts {
				if _, _, err := s.Svc.Pool.UpdateAccount(account.ID, func(a accounts.Account) accounts.Account {
					a.ModelCooldowns = map[string]int64{"auto": time.Now().Add(-time.Second).UnixMilli()}
					return a
				}); err != nil {
					t.Fatal(err)
				}
			}
			resp, raw = send("auto", false)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expired model cooldown did not recover: status=%d body=%s", resp.StatusCode, raw)
			}
		})
	}
}
