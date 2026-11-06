package provider_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// completeChatBody runs one upstream chat through a capturing transport and
// returns the JSON body that would be sent to CodeBuddy.
func completeChatBody(t *testing.T, site string, messages []map[string]any) map[string]any {
	t.Helper()
	var raw []byte
	client := provider.NewClient(config.Config{})
	client.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		raw, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}
	_, _ = client.Complete(t.Context(), provider.ChatOptions{
		Model:       "auto",
		Site:        site,
		BearerToken: "tok",
		Messages:    messages,
	})
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode chat body %q: %v", raw, err)
	}
	return body
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func firstRole(t *testing.T, body map[string]any) string {
	t.Helper()
	msgs, _ := body["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("no messages in body: %v", body)
	}
	first, _ := msgs[0].(map[string]any)
	return provider.NormalizeMessageRole(first["role"].(string))
}

func TestGlobalChatWithoutSystemAddsCanonicalSystem(t *testing.T) {
	for _, site := range []string{"global", ""} {
		t.Run("site="+site, func(t *testing.T) {
			body := completeChatBody(t, site, []map[string]any{{"role": "user", "content": "hi"}})
			if got := firstRole(t, body); got != "system" {
				t.Fatalf("first role=%q want system; body=%v", got, body)
			}
		})
	}
}

func TestDomesticChatWithoutSystemUnchanged(t *testing.T) {
	body := completeChatBody(t, "domestic", []map[string]any{{"role": "user", "content": "hi"}})
	if got := firstRole(t, body); got != "user" {
		t.Fatalf("domestic first role=%q want user; body=%v", got, body)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("domestic messages=%d want 1", len(msgs))
	}
}

func TestGlobalChatWithExistingSystemNotDuplicated(t *testing.T) {
	body := completeChatBody(t, "global", []map[string]any{
		{"role": "system", "content": "custom instruction"},
		{"role": "user", "content": "hi"},
	})
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages=%d want 2; body=%v", len(msgs), body)
	}
	if got := firstRole(t, body); got != "system" {
		t.Fatalf("first role=%q want system", got)
	}
}
