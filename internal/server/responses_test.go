package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// responsesUpstreamTransport 模拟上游返回流式文本，验证 /v1/responses 端到端。
type responsesUpstreamTransport struct {
	mu     sync.Mutex
	bodies []string
}

func (t *responsesUpstreamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	if strings.Contains(req.URL.Path, "chat") {
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			t.mu.Lock()
			t.bodies = append(t.bodies, string(raw))
			t.mu.Unlock()
		}
		header.Set("Content-Type", "text/event-stream")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     header,
			Body: io.NopCloser(strings.NewReader(
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
					"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n" +
					"data: [DONE]\n\n")),
			Request: req,
		}, nil
	}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`{"models":[],"data":{}}`)),
		Request:    req,
	}, nil
}

// TestResponsesNonStream_EndToEnd 非流式：Response 对象 + output 数组 + usage。
func TestResponsesNonStream_EndToEnd(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	seedBothSites(t, srv)
	transport := &responsesUpstreamTransport{}
	srv.Svc.Provider.HTTP = &http.Client{Transport: transport}

	body := `{"model":"auto","instructions":"be brief","input":"hi","stream":false}`
	req := newAuthRequest(http.MethodPost, "/v1/responses", body, "secret-key")
	rec := doRequest(srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if resp.Object != "response" || resp.Status != "completed" || !strings.HasPrefix(resp.ID, "resp_") {
		t.Fatalf("response head wrong: %+v", resp)
	}
	if len(resp.Output) == 0 || resp.Output[0].Type != "message" {
		t.Fatalf("output wrong: %+v", resp.Output)
	}
	text := resp.Output[0].Content[0].Text
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "world") {
		t.Fatalf("output text wrong: %q", text)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens == 0 {
		t.Fatalf("usage wrong: %+v", resp.Usage)
	}
	// 上游收到的 messages 必须含 instructions→system + input→user
	if len(transport.bodies) == 0 {
		t.Fatalf("upstream not called")
	}
	upstream := transport.bodies[0]
	if !strings.Contains(upstream, "be brief") || !strings.Contains(upstream, "hi") {
		t.Fatalf("upstream body missing instructions/input: %s", upstream)
	}
}

// TestResponsesStream_EndToEnd 流式：校验 SSE 含命名事件与完整生命周期。
func TestResponsesStream_EndToEnd(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	seedBothSites(t, srv)
	srv.Svc.Provider.HTTP = &http.Client{Transport: &responsesUpstreamTransport{}}

	body := `{"model":"auto","input":"hi","stream":true}`
	req := newAuthRequest(http.MethodPost, "/v1/responses", body, "secret-key")
	rec := doRequest(srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	// Codex 依赖命名事件：event: response.xxx
	for _, want := range []string{
		"event: response.created",
		"event: response.in_progress",
		"event: response.output_item.added",
		"event: response.output_text.delta",
		"event: response.completed",
		"data: [DONE]",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("stream missing %q; body:\n%s", want, raw)
		}
	}
	// response.completed 帧必须携带 usage
	if !strings.Contains(raw, `"response.completed"`) {
		t.Fatalf("missing response.completed; body:\n%s", raw)
	}
}

// TestResponsesStream_FunctionCall 校验 function_call 流式事件。
func TestResponsesStream_FunctionCall(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	seedBothSites(t, srv)
	srv.Svc.Provider.HTTP = &http.Client{Transport: &toolCallUpstreamTransport{}}

	body := `{"model":"auto","input":"run ls","stream":true,"tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}]}`
	req := newAuthRequest(http.MethodPost, "/v1/responses", body, "secret-key")
	rec := doRequest(srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, want := range []string{
		"event: response.output_item.added",
		`"type":"function_call"`,
		`"name":"shell"`,
		"event: response.function_call_arguments.delta",
		"event: response.function_call_arguments.done",
		"event: response.completed",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("stream missing %q; body:\n%s", want, raw)
		}
	}
}

func newAuthRequest(method, path, body, apiKey string) *http.Request {
	req := httptest.NewRequest(method, "http://127.0.0.1:32126"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func doRequest(srv *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(rec, req)
	return rec
}

// toolCallUpstreamTransport 返回一次完整 tool_use。
type toolCallUpstreamTransport struct{}

func (toolCallUpstreamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	if strings.Contains(req.URL.Path, "chat") {
		header.Set("Content-Type", "text/event-stream")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body: io.NopCloser(strings.NewReader(
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"cmd\\\":\\\"ls\\\"}\"}}]}}]}\n\n" +
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n" +
					"data: [DONE]\n\n")),
			Request: req,
		}, nil
	}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`{"models":[],"data":{}}`)),
		Request:    req,
	}, nil
}
