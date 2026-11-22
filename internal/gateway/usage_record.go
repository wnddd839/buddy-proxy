package gateway

import (
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
	"github.com/wnddd839/codebuddy-proxy/internal/usagejournal"
	"github.com/wnddd839/codebuddy-proxy/internal/watch"
)

// ChatRecordInput is one gateway-visible chat completion for the usage journal.
type ChatRecordInput struct {
	Started        time.Time
	ProxyRequestID string
	SessionKey     string
	SessionLabel   string
	Model          string
	// RoutedModel 是实际发给上游的模型名（被模型路由或标题改写改过时才有值）。
	// 与 Model 并列而不是覆盖它：前者回答「客户端要了什么」，后者回答
	// 「真正跑的是哪个」—— 改写是否生效只有并排才看得出来。
	RoutedModel string
	Stream      bool
	Ok          bool
	Canceled    bool
	ErrMsg      string
	Usage       provider.Usage
	// ReasoningRequested 表示客户端显式要求了推理（reasoning_effort / reasoning / thinking）。
	// 只有为 true 时，缺失推理 token 才说明上游掉了思考能力。
	ReasoningRequested bool
	// ReasoningEffort 是客户端本次传的档位原值（空 = 没传）。
	// 与 usage 里的 reasoning_tokens 配对落盘，才能回答「哪一档更省」。
	ReasoningEffort string
	// ReasoningTier 是收敛后的上游实际档位（low/high/max）。
	ReasoningTier string
	Trace         provider.RequestTrace
	Account       accounts.Summary
	AccountID     string

	// SystemForm / SystemFp / SystemDegraded 记录本次请求实际使用的 system 形态。
	// SystemFp 只是 SHA-256 前 8 位，绝不存 system 原文。
	SystemForm     string
	SystemFp       string
	SystemDegraded bool
	// SystemStripped / StripScope 说明这次剥离动到了哪一侧。
	SystemStripped bool
	StripScope     string
	// IO 是进/出两侧的消息统计（nil = 未观测）。
	IO *provider.MessagesIO
	// TitleRewrite 标记本次被改写成了标题生成请求（会话级统计要排除）。
	TitleRewrite bool
	// ErrorCode 是分类后的错误码（空 = 未分类）。
	ErrorCode string
	// UsageSource 区分上游真实值与本地估算（空 → upstream）。
	UsageSource string
	// UpstreamModel 是上游 SSE 回显的模型名（空 = 未观测）。
	// 与 RoutedModel 不同：后者是「我们发出去的」，这个是「上游说它跑了哪个」。
	UpstreamModel string
	// Tok* 是分词器指纹：入站字符成分 + 上游 prompt_tokens 的比值。
	// 由 IO.Composition 与 IO.PromptTokens 算出，这里用结构体携带便于传递。
	Tok *provider.MessagesIO
}

// RecordChat appends a usage journal row (persisted via usage journal flush).
func (s *Service) RecordChat(input ChatRecordInput) {
	if s == nil {
		return
	}
	duration := time.Since(input.Started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	s.recordWatchSample(input, duration)
	// 断言执行器与 usage journal 是两条独立通路：journal 可以有容量上限、
	// 可以被关闭，但那不该让「静默失效检测」跟着失明。
	s.submitInvariantRecord(input)
	if s.Journal == nil {
		return
	}
	label := strings.TrimSpace(input.Account.UserName)
	if label == "" {
		label = strings.TrimSpace(input.Account.Label)
	}
	if label == "" {
		label = strings.TrimSpace(input.Account.UserNickname)
	}
	if custom := strings.TrimSpace(input.Account.Label); custom != "" && label != "" && custom != label {
		label = custom + " · " + label
	}
	s.Journal.Record(usagejournal.Entry{
		At:                            input.Started.UnixMilli(),
		ProxyRequestID:                input.ProxyRequestID,
		UpstreamConversationID:        input.Trace.ConversationID,
		UpstreamConversationRequestID: input.Trace.ConversationRequestID,
		UpstreamMessageID:             input.Trace.MessageID,
		SessionKey:                    input.SessionKey,
		SessionLabel:                  input.SessionLabel,
		Model:                         input.Model,
		RoutedModel:                   input.RoutedModel,
		Stream:                        input.Stream,
		Ok:                            input.Ok,
		Error:                         input.ErrMsg,
		AccountID:                     input.AccountID,
		AccountLabel:                  label,
		PromptTokens:                  int64(input.Usage.PromptTokens),
		CompletionTokens:              int64(input.Usage.CompletionTokens),
		CachedTokens:                  int64(input.Usage.CachedTokens()),
		ReasoningTokens:               int64(reasoningTokens(input.Usage)),
		ReasoningEffort:               input.ReasoningEffort,
		ReasoningTier:                 input.ReasoningTier,
		Credit:                        input.Usage.Credit,
		DurationMs:                    duration,
		SystemForm:                    input.SystemForm,
		SystemFp:                      input.SystemFp,
		SystemDegraded:                input.SystemDegraded,
		SystemLen:                     ioSystemLen(input),
		SystemStripped:                input.SystemStripped,
		StripScope:                    input.StripScope,
		ErrorCode:                     input.ErrorCode,
		UsageSource:                   ioUsageSource(input),
		TitleRewrite:                  input.TitleRewrite,
		InMessages:                    ioInt(input.IO, func(m *provider.MessagesIO) int { return m.InMessages }),
		OutMessages:                   ioInt(input.IO, func(m *provider.MessagesIO) int { return m.OutMessages }),
		InBytes:                       ioInt(input.IO, func(m *provider.MessagesIO) int { return m.InBytes }),
		OutBytes:                      ioInt(input.IO, func(m *provider.MessagesIO) int { return m.OutBytes }),
		UpstreamModel:                 input.UpstreamModel,
		TokChars:                      tokEnabledInt(s, tokChars, input),
		TokCJK:                        tokEnabledInt(s, tokCJK, input),
		TokDigits:                     tokEnabledInt(s, tokDigits, input),
		TokASCII:                      tokEnabledInt(s, tokASCII, input),
		TokBucket:                     tokEnabledStr(s, tokBucket, input),
		TokRatio:                      tokEnabledFloat(s, tokRatio, input),
		ModelMismatch:                 modelMismatch(input.Model, input.RoutedModel, input.UpstreamModel),
	})
}

// submitInvariantRecord 把一条请求喂给不变量执行器。
//
// 与 usage journal 分开：journal 是给人看的明细（有容量上限），
// 这里是给断言看的输入（无容量上限，且要带 IO 两侧统计与豁免标记）。
func (s *Service) submitInvariantRecord(input ChatRecordInput) {
	sink := s.InvariantSink()
	if sink == nil {
		return
	}
	rec := decision.Record{
		Kind:             decision.KindRequest,
		At:               input.Started.UnixMilli(),
		RequestID:        input.ProxyRequestID,
		SessionKey:       input.SessionKey,
		AccountID:        input.AccountID,
		Model:            input.Model,
		RoutedModel:      input.RoutedModel,
		UpstreamModel:    input.UpstreamModel,
		TokChars:         tokEnabledInt(s, tokChars, input),
		TokCJK:           tokEnabledInt(s, tokCJK, input),
		TokDigits:        tokEnabledInt(s, tokDigits, input),
		TokASCII:         tokEnabledInt(s, tokASCII, input),
		TokBucket:        tokEnabledStr(s, tokBucket, input),
		TokRatio:         tokEnabledFloat(s, tokRatio, input),
		PromptTokens:     int64(input.Usage.PromptTokens),
		CompletionTokens: int64(input.Usage.CompletionTokens),
		UsageSource:      input.UsageSource,
		Canceled:         input.Canceled,
		TitleRewrite:     input.TitleRewrite,
		SystemFp:         input.SystemFp,
		SystemForm:       input.SystemForm,
		Stripped:         input.SystemStripped,
		StripScope:       input.StripScope,
		Degraded:         input.SystemDegraded,
		Ok:               input.Ok,
		ErrorCode:        input.ErrorCode,
		ErrorText:        input.ErrMsg,
		Credit:           input.Usage.Credit,
	}
	if input.IO != nil {
		rec.InMessages = input.IO.InMessages
		rec.OutMessages = input.IO.OutMessages
		rec.InBytes = input.IO.InBytes
		rec.OutBytes = input.IO.OutBytes
		rec.InSystemLen = input.IO.InSystemLen
		rec.OutSystemLen = input.IO.OutSystemLen
	}
	// 错误码必须在这里补齐：留空会让 I5 把所有失败都当成「未分类」，
	// 包括分类器明明认得的那一类（11128 / 14018 …）。
	code, kind := classifyForDecision(input.Ok, input.Canceled, input.ErrMsg, input.ErrorCode)
	rec.ErrorCode = code
	rec.ErrorKind = kind
	sink.Submit(rec)
}

// classifyForDecision 给失败请求补上错误码与粗分类。
//
// kind 的用途是把「不该算进未分类」的几种情形摘出去：
//   - canceled     客户端主动断开（不是故障）
//   - coded        有数字码（分类器认得）
//   - empty        没有任何错误原文（上游空回包之类，单独一类）
//   - unclassified 有原文但没有码 —— 只有这一类才进 I5
func classifyForDecision(ok, canceled bool, errMsg, provided string) (code, kind string) {
	if ok {
		return "", ""
	}
	if canceled || strings.Contains(strings.ToLower(errMsg), "context canceled") {
		return "", "canceled"
	}
	if provided != "" {
		return provided, "coded"
	}
	if c := upstreamErrorCode(errMsg); c != "" {
		return c, "coded"
	}
	if strings.TrimSpace(errMsg) == "" {
		return "", "empty"
	}
	return "", "unclassified"
}

// modelMismatch 判定「上游是否真的跑了另一个模型」。
//
// 实测口径（2026-11-22 真实请求）：
//
//	deepseek-v4.1-flash → deepseek-v4.1-flash   一致
//	hy4flash-free       → hy4-flash-free        差一个连字符
//	wb-lite             → wb-lite               一致
//	auto                → glm-5.3-flash         上游解析
//
// 所以判据不能是简单相等：
//   - 上游没回显（空）                 → 不判（未观测）
//   - 请求的是 auto 一族               → 不判（上游有权选）
//   - 其余：归一化后必须相等
//
// 归一化 = 小写 + 去掉 - _ . 空白。这样 hy4flash-free 与 hy4-flash-free
// 视为同一个，但 deepseek 与 glm 不会混。
func modelMismatch(requested, routed, upstream string) bool {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return false
	}
	sent := strings.TrimSpace(routed)
	if sent == "" {
		sent = strings.TrimSpace(requested)
	}
	if sent == "" {
		return false
	}
	if isAutoFamily(sent) {
		return false
	}
	return normalizeModelName(sent) != normalizeModelName(upstream)
}

// isAutoFamily：auto / auto-* / 空（先剥站点前缀，cn:auto 也是 auto）。
func isAutoFamily(model string) bool {
	m := strings.ToLower(strings.TrimSpace(stripModelSitePrefix(strings.TrimSpace(model))))
	return m == "auto" || strings.HasPrefix(m, "auto-")
}

// sitePrefixes 是客户端可显式指定的站点前缀（cn: / global:）。
// 它们是语法前缀，不属于模型名 —— 不剥掉会让每个带前缀的请求都被判成替换。
var sitePrefixes = []string{"cn:", "global:"}

// stripModelSitePrefix 只剥开头的、已知的两个前缀。
func stripModelSitePrefix(model string) string {
	for _, p := range sitePrefixes {
		if len(model) > len(p) && model[:len(p)] == p {
			return model[len(p):]
		}
	}
	return model
}

// normalizeModelName 去掉站点前缀、大小写与分隔符差异。
func normalizeModelName(model string) string {
	model = stripModelSitePrefix(model)
	var b strings.Builder
	for _, r := range strings.ToLower(model) {
		switch r {
		case '-', '_', '.', ' ', '/', ':':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tokChars / tokCJK / ... 读分词器指纹的各个分量（未观测时返回零值）。
// 单独拆开而不是返回整个结构，是为了让 journal 与 decision 两边都能
// 用同一组 helper，不必各自做 nil 判断。
func tokComposition(input ChatRecordInput) tokfp.Composition {
	if input.Tok == nil {
		return tokfp.Composition{}
	}
	return input.Tok.Composition
}

// tokEnabled 检查开关。关掉时所有 Tok* 都写零值 ——
// journal 与 decision 两侧用同一判据，避免一边有值一边没有。
func (s *Service) tokEnabled() bool {
	return s.TokFingerprintEnabled()
}

func tokChars(input ChatRecordInput) int     { return tokComposition(input).Chars }
func tokCJK(input ChatRecordInput) int       { return tokComposition(input).CJK }
func tokDigits(input ChatRecordInput) int    { return tokComposition(input).Digits }
func tokASCII(input ChatRecordInput) int     { return tokComposition(input).ASCII }
func tokBucket(input ChatRecordInput) string { return tokComposition(input).Bucket() }

// tokRatio 返回 prompt_tokens / 字符数（不可用时 0）。
//
// 只在「字符数够、且上游真的回了 prompt_tokens」时才非零：
// 短 prompt 的模板开销占比过高，比值会失真，这时宁可返回 0 也不给假信号。
func tokRatio(input ChatRecordInput) float64 {
	if input.Tok == nil || input.Tok.PromptTokens <= 0 {
		return 0
	}
	c := input.Tok.Composition
	if !c.Usable() {
		return 0
	}
	// 采样路径下 Ratio 没有意义：分母只是前缀字符数，分子却是全量
	// prompt_tokens，比值会被放大数倍（实测 0.31 的真实值算成 2.11）。
	// 返回 0 表示「未观测」，I9 会跳过 —— 宁可漏报，不可误报。
	if c.Sampled {
		return 0
	}
	return c.Ratio(input.Tok.PromptTokens)
}

func tokEnabledInt(s *Service, fn func(ChatRecordInput) int, input ChatRecordInput) int {
	if !s.tokEnabled() {
		return 0
	}
	return fn(input)
}

func tokEnabledStr(s *Service, fn func(ChatRecordInput) string, input ChatRecordInput) string {
	if !s.tokEnabled() {
		return ""
	}
	return fn(input)
}

func tokEnabledFloat(s *Service, fn func(ChatRecordInput) float64, input ChatRecordInput) float64 {
	if !s.tokEnabled() {
		return 0
	}
	return fn(input)
}

// ioSystemLen 取出站 system 的字节数（未观测时 0）。
func ioSystemLen(input ChatRecordInput) int {
	if input.IO == nil {
		return 0
	}
	return input.IO.OutSystemLen
}

// ioUsageSource 空值按 upstream 处理：老调用方不会填这个字段。
func ioUsageSource(input ChatRecordInput) string {
	if input.UsageSource != "" {
		return input.UsageSource
	}
	return "upstream"
}

func ioInt(io *provider.MessagesIO, pick func(*provider.MessagesIO) int) int {
	if io == nil {
		return 0
	}
	return pick(io)
}

// reasoningTokens 取出 completion_tokens_details.reasoning_tokens（没有则 0）。
func reasoningTokens(u provider.Usage) int {
	if u.CompletionTokensDetails == nil {
		return 0
	}
	return u.CompletionTokensDetails.ReasoningTokens
}

// recordWatchSample 把一次真实请求喂给波动监测器；未启用时是空操作。
func (s *Service) recordWatchSample(input ChatRecordInput, durationMs int64) {
	if s.Watch == nil || input.Canceled {
		return
	}
	var reasoning int64
	if d := input.Usage.CompletionTokensDetails; d != nil {
		reasoning = int64(d.ReasoningTokens)
	}
	s.Watch.Record(watch.Sample{
		At:              input.Started.UnixMilli(),
		Model:           input.Model,
		Ok:              input.Ok,
		ReasoningAsked:  input.ReasoningRequested,
		ReasoningTokens: reasoning,
		DurationMs:      durationMs,
	})
}

// UsageView returns admin usage summary and a page of request rows.
func (s *Service) UsageView(rangeName string, limit, offset int) usagejournal.View {
	return s.UsageQuery(usagejournal.Query{Range: rangeName, Limit: limit, Offset: offset})
}

// UsageQuery returns usage with optional account / model filters.
func (s *Service) UsageQuery(q usagejournal.Query) usagejournal.View {
	if s == nil || s.Journal == nil {
		return usagejournal.View{Summary: usagejournal.Summary{Range: q.Range}, Limit: q.Limit, Offset: q.Offset}
	}
	switch strings.ToLower(strings.TrimSpace(q.Range)) {
	case "week", "month":
		return s.Journal.Query(q)
	default:
		q.Range = "day"
		return s.Journal.Query(q)
	}
}
