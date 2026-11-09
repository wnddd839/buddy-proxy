package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
	"github.com/wnddd839/codebuddy-proxy/internal/oauth"
)

func TestPR66RouteAppliedOnlyOnceOnRetry(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "alias", Site: "global", Target: "upstream"},
		{FromSite: "global", Match: "upstream", Site: "global", Target: "other"},
	})
	tr := &scriptedChatTransport{resetOnceByAuth: map[string]string{"token-g1": ""}}
	svc.Provider.HTTP = &http.Client{Transport: tr}
	_, err := svc.CompleteFromPool(t.Context(), CompleteOptions{Site: "domestic", Model: "alias", Messages: []map[string]any{{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.chatBodies) != 2 {
		t.Fatalf("expected transport retry, got %d attempts", len(tr.chatBodies))
	}
	for i, body := range tr.chatBodies {
		var payload map[string]any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "upstream" {
			t.Fatalf("attempt %d changed routed model: got %v want upstream", i+1, payload["model"])
		}
	}
}

func TestPR66QuotaFloorCannotBeBypassed(t *testing.T) {
	for _, reset := range []int64{0, time.Now().Add(time.Hour).UnixMilli()} {
		for _, mode := range []string{"rotation", "soft-pin", "fixed"} {
			t.Run(fmt.Sprintf("reset=%t/%s", reset > 0, mode), func(t *testing.T) {
				svc := routeFixture(t, nil)
				store, err := svc.Pool.Read()
				if err != nil {
					t.Fatal(err)
				}
				for _, a := range store.Accounts {
					if _, err := svc.Pool.ApplyQuotaState(a.ID, new(float64(5)), false, reset, time.Now().UnixMilli()); err != nil {
						t.Fatal(err)
					}
				}
				svc.Pool.SetQuotaFloor(10)
				opts := CompleteOptions{Site: "domestic", Model: "auto", SessionKey: "floor-test", Messages: []map[string]any{{"role": "user", "content": "hi"}}}
				if mode == "soft-pin" {
					svc.Pins.Remember(SessionPinKey(opts.SessionKey, opts.Site), store.Accounts[0].ID)
				}
				if mode == "fixed" {
					opts.FixedAccount = true
					opts.AccountID = store.Accounts[0].ID
				}
				_, err = svc.CompleteFromPool(t.Context(), opts)
				if err == nil || len(sentTokens(t, svc)) != 0 {
					t.Fatalf("below-floor account served chat: err=%v tokens=%v", err, sentTokens(t, svc))
				}
			})
		}
	}
}

func TestPR66QuotaFloorSnapshotPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining *float64
		age       time.Duration
		unlimited bool
	}{
		{"exactly at floor", new(float64(10)), 0, false},
		{"unknown", nil, 0, false},
		{"stale", new(float64(5)), 7 * time.Hour, false},
		{"unlimited", new(float64(5)), 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := routeFixture(t, nil)
			store, err := svc.Pool.Read()
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range store.Accounts {
				if _, err := svc.Pool.ApplyQuotaState(a.ID, tc.remaining, tc.unlimited, 0, time.Now().Add(-tc.age).UnixMilli()); err != nil {
					t.Fatal(err)
				}
			}
			svc.Pool.SetQuotaFloor(10)
			if _, err := svc.CompleteFromPool(t.Context(), CompleteOptions{Site: "domestic", Model: "auto", Messages: []map[string]any{{"role": "user", "content": "hi"}}}); err != nil {
				t.Fatal(err)
			}
			if len(sentTokens(t, svc)) != 1 {
				t.Fatal("eligible account did not serve chat")
			}
		})
	}
}

func TestPR66RouteFallbackUsesOriginalChain(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "alias", Site: "global", Target: "unavailable"},
		{FromSite: "domestic", Match: "alias", Site: "domestic", Target: "backup"},
		{FromSite: "domestic", Match: "backup", Site: "global", Target: "must-not-follow"},
	})
	store, err := svc.Pool.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range store.Accounts {
		if a.Site == "global" {
			if _, _, err := svc.Pool.SetEnabled(a.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := svc.CompleteFromPool(t.Context(), CompleteOptions{Site: "domestic", Model: "alias", Messages: []map[string]any{{"role": "user", "content": "hi"}}}); err != nil {
		t.Fatal(err)
	}
	tr := svc.Provider.HTTP.Transport.(*scriptedChatTransport)
	if len(tr.chatBodies) != 1 {
		t.Fatalf("unexpected calls: %v", tr.chatBodies)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(tr.chatBodies[0]), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "backup" {
		t.Fatalf("model=%v want backup", payload["model"])
	}
}

func TestPR66RefreshUsesOAuthProxy(t *testing.T) {
	svc := routeFixture(t, nil)
	ordinary, dedicated := 0, 0
	svc.Provider.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		ordinary++
		return nil, errors.New("ordinary transport called")
	})}
	svc.oauthGlobal = oauth.NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		dedicated++
		return nil, errors.New("dedicated transport called")
	})})
	_, _ = svc.refreshSelected(t.Context(), accounts.Selection{Account: accounts.Account{ID: "g", Site: "global", BearerToken: "token", RefreshToken: "refresh"}}, true)
	if ordinary != 0 || dedicated == 0 {
		t.Fatalf("refresh transport: ordinary=%d dedicated=%d", ordinary, dedicated)
	}
}
