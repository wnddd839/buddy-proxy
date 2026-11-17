package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/modelroute"
)

const testDSHIdentitySentence = "You are an AI agent powered by DeepSeek Harness."

type retryStripTransport struct {
	mu     sync.Mutex
	calls  int
	onCall func(call int, req *http.Request, body string) (*http.Response, error)
}

func (t *retryStripTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	path := req.URL.Path
	if strings.Contains(path, "get-user-resource") {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Status: "OK", Header: header,
			Body: io.NopCloser(strings.NewReader(`{"code":1,"msg":"no fixture"}`)), Request: req,
		}, nil
	}
	if strings.Contains(path, "get-dosage-notify") {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Status: "OK", Header: header,
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"dosageNotifyCode":0}}`)), Request: req,
		}, nil
	}
	var raw []byte
	if req.Body != nil {
		raw, _ = io.ReadAll(req.Body)
	}
	t.mu.Lock()
	t.calls++
	call := t.calls
	t.mu.Unlock()
	return t.onCall(call, req, string(raw))
}

func sseOKResponse(req *http.Request) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	return &http.Response{
		StatusCode: http.StatusOK, Status: "OK", Header: header,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")),
		Request: req,
	}
}

// TestStripReportDoesNotDoubleOnSameSiteRetry 验证同区传输层重试时，
// stripReport 不会跨重试累加（48 -> 96）。
func TestStripReportDoesNotDoubleOnSameSiteRetry(t *testing.T) {
	tr := &retryStripTransport{
		onCall: func(call int, req *http.Request, _ string) (*http.Response, error) {
			if call == 1 {
				return nil, io.ErrUnexpectedEOF
			}
			return sseOKResponse(req), nil
		},
	}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeStrip, tr)
	outcome := &SystemPromptOutcome{}
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": testDSHIdentitySentence + "\n\nkeep rules"},
			{"role": "user", "content": "hi"},
		},
		SystemOutcome: outcome,
	})
	if err != nil {
		t.Fatalf("重连后应成功: %v", err)
	}
	wantBytes := len(testDSHIdentitySentence) + 2 // 含紧随的两个换行分隔符
	if !outcome.Stripped || outcome.StrippedBytes != wantBytes {
		t.Fatalf("StrippedBytes 不得翻倍：got (%v, %d) want (true, %d)",
			outcome.Stripped, outcome.StrippedBytes, wantBytes)
	}
	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 2 {
		t.Fatalf("传输重试应记录 2 条事件（首试失败 + 重试成功），实际 %d", len(events))
	}
	for _, ev := range events {
		if !ev.Stripped || ev.StrippedBytes != wantBytes {
			t.Fatalf("事件 StrippedBytes 应为 %d，实际 %+v", wantBytes, ev)
		}
	}
}

// TestStripReportDoesNotLeakAcrossSiteFallback 验证国内站剥离后因额度耗尽
// 跳 onExhausted 到国际站重试时，国际站未剥离则不得继承国内站的 Stripped=true/Bytes=48。
func TestStripReportDoesNotLeakAcrossSiteFallback(t *testing.T) {
	svc := routeFixture(t, []modelroute.Rule{
		{FromSite: "domestic", Match: "auto", Site: "global", Target: "auto", Mode: modelroute.ModeOnExhausted},
	})
	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeStrip})
	svc.Provider.HTTP = &http.Client{
		Transport: &retryStripTransport{
			onCall: func(_ int, req *http.Request, body string) (*http.Response, error) {
				auth := req.Header.Get("Authorization")
				if strings.Contains(auth, "token-d1") {
					header := make(http.Header)
					header.Set("Content-Type", "application/json")
					return &http.Response{
						StatusCode: http.StatusTooManyRequests, Status: "Too Many Requests", Header: header,
						Body:    io.NopCloser(strings.NewReader(`{"error":{"message":"model rate limit (code 6004)","code":6004}}`)),
						Request: req,
					}, nil
				}
				// 国际站不应剥离 DeepSeek Harness 句
				msgs := chatBodyMessages(t, body)
				if !strings.Contains(messageContentText(msgs[0]["content"]), testDSHIdentitySentence) {
					t.Fatalf("国际站不应剥离 DeepSeek 身份句，实际 %v", msgs[0]["content"])
				}
				return sseOKResponse(req), nil
			},
		},
	}

	outcome := &SystemPromptOutcome{}
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": testDSHIdentitySentence},
			{"role": "user", "content": "hi"},
		},
		SystemOutcome: outcome,
	})
	if err != nil {
		t.Fatalf("跨区兜底应成功: %v", err)
	}
	if outcome.Stripped || outcome.StrippedBytes != 0 {
		t.Fatalf("国际站未剥离时不得残留国内站假阳性：got %+v", outcome)
	}
}

// TestStripModeDowngradesOn11128 验证 strip 模式遇到黑名单未收录的新 11128 句子时，
// 像 auto 一样自动折叠重放一次并成功。
func TestStripModeDowngradesOn11128(t *testing.T) {
	const unknownMarker = "You are Unknown Competitor CLI."
	tr := &retryStripTransport{
		onCall: func(call int, req *http.Request, body string) (*http.Response, error) {
			msgs := chatBodyMessages(t, body)
			if call == 1 {
				if role, _ := msgs[0]["role"].(string); role != "system" || !strings.Contains(messageContentText(msgs[0]["content"]), unknownMarker) {
					t.Fatalf("首试应直传未知 system，实际 %v", msgs)
				}
				header := make(http.Header)
				header.Set("Content-Type", "application/json")
				return &http.Response{
					StatusCode: http.StatusBadRequest, Status: "Bad Request", Header: header,
					Body:    io.NopCloser(strings.NewReader(`{"error":{"message":"request illegal (code 11128)","code":11128}}`)),
					Request: req,
				}, nil
			}
			// 第二次应折叠为 fold
			if !strings.Contains(messageContentText(msgs[1]["content"]), "<client_instructions>") {
				t.Fatalf("第二次重试应已折叠，实际 %v", msgs)
			}
			return sseOKResponse(req), nil
		},
	}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeStrip, tr)
	outcome := &SystemPromptOutcome{}
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": unknownMarker},
			{"role": "user", "content": "hi"},
		},
		SystemOutcome: outcome,
	})
	if err != nil {
		t.Fatalf("strip 遇未知 11128 折叠重放应成功: %v", err)
	}
	if !outcome.Degraded || outcome.Form != "fold" || outcome.Stripped || outcome.StrippedBytes != 0 {
		t.Fatalf("outcome 异常: %+v", outcome)
	}
	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 2 {
		t.Fatalf("应记录 2 条事件（被拦截 + 降级重放），实际 %d", len(events))
	}
	if !events[0].Recovered || events[0].Mode != config.SystemPromptModeStrip {
		t.Fatalf("最新事件应为 recovered=true, mode=strip，实际 %+v", events[0])
	}
	if !events[1].Degraded || events[1].ErrorCode != "11128" || events[1].Mode != config.SystemPromptModeStrip {
		t.Fatalf("首条事件应为 degraded=true, errorCode=11128, mode=strip，实际 %+v", events[1])
	}
}
