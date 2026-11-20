package provider

import (
	"strings"
)

// EffortProfile 是单个模型声明支持的思考档位。
//
// 来源是上游 /models 的 reasoning 字段，两种形态：
//
//	复数形态：{"supportedEfforts":["low","high","max"],"defaultEffort":"high","canDisableThinking":true}
//	单数形态：{"effort":"high"}                    （11 个模型是这个形态）
//
// 单数形态不能理解成「只支持这一个档位」：实测这些模型对整条阶梯都返回
// 各不相同的 reasoning_content，且**不发 reasoning_effort 时零思考**。
// 所以 effectiveEfforts 会把单数折叠成完整阶梯，defaultEffort 取声明的那个。
type EffortProfile struct {
	// Supported 是该模型接受的档位（已归一化、已去重、按官方顺序）。
	Supported []string
	// Default 是客户端没指定时应该用的档位。空串表示上游没声明。
	Default string
	// CanDisable 表示该模型允许把思考关掉。
	CanDisable bool
	// Known 表示编目里确实有这个模型的 reasoning 信息。
	// false 时 canonicalEffortForProfile 回落到全局三档逻辑。
	Known bool
}

// effortLadder 是 DSH / pi-ai 说的完整档位阶梯，按由弱到强排序。
var effortLadder = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// ProfileFromReasoning 从上游 reasoning 对象解析出档位画像。
//
// 单数形态（只有 effort，没有 supportedEfforts）按实测折叠成全阶梯：
// 这是竞品 dsh-connect-workbuddy 在 issue #7 上的结论，与我们的实测一致
// ——这些模型对每一档都返回不同的 reasoning_content。
func ProfileFromReasoning(reasoning map[string]any) EffortProfile {
	if reasoning == nil {
		return EffortProfile{}
	}
	profile := EffortProfile{Known: true}

	// canDisableThinking
	if v, ok := reasoning["canDisableThinking"].(bool); ok {
		profile.CanDisable = v
	}

	// supportedEfforts（复数形态）
	var declared []string
	if raw, ok := reasoning["supportedEfforts"].([]any); ok && len(raw) > 0 {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				declared = append(declared, strings.ToLower(strings.TrimSpace(s)))
			}
		}
	}

	// defaultEffort / effort（单数形态）
	defaultEffort := ""
	if v, ok := reasoning["defaultEffort"].(string); ok {
		defaultEffort = strings.ToLower(strings.TrimSpace(v))
	}
	singular := ""
	if v, ok := reasoning["effort"].(string); ok {
		singular = strings.ToLower(strings.TrimSpace(v))
	}

	if len(declared) == 0 {
		// 单数形态：折叠成全阶梯；完全没声明的按 unknown 处理。
		if singular == "" {
			return EffortProfile{Known: false}
		}
		profile.Supported = append([]string{}, effortLadder...)
		if defaultEffort == "" {
			defaultEffort = singular
		}
	} else {
		// 复数形态：只保留官方阶梯里认识的档位，按阶梯顺序排列。
		set := map[string]bool{}
		for _, d := range declared {
			set[d] = true
		}
		for _, level := range effortLadder {
			if set[level] {
				profile.Supported = append(profile.Supported, level)
			}
		}
		// 声明了阶梯外的值就原样保留（上游可能新增档位）。
		for _, d := range declared {
			if !isLadderLevel(d) {
				profile.Supported = append(profile.Supported, d)
			}
		}
		if len(profile.Supported) == 0 {
			profile.Supported = append([]string{}, declared...)
		}
	}

	profile.Default = defaultEffort
	return profile
}

func isLadderLevel(v string) bool {
	for _, level := range effortLadder {
		if level == v {
			return true
		}
	}
	return false
}

// MiddleEffort 返回该模型的「上中位」档。
//
// 为什么不是最弱档：`low/high` 系列取下中位会得到 `low`，
// 那和「把思考调弱」没有区别。上中位对 `low/high` 得 `high`、
// 对 `low/high/max` 得 `high`，与上游默认值一致。
func (p EffortProfile) MiddleEffort() string {
	if len(p.Supported) == 0 {
		return ""
	}
	return p.Supported[len(p.Supported)/2]
}

// canonicalEffortForProfile 在知道模型能力时收敛档位。
//
// 顺序（与竞品 dsh-connect-workbuddy 的 default-effort.ts 同一套判断）：
//  1. 客户端值本身就是该模型支持的档位 → 原样用
//  2. 是别名且目标档该模型支持      → 用目标档
//  3. 用了该模型的 Default          （上游声明的默认档）
//  4. 用了该模型的上中位档            （绝不用最弱档）
//  5. 完全没有档位信息              → 回落全局 high
//
// 关键保证不变：未知值**绝不**原样透传。上游对不认识的档位是静默接受、
// 但完全不思考（实测 bogus-xyz 连打 3 次 reasoning_tokens=0），
// 透传等于让一个拼错的档位把思考悄悄关掉。
func canonicalEffortForProfile(raw string, profile EffortProfile) (wire string, substituted bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "", false
	}

	// 「关思考」关键字原样穿过：它是语义开关，不是档位。
	// 交给 shouldDropDisableEffort 决定「发还是不发」——
	// 若在这里收敛成 high，用户明确要求关思考却会被打开。
	if effortDisableKeywords[v] {
		return v, false
	}

	if !profile.Known || len(profile.Supported) == 0 {
		return canonicalEffort(raw)
	}

	supported := map[string]bool{}
	for _, s := range profile.Supported {
		supported[s] = true
	}

	// 1) 直接支持
	if supported[v] {
		return v, false
	}
	// 2) 别名映射后支持
	if target, ok := effortAliases[v]; ok && supported[target] {
		return target, false
	}
	// 3) 上游声明的默认档
	if profile.Default != "" && supported[profile.Default] {
		return profile.Default, true
	}
	// 4) 上中位
	if mid := profile.MiddleEffort(); mid != "" {
		return mid, true
	}
	// 5) 兜底
	return UnknownEffortFallback, true
}

// CanonicalReasoningEffortForProfile 供用量记录与测试使用。
func CanonicalReasoningEffortForProfile(raw string, profile EffortProfile) (string, bool) {
	return canonicalEffortForProfile(raw, profile)
}
