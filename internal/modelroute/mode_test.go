package modelroute

import "testing"

// 生效时机只认两个值；空值保持向后兼容（= 立即改写）。
func TestValidateMode(t *testing.T) {
	base := Rule{FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free"}
	for _, tc := range []struct {
		name string
		mode string
		ok   bool
	}{
		{"空值=always", "", true},
		{"always", ModeAlways, true},
		{"onExhausted", ModeOnExhausted, true},
		{"大小写不敏感", "OnExhausted", true},
		{"非法值", "sometimes", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := base
			rule.Mode = tc.mode
			err := Validate([]Rule{rule})
			if tc.ok && err != nil {
				t.Fatalf("应通过校验: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("非法 mode 应被拒绝")
			}
		})
	}
}

func TestRuleModeDefault(t *testing.T) {
	if got := (Rule{}).ModeOrDefault(); got != ModeAlways {
		t.Fatalf("空 mode 应按 always，实际 %q", got)
	}
	if (Rule{}).OnExhausted() {
		t.Fatal("空 mode 不该是 onExhausted")
	}
	if !(Rule{Mode: ModeOnExhausted}).OnExhausted() {
		t.Fatal("onExhausted 判定失效")
	}
	if (Rule{Mode: ModeAlways}).OnExhausted() {
		t.Fatal("always 不该被判成 onExhausted")
	}
	if !(Rule{Mode: " ONEXHAUSTED "}).OnExhausted() {
		t.Fatal("mode 应大小写/空格不敏感")
	}
}

// 审计日志必须能看出这条规则是「立即改写」还是「兜底」，否则事后追不了账。
func TestAuditLineIncludesMode(t *testing.T) {
	got := AuditLine([]Rule{{
		FromSite: "domestic", Match: "hy4free", Site: "global", Target: "hy4free",
		Mode: ModeOnExhausted,
	}})
	want := "[onExhausted]domestic:hy4free→global/hy4free"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
