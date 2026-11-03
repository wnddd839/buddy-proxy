// Package concurrency 提供进程内的并发观测：按账号记录在途请求数（in-flight）、
// 每分钟请求数（RPM），并在上游返回限流时记录「触发瞬间的在途值」，用于反推
// 单账号的安全并发上限。
//
// 设计目标：
//   - 只观测真实流量，不额外发探针请求，不消耗上游额度；
//   - 全程无阻塞：Begin/End/RecordRateLimit 只做计数与分桶写入；
//   - 分桶统计存内存（重启重来），只有限流事件落盘，文件小、写盘频率低；
//   - 保留滚动窗口（默认 6 小时）的 60 秒分桶，足以回答
//     「刚才那波 429 是在多少并发下打出来的」。
package concurrency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/atomicwrite"
)

const (
	bucketSeconds      = 60
	defaultRetention   = 6 * time.Hour
	maxRateLimitEvents = 500
	flushInterval      = 30 * time.Second
)

// RateLimitEvent 记录一次上游限流触发的现场。
type RateLimitEvent struct {
	At        int64  `json:"at"`
	AccountID string `json:"accountId"`
	Label     string `json:"label,omitempty"`
	Model     string `json:"model,omitempty"`
	Site      string `json:"site,omitempty"`
	// AccountInFlight 是触发瞬间该账号的在途请求数（含本次）。
	AccountInFlight int `json:"accountInFlight"`
	// GlobalInFlight 是所有账号合计在途数。
	GlobalInFlight int `json:"globalInFlight"`
	// ResetAt 是从上游文案解析出的重置时间（毫秒）；0 表示未解析到。
	ResetAt int64 `json:"resetAt,omitempty"`
	// WindowMs = ResetAt - At，用于区分频率限制与日配额。
	WindowMs int64  `json:"windowMs,omitempty"`
	Kind     string `json:"kind,omitempty"` // rate | quota
	Message  string `json:"message,omitempty"`
}

type bucket struct {
	Requests        int64 `json:"requests"`
	Failures        int64 `json:"failures"`
	RateLimited     int64 `json:"rateLimited"`
	MaxInFlight     int   `json:"maxInFlight"`
	SumInFlight     int64 `json:"sumInFlight"`
	InFlightSamples int64 `json:"inFlightSamples"`
	DurationSumMs   int64 `json:"durationSumMs"`
	DurationSamples int64 `json:"durationSamples"`
}

type accountState struct {
	inFlight int
	label    string
	buckets  map[int64]*bucket
}

// Snapshot 是对外暴露的只读快照。
type Snapshot struct {
	GeneratedAt    int64            `json:"generatedAt"`
	GlobalInFlight int              `json:"globalInFlight"`
	Accounts       []AccountView    `json:"accounts"`
	RateLimits     []RateLimitEvent `json:"rateLimits"`
	Totals         Totals           `json:"totals"`
}

// Totals 是 60 分钟窗口内的汇总。
type Totals struct {
	Requests    int64 `json:"requests"`
	Failures    int64 `json:"failures"`
	RateLimited int64 `json:"rateLimited"`
}

// AccountView 是单账号的观测视图。
type AccountView struct {
	AccountID string `json:"accountId"`
	Label     string `json:"label,omitempty"`
	InFlight  int    `json:"inFlight"`

	// 最近 1 / 5 / 60 分钟的请求数。
	RPM1  int64 `json:"rpm1"`
	RPM5  int64 `json:"rpm5"`
	RPM60 int64 `json:"rpm60"`

	Requests60   int64 `json:"requests60"`
	Failures60   int64 `json:"failures60"`
	RateLimits60 int64 `json:"rateLimits60"`

	PeakInFlight int     `json:"peakInFlight"`
	AvgInFlight  float64 `json:"avgInFlight"`
	AvgLatencyMs int64   `json:"avgLatencyMs,omitempty"`

	// RateLimitInFlightMax / Min 是该账号历史限流触发时的在途值区间。
	RateLimitInFlightMax int `json:"rateLimitInFlightMax,omitempty"`
	RateLimitInFlightMin int `json:"rateLimitInFlightMin,omitempty"`
	// SafeConcurrency = RateLimitInFlightMin - 1，即观测到的安全并发上界。
	// 0 表示还没有限流样本，无法推断。
	SafeConcurrency int `json:"safeConcurrency,omitempty"`
}

// Tracker 是并发观测器。零值不可用，请用 New 构造。
type Tracker struct {
	mu       sync.Mutex
	accounts map[string]*accountState
	events   []RateLimitEvent
	global   int

	path      string
	retention time.Duration

	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

// New 创建观测器。path 为空表示只在内存中保留（不落盘）。
func New(path string, retention time.Duration) *Tracker {
	if retention <= 0 {
		retention = defaultRetention
	}
	t := &Tracker{
		accounts:  map[string]*accountState{},
		path:      path,
		retention: retention,
		doneCh:    make(chan struct{}),
	}
	if path != "" {
		t.stopCh = make(chan struct{})
		_ = t.load()
		go t.flushLoop()
	}
	return t
}

// DefaultPath 与账号池同目录，默认 proxy-concurrency.json。
func DefaultPath(accountsPath string) string {
	if accountsPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(accountsPath), "proxy-concurrency.json")
}

// Begin 标记一次上游请求开始。返回的函数必须被调用（defer），否则在途数会泄漏。
func (t *Tracker) Begin(accountID, label string) func(ok bool) {
	if t == nil {
		return func(bool) {}
	}
	started := time.Now()
	t.mu.Lock()
	state := t.accountLocked(accountID)
	state.inFlight++
	if label != "" {
		state.label = label
	}
	cur := state.inFlight
	t.global++
	b := t.bucketLocked(state, started)
	b.Requests++
	b.SumInFlight += int64(cur)
	b.InFlightSamples++
	if cur > b.MaxInFlight {
		b.MaxInFlight = cur
	}
	t.mu.Unlock()

	return func(ok bool) {
		elapsed := time.Since(started).Milliseconds()
		t.mu.Lock()
		state := t.accountLocked(accountID)
		if state.inFlight > 0 {
			state.inFlight--
		}
		if t.global > 0 {
			t.global--
		}
		b := t.bucketLocked(state, started)
		if !ok {
			b.Failures++
		}
		if elapsed >= 0 {
			b.DurationSumMs += elapsed
			b.DurationSamples++
		}
		t.mu.Unlock()
	}
}

// RecordRateLimit 记录一次上游限流，自动带上当前在途数。
func (t *Tracker) RecordRateLimit(accountID, label, model, site, message string, resetAt int64) {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	state := t.accountLocked(accountID)
	if label == "" {
		label = state.label
	}
	ev := RateLimitEvent{
		At:              now.UnixMilli(),
		AccountID:       accountID,
		Label:           label,
		Model:           model,
		Site:            site,
		AccountInFlight: state.inFlight,
		GlobalInFlight:  t.global,
		ResetAt:         resetAt,
		Message:         truncate(message, 300),
	}
	if resetAt > 0 {
		ev.WindowMs = resetAt - ev.At
		if ev.WindowMs > 12*time.Hour.Milliseconds() {
			ev.Kind = "quota"
		} else {
			ev.Kind = "rate"
		}
	}
	b := t.bucketLocked(state, now)
	b.RateLimited++

	t.events = append(t.events, ev)
	if len(t.events) > maxRateLimitEvents {
		t.events = t.events[len(t.events)-maxRateLimitEvents:]
	}
	t.mu.Unlock()
}

// GlobalInFlight 返回当前全局在途请求数。
func (t *Tracker) GlobalInFlight() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.global
}

// AccountInFlight 返回某账号当前在途请求数。
func (t *Tracker) AccountInFlight(accountID string) int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if state, ok := t.accounts[accountID]; ok {
		return state.inFlight
	}
	return 0
}

func (t *Tracker) accountLocked(accountID string) *accountState {
	if accountID == "" {
		accountID = "_unknown"
	}
	state, ok := t.accounts[accountID]
	if !ok {
		state = &accountState{buckets: map[int64]*bucket{}}
		t.accounts[accountID] = state
	}
	if state.buckets == nil {
		state.buckets = map[int64]*bucket{}
	}
	return state
}

func (t *Tracker) bucketLocked(state *accountState, at time.Time) *bucket {
	key := at.Unix() - at.Unix()%bucketSeconds
	b, ok := state.buckets[key]
	if !ok {
		b = &bucket{}
		state.buckets[key] = b
		t.pruneLocked(state, at)
	}
	return b
}

// pruneLocked 丢弃过期桶，避免长期运行内存无界增长。
func (t *Tracker) pruneLocked(state *accountState, now time.Time) {
	if len(state.buckets) <= 8 {
		return
	}
	cutoff := now.Add(-t.retention).Unix()
	for key := range state.buckets {
		if key < cutoff {
			delete(state.buckets, key)
		}
	}
}

// Snapshot 生成当前观测快照。
func (t *Tracker) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{GeneratedAt: time.Now().UnixMilli()}
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	out := Snapshot{GeneratedAt: now.UnixMilli(), GlobalInFlight: t.global}
	ids := make([]string, 0, len(t.accounts))
	for id := range t.accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		state := t.accounts[id]
		if state == nil {
			continue
		}
		view := AccountView{AccountID: id, Label: state.label, InFlight: state.inFlight}
		var peak int
		var sumInFlight, inFlightSamples int64
		var durSum, durSamples int64
		var durBuckets int64

		for key, b := range state.buckets {
			if b == nil {
				continue
			}
			if b.MaxInFlight > peak {
				peak = b.MaxInFlight
			}
			sumInFlight += b.SumInFlight
			inFlightSamples += b.InFlightSamples

			age := now.Unix() - key
			if age < 60 {
				view.RPM1 += b.Requests
			}
			if age < 300 {
				view.RPM5 += b.Requests
			}
			if age < 3600 {
				view.RPM60 += b.Requests
				view.Requests60 += b.Requests
				view.Failures60 += b.Failures
				view.RateLimits60 += b.RateLimited
				out.Totals.Requests += b.Requests
				out.Totals.Failures += b.Failures
				out.Totals.RateLimited += b.RateLimited
				if b.DurationSamples > 0 {
					durSum += b.DurationSumMs
					durSamples += b.DurationSamples
					durBuckets++
				}
			}
		}
		view.PeakInFlight = peak
		if inFlightSamples > 0 {
			view.AvgInFlight = float64(sumInFlight) / float64(inFlightSamples)
		}
		if durSamples > 0 {
			view.AvgLatencyMs = durSum / durSamples
		}
		out.Accounts = append(out.Accounts, view)
	}

	cutoff := now.Add(-t.retention).UnixMilli()
	minInFlight := map[string]int{}
	maxInFlight := map[string]int{}
	for _, ev := range t.events {
		if ev.At < cutoff {
			continue
		}
		out.RateLimits = append(out.RateLimits, ev)
		// inFlight == 0 的 429 是「并发已经散开之后才收到响应」的样本，
		// 它不能说明「多少并发时被打满」，必须排除，否则 safeConcurrency 永远是 0。
		if ev.AccountInFlight <= 0 {
			continue
		}
		if prev, ok := minInFlight[ev.AccountID]; !ok || ev.AccountInFlight < prev {
			minInFlight[ev.AccountID] = ev.AccountInFlight
		}
		if ev.AccountInFlight > maxInFlight[ev.AccountID] {
			maxInFlight[ev.AccountID] = ev.AccountInFlight
		}
	}
	for i := range out.Accounts {
		v := &out.Accounts[i]
		if min, ok := minInFlight[v.AccountID]; ok {
			v.RateLimitInFlightMin = min
			v.RateLimitInFlightMax = maxInFlight[v.AccountID]
			// 触发限流时的在途数已经「打满」，安全上界保守取 min-1。
			if min > 1 {
				v.SafeConcurrency = min - 1
			}
		}
	}
	return out
}

// ---- 持久化：只存限流事件 ----

type diskSnapshot struct {
	Version int              `json:"version"`
	SavedAt int64            `json:"savedAt"`
	Events  []RateLimitEvent `json:"events"`
}

func (t *Tracker) load() error {
	raw, err := os.ReadFile(t.path)
	if err != nil {
		return err
	}
	var snap diskSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	cutoff := time.Now().Add(-t.retention).UnixMilli()
	t.mu.Lock()
	for _, ev := range snap.Events {
		if ev.At >= cutoff {
			t.events = append(t.events, ev)
		}
	}
	t.mu.Unlock()
	return nil
}

func (t *Tracker) flushLoop() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			t.flush()
			close(t.doneCh)
			return
		case <-ticker.C:
			t.flush()
		}
	}
}

func (t *Tracker) flush() {
	if t == nil || t.path == "" {
		return
	}
	t.mu.Lock()
	cutoff := time.Now().Add(-t.retention).UnixMilli()
	events := make([]RateLimitEvent, 0, len(t.events))
	for _, ev := range t.events {
		if ev.At >= cutoff {
			events = append(events, ev)
		}
	}
	snap := diskSnapshot{Version: 1, SavedAt: time.Now().UnixMilli(), Events: events}
	t.mu.Unlock()

	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	_ = atomicwrite.Write(t.path, raw, 0o600)
}

// Close 停止后台落盘并做最后一次 flush。
func (t *Tracker) Close() error {
	if t == nil {
		return nil
	}
	t.stopOnce.Do(func() {
		if t.stopCh != nil {
			close(t.stopCh)
			select {
			case <-t.doneCh:
			case <-time.After(2 * time.Second):
			}
			return
		}
		t.flush()
	})
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
