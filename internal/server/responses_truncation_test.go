package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type truncatedResponsesTransport struct {
	calls int
}

func (t *truncatedResponsesTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, "chat") {
		return (&responsesUpstreamTransport{}).RoundTrip(req)
	}
	t.calls++
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"run\",\"arguments\":\"{\\\"cmd\\\":\\\"ls\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req,
	}, nil
}

func TestResponsesTruncatedDeliveredToolFailsWithoutReplay(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	seedBothSites(t, srv)
	transport := &truncatedResponsesTransport{}
	srv.Svc.Provider.HTTP = &http.Client{Transport: transport}
	rec := doRequest(srv, newAuthRequest(http.MethodPost, "/v1/responses",
		`{"model":"auto","input":"hi","stream":true}`, "secret-key"))
	raw := rec.Body.String()
	if !strings.Contains(raw, "event: response.function_call_arguments.delta") ||
		!strings.Contains(raw, "event: response.failed") {
		t.Fatalf("missing delta or failure: %s", raw)
	}
	if strings.Contains(raw, "event: response.completed") ||
		strings.Contains(raw, "event: response.function_call_arguments.done") || transport.calls != 1 {
		t.Fatalf("truncated tool completed or replayed (calls=%d): %s", transport.calls, raw)
	}
}
