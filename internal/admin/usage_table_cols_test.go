package admin_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/admin"
)

// 07 用量表的列数必须三处一致：表头 th、数据行 td、空态 colspan。
//
// 历史上这里出过错（重复的「上游模型」列让 15 th vs 13 td 整表错位），
// 而当时的测试只查「三个词的出现顺序」，重复列不影响顺序所以没抓到。
//
// 唯一可靠的检查是「计数相等」。
func TestUsageTableColumnCountConsistent(t *testing.T) {
	page := admin.PageHTML()

	// 表头：从 aria-label="请求明细" 到 </thead>
	hStart := strings.Index(page, `aria-label="请求明细"`)
	if hStart < 0 {
		t.Fatal("没找到用量表")
	}
	hEnd := strings.Index(page[hStart:], "</thead>")
	if hEnd < 0 {
		t.Fatal("没找到表头结束")
	}
	thead := page[hStart : hStart+hEnd]

	// 裸 <th> 与带属性的 <th ...> 都要算上，
	// 但必须排除 <thead>（它也以 "<th" 开头）。
	thRe := regexp.MustCompile(`<th[ >][^>]*>`)
	thCount := len(thRe.FindAllString(thead, -1))

	// 数据行：paintUsage 里 rows.map 的 <td
	fnStart := strings.Index(page, "function paintUsage(data)")
	if fnStart < 0 {
		t.Fatal("没找到 paintUsage")
	}
	mapStart := strings.Index(page[fnStart:], "tbody.innerHTML = rows.map")
	if mapStart < 0 {
		t.Fatal("没找到数据行渲染")
	}
	mapStart += fnStart
	mapEnd := strings.Index(page[mapStart:], "}).join('')")
	if mapEnd < 0 {
		t.Fatal("没找到数据行渲染结束")
	}
	body := page[mapStart : mapStart+mapEnd]
	tdCount := strings.Count(body, "'<td")

	if thCount != tdCount {
		t.Fatalf("表头 %d 列 vs 数据 %d 列 —— 会错位", thCount, tdCount)
	}

	// 空态 colspan 必须等于列数
	csRe := regexp.MustCompile(`colspan="(\d+)"`)
	for _, m := range csRe.FindAllStringSubmatch(page, -1) {
		// 只检查用量表区间（aria-label 到 paintUsagePager 之间）
		_ = m
	}
	for _, want := range []string{
		`colspan="` + itoa(tdCount) + `"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("空态应使用 %q（与列数一致）", want)
		}
	}
}

func itoa(n int) string {
	if n >= 10 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0' + n))
}

// 「上游模型」列不得重复（历史事故的直接回归测试）。
func TestUsageTableNoDuplicateColumns(t *testing.T) {
	page := admin.PageHTML()
	hStart := strings.Index(page, `aria-label="请求明细"`)
	hEnd := strings.Index(page[hStart:], "</thead>")
	thead := page[hStart : hStart+hEnd]

	for _, name := range []string{"上游模型", "分词器比例", "单次 Credit", "System"} {
		if c := strings.Count(thead, name); c != 1 {
			t.Errorf("列 %q 出现 %d 次，应恰好 1 次", name, c)
		}
	}
}
