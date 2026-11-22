// Package invariant 持有「静默失效」断言清单与执行所需的有限状态。
//
// 一个断言 = 一个 Check 函数：吃一条 decision.Record + 当前状态，
// 吐零个或多个 Finding。同一份清单被两个执行器复用：
//   - 在线（Sink）：边跑边查，看未来
//   - 离线（Replay）：重跑历史文件，看过去
//
// 设计铁律：断言的是**数据**，不是代码。写成「这段代码应该怎样」只能测逻辑；
// 写成「这组数据不该长这样」才能测现实 —— dsh 那次上下文骤降，代码完全合法。
package invariant

import (
	"fmt"
	"sort"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 严重级别。info 只记面板，不进「需要人处理」的计数。
const (
	SeverityViolation = "violation"
	SeverityInfo      = "info"
)

// Check 检查一条记录。state 可为 nil（无状态断言）。
type Check func(rec decision.Record, state *State) []decision.Finding

// spec 是一条断言的元数据。why 是必需的：没有「违反即证明是 bug」的
// 一句话说明，断言会退化成噪音，所以注册时强制填写。
type spec struct {
	ID       string
	Title    string
	Why      string
	Severity string
	Check    Check
}

// Registry 是一组断言。Run 按注册顺序执行，结果顺序稳定。
type Registry struct {
	specs []spec
}

// NewRegistry 建一个空注册表。
func NewRegistry() *Registry {
	return &Registry{}
}

// Add 注册一条断言。why 为空时 panic —— 这是编译期无法捕获的约束，
// 用启动期崩溃把它变成必现问题，而不是让它悄悄退化成噪音。
func (r *Registry) Add(id, title, why, severity string, fn Check) {
	if why == "" {
		panic("invariant: " + id + " 缺少 why（违反即证明是 bug 的说明）")
	}
	if fn == nil {
		panic("invariant: " + id + " 没有 Check 函数")
	}
	if severity == "" {
		severity = SeverityViolation
	}
	r.specs = append(r.specs, spec{ID: id, Title: title, Why: why, Severity: severity, Check: fn})
}

// Specs 返回已注册断言的元数据（面板与文档用），按 ID 排序。
func (r *Registry) Specs() []map[string]string {
	out := make([]map[string]string, 0, len(r.specs))
	for _, s := range r.specs {
		out = append(out, map[string]string{
			"id": s.ID, "title": s.Title, "why": s.Why, "severity": s.Severity,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"] < out[j]["id"] })
	return out
}

// Run 对一条记录执行全部断言。
//
// 单条断言 panic 不得影响请求路径：recover 后转成一条 violation，
// 把「断言自己坏了」也变成可见的问题，而不是静默吞掉。
func (r *Registry) Run(rec decision.Record, state *State) []decision.Finding {
	if r == nil {
		return nil
	}
	var out []decision.Finding
	for _, s := range r.specs {
		out = append(out, runOne(s, rec, state)...)
	}
	return out
}

func runOne(s spec, rec decision.Record, state *State) (out []decision.Finding) {
	defer func() {
		if r := recover(); r != nil {
			out = append(out, decision.Finding{
				Invariant: s.ID,
				Severity:  SeverityViolation,
				Subject:   "invariant-panic",
				At:        rec.At,
				Detail:    fmt.Sprintf("断言自身 panic：%v", r),
				Why:       "断言崩溃意味着这一类检查在当前请求上完全没跑，等于回到「没有机制」的状态",
			})
		}
	}()
	findings := s.Check(rec, state)
	for i := range findings {
		if findings[i].Invariant == "" {
			findings[i].Invariant = s.ID
		}
		if findings[i].Severity == "" {
			findings[i].Severity = s.Severity
		}
		if findings[i].At == 0 {
			findings[i].At = rec.At
		}
		if findings[i].Why == "" {
			findings[i].Why = s.Why
		}
	}
	return findings
}

// DefaultRegistry 注册 v1 的全部断言。
func DefaultRegistry() *Registry {
	r := NewRegistry()
	registerQuota(r)
	registerIO(r)
	registerSession(r)
	registerSelection(r)
	registerModel(r)
	return r
}
