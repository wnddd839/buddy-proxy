package httputil

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxJSONBodyBytes is large enough for 1M-context chat payloads (messages + tools).
// Other CodeBuddy proxies typically do not apply an 8MiB JSON cap.
const MaxJSONBodyBytes int64 = 64 << 20

var ErrBodyTooLarge = errors.New("request body exceeds 64MB")

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"error":{"message":"encode failed","type":"internal_error"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func WriteHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// SSEStream 在 http.ResponseWriter 上套 bufio，把多帧 SSE 聚成更少 syscall。
// 默认不在每帧后 Flush：缓冲满时自动落盘；超过 sseFlushMaxDelay 或显式 Flush / WriteDone / WriteComment 时再刷出。
type SSEStream struct {
	buf       *bufio.Writer
	flusher   http.Flusher
	lastFlush time.Time
}

const sseFlushMaxDelay = 5 * time.Millisecond

// NewSSEStream 创建带缓冲的 SSE 写出器。size<=0 时默认 16KiB。
func NewSSEStream(w http.ResponseWriter, size int) (*SSEStream, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	if size <= 0 {
		size = 16 << 10
	}
	return &SSEStream{buf: bufio.NewWriterSize(w, size), flusher: flusher, lastFlush: time.Now()}, true
}

func (s *SSEStream) writeDataFrame(raw []byte) error {
	if _, err := s.buf.WriteString("data: "); err != nil {
		return err
	}
	if _, err := s.buf.Write(raw); err != nil {
		return err
	}
	_, err := s.buf.WriteString("\n\n")
	return err
}

func (s *SSEStream) flush() error {
	if s.buf.Buffered() == 0 {
		return nil
	}
	if err := s.buf.Flush(); err != nil {
		return err
	}
	s.flusher.Flush()
	s.lastFlush = time.Now()
	return nil
}

func (s *SSEStream) maybeFlush(force bool) error {
	if force {
		return s.flush()
	}
	if s.buf.Buffered() == 0 {
		return nil
	}
	if s.buf.Available() < 512 {
		return s.flush()
	}
	if time.Since(s.lastFlush) >= sseFlushMaxDelay {
		return s.flush()
	}
	return nil
}

// Flush 把缓冲区内数据推到客户端（流式收尾、keep-alive 等场景调用）。
func (s *SSEStream) Flush() error {
	return s.flush()
}

func (s *SSEStream) WriteEvent(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := s.writeDataFrame(raw); err != nil {
		return err
	}
	return s.maybeFlush(false)
}

func (s *SSEStream) WriteDone() error {
	if _, err := s.buf.WriteString("data: [DONE]\n\n"); err != nil {
		return err
	}
	return s.flush()
}

func (s *SSEStream) WriteComment(comment string) error {
	if _, err := s.buf.WriteString(": "); err != nil {
		return err
	}
	if _, err := s.buf.WriteString(comment); err != nil {
		return err
	}
	if _, err := s.buf.WriteString("\n\n"); err != nil {
		return err
	}
	// keep-alive 注释必须及时到达客户端。
	return s.maybeFlush(true)
}

// WriteSSE 无缓冲兼容路径（测试/简单 handler）；生产流式请用 SSEStream。
func WriteSSE(w http.ResponseWriter, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data: "); err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n\n"); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func WriteSSEDone(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func WriteSSEComment(w http.ResponseWriter, comment string) {
	_, _ = io.WriteString(w, ": "+comment+"\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func ReadJSON(r *http.Request, dst any) error {
	return readJSONLimited(r, dst, MaxJSONBodyBytes)
}

func readJSONLimited(r *http.Request, dst any, limit int64) error {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return ErrBodyTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(dst)
}

func BearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	if token, ok := strings.CutPrefix(auth, "bearer "); ok {
		return strings.TrimSpace(token)
	}
	return ""
}

func PublicOrigin(r *http.Request, configured string) string {
	if configured != "" {
		return strings.TrimRight(configured, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

func NormalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		return strings.TrimRight(path, "/")
	}
	return path
}
