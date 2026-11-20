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
// 竞品（dsh-connect-workbuddy）实测上游有个隐藏的轻量模型对这些请求
// 计 0 积分，于是把它改写到那个模型上，每个新会话省一次主模型计费。
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
	// 模型集合不同（国内只有 hy4flash-free，国际还有 deepseek-v4.1-flash、
	// hy3）。一个全局 target 只能让其中一个站点受益。
	//
	// 留空 = 该站点不改写（防御：宁可不改，也不要在运行时猜一个模型）。
	Domestic string `json:"domestic,omitempty"`
	Global   string `json:"global,omitempty"`
}

// LiteModel 是上游的隐藏轻量模型：不在 /models 编目里，但可直接调用，
// 且对标题请求计 0 积分（2026-11-20 线上实测：credit=0，能正常产出标题）。
//
// 竞品 dsh-connect-workbuddy 的 rewriteSessionTitleModel 也用它作为默认目标
// （src/upstream.ts:3928），注释写明「reporter measured the upstream's hidden
// wb-lite at 0 credits for exactly these requests」。
const LiteModel = "wb-lite"

// TargetFor 返回该站点的目标模型；空串表示这个站点不改写。
func (r *TitleRoute) TargetFor(site string) string {
	if r == nil || !r.Enabled {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(site), "global") {
		return strings.TrimSpace(r.Global)
	}
	return strings.TrimSpace(r.Domestic)
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
// 标题请求的 content 是单个 text 块（宿主构造），但这里也容忍字符串与数组两种形态。
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
