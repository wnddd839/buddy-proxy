package models

import "testing"

// wb-lite 不在上游编目里，必须由网关侧补进去 —— 否则客户端和管理台
// 都看不到它，只能手输名字。
func TestWithLiteModelInjects(t *testing.T) {
	base := []Model{
		{ModelID: "deepseek-v4.1-flash", ID: "deepseek-v4.1-flash"},
	}
	out := WithLiteModel(base)
	if len(out) != 2 {
		t.Fatalf("应补进一条，实际 %d 条", len(out))
	}
	if out[0].ModelID != LiteModelID {
		t.Fatalf("wb-lite 应排在首位，实际首条 %q", out[0].ModelID)
	}
	// 原有条目不能被丢
	found := false
	for _, m := range out {
		if m.ModelID == "deepseek-v4.1-flash" {
			found = true
		}
	}
	if !found {
		t.Fatal("原有模型被丢掉了")
	}
}

// 幂等：上游哪天真的把 wb-lite 加进编目，不能再插一条重复的。
func TestWithLiteModelIdempotent(t *testing.T) {
	base := []Model{{ModelID: LiteModelID, ID: LiteModelID}}
	out := WithLiteModel(base)
	if len(out) != 1 {
		t.Fatalf("已存在时不该重复注入，实际 %d 条", len(out))
	}
}

// 补进来的条目要标成免费、0 倍率，并标明来源是内建，
// 便于管理台区分「上游编目」与「网关补充」。
func TestLiteModelIsMarkedFreeAndBuiltin(t *testing.T) {
	out := WithLiteModel(nil)
	if len(out) != 1 {
		t.Fatalf("空列表也应补一条，实际 %d", len(out))
	}
	m := out[0]
	if m.Source != "builtin" {
		t.Fatalf("Source 应为 builtin，实际 %q", m.Source)
	}
	if m.Free == nil || !*m.Free {
		t.Fatal("应标记为免费")
	}
	if m.CreditMultiplier == nil || *m.CreditMultiplier != 0 {
		t.Fatal("creditMultiplier 应为 0")
	}
	if !m.SupportsReasoning {
		t.Fatal("应声明支持思考（它接受 reasoning_effort）")
	}
}

// 空列表也要能补（上游编目拉取失败时的兜底路径）。
func TestWithLiteModelOnEmptyList(t *testing.T) {
	if got := WithLiteModel(nil); len(got) != 1 {
		t.Fatalf("空列表应补一条，实际 %d", len(got))
	}
	if got := WithLiteModel([]Model{}); len(got) != 1 {
		t.Fatalf("空切片应补一条，实际 %d", len(got))
	}
}
