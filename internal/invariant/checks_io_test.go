package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// fold 是合法的条数 -1；strip 不改条数；echo 只增不减。全都不许报。
func TestI3IgnoresRecordedTransforms(t *testing.T) {
	// Ok: true —— I3 现在只判成功请求（失败请求可能压根没发出去，
	// 「出站 0 条」不代表丢历史）。下面这些验证的是成功路径上的豁免，
	// 当初写的时候还没有这条前置条件。
	ok := []decision.Record{
		{Kind: decision.KindRequest, At: 1, Ok: true, InMessages: 3, OutMessages: 2, InSystemLen: 50, SystemForm: "fold"},
		// strip 只删身份句：条数与 system 都还在。
		{Kind: decision.KindRequest, At: 1, Ok: true, InMessages: 3, OutMessages: 3, InSystemLen: 50, OutSystemLen: 42, Stripped: true, StripScope: "system"},
		// echo：system 位不变，user 里多一份（只增不减）。
		{Kind: decision.KindRequest, At: 1, Ok: true, InMessages: 3, OutMessages: 3, InSystemLen: 50, OutSystemLen: 50},
		{Kind: decision.KindRequest, At: 1, Ok: true, InMessages: 0, OutMessages: 0}, // 老调用方未观测
	}
	for i, rec := range ok {
		if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
			t.Fatalf("case %d 误报：%+v", i, got)
		}
	}

	bad := decision.Record{Kind: decision.KindRequest, At: 1, Ok: true, InMessages: 5, OutMessages: 3}
	got := DefaultRegistry().Run(bad, NewState(8, 8))
	if len(got) != 1 || got[0].Invariant != "I3" {
		t.Fatalf("应命中 I3：%+v", got)
	}
}

// 条数对但内容全空，同样是丢历史。
func TestI3DetectsEmptiedContent(t *testing.T) {
	// Ok: true —— 验证的是成功路径的字节归零判据；失败请求根本没出站，
	// 由 TestI3SkipsFailedBytesZero 单独覆盖。
	rec := decision.Record{Kind: decision.KindRequest, At: 1, Ok: true, InMessages: 2, OutMessages: 2, InBytes: 900, OutBytes: 0}
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	if len(got) != 1 || got[0].Invariant != "I3" {
		t.Fatalf("应命中 I3：%+v", got)
	}
}

// 取消的请求不参与（客户端断开时出站本来就可能不完整）。
func TestI3SkipsCanceled(t *testing.T) {
	rec := decision.Record{Kind: decision.KindRequest, At: 1, InMessages: 5, OutMessages: 0, Canceled: true}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("取消不该报：%+v", got)
	}
}

// I4 的全部合法清空路径都不得报，其余必须报。
func TestI4Exemptions(t *testing.T) {
	base := decision.Record{
		Kind: decision.KindRequest, At: 1, InMessages: 2, OutMessages: 2,
		InSystemLen: 100, OutSystemLen: 0,
	}
	allowed := []struct {
		name string
		mut  func(*decision.Record)
	}{
		{"fold", func(r *decision.Record) { r.SystemForm = "fold" }},
		{"strip-system", func(r *decision.Record) { r.StripScope = "system" }},
		{"strip-both", func(r *decision.Record) { r.StripScope = "both" }},
		{"degraded", func(r *decision.Record) { r.Degraded = true }},
		{"title", func(r *decision.Record) { r.TitleRewrite = true }},
		{"canceled", func(r *decision.Record) { r.Canceled = true }},
	}
	for _, a := range allowed {
		rec := base
		a.mut(&rec)
		if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
			t.Errorf("%s 不该报：%+v", a.name, got)
		}
	}

	got := DefaultRegistry().Run(base, NewState(8, 8))
	if len(got) != 1 || got[0].Invariant != "I4" {
		t.Fatalf("应命中 I4：%+v", got)
	}
}

// 出站仍有 system 时不报。
func TestI4KeepsSystemSilent(t *testing.T) {
	rec := decision.Record{Kind: decision.KindRequest, At: 1, InSystemLen: 100, OutSystemLen: 100}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("ystem 还在，不该报：%+v", got)
	}
}

// 会话骤降只出 info；取消 / 标题改写 / 估算 usage / 小上下文都不参与。
func TestI3bSkipsCanceledAndSmall(t *testing.T) {
	st := NewState(8, 8)
	st.SetPrompt("s1|a1|m", 477680)

	canceled := decision.Record{
		Kind: decision.KindRequest, At: 2, SessionKey: "s1", AccountID: "a1",
		Model: "m", PromptTokens: 100, Canceled: true, Ok: true, UsageSource: decision.UsageActual,
	}
	if got := DefaultRegistry().Run(canceled, st); len(got) != 0 {
		t.Fatalf("取消不该报：%+v", got)
	}

	title := decision.Record{
		Kind: decision.KindRequest, At: 2, SessionKey: "s1", AccountID: "a1",
		Model: "m", PromptTokens: 2100, Ok: true, UsageSource: decision.UsageActual, TitleRewrite: true,
	}
	if got := DefaultRegistry().Run(title, st); len(got) != 0 {
		t.Fatalf("标题改写不该报：%+v", got)
	}

	estimated := decision.Record{
		Kind: decision.KindRequest, At: 2, SessionKey: "s1", AccountID: "a1",
		Model: "m", PromptTokens: 1000, Ok: true, UsageSource: decision.UsageEstimated,
	}
	if got := DefaultRegistry().Run(estimated, st); len(got) != 0 {
		t.Fatalf("估算 usage 不该报：%+v", got)
	}

	drop := decision.Record{
		Kind: decision.KindRequest, At: 2, SessionKey: "s1", AccountID: "a1",
		Model: "m", PromptTokens: 317173, Ok: true, UsageSource: decision.UsageActual,
	}
	got := DefaultRegistry().Run(drop, st)
	if len(got) != 1 || got[0].Invariant != "I3b" || got[0].Severity != SeverityInfo {
		t.Fatalf("应出 info：%+v", got)
	}
}

// 小幅波动不报（掉不到 25%）。
func TestI3bIgnoresSmallDrop(t *testing.T) {
	st := NewState(8, 8)
	st.SetPrompt("s1|a1|m", 100000)
	rec := decision.Record{
		Kind: decision.KindRequest, At: 2, SessionKey: "s1", AccountID: "a1",
		Model: "m", PromptTokens: 80000, Ok: true, UsageSource: decision.UsageActual,
	}
	if got := DefaultRegistry().Run(rec, st); len(got) != 0 {
		t.Fatalf("20%% 的波动不该报：%+v", got)
	}
}

// I5 单次不报，恰好第 3 次报一次，第 4 次不再报。
func TestI5Threshold(t *testing.T) {
	st := NewState(8, 8)
	rec := decision.Record{Kind: decision.KindRequest, At: 1, Ok: false, ErrorText: "上游超时了"}
	reg := DefaultRegistry()
	for i := 1; i <= 2; i++ {
		if got := reg.Run(rec, st); len(got) != 0 {
			t.Fatalf("第 %d 次不该报：%+v", i, got)
		}
	}
	if got := reg.Run(rec, st); len(got) != 1 || got[0].Invariant != "I5" {
		t.Fatalf("第 3 次应报：%+v", got)
	}
	if got := reg.Run(rec, st); len(got) != 0 {
		t.Fatalf("第 4 次不该重复报：%+v", got)
	}
}

// 有错误码 / 取消 / 成功都不参与。
func TestI5Exemptions(t *testing.T) {
	reg := DefaultRegistry()
	cases := []decision.Record{
		{Kind: decision.KindRequest, Ok: false, ErrorCode: "11128", ErrorText: "x"},
		{Kind: decision.KindRequest, Ok: false, Canceled: true, ErrorText: "x"},
		{Kind: decision.KindRequest, Ok: true, ErrorText: ""},
	}
	st := NewState(8, 8)
	for i := 0; i < 5; i++ { // 反复跑也不得累计出 finding
		for _, rec := range cases {
			if got := reg.Run(rec, st); len(got) != 0 {
				t.Fatalf("不该报：%+v", got)
			}
		}
	}
}
