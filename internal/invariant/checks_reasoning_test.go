package invariant

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

func reasoningRec(session, fp, tier string, completion, rtokens int64, at int64) decision.Record {
	return decision.Record{
		Kind: decision.KindRequest, At: at, Ok: true,
		RequestID: "r", SessionKey: session, AccountID: "a1", Model: "m",
		SystemFp: fp, ReasoningTier: tier,
		CompletionTokens: completion, ReasoningTokens: rtokens,
		UsageSource: decision.UsageUpstream,
	}
}

func runReasoning(t *testing.T, recs ...decision.Record) []decision.Finding {
	t.Helper()
	st := NewState(8, 8)
	reg := DefaultRegistry()
	var out []decision.Finding
	for _, rec := range recs {
		out = append(out, reg.Run(rec, st)...)
	}
	return out
}

// 4 条连续静默 ⇒ I12a（起点，info）。
func TestI12aFiresOnFourthSilent(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 703, 0)) // 思考
	for i := 1; i <= 4; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, int64(i)))
	}
	findings := runReasoning(t, recs...)
	if len(findings) != 1 || findings[0].Invariant != "I12a" {
		t.Fatalf("应命中 1 条 I12a，实际 %+v", findings)
	}
	if findings[0].Severity != SeverityInfo {
		t.Fatalf("I12a 应为 info，实际 %q", findings[0].Severity)
	}
	if !strings.Contains(findings[0].Detail, "此前 1 条中 1 条思考") {
		t.Fatalf("detail 应含早期基线，实际 %q", findings[0].Detail)
	}
}

// 段长到 8 ⇒ I12b（锁死，violation）。
func TestI12bFiresAtEightSilent(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 703, 0))
	for i := 1; i <= 8; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, int64(i)))
	}
	findings := runReasoning(t, recs...)
	var gotA, gotB bool
	for _, f := range findings {
		switch f.Invariant {
		case "I12a":
			gotA = true
		case "I12b":
			gotB = true
		}
	}
	if !gotA || !gotB {
		t.Fatalf("应有 I12a 与 I12b，实际 %+v", findings)
	}
}

// 静默之后恢复思考 ⇒ I12c（info）。
func TestI12cFiresOnRecovery(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 703, 0))
	for i := 1; i <= 5; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, int64(i)))
	}
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 2124, 6))
	findings := runReasoning(t, recs...)
	var gotC bool
	for _, f := range findings {
		if f.Invariant == "I12c" {
			gotC = true
			if !strings.Contains(f.Detail, "5 条") {
				t.Fatalf("I12c 应写段长，实际 %q", f.Detail)
			}
		}
	}
	if !gotC {
		t.Fatalf("恢复应命中 I12c，实际 %+v", findings)
	}
}

// Review Focus 1：systemFp 为空 ⇒ 不参与（无法区分父子分支）。
func TestI12SkipsWithoutSystemFp(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "", "max", 800, 703, 0))
	for i := 1; i <= 8; i++ {
		recs = append(recs, reasoningRec("s", "", "max", 800, 0, int64(i)))
	}
	if findings := runReasoning(t, recs...); len(findings) != 0 {
		t.Fatalf("无 systemFp 不应参与 I12：%+v", findings)
	}
}

// Review Focus 5：部署前的老记录没有 reasoningTier ⇒ 全部不可判，不得报。
func TestI12SkipsLegacyRecords(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "", 800, 0, 0))
	for i := 1; i <= 8; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "", 800, 0, int64(i)))
	}
	if findings := runReasoning(t, recs...); len(findings) != 0 {
		t.Fatalf("未声明思考（含老记录）不应参与 I12：%+v", findings)
	}
}

// 短输出不参与：reasoning 计入 completion，短输出必然挤压思考。
func TestI12SkipsShortCompletion(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fp1", "max", 800, 703, 0))
	for i := 1; i <= 8; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 150, 0, int64(i)))
	}
	if findings := runReasoning(t, recs...); len(findings) != 0 {
		t.Fatalf("completion<300 不应参与：%+v", findings)
	}
}

// 不同 systemFp = 不同分支：交替灌入不得打断段，也不得跨分支累计。
func TestI12SeparatesBranchesBySystemFp(t *testing.T) {
	var recs []decision.Record
	recs = append(recs, reasoningRec("s", "fpParent", "max", 800, 703, 0))
	for i := 1; i <= 4; i++ {
		recs = append(recs, reasoningRec("s", "fpParent", "max", 800, 0, int64(i*2)))
		recs = append(recs, reasoningRec("s", "fpChild", "max", 800, 900, int64(i*2+1)))
	}
	findings := runReasoning(t, recs...)
	if len(findings) != 1 || findings[0].Invariant != "I12a" {
		t.Fatalf("父分支应独立命中 I12a，实际 %+v", findings)
	}
}

// 本来就不思考（无思考证据）不是退化：不得报。
func TestI12SkipsWithoutPriorThinking(t *testing.T) {
	var recs []decision.Record
	for i := 0; i < 10; i++ {
		recs = append(recs, reasoningRec("s", "fp1", "max", 800, 0, int64(i)))
	}
	if findings := runReasoning(t, recs...); len(findings) != 0 {
		t.Fatalf("从未思考过不是退化：%+v", findings)
	}
}
