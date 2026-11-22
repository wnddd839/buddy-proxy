package tokfp

// 官方 DeepSeek-V3 词表的参考比例。
//
// ## 来源
//
// 下载 `deepseek-ai/DeepSeek-V3/tokenizer.json`（7.8MB，经 hf-mirror），
// 用 Python `tokenizers` 库在纯样本上标定（脚本 gen_calibration.py）。
//
// ## 为什么 Go 里不直接跑 BPE
//
// 试过两个 Go 库，都不行：
//
//   - github.com/daulet/tokenizers（Rust 绑定）
//     需要预编译 libtokenizers.so，本机没有 Rust 工具链，链接失败。
//
//   - github.com/sugarme/tokenizer（纯 Go）
//     DeepSeek 的 pre-tokenizer 正则含 `(?!\S)` 负向前瞻，
//     Go 的 regexp（RE2）不支持 lookahead → panic。
//     剥掉那段正则能让程序跑起来，但 tokenization 语义就变了，
//     对「精确计数」不可接受。
//
// 所以网关内**不做** BPE；精确鉴定靠 tokfp.CalibrationSamples
// （由官方词表预生成的常量）在运行时与上游实测值对比。
const (
	// ReferenceCJK 是纯中文样本的 token/字符比。
	ReferenceCJK = 0.636364
	// ReferenceDigits 是纯数字样本的比。
	ReferenceDigits = 0.454545
	// ReferenceAlpha 是纯英文字母（含空格）的比。
	ReferenceAlpha = 0.204697
	// ReferenceSym 是 ASCII 符号的比。
	ReferenceSym = 0.678571
	// ReferenceOther 是全角标点 / emoji 等非 ASCII 非 CJK 字符的比。
	ReferenceOther = 0.823824
)

// TokenTemplateOverhead 是 chat template 的基础开销（常数项 3）。
//
// 上游 prompt_tokens = 官方 tokenizer 计数 + 模板开销，其中：
//
//	delta = 3 + #system + #user + 3*(每个 assistant 段的首条)
//	                         + 1*(同段内的后续 assistant)
//
// 证据（9 种结构全部吻合，见 TemplateCases）：
//
//	1u            3+0+1+0        = 4   实测 4
//	2u            3+0+2+0        = 5   实测 5
//	3u            3+0+3+0        = 6   实测 6
//	u_a           3+0+1+3        = 7   实测 7
//	u_a_u         3+0+2+3        = 8   实测 8
//	s_u           3+1+1+0        = 5   实测 5
//	u_a_u_a       3+0+2+3+3      = 11  实测 11
//	u_a_a_u       3+0+2+3+1      = 9   实测 9
//	u_a_a_a_u     3+0+2+3+1+1    = 10  实测 10
//	u_a_u_a_u_a_u 3+0+4+3+3+3    = 16  实测 16
//
// 内容长度完全不影响 delta（u_a_u 官方 3 → 8；同结构换成 722 token 内容仍是 8）。
const TokenTemplateOverhead = 3

// CompositionEstimatedTokens 用参考权重把成分转成「官方 tokenizer 会切出多少 token」。
//
// 实测精度（混合样本与官方精确计数对比）：
//
//	zh+code   5.00%
//	en+code   7.26%  ← 最差（英文散文与代码同属 ASCII，权重被平均）
//	zh+nums   1.15%
//	zh+en     1.35%
//	code+zh   1.65%
//	json+zh   1.16%
//
// 只适合抓大偏差（例如 glm 的数字样本 +32%），不适合判定细微差异。
func (c Composition) EstimatedTokens() float64 {
	return float64(c.CJK)*ReferenceCJK +
		float64(c.Digits)*ReferenceDigits +
		float64(c.ASCII)*ReferenceAlpha +
		float64(c.Other)*ReferenceOther
}
