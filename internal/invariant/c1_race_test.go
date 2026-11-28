package invariant

import (
	"sync"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// C1 回归：ObserveTokRatio 在并发下不得有数据竞争。
//
// 历史缺陷：它返回 map 内的 `*tokBaseline` 裸指针，调用方（checks_tokfp）
// 在锁外读 Fast/Slow —— 另一 goroutine 同时写就有竞争。测试全串行，
// 所以 `-race` 一直是绿的，生产上并发请求才会炸。
//
// 修法是返回**值副本**。这条测试用并发读写把问题钉住：
// 若哪天有人把返回值改回指针，-race 会立刻报。
func TestObserveTokRatioConcurrentSafe(t *testing.T) {
	st := NewState(512, 512)

	const workers = 8
	const iterations = 200

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ratio := 0.45 + float64(id%3)*0.02
				base, ready := st.ObserveTokRatio("concurrent-model", tokfp.BucketDigits, ratio)
				if ready {
					// 锁外读：若 base 是 map 内指针，这里就是竞争点
					_ = base.Deviates(ratio)
					_ = base.Fast
					_ = base.Slow
					_ = base.Samples
				}
			}
		}(w)
	}
	wg.Wait()

	var wg2 sync.WaitGroup
	for r := 0; r < workers; r++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for i := 0; i < iterations; i++ {
				_, _ = st.TokBaseline("concurrent-model", tokfp.BucketDigits)
				_ = st.TokBaselines()
			}
		}()
	}
	wg2.Wait()
}
