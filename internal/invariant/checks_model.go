package invariant

import (
	"fmt"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 模型偷换检测 I8。
//
// 与其它断言的区别：这条**不改请求行为**，只记录事实 ——
// 上游说它跑了哪个模型，与我们发出去的对比。
//
// 之所以重要：一旦模型被替换，所有基于「这个模型在想什么」的
// 分析（思考 token 占比、档位效果、缓存命中率）全部失效，
// 而且失效得悄无声息。
func registerModel(r *Registry) {
	r.Add("I8", "上游跑的就是我们发的模型",
		"模型被替换后，所有按模型做的分析（思考占比/档位/缓存）全部失效，而且不会有任何报错",
		SeverityViolation, checkModelSubstitution)
	r.Add("I8b", "上游回显的模型（观测）",
		"只记录上游声明跑了哪个模型；auto 一族由上游解析，不算替换",
		SeverityInfo, checkModelEcho)
}

// modelNameNormalized 去掉大小写与分隔符差异。
//
// 实测：我们发 hy4flash-free，上游回显 hy4-flash-free —— 只差一个连字符，
// 是同一个模型。直接相等比较会把它当成偷换。
func modelNameNormalized(name string) string {
	var b []rune
	for _, r := range name {
		switch r {
		case '-', '_', '.', ' ', '/', ':':
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b = append(b, r)
	}
	return string(b)
}

// isAutoModel：auto / auto-* 由上游选模型，不判偷换。
func isAutoModel(name string) bool {
	return name == "auto" || len(name) > 5 && name[:5] == "auto-"
}

// checkModelSubstitution 判定「上游真的跑了另一个模型」。
func checkModelSubstitution(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled {
		return nil
	}
	upstream := rec.UpstreamModel
	if upstream == "" {
		return nil // 未观测
	}
	sent := rec.RoutedModel
	if sent == "" {
		sent = rec.Model
	}
	if sent == "" || isAutoModel(sent) {
		return nil
	}
	if modelNameNormalized(sent) == modelNameNormalized(upstream) {
		return nil
	}
	return []decision.Finding{{
		Subject: rec.RequestID,
		Detail:  fmt.Sprintf("发出去 %q，上游回显 %q（模型被替换）", sent, upstream),
		Evidence: map[string]any{
			"requested":     rec.Model,
			"routedModel":   rec.RoutedModel,
			"upstreamModel": upstream,
			"accountId":     rec.AccountID,
		},
	}}
}

// checkModelEcho 只记录「auto 一族被上游解析成了什么」，供面板展示。
func checkModelEcho(rec decision.Record, _ *State) []decision.Finding {
	if rec.Kind != decision.KindRequest || !rec.Ok || rec.Canceled {
		return nil
	}
	if rec.UpstreamModel == "" {
		return nil
	}
	sent := rec.RoutedModel
	if sent == "" {
		sent = rec.Model
	}
	if !isAutoModel(sent) {
		return nil // 非 auto 的对照在 I8 里做
	}
	return []decision.Finding{{
		Subject: rec.RequestID,
		Detail:  fmt.Sprintf("请求 %q 被上游解析为 %q", sent, rec.UpstreamModel),
		Evidence: map[string]any{
			"requested": sent, "upstreamModel": rec.UpstreamModel,
		},
	}}
}
