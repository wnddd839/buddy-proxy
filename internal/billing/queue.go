package billing

import "strings"

// 上游排队类业务码。
//
// 这三个码表示「请求被接受，但还在排队」，不是失败：
//
//	6020 排队中  —— 队列在动，稍后自动完成
//	6021 队列满  —— 现在排不进去
//	6022 已达并发上限 —— 当前用户持有的在途请求太多
//
// 竞品（dsh-connect-workbuddy）的实测：这类响应不该换号、也不该重试 ——
// 换号只会换一个同样排队的账号再排一次，重试会叠加队列压力。
// 正确做法是返回 429 + Retry-After，让客户端按标准退避。
const (
	CodeQueueWaiting  = "6020"
	CodeQueueFull     = "6021"
	CodeQueueLimitHit = "6022"
)

// queueCodes 是全部排队类业务码。
var queueCodes = []string{CodeQueueWaiting, CodeQueueFull, CodeQueueLimitHit}

// IsQueueError 报告这条错误是否是「上游排队」而不是失败。
func IsQueueError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, code := range queueCodes {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// QueueCode 返回命中的排队业务码；不是排队类时返回空串。
func QueueCode(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	for _, code := range queueCodes {
		if strings.Contains(msg, code) {
			return code
		}
	}
	return ""
}
