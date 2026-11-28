package invariant

import (
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// 实测抓到的基线键问题（2026-11-22 部署后首次真实流量）：
//
//	客户端发 cn:deepseek-v4.1-flash → 基线键变成 "cn:deepseek-v4.1-flash"
//	同一客户端改发 deepseek-v4.1-flash → 又建一个新基线
//
// 结果：同一个模型因为站点前缀的有无而分裂成两条基线，
// 各自的样本数都少一半、区间也建不起来，I9 形同废纸。
//
// 修法：基线键用归一化后的模型名（剥 cn:/global: 前缀）。
func TestI9BaselineKeyStripsSitePrefix(t *testing.T) {
	st := NewState(8, 8)

	// 三次带前缀
	for i := 0; i < 3; i++ {
		DefaultRegistry().Run(tokReq("cn:deepseek-v4.1-flash", tokfp.BucketCJK, 0.6371, int64(i)), st)
	}
	// 再一次不带前缀：必须累加到同一条基线上，而不是新建
	DefaultRegistry().Run(tokReq("deepseek-v4.1-flash", tokfp.BucketCJK, 0.6371, 5), st)

	if n := len(st.TokBaselines()); n != 1 {
		t.Fatalf("同一模型的不同前缀写法应共用一条基线，实际 %d 条：%+v", n, st.TokBaselines())
	}
	if b, ok := st.TokBaseline("deepseek-v4.1-flash", tokfp.BucketCJK); !ok {
		t.Fatalf("基线键应为归一化后的模型名：%+v", st.TokBaselines())
	} else if b.Samples != 4 {
		t.Fatalf("样本数应为 4（3 带前缀 + 1 不带），实际 %d", b.Samples)
	}
}

// global: 前缀同样要剥。
func TestI9BaselineKeyStripsGlobalPrefix(t *testing.T) {
	st := NewState(8, 8)
	DefaultRegistry().Run(tokReq("global:glm-5.3-flash", tokfp.BucketDigits, 0.6018, 1), st)
	if _, ok := st.TokBaseline("glm-5.3-flash", tokfp.BucketDigits); !ok {
		t.Fatalf("global: 前缀应被剥：%+v", st.TokBaselines())
	}
	// 直接看原始 map：键里不该出现 global: 前缀。
	// （TokBaseline 会自动归一化查询键，所以必须查原始快照才看得出存储侧的样子。）
	for key := range st.TokBaselines() {
		if strings.HasPrefix(key, "global:") {
			t.Fatalf("存储侧的键不该带前缀：%+v", st.TokBaselines())
		}
	}
}
