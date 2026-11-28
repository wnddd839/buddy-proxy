package accounts

import (
	"testing"
	"time"
)

// C3 回归：走全冷却兜底时，被选中的那个号不该同时出现在 skipped 里。
//
// ## 缺陷
//
// 主循环里，冷却中的号：
//  1. 被计进 skipped[SkipCoolingDown]++
//  2. 同时被记成 fallbackIdx（兜底候选）
//
// 若最后走兜底分支，那个号既是 Chosen 又在 skipped 里 ⇒
// sum(skipped) == Examined，而 I7 要求 Examined-1 ⇒ 池内 ≥2 号必误报。
func TestFallbackChosenIsNotCountedAsSkipped(t *testing.T) {
	pool := newObserverPool(t)
	future := time.Now().Add(time.Hour).UnixMilli()

	// 两个号都处于冷却：一个先恢复、一个后恢复 ⇒ 走兜底
	a := CreateAccount(Account{
		Label: "coolA", BearerToken: "t1", Site: "global",
		CooldownUntil: future, CooldownSource: CooldownSourceUpstream,
	})
	b := CreateAccount(Account{
		Label: "coolB", BearerToken: "t2", Site: "global",
		CooldownUntil: future + 60000, CooldownSource: CooldownSourceUpstream,
	})
	for _, acc := range []Account{a, b} {
		if _, _, err := pool.Upsert(acc); err != nil {
			t.Fatal(err)
		}
	}

	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "global", Model: "m", RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if !sel.BypassedCooldown {
		t.Fatalf("应走兜底：%+v", sel)
	}

	got := obs.selection[0]
	if got.Chosen != sel.Account.ID {
		t.Fatalf("事件与 Selection 不一致：%+v vs %+v", got, sel)
	}

	sum := 0
	for _, v := range got.Skipped {
		sum += v
	}
	// 选中了 1 个，跳过原因应覆盖剩下的 Examined-1 个
	if want := got.Examined - 1; sum != want {
		t.Fatalf("跳过原因总数 %d，应为 Examined-1=%d（Skipped=%v）",
			sum, want, got.Skipped)
	}
}

// 兜底时若只有一个号，Examined=1 ⇒ skipped 应为空。
func TestFallbackSingleAccountNoSkip(t *testing.T) {
	pool := newObserverPool(t)
	only := CreateAccount(Account{
		Label: "only", BearerToken: "t1", Site: "global",
		CooldownUntil:  time.Now().Add(time.Hour).UnixMilli(),
		CooldownSource: CooldownSourceUpstream,
	})
	if _, _, err := pool.Upsert(only); err != nil {
		t.Fatal(err)
	}

	obs := &fakeObserver{}
	pool.SetObserver(obs)

	sel, err := pool.Select(SelectOptions{Site: "global", Model: "m", RequestID: "r2"})
	if err != nil {
		t.Fatal(err)
	}
	if !sel.BypassedCooldown {
		t.Fatalf("应走兜底：%+v", sel)
	}

	got := obs.selection[0]
	if len(got.Skipped) != 0 {
		t.Fatalf("单号兜底不该有跳过原因：%v", got.Skipped)
	}
}
