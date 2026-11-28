package invariant

import (
	"sync"
	"testing"
)

// 连续静默要能成段；插入一条「可判但思考」就断段。
func TestReasoningRunGrowsAndBreaks(t *testing.T) {
	st := NewState(8, 8)
	for i := 0; i < 3; i++ {
		_, _ = st.ObserveReasoning("k", true, true /*silent*/, int64(i))
	}
	got, _ := st.ObserveReasoning("k", true, true, 3)
	if got.RunLen != 4 {
		t.Fatalf("RunLen 应为 4，实际 %d", got.RunLen)
	}
	_, _ = st.ObserveReasoning("k", true, false /*思考了*/, 4)
	got, _ = st.ObserveReasoning("k", true, true, 5)
	if got.RunLen != 1 || !got.IsNewRun {
		t.Fatalf("思考后应重新开段，实际 RunLen=%d IsNewRun=%v", got.RunLen, got.IsNewRun)
	}
	if got.LastRunLen != 4 {
		t.Fatalf("LastRunLen 应归档被打断的 4 条段，实际 %d", got.LastRunLen)
	}
}

// 不可判记录（tiny / 取消 / 未声明思考）不得切断段 ——
// 否则坏会话的 18 条段会被大量 tiny 回复切成碎片，段长永远到不了阈值。
func TestReasoningRunSurvivesUnjudged(t *testing.T) {
	st := NewState(8, 8)
	_, _ = st.ObserveReasoning("k", true, true, 0)
	_, _ = st.ObserveReasoning("k", false, false, 1) // 不可判
	_, _ = st.ObserveReasoning("k", false, false, 2) // 不可判
	got, _ := st.ObserveReasoning("k", true, true, 3)
	if got.RunLen != 2 {
		t.Fatalf("不可判记录不该断段也不该计数，RunLen 应为 2，实际 %d", got.RunLen)
	}
}

// 键被淘汰后重入必须重新攒基线，不能续上淘汰前的段。
func TestReasoningStateEvictsAndForgets(t *testing.T) {
	st := NewState(2, 2)
	_, _ = st.ObserveReasoning("a", true, true, 0)
	_, _ = st.ObserveReasoning("b", true, true, 0)
	_, _ = st.ObserveReasoning("c", true, true, 0) // 淘汰 a
	got, _ := st.ObserveReasoning("a", true, true, 1)
	if got.RunLen != 1 || !got.IsNewRun {
		t.Fatalf("淘汰后重入应为新段，实际 RunLen=%d IsNewRun=%v", got.RunLen, got.IsNewRun)
	}
}

// Sink.Submit 在请求路径同步跑断言：多个请求可能同时喂同一键。
func TestReasoningObserveRaceFree(t *testing.T) {
	st := NewState(4, 4)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				_, _ = st.ObserveReasoning("k", true, j%2 == 0, int64(n*100+j))
			}
		}(i)
	}
	wg.Wait()
	if st.ReasoningObserveCount() > 4 {
		t.Fatalf("观测键数应受 sessionCap 限制，实际 %d", st.ReasoningObserveCount())
	}
}
