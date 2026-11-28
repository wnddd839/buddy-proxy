package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/tokfp"
)

// TokIdentityResult 是一次词表身份鉴定的结果。
//
// Vocabulary 是「词表是不是官方 DeepSeek」，Weight 恒为「无法验证」——
// 因为上游不返回 logprobs（实测三个模型都没有），而词表相同不能证明
// 权重相同（蒸馏/微调模型可以继承词表）。
type TokIdentityResult struct {
	Model      string              `json:"model"`
	Site       string              `json:"site"`
	AccountID  string              `json:"accountId"`
	Samples    []toxIdentitySample `json:"samples"`
	Vocabulary string              `json:"vocabulary"`
	Weight     string              `json:"weight"`
	TemplateOK bool                `json:"templateOK"`
	// TemplateDetail 逐结构记录「观测 vs 预测」，模板不符时能直接看到差在哪。
	TemplateDetail []TokTemplateCase `json:"templateDetail,omitempty"`
	Note           string            `json:"note,omitempty"`
	Cost           string            `json:"cost"`
}

// TokTemplateCase 是一种消息结构的模板校验明细。
type TokTemplateCase struct {
	Name      string `json:"name"`
	Observed  int    `json:"observed"`
	Predicted int    `json:"predicted"`
	OK        bool   `json:"ok"`
}

type toxIdentitySample struct {
	Name     string `json:"name"`
	Observed int    `json:"observed"`
	Official int    `json:"official"`
	Delta    int    `json:"delta"`
	Verdict  string `json:"verdict"`
}

// TokIdentityCheck 对指定模型跑一次词表身份鉴定。
//
// ## 成本控制
//
// 3 条标定样本 + 4 种模板结构，每条约 1 token 输出。总计约 7 次请求，
// 实测约 0.02 credit。所以只做手动触发，不做定时。
//
// ## 为什么需要 4 种模板结构
//
// 单条样本只能验证「词表 + 模板」的组合。模板结构变化时 delta 会变，
// 若只测一种结构，就无法区分「词表不同」与「模板不同」。
// 所以必须同时验证模板公式（见 tokfp.TemplateCases）。
func (s *Service) TokIdentityCheck(ctx context.Context, site, model string) (TokIdentityResult, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return TokIdentityResult{}, fmt.Errorf("需要 model")
	}
	out := TokIdentityResult{
		Model:  model,
		Site:   site,
		Weight: "无法验证（上游不提供 logprobs）",
		Cost:   "约 0.02 credit（7 次请求，每次 max_tokens=1）",
	}

	sel, err := s.Pool.Select(accounts.SelectOptions{
		Site: site, Model: model, PreferQuota: true,
	})
	if err != nil {
		return out, err
	}
	out.AccountID = sel.Account.ID
	account := sel.Account

	// ── 1. 三条词表标定样本 ────────────────────────────
	observed := make([]int, 0, len(tokfp.CalibrationSamples))
	for _, sample := range tokfp.CalibrationSamples {
		usage, err := s.probePromptTokens(ctx, account, site, model, []provider.ChatOptions{
			{Messages: []map[string]any{{"role": "user", "content": sample.Text()}}, MaxCompletionTokens: 1},
		}[0])
		if err != nil {
			out.Samples = append(out.Samples, toxIdentitySample{
				Name: sample.Name, Official: sample.OfficialTokens,
				Verdict: tokfp.VerdictUnknown.String(),
			})
			continue
		}
		pt := int(usage.PromptTokens)
		observed = append(observed, pt)
		out.Samples = append(out.Samples, toxIdentitySample{
			Name:     sample.Name,
			Observed: pt,
			Official: sample.OfficialTokens,
			Delta:    pt - sample.OfficialTokens,
		})
	}

	// ── 2. 判定 ────────────────────────────────────────
	report := tokfp.AnalyzeIdentity(model, observed)
	out.Vocabulary = report.VocabularyLabel()
	out.Note = report.Note
	// 把逐样本判定回填（AnalyzeIdentity 里有更细的判断）
	for i := range report.Samples {
		for j := range out.Samples {
			if out.Samples[j].Name == report.Samples[i].Name {
				out.Samples[j].Verdict = report.Samples[i].Verdict.String()
			}
		}
	}

	// ── 3. 模板公式校验 ────────────────────────────────
	// 只在词表判定通过时做：词表都不对，模板校验没有意义。
	if report.Vocabulary == tokfp.VerdictPass {
		out.TemplateOK, out.TemplateDetail = s.checkTemplateFormula(ctx, account, site, model)
	}

	return out, nil
}

// probePromptTokens 发一次最小请求，取回 prompt_tokens。
//
// 不记录到 usage journal：标定请求不是真实业务流量，
// 混进去会污染用量统计。
func (s *Service) probePromptTokens(ctx context.Context, account accounts.Account, site, model string, opts provider.ChatOptions) (provider.Usage, error) {
	chatOpts := s.chatOptionsFromAccount(account, CompleteOptions{
		Model: model, Site: site,
	})
	chatOpts.Messages = opts.Messages
	chatOpts.MaxCompletionTokens = opts.MaxCompletionTokens
	chatOpts.Stream = false
	chatOpts.Site = site

	// 关键：标定请求必须绕过网关自身的消息变换。
	//
	// 实测事故（2026-11-22）：线上 echo 开关开着
	// （CODEBUDDY_PROXY_SYSTEM_ECHO_USER=1），于是 s_u 结构里
	// system 被复制进 user 并用 <client_instructions> 包裹，
	// 上游返回 19 而直连只有 7 —— 多出的 12 个 token 全是网关自己加的，
	// 却被算进「模板开销」，导致公式校验误判为不符。
	//
	// 这里量的是**上游本身**，不是「网关 + 上游」的组合。
	chatOpts.EchoSystemToUser = false

	res, err := s.Provider.Complete(ctx, chatOpts)
	if err != nil {
		return provider.Usage{}, err
	}
	return res.Turn.Usage, nil
}

// checkTemplateFormula 用四种结构验证模板开销公式，返回逐结构明细。
//
// 返回明细而不是 bool：模板不符时「差在哪个结构、差多少」才是可用信息，
// 一个 false 无法判断是公式错了还是上游模板变了。
func (s *Service) checkTemplateFormula(ctx context.Context, account accounts.Account, site, model string) (bool, []TokTemplateCase) {
	all := true
	details := make([]TokTemplateCase, 0, len(tokfp.TemplateCases))
	for _, tc := range tokfp.TemplateCases {
		msgs := make([]map[string]any, 0, len(tc.Messages))
		official := 0
		for _, m := range tc.Messages {
			msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
			if m.Content != "" {
				official += tokfp.TemplateProbeOfficialTokens
			}
		}
		predicted := official + tc.ExpectedOverhead
		usage, err := s.probePromptTokens(ctx, account, site, model, provider.ChatOptions{
			Messages: msgs, MaxCompletionTokens: 1,
		})
		if err != nil {
			details = append(details, TokTemplateCase{
				Name: tc.Name, Predicted: predicted,
			})
			all = false
			continue
		}
		observed := int(usage.PromptTokens)
		ok := observed == predicted
		if !ok {
			all = false
		}
		details = append(details, TokTemplateCase{
			Name: tc.Name, Observed: observed,
			Predicted: predicted, OK: ok,
		})
	}
	return all, details
}
