package invariant

import (
	"strings"
	"sync"
)

// defaultSessionCap / defaultErrorCap 是生产默认上限。
// 512 个会话足够覆盖并发会话；超过就淘汰最旧的 —— 宁可少记，
// 不可让「观测」变成内存泄漏。
const (
	defaultSessionCap = 512
	defaultErrorCap   = 256
)

// State 是断言需要的跨记录状态。
//
// 全部带上限且 FIFO 淘汰：这是观测用的旁路数据，不允许无界增长。
// cap 传 0 表示不限制（离线回放用：一次跑完一整段历史，不需要淘汰）。
type State struct {
	mu sync.Mutex

	sessionCap int
	// prompt 记每个会话上一次的 promptTokens（I3b）。
	prompt     map[string]int64
	promptRing []string

	errCap int
	// errCount 记「某条错误文案出现过几次」（I5 的阈值升级）。
	errCount map[string]int
	errRing  []string

	// deduct 记「某请求实际扣了多少额度」，等 request 记录到达时对账（I2a）。
	deduct map[string]float64
}

// NewState 建状态。sessionCap / errCap 为 0 表示不限制。
func NewState(sessionCap, errCap int) *State {
	return &State{
		sessionCap: sessionCap,
		errCap:     errCap,
		prompt:     make(map[string]int64),
		errCount:   make(map[string]int),
		deduct:     make(map[string]float64),
	}
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

// PutDeduct 记一条额度扣减，供 I2a 与请求记录对账。
func (s *State) PutDeduct(requestID string, credit float64) {
	if s == nil || requestID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deduct[requestID] = credit
}

// TakeDeduct 取出并删除该请求的扣减记录。
func (s *State) TakeDeduct(requestID string) (float64, bool) {
	if s == nil || requestID == "" {
		return 0, false
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
