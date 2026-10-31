package watch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type recordNotifier struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordNotifier) Notify(_ context.Context, ev Event) error {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	return nil
}

func (r *recordNotifier) get() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestMonitor(t *testing.T) (*Monitor, *recordNotifier, *time.Time) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Window = 10
	cfg.Baseline = 20
	cfg.MinSample = 5
	notifier := &recordNotifier{}
	m := New(cfg, quietLogger(), notifier)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	return m, notifier, &now
}

func addSamples(m *Monitor, n int, s Sample) {
	for i := 0; i < n; i++ {
		m.Record(s)
	}
}

func TestHealthySamplesStayOK(t *testing.T) {
	m, notifier, _ := newTestMonitor(t)
	addSamples(m, 20, Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 180, DurationMs: 900})

	st := m.Evaluate(context.Background())
	if !st.OK {
		t.Fatalf("expected OK, got %+v", st)
	}
	if len(notifier.get()) != 0 {
		t.Fatalf("healthy traffic must not alert, got %d events", len(notifier.get()))
	}
}

func TestSmallSampleDoesNotAlert(t *testing.T) {
	m, notifier, _ := newTestMonitor(t)
	addSamples(m, 3, Sample{Ok: false})

	st := m.Evaluate(context.Background())
	if !st.OK {
		t.Fatalf("expected OK below MinSample, got %+v", st)
	}
	if len(notifier.get()) != 0 {
		t.Fatalf("must not alert below MinSample")
	}
}

func TestErrorRateSpikeAlertsOnceThenRecovers(t *testing.T) {
	m, notifier, now := newTestMonitor(t)
	addSamples(m, 20, Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 180, DurationMs: 900})
	m.Evaluate(context.Background())

	addSamples(m, 10, Sample{Ok: false, ReasoningAsked: true, DurationMs: 200})
	st := m.Evaluate(context.Background())
	if st.OK {
		t.Fatalf("expected bad status, got %+v", st)
	}
	if st.Reason == "" {
		t.Fatal("expected a reason")
	}
	events := notifier.get()
	if len(events) != 1 || events[0].Kind != "alert" {
		t.Fatalf("expected exactly one alert, got %+v", events)
	}

	// 持续异常：未到 Reminder 间隔不应再发。
	*now = now.Add(5 * time.Minute)
	addSamples(m, 10, Sample{Ok: false})
	m.Evaluate(context.Background())
	if got := len(notifier.get()); got != 1 {
		t.Fatalf("reminder fired too early, events=%d", got)
	}

	// 到达提醒间隔后再发一次，且标记为 remind。
	*now = now.Add(7 * time.Hour)
	addSamples(m, 10, Sample{Ok: false})
	m.Evaluate(context.Background())
	events = notifier.get()
	if len(events) != 2 || events[1].Kind != "remind" {
		t.Fatalf("expected remind event, got %+v", events)
	}

	// 恢复正常：只发一封 recover。
	addSamples(m, 10, Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 180, DurationMs: 900})
	st = m.Evaluate(context.Background())
	if !st.OK {
		t.Fatalf("expected recovered, got %+v", st)
	}
	events = notifier.get()
	if len(events) != 3 || events[2].Kind != "recover" {
		t.Fatalf("expected recover event, got %+v", events)
	}
}

func TestReasoningDropDetected(t *testing.T) {
	m, notifier, _ := newTestMonitor(t)
	healthy := Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 200, DurationMs: 900}
	addSamples(m, 20, healthy)
	m.Evaluate(context.Background())

	// 思考字段整体消失（仍有响应、仍 200）。
	addSamples(m, 10, Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 0, DurationMs: 900})
	st := m.Evaluate(context.Background())
	if st.OK {
		t.Fatalf("expected reasoning drop to be flagged, got %+v", st)
	}
	if st.Metrics.ReasoningMissRate < 0.9 {
		t.Fatalf("expected miss rate ~1.0, got %+v", st.Metrics)
	}
	events := notifier.get()
	if len(events) != 1 || events[0].Kind != "alert" {
		t.Fatalf("expected one alert, got %+v", events)
	}
}

func TestReasoningIgnoredWhenNotRequested(t *testing.T) {
	m, notifier, _ := newTestMonitor(t)
	// 客户端没要思考，就不该因为没思考而报警。
	addSamples(m, 20, Sample{Ok: true, ReasoningAsked: false, ReasoningTokens: 0, DurationMs: 900})

	st := m.Evaluate(context.Background())
	if !st.OK {
		t.Fatalf("must not flag when reasoning was not requested, got %+v", st)
	}
	if len(notifier.get()) != 0 {
		t.Fatal("unexpected alert")
	}
}

func TestLatencySpikeDetected(t *testing.T) {
	m, notifier, _ := newTestMonitor(t)
	addSamples(m, 20, Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 180, DurationMs: 900})
	m.Evaluate(context.Background())

	addSamples(m, 10, Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 180, DurationMs: 5000})
	st := m.Evaluate(context.Background())
	if st.OK {
		t.Fatalf("expected latency spike to be flagged, got %+v", st)
	}
	if st.Metrics.LatencyP50 != 5000 {
		t.Fatalf("unexpected latency p50: %+v", st.Metrics)
	}
	if len(notifier.get()) != 1 {
		t.Fatalf("expected one alert, got %d", len(notifier.get()))
	}
}

func TestSampleBufferIsBounded(t *testing.T) {
	m, _, _ := newTestMonitor(t)
	addSamples(m, 500, Sample{Ok: true})
	m.mu.Lock()
	n := len(m.samples)
	m.mu.Unlock()
	if limit := m.cfg.Window + m.cfg.Baseline; n != limit {
		t.Fatalf("buffer not trimmed: have %d, want %d", n, limit)
	}
}

func TestWebhookNotifierPostsJSON(t *testing.T) {
	var got Event
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, "secret", 5*time.Second)
	ev := Event{Kind: "alert", Reason: "推理字段缺失", At: 123, Metrics: Metrics{ErrorRate: 0.5}}
	if err := n.Notify(context.Background(), ev); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if got.Kind != "alert" || got.Reason != "推理字段缺失" || got.At != 123 {
		t.Fatalf("unexpected payload: %+v", got)
	}
	if auth != "Bearer secret" {
		t.Fatalf("unexpected auth header: %q", auth)
	}
}

func TestWebhookNotifierEmptyURLIsNoop(t *testing.T) {
	n := NewWebhookNotifier("", "", time.Second)
	if err := n.Notify(context.Background(), Event{Kind: "alert"}); err != nil {
		t.Fatalf("expected noop, got %v", err)
	}
}

func TestDisabledMonitorIgnoresEverything(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	notifier := &recordNotifier{}
	m := New(cfg, quietLogger(), notifier)
	m.Record(Sample{Ok: false})
	st := m.Evaluate(context.Background())
	if st.Enabled || !st.OK {
		t.Fatalf("unexpected status: %+v", st)
	}
	if len(notifier.get()) != 0 {
		t.Fatal("disabled monitor must not alert")
	}
}
