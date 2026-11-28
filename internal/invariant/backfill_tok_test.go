package invariant

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// Backfill 必须从历史 request 记录重建分词器基线。
//
// 不重建的后果（实测踩过）：重启后基线从零开始，前 3 个样本即固化。
// 若那 3 个恰好异常（例如采样 bug 期间的假值 2.11，真实值 0.31），
// 错误就被当成「正常」，之后所有真实值都被判偏离。
func TestBackfillRebuildsTokBaseline(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	// 灌入 3 条历史 request（固定 ratio，模拟稳定基线）
	for i := 0; i < 3; i++ {
		if err := st.Append(decision.Record{
			Kind:      decision.KindRequest,
			At:        now.Add(-time.Hour).UnixMilli() + int64(i),
			Model:     "deepseek-v4.1-flash",
			Ok:        true,
			TokBucket: tokfp.BucketCJK,
			TokRatio:  0.6371,
		}); err != nil {
			t.Fatal(err)
		}
	}

	state := NewState(64, 64)
	sink := NewSink(st, DefaultRegistry(), state, 50, 128)
	if err := sink.Backfill(now); err != nil {
		t.Fatal(err)
	}

	if b, ok := state.TokBaseline("deepseek-v4.1-flash", tokfp.BucketCJK); !ok {
		t.Fatalf("Backfill 应重建基线：%+v", state.TokBaselines())
	} else {
		if b.Samples != 3 {
			t.Fatalf("样本数应为 3，实际 %d", b.Samples)
		}
		if b.Fast < 0.63 || b.Fast > 0.64 {
			t.Fatalf("Fast 应约 0.6371，实际 %.4f", b.Fast)
		}
	}
}

// 重建后的基线必须立刻可用于判定：
// 下一条真实请求（不同 ratio）应当命中 I9。
func TestBackfillBaselineIsUsable(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		_ = st.Append(decision.Record{
			Kind:  decision.KindRequest,
			At:    now.Add(-time.Hour).Add(time.Duration(i)).UnixMilli(),
			Model: "m", Ok: true,
			TokBucket: tokfp.BucketDigits, TokRatio: 0.4552,
		})
	}

	state := NewState(64, 64)
	sink := NewSink(st, DefaultRegistry(), state, 50, 128)
	if err := sink.Backfill(now); err != nil {
		t.Fatal(err)
	}

	// 模拟模型被换掉：ratio 跳到 0.6018（值差 32%）
	findings := DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: now.UnixMilli(),
		Model: "m", Ok: true, RequestID: "r-new",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.6018,
	}, state)
	var hit bool
	for _, f := range findings {
		if f.Invariant == "I9" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("重建后的基线应能立即抓到突变：%+v", findings)
	}
}

// 采样记录（TokRatio=0）不得污染基线。
func TestBackfillSkipsSampledRecords(t *testing.T) {
	st, err := decision.OpenStore(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = st.Append(decision.Record{
		Kind: decision.KindRequest, At: now.Add(-time.Hour).UnixMilli(),
		Model: "m", Ok: true, TokBucket: tokfp.BucketASCII,
		TokRatio: 0, // 采样后 tokRatio 会是 0
	})
	state := NewState(64, 64)
	sink := NewSink(st, DefaultRegistry(), state, 50, 128)
	_ = sink.Backfill(now)
	if b, ok := state.TokBaseline("m", tokfp.BucketASCII); ok {
		t.Fatalf("ratio=0 的记录不该进基线：%+v", b)
	}
}

var _ = tokfp.MaxScanChars
