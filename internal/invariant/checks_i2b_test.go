package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// I2b 的正确判据：After 与 Before 都是「已扣过在途消耗」的口径，
// 相等就说明账对得上。spentSinceCheck 只是证据，不能再减一次。
//
// 这条是上线首分钟用真实数据抓到的：本地 spent=0.56、探针前后余额都
// 是 162.36，旧公式 (before-after)-spent = -0.56 于是误报。
func TestI2bComparesBeforeAfterDirectly(t *testing.T) {
	// 真实值：探针前后都是 162.36000278，本地累计 0.56（已在 before 里）
	before, after := 162.36000278, 162.36000278
	rec := decision.Record{
		Kind: decision.KindQuota, At: 1, AccountID: "a1",
		Op: decision.OpRefresh, Before: &before, After: &after, SpentSinceCheck: 0.56,
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("账对得上不该报：%+v", got)
	}
}

// 上游余额比本地低了 1.5（漏记一笔）⇒ 必须报。
func TestI2bDetectsRealDrift(t *testing.T) {
	before, after := 100.0, 98.5
	rec := decision.Record{
		Kind: decision.KindQuota, At: 1, AccountID: "a1",
		Op: decision.OpRefresh, Before: &before, After: &after, SpentSinceCheck: 3.0,
	}
	got := DefaultRegistry().Run(rec, NewState(8, 8))
	if len(got) != 1 || got[0].Severity != SeverityInfo {
		t.Fatalf("真漂移应出 info：%+v", got)
	}
}

// 小额浮点误差（1e-14 量级）不得报 —— 上游回包是浮点运算结果。
func TestI2bToleratesFloatNoise(t *testing.T) {
	before, after := 154.22000024999997, 154.22000025
	rec := decision.Record{
		Kind: decision.KindQuota, At: 1, AccountID: "a1",
		Op: decision.OpRefresh, Before: &before, After: &after, SpentSinceCheck: 4.27,
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("浮点噪声不该报：%+v", got)
	}
}

// 首次探针（Before 为 nil）不参与。
func TestI2bSkipsFirstProbe(t *testing.T) {
	after := 50.0
	rec := decision.Record{
		Kind: decision.KindQuota, At: 1, AccountID: "a1",
		Op: decision.OpRefresh, Before: nil, After: &after, SpentSinceCheck: 9.9,
	}
	if got := DefaultRegistry().Run(rec, NewState(8, 8)); len(got) != 0 {
		t.Fatalf("首次探针不该报：%+v", got)
	}
}
