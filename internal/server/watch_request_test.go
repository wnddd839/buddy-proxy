package server

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/gateway"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/watch"
)

func TestCanceledJournalRowsDoNotFeedMonitoring(t *testing.T) {
	srv := testServer(t, true, "", "secret-key")
	cfg := watch.DefaultConfig()
	cfg.Enabled = true
	srv.Svc.Watch = watch.New(cfg, nil, nil)
	opts := gateway.CompleteOptions{Model: "auto", ReasoningEffort: "high"}
	for range 30 {
		srv.recordChatJournal(time.Now(), "canceled", opts, "auto", true, true, "", provider.Usage{}, nil)
	}
	if st := srv.Svc.Watch.Evaluate(t.Context()); st.Metrics.Samples != 0 {
		t.Fatalf("cancellations became completed observations: %+v", st)
	}
	view := srv.Svc.UsageView("day", 100, 0)
	if view.Summary.Requests != 30 {
		t.Fatalf("canceled requests should retain journal semantics: %+v", view.Summary)
	}
	opts.ReasoningEffort = "none"
	srv.recordChatJournal(time.Now(), "completed", opts, "auto", true, true, "", provider.Usage{}, &gateway.CompleteResult{})
	if st := srv.Svc.Watch.Evaluate(t.Context()); st.Metrics.Samples != 1 || st.Metrics.ReasoningAsked != 0 {
		t.Fatalf("disabled reasoning incorrectly monitored: %+v", st)
	}
}
