package invariant

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// C2 回归：带 tools 或 system echo 的请求，三条比例断言都必须跳过。
//
// ## 缺陷
//
// TokRatio = prompt_tokens ÷ TotalChars，其中：
//
//	prompt_tokens 含 tools schema（实测 dsh 的工具定义几千 token）
//	TotalChars    只有入站 messages 的 content
//
// 带 tools 时比例虚高 ⇒ I9/I10/I11 会误报。「上游收了工具定义的 token」
// 是设计内事实，不是模型被换了的证据。
//
// SystemEchoUser=1 时同理：system 被复制进 user，分子多一份。
func TestTokAssertionsSkipToolsAndEcho(t *testing.T) {
	// 构造一个「本该命中 I11」的请求：DeepSeek 家族、数字桶、比例偏 32%
	base := decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true, RequestID: "r-base",
		Model: "deepseek-v4.1-flash", TokBucket: tokfp.BucketDigits,
		TokRatio: 0.6018, TokChars: 6600, TokDigits: 6200,
		SessionKey: "s1",
	}

	// 对照：不带 tools 时应当命中 I11
	got := DefaultRegistry().Run(base, NewState(64, 64))
	found := false
	for _, f := range got {
		if f.Invariant == "I11" {
			found = true
		}
	}
	if !found {
		t.Fatalf("对照组应命中 I11：%+v", got)
	}

	// 带 tools：不命中
	for _, flag := range []string{"tools", "echo"} {
		rec := base
		rec.RequestID = "r-" + flag
		rec.SessionKey = "s-" + flag
		if flag == "tools" {
			rec.HasTools = true
		} else {
			rec.SystemEchoed = true
		}
		for _, f := range DefaultRegistry().Run(rec, NewState(64, 64)) {
			switch f.Invariant {
			case "I9", "I10", "I11":
				t.Fatalf("带 %s 时不该命中 %s：%+v", flag, f.Invariant, f)
			}
		}
	}
}

// 同一会话内带 tools 的请求不得污染会话参照：
// 后续不带 tools 的请求不该因为「上一次是带 tools 的」而判突变。
func TestToolsRequestDoesNotPolluteSessionBaseline(t *testing.T) {
	st := NewState(64, 64)

	// 第一条带 tools、比例虚高
	DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 1, Ok: true,
		SessionKey: "s1", AccountID: "a1", Model: "deepseek-v4.1-flash",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.95, HasTools: true,
	}, st)

	// 第二条正常值：不该被判突变
	got := DefaultRegistry().Run(decision.Record{
		Kind: decision.KindRequest, At: 2, Ok: true,
		SessionKey: "s1", AccountID: "a1", Model: "deepseek-v4.1-flash",
		TokBucket: tokfp.BucketDigits, TokRatio: 0.4552,
	}, st)
	for _, f := range got {
		if f.Invariant == "I10" {
			t.Fatalf("带 tools 的记录不该进会话参照：%+v", f)
		}
	}
}
