package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 锁住失败转储的截断行为：一份 4MB 的正文不应该原样落盘，
// 否则失败路径的内存与磁盘都按 body 大小放大。
func TestDumpUpstreamFailureShrinksHugeStrings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dumps")
	huge := strings.Repeat("x", 4<<20)
	body := map[string]any{
		"model": "deepseek-v4.1-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": huge},
		},
	}

	base, err := dumpUpstreamFailure(dir, 400, body, "prompt is too long")
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		t.Fatal("应写出转储文件")
	}

	info, err := os.Stat(base + ".request.json")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 2<<20 {
		t.Fatalf("转储体积未被压住：%d 字节", info.Size())
	}

	raw, _ := os.ReadFile(base + ".request.json")
	if !strings.Contains(string(raw), "TRUNCATED") {
		t.Fatal("截断标记未写入")
	}

	// 原始 body 不能被修改：上游请求与日志仍要用完整内容。
	if got := body["messages"].([]any)[0].(map[string]any)["content"].(string); len(got) != 4<<20 {
		t.Fatalf("原 body 被修改，长度=%d", len(got))
	}
}

// 小字符串必须原样保留 —— 转储的价值就在这些细节上。
func TestDumpUpstreamFailureKeepsSmallStringsIntact(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dumps")
	body := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{"function": map[string]any{"name": "bash", "arguments": `{"cmd":"ls"}`}},
				},
			},
		},
	}

	base, err := dumpUpstreamFailure(dir, 400, body, "invalid function call parameters (code 11129)")
	if err != nil || base == "" {
		t.Fatalf("dump 失败: err=%v base=%q", err, base)
	}
	raw, _ := os.ReadFile(base + ".request.json")
	var round map[string]any
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("转储不是合法 JSON：%v", err)
	}
	if !strings.Contains(string(raw), `{\"cmd\":\"ls\"}`) && !strings.Contains(string(raw), `{"cmd":"ls"}`) {
		t.Fatalf("小参数被改动：%s", raw)
	}
}

// 总预算生效：多个中等字符串加起来也不能把转储撑爆。
func TestDumpShrinkerBudgetCapsTotal(t *testing.T) {
	d := &dumpShrinker{budget: 1000}
	msgs := make([]any, 100)
	for i := range msgs {
		msgs[i] = map[string]any{"content": strings.Repeat("y", 500)}
	}
	out := d.shrink(map[string]any{"messages": msgs})

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	// 100×500 字节的正文，预算只有 1000：加上结构开销也应在几十 KB 内。
	if len(raw) > 50_000 {
		t.Fatalf("预算未生效，序列化后 %d 字节", len(raw))
	}
}
