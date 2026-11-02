package gateway

import (
	"net/http"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

func TestEmptyReplayBudgetSurvivesUnrelatedFailure(t *testing.T) {
	svc := New(config.Config{Site: "domestic", AccountsPath: t.TempDir() + "/accounts.json"}, nil)
	t.Cleanup(func() { _ = svc.Close() })
	for _, token := range []string{"token-a", "token-b", "token-c"} {
		if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
			Site: "domestic", BearerToken: token, Enabled: true,
		})); err != nil {
			t.Fatal(err)
		}
	}
	transport := &scriptedChatTransport{
		byAuth:        map[string]int{"token-a": http.StatusTooManyRequests},
		rawBodyByAuth: map[string]string{"token-b": sseEmptyFinish},
	}
	svc.Provider.HTTP = &http.Client{Transport: transport}
	result, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil || result.Result.Turn.Text != "ok" {
		t.Fatalf("empty replay was consumed by 429: result=%+v err=%v", result.Result.Turn, err)
	}
	calls, _ := transport.snapshot()
	if strings.Join(calls, ",") != "token-a,token-b,token-c" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestDeliveredContentNeverReplaysUpstreamError(t *testing.T) {
	for _, message := range []string{"transport error: connection reset by peer", "401 unauthorized token expired", "429 too many requests"} {
		t.Run(message, func(t *testing.T) {
			svc := New(config.Config{Site: "domestic", AccountsPath: t.TempDir() + "/accounts.json"}, nil)
			t.Cleanup(func() { _ = svc.Close() })
			for _, token := range []string{"token-a", "token-b"} {
				if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
					Site: "domestic", BearerToken: token, Enabled: true,
				})); err != nil {
					t.Fatal(err)
				}
			}
			transport := &scriptedChatTransport{rawBodyByAuth: map[string]string{
				"token-a": "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
					"data: {\"error\":{\"message\":\"" + message + "\"}}\n\n",
			}}
			svc.Provider.HTTP = &http.Client{Transport: transport}
			var delivered strings.Builder
			_, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
				Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
				OnDelta: func(s string) { delivered.WriteString(s) },
				OnEvent: func(provider.Event) {},
			})
			calls, _ := transport.snapshot()
			if err == nil || delivered.String() != "hi" || len(calls) != 1 {
				t.Fatalf("replayed a delivered stream: output=%q calls=%v err=%v", delivered.String(), calls, err)
			}
		})
	}
}
