package invariant

import (
	"fmt"
	"sort"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// ReplayReport 是一次离线回放的结果。
type ReplayReport struct {
	From     int64         `json:"from"`
	To       int64         `json:"to"`
	Scanned  int           `json:"scanned"`
	Coverage string        `json:"coverage"`
	Groups   []ReplayGroup `json:"groups"`
}

// ReplayGroup 是同一断言 + 同一对象的一组命中。
type ReplayGroup struct {
	Invariant string `json:"invariant"`
	Severity  string `json:"severity"`
	Subject   string `json:"subject"`
	Count     int    `json:"count"`
	FirstAt   int64  `json:"firstAt"`
	LastAt    int64  `json:"lastAt"`
	Detail    string `json:"detail"`
	Why       string `json:"why,omitempty"`
}

// Replay 用给定注册表重跑历史记录。
//
// 重要：这里**重跑断言**，而不是读取当时落盘的结论 ——
// 只有重跑才能回答「新加一条断言，历史上有没有中过」，
// 也才能让断言改进后回溯旧数据。Kind=finding 的记录被跳过（那是输出，不是输入）。
//
// only 非空时只保留这些断言的命中。
func Replay(store *decision.Store, from, to time.Time, only []string) (ReplayReport, error) {
	report := ReplayReport{From: from.UnixMilli(), To: to.UnixMilli()}
	if store == nil {
		report.Coverage = "未指定决策日志目录；无法回放"
		return report, nil
	}

	recs, err := store.ReadKind(from, to, "") // 空 kind = 全部
	if err != nil {
		return report, err
	}

	allow := make(map[string]struct{}, len(only))
	for _, id := range only {
		allow[id] = struct{}{}
	}

	// 离线用不限制容量的状态：一次跑完一整段历史，淘汰逻辑会让跨天状态断掉。
	state := NewState(0, 0)
	state.SetDeductCap(0)
	reg := DefaultRegistry()

	groups := make(map[string]*ReplayGroup)
	var order []string

	first, last := int64(0), int64(0)
	for _, rec := range recs {
		if rec.Kind == decision.KindFinding {
			continue
		}
		report.Scanned++
		if first == 0 || rec.At < first {
			first = rec.At
		}
		if rec.At > last {
			last = rec.At
		}
		state.PrimeFromRecord(rec)
		for _, f := range reg.Run(rec, state) {
			if len(allow) > 0 {
				if _, ok := allow[f.Invariant]; !ok {
					continue
				}
			}
			key := f.Invariant + "\x00" + f.Subject
			g, ok := groups[key]
			if !ok {
				g = &ReplayGroup{
					Invariant: f.Invariant, Severity: f.Severity,
					Subject: f.Subject, FirstAt: f.At, LastAt: f.At,
					Detail: f.Detail, Why: f.Why,
				}
				groups[key] = g
				order = append(order, key)
			}
			g.Count++
			if f.At < g.FirstAt {
				g.FirstAt = f.At
			}
			if f.At > g.LastAt {
				g.LastAt = f.At
				g.Detail = f.Detail // 保留最近一条的说明
			}
		}
	}

	for _, k := range order {
		report.Groups = append(report.Groups, *groups[k])
	}
	sort.Slice(report.Groups, func(i, j int) bool {
		if report.Groups[i].Count != report.Groups[j].Count {
			return report.Groups[i].Count > report.Groups[j].Count
		}
		return report.Groups[i].Invariant < report.Groups[j].Invariant
	})

	report.Coverage = coverageText(report.Scanned, first, last, store)
	return report, nil
}

// coverageText 说明这次回放实际能看见多少。
//
// 必须如实：决策日志是这套机制上线后才开始写的，更早的历史
// **确实看不见** —— 声称覆盖了没读的东西，会让「以前没发生过」
// 变成一句无法验证的话，正是本体系要消灭的那种误导。
func coverageText(scanned int, firstAt, lastAt int64, store *decision.Store) string {
	since := "(未知)"
	if store != nil {
		if d := store.EarliestDate(); d != "" {
			since = d[:4] + "-" + d[4:6] + "-" + d[6:]
		}
	}
	if scanned == 0 {
		return fmt.Sprintf("目录 %s 内没有可回放的决策记录（决策日志起于 %s）；更早历史无法回放",
			dirOf(store), since)
	}
	return fmt.Sprintf("已回放 %d 条决策记录（%s ~ %s）；决策日志起于 %s，更早历史不在覆盖范围内",
		scanned, formatMillis(firstAt), formatMillis(lastAt), since)
}

func dirOf(store *decision.Store) string {
	if store == nil {
		return "(无)"
	}
	return store.Dir()
}

func formatMillis(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).In(time.Local).Format("2006-01-02 15:04")
}

// FormatReplay 把报告渲染成人类可读的表格。
func FormatReplay(r ReplayReport) string {
	out := ""
	if len(r.Groups) == 0 {
		out = "未命中任何不变量。\n"
	} else {
		out = fmt.Sprintf("%-6s %-10s %8s  %-34s %s\n", "ID", "严重度", "次数", "对象", "最近一次说明")
		for _, g := range r.Groups {
			subject := g.Subject
			if len(subject) > 34 {
				subject = subject[:31] + "..."
			}
			out += fmt.Sprintf("%-6s %-10s %8d  %-34s %s\n", g.Invariant, g.Severity, g.Count, subject, g.Detail)
		}
	}
	out += "\n" + r.Coverage + "\n"
	return out
}
