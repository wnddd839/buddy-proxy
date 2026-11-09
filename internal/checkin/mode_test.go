package checkin

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestNormalizeModeDerivation(t *testing.T) {
	day := "2026-11-07"

	cases := []struct {
		name     string
		rule     Rule
		wantMode string
		wantMin  int
		wantMax  int
	}{
		{"旧规则：只有开始 → fixed", Rule{AccountID: "a", Enabled: true, Start: "09:00"}, "fixed", 9 * 60, 9 * 60},
		{"旧规则：开始+结束 → window", Rule{AccountID: "a", Enabled: true, Start: "09:00", End: "11:00"}, "window", 9 * 60, 11 * 60},
		{"旧规则：全空 → allDay", Rule{AccountID: "a", Enabled: true}, "allDay", 0, 23*60 + 59},
		{"显式 human 忽略时间窗口", Rule{AccountID: "a", Enabled: true, Mode: "human", Start: "01:00", End: "02:00"}, "human", 0, 0},
	}
	for _, tc := range cases {
		got, err := normalizeRules([]Rule{tc.rule})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(got) != 1 {
			t.Fatalf("%s: want 1, got %d", tc.name, len(got))
		}
		if got[0].Mode != tc.wantMode {
			t.Fatalf("%s: mode want %q got %q", tc.name, tc.wantMode, got[0].Mode)
		}
		if got[0].StartMin != tc.wantMin || got[0].EndMin != tc.wantMax {
			t.Fatalf("%s: window want [%d,%d] got [%d,%d]", tc.name, tc.wantMin, tc.wantMax, got[0].StartMin, got[0].EndMin)
		}
		_ = day
	}
}

func TestNormalizeOffsetClamp(t *testing.T) {
	got, err := normalizeRules([]Rule{{AccountID: "a", Enabled: true, Start: "09:00", Offset: 5000}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].OffsetMin != maxOffsetMinutes {
		t.Fatalf("offset 应被夹到 %d，got %d", maxOffsetMinutes, got[0].OffsetMin)
	}
	got2, err := normalizeRules([]Rule{{AccountID: "a", Enabled: true, Start: "09:00", Offset: -9999}})
	if err != nil {
		t.Fatal(err)
	}
	if got2[0].OffsetMin != -maxOffsetMinutes {
		t.Fatalf("负偏移应被夹到 -%d，got %d", maxOffsetMinutes, got2[0].OffsetMin)
	}
}

func TestNormalizeRejectsBadMode(t *testing.T) {
	if _, err := normalizeRules([]Rule{{AccountID: "a", Enabled: true, Mode: "chaos"}}); err == nil {
		t.Fatal("未知 mode 应报错")
	}
}

// fixed + offset：偏移把时刻平移，并夹进当天范围。
func TestDueAtFixedWithOffset(t *testing.T) {
	nr := normalizedRule{AccountID: "a", Mode: "fixed", StartMin: 23 * 60, EndMin: 23 * 60, OffsetMin: 120}
	at := testDueAt(nr, "2026-11-07").In(beijing)
	// 23:00 + 120min 会跨过零点，应被夹到 23:59
	if at.Hour() != 23 || at.Minute() != 59 {
		t.Fatalf("want 23:59（夹到当天末尾），got %s", at.Format("15:04"))
	}
}

// 负偏移同理夹到当天开头。
func TestDueAtFixedNegativeOffsetClampsToStart(t *testing.T) {
	nr := normalizedRule{AccountID: "a", Mode: "fixed", StartMin: 0, EndMin: 0, OffsetMin: -30}
	at := testDueAt(nr, "2026-11-07").In(beijing)
	if at.Hour() != 0 || at.Minute() != 0 {
		t.Fatalf("want 00:00，got %s", at.Format("15:04"))
	}
}

// human 模式：结果落在仿人时段内（07:00–23:00），且同一天稳定。
func TestDueAtHuman(t *testing.T) {
	nr := normalizedRule{AccountID: "a", Mode: "human"}
	first := testDueAt(nr, "2026-11-07").In(beijing)
	if first.Hour() < 7 || first.Hour() >= 23 {
		t.Fatalf("human 模式应落在 07:00-22:59，got %s", first.Format("15:04"))
	}
	same := testDueAt(nr, "2026-11-07")
	if !same.Equal(first) {
		t.Fatal("同一天必须稳定")
	}
}

// window 模式保持原行为：落在窗口内。
func TestDueAtWindowUnchanged(t *testing.T) {
	nr := normalizedRule{AccountID: "a", Mode: "window", StartMin: 9 * 60, EndMin: 11 * 60}
	for d := 1; d <= 28; d++ {
		day := fmt.Sprintf("2026-11-%02d", d)
		at := testDueAt(nr, day).In(beijing)
		m := at.Hour()*60 + at.Minute()
		if m < 9*60 || m > 11*60 {
			t.Fatalf("%s 时刻 %s 落在窗口外", day, at.Format("15:04"))
		}
	}
}

// human 分布抽查：多账号多天里凌晨一个都不该出现，午休+晚间应占多数。
func TestDueAtHumanDistribution(t *testing.T) {
	counts := map[string]int{}
	total := 0
	for d := 1; d <= 28; d++ {
		day := fmt.Sprintf("2026-11-%02d", d)
		for i := 0; i < 20; i++ {
			nr := normalizedRule{AccountID: fmt.Sprintf("acct%02d", i), Mode: "human"}
			at := testDueAt(nr, day).In(beijing)
			h := at.Hour()
			total++
			switch {
			case h < 7:
				counts["凌晨"]++
			case h < 9:
				counts["早"]++
			case h < 12:
				counts["上午"]++
			case h < 14:
				counts["午休"]++
			case h < 18:
				counts["午后"]++
			case h < 21:
				counts["晚间"]++
			default:
				counts["睡前"]++
			}
		}
	}
	if counts["凌晨"] != 0 {
		t.Fatalf("human 模式不应出现凌晨，got %d", counts["凌晨"])
	}
	hot := counts["午休"] + counts["晚间"]
	if hot*2 < total/2 {
		t.Fatalf("午休+晚间应占明显多数（~50%%），got %d/%d", hot, total)
	}
}

// 旧 JSON（无 mode/offset 字段）必须能解析。
func TestOldJSONBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	old := `{"version":1,"rules":[{"accountId":"a1","enabled":true,"start":"09:00","end":"11:00"}]}`
	if err := writeFile(path, old); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Rules) != 1 || store.Rules[0].Start != "09:00" {
		t.Fatalf("旧文件应原样读回: %+v", store.Rules)
	}
	nr, err := normalizeRules(store.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if nr[0].Mode != "window" {
		t.Fatalf("旧规则应推导为 window，got %q", nr[0].Mode)
	}
}
