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
	// 站内换号已删除：只用一个号，验证「先失败一次无关请求、再空回合，
	// 空回合的重放预算不会被那次无关失败吃掉」。
	if _, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Site: "domestic", BearerToken: "token-a", Enabled: true,
	})); err != nil {
		t.Fatal(err)
	}
	transport := &scriptedChatTransport{
		emptyTurnOnceByAuth: map[string]bool{"token-a": true},
	}
	svc.Provider.HTTP = &http.Client{Transport: transport}
	result, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil || result.Result.Turn.Text != "ok" {
		t.Fatalf("空回合重放应成功: result=%+v err=%v", result.Result.Turn, err)
	}
	calls, _ := transport.snapshot()
	if len(calls) != 2 {
		t.Fatalf("应同账号重放 1 次（共 2 发），实际 %v", calls)
	}
	for _, tok := range calls {
		if tok != "token-a" {
			t.Fatalf("不应换号，实际 %v", calls)
		}
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
