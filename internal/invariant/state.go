package invariant

import (
	"strings"
	"sync"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 生产默认上限。512 个会话足够覆盖并发会话；超过就淘汰最旧的 ——
// 观测数据不允许无界增长。
const (
	defaultSessionCap = 512
	defaultErrorCap   = 256
	defaultDeductCap  = 4096
)

// deductRec 是一条「本次请求扣了多少」的记录。
//
// Skipped 非空表示**没有扣**，值说明为什么。必须分开记：
// I2a 要能区分「额度未知，扣不了（设计内）」与「上游收费了，
// 扣减路径却没跑（真 bug）」—— 只看金额分不出这两者。
type deductRec struct {
	Amount  float64
	Skipped string
}

// State 是断言需要的跨记录状态。
//
// 全部带上限且 FIFO 淘汰：这是观测用的旁路数据，不允许无界增长。
// cap 传 0 表示不限制（离线回放用：一次跑完一整段历史，不需要淘汰）。
//
// 锁序：Pool.mu → State.mu。反向获取必死锁（观察者在 Pool 锁内调 State）。
type State struct {
	mu sync.Mutex

	sessionCap int
	prompt     map[string]int64
	promptRing []string

	errCap   int
	errCount map[string]int
	errRing  []string

	// deduct 只在「请求结束 → 记录写入」这段窗口内有值，窗口远小于
	// 一次请求的生命周期。仍然设上限：一旦某个入口的 RequestID 与
	// ProxyRequestID 不同源，没有上限就会变成慢性内存泄漏。
	deductCap  int
	deduct     map[string]deductRec
	deductRing []string
}

// NewState 建状态。任一 cap 为 0 表示该项不限制。
func NewState(sessionCap, errCap int) *State {
	return &State{
		sessionCap: sessionCap,
		errCap:     errCap,
		deductCap:  defaultDeductCap,
		prompt:     make(map[string]int64),
		errCount:   make(map[string]int),
		deduct:     make(map[string]deductRec),
	}
}

// SetDeductCap 调整扣减记录的容量上限（0 = 不限制，离线回放用）。
func (s *State) SetDeductCap(n int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.deductCap = n
	s.mu.Unlock()
}

// PrevPrompt 返回该会话上一次的 promptTokens。
func (s *State) PrevPrompt(key string) (int64, bool) {
	if s == nil || key == "" {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.prompt[key]
	return v, ok
}

// SetPrompt 记录该会话本次的 promptTokens。
func (s *State) SetPrompt(key string, tokens int64) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.prompt[key]; !exists && s.sessionCap > 0 {
		for len(s.promptRing) >= s.sessionCap {
			oldest := s.promptRing[0]
			s.promptRing = s.promptRing[1:]
			delete(s.prompt, oldest)
		}
		s.promptRing = append(s.promptRing, key)
	}
	s.prompt[key] = tokens
}

// PrimeFromRecord 把一条决策记录里「对后续断言有影响的状态」写入 State。
//
// 在线与离线两个执行器必须都调用它：否则回放会缺少扣减记录，
// 把每一笔正常扣费都判成「上游收费但本地没记账」（影子回放抓到过）。
func (s *State) PrimeFromRecord(rec decision.Record) {
	if s == nil || rec.Kind != decision.KindQuota {
		return
	}
	if rec.Op != decision.OpDeduct || rec.RequestID == "" {
		return
	}
	amount := 0.0
	if rec.Before != nil && rec.After != nil {
		amount = *rec.Before - *rec.After
	}
	s.PutDeduct(rec.RequestID, amount, rec.SkipReason)
}

// PutDeduct 记一条扣减结果（amount 是**实际扣减量**，skipped 说明为何没扣）。
func (s *State) PutDeduct(requestID string, amount float64, skipped string) {
	if s == nil || requestID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.deduct[requestID]; !exists && s.deductCap > 0 {
		for len(s.deductRing) >= s.deductCap {
			oldest := s.deductRing[0]
			s.deductRing = s.deductRing[1:]
			delete(s.deduct, oldest)
		}
		s.deductRing = append(s.deductRing, requestID)
	}
	s.deduct[requestID] = deductRec{Amount: amount, Skipped: skipped}
}

// TakeDeduct 取出并删除该请求的扣减记录。
func (s *State) TakeDeduct(requestID string) (deductRec, bool) {
	if s == nil || requestID == "" {
		return deductRec{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.deduct[requestID]
	if ok {
		delete(s.deduct, requestID)
	}
	return v, ok
}

// PendingDeducts 返回尚未对账的扣减条数（面板诊断用）。
func (s *State) PendingDeducts() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.deduct)
}

// BumpError 给某条错误文案计数并返回当前次数。
// 文案先归一（去空白、截断），否则同一条错误会因为 requestId 不同而分裂。
func (s *State) BumpError(text string) int {
	if s == nil {
		return 0
	}
	key := normalizeErrorText(text)
	if key == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.errCount[key]; !exists && s.errCap > 0 {
		for len(s.errRing) >= s.errCap {
			oldest := s.errRing[0]
			s.errRing = s.errRing[1:]
			delete(s.errCount, oldest)
		}
		s.errRing = append(s.errRing, key)
	}
	s.errCount[key]++
	return s.errCount[key]
}

// normalizeErrorText 把错误文案压成可聚合的键：取前 120 字符。
// 不在这里做正则改写 —— 上游措辞变了应该被看见，而不是被抹平。
func normalizeErrorText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	const maxLen = 120
	if len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

// SessionKey 拼出会话级断言的 subject：换号或换模型都会改变上游会话连续性，
// 所以三者都要进键。
func SessionKey(sessionKey, accountID, model string) string {
	if sessionKey == "" {
		return ""
	}
	return sessionKey + "|" + accountID + "|" + model
}
