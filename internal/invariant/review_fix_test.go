package invariant

import (
	"strings"
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 一次恢复只报一次 I12c —— 之后每条思考都不许再报。
//
// 评审 Critical 1：lastLen/lastStart 归档后从不清零，恢复之后的每一条
// 思考都会再产出 I12c（实测 4 静默 + 30 思考 ⇒ 30 条）。在线 Sink 每 10
// 分钟去重会把它变成每天约 144 行，直接违反「每次退化各一条」。
func TestI12cReportsOncePerRecovery(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 703, 0))
	for i := 1; i <= 5; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, int64(i)))
	}
	// 恢复之后连续 10 条思考
	for i := 6; i <= 16; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 900, int64(i)))
	}
	findings := runReasoning(t, recs...)
	count := 0
	for _, f := range findings {
		if f.Invariant == "I12c" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("一次恢复应只报 1 条 I12c，实际 %d 条：%+v", count, findings)
	}
}

// At=0 不得 panic（formatMillis(0) 返回 "-"，[11:] 会越界）。
//
// 评审 Critical 2：runOne 的 recover 会把 slice 越界转成 violation 级
// 的 invariant-panic —— 一条缺 At 的记录就能把断言变成假告警。
func TestI12HandlesZeroTimestamp(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 703, 0))
	for i := 1; i <= 4; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, 0)) // 全零 At
	}
	findings := runReasoning(t, recs...)
	for _, f := range findings {
		if strings.Contains(f.Detail, "panic") || f.Subject == "invariant-panic" {
			t.Fatalf("At=0 不该 panic：%+v", f)
		}
	}
	// 仍然应该报出节点（只是时间显示为 "-"）
	hit := false
	for _, f := range findings {
		if f.Invariant == "I12a" {
			hit = true
			if !strings.Contains(f.Detail, "-") {
				t.Fatalf("零时间应显示为 \"-\"，实际 %q", f.Detail)
			}
		}
	}
	if !hit {
		t.Fatalf("零 At 仍应报 I12a，实际 %+v", findings)
	}
}

// 开局就静默、之后才首次思考 ⇒ 不许报 I12c（从没思考过，不是「恢复」）。
//
// 评审 Important 4：PriorThink 取更新后的值（本条自己 +1），守卫永远为真。
func TestI12cRequiresPriorThinking(t *testing.T) {
	var recs []decision.Record
	// 开局直接静默 4 条（够长），此时 priorThink == 0
	for i := 0; i <= 3; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, int64(i)))
	}
	// 然后首次思考
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 900, 4))
	findings := runReasoning(t, recs...)
	for _, f := range findings {
		if f.Invariant == "I12c" {
			t.Fatalf("从未思考过不是「恢复」，不该报 I12c：%+v", f)
		}
	}
}

// I12b 的严重度必须是 violation（评审 Minor 6：旧测试只查 ID 不查级别）。
func TestI12bSeverityIsViolation(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 703, 0))
	for i := 1; i <= 8; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, int64(i)))
	}
	findings := runReasoning(t, recs...)
	for _, f := range findings {
		if f.Invariant == "I12b" {
			if f.Severity != SeverityViolation {
				t.Fatalf("I12b 应为 violation，实际 %q", f.Severity)
			}
			return
		}
	}
	t.Fatalf("应命中 I12b，实际 %+v", findings)
}

// 并发跑真正的契约路径：DefaultRegistry().Run 从多 goroutine 同时喂同一键。
//
// 评审 Minor 11：旧测试只直连 State.ObserveReasoning，没走 Run/Sink 那条
// 真实路径。这里补上 —— -race 下必须干净。
func TestI12RegistryRunRaceFree(t *testing.T) {
	st := NewState(4, 4)
	reg := DefaultRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				rec := reasoningRec("s", "fp1", "max", 800,
					int64(j%2*900), int64(n*100+j))
				_ = reg.Run(rec, st)
			}
		}(i)
	}
	wg.Wait()
	if st.ReasoningObserveCount() > 4 {
		t.Fatalf("观测键数应受 sessionCap 限制，实际 %d", st.ReasoningObserveCount())
	}
}
