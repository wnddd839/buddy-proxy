package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

func TestAccountChatTestResultJSONOmitsZero(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(AccountChatTestResult{
		AccountID: "a1",
		Site:      "domestic",
		Model:     "auto",
		Message:   "fail",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["ok"]; ok {
		t.Fatalf("false ok should be omitted: %s", raw)
	}
	if _, ok := decoded["latencyMs"]; ok {
		t.Fatalf("zero latencyMs should be omitted: %s", raw)
	}
}

func TestTestAccountChatPinsAccountWithoutPoolSideEffects(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/accounts.json"
	svc := New(config.Config{Site: "domestic", AccountsPath: path}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	bad, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "bad", Site: "domestic", BearerToken: "token-bad", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-bad"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "good", Site: "domestic", BearerToken: "token-good", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u-good"},
	}))
	if err != nil {
		t.Fatal(err)
	}

	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-bad":  http.StatusTooManyRequests,
		"token-good": http.StatusOK,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	out := svc.TestAccountChat(context.Background(), bad, "auto")
	if out.OK {
		t.Fatalf("expected failure, got %+v", out)
	}
	if out.AccountID != bad.ID {
		t.Fatalf("accountId=%q want %q", out.AccountID, bad.ID)
	}
	if out.Model != "auto" {
		t.Fatalf("model=%q", out.Model)
	}
	if !strings.Contains(out.Message, "429") && !strings.Contains(strings.ToLower(out.Message), "too many") {
		t.Fatalf("message=%q", out.Message)
	}

	chat, _ := transport.snapshot()
	if len(chat) != 1 || chat[0] != "token-bad" {
		t.Fatalf("expected only pinned account call, got %v", chat)
	}

	store, err := svc.Pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, acc := range store.Accounts {
		if acc.ID != bad.ID {
			continue
		}
		if acc.FailedRequests != 0 {
			t.Fatalf("test must not MarkResult: failedRequests=%d", acc.FailedRequests)
		}
		if acc.CooldownUntil != 0 {
			t.Fatalf("test must not set cooldown: %d", acc.CooldownUntil)
		}
	}
}

func TestTestAccountChatSuccessReportsLatency(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	acc, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "ok", Site: "domestic", BearerToken: "token-ok", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "u1"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	svc.Provider.HTTP = &http.Client{Transport: &scriptedChatTransport{}}

	out := svc.TestAccountChat(context.Background(), acc, "cheap-model")
	if !out.OK {
		t.Fatalf("expected ok: %+v", out)
	}
	if out.LatencyMs < 0 {
		t.Fatalf("latencyMs=%d", out.LatencyMs)
	}
	if out.Model != "cheap-model" {
		t.Fatalf("model=%q", out.Model)
	}
	if out.Message == "" {
		t.Fatal("expected success message")
	}
}

func TestRunPoolChatTestSerialSummaryAndSkip(t *testing.T) {
	dir := t.TempDir()
	svc := New(config.Config{Site: "domestic", AccountsPath: dir + "/accounts.json"}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { _ = svc.Close() })

	_, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "a", Site: "domestic", BearerToken: "token-a", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "ua"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "b", Site: "domestic", BearerToken: "token-b", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "ub"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	disabled, _, err := svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "disabled", Site: "domestic", BearerToken: "token-d", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "ud"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Pool.SetEnabled(disabled.ID, false); err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Pool.Upsert(accounts.CreateAccount(accounts.Account{
		Label: "other-site", Site: "global", BearerToken: "token-g", Enabled: true,
		AuthStatus: accounts.AuthStatus{UserID: "ug"},
	}))
	if err != nil {
		t.Fatal(err)
	}

	transport := &scriptedChatTransport{byAuth: map[string]int{
		"token-a": http.StatusOK,
		"token-b": http.StatusUnauthorized,
	}}
	svc.Provider.HTTP = &http.Client{Transport: transport}

	// Gap < 0 disables the production 350ms spacing.
	out := svc.RunPoolChatTest(context.Background(), "domestic", "auto", RunPoolChatTestOptions{Gap: -1})
	if out.OK {
		t.Fatalf("batch should fail when any account fails: %+v", out)
	}
	if out.PoolSite != "domestic" {
		t.Fatalf("poolSite=%q", out.PoolSite)
	}
	if out.Model != "auto" {
		t.Fatalf("model=%q", out.Model)
	}
	if out.Summary.Total != 2 || out.Summary.Passed != 1 || out.Summary.Failed != 1 {
		t.Fatalf("summary=%+v", out.Summary)
	}
	chat, _ := transport.snapshot()
	if len(chat) != 2 {
		t.Fatalf("expected 2 chat calls, got %v", chat)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	skipped := svc.RunPoolChatTest(ctx, "domestic", "auto", RunPoolChatTestOptions{Gap: -1})
	if skipped.Summary.Skipped == 0 {
		t.Fatalf("expected skipped when deadline exhausted: %+v", skipped)
	}
}
