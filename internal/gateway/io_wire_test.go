package gateway

import (
	"testing"
)

// 入站统计必须在 CompleteFromPool 之后真的有值 —— 这是 I3/I4/I9/I10/I11
// 全部判据的前提。
//
// 历史事故（2026-11-24）：上一版把 provider 的 FillMessagesIO 改成「只填
// 出站侧」，而唯一的入站补位 primeInboundIO 因为 IO 尚未分配而永久 no-op，
// 导致 InMessages/InBytes/InSystemLen 全 0、Composition.Chars=0，
// I3/I4/I9/I10/I11 全线失明。全仓没有这条断言，所以它一路绿着走过去。
func TestCompleteFromPoolFillsInboundIO(t *testing.T) {
	svc := routeFixture(t, nil)
	outcome := &SystemPromptOutcome{}

	_, err := svc.CompleteFromPool(t.Context(), CompleteOptions{
		Site: "domestic", Model: "auto",
		Messages:      []map[string]any{{"role": "system", "content": "SYS"}, {"role": "user", "content": "hi"}},
		SystemOutcome: outcome,
	})
	if err != nil {
		t.Fatal(err)
	}

	if outcome.IO == nil {
		t.Fatal("CompleteFromPool 之后 SystemOutcome.IO 不应为 nil")
	}
	if outcome.IO.InMessages != 2 {
		t.Fatalf("InMessages = %d，want 2", outcome.IO.InMessages)
	}
	if outcome.IO.InBytes == 0 {
		t.Fatalf("InBytes = 0，入站内容统计丢了")
	}
	if outcome.IO.InSystemLen == 0 {
		t.Fatalf("InSystemLen = 0，system 统计丢了")
	}
	if outcome.IO.Composition.Chars == 0 {
		t.Fatalf("Composition.Chars = 0，分词器比例会因比而为 0，I9/I10/I11 全部失明")
	}
	if !outcome.IO.OutObserved {
		t.Fatalf("OutObserved 应为 true，实际 false")
	}
	if outcome.IO.OutMessages == 0 {
		t.Fatalf("OutMessages = 0")
	}
}
