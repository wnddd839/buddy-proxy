package modelroute

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wnddd839/codebuddy-proxy/internal/atomicwrite"
)

// Rule 是一条**有方向**的模型路由：
//
//	FromSite 站点收到 Match 模型名的请求时，改写到 Site 站点的 Target 模型执行。
//
// 方向必须显式写明，两个方向各配各的：
//
//	{"fromSite":"domestic","match":"gpt-5.6","site":"global","target":"gpt-5.6"}   // 国内 → 国际
//	{"fromSite":"global","match":"glm-5.3","site":"domestic","target":"glm-5.3"}   // 国际 → 国内
//
// FromSite 必填：只有「请求本来就落在该站点」时才改写。这样客户端用
// `cn:` / `global:` 前缀或 X-Site 头显式指定站点的请求，不会被另一个方向的
// 规则劫走——它只会命中「源站点 = 自己指定的站点」的那条规则。
//
// 匹配顺序 = 数组顺序；同 FromSite + 同 Match 的多条 = 备用链。
type Rule struct {
	FromSite string `json:"fromSite"` // 源站点：domestic / global（必填）
	Match    string `json:"match"`    // 源模型名（客户端请求的，精确匹配，不区分大小写）
	// Mode 决定这条规则「什么时候生效」：
	//   always      —— 命中就改写（默认，空值同义）
	//   onExhausted —— 先按源站点原模型正常选号，只有源站点用尽时才改写
	Mode   string `json:"mode,omitempty"`
	Site   string `json:"site"`           // 目标站点：domestic / global（必填）
	Target string `json:"target"`         // 目标上游模型名
	Note   string `json:"note,omitempty"` // 备注，仅展示
}

// 路由生效时机。
const (
	ModeAlways      = "always"
	ModeOnExhausted = "onExhausted"
)

// normalizeMode 归一 mode 字面量；第二个返回值表示是否合法。
// 比较必须大小写不敏感：配置文件是手写/前端写入的，别要求用户记住驼峰。
func normalizeMode(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ModeAlways:
		return ModeAlways, true
	case strings.ToLower(ModeOnExhausted):
		return ModeOnExhausted, true
	}
	return "", false
}

// ModeOrDefault 返回归一后的生效时机；空值按 always（向后兼容老规则）。
func (r Rule) ModeOrDefault() string {
	if mode, ok := normalizeMode(r.Mode); ok {
		return mode
	}
	return ModeAlways
}

// OnExhausted 报告这条规则是否要等到源站点用尽才改写。
func (r Rule) OnExhausted() bool { return r.ModeOrDefault() == ModeOnExhausted }

type Store struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

// File 是带锁的路由表文件。文件不存在视为空表。
type File struct {
	path string
	mu   sync.Mutex
}

func Open(path string) *File { return &File{path: path} }

func (f *File) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

func (f *File) Read() (Store, error) {
	if f == nil {
		return Store{Version: 1}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: 1, Rules: []Rule{}}, nil
		}
		return Store{}, err
	}
	var store Store
	if err := json.Unmarshal(raw, &store); err != nil {
		return Store{}, fmt.Errorf("modelroute: 解析 %s 失败: %w", f.path, err)
	}
	if store.Version == 0 {
		store.Version = 1
	}
	if store.Rules == nil {
		store.Rules = []Rule{}
	}
	return store, nil
}

func (f *File) Write(store Store) error {
	if err := Validate(store.Rules); err != nil {
		return err
	}
	store.Version = 1
	if store.Rules == nil {
		store.Rules = []Rule{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return atomicwrite.Write(f.path, append(raw, '\n'), 0o600)
}

// Validate 校验规则集：源站点/目标站点必须合法、match 非空、target 非空，
// 同 fromSite+match+site 只能有一条（避免同一跳重复）。
func Validate(rules []Rule) error {
	// 同 fromSite+match 允许多条（备用链，各跳站点不同），但同跳站点只能一条。
	pairSeen := make(map[string]struct{}, len(rules))
	for i, rule := range rules {
		match := strings.ToLower(strings.TrimSpace(rule.Match))
		if match == "" {
			return fmt.Errorf("modelroute: 第 %d 条缺少 match", i+1)
		}
		from := strings.ToLower(strings.TrimSpace(rule.FromSite))
		if from == "" {
			return fmt.Errorf("modelroute: 模型 %q 缺少 fromSite（源站点必须写明国内或国际）", rule.Match)
		}
		if !isKnownSite(from) {
			return fmt.Errorf("modelroute: 模型 %q 的 fromSite %q 无效（只能是 domestic 或 global）", rule.Match, rule.FromSite)
		}
		site := strings.ToLower(strings.TrimSpace(rule.Site))
		if site == "" {
			return fmt.Errorf("modelroute: 模型 %q 缺少 site（目标站点必须写明国内或国际）", rule.Match)
		}
		if !isKnownSite(site) {
			return fmt.Errorf("modelroute: 模型 %q 的 site %q 无效（只能是 domestic 或 global）", rule.Match, rule.Site)
		}
		pair := canonicalSite(from) + "|" + match + "|" + canonicalSite(site)
		if _, dup := pairSeen[pair]; dup {
			return fmt.Errorf("modelroute: %q → %q 这条路由重复配置了", rule.Match, rule.Site)
		}
		pairSeen[pair] = struct{}{}
		if strings.TrimSpace(rule.Target) == "" {
			return fmt.Errorf("modelroute: 模型 %q 缺少 target", rule.Match)
		}
		if _, ok := normalizeMode(rule.Mode); !ok {
			return fmt.Errorf("modelroute: 模型 %q 的 mode %q 无效（只能是 always 或 onExhausted）", rule.Match, rule.Mode)
		}
	}
	return nil
}

// isKnownSite 只认 domestic / global 及其常见别名。
func isKnownSite(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "domestic", "cn", "china", "internal", "global", "intl", "international":
		return true
	}
	return false
}

// canonicalSite 把站点别名归一成 domestic / global。
func canonicalSite(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "domestic", "cn", "china", "internal":
		return "domestic"
	default:
		return "global"
	}
}

// Chain 返回「当前请求落在 fromSite 站点、模型名为 model」时的完整路由链（按文件顺序）。
//
// 一条 = 固定路由；多条 = 备用链：链首按 Mode 决定「立即改写」还是
// 「源站点用尽才改写」，其余各跳都在源站点用尽后按顺序推进。大小写不敏感。
//
// fromSite 是这次请求实际要去的站点（已由前缀 / X-Site / Key 绑定 / 全局默认决定）。
// 只返回 FromSite 与它一致的规则 —— 方向不同的一律不命中，这是「客户端显式指定
// 站点绝不被改写」的保证。
func (f *File) Chain(fromSite, model string) []Rule {
	if f == nil {
		return nil
	}
	cleaned := strings.TrimSpace(model)
	if cleaned == "" || hasSitePrefix(cleaned) {
		return nil
	}
	store, err := f.Read()
	if err != nil {
		return nil
	}
	key := strings.ToLower(cleaned)
	want := canonicalSite(fromSite)
	var out []Rule
	for _, rule := range store.Rules {
		if !strings.EqualFold(strings.TrimSpace(rule.Match), key) {
			continue
		}
		// 缺 fromSite 的规则一律不生效（老格式无方向）。绝不能把空值
		// 归一成 global —— 那会让一条没有方向的旧规则悄悄劫走国际站请求。
		if strings.TrimSpace(rule.FromSite) == "" {
			continue
		}
		if canonicalSite(rule.FromSite) != want {
			continue
		}
		out = append(out, rule)
	}
	return out
}

// Resolve 是 Chain 的单值兼容口：返回链首。
func (f *File) Resolve(fromSite, model string) (Rule, bool) {
	chain := f.Chain(fromSite, model)
	if len(chain) == 0 {
		return Rule{}, false
	}
	return chain[0], true
}

// AuditLine 把整张路由表压成一行，供审计日志记录。
// 格式：domestic:gpt-5.6→global/gpt-5.6, global:glm-5.3→domestic/glm-5.3
func AuditLine(rules []Rule) string {
	if len(rules) == 0 {
		return "(empty)"
	}
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		line := fmt.Sprintf("%s:%s→%s/%s",
			canonicalSite(r.FromSite), strings.TrimSpace(r.Match),
			canonicalSite(r.Site), strings.TrimSpace(r.Target))
		if r.OnExhausted() {
			line = "[" + ModeOnExhausted + "]" + line
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, ", ")
}

// hasSitePrefix 判断模型名是否带 cn:/global: 之类站点前缀。
// 与 config.SplitSitePrefix 同口径，但不引包避免循环依赖。
func hasSitePrefix(model string) bool {
	key, _, found := strings.Cut(model, ":")
	if !found {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "cn", "domestic", "china", "internal", "global", "intl", "international":
		return true
	}
	return false
}
