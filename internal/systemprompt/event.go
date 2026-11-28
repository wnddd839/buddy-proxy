package systemprompt

// Event 是「一次请求的 system 提示词处理结果」。
// 只允许出现原始 system 的 SHA-256 前 8 位（Fingerprint），绝不存原文。
type Event struct {
	At        int64  `json:"at"`
	RequestID string `json:"requestId,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Site      string `json:"site,omitempty"`
	Model     string `json:"model,omitempty"`

	Mode        string `json:"mode"`                  // auto / keep / fold / strip
	Form        string `json:"form"`                  // keep / fold
	Degraded    bool   `json:"degraded,omitempty"`    // 因 11128 降级过
	Recovered   bool   `json:"recovered,omitempty"`   // 本次是 11128 降级后的重放且成功
	Fingerprint string `json:"fingerprint,omitempty"` // system 的 SHA-256 前 8 位
	SystemLen   int    `json:"systemLen"`

	// ClientHint 是从请求头推断的客户端类型（claude-code / opencode /
	// deepseek-harness / ...），空表示识别不出。仅用于观测，不参与任何决策。
	ClientHint string `json:"clientHint,omitempty"`
	// SessionSource 是会话键来源（header / cachekey / content / none），
	// 用来判断客户端有没有发会话头（影响账号亲和与上游 prompt cache）。
	SessionSource string `json:"sessionSource,omitempty"`
	// Stripped 表示本次请求删过已知指纹；StrippedBytes 是总字节数。
	//
	// StripScope 说明删在哪一侧：
	//   system    = system 被削（客户端看到的人设与模型看到的不一致）
	//   assistant = 只动了历史输出，system 仍 100% 原样
	//   both      = 两侧都删过
	// 分开记的原因：只看 stripped=true 无法回答「我的 system 有没有被改」，
	// 而这是排查「注入是否原样」时最关键的一条。
	Stripped            bool   `json:"stripped,omitempty"`
	StrippedBytes       int    `json:"strippedBytes,omitempty"`
	StripScope          string `json:"stripScope,omitempty"`
	StripSystemBytes    int    `json:"stripSystemBytes,omitempty"`
	StripAssistantBytes int    `json:"stripAssistantBytes,omitempty"`

	Status     int    `json:"status"`
	ErrorCode  string `json:"errorCode,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

// IsProblem 判定这条事件是否值得用户注意。
// 传输层失败（Status==0）也算问题。
func (e Event) IsProblem() bool {
	if e.Status == 0 || e.Status >= 400 {
		return true
	}
	if e.ErrorCode != "" || e.Degraded {
		return true
	}
	return false
}
