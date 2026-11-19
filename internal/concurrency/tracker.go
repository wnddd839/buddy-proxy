// Package concurrency 提供进程内、无落盘的按账号并发观测。
package concurrency

import (
	"sort"
	"sync"
	"time"
)

const bucketSeconds = 1

// bucketRetention 是秒桶的保留期，与 Snapshot 里「只统计最近一小时」一致。
const bucketRetention = time.Hour

// pruneEvery 是惰性清理的最小间隔。
//
// 为什么要写入时清理：此前秒桶只在 Snapshot() 里淘汰，而 Snapshot 只有打开
// 管理台才会被调用 —— 没人看面板时，秒桶按 1 个/秒/账号无限累积。
// 清理本身 O(账号×桶)，5 分钟一次可以忽略。
const pruneEvery = 5 * time.Minute

type bucket struct {
	requests    int64
	failures    int64
	rateLimited int64
	peak        int
}

type state struct {
	inFlight int
	label    string
	buckets  map[int64]*bucket
}

// AccountView 是单账号的观测视图。
type AccountView struct {
	AccountID    string  `json:"accountId"`
	Label        string  `json:"label,omitempty"`
	InFlight     int     `json:"inFlight"`
	RPM1         float64 `json:"rpm1"`
	RPM5         float64 `json:"rpm5"`
	RPM60        float64 `json:"rpm60"`
	PeakInFlight int     `json:"peakInFlight"`
	RateLimits60 int64   `json:"rateLimits60"`
	AvgLatencyMs float64 `json:"avgLatencyMs,omitempty"`
}

// Snapshot 是当前只读快照。事件只保留最近一小时、最多 200 条。
type Snapshot struct {
	GeneratedAt    int64         `json:"generatedAt"`
	GlobalInFlight int           `json:"globalInFlight"`
	Accounts       []AccountView `json:"accounts"`
	RateLimits     []Event       `json:"rateLimits"`
}

// Event 记录一次限流触发瞬间的在途数。
type Event struct {
	At              int64  `json:"at"`
	AccountID       string `json:"accountId"`
	AccountInFlight int    `json:"accountInFlight"`
	GlobalInFlight  int    `json:"globalInFlight"`
	Message         string `json:"message,omitempty"`
}

// Tracker 只做内存计数；零值不可用，请用 New。
type Tracker struct {
	mu        sync.Mutex
	accounts  map[string]*state
	events    []Event
	global    int
	lastPrune time.Time
}

func New() *Tracker { return &Tracker{accounts: map[string]*state{}} }

// pruneLocked 淘汰过期的秒桶，并清掉「既无在途、又无桶」的空账号条目。
// 调用方必须持有 t.mu。
func (t *Tracker) pruneLocked(now time.Time) {
	if now.Sub(t.lastPrune) < pruneEvery {
		return
	}
	t.lastPrune = now
	cutoff := now.Add(-bucketRetention).Unix()
	for id, st := range t.accounts {
		for key := range st.buckets {
			if key < cutoff {
				delete(st.buckets, key)
			}
		}
		if st.inFlight == 0 && len(st.buckets) == 0 {
			delete(t.accounts, id)
		}
	}
}

func (t *Tracker) locked(accountID string) *state {
	if accountID == "" {
		accountID = "_unknown"
	}
	st, ok := t.accounts[accountID]
	if !ok {
		st = &state{buckets: map[int64]*bucket{}}
		t.accounts[accountID] = st
	}
	return st
}

func (t *Tracker) bucketLocked(st *state, at time.Time) *bucket {
	key := at.Unix() - at.Unix()%bucketSeconds
	b, ok := st.buckets[key]
	if !ok {
		b = &bucket{}
		st.buckets[key] = b
	}
	return b
}

// Begin 标记一次上游请求开始；返回的函数恰好调用一次。
func (t *Tracker) Begin(accountID, label string) func(ok bool) {
	if t == nil {
		return func(bool) {}
	}
	started := time.Now()
	t.mu.Lock()
	t.pruneLocked(started)
	st := t.locked(accountID)
	st.inFlight++
	if label != "" {
		st.label = label
	}
	t.global++
	cur := st.inFlight
	b := t.bucketLocked(st, started)
	b.requests++
	if cur > b.peak {
		b.peak = cur
	}
	t.mu.Unlock()

	var once sync.Once
	return func(ok bool) {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			st := t.locked(accountID)
			if st.inFlight > 0 {
				st.inFlight--
			}
			if t.global > 0 {
				t.global--
			}
			b := t.bucketLocked(st, started)
			if !ok {
				b.failures++
			}
		})
	}
}

// RecordRateLimit 在请求结束前调用，才能记录触发瞬间的真实并发。
func (t *Tracker) RecordRateLimit(accountID, message string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
	st := t.locked(accountID)
	t.events = append(t.events, Event{
		At:              now.UnixMilli(),
		AccountID:       accountID,
		AccountInFlight: st.inFlight,
		GlobalInFlight:  t.global,
		Message:         message,
	})
	if len(t.events) > 200 {
		t.events = t.events[len(t.events)-200:]
	}
	t.bucketLocked(st, now).rateLimited++
}

func (t *Tracker) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{GeneratedAt: time.Now().UnixMilli()}
	}
	now := time.Now()
	cutoff := now.Add(-time.Hour).UnixMilli()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastPrune = time.Time{} // 强制走一次完整清理，保证面板看到的就是清理后的口径
	t.pruneLocked(now)
	out := Snapshot{GeneratedAt: now.UnixMilli(), GlobalInFlight: t.global}
	ids := make([]string, 0, len(t.accounts))
	for id := range t.accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		st := t.accounts[id]
		view := AccountView{AccountID: id, Label: st.label, InFlight: st.inFlight}
		for key, b := range st.buckets {
			age := now.Unix() - key
			if b.peak > view.PeakInFlight {
				view.PeakInFlight = b.peak
			}
			if age < 3600 {
				view.RPM60 += float64(b.requests)
				view.RateLimits60 += b.rateLimited
				if age < 300 {
					view.RPM5 += float64(b.requests)
				}
				if age < 60 {
					view.RPM1 += float64(b.requests)
				}
			}
		}
		view.RPM5 /= 5
		view.RPM60 /= 60
		out.Accounts = append(out.Accounts, view)
	}
	for _, ev := range t.events {
		if ev.At >= cutoff {
			out.RateLimits = append(out.RateLimits, ev)
		}
	}
	return out
}

// Summary 只返回数字，供 15 秒轮询的 /status 使用；前端不得把它当数组。
func (t *Tracker) Summary() map[string]any {
	s := t.Snapshot()
	return map[string]any{
		"generatedAt":    s.GeneratedAt,
		"globalInFlight": s.GlobalInFlight,
		"accounts":       len(s.Accounts),
		"rateLimits":     len(s.RateLimits),
	}
}
