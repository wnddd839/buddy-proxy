package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/systemprompt"
)

// TestSystemPromptOverviewStatsUsesDiskNotMemory 锁住桥接的核心目的：
// 进程内 Journal 为空（模拟服务刚重启）但落盘 jsonl 有事件时，
// stats 必须来自落盘全量，scope 标成 date:，而不是返回 0。
func TestSystemPromptOverviewStatsUsesDiskNotMemory(t *testing.T) {
	srv := systemPromptTestServer(t)
	// 模拟重启：内存事件流清空，落盘保留。
	srv.Svc.SystemPromptJournal.Clear()
	now := time.Now()
	for i := 0; i < 7; i++ {
		srv.Svc.RecordSystemPromptEvent(systemprompt.Event{
			At:   now.Add(time.Duration(i) * time.Second).UnixMilli(),
			Form: "keep", Status: 200,
		})
	}
	// RecordSystemPromptEvent 同时写内存与落盘；只清内存，保留落盘。
	srv.Svc.SystemPromptJournal.Clear()

	stats, scope := srv.systemPromptOverviewStats()
	if want := "date:" + now.Format("20060102"); scope != want {
		t.Fatalf("scope=%q want %q", scope, want)
	}
	if stats.Total != 7 {
		t.Fatalf("stats.Total=%d want 7（应来自落盘全量，而非重启后清零的内存）", stats.Total)
	}
}

// TestSystemPromptOverviewStatsFallbackToMemory 锁住降级路径：
// 落盘 store 不可用时退回进程内近 24h，scope 标成 memory，且不 panic。
func TestSystemPromptOverviewStatsFallbackToMemory(t *testing.T) {
	srv := systemPromptTestServer(t)
	srv.Svc.SystemPromptStore = nil
	srv.Svc.SystemPromptJournal.Clear()
	srv.Svc.SystemPromptJournal.Record(systemprompt.Event{
		At: time.Now().UnixMilli(), Form: "keep", Status: 200,
	})

	stats, scope := srv.systemPromptOverviewStats()
	if scope != "memory" {
		t.Fatalf("scope=%q want memory", scope)
	}
	if stats.Total != 1 {
		t.Fatalf("stats.Total=%d want 1", stats.Total)
	}
}

// TestSystemPromptAPIExposesStatsScope 锁住 API 响应确实带 statsScope，
// 前端 #spStatsScope 才有值可显示。
func TestSystemPromptAPIExposesStatsScope(t *testing.T) {
	srv := systemPromptTestServer(t)
	rec := doAdminRequest(t, srv, http.MethodGet, "/direct-admin/api/system-prompt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Stats      systemprompt.Stats `json:"stats"`
		StatsScope string             `json:"statsScope"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.StatsScope == "" {
		t.Fatal("响应缺少 statsScope")
	}
	if payload.StatsScope != "memory" && len(payload.StatsScope) != len("date:20060102") {
		t.Fatalf("statsScope 值不合法：%q", payload.StatsScope)
	}
}
