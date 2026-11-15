package gateway

import (
	"errors"
	"net/http"
	"testing"
)

// TestUpstreamErrorCodePrefersParenthesized 锁住最可靠口径：(code N) 优先。
func TestUpstreamErrorCodePrefersParenthesized(t *testing.T) {
	msg := "CodeBuddy chat completion failed with 400: request illegal (code 11128) [region=domestic model=auto]"
	if got := upstreamErrorCode(msg); got != "11128" {
		t.Fatalf("应提取 11128，实际 %q", got)
	}
}

// TestUpstreamErrorCodeBareCodeNotInWhitelist 锁住 #3：
// 上游只回裸码（没有 (code N) 包裹）且不在旧白名单里时，也必须识别出来。
// 线上出现过 11134；旧实现会因白名单漏它而返回空，进而被
// systemPromptErrorStatus 误标成 Status=0（传输失败）。
func TestUpstreamErrorCodeBareCodeNotInWhitelist(t *testing.T) {
	for _, code := range []string{"11134", "11128", "11199", "12345"} {
		msg := "CodeBuddy chat completion failed with 500: upstream invoke error " + code + " [model=auto]"
		if got := upstreamErrorCode(msg); got != code {
			t.Fatalf("裸码 %q 应被识别，实际 %q（msg=%q）", code, got, msg)
		}
	}
}

// TestSystemPromptErrorStatusBareCodeIsNotTransportFailure 锁住 #3 的下游后果：
// 只有裸码、没有 (code N)、也没有 "failed with NNN" 的错误，
// 不能落到 Status=0（传输失败），应给出 502 + 该码。
func TestSystemPromptErrorStatusBareCodeIsNotTransportFailure(t *testing.T) {
	err := errors.New("CodeBuddy chat completion stream error: upstream invoke error 11134 [model=auto]")
	status, code := systemPromptErrorStatus(err)
	if code != "11134" {
		t.Fatalf("ErrorCode 应为 11134，实际 %q", code)
	}
	if status == 0 {
		t.Fatal("有明确上游错误码时不得标成 Status=0（传输失败）")
	}
	if status != http.StatusBadGateway {
		t.Fatalf("应为 502，实际 %d", status)
	}
}

// TestSystemPromptErrorStatusRealTransportFailureStillZero 反向锁：
// 真正的传输层错误（没有任何错误码）仍必须是 Status=0，不能被新正则误伤。
func TestSystemPromptErrorStatusRealTransportFailureStillZero(t *testing.T) {
	for _, msg := range []string{
		"CodeBuddy chat completion transport error: read tcp 127.0.0.1:14080: i/o timeout [region=domestic model=auto]",
		"CodeBuddy chat completion stream stalled before first content: context deadline exceeded",
		"dial tcp: connection reset by peer [model=auto]",
		// 曾经误伤：WARP 本地端口 14080 被 \b1\d{4}\b 当成错误码。
		"read tcp 127.0.0.1:14080: i/o timeout [region=domestic model=auto]",
		"connect to 192.168.1.7:18090: connection refused",
	} {
		status, code := systemPromptErrorStatus(errors.New(msg))
		if status != 0 || code != "" {
			t.Fatalf("纯传输错误应为 (0, \"\")，实际 (%d, %q)：%q", status, code, msg)
		}
	}
}

// TestSystemPromptErrorStatusHTTPStatusStillWins 锁住既有优先级：
// "failed with NNN" 的 HTTP 状态不能被裸码正则覆盖。
func TestSystemPromptErrorStatusHTTPStatusStillWins(t *testing.T) {
	err := errors.New("CodeBuddy chat completion failed with 400: bad request 11128 [model=auto]")
	status, code := systemPromptErrorStatus(err)
	if status != http.StatusBadRequest {
		t.Fatalf("HTTP 状态应为 400，实际 %d", status)
	}
	if code != "11128" {
		t.Fatalf("错误码应为 11128，实际 %q", code)
	}
}

// TestUpstreamBareCodePatternDoesNotMatchOtherNumbers 防误伤：
// 耗时、长度、HTTP 3 位状态、6 位数字都不该被当成上游错误码。
func TestUpstreamBareCodePatternDoesNotMatchOtherNumbers(t *testing.T) {
	for _, msg := range []string{
		"durationMs=35627 model=auto",
		"systemLen=83967 contentLen=36761",
		"failed with 400: request illegal",
		"prompt is too long: 100001 > 100000",
		"requestId=08c3e474eb20c702d44de998e77c069f",
		"read tcp 127.0.0.1:14080: i/o timeout",
		"endpoint=https://www.codebuddy.ai:443/v2/chat/completions",
	} {
		if m := upstreamBareCodePattern.FindStringSubmatch(msg); m != nil {
			t.Fatalf("不应把 %q 当成 5 位错误码，却匹配到 %q", msg, m[1])
		}
	}
}
