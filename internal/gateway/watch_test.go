package gateway

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/watch"
)

// 验证真实请求会被喂进波动监测器：Ok / ReasoningAsked / ReasoningTokens / 耗时都要对上。
func TestRecordChatFeedsWatchMonitor(t *testing.T) {
	cfg := watch.DefaultConfig()
	cfg.Enabled = true
	cfg.Window = 5
	cfg.Baseline = 5
	cfg.MinSample = 2
	mon := watch.New(cfg, nil, nil)

	svc := &Service{Watch: mon, Journal: nil}
	usage := provider.Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		CompletionTokensDetails: &provider.CompletionTokensDetails{
			ReasoningTokens: 7,
		},
	}
	svc.RecordChat(ChatRecordInput{
		Started:            time.Now().Add(-1500 * time.Millisecond),
		Model:              "deepseek-v4.1-flash",
		Ok:                 true,
		Usage:              usage,
		ReasoningRequested: true,
	})

	st := mon.Evaluate(t.Context())
	if st.Metrics.Samples != 1 {
		t.Fatalf("expected 1 sample, got %+v", st.Metrics)
	}
	if st.Metrics.ReasoningAsked != 1 || st.Metrics.ReasoningMissed != 0 {
		t.Fatalf("unexpected reasoning metrics: %+v", st.Metrics)
	}
	if st.Metrics.LatencyP50 < 1000 {
		t.Fatalf("expected duration to be recorded, got %d", st.Metrics.LatencyP50)
	}
}

func TestRecordChatWithoutWatchIsNoop(t *testing.T) {
	svc := &Service{}
	svc.RecordChat(ChatRecordInput{Started: time.Now(), Model: "auto"})
	if st := svc.WatchStatus(); st.Enabled {
		t.Fatalf("expected disabled watch, got %+v", st)
	}
}
