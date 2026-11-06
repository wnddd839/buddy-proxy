package gateway

import "testing"

func TestRetryable500OnlyInvokeModelError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"CodeBuddy chat completion failed with 500: invoke model error", true},
		{"CodeBuddy chat completion failed with 500: internal error", false},
		{"CodeBuddy chat completion failed with 502: invoke model error", true},
	}
	for _, tc := range cases {
		if got := isRetryable500(tc.msg); got != tc.want {
			t.Fatalf("isRetryable500(%q)=%v want %v", tc.msg, got, tc.want)
		}
	}
}
