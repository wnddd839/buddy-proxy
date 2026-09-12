// Package usagejournal keeps request明细与按日汇总，可选落盘到 proxy-usage.json。
package usagejournal

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

const (
	defaultCapacity = 400
	defaultPageSize = 20
	maxPageSize     = 100
)

// Entry is one completed chat completion through the gateway.
type Entry struct {
	At int64 `json:"at"`

	ProxyRequestID string `json:"proxyRequestId"`

	UpstreamConversationID        string `json:"upstreamConversationId,omitempty"`
	UpstreamConversationRequestID string `json:"upstreamConversationRequestId,omitempty"`
	UpstreamMessageID             string `json:"upstreamMessageId,omitempty"`

	SessionKey   string `json:"sessionKey,omitempty"`
	SessionLabel string `json:"sessionLabel,omitempty"`

	Model  string `json:"model"`
	Stream bool   `json:"stream"`

	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	AccountID    string `json:"accountId,omitempty"`
	AccountLabel string `json:"accountLabel,omitempty"`

	PromptTokens     int64    `json:"promptTokens"`
	CompletionTokens int64    `json:"completionTokens"`
	CachedTokens     int64    `json:"cachedTokens"`
	Credit           *float64 `json:"credit,omitempty"`

	DurationMs int64 `json:"durationMs"`
}

// Summary rolls up token / credit totals for a time window.
type Summary struct {
	Range string `json:"range"`
	From  int64  `json:"from"`
	To    int64  `json:"to"`

	Requests         int64   `json:"requests"`
	Failed           int64   `json:"failed"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	TotalTokens      int64   `json:"totalTokens"`
	CachedTokens     int64   `json:"cachedTokens"`
	CacheHitRate     float64 `json:"cacheHitRate"`
	Credits          float64 `json:"credits"`
	CreditRows       int64   `json:"creditRows"`
}

// ChartPoint is one bucket on the usage trend line chart.
type ChartPoint struct {
	At           int64   `json:"at"`
	Label        string  `json:"label"`
	TotalTokens  int64   `json:"totalTokens"`
	CacheHitRate float64 `json:"cacheHitRate"`
}

// View is the admin API payload.
type View struct {
	Summary       Summary      `json:"summary"`
	Series        []ChartPoint `json:"series"`
	Requests      []Entry      `json:"requests"`
	RequestsTotal int          `json:"requestsTotal"`
	Limit         int          `json:"limit"`
	Offset        int          `json:"offset"`
}

type dayBucket struct {
	Requests         int64
	Failed           int64
	PromptTokens     int64
	CompletionTokens int64
	CachedTokens     int64
	Credits          float64
	CreditRows       int64
}

// Journal stores recent requests and per-day aggregates in local timezone.
type Journal struct {
	capacity int
	path     string
	persist  bool

	mu      sync.Mutex
	entries []Entry
	byDay   map[string]dayBucket
	dirty   bool

	persistMu sync.Mutex
	dirReady  bool
	stopCh    chan struct{}
	wakeCh    chan struct{}
	doneCh    chan struct{}
	closeOnce sync.Once
}

// New creates a ring buffer with the given capacity (default 400).
func New(capacity int) *Journal {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &Journal{
		capacity: capacity,
		entries:  make([]Entry, 0, min(capacity, 64)),
		byDay:    map[string]dayBucket{},
	}
}

// Record appends an entry and updates daily totals.
func (j *Journal) Record(e Entry) {
	if j == nil {
		return
	}
	if e.At <= 0 {
		e.At = time.Now().UnixMilli()
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(j.entries) >= j.capacity {
		j.entries = slices.Delete(j.entries, 0, 1)
	}
	j.entries = append(j.entries, e)

	day := time.UnixMilli(e.At).Format("2006-01-02")
	b := j.byDay[day]
	b.Requests++
	if !e.Ok {
		b.Failed++
	}
	b.PromptTokens += e.PromptTokens
	b.CompletionTokens += e.CompletionTokens
	b.CachedTokens += e.CachedTokens
	if e.Credit != nil {
		b.Credits += *e.Credit
		b.CreditRows++
	}
	j.byDay[day] = b
	j.markDirty()
}

// View returns summary for range and a page of requests (newest first).
func (j *Journal) View(rangeName string, limit, offset int) View {
	if j == nil {
		return View{Summary: Summary{Range: rangeName}, Limit: normalizePageSize(limit), Offset: max(0, offset)}
	}
	limit = normalizePageSize(limit)
	if offset < 0 {
		offset = 0
	}
	now := time.Now()
	from, to := windowBounds(rangeName, now)

	j.mu.Lock()
	defer j.mu.Unlock()

	if rangeName == "month" {
		from, to = monthWindowOrWeek(now, j.byDay)
	}

	sum := aggregateBuckets(j.byDay, from, to)
	sum.Range = rangeName
	sum.From = from.UnixMilli()
	sum.To = to.UnixMilli()

	matched := make([]Entry, 0, len(j.entries))
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		at := time.UnixMilli(e.At)
		if at.Before(from) || at.After(to) {
			continue
		}
		matched = append(matched, e)
	}
	total := len(matched)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := matched[offset:end]

	finalizeSummary(&sum)
	series := buildSeries(rangeName, from, to, j.entries, j.byDay)
	return View{
		Summary:       sum,
		Series:        series,
		Requests:      page,
		RequestsTotal: total,
		Limit:         limit,
		Offset:        offset,
	}
}

func normalizePageSize(limit int) int {
	if limit <= 0 {
		return defaultPageSize
	}
	if limit > maxPageSize {
		return maxPageSize
	}
	return limit
}

func finalizeSummary(sum *Summary) {
	if sum == nil {
		return
	}
	if sum.TotalTokens == 0 {
		sum.TotalTokens = sum.PromptTokens + sum.CompletionTokens
	}
	sum.CacheHitRate = cacheHitRate(sum.CachedTokens, sum.PromptTokens)
}

// formatChartDayLabel uses Go layout 1/2 (month/day). Do not use "m-d" — m is minutes.
func formatChartDayLabel(t time.Time) string {
	return t.Format("1/2")
}

func cacheHitRate(cached, prompt int64) float64 {
	if prompt <= 0 {
		return -1
	}
	return float64(cached) / float64(prompt) * 100
}

func buildSeries(rangeName string, from, to time.Time, entries []Entry, byDay map[string]dayBucket) []ChartPoint {
	loc := from.Location()
	switch rangeName {
	case "day":
		return buildHourlySeries(entries, from, to, loc)
	default:
		return buildDailySeriesFromBuckets(byDay, from, to, loc)
	}
}

func buildHourlySeries(entries []Entry, from, to time.Time, loc *time.Location) []ChartPoint {
	buckets := map[int]struct {
		prompt, completion, cached int64
	}{}
	for _, e := range entries {
		at := time.UnixMilli(e.At).In(loc)
		if at.Before(from) || at.After(to) {
			continue
		}
		h := at.Hour()
		b := buckets[h]
		b.prompt += e.PromptTokens
		b.completion += e.CompletionTokens
		b.cached += e.CachedTokens
		buckets[h] = b
	}
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	endHour := to.In(loc).Hour()
	out := make([]ChartPoint, 0, endHour+1)
	for h := 0; h <= endHour; h++ {
		b := buckets[h]
		pt := ChartPoint{
			At:           start.Add(time.Duration(h) * time.Hour).UnixMilli(),
			Label:        fmt.Sprintf("%02d:00", h),
			TotalTokens:  b.prompt + b.completion,
			CacheHitRate: cacheHitRate(b.cached, b.prompt),
		}
		out = append(out, pt)
	}
	return out
}

func buildDailySeriesFromBuckets(byDay map[string]dayBucket, from, to time.Time, loc *time.Location) []ChartPoint {
	cur := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc)
	out := make([]ChartPoint, 0, 32)
	for !cur.After(endDay) {
		key := cur.Format("2006-01-02")
		b := byDay[key]
		out = append(out, ChartPoint{
			At:           cur.UnixMilli(),
			Label:        formatChartDayLabel(cur),
			TotalTokens:  b.PromptTokens + b.CompletionTokens,
			CacheHitRate: cacheHitRate(b.CachedTokens, b.PromptTokens),
		})
		cur = cur.AddDate(0, 0, 1)
	}
	return out
}

// monthWindowOrWeek uses the full 30-day window only when data goes back that far; otherwise 7 days.
func monthWindowOrWeek(now time.Time, byDay map[string]dayBucket) (time.Time, time.Time) {
	loc := now.Location()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	monthFrom := startOfDay.AddDate(0, 0, -29)
	earliest := earliestDataDay(byDay, loc)
	if earliest.IsZero() || earliest.After(monthFrom) {
		return windowBounds("week", now)
	}
	return monthFrom, now
}

func earliestDataDay(byDay map[string]dayBucket, loc *time.Location) time.Time {
	var earliest time.Time
	for key := range byDay {
		t, err := time.ParseInLocation("2006-01-02", key, loc)
		if err != nil {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

func windowBounds(rangeName string, now time.Time) (time.Time, time.Time) {
	loc := now.Location()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	end := now
	switch rangeName {
	case "week":
		from := startOfDay.AddDate(0, 0, -6)
		return from, end
	case "month":
		from := startOfDay.AddDate(0, 0, -29)
		return from, end
	default: // day
		return startOfDay, end
	}
}

func aggregateBuckets(byDay map[string]dayBucket, from, to time.Time) Summary {
	var sum Summary
	loc := from.Location()
	cur := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc)
	for !cur.After(endDay) {
		if b, ok := byDay[cur.Format("2006-01-02")]; ok {
			sum.Requests += b.Requests
			sum.Failed += b.Failed
			sum.PromptTokens += b.PromptTokens
			sum.CompletionTokens += b.CompletionTokens
			sum.CachedTokens += b.CachedTokens
			sum.Credits += b.Credits
			sum.CreditRows += b.CreditRows
		}
		cur = cur.AddDate(0, 0, 1)
	}
	sum.TotalTokens = sum.PromptTokens + sum.CompletionTokens
	finalizeSummary(&sum)
	return sum
}

