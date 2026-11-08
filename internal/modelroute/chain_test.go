package modelroute

import (
	"os"
	"path/filepath"
	"testing"
)

// 同一源站 + 同一源模型的多条 = 备用链，按文件顺序返回。
func TestChainReturnsAllInOrder(t *testing.T) {
	f := Open(filepath.Join(t.TempDir(), "r.json"))
	if err := f.Write(Store{Rules: []Rule{
		{FromSite: "domestic", Match: "cn-test", Site: "global", Target: "gpt-5.6"},
		{FromSite: "domestic", Match: "cn-test", Site: "domestic", Target: "glm-5.3"},
		{FromSite: "domestic", Match: "other", Site: "global", Target: "x"},
	}}); err != nil {
		t.Fatal(err)
	}
	chain := f.Chain("domestic", "cn-test")
	if len(chain) != 2 {
		t.Fatalf("want 2 hops, got %d", len(chain))
	}
	if chain[0].Site != "global" || chain[1].Site != "domestic" {
		t.Fatalf("顺序错: %+v", chain)
	}
	if got := f.Chain("domestic", "other"); len(got) != 1 {
		t.Fatalf("other want 1, got %d", len(got))
	}
	if got := f.Chain("domestic", "nope"); len(got) != 0 {
		t.Fatalf("nope want 0, got %d", len(got))
	}
}

// 单行 = 固定路由。
func TestChainSingleIsFixed(t *testing.T) {
	f := Open(filepath.Join(t.TempDir(), "r.json"))
	if err := f.Write(Store{Rules: []Rule{
		{FromSite: "domestic", Match: "a", Site: "global", Target: "t"},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := f.Chain("domestic", "a"); len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}

// 方向是硬隔离：国内 → 国际的规则，只在「源站 = 国内」时命中。
func TestChainIsDirectional(t *testing.T) {
	f := Open(filepath.Join(t.TempDir(), "r.json"))
	if err := f.Write(Store{Rules: []Rule{
		{FromSite: "domestic", Match: "gpt-5.6", Site: "global", Target: "gpt-5.6"},
		{FromSite: "global", Match: "gpt-5.6", Site: "domestic", Target: "glm-5.3"},
	}}); err != nil {
		t.Fatal(err)
	}
	fromDomestic := f.Chain("domestic", "gpt-5.6")
	if len(fromDomestic) != 1 || fromDomestic[0].Site != "global" {
		t.Fatalf("国内站应只命中「国内→国际」那条: %+v", fromDomestic)
	}
	fromGlobal := f.Chain("global", "gpt-5.6")
	if len(fromGlobal) != 1 || fromGlobal[0].Site != "domestic" {
		t.Fatalf("国际站应只命中「国际→国内」那条: %+v", fromGlobal)
	}
	// 站点别名归一：cn 视同 domestic
	if got := f.Chain("cn", "gpt-5.6"); len(got) != 1 || got[0].Site != "global" {
		t.Fatalf("cn 别名应等同 domestic: %+v", got)
	}
}

// 大小写不敏感；带前缀不命中；空串不命中。
func TestChainMatchingRules(t *testing.T) {
	f := Open(filepath.Join(t.TempDir(), "r.json"))
	if err := f.Write(Store{Rules: []Rule{
		{FromSite: "domestic", Match: "GPT-5.6", Site: "global", Target: "gpt-5.6"},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := f.Chain("domestic", "gpt-5.6"); len(got) != 1 {
		t.Fatalf("大小写不敏感失败: %d", len(got))
	}
	if got := f.Chain("domestic", "cn:gpt-5.6"); len(got) != 0 {
		t.Fatalf("带前缀不该命中: %d", len(got))
	}
	if got := f.Chain("domestic", ""); len(got) != 0 {
		t.Fatalf("空名不该命中: %d", len(got))
	}
}

// Validate：同源站+同 match 允许多条（备用链，各跳站点不同），
// 同源站+同 match+同目标站点 拒绝；fromSite / site 必填且必须合法。
func TestValidateChain(t *testing.T) {
	// 同源站+同 match，不同目标站点：合法（备用链）
	if err := Validate([]Rule{
		{FromSite: "domestic", Match: "x", Site: "global", Target: "a"},
		{FromSite: "domestic", Match: "x", Site: "domestic", Target: "b"},
	}); err != nil {
		t.Fatalf("备用链应合法: %v", err)
	}
	// 两个方向各一条：合法
	if err := Validate([]Rule{
		{FromSite: "domestic", Match: "x", Site: "global", Target: "a"},
		{FromSite: "global", Match: "x", Site: "domestic", Target: "b"},
	}); err != nil {
		t.Fatalf("双向配置应合法: %v", err)
	}
	// 同源站+同 match+同目标站点：拒绝
	if err := Validate([]Rule{
		{FromSite: "domestic", Match: "x", Site: "global", Target: "a"},
		{FromSite: "domestic", Match: "x", Site: "global", Target: "b"},
	}); err == nil {
		t.Fatal("同源站+同 match+同目标站点应拒绝")
	}
	// 大小写变体同样算重复
	if err := Validate([]Rule{
		{FromSite: "domestic", Match: "x", Site: "global", Target: "a"},
		{FromSite: "DOMESTIC", Match: "X", Site: "Global", Target: "b"},
	}); err == nil {
		t.Fatal("大小写变体也应拒绝")
	}
	// 缺少 fromSite：拒绝（方向必须写明）
	if err := Validate([]Rule{{Match: "x", Site: "global", Target: "t"}}); err == nil {
		t.Fatal("缺少 fromSite 应拒绝")
	}
	// fromSite 非法：拒绝
	if err := Validate([]Rule{{FromSite: "mars", Match: "x", Site: "global", Target: "t"}}); err == nil {
		t.Fatal("非法 fromSite 应拒绝")
	}
	// 缺少 site：拒绝
	if err := Validate([]Rule{{FromSite: "domestic", Match: "x", Target: "t"}}); err == nil {
		t.Fatal("缺少目标站点应拒绝")
	}
	// 基础校验
	if err := Validate([]Rule{{FromSite: "domestic", Match: "", Site: "global", Target: "t"}}); err == nil {
		t.Fatal("空 match 应拒绝")
	}
	if err := Validate([]Rule{{FromSite: "domestic", Match: "x", Site: "global", Target: ""}}); err == nil {
		t.Fatal("空 target 应拒绝")
	}
}

// 旧文件（无 fromSite）能读出来，但不会被任何请求命中（方向缺失 = 不生效），
// 且写回时会被 Validate 拦下，提醒重新配置。
func TestChainOldFileNeedsMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"rules":[{"match":"a","site":"global","target":"t"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path).Read()
	if err != nil {
		t.Fatalf("旧文件应能读出: %v", err)
	}
	if len(store.Rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(store.Rules))
	}
	// 缺 fromSite ⇒ 任何站点都不命中，绝不会被误用
	if got := Open(path).Chain("domestic", "a"); len(got) != 0 {
		t.Fatalf("缺 fromSite 的旧规则不该命中，got %d", len(got))
	}
	if got := Open(path).Chain("global", "a"); len(got) != 0 {
		t.Fatalf("缺 fromSite 的旧规则不该命中，got %d", len(got))
	}
	// 写回时被拦下
	if err := Open(path).Write(store); err == nil {
		t.Fatal("缺 fromSite 的旧规则写回应被 Validate 拒绝")
	}
}
