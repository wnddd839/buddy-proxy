package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 两条并行历史分支共用同一 systemFp 时，只能靠消息条数区分：
// prompt 90-100K（37-61 条消息）与 65-70K（25-43 条）在一秒内交替。
//
// 实测（2026-11-27）：修掉跨 systemFp 后剩 101 条同 fp 骤降，
// 其中 88 条形态就是这样 —— InMessages 一起掉。
func TestI3bSkipsBranchSwitchWhenMessagesDrop(t *testing.T) {
	st := NewState(8, 8)
	reg := DefaultRegistry()
	base := decision.Record{
		Kind: decision.KindRequest, Ok: true, UsageSource: decision.UsageUpstream,
		SessionKey: "s", AccountID: "a1", Model: "m", SystemFp: "fp1",
	}
	big := base
	big.At, big.PromptTokens, big.InMessages = 0, 89782, 37
	small := base
	small.At, small.PromptTokens, small.InMessages = 1000, 64879, 25 // 一秒后
	if fs := reg.Run(big, st); len(fs) != 0 {
		t.Fatalf("首条不该报：%+v", fs)
	}
	for _, f := range reg.Run(small, st) {
		if f.Invariant == "I3b" {
			t.Fatalf("消息条数也掉了是换分支，不该报骤降：%+v", f)
		}
	}
}

// 对照：同一分支消息条数不减、token 掉一半 ⇒ 真骤降，仍要报。
// 实测形态：129658→5938 且 51→51（客户端把内容压成摘要但保留条数）。
func TestI3bFiresWhenMessagesStayButTokensDrop(t *testing.T) {
	st := NewState(8, 8)
	reg := DefaultRegistry()
	base := decision.Record{
		Kind: decision.KindRequest, Ok: true, UsageSource: decision.UsageUpstream,
		SessionKey: "s", AccountID: "a1", Model: "m", SystemFp: "fp1",
	}
	big := base
	big.At, big.PromptTokens, big.InMessages = 0, 129658, 51
	small := base
	small.At, small.PromptTokens, small.InMessages = 1000, 5938, 51
	if fs := reg.Run(big, st); len(fs) != 0 {
		t.Fatalf("首条不该报：%+v", fs)
	}
	hit := false
	for _, f := range reg.Run(small, st) {
		if f.Invariant == "I3b" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("同分支 token 骤降 95%% 仍应报 I3b")
	}
}

// 消息条数增长但 token 掉（客户端重排/压缩后条数变多）⇒ 仍算骤降。
func TestI3bFiresWhenMessagesGrew(t *testing.T) {
	st := NewState(8, 8)
	reg := DefaultRegistry()
	base := decision.Record{
		Kind: decision.KindRequest, Ok: true, UsageSource: decision.UsageUpstream,
		SessionKey: "s", AccountID: "a1", Model: "m", SystemFp: "fp1",
	}
	big := base
	big.At, big.PromptTokens, big.InMessages = 0, 104496, 62
	small := base
	small.At, small.PromptTokens, small.InMessages = 1000, 75605, 68
	if fs := reg.Run(big, st); len(fs) != 0 {
		t.Fatalf("首条不该报：%+v", fs)
	}
	hit := false
	for _, f := range reg.Run(small, st) {
		if f.Invariant == "I3b" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("消息增长时 token 掉 28%% 应报 I3b（消息不减就不是换分支）")
	}
}
