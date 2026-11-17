package systemprompt

import "testing"

func TestFingerprintStableAndOpaque(t *testing.T) {
	a := Fingerprint("You are Claude Code, Anthropic's official CLI for Claude.")
	b := Fingerprint("You are Claude Code, Anthropic's official CLI for Claude.")
	c := Fingerprint("You are opencode.")

	if a != b {
		t.Fatalf("同内容指纹应稳定：%s vs %s", a, b)
	}
	if a == c {
		t.Fatal("不同内容指纹应不同")
	}
	if len(a) != 8 {
		t.Fatalf("指纹应为 8 位，实际 %d: %s", len(a), a)
	}
	for _, r := range a {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("指纹只能是小写 hex：%q", a)
		}
	}
}

func TestFingerprintEmpty(t *testing.T) {
	if got := Fingerprint(""); got != "" {
		t.Fatalf("空 system 应返回空指纹，实际 %q", got)
	}
	if got := Fingerprint("   \n\t "); got != "" {
		t.Fatalf("纯空白 system 应返回空指纹，实际 %q", got)
	}
}

func TestFingerprintDoesNotContainPlaintext(t *testing.T) {
	const secret = "ZQ7X-SECRET-MARKER"
	got := Fingerprint("prefix " + secret + " suffix")
	if len(got) != 8 {
		t.Fatalf("长度不对：%q", got)
	}
	if got == secret {
		t.Fatal("指纹不得等于原文")
	}
}

func TestEventIsProblem(t *testing.T) {
	cases := []struct {
		name string
		e    Event
		want bool
	}{
		{"正常", Event{Status: 200}, false},
		{"11128 拦截", Event{Status: 502, ErrorCode: "11128"}, true},
		{"降级", Event{Status: 200, Degraded: true}, true},
		{"传输层失败 Status=0", Event{Status: 0}, true},
		{"其它错误", Event{Status: 500}, true},
	}
	for _, c := range cases {
		if got := c.e.IsProblem(); got != c.want {
			t.Fatalf("%s: IsProblem()=%v want %v", c.name, got, c.want)
		}
	}
}
