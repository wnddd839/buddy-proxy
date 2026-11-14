package provider_test

import (
	"encoding/json"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// EnsureUpstreamMessages 绝不能改动调用方传入的切片与 map：
// 网关在一次请求里会对同一份 opts.Messages 反复调用（换号 / 跨区 / 重放），
// 一旦发生别名污染，第二次下发的 payload 就会与第一次不同。
func TestEnsureUpstreamMessagesDoesNotMutateInput(t *testing.T) {
	input := []map[string]any{
		{"role": "system", "content": "SENTINEL-SYSTEM"},
		{"role": "user", "content": "SENTINEL-USER"},
		{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "c1"}}},
		{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "c2"}}},
	}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	_ = provider.EnsureUpstreamMessages(input)
	after, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("输入被改写:\n before=%s\n after =%s", before, after)
	}
}

// 连续两次调用必须产出完全一致的 payload（幂等 + 无残留状态）。
func TestEnsureUpstreamMessagesIsStableAcrossCalls(t *testing.T) {
	input := []map[string]any{
		{"role": "system", "content": "S"},
		{"role": "user", "content": "U"},
	}
	first, _ := json.Marshal(provider.EnsureUpstreamMessages(input))
	second, _ := json.Marshal(provider.EnsureUpstreamMessages(input))
	if string(first) != string(second) {
		t.Fatalf("两次调用结果不同:\n 1=%s\n 2=%s", first, second)
	}
}
