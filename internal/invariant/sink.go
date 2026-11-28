package invariant

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// dedupeWindow：同一 (invariant, subject) 在这个窗口内只落一条。
// 10 分钟是经验值：足够把一个持续故障压成一条，又不会把「又来了一次」
// 永久吞掉。
const dedupeWindow = 10 * time.Minute

// Sink 是在线执行器：同步跑断言、异步落盘、面板环形缓冲。
//
// 三条硬约束：
//  1. Submit 绝不做 I/O，也绝不阻塞 —— 队列满就丢，只计数
//  2. 输入记录（request/selection/quota）与命中的 finding **都**落盘：
//     只落 finding 的话，离线回放永远没有输入，本机制就废了
//  3. 日志在写协程里发，不在 Submit 路径上发 —— 后者常常运行在
//     Pool 的锁里，一次卡住的 journald 写入会串行阻塞整个选号
type Sink struct {
	store *decision.Store
	reg   *Registry
	state *State

	queue chan decision.Record

	ringSize int

	mu        sync.Mutex
	ring      []decision.Finding
	dedupe    map[string]*dedupeEntry
	supCount  int64
	startedAt int64

	dropped   atomic.Int64
	writeErrs atomic.Int64
	persisted atomic.Int64

	started atomic.Bool
	stopped atomic.Bool

	startOnce sync.Once
	stopOnce  sync.Once
	doneCh    chan struct{}
	stopCh    chan struct{}

	// logf 由调用方注入，避免 invariant 包依赖具体日志库。
	// 只在写协程里调用。
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

// SetLogger 注入日志函数（可选）。函数会被写在写协程里调用，
// 因此可以安全地做 I/O。
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
		s.started.Store(true)
		go s.loop()
	})
}

// Stop 关闭写协程并等待退出（幂等）。
//
// 未 Start 过时立即返回：否则每次都会白等 2 秒（测试里大量
// 「构造但不 Start」的用法会因此变慢）。
func (s *Sink) Stop() {
	if s == nil {
		return
	}
	if !s.started.Load() {
		s.stopOnce.Do(func() { close(s.stopCh) })
		return
	}
	s.stopOnce.Do(func() {
		s.stopped.Store(true)
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
			s.handle(rec)
		case <-s.stopCh:
			// 尽力把队列里剩下的写完，但不无限等。
			for {
				select {
				case rec := <-s.queue:
					s.handle(rec)
				default:
					return
				}
			}
		}
	}
}

// handle 在写协程里执行：先发日志（去重后的 finding 才发），再落盘。
func (s *Sink) handle(rec decision.Record) {
	if rec.Kind == decision.KindFinding && rec.Finding != nil && s.logf != nil {
		f := rec.Finding
		s.logf("invariant hit", map[string]any{
			"invariant": f.Invariant, "severity": f.Severity,
			"subject": f.Subject, "detail": f.Detail,
		})
	}
	s.persist(rec)
}

func (s *Sink) persist(rec decision.Record) {
	if s.store == nil {
		return
	}
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = nil // panic 不冒泡，但计入 writeErrs 由下面统一处理
			}
		}()
		return s.store.Append(rec)
	}()
	if err != nil {
		s.writeErrs.Add(1)
		return
	}
	s.persisted.Add(1)
}

// RememberDeduct 记一条「本次请求扣了多少」，供 I2a 与 request 记录对账。
//
// amount 是实际扣减量；skipped 非空表示没有扣减（值说明原因）。
// 必须在 Submit(request) 之前调用：额度事件先发生（MarkResult），
// 请求记录后写（RecordChat）。
func (s *Sink) RememberDeduct(requestID string, amount float64, skipped string) {
	if s == nil || s.state == nil || requestID == "" {
		return
	}
	s.state.PutDeduct(requestID, amount, skipped)
}

// Submit 提交一条记录：同步跑断言（O(1) 内存操作），
// 输入记录与 finding 都投进队列落盘。
func (s *Sink) Submit(rec decision.Record) {
	if s == nil || s.reg == nil {
		return
	}
	// 先把记录里「影响后续断言的状态」入库，再跑断言：
	// 额度扣减记录必须在对应 request 到达前就位，I2a 才对的账。
	s.state.PrimeFromRecord(rec)
	findings := s.reg.Run(rec, s.state)

	// 输入记录本身也要落盘 —— 否则离线回放永远读不到 request/selection/quota，
	// 只能看到稀疏的 finding，回放会输出「日志为空」。
	if rec.Kind != decision.KindFinding {
		if rec.At == 0 {
			rec.At = time.Now().UnixMilli()
		}
		s.offer(rec)
	}
	for _, f := range findings {
		s.enqueue(f)
	}
}

// offer 把记录投进落盘队列；满则丢弃并计数（绝不阻塞请求）。
func (s *Sink) offer(rec decision.Record) {
	select {
	case s.queue <- rec:
	default:
		s.dropped.Add(1)
	}
}

func (s *Sink) enqueue(f decision.Finding) {
	now := time.Now().UnixMilli() // 去重时钟用「现在」，不用 f.At（长请求会让窗口失效）
	if f.At == 0 {
		f.At = now
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
	if len(s.ring) < s.ringSize {
		s.ring = append(s.ring, f)
		entry.indexInRing = len(s.ring) - 1
	} else {
		s.ring = append(s.ring[1:], f)
		entry.indexInRing = len(s.ring) - 1
		for k, old := range s.dedupe {
			old.indexInRing--
			if old.indexInRing < 0 {
				delete(s.dedupe, k)
			}
		}
	}
	s.dedupe[key] = entry
	s.mu.Unlock()

	s.offer(decision.Record{Kind: decision.KindFinding, At: f.At, Finding: &f})
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

// Dropped 返回因队列满被丢弃的记录数。
func (s *Sink) Dropped() int64 {
	if s == nil {
		return 0
	}
	return s.dropped.Load()
}

// WriteErrors 返回落盘失败次数。
//
// 必须暴露：只丢不报的话，「面板显示有命中、磁盘上一条没有、重启后回扫为 0」
// 就是观测系统自身的静默失效。
func (s *Sink) WriteErrors() int64 {
	if s == nil {
		return 0
	}
	return s.writeErrs.Load()
}

// Persisted 返回成功落盘的记录数（可与决策数对账）。
func (s *Sink) Persisted() int64 {
	if s == nil {
		return 0
	}
	return s.persisted.Load()
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

// Backfill 回扫最近 24h 已落盘的 finding，重建 ring 与去重表。
//
// 必须做的原因：每次部署都要重启，而「重启后显示 0 命中」与
// 「确实没发生」不可区分 —— 那正是本体系要消灭的那种静默。
//
// 只读 finding 行：使用 ReadKind 流式过滤，避免把几万条 request
// 一次性读进内存（那是 80MB 预算里最容易被吃掉的部分）。
func (s *Sink) Backfill(now time.Time) error {
	if s == nil || s.store == nil {
		return nil
	}
	from := now.Add(-24 * time.Hour)
	recs, err := s.store.ReadKind(from, now, decision.KindFinding)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Finding == nil {
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

	// 同时重建 I9 的分词器基线。
	//
	// 不重建的后果：每次重启基线从零开始，重启后前 3 个样本就固化了
	// 基线 —— 若那 3 个恰好异常（例如采样 bug 期间的假值），
	// 错误的值就被当成「正常」，之后所有真实值都被判偏离。
	// 实测踩过：重启后 ascii 基线被固化成 2.11（真实值 0.31）。
	s.backfillTokBaseline(from, now)
	return nil
}

// backfillTokBaseline 用历史 request 记录重建分词器比例基线。
func (s *Sink) backfillTokBaseline(from, to time.Time) {
	if s == nil || s.store == nil || s.state == nil {
		return
	}
	recs, err := s.store.ReadKind(from, to, decision.KindRequest)
	if err != nil {
		return
	}
	for _, rec := range recs {
		if rec.TokRatio <= 0 || rec.TokBucket == "" {
			continue
		}
		model := rec.RoutedModel
		if model == "" {
			model = rec.Model
		}
		// ObserveTokRatio 会自然累积 Fast/Slow，与在线路径完全同一套逻辑。
		s.state.ObserveTokRatio(model, rec.TokBucket, rec.TokRatio)
	}
}

// restore 把一条历史 finding 放回 ring（不做 dedupe 计数，只保留最后一条）。
func (s *Sink) restore(f decision.Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := f.Invariant + "\x00" + f.Subject
	entry, ok := s.dedupe[key]
	if ok && entry.indexInRing >= 0 && entry.indexInRing < len(s.ring) {
		if f.At < s.ring[entry.indexInRing].At {
			return // 旧记录不得覆盖更新的那条
		}
		f.Suppressed = s.ring[entry.indexInRing].Suppressed
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

// TokBaselines 返回分词器指纹基线快照（面板展示用）。
//
// 把 State 的内部结构转成面板能直接渲染的扁平行：
// 每个 (模型, 分桶) 一行，带快/慢线与样本数。
func (s *Sink) TokBaselines(limit int) []map[string]any {
	if s == nil || s.state == nil {
		return nil
	}
	all := s.state.TokBaselines()
	rows := make([]map[string]any, 0, len(all))
	for key, b := range all {
		model, bucket, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		rows = append(rows, map[string]any{
			"model":     model,
			"bucket":    bucket,
			"fast":      b.Fast,
			"slow":      b.Slow,
			"lastRatio": b.LastRatio,
			"samples":   b.Samples,
			// deviating 只在面板高亮用：真正的判定在 I9 里，
			// 这里只是「当前值是否已超出区间」的即时视图。
			"deviating": b.Samples >= 3 &&
				(b.LastRatio < b.Fast-tokTolerance*b.Fast ||
					b.LastRatio > b.Slow+tokTolerance*b.Slow),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, _ := rows[i]["model"].(string)
		b, _ := rows[j]["model"].(string)
		if a != b {
			return a < b
		}
		ba, _ := rows[i]["bucket"].(string)
		bb, _ := rows[j]["bucket"].(string)
		return ba < bb
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// Prune 清理过期的决策日志。由调用方按较低频率触发（例如每小时一次）。
func (s *Sink) Prune(now time.Time) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Prune(now)
}
