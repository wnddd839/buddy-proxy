package decision_test

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 跨包一致性：accounts 里的清空理由必须全部在 decision 的白名单里，
// 否则 I1 会对每一次合法清空报 violation（评审 C2 的成因）。
//
// 这个测试放在 decision 包：accounts 依赖 decision 是单向的，
// 反过来 import 会绕圈，所以用「字符串表对照」而不是类型引用。
func TestClearReasonConstantsMatchAccounts(t *testing.T) {
	// 与 internal/accounts/pool.go 的 ClearReason* 常量逐一对应。
	// 改任一处的常量值都必须同步改这里 —— 这正是本测试的目的。
	accountReasons := []string{
		"quota_recovered",
		"invalid_snapshot",
	}
	for _, r := range accountReasons {
		if _, ok := decision.AllowedClearReasons[r]; !ok {
			t.Errorf("accounts 会发出 reason=%q 的清空事件，但 decision 的白名单里没有它 —— I1 会误报", r)
		}
	}
}

// 白名单里不应有空理由。
func TestAllowedClearReasonsAreNonEmpty(t *testing.T) {
	for r := range decision.AllowedClearReasons {
		if r == "" {
			t.Error("白名单里不应有空理由")
		}
	}
}
