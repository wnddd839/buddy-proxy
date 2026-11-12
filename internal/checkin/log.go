package checkin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/atomicwrite"
)

// Retention 是签到结果的保留窗口。前端「48 小时 N/2」的 N 就从这里来，
// 所以窗口长度与 UI 语义必须一致，不要单独改。
const Retention = 48 * time.Hour

// maxEntries 是磁盘上最多保留的条数，防止长期运行把文件撑大。
// 48h 窗口通常先命中；这个上限只是兜底。
const maxEntries = 200

// Entry 是一次签到尝试的结果。一个账号一天可能有多条（失败后重试），
// 前端取当天最后一条作为「今日状态」。
// ExecutedAt 返回真实执行时刻；旧日志没有 RanAt 时退回 At。
func (e Entry) ExecutedAt() int64 {
	if e.RanAt > 0 {
		return e.RanAt
	}
	return e.At
}

type Entry struct {
	AccountID string `json:"accountId"`
	Day       string `json:"day"`
	// At 是「计划时刻」（排班表上的那一秒）。重试没有计划时刻，At=RanAt。
	At int64 `json:"at"`
	// RanAt 是真实执行时刻。与 At 的差值就是「迟到多久」，
	// 也是判断「多个账号是否被同一轮批量打出去」的唯一依据。
	// 旧日志没有这个字段，读出来是 0，按 At 处理。
	RanAt int64 `json:"ranAt,omitzero"`
	OK    bool  `json:"ok,omitzero"`
	// Already 表示上游回报「今天已签到」——算成功，但没有新积分。
	Already bool `json:"already,omitzero"`
	// Credits 是本次签到获得的积分（Already 时为 0）。
	Credits int `json:"credits,omitzero"`
	// Message 是失败原因或上游原话，成功时通常为空。
	Message string `json:"message,omitempty"`
	// Egress 记录本次实际使用的出口，便于事后判断是出口问题还是账号问题。
	Egress string `json:"egress,omitempty"`
	// Degraded 表示本次是「出口失败后回落直连」重试成功的。
	Degraded bool `json:"degraded,omitzero"`
}

// logStore 是磁盘结构。Version 恒为 1，为将来迁移留余地。
type logStore struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Log 是签到结果存储。写在独立文件里（默认与账号池同目录的
// proxy-checkin-log.json），不碰 proxy-accounts.json：账号池会被外部编辑
// 并被内存副本回写，混放容易互相覆盖。
type Log struct {
	path string
	mu   sync.Mutex
}

func OpenLog(path string) *Log { return &Log{path: path} }

func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Read 返回全部条目。文件不存在视为空，不算错误。
// 读到的条目会按时间升序排列，方便调用方取「最后一条」。
func (l *Log) Read() ([]Entry, error) {
	if l == nil || l.path == "" {
		return []Entry{}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.readLocked()
}

func (l *Log) readLocked() ([]Entry, error) {
	raw, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	var store logStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, fmt.Errorf("checkin: 解析 %s 失败: %w", l.path, err)
	}
	if store.Entries == nil {
		return []Entry{}, nil
	}
	sort.SliceStable(store.Entries, func(i, j int) bool {
		return store.Entries[i].At < store.Entries[j].At
	})
	return store.Entries, nil
}

// Append 追加一条记录，并顺手做过期清理与条数上限。
//
// 锁必须覆盖「读 → 改 → 写」整段：否则两个 goroutine 并发 append 时
// 后写者会覆盖前者的条目，或者把文件写成半截 JSON。
func (l *Log) Append(e Entry) error {
	if l == nil || l.path == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entries, err := l.readLocked()
	if err != nil {
		// 文件损坏时不要让签到本身失败——从空开始重建，坏文件留档。
		_ = os.Rename(l.path, l.path+".corrupt")
		entries = []Entry{}
	}
	entries = append(entries, e)

	// 过期清理。边界用 <（严格早于 48h 才丢），保证「正好 48h」的条目保留。
	cutoff := time.Now().Add(-Retention).UnixMilli()
	kept := entries[:0]
	for _, item := range entries {
		if item.At < cutoff {
			continue
		}
		kept = append(kept, item)
	}
	entries = kept

	// 条数上限：保留最新的 maxEntries 条。
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}

	store := logStore{Version: 1, Entries: entries}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return atomicwrite.Write(l.path, append(raw, '\n'), 0o600)
}

// Today 返回该账号指定日期（北京时间 YYYY-MM-DD）的最后一条记录。
func (l *Log) Today(accountID, day string) (Entry, bool) {
	entries, err := l.Read()
	if err != nil {
		return Entry{}, false
	}
	var (
		best  Entry
		found bool
	)
	for _, e := range entries {
		if e.AccountID != accountID || e.Day != day {
			continue
		}
		if !found || e.At >= best.At {
			best = e
			found = true
		}
	}
	return best, found
}

// Count48h 统计该账号在最近 48 小时内**成功**签到的次数。
// 前端「48 小时 N/2」的分子就是它；分母由 UI 硬编码为 2。
func (l *Log) Count48h(accountID string, now time.Time) int {
	entries, err := l.Read()
	if err != nil {
		return 0
	}
	cutoff := now.Add(-Retention).UnixMilli()
	n := 0
	for _, e := range entries {
		if e.AccountID == accountID && e.OK && e.At >= cutoff {
			n++
		}
	}
	return n
}

// AttemptsToday 统计该账号在指定日期（北京时间 YYYY-MM-DD）的签到尝试次数。
// 用于判断「今天已经试过几次」，是重试预算的唯一依据。
func (l *Log) AttemptsToday(accountID, day string) int {
	entries, err := l.Read()
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.AccountID == accountID && e.Day == day {
			n++
		}
	}
	return n
}

// SucceededToday 报告该账号在指定日期是否已经签到成功。
// 只要当天有一条 OK 记录就算成功——失败后成功的场景不该再重试。
func (l *Log) SucceededToday(accountID, day string) bool {
	entries, err := l.Read()
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.AccountID == accountID && e.Day == day && e.OK {
			return true
		}
	}
	return false
}

// LastAttemptToday 返回该账号当天最后一次尝试的时间（毫秒）与是否存在。
// 重试间隔用它来算「上次失败距今多久」。
func (l *Log) LastAttemptToday(accountID, day string) (int64, bool) {
	entries, err := l.Read()
	if err != nil {
		return 0, false
	}
	var last int64
	found := false
	for _, e := range entries {
		if e.AccountID != accountID || e.Day != day {
			continue
		}
		if !found || e.At >= last {
			last = e.At
			found = true
		}
	}
	return last, found
}
