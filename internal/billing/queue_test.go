package billing

import (
	"errors"
	"testing"
)

func TestIsQueueError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"upstream error: 6020", true},
		{"code 6021 queue full", true},
		{"6022 maximum in flight", true},
		{"429 rate limit", false},
		{"6004 volume limit", false},
		{"11128 channel reject", false},
		{"", false},
	}
	for _, tc := range cases {
		var err error
		if tc.msg != "" {
			err = errors.New(tc.msg)
		}
		if got := IsQueueError(err); got != tc.want {
			t.Fatalf("%q = %v，期望 %v", tc.msg, got, tc.want)
		}
	}
}

func TestQueueCode(t *testing.T) {
	if got := QueueCode(errors.New("error 6020 waiting")); got != CodeQueueWaiting {
		t.Fatalf("6020 识别错：%q", got)
	}
	if got := QueueCode(errors.New("error 6021")); got != CodeQueueFull {
		t.Fatalf("6021 识别错：%q", got)
	}
	if got := QueueCode(errors.New("6022")); got != CodeQueueLimitHit {
		t.Fatalf("6022 识别错：%q", got)
	}
	if got := QueueCode(errors.New("429")); got != "" {
		t.Fatalf("非排队错误应返回空串，实际 %q", got)
	}
}

// 关键行为：排队不能被当成模型限流，否则会触发换号。
// 换号只会换一个同样排队的账号再排一次。
func TestQueueIsNotModelRateLimit(t *testing.T) {
	for _, code := range []string{"6020", "6021", "6022"} {
		if IsModelRateLimitError(errors.New("upstream " + code)) {
			t.Fatalf("%s 不该被判定为模型限流", code)
		}
	}
	// 6004 仍是模型限流
	if !IsModelRateLimitError(errors.New("6004 rate-model")) {
		t.Fatal("6004 应仍是模型限流")
	}
}
