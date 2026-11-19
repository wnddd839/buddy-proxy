package httputil

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// countingBody 记录实际被读走的字节数，用来证明超限请求「一个字节都不读」。
type countingBody struct {
	r io.Reader
	n int64
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func TestReadJSONLimitedRejectsByContentLengthWithoutReading(t *testing.T) {
	body := &countingBody{r: strings.NewReader(`{"a":1}`)}
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.ContentLength = 8 << 20 // 声称 8MB，远超 1KB 上限

	var dst map[string]any
	err := ReadJSONLimited(req, &dst, 1024)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
	if body.n != 0 {
		t.Fatalf("按 Content-Length 拒绝时不应读取，实际读了 %d 字节", body.n)
	}
}

func TestReadJSONLimitedStillCatchesChunkedOversize(t *testing.T) {
	// 分块上传（ContentLength=-1）没有声明长度，只能靠 LimitReader 兜底。
	payload := strings.Repeat(" ", 4096) + `{"a":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	req.ContentLength = -1

	var dst map[string]any
	err := ReadJSONLimited(req, &dst, 1024)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
}

func TestReadJSONLimitedAcceptsBodyExactlyAtLimit(t *testing.T) {
	// 边界不能回归：长度正好等于上限的 body 必须放行（旧语义也是 >limit 才拒）。
	payload := `{"a":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	req.ContentLength = int64(len(payload))

	var dst map[string]any
	if err := ReadJSONLimited(req, &dst, int64(len(payload))); err != nil {
		t.Fatalf("正好等于上限应放行，err = %v", err)
	}
	if dst["a"] == nil {
		t.Fatalf("解码结果为空: %v", dst)
	}
}
