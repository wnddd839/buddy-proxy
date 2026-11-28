package gateway

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
)

func tokCompositionForTest() tokfp.Composition {
	return tokfp.Composition{Chars: 6600, Digits: 6600}
}

// 关掉分词器指纹开关后，新请求的 Tok* 必须全为零值。
//
// 否则 journal 里会混着「关掉之前」和「关掉之后仍有值」的记录，
// 面板上的基线也会被污染。
func TestTokFieldsAreZeroWhenDisabled(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	build := func() ChatRecordInput {
		return ChatRecordInput{
			Started: time.Now(), Model: "deepseek-v4.1-flash", Ok: true,
			Tok: &provider.MessagesIO{
				Composition:  tokCompositionForTest(),
				PromptTokens: 3000,
			},
		}
	}

	// 默认开启：应写入 Tok*
	svc.RecordChat(build())
	rows := svc.Journal.View("day", 10, 0).Requests
	if len(rows) != 1 {
		t.Fatalf("应有 1 条，实际 %d", len(rows))
	}
	if rows[0].TokRatio == 0 || rows[0].TokBucket == "" {
		t.Fatalf("开启时应写入 Tok*：%+v", rows[0])
	}

	// 关掉：Tok* 必须全零
	svc.SetTokFingerprintEnabled(false)
	svc.RecordChat(build())
	rows = svc.Journal.View("day", 10, 0).Requests
	if len(rows) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(rows))
	}
	last := usagejournal.Entry(rows[0]) // View 最新在前
	if last.TokRatio != 0 || last.TokBucket != "" || last.TokChars != 0 {
		t.Fatalf("关闭后 Tok* 应全为零：%+v", last)
	}
}

// 开关本身可热切换，且读回一致。
func TestTokFingerprintToggleRoundTrip(t *testing.T) {
	svc := New(config.Config{Site: "domestic"}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	if !svc.TokFingerprintEnabled() {
		t.Fatal("默认应开启")
	}
	svc.SetTokFingerprintEnabled(false)
	if svc.TokFingerprintEnabled() {
		t.Fatal("关闭后应读回 false")
	}
	svc.SetTokFingerprintEnabled(true)
	if !svc.TokFingerprintEnabled() {
		t.Fatal("再开启后应读回 true")
	}
}
