package watch

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestLogOnlyMonitorReportsAllTransitions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Window, cfg.MinSample = true, 5, 2
	var log bytes.Buffer
	m := New(cfg, slog.New(slog.NewTextHandler(&log, nil)), nil)
	now := time.Now()
	m.now = func() time.Time { return now }
	addSamples(m, 5, Sample{Ok: false})
	m.Evaluate(t.Context())
	now = now.Add(cfg.Reminder)
	m.Evaluate(t.Context())
	addSamples(m, 5, Sample{Ok: true})
	m.Evaluate(t.Context())
	for _, want := range []string{"检测到异常", "异常仍在持续", "已恢复正常"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log-only monitor omitted %q: %s", want, log.String())
		}
	}
}

func TestReasoningNeedsQualifyingBaseline(t *testing.T) {
	for _, prior := range []int{0, 30} {
		m, notifier, _ := newTestMonitor(t)
		addSamples(m, prior, Sample{Ok: true, DurationMs: 900})
		addSamples(m, 10, Sample{Ok: true, ReasoningAsked: true, DurationMs: 900})
		st := m.Evaluate(t.Context())
		if !st.OK || len(notifier.get()) != 0 || st.Metrics.BaselineReasoningAsked != 0 {
			t.Fatalf("missing reasoning history counted as a healthy baseline: %+v", st)
		}
	}
}

func TestSustainedDegradationCannotBecomeItsOwnBaseline(t *testing.T) {
	for _, kind := range []string{"reasoning", "latency"} {
		t.Run(kind, func(t *testing.T) {
			m, notifier, now := newTestMonitor(t)
			healthy := Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 100, DurationMs: 900}
			addSamples(m, 30, healthy)
			m.Evaluate(t.Context())
			bad := healthy
			if kind == "reasoning" {
				bad.ReasoningTokens = 0
			} else {
				bad.DurationMs = 5000
			}
			for range 12 {
				addSamples(m, 10, bad)
				if st := m.Evaluate(t.Context()); st.OK {
					t.Fatalf("sustained %s regression self-recovered: %+v", kind, st)
				}
			}
			*now = now.Add(m.cfg.Reminder)
			m.Evaluate(t.Context())
			events := notifier.get()
			if len(events) != 2 || events[0].Kind != "alert" || events[1].Kind != "remind" {
				t.Fatalf("unexpected ongoing incident events: %+v", events)
			}
			addSamples(m, 10, healthy)
			if st := m.Evaluate(t.Context()); !st.OK {
				t.Fatalf("genuine recovery not detected: %+v", st)
			}
			if events = notifier.get(); len(events) != 3 || events[2].Kind != "recover" {
				t.Fatalf("unexpected recovery events: %+v", events)
			}
		})
	}
}

func TestMissingReasoningObservationsDoNotCloseIncident(t *testing.T) {
	m, notifier, _ := newTestMonitor(t)
	healthy := Sample{Ok: true, ReasoningAsked: true, ReasoningTokens: 100, DurationMs: 900}
	addSamples(m, 30, healthy)
	m.Evaluate(t.Context())
	addSamples(m, 10, Sample{Ok: true, ReasoningAsked: true, DurationMs: 900})
	m.Evaluate(t.Context())
	addSamples(m, 60, Sample{Ok: true, DurationMs: 900})
	if st := m.Evaluate(t.Context()); st.OK || st.Metrics.ReasoningAsked != 0 {
		t.Fatalf("absence of reasoning requests counted as recovery: %+v", st)
	}
	if len(notifier.get()) != 1 {
		t.Fatalf("unknown observations must not emit recovery: %+v", notifier.get())
	}
	addSamples(m, 10, healthy)
	if st := m.Evaluate(t.Context()); !st.OK {
		t.Fatalf("qualifying healthy responses should recover: %+v", st)
	}
}

func TestSMTPExchangeHonorsTimeoutAndCancellation(t *testing.T) {
	for _, mode := range []string{"none", "starttls", "implicit"} {
		for _, cancelEarly := range []bool{false, true} {
			t.Run(mode+"/"+map[bool]string{false: "timeout", true: "cancel"}[cancelEarly], func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				accepted := make(chan struct{})
				serverDone := make(chan struct{})
				go func() {
					defer close(serverDone)
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					defer conn.Close()
					close(accepted)
					_, _ = io.Copy(io.Discard, conn)
				}()
				timeout := 80 * time.Millisecond
				if cancelEarly {
					timeout = 10 * time.Second
				}
				n := NewSMTPNotifier(SMTPConfig{
					Addr: listener.Addr().String(), From: "sender@example.invalid",
					To: []string{"recipient@example.invalid"}, Mode: mode, Timeout: timeout,
				})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- n.Notify(ctx, Event{Kind: "alert"}) }()
				select {
				case <-accepted:
				case <-time.After(2 * time.Second):
					t.Fatal("SMTP connection not established")
				}
				if cancelEarly {
					cancel()
				}
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("stalled SMTP exchange unexpectedly succeeded")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("stalled SMTP exchange ignored timeout/cancellation")
				}
				select {
				case <-serverDone:
				case <-time.After(2 * time.Second):
					t.Fatal("SMTP connection was not closed")
				}
			})
		}
	}
}
