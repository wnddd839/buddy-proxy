package checkin

import (
	"testing"
)

// testKey 是固定密钥，保证分布测试可复现。
func distKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i*7 + 11)
	}
	return k
}

// TestSpreadAllDayNoHardEdge 验证 07:00 不再有硬台阶。
//
// 旧实现把一天切成「夜间 0-7 点」和「白天 7-23:30」两段硬边界，
// 导致 06 时几乎没有、07 时突然满格 —— 这个台阶本身就是可识别特征。
// 新实现让凌晨权重从 0 点起逐渐上升，07:00 前后应当平滑过渡，
// 即 05-06 时与 07-08 时的样本量差距不能过大。
func TestSpreadAllDayNoHardEdge(t *testing.T) {
	key := distKey()
	const days = 400
	buckets := make(map[int]int)
	for d := 0; d < days; d++ {
		day := dayOffset(d)
		sec := spreadAllDaySecond(key, "acct|"+day)
		buckets[sec/3600]++
	}

	// 统计 05、06、07、08 四个小时。
	h05, h06, h07, h08 := buckets[5], buckets[6], buckets[7], buckets[8]
	t.Logf("-05时=%d ->06时=%d ->07时=%d ->08时=%d", h05, h06, h07, h08)

	// 关键断言：06 时不能是 07 时的零头。
	// 旧实现这里是 142 vs 634（约 1:4.5 的断崖）。
	if h06*2 < h07 {
		t.Fatalf("06时=%d 与 07时=%d 之间仍有硬台阶（应平滑过渡）", h06, h07)
	}
	// 05 时也应有可观测的样本（深夜渐入，不是归零）。
	if h05 == 0 {
		t.Fatalf("05时样本为 0，凌晨渐变未生效")
	}
}

// TestSpreadAllDayNightShareStillBounded 验证夜间占比仍在合理范围。
//
// 平滑化不能把夜间占比抬得太高（那会变成"半夜签到"这种强特征）。
// 目标区间 6%~14%。
func TestSpreadAllDayNightShareStillBounded(t *testing.T) {
	key := distKey()
	const days = 2000
	const nightEnd = 7 * 3600
	night := 0
	for d := 0; d < days; d++ {
		sec := spreadAllDaySecond(key, "acct|"+dayOffset(d))
		if sec < nightEnd {
			night++
		}
	}
	share := float64(night) / float64(days)
	t.Logf("夜间(00:00-07:00)占比 = %.2f%%", share*100)
	if share < 0.06 || share > 0.14 {
		t.Fatalf("夜间占比 %.2f%% 超出 6%%~14%% 合理区间", share*100)
	}
}

// TestSpreadAllDayStaysInDay 验证边界不越界。
func TestSpreadAllDayStaysInDay(t *testing.T) {
	key := distKey()
	for d := 0; d < 500; d++ {
		sec := spreadAllDaySecond(key, "acct|"+dayOffset(d))
		if sec < 0 || sec >= 24*3600 {
			t.Fatalf("秒数越界: %d", sec)
		}
	}
}

// TestWindowSecondIsNearUniform 验证窗口模式不再挤成钟形。
//
// 旧实现用 0.45 权重的三角分布，实测前/后 10% 区间各只有 0.6%、
// 中间 10% 却有 17.9% —— 用户选「09:00-11:00」却拿不到边缘时刻。
// 新实现应当接近均匀：每个 10% 区间的占比应在 7%~14% 之间。
func TestWindowSecondIsNearUniform(t *testing.T) {
	key := distKey()
	lo, hi := 9*3600, 11*3600
	const n = 8000
	buckets := make([]int, 10)
	for i := 0; i < n; i++ {
		seed := "probe|" + itoa(i)
		sec := windowSecond(key, seed, lo, hi)
		if sec < lo || sec >= hi {
			t.Fatalf("窗口越界: %d 不在 [%d,%d)", sec, lo, hi)
		}
		idx := (sec - lo) * 10 / (hi - lo)
		if idx > 9 {
			idx = 9
		}
		buckets[idx]++
	}

	t.Logf("窗口十等分分布: %v", buckets)
	const ideal = float64(n) / 10.0
	for i, c := range buckets {
		share := float64(c) / ideal
		if share < 0.7 || share > 1.4 {
			t.Fatalf("第 %d 段占比 %.2fx 偏离均匀太远（%d 个，理想 %.0f）", i, share, c, ideal)
		}
	}
}

// TestWindowSecondDegenerateRange 验证退化区间不 panic、不越界。
func TestWindowSecondDegenerateRange(t *testing.T) {
	key := distKey()
	if got := windowSecond(key, "s", 10*3600, 10*3600); got != 10*3600 {
		t.Fatalf("空区间应返回 lo，得到 %d", got)
	}
	if got := windowSecond(key, "s", 11*3600, 10*3600); got != 11*3600 {
		t.Fatalf("倒置区间应返回 lo，得到 %d", got)
	}
}

// TestDueAtDeterministicPerDay 验证同一天多次调用得到同一时刻（跨重启稳定）。
func TestDueAtDeterministicPerDay(t *testing.T) {
	key := distKey()
	a := spreadAllDaySecond(key, "acct|2026-11-12")
	b := spreadAllDaySecond(key, "acct|2026-11-12")
	if a != b {
		t.Fatalf("同一天两次调用不一致: %d vs %d", a, b)
	}
	c := spreadAllDaySecond(key, "acct|2026-11-13")
	if a == c {
		t.Logf("提示：相邻两天恰好同秒（概率极低，可接受）: %d", a)
	}
}

// TestDueAtVariesByAccount 验证不同账号同一天得到不同时刻。
func TestDueAtVariesByAccount(t *testing.T) {
	key := distKey()
	seen := make(map[int]string, 8)
	for _, acc := range []string{"a1", "a2", "a3", "a4", "a5", "a6"} {
		sec := spreadAllDaySecond(key, acc+"|2026-11-12")
		if prev, dup := seen[sec]; dup {
			t.Fatalf("账号 %s 与 %s 撞车在同一秒 %d", acc, prev, sec)
		}
		seen[sec] = acc
	}
}

// ---------- 测试辅助 ----------

func dayOffset(d int) string {
	// 用固定基准日期加偏移，避免依赖真实时间。
	base := []int{2026, 1, 1}
	// 简化实现：天数直接转成 2026-01-01 起的天数（不做日历精确换算，
	// 分布测试只需要「不同的 day 字符串」）。
	year := base[0] + d/365
	doy := d%365 + 1
	month := (doy-1)/31 + 1
	day := (doy-1)%31 + 1
	return itoa(year) + "-" + pad2(month) + "-" + pad2(day)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
