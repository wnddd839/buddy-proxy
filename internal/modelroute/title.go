package modelroute

import "strings"

// SessionTitlePrefix 是 dsh 宿主生成会话标题时的固定提示词开头。
//
// 实测存在于两个版本（0.1.x 与 0.2.2-rc）：
//
//	/opt/dsh-0222/node_modules/@deepseek-ai/dsh-session-title-llm/lib/index.js
//	/data/.../dsh-session-title-all-prompts-llm/lib/index.js
//
// 宿主每新建一个会话就会发一次这样的请求，走的是当前选中的付费模型。
// 若配置了目标，这些请求会被改写到更便宜的模型上。
const SessionTitlePrefix = "Generate the session title from this JSON array of human messages:"

// TitleRoute 是「会话标题请求改写」的配置。
//
// 放在模型路由里：它改的也是模型名，与 Rules 同一类行为，
// 只是触发条件是请求体形状而不是站点+模型名。
type TitleRoute struct {
	// Enabled 打开后才会改写；默认关。
	Enabled bool `json:"enabled"`
	// Domestic / Global 是两个站点的目标模型。
	//
	// 为什么要按站点分开：标题请求本来就来自两个站点，而两站的免费
	// 模型集合不同。一个全局 target 只能让其中一个站点受益。
	//
	// 留空 = 该站点不改写。
	Domestic string `json:"domestic,omitempty"`
	Global   string `json:"global,omitempty"`
}

// TargetFor 返回该站点的目标模型。
//
// **未配置目标时返回空字符串 = 不改写**。
//
// 这个默认值是有意选的（2026-11-22，用户明确要求「默认目标就跟随实际
// 请求的模型」）：标题请求走当前会话的主模型，不额外改写。
// 之前这里回落到 `wb-lite`（上游隐藏轻量模型），但实测它其实只是
// `deepseek-v4.1-flash` 的 0 价别名（分词器 delta 恒为 4，完全同一套词表），
// 而且标题请求本身常常不计费 —— 改写的收益远小于「多一层特殊逻辑」的成本，
// 所以删掉了。
//
// 想省钱就在面板上显式配 `hy4flash-free`（实测 0 价、在编目里）。
func (r *TitleRoute) TargetFor(site string) string {
	if r == nil || !r.Enabled {
		return ""
	}
	configured := r.Domestic
	if strings.EqualFold(strings.TrimSpace(site), "global") {
		configured = r.Global
	}
	return strings.TrimSpace(configured)
}

// Normalize 归一站点键（旧配置的 site 字段不再使用，这里只做空白裁剪）。
func (r *TitleRoute) Normalize() {
	if r == nil {
		return
	}
	r.Domestic = strings.TrimSpace(r.Domestic)
	r.Global = strings.TrimSpace(r.Global)
}

// IsSessionTitleRequest 判断这份请求是否是一次「会话标题生成」。
//
// fail-closed：条件全部满足才认为是标题请求，任何一条不满足就返回 false。
//
//  1. 只有一条 user 消息
//  2. 没有 assistant / tool 消息
//  3. 该消息文本以宿主的标题模板开头
//
// 真实的对话几乎不可能同时满足这三条（宿主自己也会校验模板），
// 而一旦宿主改了文案，改写会自动停止 —— 失败方向是安全的
// （标题只是照旧按主模型计费），不会误伤正常对话。
func IsSessionTitleRequest(messages []map[string]any) bool {
	if len(messages) == 0 {
		return false
	}
	var userText string
	userCount := 0
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(asString(message["role"])))
		switch role {
		case "assistant", "tool":
			return false
		case "user":
			userCount++
			if userCount > 1 {
				return false
			}
			userText = contentToText(message["content"])
		}
	}
	if userCount != 1 {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(userText, " \t\r\n"), SessionTitlePrefix)
}

// contentToText 把 content 拍平成纯文本。
func contentToText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			pm, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := pm["text"].(string); ok {
				b.WriteString(text)
			}
		}
		return b.String()
	}
	return ""
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
