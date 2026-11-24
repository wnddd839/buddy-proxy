package gateway

import (
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// primeInboundIO 在网关入口把「入站侧」的消息统计写进共享的 IO 指针。
//
// 为什么必须在这里做：provider 内部的 FillMessagesIO 拿到的是
// `opts.Messages` 经过网关准备之后的副本。若历史缩减发生在网关侧
// （任何在调用 provider 之前重整 messages 的地方），那里看到的
// In == Out，I3 就永远发现不了 —— 结构性失明。
//
// 出站侧仍由 provider 填（那里才知道最终发出去的形态）。
// 本函数只写 In* 三个字段，不碰 Out*。
func primeInboundIO(opts CompleteOptions) {
	if opts.SystemOutcome == nil || opts.SystemOutcome.IO == nil {
		return
	}
	// 只填入站侧（out 传 nil）：出站侧由 provider.Complete 在真正发出请求后
	// 填充，并同时置 OutObserved。两阶段分开是 I3/I4 区分「没发出去」与
	// 「发出去了但丢光了」的依据。
	provider.FillMessagesIO(opts.SystemOutcome.IO, opts.Messages, nil)
}
