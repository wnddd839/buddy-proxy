package invariant

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 影子回放：造一天合成决策，覆盖评审 Review Focus 列出的全部高风险场景，
// 断言「只有预期的那几条命中」。
//
// 这比上线后在面板上肉眼判断便宜得多，也是「误报会把面板刷成噪音」
// 这条反目标的直接守卫。
func TestShadowReplayOnlyExpectedHits(t *testing.T) {
	dir := t.TempDir()
	st, err := decision.OpenStore(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-2 * time.Hour).UnixMilli()
	appendAt := func(offset int64, r decision.Record) {
		r.At = base + offset
		if err := st.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	f := func(v float64) *float64 { return &v }

	// ---------- 正常成功（应零命中）----------
	appendAt(0, decision.Record{
		Kind: decision.KindRequest, RequestID: "ok-1", AccountID: "a1", SessionKey: "s1",
		Model: "m", Ok: true, UsageSource: decision.UsageActual,
		PromptTokens: 1000, InMessages: 3, OutMessages: 3,
		InBytes: 500, OutBytes: 500, InSystemLen: 100, OutSystemLen: 100,
	})

	// ---------- 标题改写（同会话、无 system、小 prompt）----------
	appendAt(1000, decision.Record{
		Kind: decision.KindRequest, RequestID: "title-1", AccountID: "a1", SessionKey: "s1",
		Model: "m", Ok: true, TitleRewrite: true, UsageSource: decision.UsageActual,
		PromptTokens: 300, InMessages: 2, OutMessages: 2,
		InBytes: 120, OutBytes: 120,
	})

	// ---------- 客户端取消（Ok=true 但 usage 全 0）----------
	appendAt(2000, decision.Record{
		Kind: decision.KindRequest, RequestID: "cancel-1", AccountID: "a1", SessionKey: "s1",
		Model: "m", Ok: true, Canceled: true,
	})

	// ---------- 免费模型成功（credit=0，且额度事件标了 no_credit）----------
	appendAt(3000, decision.Record{
		Kind: decision.KindQuota, AccountID: "a1", RequestID: "free-1",
		Op: decision.OpDeduct, Before: f(50), After: f(50),
		SkipReason: decision.DeductSkipNoCredit,
	})
	appendAt(3100, decision.Record{
		Kind: decision.KindRequest, RequestID: "free-1", AccountID: "a1", Ok: true,
		Credit: f(0), UsageSource: decision.UsageActual, PromptTokens: 800,
	})

	// ---------- 付费成功 + 一致扣减 ----------
	appendAt(4000, decision.Record{
		Kind: decision.KindQuota, AccountID: "a1", RequestID: "paid-1",
		Op: decision.OpDeduct, Before: f(50), After: f(48.7), Credit: f(1.3),
	})
	appendAt(4100, decision.Record{
		Kind: decision.KindRequest, RequestID: "paid-1", AccountID: "a1", Ok: true,
		Credit: f(1.3), UsageSource: decision.UsageActual, PromptTokens: 900,
	})

	// ---------- 探针刷新，账面吻合 ----------
	//
	// Before / After 是同一口径：ApplyQuotaProbe 已把在途消耗从上游回包里
	// 减掉，所以账对得上时两者相等（实测值见 2026-11-21 上线日志）。
	// SpentSinceCheck 只作证据，不参与判定。
	appendAt(5000, decision.Record{
		Kind: decision.KindQuota, AccountID: "a1", Op: decision.OpRefresh,
		Before: f(48.7), After: f(48.7), SpentSinceCheck: 3.0,
	})

	// ---------- 选号：健康池首选（只检查 1 个）----------
	appendAt(6000, decision.Record{
		Kind: decision.KindSelection, RequestID: "sel-1",
		Candidates: 243, Examined: 1, Chosen: "a1",
	})

	// ---------- 选号：含禁用号 + 额度不足，全部交代 ----------
	appendAt(7000, decision.Record{
		Kind: decision.KindSelection, RequestID: "sel-2",
		Candidates: 4, Examined: 3,
		Skipped: map[string]int{"disabled": 1, "quota_floor": 2},
	})

	// ================= 以下是应当命中的 =================

	// 【I1】额度被无声清成 nil（没有理由）
	appendAt(8000, decision.Record{
		Kind: decision.KindQuota, AccountID: "a2", Op: decision.OpClear,
		Before: f(30), After: nil,
	})

	// 【I2a】上游收费 2.0，但没有任何扣减记录
	appendAt(9000, decision.Record{
		Kind: decision.KindRequest, RequestID: "paid-2", AccountID: "a3", Ok: true,
		Credit: f(2.0), UsageSource: decision.UsageActual,
	})

	// 【I3】网关内部把 5 条消息压成 3 条
	appendAt(10000, decision.Record{
		Kind: decision.KindRequest, RequestID: "shrink-1", AccountID: "a1",
		Ok: true, InMessages: 5, OutMessages: 3, InBytes: 800, OutBytes: 400,
	})

	// 【I4】system 无声消失（没有任何策略豁免）
	appendAt(11000, decision.Record{
		Kind: decision.KindRequest, RequestID: "sys-1", AccountID: "a1",
		Ok: true, InMessages: 2, OutMessages: 2, InSystemLen: 900, OutSystemLen: 0,
	})

	// 【I7】检查了 3 个号、选中 1 个、却没有任何跳过原因
	appendAt(12000, decision.Record{
		Kind: decision.KindSelection, RequestID: "sel-bad",
		Candidates: 5, Examined: 3, Chosen: "a1",
	})

	rep, err := Replay(st, time.UnixMilli(base-1000), time.UnixMilli(base+20000), nil)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]int{"I1": 1, "I2a": 1, "I3": 1, "I4": 1, "I7": 1}
	got := map[string]int{}
	for _, g := range rep.Groups {
		got[g.Invariant] += g.Count
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%s 应命中 %d 次，实际 %d", id, n, got[id])
		}
	}
	for id, n := range got {
		if _, expected := want[id]; !expected {
			t.Errorf("意外命中 %s（%d 次）—— 这是误报，共 %+v", id, n, rep.Groups)
		}
	}
	if rep.Scanned == 0 {
		t.Fatal("应扫描到记录")
	}
	if rep.Coverage == "" {
		t.Fatal("必须给出覆盖说明")
	}
}
