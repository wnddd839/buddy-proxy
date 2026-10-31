// Package watch 提供进程内的健康波动监测：从真实请求流里取样，周期性判断
// 「错误率 / 思考（推理）字段缺失率 / 响应耗时」是否相对自身基线异常，并在
// 状态翻转时通过告警通道（webhook）通知一次。
//
// 设计目标：
//   - 只观测真实流量，不额外发探针请求，不消耗上游额度；
//   - 相对基线判断，避免不同模型、不同客户端的绝对差异造成误报；
//   - 只在“进入异常 / 恢复”时各发一次，持续异常按 Reminder 间隔提醒，绝不按次发；
//   - 全程无阻塞：Record 只写环形缓冲，评估在独立 goroutine 里做。
package watch

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"
)

// Sample 是一次已完成请求的观测值。
type Sample struct {
	At              int64  `json:"at"`
	Model           string `json:"model,omitempty"`
	Ok              bool   `json:"ok"`
	ReasoningAsked  bool   `json:"reasoningAsked,omitempty"`
	ReasoningTokens int64  `json:"reasoningTokens,omitempty"`
	DurationMs      int64  `json:"durationMs,omitempty"`
}

// Config 是监测参数。零值不可用，请用 DefaultConfig 再改。
type Config struct {
	Enabled  bool
	Interval time.Duration // 评估周期

	Window    int // 近期窗口样本数
	Baseline  int // 基线窗口样本数（近期窗口之前）
	MinSample int // 少于该样本数不做判定，避免小样本误报

	// 近期错误率超过该值判定异常。
	ErrorRate float64
	// 请求了推理但上游没返回推理 token 的比例，超过该值判定「思考字段消失」。
	ReasoningMissRate float64
	// 近期耗时中位数超过基线的该倍数判定异常。
	LatencyFactor float64
	// 基线耗时低于该值（毫秒）不判耗时，避免本地极快请求被放大。
	MinBaselineMs int64

	Reminder time.Duration // 持续异常时的提醒间隔

	WebhookURL     string
	WebhookToken   string
	WebhookTimeout time.Duration

	// SMTP 邮件通道（可选，与 webhook 可同时启用）。
	SMTP SMTPConfig
}

// DefaultConfig 给出保守的默认值：宁可漏报，不要误报。
func DefaultConfig() Config {
	return Config{
		Interval:          time.Minute,
		Window:            120,
		Baseline:          480,
		MinSample:         20,
		ErrorRate:         0.25,
		ReasoningMissRate: 0.50,
		LatencyFactor:     3.0,
		MinBaselineMs:     300,
		Reminder:          6 * time.Hour,
		WebhookTimeout:    10 * time.Second,
	}
}

// Metrics 是判定时算出的可比数字，会随状态一起暴露。
type Metrics struct {
	Samples int `json:"samples"`

	ErrorRate          float64 `json:"errorRate"`
	ReasoningAsked     int     `json:"reasoningAsked"`
	ReasoningMissed    int     `json:"reasoningMissed"`
	ReasoningMissRate  float64 `json:"reasoningMissRate"`
	BaselineMissRate   float64 `json:"baselineMissRate"`
	LatencyP50         int64   `json:"latencyP50"`
	BaselineLatencyP50 int64   `json:"baselineLatencyP50"`
}

// Status 是对外暴露的监测状态。
type Status struct {
	Enabled     bool     `json:"enabled"`
	OK          bool     `json:"ok"`
	Reason      string   `json:"reason,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
	Since       int64    `json:"since,omitempty"`
	LastAlertAt int64    `json:"lastAlertAt,omitempty"`
	Metrics     Metrics  `json:"metrics"`
}

// Event 是投递给告警通道的事件。
type Event struct {
	Kind    string   `json:"kind"` // alert | recover
	Reason  string   `json:"reason"`
	Reasons []string `json:"reasons,omitempty"`
	At      int64    `json:"at"`
	Since   int64    `json:"since,omitempty"`
	Metrics Metrics  `json:"metrics"`
}

// Notifier 抽象告警投递，便于测试替换。
type Notifier interface {
	Notify(ctx context.Context, ev Event) error
}

// Monitor 是进程内监测器。零值不可用，请用 New。
type Monitor struct {
	cfg    Config
	log    *slog.Logger
	notify Notifier
	now    func() time.Time

	mu       sync.Mutex
	samples  []Sample
	status   Status
	lastSent time.Time
	since    time.Time
}

// New 创建监测器。notifier 为 nil 时使用 WebhookNotifier（未配置 URL 则只记日志）。
func New(cfg Config, log *slog.Logger, notifier Notifier) *Monitor {
	if log == nil {
		log = slog.Default()
	}
	if notifier == nil {
		notifier = NewNotifier(cfg)
	}
	m := &Monitor{
		cfg:    cfg,
		log:    log,
		notify: notifier,
		now:    time.Now,
		status: Status{Enabled: cfg.Enabled, OK: true},
	}
	return m
}

// Enabled 报告监测是否开启。
func (m *Monitor) Enabled() bool { return m != nil && m.cfg.Enabled }

// Record 记录一次请求样本。可在任意 goroutine 调用，不阻塞。
func (m *Monitor) Record(s Sample) {
	if !m.Enabled() {
		return
	}
	if s.At == 0 {
		s.At = m.now().UnixMilli()
	}
	m.mu.Lock()
	m.samples = append(m.samples, s)
	if max := m.cfg.Window + m.cfg.Baseline; max > 0 && len(m.samples) > max {
		m.samples = append([]Sample(nil), m.samples[len(m.samples)-max:]...)
	}
	m.mu.Unlock()
}

// Snapshot 返回当前状态的副本。
func (m *Monitor) Snapshot() Status {
	if m == nil {
		return Status{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status
	st.Enabled = m.cfg.Enabled
	return st
}

// Evaluate 依据当前缓冲算一次判定并推进状态机。返回最新状态。
func (m *Monitor) Evaluate(ctx context.Context) Status {
	if !m.Enabled() {
		return m.Snapshot()
	}
	now := m.now()

	m.mu.Lock()
	recent, base := m.splitLocked()
	metrics := computeMetrics(recent, base)
	reasons := triage(metrics, m.cfg)

	prev := m.status
	st := Status{
		Enabled:     true,
		OK:          len(reasons) == 0,
		Reason:      firstReason(reasons),
		Reasons:     reasons,
		Since:       prev.Since,
		LastAlertAt: prev.LastAlertAt,
		Metrics:     metrics,
	}
	if st.OK {
		st.Reason, st.Reasons = "", nil
		if !prev.OK {
			since := m.since
			if since.IsZero() {
				since = now
			}
			st.Since = 0
			st.LastAlertAt = 0
			m.since = time.Time{}
			m.lastSent = time.Time{}
			st.Metrics = metrics
			m.status = st
			m.mu.Unlock()
			m.send(ctx, Event{Kind: "recover", Reason: prev.Reason, At: now.UnixMilli(), Since: since.UnixMilli(), Metrics: metrics})
			return st
		}
		st.Since = 0
		m.status = st
		m.mu.Unlock()
		return st
	}

	enter := prev.OK
	if enter {
		m.since = now
		st.Since = now.UnixMilli()
		m.lastSent = time.Time{}
	}
	if m.since.IsZero() {
		m.since = now
		st.Since = now.UnixMilli()
	}

	shouldSend := enter || m.cfg.Reminder <= 0 || now.Sub(m.lastSent) >= m.cfg.Reminder
	if shouldSend {
		m.lastSent = now
		st.LastAlertAt = now.UnixMilli()
	}
	m.status = st
	m.mu.Unlock()

	if shouldSend {
		kind := "alert"
		if !enter {
			kind = "remind"
		}
		m.send(ctx, Event{Kind: kind, Reason: st.Reason, Reasons: reasons, At: now.UnixMilli(), Since: st.Since, Metrics: metrics})
	}
	return st
}

// Run 周期性评估，直到 ctx 结束。
func (m *Monitor) Run(ctx context.Context) {
	if !m.Enabled() {
		return
	}
	interval := m.cfg.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	m.log.Info("watch: 波动监测已启用", "interval", interval.String(), "window", m.cfg.Window, "baseline", m.cfg.Baseline, "webhook", m.cfg.WebhookURL != "")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Evaluate(ctx)
		}
	}
}

func (m *Monitor) send(ctx context.Context, ev Event) {
	if m.notify == nil {
		return
	}
	switch ev.Kind {
	case "alert":
		m.log.Warn("watch: 检测到异常", "reason", ev.Reason, "metrics", ev.Metrics)
	case "remind":
		m.log.Warn("watch: 异常仍在持续", "reason", ev.Reason, "metrics", ev.Metrics)
	case "recover":
		m.log.Info("watch: 已恢复正常", "was", ev.Reason, "metrics", ev.Metrics)
	}
	if err := m.notify.Notify(ctx, ev); err != nil {
		m.log.Error("watch: 告警投递失败", "kind", ev.Kind, "error", err)
	}
}

func (m *Monitor) splitLocked() (recent, base []Sample) {
	n := len(m.samples)
	if n == 0 {
		return nil, nil
	}
	w := m.cfg.Window
	if w <= 0 || w > n {
		w = n
	}
	recent = m.samples[n-w:]
	rest := m.samples[:n-w]
	b := m.cfg.Baseline
	if b > 0 && len(rest) > b {
		rest = rest[len(rest)-b:]
	}
	return recent, rest
}

func computeMetrics(recent, base []Sample) Metrics {
	mt := Metrics{Samples: len(recent)}
	if len(recent) == 0 {
		return mt
	}
	failed := 0
	asked := 0
	missed := 0
	lat := make([]int64, 0, len(recent))
	for _, s := range recent {
		if !s.Ok {
			failed++
		}
		if s.Ok && s.ReasoningAsked {
			asked++
			if s.ReasoningTokens <= 0 {
				missed++
			}
		}
		if s.Ok && s.DurationMs > 0 {
			lat = append(lat, s.DurationMs)
		}
	}
	mt.ErrorRate = ratio(failed, len(recent))
	mt.ReasoningAsked = asked
	mt.ReasoningMissed = missed
	mt.ReasoningMissRate = ratio(missed, asked)
	mt.LatencyP50 = int64Median(lat)

	baseAsked, baseMissed := 0, 0
	baseLat := make([]int64, 0, len(base))
	for _, s := range base {
		if s.Ok && s.ReasoningAsked {
			baseAsked++
			if s.ReasoningTokens <= 0 {
				baseMissed++
			}
		}
		if s.Ok && s.DurationMs > 0 {
			baseLat = append(baseLat, s.DurationMs)
		}
	}
	mt.BaselineMissRate = ratio(baseMissed, baseAsked)
	mt.BaselineLatencyP50 = int64Median(baseLat)
	return mt
}

func triage(mt Metrics, cfg Config) []string {
	var reasons []string
	if mt.Samples < cfg.MinSample {
		return nil
	}
	if mt.ErrorRate > cfg.ErrorRate {
		reasons = append(reasons, fmt.Sprintf("错误率偏高: %.0f%%", mt.ErrorRate*100))
	}
	if mt.ReasoningAsked >= cfg.MinSample &&
		mt.ReasoningMissRate > cfg.ReasoningMissRate &&
		mt.ReasoningMissRate-mt.BaselineMissRate > 0.2 {
		reasons = append(reasons, fmt.Sprintf("推理字段缺失: %.0f%%（基线 %.0f%%）", mt.ReasoningMissRate*100, mt.BaselineMissRate*100))
	}
	if cfg.MinBaselineMs > 0 && mt.BaselineLatencyP50 >= cfg.MinBaselineMs &&
		mt.LatencyP50 > int64(math.Round(float64(mt.BaselineLatencyP50)*cfg.LatencyFactor)) {
		reasons = append(reasons, fmt.Sprintf("耗时异常: P50 %dms（基线 %dms）", mt.LatencyP50, mt.BaselineLatencyP50))
	}
	return reasons
}

func firstReason(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return reasons[0]
}

func ratio(a, b int) float64 {
	if b <= 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func int64Median(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]int64(nil), v...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}
