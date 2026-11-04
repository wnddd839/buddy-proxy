package gateway

import "testing"

// 500 只在已知的瞬时形态（invoke model error）下换号重试；
// 其余 500 视为上游持续性错误，避免无差别换号把请求放大。
func TestRetryable500OnlyInvokeModelError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{`CodeBuddy chat completion failed with 500: invoke model error [region=domestic site=domestic endpoint=https://copilot.tencent.com/v2/chat/completions]`, true},
		{`CodeBuddy chat completion failed with 500: internal error`, false},
		{`CodeBuddy chat completion failed with 502: bad gateway`, false},
		{`CodeBuddy chat completion failed with 401: unauthorized`, false},
	}
	for _, tc := range cases {
		if got := isRetryable500(tc.msg); got != tc.want {
			t.Fatalf("isRetryable500(%q)=%v want %v", tc.msg, got, tc.want)
		}
	}
}

// 回归：500 不再因为裸正则 \b500\b 被无差别重试。
func TestRetryRegexDoesNotMatchPlain500(t *testing.T) {
	if reRetryNextAccount.MatchString(`CodeBuddy chat completion failed with 500: internal error`) {
		t.Fatal("plain 500 must not match reRetryNextAccount")
	}
}
