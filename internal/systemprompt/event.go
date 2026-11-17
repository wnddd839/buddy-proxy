package systemprompt

// Event 是「一次请求的 system 提示词处理结果」。
// 只允许出现原始 system 的 SHA-256 前 8 位（Fingerprint），绝不存原文。
type Event struct {
	At        int64  `json:"at"`
	RequestID string `json:"requestId,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Site      string `json:"site,omitempty"`
	Model     string `json:"model,omitempty"`

	Mode        string `json:"mode"`                  // auto / keep / fold
	Form        string `json:"form"`                  // keep / fold
	Degraded    bool   `json:"degraded,omitempty"`    // 因 11128 降级过
	Recovered   bool   `json:"recovered,omitempty"`   // 本次是 11128 降级后的重放且成功
	Fingerprint string `json:"fingerprint,omitempty"` // system 的 SHA-256 前 8 位
	SystemLen   int    `json:"systemLen"`

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
