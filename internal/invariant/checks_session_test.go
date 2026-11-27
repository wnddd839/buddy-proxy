package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 两个 systemFp 交替 = 父子两分支，不是同一会话的骤降。
//
// 线上实测（2026-11-24 的 hdr:ses_df32295a3ffeKCAzWBigtMWRd0）：
// I3b 报出 123 + 108 条「上下文从 721120 降到 62980（-91%）」/「355695 降到
// 125074（-65%）」，全部是父分支（fp e7a34b5d、prompt ~355K）与子 agent
// （fp 6cf557b9、prompt ~125K）被当成同一会话的前后两条。
// 两者共用 opencode 的 X-Opencode-Session，所以 sessionKey 相同。
func TestI3bSkipsDifferentSystemFp(t *testing.T) {
	st := NewState(8, 8)
	reg := DefaultRegistry()
	base := decision.Record{
		Kind: decision.KindRequest, Ok: true, UsageSource: decision.UsageUpstream,
		SessionKey: "s", AccountID: "a1", Model: "m",
	}
	parent := base
	parent.At, parent.SystemFp, parent.PromptTokens = 0, "e7a34b5d", 355695
	child := base
	child.At, child.SystemFp, child.PromptTokens = 1, "6cf557b9", 125074
	if fs := reg.Run(parent, st); len(fs) != 0 {
		t.Fatalf("首条不该报：%+v", fs)
	}
	fs := reg.Run(child, st)
	for _, f := range fs {
		if f.Invariant == "I3b" {
			t.Fatalf("不同 systemFp 是不同分支，不该报骤降：%+v", f)
		}
	}
}

// 对照：同一 systemFp 真掉下去仍要报（修分支误判不能把真骤降放过）。
func TestI3bStillFiresOnSameSystemFp(t *testing.T) {
	st := NewState(8, 8)
	reg := DefaultRegistry()
	base := decision.Record{
		Kind: decision.KindRequest, Ok: true, UsageSource: decision.UsageUpstream,
		SessionKey: "s", AccountID: "a1", Model: "m", SystemFp: "fp1",
	}
	big := base
	big.At, big.PromptTokens = 0, 355695
	small := base
	small.At, small.PromptTokens = 1, 125074
	if fs := reg.Run(big, st); len(fs) != 0 {
		t.Fatalf("首条不该报：%+v", fs)
	}
	fs := reg.Run(small, st)
	hit := false
	for _, f := range fs {
		if f.Invariant == "I3b" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("同一 systemFp 骤降 65%% 仍应报 I3b：%+v", fs)
	}
}
