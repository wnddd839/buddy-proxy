package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// 官方 reference 比例（见 tokfp/reference.go）：
//
//	cjk    0.636364
//	digits 0.454545
//	alpha  0.204697
//	sym    0.678571
//	other  0.823824
//
// I11 只对**单一成分占比 >= 90%** 的请求生效：
// 混合文本的成分加权有 7.26% 误差，不足以支撑绝对比对。

func absRecord(model, bucket string, chars, dominant int, ratio float64) decision.Record {
	return decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true, RequestID: "r1",
		Model:     model,
		TokBucket: bucket,
		TokRatio:  ratio,
		TokChars:  chars,
	}
}

// DeepSeek 家族的实际值必须通过（实测偏差 0.11%）。
func TestI11PassesDeepSeek(t *testing.T) {
	for _, tc := range []struct {
		model  string
		bucket string
		chars  int
		cjk    int
		ratio  float64
	}{
		{"deepseek-v4.1-flash", tokfp.BucketCJK, 6600, 6200, 0.6371},
		{"cn:deepseek-v4.1-flash", tokfp.BucketCJK, 6600, 6200, 0.6371},
		{"deepseek-v4-pro", tokfp.BucketDigits, 6600, 6200, 0.4545},
	} {
		rec := absRecord(tc.model, tc.bucket, tc.chars, tc.cjk, tc.ratio)
		rec.TokCJK = tc.cjk
		got := DefaultRegistry().Run(rec, NewState(8, 8))
		for _, f := range got {
			if f.Invariant == "I11" {
				t.Errorf("%s/%s 不该命中 I11：%+v", tc.model, tc.bucket, f)
			}
		}
	}
}

// glm 的数字样本 0.6018 vs reference 0.4545 = +32% —— 必须报。
// 但 glm 不在 DeepSeek 家族，所以不该走 I11。
func TestI11SkipsNonDeepSeek(t *testing.T) {
	rec := absRecord("glm-5.3-flash", tokfp.BucketDigits, 6600, 0, 0.6018)
	rec.TokDigits = 6200
	for _, f := range DefaultRegistry().Run(rec, NewState(8, 8)) {
		if f.Invariant == "I11" {
			t.Errorf("glm 不在 DeepSeek 家族，不该走 I11：%+v", f)
		}
	}
}

// 真被换成别的词表时（DeepSeek 家族但比例偏 32%）必须报。
func TestI11CatchesSubstituted(t *testing.T) {
	rec := absRecord("deepseek-v4.1-flash", tokfp.BucketDigits, 6600, 0, 0.6018)
	rec.TokDigits = 6200
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	var hit *decision.Finding
	for i := range got {
		if got[i].Invariant == "I11" {
			hit = &got[i]
		}
	}
	if hit == nil {
		t.Fatalf("DeepSeek 家族偏离 32%% 应命中 I11：%+v", got)
	}
	if hit.Severity != SeverityViolation {
		t.Fatalf("I11 必须是 violation：%+v", hit)
	}
}

// 混合文本（单成分占比 < 90%）不判 —— 成分加权有 7.26% 误差。
func TestI11SkipsMixedText(t *testing.T) {
	rec := absRecord("deepseek-v4.1-flash", tokfp.BucketASCII, 6600, 0, 0.6018)
	// ASCII 只占 60%，其余是中文与数字
	rec.TokASCII = 3960
	rec.TokCJK = 2000
	rec.TokDigits = 640
	for _, f := range DefaultRegistry().Run(rec, NewState(8, 8)) {
		if f.Invariant == "I11" {
			t.Errorf("混合文本不该走 I11：%+v", f)
		}
	}
}

// 8% 容差内的偏差不报。
func TestI11ToleratesSmallDeviation(t *testing.T) {
	// 0.4545 * 1.05 = 0.4772（+5%）
	rec := absRecord("deepseek-v4.1-flash", tokfp.BucketDigits, 6600, 0, 0.4772)
	rec.TokDigits = 6200
	for _, f := range DefaultRegistry().Run(rec, NewState(8, 8)) {
		if f.Invariant == "I11" {
			t.Errorf("5%% 偏差不该报：%+v", f)
		}
	}
}

// 各豁免：未观测 / 失败 / 取消 / tiny / 物理不合理。
func TestI11Exemptions(t *testing.T) {
	st := NewState(8, 8)
	for _, rec := range []decision.Record{
		{Kind: decision.KindRequest, Ok: true, Model: "deepseek-v4-flash", TokBucket: tokfp.BucketDigits, TokRatio: 0},
		{Kind: decision.KindRequest, Ok: false, Model: "deepseek-v4-flash", TokBucket: tokfp.BucketDigits, TokRatio: 0.9},
		{Kind: decision.KindRequest, Ok: true, Canceled: true, Model: "deepseek-v4-flash", TokBucket: tokfp.BucketDigits, TokRatio: 0.9},
		{Kind: decision.KindRequest, Ok: true, Model: "deepseek-v4-flash", TokBucket: tokfp.BucketTiny, TokRatio: 0.9},
		{Kind: decision.KindRequest, Ok: true, Model: "deepseek-v4-flash", TokBucket: tokfp.BucketDigits, TokRatio: 2.5},
	} {
		for _, f := range DefaultRegistry().Run(rec, st) {
			if f.Invariant == "I11" {
				t.Fatalf("豁免不该命中 I11：%+v（%+v）", f, rec)
			}
		}
	}
}
