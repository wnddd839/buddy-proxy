package server

import (
	"os"
	"strings"
	"testing"
)

// 出图与搜索在 server 侧必须把 proxyRequestID 传进 Options，
// 否则这些路径的 selection 记录 requestId 为空，无法与 request 配对。
//
// 这条测试用源码扫描而不是跑请求：这些路径会真的打上游，不适合在单测里跑。
func TestServerPassesRequestIDToImageAndSearch(t *testing.T) {
	src := readServerSource(t)

	// 每个 Options 构造都必须紧跟着 RequestID 赋值。
	for _, want := range []struct {
		needle string
		count  int
		why    string
	}{
		{"gateway.ImageOptions{\n\t\tRequestID:", 1, "出图"},
		{"gateway.SearchOptions{\n\t\tRequestID:", 2, "搜索 / webfetch"},
		{"SearchOptions: gateway.SearchOptions{\n\t\t\tRequestID:", 2, "agentic search（流式与非流式）"},
	} {
		if got := strings.Count(src, want.needle); got != want.count {
			t.Errorf("%s 的 RequestID 透传数 = %d，want %d", want.why, got, want.count)
		}
	}
}

func readServerSource(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, name := range []string{"images.go", "search.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(string(raw))
	}
	return b.String()
}
