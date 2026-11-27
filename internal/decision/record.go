package decision

// Kind 取值。
const (
	KindRequest   = "request"
	KindSelection = "selection"
	KindQuota     = "quota"
	KindFinding   = "finding"
)

// UsageSource 取值：只有 actual 参与上下文环比，estimated 不可比。
const (
	UsageActual    = "actual"
	UsageEstimated = "estimated"
	// UsageUpstream 与 actual 同义，保留为上游原始字面值。
	UsageUpstream = "upstream"
)

// 额度变更的操作类型。
const (
	OpDeduct  = "deduct"
	OpClear   = "clear"
	OpRefresh = "refresh"
	OpImport  = "import"
	OpReset   = "reset"
)

// 扣减被跳过的原因（此时 After == Before，额度没动）。
//
// 单列出来的理由：只有把「为什么没扣」也记下来，I2a 才能区分
// 「额度未知/不限量，扣不了」与「上游明明收费了，扣减路径却没跑」。
const (
	DeductSkipQuotaUnknown = "quota_unknown"
	DeductSkipUnlimited    = "unlimited"
	DeductSkipNoCredit     = "no_credit"
)

// 允许把余额清成 nil 的显式理由。
//
// 这里是**唯一来源**：internal/accounts 里的 ClearReason* 常量必须与之一致，
// 由 crosscheck_test.go 强制（历史上这两套没同步，导致 I1 对每次合法清空都误报）。
const (
	ReasonQuotaRecovered  = "quota_recovered"  // 付费成功证明旧「耗尽」快照已过期
	ReasonInvalidSnapshot = "invalid_snapshot" // 上游回包 NaN/Inf，只能清空
	ReasonImport          = "import"           // 账号导入/覆盖
	ReasonAdminClear      = "admin_clear"      // 管理台手动清空（暂无调用点，预留）
)

// AllowedClearReasons 是 I1 的白名单。
var AllowedClearReasons = map[string]struct{}{
	ReasonQuotaRecovered:  {},
	ReasonInvalidSnapshot: {},
	ReasonImport:          {},
	ReasonAdminClear:      {},
}

// Record 是一条决策。四种 kind 共用一张扁平表：jsonl 好读、回放好过滤，
// 代价是字段多，但每行只有一个 kind 的字段非零。
type Record struct {
	Kind string `json:"kind"`
	At   int64  `json:"at"`

	// request
	RequestID   string `json:"requestId,omitempty"`
	SessionKey  string `json:"sessionKey,omitempty"`
	AccountID   string `json:"accountId,omitempty"`
	Site        string `json:"site,omitempty"`
	Model       string `json:"model,omitempty"`
	RoutedModel string `json:"routedModel,omitempty"`
	// 分词器指纹（见 internal/tokfp）。与上游回显互补：
	// 回显是「它说的」，比例是「它实际用的分词器」。
	TokChars  int     `json:"tokChars,omitempty"`
	TokCJK    int     `json:"tokCJK,omitempty"`
	TokDigits int     `json:"tokDigits,omitempty"`
	TokASCII  int     `json:"tokASCII,omitempty"`
	TokBucket string  `json:"tokBucket,omitempty"`
	TokRatio  float64 `json:"tokRatio,omitempty"`
	// HasTools / SystemEchoed 让 I10/I11 跳过「分子含额外 token」的请求。
	HasTools     bool `json:"hasTools,omitempty"`
	SystemEchoed bool `json:"systemEchoed,omitempty"`
	// OutObserved 表示这次请求真的发往了上游（出站统计已产生）。
	// I3/I4 靠它区分「没发出去」与「发出去了但丢光了」。
	OutObserved bool `json:"outObserved,omitempty"`

	// UpstreamModel 是上游 SSE 回显的模型名（空 = 未观测）。
	// 与 Model/RoutedModel 三者并列：客户端要的 / 我们发的 / 上游说它跑的。
	UpstreamModel    string `json:"upstreamModel,omitempty"`
	PromptTokens     int64  `json:"promptTokens,omitempty"`
	CompletionTokens int64  `json:"completionTokens,omitempty"`
	// ReasoningTokens 是 completion_tokens 里用于思考的那部分（DeepSeek 思考模式）。
	//
	// 单列的理由：它计入 completion_tokens，所以只看总量无法区分
	// 「模型想了很多但答得短」与「模型压根没想」—— 这两件事对用户
	// 是完全不同的体验（后者就是「逐渐不思考、废话变多」）。
	ReasoningTokens int64 `json:"reasoningTokens,omitempty"`
	// ReasoningTier 是收敛后**上游实际生效**的档位（low/high/max；空 = 未声明）。
	//
	// 与 UsageRecord 的 ReasoningEffort（客户端原值）分开：未知档位会被
	// 网关回落成 high，只看原值无法判断「客户端到底有没有要求思考」。
	ReasoningTier string `json:"reasoningTier,omitempty"`
	UsageSource   string `json:"usageSource,omitempty"`
	Canceled      bool   `json:"canceled,omitempty"`
	TitleRewrite  bool   `json:"titleRewrite,omitempty"`
	InMessages    int    `json:"inMessages,omitempty"`
	OutMessages   int    `json:"outMessages,omitempty"`
	InBytes       int    `json:"inBytes,omitempty"`
	OutBytes      int    `json:"outBytes,omitempty"`
	InSystemLen   int    `json:"inSystemLen,omitempty"`
	OutSystemLen  int    `json:"outSystemLen,omitempty"`
	SystemFp      string `json:"systemFp,omitempty"`
	SystemForm    string `json:"systemForm,omitempty"`
	Stripped      bool   `json:"stripped,omitempty"`
	StripScope    string `json:"stripScope,omitempty"`
	Degraded      bool   `json:"degraded,omitempty"`
	Ok            bool   `json:"ok,omitempty"`
	ErrorCode     string `json:"errorCode,omitempty"`
	// ErrorKind 是粗分类（canceled / coded / empty / unclassified）。
	// 与 ErrorCode 分开：I5 只关心「有原文但没有码」，需要把
	// 取消、空回包、有码这几种情况摘出去。
	ErrorKind string   `json:"errorKind,omitempty"`
	ErrorText string   `json:"errorText,omitempty"`
	Credit    *float64 `json:"credit,omitempty"`

	// selection
	// SelSource 是选号路径：pin / prefer_quota / rotation。
	SelSource  string         `json:"selSource,omitempty"`
	Candidates int            `json:"candidates,omitempty"`
	Examined   int            `json:"examined,omitempty"`
	Chosen     string         `json:"chosen,omitempty"`
	Skipped    map[string]int `json:"skipped,omitempty"`

	// quota
	Op              string   `json:"op,omitempty"`
	Before          *float64 `json:"before,omitempty"`
	After           *float64 `json:"after,omitempty"`
	SpentSinceCheck float64  `json:"spentSinceCheck,omitempty"`
	CheckedAtBefore int64    `json:"checkedAtBefore,omitempty"`
	CheckedAtAfter  int64    `json:"checkedAtAfter,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	// SkipReason 非空表示这次扣减**被跳过**，值说明原因（DeductSkip*）。
	SkipReason string `json:"skipReason,omitempty"`

	// finding
	Finding *Finding `json:"finding,omitempty"`
}

// Finding 是一条断言命中。
type Finding struct {
	Invariant  string         `json:"invariant"`
	Severity   string         `json:"severity"`
	Subject    string         `json:"subject"`
	At         int64          `json:"at"`
	Detail     string         `json:"detail"`
	Why        string         `json:"why,omitempty"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	Suppressed int            `json:"suppressed,omitempty"`
}
