package systemprompt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreStatsForDate 验证按天从落盘全量统计，覆盖各类计数：
// Recovered / Degraded / Blocked / Status=0 / Status=500 等。
func TestStoreStatsForDate(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local).UnixMilli()
	events := []Event{
		// 降级重放成功：fold/200，Recovered=true（与 Stats24h 口径一致）。
		{At: at, Form: "fold", Status: 200, Recovered: true},
		// 被 11128 拦截的首试：keep/400，Degraded=true，ErrorCode=11128。
		{At: at, Form: "keep", Status: 400, ErrorCode: "11128", Degraded: true},
		// 传输层失败：Status=0（IsProblem 判问题，不计 OK）。
		{At: at, Form: "keep", Status: 0},
		// 上游 500：Status>=400，判问题。
		{At: at, Form: "fold", Status: 500},
		// 普通 keep 成功。
		{At: at, Form: "keep", Status: 200},
		// 普通 fold 成功。
		{At: at, Form: "fold", Status: 200},
	}
	for _, e := range events {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.StatsForDate("20261115")
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 6 {
		t.Fatalf("Total=%d want 6", got.Total)
	}
	if got.Keep != 3 || got.Fold != 3 {
		t.Fatalf("Keep=%d Fold=%d want 3/3", got.Keep, got.Fold)
	}
	if got.Degraded != 1 {
		t.Fatalf("Degraded=%d want 1", got.Degraded)
	}
	if got.Recovered != 1 {
		t.Fatalf("Recovered=%d want 1", got.Recovered)
	}
	if got.Blocked != 1 {
		t.Fatalf("Blocked=%d want 1", got.Blocked)
	}
	// IsProblem：400/0/500 计为 Failed，其余 3 条（含 Recovered 那条）计 OK。
	if got.OK != 3 || got.Failed != 3 {
		t.Fatalf("OK=%d Failed=%d want 3/3", got.OK, got.Failed)
	}

	// 与 Journal.Stats24h 口径一致性：同样 6 条事件喂进 Journal 应得同一结果。
	j := NewJournal(50)
	for _, e := range events {
		j.Record(e)
	}
	ref := j.Stats24h(time.UnixMilli(at))
	if got != ref {
		t.Fatalf("StatsForDate 与 Stats24h 口径不一致：disk=%+v journal=%+v", got, ref)
	}
}

// TestStoreStatsForDateMissingDate 不存在的日期应返回零值 Stats 且不报错。
func TestStoreStatsForDateMissingDate(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, err := s.StatsForDate("20200101")
	if err != nil {
		t.Fatalf("不存在的日期不应报错：%v", err)
	}
	if got != (Stats{}) {
		t.Fatalf("不存在的日期应返回零值，实际 %+v", got)
	}

	// nil Store 也应安全返回零值。
	var nilStore *Store
	if got, err := nilStore.StatsForDate("20200101"); err != nil || got != (Stats{}) {
		t.Fatalf("nil Store 应返回零值无错误，实际 %+v err=%v", got, err)
	}
}

// TestStoreStatsForDateSkipsBadLines 与 Read 一致：坏行跳过不影响统计。
func TestStoreStatsForDateSkipsBadLines(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir, 7*24*time.Hour)
	defer s.Close()
	path := filepath.Join(dir, "proxy-systemprompt-20261115.jsonl")
	content := `{"at":1700000000000,"form":"keep","status":200,"recovered":true}
not json at all
{"at":1700000000000,"form":"fold","status":200}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.StatsForDate("20261115")
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Recovered != 1 || got.Keep != 1 || got.Fold != 1 {
		t.Fatalf("坏行应被跳过且统计正确，实际 %+v", got)
	}
}

// TestStoreStatsForDateMemoryOnly 纯内存 store 也能按天全量统计。
func TestStoreStatsForDateMemoryOnly(t *testing.T) {
	s, err := OpenStore("", 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	today := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local).UnixMilli()
	yesterday := time.Date(2026, 11, 14, 10, 0, 0, 0, time.Local).UnixMilli()
	_ = s.Append(Event{At: today, Form: "fold", Status: 200, Recovered: true})
	_ = s.Append(Event{At: today, Form: "keep", Status: 200})
	_ = s.Append(Event{At: yesterday, Form: "keep", Status: 200})

	got, err := s.StatsForDate("20261115")
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Recovered != 1 {
		t.Fatalf("内存 store 应只统计当日 2 条（含 1 条 Recovered），实际 %+v", got)
	}
}
