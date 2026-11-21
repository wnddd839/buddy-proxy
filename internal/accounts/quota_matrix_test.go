package accounts

import (
	"testing"
	"time"
)

// 两个额度判定函数必须对同一份数据给出同一结论。
//
// 它们语义不同（< floor vs <= floor），但「这份快照值不值得信」这件事
// 只能有一种答案 —— 否则一处拦住、另一处放行，就是已知那个后门的温床。
func TestQuotaPredicatesAgree(t *testing.T) {
	now := int64(1_700_000_000_000)
	hour := int64(time.Hour / time.Millisecond)
	future := now + 24*hour
	past := now - hour
	rem := func(v float64) *float64 { return &v }

	cases := []struct {
		name        string
		acct        Account
		wantBelow   bool // QuotaBelowFloor(_, now, 20)
		wantBlocked bool // QuotaBlocked(_, now)  （floor=0）
	}{
		{
			name:        "nil 余额按设计放行",
			acct:        Account{},
			wantBelow:   false,
			wantBlocked: false,
		},
		{
			name:        "0 余额 + 新鲜快照",
			acct:        Account{QuotaRemaining: rem(0), QuotaCheckedAt: now - 1000, QuotaResetAt: future},
			wantBelow:   true,
			wantBlocked: true,
		},
		{
			name:        "0 余额 + 时间未知（checkedAt=0）",
			acct:        Account{QuotaRemaining: rem(0), QuotaCheckedAt: 0, QuotaResetAt: future},
			wantBelow:   true,
			wantBlocked: true,
		},
		{
			// 没有重置时间不等于「可以用」：0 余额先拦住，
			// 6 小时快照过期后自然释放。
			name:        "0 余额 + 无重置时间",
			acct:        Account{QuotaRemaining: rem(0), QuotaCheckedAt: now - 1000},
			wantBelow:   true,
			wantBlocked: true,
		},
		{
			name:        "0 余额 + 快照陈旧",
			acct:        Account{QuotaRemaining: rem(0), QuotaCheckedAt: now - 7*hour, QuotaResetAt: future},
			wantBelow:   false,
			wantBlocked: false,
		},
		{
			name:        "0 余额 + 重置时刻已过",
			acct:        Account{QuotaRemaining: rem(0), QuotaCheckedAt: now - 1000, QuotaResetAt: past},
			wantBelow:   false,
			wantBlocked: false,
		},
		{
			name:        "余额充足",
			acct:        Account{QuotaRemaining: rem(50), QuotaCheckedAt: now - 1000, QuotaResetAt: future},
			wantBelow:   false,
			wantBlocked: false,
		},
		{
			name:        "不限量",
			acct:        Account{QuotaUnlimited: true, QuotaRemaining: rem(0), QuotaCheckedAt: now - 1000, QuotaResetAt: future},
			wantBelow:   false,
			wantBlocked: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			below := QuotaBelowFloor(c.acct, now, 20)
			blocked := QuotaBlocked(c.acct, now)
			if below != c.wantBelow {
				t.Errorf("QuotaBelowFloor = %v, want %v", below, c.wantBelow)
			}
			if blocked != c.wantBlocked {
				t.Errorf("QuotaBlocked = %v, want %v", blocked, c.wantBlocked)
			}
			if below != blocked {
				t.Errorf("两个判定分叉：below=%v blocked=%v", below, blocked)
			}
		})
	}
}
