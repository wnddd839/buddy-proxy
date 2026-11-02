package provider

import (
	"errors"
	"strings"
	"testing"
)

// resetReader 先吐出 `data` 再以 `reset` 失败，模拟上游连接在响应中途被掐断
// （收到响应头后的传输层 RST）。
type resetReader struct {
	data  string
	off   int
	reset error
}

func (r *resetReader) Read(p []byte) (int, error) {
	if r.off < len(r.data) {
		n := copy(p, r.data[r.off:])
		r.off += n
		return n, nil
	}
	return 0, r.reset
}

var errUpstreamReset = errors.New("read tcp 10.0.0.2:52352->43.160.158.125:443: wsarecv: An existing connection was forcibly closed by the remote host.")

func TestReadSSEResetBeforeFirstContentMarksReplayable(t *testing.T) {
	c := &Client{}
	// 纯注释字节（心跳）不得算作内容。
	body := &resetReader{data: ": keep-alive\n\n", reset: errUpstreamReset}
	_, err := c.readSSE(body, ChatOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stream reset before first content") {
		t.Fatalf("pre-content reset must be marked replayable, got %v", err)
	}
	if !errors.Is(err, errUpstreamReset) {
		t.Fatalf("original error must stay wrapped, got %v", err)
	}
}

func TestReadSSEThinkingOnlyThenResetMarksReplayable(t *testing.T) {
	// 生产证据类：思考流了几分钟，随后硬重置，零正文/零工具下发。思考对 agent
	// 客户端属辅助展示（它们只消费正文+工具），重放不可见。
	c := &Client{}
	body := &resetReader{
		data:  "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"long thinking...\"}}]}\n\n",
		reset: errUpstreamReset,
	}
	_, err := c.readSSE(body, ChatOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stream reset before first content") {
		t.Fatalf("thinking-only reset must be marked replayable, got %v", err)
	}
}

func TestReadSSEResetAfterFirstFrameNotMarked(t *testing.T) {
	c := &Client{}
	body := &resetReader{
		data:  "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n",
		reset: errUpstreamReset,
	}
	_, err := c.readSSE(body, ChatOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "stream reset before first content") {
		t.Fatalf("post-content reset must NOT be marked replayable, got %v", err)
	}
	if !errors.Is(err, errUpstreamReset) {
		t.Fatalf("original error must be returned unwrapped, got %v", err)
	}
}
