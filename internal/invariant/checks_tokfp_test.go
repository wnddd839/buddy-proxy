package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// 实测基线（2026-11-22，真实请求）：
//
//	deepseek-v4.1-flash  zh=0.6371  code=0.3073  nums=0.4552
//	glm-5.3-flash        zh=0.6689  code=0.3217  nums=0.6018
//
// 数字密集样本上两者差 32%，是唯一能独立判别模型的桶。
func tokReq(model, bucket string, ratio float64, at int64) decision.Record {
	return decision.Record{
		Kind: decision.KindRequest, At: at, RequestID: "r1",
		Model: model, TokBucket: bucket, TokRatio: ratio, Ok: true,
	}
}

// 前 2 次只建基线，不判异常。
func TestI9DoesNotJudgeBeforeBaseline(t *testing.T) {
	st := NewState(8, 8)
	for i := 0; i < 2; i++ {
		got := DefaultRegistry().Run(tokReq("deepseek-v4.1-flash", tokfp.BucketDigits, 0.4552, int64(i)), st)
		for _, f := range got {
			if f.Invariant == "I9" {
				t.Fatalf("样本不足时不该命 I9：%+v", f)
			}
		}
	}
}

// 第 3 个样本建立基线，出一次 I9b（info），不出 I9。
func TestI9BaselineEstablishedAtThirdSample(t *testing.T) {
	st := NewState(8, 8)
	var got []decision.Finding
	for i := 0; i < 3; i++ {
		got = DefaultRegistry().Run(tokReq("deepseek-v4.1-flash", tokfp.BucketDigits, 0.4552, int64(i)), st)
	}
	var info *decision.Finding
	for i := range got {
		if got[i].Invariant == "I9b" {
			info = &got[i]
		}
		if got[i].Invariant == "I9" {
			t.Fatalf("建立基线时不该命 I9：%+v", got[i])
		}
	}
	if info == nil {
		t.Fatalf("第 3 个样本应出 I9b：%+v", got)
	}
	if info.Severity != SeverityInfo {
		t.Fatalf("I9b 必须是 info：%+v", info)
	}
}

// 稳定样本不出 I9。
func TestI9StableRatioIsSilent(t *testing.T) {
	st := NewState(8, 8)
	for i := 0; i < 10; i++ {
		r := 0.4552 + float64(i%3-1)*0.0005
		for _, f := range DefaultRegistry().Run(tokReq("deepseek-v4.1-flash", tokfp.BucketDigits, r, int64(i)), st) {
			if f.Invariant == "I9" {
				t.Fatalf("稳定比例不该命 I9（第 %d 次）：%+v", i, f)
			}
		}
	}
}

// 真换了模型：数字密集 0.4552 → 0.6018（差 32%）必须报。
func TestI9CatchesModelSwapOnDigits(t *testing.T) {
	st := NewState(8, 8)
	for i := 0; i < 5; i++ {
		DefaultRegistry().Run(tokReq("deepseek-v4.1-flash", tokfp.BucketDigits, 0.4552, int64(i)), st)
	}
	got := DefaultRegistry().Run(tokReq("deepseek-v4.1-flash", tokfp.BucketDigits, 0.6018, 10), st)
	var hit *decision.Finding
	for i := range got {
		if got[i].Invariant == "I9" {
			hit = &got[i]
		}
	}
	if hit == nil {
		t.Fatalf("32%% 的偏差必须命中 I9：%+v", got)
	}
	if hit.Severity != SeverityViolation {
		t.Fatalf("I9 必须是 violation：%+v", hit)
	}
}

// 5% 的偏差不该报（容忍度 8%）。
func TestI9ToleratesSmallDeviation(t *testing.T) {
	st := NewState(8, 8)
	for i := 0; i < 5; i++ {
		DefaultRegistry().Run(tokReq("m", tokfp.BucketCJK, 0.6371, int64(i)), st)
	}
	for _, f := range DefaultRegistry().Run(tokReq("m", tokfp.BucketCJK, 0.6690, 10), st) {
		if f.Invariant == "I9" {
			t.Fatalf("5%% 偏差不该命 I9：%+v", f)
		}
	}
}

// 分桶必须进键：中文与数字的比例差 2 倍，混在一起会污染基线。
func TestI9BucketsDoNotShareBaseline(t *testing.T) {
	st := NewState(8, 8)
	for i := 0; i < 5; i++ {
		DefaultRegistry().Run(tokReq("m", tokfp.BucketCJK, 0.6371, int64(i)), st)
	}
	for _, f := range DefaultRegistry().Run(tokReq("m", tokfp.BucketDigits, 0.4552, 10), st) {
		if f.Invariant == "I9" {
			t.Fatalf("不同分桶不该共用基线：%+v", f)
		}
	}
}

// 短 prompt / 无观测 / 失败 / 取消都不参与。
func TestI9SkipsTinyAndUnobserved(t *testing.T) {
	st := NewState(8, 8)
	for i := 0; i < 5; i++ {
		DefaultRegistry().Run(tokReq("m", tokfp.BucketDigits, 0.4552, int64(i)), st)
	}
	for _, rec := range []decision.Record{
		{Kind: decision.KindRequest, Ok: true, Model: "m", TokBucket: tokfp.BucketTiny, TokRatio: 0.99},
		{Kind: decision.KindRequest, Ok: true, Model: "m", TokBucket: tokfp.BucketDigits, TokRatio: 0},
		{Kind: decision.KindRequest, Ok: false, Model: "m", TokBucket: tokfp.BucketDigits, TokRatio: 0.99},
		{Kind: decision.KindRequest, Ok: true, Canceled: true, Model: "m", TokBucket: tokfp.BucketDigits, TokRatio: 0.99},
	} {
		if got := DefaultRegistry().Run(rec, st); len(got) > 0 {
			t.Fatalf("不该命中任何断言：%+v", got)
		}
	}
}

// 路由改写后的模型名才该做基线键（客户端可能传别名）。
func TestI9UsesRoutedModel(t *testing.T) {
	st := NewState(8, 8)
	rec := decision.Record{
		Kind: decision.KindRequest, Ok: true, Model: "my-alias",
		RoutedModel: "deepseek-v4.1-flash",
		TokBucket:   tokfp.BucketDigits, TokRatio: 0.4552,
	}
	DefaultRegistry().Run(rec, st)
	if b, ok := st.TokBaseline("deepseek-v4.1-flash", tokfp.BucketDigits); !ok {
		t.Fatalf("基线应记在 RoutedModel 上，实际：%+v", st.TokBaselines())
	} else if b.Samples != 1 {
		t.Fatalf("样本数应为 1：%+v", b)
	}
}

// 每条记录只该观测一次 —— 多个 check 各调一次会让样本数翻倍。
func TestI9ObservesOncePerRecord(t *testing.T) {
	st := NewState(8, 8)
	DefaultRegistry().Run(tokReq("m", tokfp.BucketDigits, 0.4552, 1), st)
	if b, ok := st.TokBaseline("m", tokfp.BucketDigits); !ok {
		t.Fatal("应有基线")
	} else if b.Samples != 1 {
		t.Fatalf("一条记录应只累积 1 个样本，实际 %d", b.Samples)
	}
}
