package invariant

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 用 2026-11-24 那个会话的**真实数据**锁住标定结果。
//
// ## 为什么要这条测试
//
// 阈值 minSilentRun=4 / lockedRun=8 是用 Python 模型标定出来的。
// Python 模型与 Go 实现在几个细节上可能悄悄分叉：
//   - priorThink 计不计静默条（Python 不计，Go 注释说不计）
//   - 段长到阈值时报一次，还是每条都报
//   - 恢复（LastRunLen 归档）时机
//
// 只有把同一份真实数据喂给 Go 实现，才能确认两者一致。
// 数据来自 hdr:ses_df32295a3ffeKCAzWBigtMWRd0 的父分支，
// completion>=500 的 41 条实质回合（time/compl/reasoning）。
//
// ## 期望结果（Python 模型输出）
//
//	[info]     此前 2 条中 2 条思考（100%）；自 18:35:26 起连续 4 条静默
//	[violation] 此前 15 条中 6 条思考（40%）；自 19:05:37 起连续 18 条静默
//	[violation] 此前 34 条中 7 条思考（21%）；自 20:08:17 起连续 7 条实质回合零思考
//
// 注意第三条：段长 7 < lockedRun=8，但它是「延续到数据末尾」。
// Go 实现刻意**不用**「到末尾」判据（那是数据集属性，在线判不了），
// 所以 Go 版会把它判成 info 而不是 violation —— 这是与 Python 模型的
// 唯一故意差异，见下面的断言。
func TestI12ReproducesRealSessionCalibration(t *testing.T) {
	// (hour, minute, second, completionTokens, reasoningTokens)
	type row struct {
		h, m, s   int
		compl     int64
		reasoning int64
	}
	rows := []row{
		{18, 34, 5, 835, 554}, {18, 35, 14, 1755, 126}, {18, 35, 26, 1593, 0},
		{18, 39, 1, 538, 1}, {18, 39, 7, 530, 1}, {18, 39, 50, 881, 1},
		{18, 48, 2, 4449, 1167}, {18, 51, 33, 1506, 1}, {18, 51, 59, 790, 0},
		{18, 53, 21, 843, 522}, {18, 53, 29, 559, 1}, {18, 53, 43, 580, 1},
		{18, 53, 55, 641, 187}, {18, 54, 25, 1060, 1}, {19, 5, 11, 1033, 703},
		{19, 5, 37, 1510, 1}, {19, 49, 43, 575, 0}, {19, 50, 31, 510, 1},
		{19, 51, 4, 715, 1}, {19, 51, 17, 968, 1}, {19, 51, 45, 754, 1},
		{19, 52, 23, 517, 1}, {19, 53, 4, 2878, 0}, {19, 53, 27, 1058, 0},
		{19, 57, 29, 636, 1}, {19, 58, 29, 538, 1}, {19, 58, 40, 812, 1},
		{19, 58, 56, 824, 0}, {19, 59, 25, 978, 1}, {20, 1, 47, 2336, 1},
		{20, 2, 10, 1318, 0}, {20, 5, 22, 1470, 1}, {20, 5, 41, 649, 1},
		{20, 7, 54, 2654, 2124}, {20, 8, 17, 577, 1}, {20, 9, 15, 516, 1},
		{20, 9, 47, 1028, 1}, {20, 10, 11, 501, 1}, {20, 10, 38, 532, 1},
		{20, 10, 45, 860, 0}, {20, 11, 13, 1154, 0},
	}

	st := NewState(0, 0) // 离线：不淘汰
	reg := DefaultRegistry()
	var out []decision.Finding
	for _, r := range rows {
		ms := int64(((r.h*3600 + r.m*60 + r.s) * 1000))
		rec := decision.Record{
			Kind: decision.KindRequest, At: ms, Ok: true,
			RequestID: "req", SessionKey: "df32295a3ffe", AccountID: "43ff2ea5",
			Model: "deepseek-v4.1-flash", SystemFp: "e7a34b5d",
			ReasoningTier: "max", CompletionTokens: r.compl, ReasoningTokens: r.reasoning,
			UsageSource: decision.UsageUpstream, PromptTokens: 350000,
		}
		out = append(out, reg.Run(rec, st)...)
	}

	var a, b, c int
	for _, f := range out {
		switch f.Invariant {
		case "I12a":
			a++
		case "I12b":
			b++
		case "I12c":
			c++
		default:
			t.Fatalf("意外的断言 %s：%+v", f.Invariant, f)
		}
	}
	if a != 3 {
		t.Fatalf("I12a 应有 3 条（三处退化起点），实际 %d：%+v", a, out)
	}
	if b != 1 {
		t.Fatalf("I12b 应有 1 条（18 条那段锁死），实际 %d：%+v", b, out)
	}
	if c != 2 {
		// 19:05:37 那段 18 条在 20:07:54 恢复 → 1 条；
		// 20:08:17 那段 7 条未恢复（数据结束）→ 无 I12c。
		// 18:35:26 那段 4 条在 18:48:02 恢复 → 1 条。合计 2。
		t.Fatalf("I12c 应有 2 条，实际 %d：%+v", c, out)
	}

	// 逐个核验 detail 里的关键事实（这些是用户判断「节点在哪」的依据）。
	wantSubstr := []struct{ inv, substr string }{
		{"I12a", "此前 2 条中 2 条思考（100%）"},
		{"I12a", "此前 15 条中 6 条思考（40%）"},
		{"I12a", "此前 34 条中 7 条思考（21%）"},
		{"I12b", "连续 8 条实质回合零思考"},
	}
	for _, w := range wantSubstr {
		found := false
		for _, f := range out {
			if f.Invariant == w.inv && strings.Contains(f.Detail, w.substr) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s 的 detail 里应有 %q，实际全部 findings：%+v", w.inv, w.substr, out)
		}
	}

	// 与 Python 模型的故意差异：那段 7 条（20:08:17 起，数据结束）在 Python
	// 里因「延续到末尾」被升级成 violation；Go 不用该判据，只报 info。
	// 这条断言把差异钉死，避免后人以为是回归。
	for _, f := range out {
		if strings.Contains(f.Detail, "此前 34 条中 7 条思考") && f.Severity != SeverityInfo {
			t.Fatalf("段长 7 不到 lockedRun=8，应为 info（不以「到数据末尾」升级）：%+v", f)
		}
	}
}
