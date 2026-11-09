package checkin

import "time"

// testJitterKey 是测试专用固定密钥：dueAt 现在依赖服务端私钥，
// 测试必须给一个确定值才能断言具体时刻。
var testJitterKey = []byte("0123456789abcdef0123456789abcdef")

// testDueAt 用固定密钥计算签到时刻，供纯函数式断言使用。
func testDueAt(rule normalizedRule, day string) time.Time {
	r := &Runner{jitterKey: testJitterKey}
	return r.dueAt(rule, day)
}
