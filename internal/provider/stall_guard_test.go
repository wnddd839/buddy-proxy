package provider

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// blockingReader 是一个「不主动返回」的 reader，用来观察看门狗定时器。
// 每次 Read 都阻塞在 ch 上，直到测试放行。
type blockingReader struct {
	ch     chan struct{}
	closed chan struct{}
}

func newBlockingReader() *blockingReader {
	return &blockingReader{ch: make(chan struct{}), closed: make(chan struct{})}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	select {
	case <-r.ch:
		return 0, io.EOF
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
}

func (r *blockingReader) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}

// feedByte 让 reader 吐出一个字节（模拟收到任意 SSE 增量，含 thinking/心跳）。
func (r *blockingReader) feedByte() {
	select {
	case <-r.ch:
	default:
		close(r.ch)
	}
}

// TestStallGuardWidensAfterFirstByte 锁住用户要求：
// 首字节前用窄阈值（StallTimeout），一旦读到任意字节就切到宽阈值
// （StallTimeoutAfterFirstByte），让长静默/长思考不被误杀。
// 用毫秒级阈值驱动，避免真等数分钟。
func TestStallGuardWidensAfterFirstByte(t *testing.T) {
	r := newBlockingReader()
	g := newStallGuard(r, 40*time.Millisecond, 500*time.Millisecond)
	if g == nil {
		t.Fatal("看门狗不应为 nil")
	}
	defer g.timer.Stop()

	if got := g.timeout; got != 40*time.Millisecond {
		t.Fatalf("首字节前阈值应为 40ms，实际 %s", got)
	}
	if got := g.afterFirstByte; got != 500*time.Millisecond {
		t.Fatalf("首字节后阈值应为 500ms，实际 %s", got)
	}

	// 模拟 Read 读到第一个字节后自动放宽看门狗。
	mr := struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(strings.NewReader("x"), r),
		Closer: r,
	}
	g = newStallGuard(mr, 40*time.Millisecond, 500*time.Millisecond)
	defer g.timer.Stop()
	buf := make([]byte, 1)
	if n, err := g.Read(buf); err != nil || n != 1 {
		t.Fatalf("首字节读取失败: n=%d err=%v", n, err)
	}
	if g.fired.Load() {
		t.Fatal("刚收到首字节不应触发")
	}
	if got := g.timeout; got != 500*time.Millisecond {
		t.Fatalf("首字节后应切到宽阈值，实际 %s", got)
	}

	// 宽阈值下，超过窄阈值（40ms）但远小于宽阈值（500ms）的时间不该触发。
	time.Sleep(90 * time.Millisecond)
	if g.fired.Load() {
		t.Fatal("首字节后仅过 90ms（<500ms）不应被判停滞")
	}
}

// TestStallGuardStillFiresBeforeFirstByte 反向锁：首字节前仍用窄阈值，
// 超时必须触发并关闭 body，防止「上游假活」无限挂起。
func TestStallGuardStillFiresBeforeFirstByte(t *testing.T) {
	r := newBlockingReader()
	g := newStallGuard(r, 40*time.Millisecond, 500*time.Millisecond)
	if g == nil {
		t.Fatal("看门狗不应为 nil")
	}
	defer g.timer.Stop()

	time.Sleep(90 * time.Millisecond)
	if !g.fired.Load() {
		t.Fatal("首字节前超过窄阈值应触发看门狗")
	}
	// 关闭后阻塞中的 Read 应立即返回。
	if _, err := g.Read(make([]byte, 1)); err == nil {
		t.Fatal("看门狗触发后 Read 应立即返回错误")
	}
}

// TestStallGuardZeroDisables 锁住 &Client{} 零值行为：0 阈值关闭看门狗。
func TestStallGuardZeroDisables(t *testing.T) {
	r := newBlockingReader()
	if g := newStallGuard(r, 0, 0); g != nil {
		t.Fatal("零阈值应关闭看门狗")
	}
}

// TestNewClientUsesConfiguredStallTimeouts 锁住 NewClient 从 config 取两个阈值，
// 而不是写死 3 分钟常量。
func TestNewClientUsesConfiguredStallTimeouts(t *testing.T) {
	c := NewClient(config.Config{
		StallTimeout:               7 * time.Minute,
		StallTimeoutAfterFirstByte: 25 * time.Minute,
	})
	if c.StallTimeout != 7*time.Minute {
		t.Fatalf("StallTimeout=%s want 7m", c.StallTimeout)
	}
	if c.StallTimeoutAfterFirstByte != 25*time.Minute {
		t.Fatalf("StallTimeoutAfterFirstByte=%s want 25m", c.StallTimeoutAfterFirstByte)
	}
	if c.HTTP.Timeout != 0 {
		t.Fatalf("http.Client.Timeout 必须保持 0（不得引入总超时），实际 %s", c.HTTP.Timeout)
	}
}

// TestStallGuardReadResetsTimer 锁住「连续无字节」语义：
// 每次读到字节都把定时器往后推（不是总时长上限）。
func TestStallGuardReadResetsTimer(t *testing.T) {
	// 用一段可反复读取的字节流：读一次推一次。
	r := strings.NewReader("data: x\n\n")
	g := newStallGuard(io.NopCloser(r), 40*time.Millisecond, 500*time.Millisecond)
	if g == nil {
		t.Fatal("看门狗不应为 nil")
	}
	defer g.timer.Stop()
	buf := make([]byte, 4)
	for i := 0; i < 3; i++ {
		if _, err := g.Read(buf); err != nil {
			break
		}
	}
	// 只要还在读到字节，就绝不能触发。
	if g.fired.Load() {
		t.Fatal("持续读到字节时不应触发停滞判定")
	}
}
