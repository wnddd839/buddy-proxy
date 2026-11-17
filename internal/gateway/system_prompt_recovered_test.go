package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/systemprompt"
)

// TestAutoDowngradeEventsMarkRecovered 锁住 auto 降级链两条事件的 Recovered 标记：
// 被 11128 拦截的首试只标 Degraded，折叠后重放成功才标 Recovered，
// 且两条事件带同一个 RequestID，供前端把它们关联起来。
func TestAutoDowngradeEventsMarkRecovered(t *testing.T) {
	transport := &blockOnceTransport{}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeAuto, transport)

	const requestID = "req-degrade-001"
	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:     "auto",
		RequestID: requestID,
		Messages: []map[string]any{
			{"role": "system", "content": claudeCodeMarker},
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("降级后应成功，实际 %v", err)
	}

	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 2 {
		t.Fatalf("应记 2 条事件（keep/11128 + fold/200），实际 %d：%+v", len(events), events)
	}
	// Recent 最新在前：events[0] 是降级重放成功，events[1] 是被拦截的首试。
	recovered, blocked := events[0], events[1]

	if blocked.Form != "keep" || !blocked.Degraded || blocked.Recovered {
		t.Fatalf("被拦截那条应 Form=keep, Degraded=true, Recovered=false：%+v", blocked)
	}
	if blocked.Status != http.StatusBadRequest || blocked.ErrorCode != "11128" {
		t.Fatalf("被拦截那条应 Status=400, ErrorCode=11128：%+v", blocked)
	}
	if recovered.Form != "fold" || recovered.Status != http.StatusOK || recovered.Degraded {
		t.Fatalf("重放成功那条应 Form=fold, Status=200, Degraded=false：%+v", recovered)
	}
	if !recovered.Recovered {
		t.Fatalf("重放成功那条应 Recovered=true：%+v", recovered)
	}
	// 两条事件必须带同一个 RequestID，供前端关联「被拦截」与「降级成功」。
	if blocked.RequestID != requestID || recovered.RequestID != requestID || blocked.RequestID != recovered.RequestID {
		t.Fatalf("两条事件 RequestID 应相同且为 %q，实际 %q vs %q", requestID, blocked.RequestID, recovered.RequestID)
	}
	// IsProblem 保持不变：Recovered 的成功事件不该被算成「问题」。
	if recovered.IsProblem() {
		t.Fatalf("Recovered 成功事件不应被 IsProblem 判为问题：%+v", recovered)
	}
}

// TestFoldModeNoDowngradeNotRecovered 防止把普通 fold 成功误标成「降级恢复」。
func TestFoldModeNoDowngradeNotRecovered(t *testing.T) {
	transport := &blockOnceTransport{neverBlock: true}
	svc := newModeSnapshotTestService(t, config.SystemPromptModeFold, transport)

	_, err := svc.CompleteFromPool(context.Background(), CompleteOptions{
		Model:     "auto",
		RequestID: "req-fold-normal",
		Messages: []map[string]any{
			{"role": "system", "content": "SYSTEM-RAW-TEXT"},
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("fold 模式正常请求应成功，实际 %v", err)
	}
	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 1 {
		t.Fatalf("应只记 1 条事件，实际 %d：%+v", len(events), events)
	}
	if events[0].Recovered {
		t.Fatalf("fold 模式正常请求（无降级）不得标 Recovered：%+v", events[0])
	}
	if events[0].Form != "fold" || events[0].Status != http.StatusOK {
		t.Fatalf("应为 fold/200，实际 %+v", events[0])
	}
}

// TestRecordSystemPromptAttemptRecoveredOnlyOnSuccessfulReplay 直接覆盖
// recordSystemPromptAttempt 的边界：FoldedRetry 的重放也可能再次失败，
// 那种情况绝不能置 Recovered；普通首试成功同样不能置。
func TestRecordSystemPromptAttemptRecoveredOnlyOnSuccessfulReplay(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	account := accounts.Account{ID: "acc-1"}
	att := systemAttempt{started: time.Now(), keepForm: true, fp: "deadbeef", size: 8, mode: config.SystemPromptModeAuto}

	// 降级重放成功：FoldedRetry=true 且 callErr==nil ⇒ Recovered=true。
	svc.recordSystemPromptAttempt(
		CompleteOptions{RequestID: "r-ok", Model: "auto", FoldedRetry: true},
		account, "domestic", att, nil, false,
	)
	// 降级重放又失败：FoldedRetry=true 但 callErr!=nil ⇒ 不得置 Recovered。
	svc.recordSystemPromptAttempt(
		CompleteOptions{RequestID: "r-fail", Model: "auto", FoldedRetry: true},
		account, "domestic", att, errors.New("upstream 500"), false,
	)
	// 普通首试成功（非重放）⇒ 不得置 Recovered。
	svc.recordSystemPromptAttempt(
		CompleteOptions{RequestID: "r-normal", Model: "auto"},
		account, "domestic", att, nil, false,
	)

	events := svc.SystemPromptJournal.Recent(10)
	if len(events) != 3 {
		t.Fatalf("应记 3 条事件，实际 %d：%+v", len(events), events)
	}
	byID := map[string]systemprompt.Event{}
	for _, e := range events {
		byID[e.RequestID] = e
	}
	if !byID["r-ok"].Recovered {
		t.Fatalf("重放成功应 Recovered=true：%+v", byID["r-ok"])
	}
	if byID["r-fail"].Recovered {
		t.Fatalf("重放失败不得 Recovered：%+v", byID["r-fail"])
	}
	if byID["r-fail"].Status != 0 {
		t.Fatalf("无 HTTP 状态的错误应压成 Status=0，实际 %+v", byID["r-fail"])
	}
	if byID["r-normal"].Recovered {
		t.Fatalf("普通首试成功不得 Recovered：%+v", byID["r-normal"])
	}
}
