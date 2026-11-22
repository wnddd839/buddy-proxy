package invariant

import (
	"math"
	"strings"
)

// tokBaseline 是某个 (模型, 分桶) 的分词器比例基线。
//
// ## 为什么用快/慢双基线而不是单一均值
//
// 同一模型的分词器比例稳定（实测同模型重复测完全一致），但会因样本
// 成分的微小波动而浮动。单一均值 + 固定阈值会被正常波动反复触发；
// 双基线把「正常区间」显式建模出来：
//
//	Fast ← 观测到的最小比例（指数平滑下趋）
//	Slow ← 观测到的最大比例（指数平滑上趋）
//
// 新观测落在 [Fast, Slow] 内 → 正常。
// 超出区间超过容忍度 → 分词器变了。
//
// 这既能容纳缓慢漂移，又能抓住突变 —— 后者正是「模型被悄悄换掉」的形态。
type tokBaseline struct {
	Fast    float64
	Slow    float64
	Samples int
	// LastRatio 是最近一次观测，供面板显示偏离方向。
	LastRatio float64
}

// 基线平滑系数。取 0.25 意味着新样本占 1/4 权重 ——
// 足够跟上真实漂移，又不会被单次异常值拽走。
const tokAlpha = 0.25

// 判定可用的最少样本数。
//
// 取 3：前两次用来建立区间，第三次起才可能判「超出」。
// 样本太少时任何阈值都会误报。
const tokMinSamples = 3

// 容忍度：超出 [Fast, Slow] 这个相对量才算异常。
//
// 实测跨模型差异量级（2026-11-22）：
//
//	数字密集：glm 0.602 vs deepseek 0.455 → 差 32%
//	中文：    glm 0.669 vs deepseek 0.637 → 差 5%
//	代码：    glm 0.322 vs deepseek 0.307 → 差 5%
//
// 取 8%：能抓住 32% 那一档（数字密集样本），不会误伤 5% 的正常跨模型差异
// —— 后者本来就在同一个 [Fast, Slow] 区间里，因为基线是按模型分开存的。
// 8% 的真正用途是「同一模型的分词器变了」，而不是「换了个模型」。
const tokTolerance = 0.08

// tokKey 拼出基线的键：模型 + 分桶。
//
// 分桶必须进键：不同桶的比例差异是 2~3 倍（ASCII 0.22 vs 中文 0.64），
// 混在一起算基线毫无意义。
func tokKey(model, bucket string) string {
	model = strings.TrimSpace(model)
	if model == "" || bucket == "" {
		return ""
	}
	return model + "|" + bucket
}

// ObserveTokRatio 记录一次分词器比例观测，返回是否触发异常。
//
// 返回 (baseline, ok)：ok=false 表示样本不足，不做判定。
func (s *State) ObserveTokRatio(model, bucket string, ratio float64) (*tokBaseline, bool) {
	if s == nil || ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return nil, false
	}
	key := tokKey(model, bucket)
	if key == "" {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tokBaseline == nil {
		s.tokBaseline = make(map[string]*tokBaseline)
	}
	b, exists := s.tokBaseline[key]
	if !exists {
		if s.tokCap > 0 {
			for len(s.tokRing) >= s.tokCap {
				oldest := s.tokRing[0]
				s.tokRing = s.tokRing[1:]
				delete(s.tokBaseline, oldest)
			}
		}
		s.tokRing = append(s.tokRing, key)
		b = &tokBaseline{Fast: ratio, Slow: ratio}
		s.tokBaseline[key] = b
	}

	b.Samples++
	b.LastRatio = ratio
	// 快线只往下走，慢线只往上走 —— 但用平滑而不是硬覆盖，
	// 这样一次异常极值不会把区间永久撑开。
	if ratio < b.Fast {
		b.Fast = b.Fast + tokAlpha*(ratio-b.Fast)
	}
	if ratio > b.Slow {
		b.Slow = b.Slow + tokAlpha*(ratio-b.Slow)
	}

	// 样本不足不做判定：前两次只用来建立区间。
	if b.Samples < tokMinSamples {
		return b, false
	}
	return b, true
}

// Deviates 报告该比例是否偏离基线超过容忍度。
//
// 只判「超出区间」，不判「在区间内的偏移」——后者可能只是样本成分变化。
func (b *tokBaseline) Deviates(ratio float64) bool {
	if b == nil || b.Samples < tokMinSamples {
		return false
	}
	if ratio < b.Fast {
		return (b.Fast-ratio)/b.Fast > tokTolerance
	}
	if ratio > b.Slow {
		return (ratio-b.Slow)/b.Slow > tokTolerance
	}
	return false
}

// TokBaseline 返回某个 (模型, 分桶) 的基线副本（面板展示用）。
func (s *State) TokBaseline(model, bucket string) (tokBaseline, bool) {
	if s == nil {
		return tokBaseline{}, false
	}
	key := tokKey(model, bucket)
	if key == "" {
		return tokBaseline{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.tokBaseline[key]
	if !ok || b == nil {
		return tokBaseline{}, false
	}
	return *b, true
}

// TokBaselines 返回全部基线快照（面板列表用）。
func (s *State) TokBaselines() map[string]tokBaseline {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]tokBaseline, len(s.tokBaseline))
	for k, v := range s.tokBaseline {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}
