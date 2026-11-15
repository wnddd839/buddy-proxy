package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/openai"
)

// searchOutcomeTransport 驱动一轮内置搜索闭环：
//   - 第 1 轮 chat 回 web_search 工具调用，触发 prepareBuiltinSearch 的第 2 轮；
//   - /agenttool/v1/search 回一条检索结果；
//   - 第 2 轮 chat 默认回正常答案；failSecondRound=true 时回 500/11140，
//     该错误不触发降级 / 换号 / 传输重放 / oauth 刷新，直接冒泡。
type searchOutcomeTransport struct {
	mu              sync.Mutex
	chats           int
	failSecondRound bool
}

func (t *searchOutcomeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	if strings.Contains(req.URL.Path, "/agenttool/v1/search") {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Status: http.StatusText(http.StatusOK), Header: header,
			Body: io.NopCloser(strings.NewReader(
				`{"query":"q","type":"text2text","total_results":1,"results":[{"title":"T","url":"https://example.test","snippet":"S"}]}`)),
			Request: req,
		}, nil
	}
	if !strings.Contains(req.URL.Path, "chat") {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Status: http.StatusText(http.StatusOK), Header: header,
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{}}`)), Request: req,
		}, nil
	}
	t.mu.Lock()
	t.chats++
	round := t.chats
	t.mu.Unlock()
	if round == 2 && t.failSecondRound {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusInternalServerError, Status: http.StatusText(http.StatusInternalServerError), Header: header,
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"upstream boom (code 11140)"}}`)), Request: req,
		}, nil
	}
	header.Set("Content-Type", "text/event-stream")
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\ndata: [DONE]\n\n"
	if round == 1 {
		body = "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"" +
			openai.BuiltinWebSearchToolName + "\",\"arguments\":\"{\\\"query\\\":\\\"golang\\\"}\"}}]}}]}\n\ndata: [DONE]\n\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK, Status: http.StatusText(http.StatusOK), Header: header,
		Body: io.NopCloser(strings.NewReader(body)), Request: req,
	}, nil
}

func (t *searchOutcomeTransport) chatCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chats
}

func searchOutcomeServer(t *testing.T, upstream *searchOutcomeTransport) *Server {
	t.Helper()
	srv := testServer(t, true, "", "cbp_test")
	seedBothSites(t, srv)
	srv.Svc.Provider.HTTP = &http.Client{Transport: upstream}
	return srv
}

func searchOutcomeOpts(outcome *gateway.SystemPromptOutcome) gateway.CompleteOptions {
	return gateway.CompleteOptions{
		Model: "auto",
		Messages: []map[string]any{
			{"role": "system", "content": "You are a test assistant."},
			{"role": "user", "content": "latest go release"},
		},
		SystemOutcome: outcome,
	}
}

// TestPrepareBuiltinSearchUsesIndependentSystemOutcome 锁住问题 #5 的结构根因：
// 两轮 CompleteFromPool 不能共享调用方那个 SystemOutcome 指针。未修复版本里
// roundOpts := opts 按值拷贝但指针共享，第 2 轮覆写第 1 轮、Degraded 只置不清，
// 最终 07 用量行拿到「最后一轮形态 + 期间是否降级」的混合值，06 面板里根本
// 没有对应的事件（同一 requestId 无法互查）。
func TestPrepareBuiltinSearchUsesIndependentSystemOutcome(t *testing.T) {
	upstream := &searchOutcomeTransport{}
	srv := searchOutcomeServer(t, upstream)

	callerOutcome := &gateway.SystemPromptOutcome{}
	opts := searchOutcomeOpts(callerOutcome)

	roundOpts, result, err := srv.prepareBuiltinSearch(context.Background(), opts, opts.Site)
	if err != nil {
		t.Fatalf("内置搜索应成功，实际 %v", err)
	}
	if result == nil {
		t.Fatal("应返回最终结果")
	}
	if got := upstream.chatCount(); got != 2 {
		t.Fatalf("应跑 2 轮 chat，实际 %d（测试基线失效）", got)
	}
	// 修复后返回的 roundOpts 携带「最终一轮」的独立 outcome 指针。
	if roundOpts.SystemOutcome == nil {
		t.Fatal("返回的 roundOpts.SystemOutcome 不应为 nil")
	}
	if roundOpts.SystemOutcome == callerOutcome {
		t.Fatal("prepareBuiltinSearch 仍与调用方共享同一个 SystemOutcome 指针（未修复：两轮会串写）")
	}
	// 每轮用新指针 ⇒ 调用方原始指针从未被 prepareBuiltinSearch 写入。
	if callerOutcome.Form != "" || callerOutcome.Fingerprint != "" || callerOutcome.Degraded {
		t.Fatalf("调用方 SystemOutcome 被 prepareBuiltinSearch 串写: %+v", callerOutcome)
	}
	// 最终一轮的 outcome 必须已被 CompleteFromPool 回填（写回 07 用量行才有值）。
	if roundOpts.SystemOutcome.Form == "" || roundOpts.SystemOutcome.Fingerprint == "" {
		t.Fatalf("最终一轮 outcome 未回填: %+v", roundOpts.SystemOutcome)
	}
}

// TestCompleteWithRequestedSearchBackfillsOutcome 锁住「最终形态写回调用方」：
// 调用方按值传 opts、仍用自己那份 opts 去 recordChatJournal，只有共享指针被
// 写回，07 用量行才能与 06 面板按同一 requestId 互查。成功 / 失败路径都要回填，
// 否则失败时 07 行显示空。
func TestCompleteWithRequestedSearchBackfillsOutcome(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		upstream := &searchOutcomeTransport{}
		srv := searchOutcomeServer(t, upstream)
		callerOutcome := &gateway.SystemPromptOutcome{}
		opts := searchOutcomeOpts(callerOutcome)

		if _, err := srv.completeWithRequestedSearch(context.Background(), opts, true); err != nil {
			t.Fatalf("应成功，实际 %v", err)
		}
		if got := upstream.chatCount(); got != 2 {
			t.Fatalf("应跑 2 轮 chat，实际 %d", got)
		}
		if callerOutcome.Form == "" || callerOutcome.Fingerprint == "" {
			t.Fatalf("调用方 outcome 未被回填（07 行会显示空）: %+v", callerOutcome)
		}
		// 本用例无 11128 降级，不应出现「期间发生过降级」的混合残留值。
		if callerOutcome.Degraded {
			t.Fatalf("调用方 outcome 出现 Degraded 混合残留: %+v", callerOutcome)
		}
	})

	t.Run("failure", func(t *testing.T) {
		upstream := &searchOutcomeTransport{failSecondRound: true}
		srv := searchOutcomeServer(t, upstream)
		callerOutcome := &gateway.SystemPromptOutcome{}
		opts := searchOutcomeOpts(callerOutcome)

		if _, err := srv.completeWithRequestedSearch(context.Background(), opts, true); err == nil {
			t.Fatal("第 2 轮失败应返回错误")
		}
		if got := upstream.chatCount(); got != 2 {
			t.Fatalf("应跑 2 轮 chat，实际 %d", got)
		}
		// 关键回归点：失败路径 recordChatJournal 拿到的正是调用方 opts，
		// 其 SystemOutcome 必须已被写回最终一轮形态，07 行不能显示空。
		if callerOutcome.Form == "" || callerOutcome.Fingerprint == "" {
			t.Fatalf("失败路径调用方 outcome 未回填（07 行会显示空）: %+v", callerOutcome)
		}
	})
}

// TestHandleResponsesWithSearchJournalCarriesOutcome 走完整非流式 HTTP 路径，
// 断言最终写入 07 用量明细的 SystemFp 非空且与最终一轮形态一致，
// 且不残留第 1 轮的 Degraded 混合值。
func TestHandleResponsesWithSearchJournalCarriesOutcome(t *testing.T) {
	upstream := &searchOutcomeTransport{}
	srv := searchOutcomeServer(t, upstream)
	body := `{"model":"auto","instructions":"You are a test assistant.","input":"latest go release","tools":[{"type":"web_search"}],"stream":false}`
	req := newAuthRequest(http.MethodPost, "/v1/responses", body, "cbp_test")
	rec := doRequest(srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := upstream.chatCount(); got != 2 {
		t.Fatalf("应跑 2 轮 chat，实际 %d", got)
	}
	view := srv.Svc.Journal.View("all", 10, 0)
	if len(view.Requests) != 1 {
		t.Fatalf("应写 1 条用量明细，实际 %d", len(view.Requests))
	}
	entry := view.Requests[0]
	if entry.SystemForm == "" || entry.SystemFp == "" {
		t.Fatalf("07 用量行缺少 system 形态（与 06 面板无法按 requestId 互查）: %+v", entry)
	}
	if entry.SystemDegraded {
		t.Fatalf("07 用量行出现 Degraded 混合残留: %+v", entry)
	}
}
