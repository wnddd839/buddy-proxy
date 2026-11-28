package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// 物理上不可能的比例必须被拒收。
//
// 实测踩过的坑：采样 bug 期间 1.2MB 长上下文的 tokRatio 被算成 2.11
// （真实 0.31）。修复 tokRatio 之后，Backfill 仍会把**历史记录里**的
// 2.11 装回基线 —— 因为恢复逻辑只信任 record 里的值，不校验物理可能。
//
// 闸门的依据：官方 reference 的最高比例是符号的 0.8238（tokfp.ReferenceSym），
// 真实混合文本不可能超过 1.0。
func TestTokRatioPlausibleGate(t *testing.T) {
	cases := []struct {
		ratio float64
		want  bool
		why   string
	}{
		{2.11, false, "采样 bug 产生的假值（实测）"},
		{1.5, false, "一倍以上不可能"},
		{1.0, true, "边界：全符号文本的理论上限"},
		{0.8238, true, "官方 reference 的符号比例"},
		{0.6371, true, "中文实测值"},
		{0.4552, true, "数字实测值"},
		{0.3073, true, "代码实测值"},
		{0.05, true, "边界：极稀疏文本"},
		{0.01, false, "过于稀疏，可能是单位错误"},
		{0, false, "未观测"},
	}
	for _, c := range cases {
		if got := tokRatioPlausible(c.ratio); got != c.want {
			t.Errorf("tokRatioPlausible(%.4f) = %v, want %v（%s）",
				c.ratio, got, c.want, c.why)
		}
	}
}

// 闸门必须真的挡住 ObserveTokRatio：不合理值不建基线。
func TestObserveTokRatioRejectsImplausible(t *testing.T) {
	st := NewState(8, 8)
	if b, _ := st.ObserveTokRatio("m", tokfp.BucketASCII, 2.11); b.Samples != 0 {
		t.Fatalf("2.11 不该建立基线：%+v", b)
	}
	if _, ok := st.TokBaseline("m", tokfp.BucketASCII); ok {
		t.Fatal("不该留下基线")
	}

	// 合理值正常建立
	if b, _ := st.ObserveTokRatio("m", tokfp.BucketASCII, 0.31); b.Samples == 0 {
		t.Fatal("0.31 应正常建立基线")
	}
}

// 对照：没有闸门时，错误值会被固化成「正常」。
// 这条测试描述的是「闸门存在的原因」，用来说明问题而非验证实现。
func TestWithoutGateBadValueBecomesBaseline(t *testing.T) {
	// 手动模拟无闸门路径：直接往 map 写
	st := NewState(8, 8)
	st.mu.Lock()
	st.tokBaseline["m|"+tokfp.BucketASCII] = &tokBaseline{Fast: 2.11, Slow: 2.11}
	st.mu.Unlock()

	if b, ok := st.TokBaseline("m", tokfp.BucketASCII); ok {
		t.Logf("无闸门时的结果：基线被固化为 %.2f —— 之后所有真实值(0.31)都会被判偏离 85%%", b.Fast)
	}
}
