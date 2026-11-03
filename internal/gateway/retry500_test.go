package gateway

import "testing"

// 上游 500（invoke model error）必须能换号重试：2026-11-03 18:10-18:30 窗口
// 8 条失败里有它，旧正则只认 429/502/503/504，500 直接被抛给客户端。
func TestRetryRegexMatchesUpstream500(t *testing.T) {
	cases := []string{
		`CodeBuddy chat completion failed with 500: invoke model error [region=domestic site=domestic endpoint=https://copilot.tencent.com/v2/chat/completions]`,
		`CodeBuddy chat completion failed with 500: internal error`,
	}
	for _, msg := range cases {
		if !reRetryNextAccount.MatchString(msg) {
			t.Fatalf("500 must be replayable: %s", msg)
		}
	}
}

// 不能把「不该重试」的 4xx 误扩进来（防止正则写宽）。
func TestRetryRegexStillRejectsPermanentErrors(t *testing.T) {
	for _, msg := range []string{
		`CodeBuddy chat completion failed with 401: unauthorized`,
		`CodeBuddy chat completion failed with 404: model not found`,
	} {
		if reRetryNextAccount.MatchString(msg) {
			t.Fatalf("must not be replayable: %s", msg)
		}
	}
}

// 停滞错误在首内容前必须计入传输层重放预算，否则多账号会把请求放大。
func TestStallBeforeContentIsTransportReplay(t *testing.T) {
	if !isTransportReplayError("CodeBuddy chat completion stream stalled before first content: read tcp: closed") {
		t.Fatal("stall before first content must be a transport replay error")
	}
}
