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
	defaultTokCap     = 512
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
	// prompt 存的是「该键上一次的 (token, 消息条数) 快照」。
	//
	// 为什么必须带消息条数：只比 token 分不出「换了另一个历史分支」
	// 与「同一分支内容被压缩」—— 两者都表现为 token 骤降。
	// 分支切换必然换一整套消息（条数一起变），压缩则条数不减。
	// 实测（2026-11-27，101 条同 fp 骤降）：88 条条数一起掉（换分支），
	// 13 条条数持平（真骤降）。
	prompt     map[string]promptSnap
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

	// reasoning 是「键 → 思考退化段状态」，供 I12 判「声明要思考却持续不思考」。
	//
	// 为什么必须按段（而不是单条）：单条静默在正常账号也占 1.5–10%
	// （实测 256 条可判样本），逐条报等于天天误报；而「连续成段」
	// 才是用户真正感知到的「逐渐不思考」。
	reasoning     map[string]*reasoningRun
	reasoningRing []string

	// sessionTok 是「会话|模型|分桶 → 上一次的比例」，供 I10 做会话内前后对比。
	//
	// 与 tokBaseline 是**两套状态**，不能复用：
	//   tokBaseline：全局，每 (模型,分桶) 一条，需要 3 样本才判定
	//   sessionTok ：会话级，每 (会话,模型,分桶) 一条，两次即可判定
	//
	// 上限与会话容量共用 sessionCap（默认 512）。
	sessionTok     map[string]float64
	sessionTokRing []string

	// tokBaseline 是「(模型, 分桶) → 快/慢双基线」的分词器比例基线。
	//
	// 它的用途是**发现模型被悄悄换掉**：回显的 model 字段（I8）只能
	// 覆盖「上游老实说了」的情况，而 token 比例是改不了的物理证据。
	//
	// 上限：最多 512 条（模型×分桶的组合远小于此）。
	tokCap      int
	tokBaseline map[string]*tokBaseline
	tokRing     []string
}

// NewState 建状态。任一 cap 为 0 表示该项不限制。
func NewState(sessionCap, errCap int) *State {
	return &State{
		sessionCap:  sessionCap,
		errCap:      errCap,
		deductCap:   defaultDeductCap,
		tokCap:      defaultTokCap,
		prompt:      make(map[string]promptSnap),
		errCount:    make(map[string]int),
		deduct:      make(map[string]deductRec),
		tokBaseline: make(map[string]*tokBaseline),
		sessionTok:  make(map[string]float64),
		reasoning:   make(map[string]*reasoningRun),
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

// promptSnap 是某键上一次的上下文快照。
type promptSnap struct {
	Tokens     int64
	InMessages int
}

// PrevPrompt 返回该会话上一次的上下文快照。
func (s *State) PrevPrompt(key string) (promptSnap, bool) {
	if s == nil || key == "" {
		return promptSnap{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.prompt[key]
	return v, ok
}

// SetPrompt 只记 token（历史调用点与既有测试用）。
func (s *State) SetPrompt(key string, tokens int64) {
	s.SetPromptWithIO(key, tokens, 0)
}

// SetPromptWithIO 记录该会话本次的 (promptTokens, 消息条数)。
//
// inMessages 只用于「换分支 vs 真骤降」的判别；传 0 表示未观测，
// 此时 I3b 会退化成「只比 token」（与加闸门前等价，不误报）。
func (s *State) SetPromptWithIO(key string, tokens int64, inMessages int) {
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
	s.prompt[key] = promptSnap{Tokens: tokens, InMessages: inMessages}
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

// ReasoningSnapshot 是段状态的**值快照**。
//
// 只返回值、不返回指针：断言在请求路径上与多个 goroutine 并发执行，
// 交出内部指针就是数据竞争（评审 C1 已因此修过一次）。
type ReasoningSnapshot struct {
	RunLen       int   // 当前静默段的长度（0 = 已恢复）
	RunStart     int64 // 当前段的起始毫秒
	LastRunLen   int   // 上一条把段打断时该段有多长（I12c 报「恢复」用）
	LastRunStart int64 // 上一条被打断段的起始毫秒
	PriorThink   int   // 历史上思考过的条数
	PriorJudged  int   // 历史上可判的条数（含静默；静默也是可判记录）
	IsNewRun     bool  // 本次是否新开了一段
}

type reasoningRun struct {
	runLen      int
	runStartAt  int64
	lastLen     int
	lastStart   int64
	priorThink  int // 历史上思考过的条数
	priorJudged int // 历史上可判的条数（含静默）
}

// ObserveReasoning 更新该键的思考退化段状态，返回**值快照**。
//
// judged=false 表示这条请求不可判（tiny / 取消 / 未声明思考），此时既不计数
// 也不断段 —— 坏会话里大量 tiny 回复会把长段切成碎片，段长永远到不了阈值。
func (s *State) ObserveReasoning(key string, judged, silent bool, at int64) (ReasoningSnapshot, bool) {
	if s == nil || key == "" {
		return ReasoningSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reasoning == nil {
		s.reasoning = make(map[string]*reasoningRun)
	}
	cur, had := s.reasoning[key]
	if !had && s.sessionCap > 0 {
		for len(s.reasoningRing) >= s.sessionCap {
			oldest := s.reasoningRing[0]
			s.reasoningRing = s.reasoningRing[1:]
			delete(s.reasoning, oldest)
		}
	}
	newRun := !had
	if newRun {
		s.reasoningRing = append(s.reasoningRing, key)
		cur = &reasoningRun{runStartAt: at}
		s.reasoning[key] = cur
	}
	if judged {
		if silent {
			if cur.runLen == 0 {
				cur.runStartAt = at
				newRun = true
			}
			cur.runLen++
			// 静默也是「可判」记录：必须计入 priorJudged，否则
			// 「此前 X 条中 Y 条思考」的 X 会退化成等于分子、百分比恒 100%，
			// 读不出「退化前它其实已经只剩 21% 在思考」这个关键事实。
			// （2026-11-27 用真实会话数据发现：线上口径 6 条 vs 标定口径 15 条。）
			cur.priorJudged++
		} else {
			// 归档刚被打断的段，供 I12c 报「恢复」时使用。
			if cur.runLen > 0 {
				cur.lastLen = cur.runLen
				cur.lastStart = cur.runStartAt
			}
			// priorJudged 已在上面的 silent 分支按「全部可判记录」累计；
			// 这里只额外把「思考过的」计入 priorThink。两者之差就是静默条数。
			cur.priorJudged++
			cur.priorThink++
			cur.runLen = 0
		}
	}
	return ReasoningSnapshot{
		RunLen: cur.runLen, RunStart: cur.runStartAt,
		LastRunLen: cur.lastLen, LastRunStart: cur.lastStart,
		PriorThink: cur.priorThink, PriorJudged: cur.priorJudged,
		IsNewRun: newRun,
	}, true
}

// ReasoningObserveCount 返回当前观测键数（测试与面板用）。
func (s *State) ReasoningObserveCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reasoning)
}

// ObserveSessionRatio 记录该会话本次的比例，返回上次的值与是否存在。
//
// 与 SetPrompt 同一套 LRU 逻辑：会话数超过 sessionCap 时淘汰最旧的。
func (s *State) ObserveSessionRatio(key string, ratio float64) (float64, bool) {
	if s == nil || key == "" {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionTok == nil {
		s.sessionTok = make(map[string]float64)
	}
	prev, had := s.sessionTok[key]
	if !had && s.sessionCap > 0 {
		for len(s.sessionTokRing) >= s.sessionCap {
			oldest := s.sessionTokRing[0]
			s.sessionTokRing = s.sessionTokRing[1:]
			delete(s.sessionTok, oldest)
		}
	}
	if !had {
		s.sessionTokRing = append(s.sessionTokRing, key)
	}
	s.sessionTok[key] = ratio
	return prev, had
}

// SessionTokCount 返回当前会话参照条数（测试与面板用）。
func (s *State) SessionTokCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessionTok)
}
