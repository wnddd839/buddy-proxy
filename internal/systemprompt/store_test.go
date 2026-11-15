package systemprompt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	for i := 0; i < 3; i++ {
		if err := s.Append(Event{At: at.Add(time.Duration(i) * time.Minute).UnixMilli(), Form: "keep", Status: 200}); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.Dates(); len(got) != 1 || got[0] != "20261115" {
		t.Fatalf("Dates=%v want [20261115]", got)
	}
	events, total, err := s.Read("20261115", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total=%d want 3", total)
	}
	if len(events) != 2 || events[0].At <= events[1].At {
		t.Fatalf("应最新在前且限 2 条：%v", events)
	}
}

func TestStoreFileNameAndSingleLineWrites(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	if err := s.Append(Event{At: at.UnixMilli(), Form: "keep", Status: 200, SystemLen: 42}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "proxy-systemprompt-20261115.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("文件名应为 proxy-systemprompt-20261115.jsonl: %v", err)
	}
	if n := len(raw); n == 0 || raw[n-1] != '\n' {
		t.Fatalf("每行必须以换行结尾，实际 %q", string(raw))
	}
	var e Event
	if err := json.Unmarshal(raw[:len(raw)-1], &e); err != nil {
		t.Fatalf("行必须是完整 JSON：%v", err)
	}
	if e.SystemLen != 42 {
		t.Fatalf("SystemLen 丢失：%d", e.SystemLen)
	}
}

func TestStoreConcurrentAppendNoInterleaving(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local).UnixMilli()
	const workers, perWorker = 8, 40
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				model := fmt.Sprintf("model-%d-%d", w, i)
				_ = s.Append(Event{At: at, Form: "keep", Status: 200, Model: model})
			}
		}(w)
	}
	wg.Wait()

	path := filepath.Join(dir, "proxy-systemprompt-20261115.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, line := range splitLines(raw) {
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("第 %d 行不是完整 JSON（写交错）：%v；内容=%q", lines+1, err, line)
		}
		lines++
	}
	if lines != workers*perWorker {
		t.Fatalf("应写入 %d 行，实际 %d", workers*perWorker, lines)
	}
}

func TestStoreConcurrentAppendAndRead(t *testing.T) {
	// Read 把文件 I/O 与反序列化移出 s.mu 后，必须仍能和持续 Append 安全并发：
	// -race 下无 DATA RACE，不 panic，且不会读到半行 JSON（坏行会被跳过）。
	dir := t.TempDir()
	s, err := OpenStore(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local).UnixMilli()
	const appends = 200
	const reads = 200

	var wg sync.WaitGroup
	// 写协程：每条都是一次完整的 O_APPEND 整行写。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < appends; i++ {
			if err := s.Append(Event{At: at, Form: "keep", Status: 200, Model: fmt.Sprintf("m-%d", i)}); err != nil {
				t.Errorf("Append 失败: %v", err)
				return
			}
		}
	}()
	// 读协程：一边写一边反复全量读。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < reads; i++ {
			events, total, err := s.Read("20261115", 0, 0)
			if err != nil {
				t.Errorf("Read 失败: %v", err)
				return
			}
			if total < len(events) || len(events) > appends {
				t.Errorf("读取结果异常: total=%d len=%d", total, len(events))
				return
			}
			for _, e := range events {
				// 能反序列化出来的必然是完整行；关键字段不该被并发写坏。
				if e.At != at {
					t.Errorf("读到损坏事件: %+v", e)
					return
				}
			}
		}
	}()
	wg.Wait()

	// 全部写完后，最终全量读必须能看到全部整行。
	_, total, err := s.Read("20261115", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != appends {
		t.Fatalf("最终应有 %d 条，实际 %d", appends, total)
	}
}

func TestStoreSkipsBadLines(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, 7*24*time.Hour)
	defer s.Close()
	path := filepath.Join(dir, "proxy-systemprompt-20261115.jsonl")
	content := `{"at":1,"form":"keep","status":200}
not json at all
{"at":2,"form":"fold","status":200}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	events, total, err := s.Read("20261115", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(events) != 2 {
		t.Fatalf("坏行应被跳过：total=%d len=%d", total, len(events))
	}
}

func TestStoreReadMissingDate(t *testing.T) {
	s, _ := OpenStore(t.TempDir(), 7*24*time.Hour)
	defer s.Close()
	events, total, err := s.Read("20200101", 0, 10)
	if err != nil {
		t.Fatalf("不存在的日期不应报错：%v", err)
	}
	if total != 0 || len(events) != 0 {
		t.Fatalf("应为空：total=%d len=%d", total, len(events))
	}
}

func TestStorePrune(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, 7*24*time.Hour)
	defer s.Close()
	old := time.Now().Add(-10 * 24 * time.Hour)
	recent := time.Now()
	if err := s.Append(Event{At: old.UnixMilli(), Status: 200}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Event{At: recent.UnixMilli(), Status: 200}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(recent); err != nil {
		t.Fatal(err)
	}
	if got := s.Dates(); len(got) != 1 || got[0] != recent.Format("20060102") {
		t.Fatalf("应只留今天，实际 %v", got)
	}
}

func TestStoreEmptyDirIsMemoryOnly(t *testing.T) {
	s, err := OpenStore("", 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Append(Event{At: time.Now().UnixMilli(), Status: 200}); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := s.Read(time.Now().Format("20060102"), 0, 10); total != 1 {
		t.Fatalf("内存 store 应能读回 1 条，实际 %d", total)
	}
}

func TestStoreDateFollowsEventTimeNotWallClock(t *testing.T) {
	// 跨零点：用请求开始时刻选文件，而不是当前时间。
	dir := t.TempDir()
	s, _ := OpenStore(dir, 7*24*time.Hour)
	defer s.Close()
	lateNight := time.Date(2026, 11, 15, 23, 59, 59, 0, time.Local)
	if err := s.Append(Event{At: lateNight.UnixMilli(), Status: 200}); err != nil {
		t.Fatal(err)
	}
	if got := s.Dates(); len(got) != 1 || got[0] != "20261115" {
		t.Fatalf("应按事件时间落 20261115，实际 %v", got)
	}
}

func splitLines(raw []byte) []string {
	var out []string
	cur := make([]byte, 0, 256)
	for _, b := range raw {
		if b == '\n' {
			out = append(out, string(cur))
			cur = cur[:0]
			continue
		}
		cur = append(cur, b)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}
