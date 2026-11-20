package provider

import "strings"

// offRefusedModelIDs 是实测对 `reasoning_effort: "off"` 返回 400 的模型。
//
// 背景（与竞品 dsh-connect-workbuddy 的 issue #34 同一类缺陷）：
// 上游编目对部分模型声明了 `canDisableThinking: true`，但真实发
// `reasoning_effort: "off"` 会被拒。拒绝文案不点名参数（extError.param 为空），
// 而且档位会随模型选择持久化 —— 所以一次踩中会让该模型**每个后续请求**都失败，
// 用户看到的是「这个模型坏了」而不是「这个选项不合法」。
//
// 处理方式与竞品一致：按**精确模型 ID** 维护拒绝表，命中时**不发**
// reasoning_effort（等价于「客户端没指定档位」）。这是 fail-closed 方向：
// 隐藏一个本来能用的档位只损失一个选项，而放出一个必然失败的档位会让
// 该模型的每个请求都挂。
//
// 注意：目前表里只有我们自己实测确认过的条目。新增条目必须先实测，
// 不能照抄竞品的表 —— 它们混合了国内/国际两站，模型 ID 集合不同。
var offRefusedModelIDs = map[string]bool{
	"deepseek-v4.1-flash":    true,
	"deepseek-v4.1-flash-sg": true,
	"deepseek-v4-pro":        true,
	"primary-model":          true,
}

// offRefused 报告该模型的 `off` 档位是否会被上游拒绝。
func offRefused(model string) bool {
	return offRefusedModelIDs[strings.ToLower(strings.TrimSpace(model))]
}

// shouldDropDisableEffort 决定是否要在发出前把「关思考」的档位整个删掉。
//
// 只对字面量 none/off/disabled 生效，且只对拒绝表里的模型生效。
// 其他档位一律不动：上游对其他所有值都返回 200（实测），
// 而「不发档位」与「发一个被拒的 off」的区别是前者能正常工作。
func shouldDropDisableEffort(model, rawEffort string) bool {
	v := strings.ToLower(strings.TrimSpace(rawEffort))
	if v == "" {
		return false
	}
	if !effortDisableKeywords[v] {
		return false
	}
	return offRefused(model)
}
