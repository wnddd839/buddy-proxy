// Package decision 记录网关的决策流：请求、选号、额度变动、断言命中。
//
// 与 usagejournal 的分工：
//   - usagejournal 服务 UI（内存里只留最近 400 条明细），历史深度不够
//   - 本包按日追加 jsonl，无内存上限，供离线回放与「以前有没有发生过」
//
// 与 systemprompt 的分工：那边只记 system 侧处理结果，本包记整条决策链。
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
)

// 额度变更的操作类型。
const (
	OpDeduct  = "deduct"
	OpClear   = "clear"
	OpRefresh = "refresh"
	OpImport  = "import"
	OpReset   = "reset"
)

// 允许把余额清成 nil 的显式理由。
// 只有这里列出的值才被 I1 接受；自由字符串不算。
const (
	ReasonProbeExpired     = "probe_expired"
	ReasonAdminClear       = "admin_clear"
	ReasonAccountReimport  = "account_reimport"
	ReasonModelUnsupported = "model_unsupported"
)

// Record 是一条决策。四种 kind 共用一张扁平表：jsonl 好读、回放好过滤，
// 代价是字段多，但每行只有一个 kind 的字段非零。
type Record struct {
	Kind string `json:"kind"`
	At   int64  `json:"at"`

	// request
	RequestID        string   `json:"requestId,omitempty"`
	SessionKey       string   `json:"sessionKey,omitempty"`
	AccountID        string   `json:"accountId,omitempty"`
	Site             string   `json:"site,omitempty"`
	Model            string   `json:"model,omitempty"`
	RoutedModel      string   `json:"routedModel,omitempty"`
	PromptTokens     int64    `json:"promptTokens,omitempty"`
	CompletionTokens int64    `json:"completionTokens,omitempty"`
	UsageSource      string   `json:"usageSource,omitempty"`
	Canceled         bool     `json:"canceled,omitempty"`
	TitleRewrite     bool     `json:"titleRewrite,omitempty"`
	InMessages       int      `json:"inMessages,omitempty"`
	OutMessages      int      `json:"outMessages,omitempty"`
	InBytes          int      `json:"inBytes,omitempty"`
	OutBytes         int      `json:"outBytes,omitempty"`
	InSystemLen      int      `json:"inSystemLen,omitempty"`
	OutSystemLen     int      `json:"outSystemLen,omitempty"`
	SystemFp         string   `json:"systemFp,omitempty"`
	SystemForm       string   `json:"systemForm,omitempty"`
	Stripped         bool     `json:"stripped,omitempty"`
	StripScope       string   `json:"stripScope,omitempty"`
	Degraded         bool     `json:"degraded,omitempty"`
	Ok               bool     `json:"ok,omitempty"`
	ErrorCode        string   `json:"errorCode,omitempty"`
	ErrorText        string   `json:"errorText,omitempty"`
	Credit           *float64 `json:"credit,omitempty"`

	// selection
	Candidates int            `json:"candidates,omitempty"`
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

// AllowedClearReasons 是 I1 的白名单：只有这些理由允许把余额清成 nil。
var AllowedClearReasons = map[string]struct{}{
	ReasonProbeExpired:     {},
	ReasonAdminClear:       {},
	ReasonAccountReimport:  {},
	ReasonModelUnsupported: {},
}
