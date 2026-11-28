package provider

import "strings"

// 上游只有三个真实思考档位（2026-11-18 交错采样实测，见 ApplyReasoningFields 注释）：
//
//	low  —— minimal / low 都落在这里
//	high —— medium / high / xhigh 都落在这里
//	max  —— max / ultra 都落在这里
const (
	EffortLow  = "low"
	EffortHigh = "high"
	EffortMax  = "max"
)

// effortAliases 是客户端可能送来的写法 → 上游真实档位。
//
// 之所以要把 6 个 + ultra 收敛成 3 个：官方文档给的是 6 个取值，
// 但同一 prompt 的实测 reasoning_tokens 中位把它们压成三堆
// （low 堆 412~459、high 堆 415~591、max 堆 5632~5885），
// 中间那些名字只是别名，透传上去只是让日志更难读、让「哪一档更快」无法回答。
var effortAliases = map[string]string{
	"minimal": EffortLow,
	"low":     EffortLow,
	"medium":  EffortHigh,
	"high":    EffortHigh,
	"xhigh":   EffortHigh,
	"max":     EffortMax,
	// ultra 官方文档记作「映射到 max」。实测 ultra 与 max 无差异
	// （各 10 次交错：5632 vs 5885，配对为正 4/10），故一并收敛到 max。
	"ultra": EffortMax,
}

// effortDisableKeywords 表示「明确要求关掉思考」，原样透传交给上游判断。
var effortDisableKeywords = map[string]bool{
	"none":     true,
	"off":      true,
	"disabled": true,
}

// UnknownEffortFallback 是未知档位值的回落档位。
//
// 为什么回落而不是原样透传：上游对未知档位值是**静默接受**的，但结果是完全
// 不思考。实测 bogus-xyz 连续 3 次全部 reasoning_tokens=0、正文 0 字，
// 而同期 max 是 6172。原样透传等于让一个拼错的档位把思考悄悄关掉。
// 回落 high 与官方「thinking 默认 high」一致，是伤害最小的选择。
const UnknownEffortFallback = EffortHigh

// canonicalEffort 把任意档位写法收敛成上游真实档位。
//
// 返回值：
//   - wire        最终要写进上游 body 的值；空串表示「什么都不写」
//   - substituted 是否因为无法识别而回落（便于日志与用量明细区分
//     「客户端就要 high」和「客户端写错了被我们改成 high」）
func canonicalEffort(raw string) (wire string, substituted bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "", false
	}
	if tier, ok := effortAliases[v]; ok {
		return tier, false
	}
	if effortDisableKeywords[v] {
		return v, false
	}
	return UnknownEffortFallback, true
}

// CanonicalReasoningEffort 是 canonicalEffort 的导出形式，供用量记录与测试使用。
func CanonicalReasoningEffort(raw string) (wire string, substituted bool) {
	return canonicalEffort(raw)
}
