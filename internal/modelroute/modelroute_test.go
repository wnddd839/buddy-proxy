package modelroute

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExactMatch(t *testing.T) {
	f := Open(filepath.Join(t.TempDir(), "r.json"))
	if err := f.Write(Store{Rules: []Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
		{FromSite: "domestic", Match: "GLM-5.3-Flash", Site: "domestic", Target: "glm-5.3-flash"},
	}}); err != nil {
		t.Fatal(err)
	}

	r, ok := f.Resolve("domestic", "gpt-5.6")
	if !ok || r.Site != "global" || r.Target != "gpt-5.6" {
		t.Fatalf("精确匹配失败: %+v ok=%v", r, ok)
	}
	r, ok = f.Resolve("domestic", "glm-5.3-flash")
	if !ok || r.Site != "domestic" || r.Target != "glm-5.3-flash" {
		t.Fatalf("大小写不敏感匹配失败: %+v ok=%v", r, ok)
	}
	// 方向不同：国际站请求不该命中「国内→国际」的规则
	if _, ok = f.Resolve("global", "gpt-5.6"); ok {
		t.Fatal("反方向不该命中")
	}
	if _, ok = f.Resolve("domestic", "cn:gpt-5.6"); ok {
		t.Fatal("带前缀的模型不该被路由改写")
	}
	if _, ok = f.Resolve("domestic", "other"); ok {
		t.Fatal("未配置模型不该命中")
	}
}

func TestResolveFirstWins(t *testing.T) {
	f := Open(filepath.Join(t.TempDir(), "r.json"))
	if err := f.Write(Store{Rules: []Rule{
		{FromSite: "domestic", Match: "x", Site: "domestic", Target: "a"},
		{FromSite: "domestic", Match: "y", Site: "global", Target: "b"},
	}}); err != nil {
		t.Fatal(err)
	}
	r, ok := f.Resolve("domestic", "x")
	if !ok || r.Target != "a" {
		t.Fatalf("x 应命中第一条: %+v", r)
	}
	r2, ok2 := f.Resolve("domestic", "y")
	if !ok2 || r2.Target != "b" {
		t.Fatalf("y 应命中第二条: %+v", r2)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := [][]Rule{
		{{FromSite: "domestic", Match: "", Site: "global", Target: "x"}},
		{{FromSite: "domestic", Match: "dup", Site: "global", Target: "a"}, {FromSite: "domestic", Match: "DUP", Site: "global", Target: "b"}},
		{{FromSite: "domestic", Match: "x", Site: "global", Target: ""}},
		{{Match: "x", Site: "global", Target: "y"}},                     // 缺 fromSite
		{{FromSite: "domestic", Match: "x", Target: "y"}},               // 缺目标站点
		{{FromSite: "nope", Match: "x", Site: "global", Target: "y"}},   // 非法源站点
		{{FromSite: "domestic", Match: "x", Site: "nope", Target: "y"}}, // 非法目标站点
	}
	for i, rules := range cases {
		if err := Validate(rules); err == nil {
			t.Fatalf("case %d 应被拒绝", i)
		}
	}
	// 同区替身（domestic → domestic）合法
	if err := Validate([]Rule{{FromSite: "domestic", Match: "x", Site: "domestic", Target: "y"}}); err != nil {
		t.Fatalf("同区替身应合法: %v", err)
	}
	// 双向各一条合法
	if err := Validate([]Rule{
		{FromSite: "domestic", Match: "x", Site: "global", Target: "a"},
		{FromSite: "global", Match: "x", Site: "domestic", Target: "b"},
	}); err != nil {
		t.Fatalf("双向配置应合法: %v", err)
	}
}

func TestRoundTripOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Rules) != 0 {
		t.Fatalf("want empty, got %+v", store.Rules)
	}
}

// AuditLine 把整表压成一行，供审计日志使用。
func TestAuditLine(t *testing.T) {
	if got := AuditLine(nil); got != "(empty)" {
		t.Fatalf("空表: %q", got)
	}
	got := AuditLine([]Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
		{FromSite: "global", Match: "glm-5.3", Site: "domestic", Target: "glm-5.3"},
	})
	want := "domestic:gpt-5.6→global/gpt-5.6, global:glm-5.3→domestic/glm-5.3"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
