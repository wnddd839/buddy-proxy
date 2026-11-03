package provider

import (
	"io"
	"strings"
	"testing"
	"time"
)

// 上游停滞看门狗：已下发正文后卡住 => 显式报错，不能一直挂着。
func TestReadSSEStallAfterDelivery(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() {
		_, _ = pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半句话\"}}]}\n\n"))
		// 之后不再写、也不关闭：模拟上游「假活」。
	}()
	var got strings.Builder
	client := &Client{StallTimeout: 60 * time.Millisecond}
	start := time.Now()
	result, err := client.readSSE(pr, ChatOptions{OnDelta: func(s string) { got.WriteString(s) }})
	if err == nil {
		t.Fatal("expected stall error, got nil")
	}
	if !strings.Contains(err.Error(), "stalled after delivering content") {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("watchdog fired too late: %s", elapsed)
	}
	if got.String() != "半句话" {
		t.Fatalf("content must reach the client once, got %q", got.String())
	}
	if !result.Turn.Delivered {
		t.Fatal("Delivered must be true")
	}
}

// 上游停滞发生在首内容前 => 可重放错误（服务层换号重试，客户端无感）。
func TestReadSSEStallBeforeDelivery(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	client := &Client{StallTimeout: 60 * time.Millisecond}
	_, err := client.readSSE(pr, ChatOptions{})
	if err == nil {
		t.Fatal("expected stall error, got nil")
	}
	if !strings.Contains(err.Error(), "stalled before first content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 看门狗关闭时（StallTimeout<=0）行为必须完全不变——不能把现有测试和
// 非流式路径带崩。
func TestStallGuardDisabledByDefault(t *testing.T) {
	if g := newStallGuard(strings.NewReader("x"), 0); g != nil {
		t.Fatal("stall guard must be disabled when timeout <= 0")
	}
	body := strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	if _, err := (&Client{}).readSSE(body, ChatOptions{OnDelta: func(string) {}}); err != nil {
		t.Fatalf("guard disabled path must not error: %v", err)
	}
}
