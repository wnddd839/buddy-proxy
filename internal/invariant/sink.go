package invariant

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// dedupeWindow：同一 (invariant, subject) 在这个窗口内只落一条。
// 10 分钟是经验值：足够把一个持续故障压成一条，又不会把「又来了一次」
// 永久吞掉。
const dedupeWindow = 10 * time.Minute

// Sink 是在线执行器：同步跑断言，异步落盘 + 面板环形缓冲。
//
// 三条硬约束：
//  1. Submit 绝不做 I/O，也绝不阻塞 —— 队列满就丢，只计数
//  2. 断言 panic 已被 Registry 兜住，这里再包一层 defer 保底
//  3. 去重与 ring 都在锁内，但临界区只有 map 操作
type Sink struct {
	store *decision.Store
	reg   *Registry
	state *State

	queue chan decision.Record

	ringSize int

	mu         sync.Mutex
	ring       []decision.Finding
	dedupe     map[string]*dedupeEntry
	supCount   int64
	startedAt  int64
	backfilled bool

	dropped atomic.Int64

	startOnce sync.Once
	stopOnce  sync.Once
	doneCh    chan struct{}
	stopCh    chan struct{}

	// logf 由调用方注入，避免 invariant 包依赖具体日志库。
	logf func(msg string, fields map[string]any)
}

type dedupeEntry struct {
	lastAt      int64
	indexInRing int
	seen        int
}

// NewSink 建一个在线执行器。queueSize<=0 时默认 1024，ringSize<=0 时默认 200。
func NewSink(store *decision.Store, reg *Registry, st *State, ringSize, queueSize int) *Sink {
	if queueSize <= 0 {
		queueSize = 1024
	}
	if ringSize <= 0 {
		ringSize = 200
	}
	return &Sink{
		store:     store,
		reg:       reg,
		state:     st,
		queue:     make(chan decision.Record, queueSize),
		ringSize:  ringSize,
		dedupe:    make(map[string]*dedupeEntry),
		doneCh:    make(chan struct{}),
		stopCh:    make(chan struct{}),
		startedAt: time.Now().UnixMilli(),
	}
}

// SetLogger 注入日志函数（可选）。
func (s *Sink) SetLogger(fn func(msg string, fields map[string]any)) {
	if s == nil {
		return
	}
	s.logf = fn
}

// Start 启动写协程。
func (s *Sink) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.loop()
	})
}

// Stop 关闭写协程并等待退出（幂等）。
func (s *Sink) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	select {
	case <-s.doneCh:
	case <-time.After(2 * time.Second):
	}
}

func (s *Sink) loop() {
	defer close(s.doneCh)
	for {
		select {
		case rec := <-s.queue:
			s.persist(rec)
		case <-s.stopCh:
			// 尽力把队列里剩下的写完，但不无限等。
			for {
				select {
				case rec := <-s.queue:
					s.persist(rec)
				default:
					return
				}
			}
		}
	}
}

func (s *Sink) persist(rec decision.Record) {
	if s.store == nil {
		return
	}
	defer func() { _ = recover() }() // 落盘失败绝不冒泡
	_ = s.store.Append(rec)
}

// Submit 提交一条记录：同步跑断言（O(1) 内存操作），finding 入队。
//
// 记录本身不落盘 —— 落盘由调用方自己的 Decision 写入器负责，
// Sink 只负责「跑断言 + 落 finding」，避免同一行写两遍。
func (s *Sink) Submit(rec decision.Record) {
	if s == nil || s.reg == nil {
		return
	}
	findings := s.reg.Run(rec, s.state)
	for _, f := range findings {
		s.enqueue(f)
	}
}

func (s *Sink) enqueue(f decision.Finding) {
	now := f.At
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	key := f.Invariant + "\x00" + f.Subject

	s.mu.Lock()
	if e, ok := s.dedupe[key]; ok && now-e.lastAt < dedupeWindow.Milliseconds() {
		e.seen++
		e.lastAt = now
		if e.indexInRing >= 0 && e.indexInRing < len(s.ring) {
			s.ring[e.indexInRing].Suppressed = e.seen - 1
		}
		s.supCount++
		s.mu.Unlock()
		return
	}
	entry := &dedupeEntry{lastAt: now, indexInRing: -1, seen: 1}
	// 环形推进
	if len(s.ring) < s.ringSize {
		s.ring = append(s.ring, f)
		entry.indexInRing = len(s.ring) - 1
	} else {
		// 覆盖最旧的那条（FIFO），并让它的去重项失效
		s.ring = append(s.ring[1:], f)
		entry.indexInRing = len(s.ring) - 1
		for k, old := range s.dedupe {
			if old.indexInRing >= 0 {
				old.indexInRing--
			}
			if old.indexInRing < 0 {
				delete(s.dedupe, k)
			}
		}
	}
	s.dedupe[key] = entry
	s.mu.Unlock()

	if s.logf != nil {
		s.logf("invariant hit", map[string]any{
			"invariant": f.Invariant, "severity": f.Severity,
			"subject": f.Subject, "detail": f.Detail,
		})
	}

	select {
	case s.queue <- decision.Record{Kind: decision.KindFinding, At: now, Finding: &f}:
	default:
		s.dropped.Add(1)
	}
}

// Findings 返回最近命中的副本（最新在前）。
func (s *Sink) Findings(limit int) []decision.Finding {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]decision.Finding, 0, len(s.ring))
	for i := len(s.ring) - 1; i >= 0; i-- {
		out = append(out, s.ring[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Dropped 返回因队列满被丢弃的 finding 数。
func (s *Sink) Dropped() int64 {
	if s == nil {
		return 0
	}
	return s.dropped.Load()
}

// Suppressed 返回因去重被压掉的条数。
func (s *Sink) Suppressed() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.supCount
}

// StartedAt 是进程内计数起点（面板要标注「自 … 起」）。
func (s *Sink) StartedAt() int64 {
	if s == nil {
		return 0
	}
	return s.startedAt
}

// Counts 按不变量聚合近 24h 内的命中数（面板用）。
func (s *Sink) Counts(now time.Time) map[string]int {
	if s == nil {
		return nil
	}
	cutoff := now.Add(-24 * time.Hour).UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int)
	for _, f := range s.ring {
		if f.At < cutoff {
			continue
		}
		out[f.Invariant]++
	}
	return out
}

// Backfill 回扫今天与昨天已经落盘的 finding，重建 ring 与去重表。
//
// 必须做的原因：每次部署都要重启，而「重启后显示 0 命中」与
// 「确实没发生」不可区分 —— 那正是本体系要消灭的那种静默。
func (s *Sink) Backfill(now time.Time) error {
	if s == nil || s.store == nil {
		return nil
	}
	from := now.Add(-24 * time.Hour)
	recs, err := s.store.ReadRange(from, now)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Kind != decision.KindFinding || rec.Finding == nil {
			continue
		}
		f := *rec.Finding
		if f.At == 0 {
			f.At = rec.At
		}
		if f.At < from.UnixMilli() {
			continue
		}
		s.restore(f)
	}
	s.mu.Lock()
	s.backfilled = true
	s.mu.Unlock()
	return nil
}

// restore 把一条历史 finding 放回 ring（不做 dedupe 计数，只保留最后一条）。
func (s *Sink) restore(f decision.Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := f.Invariant + "\x00" + f.Subject
	entry, ok := s.dedupe[key]
	if ok && entry.indexInRing >= 0 && entry.indexInRing < len(s.ring) {
		s.ring[entry.indexInRing] = f
		entry.lastAt = f.At
		return
	}
	if len(s.ring) < s.ringSize {
		s.ring = append(s.ring, f)
		s.dedupe[key] = &dedupeEntry{lastAt: f.At, indexInRing: len(s.ring) - 1, seen: 1}
		return
	}
	s.ring = append(s.ring[1:], f)
	for k, old := range s.dedupe {
		old.indexInRing--
		if old.indexInRing < 0 {
			delete(s.dedupe, k)
		}
	}
	s.dedupe[key] = &dedupeEntry{lastAt: f.At, indexInRing: len(s.ring) - 1, seen: 1}
}
