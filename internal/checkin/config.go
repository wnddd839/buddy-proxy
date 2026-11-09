// Package checkin 保存「每账号自动签到」规则并按北京时间触发。
//
// 规则放在独立的 proxy-checkin.json，不写进 proxy-accounts.json：账号池文件
// 会被外部编辑并被内存副本回写，混放容易互相覆盖。
package checkin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/wnddd839/codebuddy-proxy/internal/atomicwrite"
)

// Rule 是一个账号的一天一次签到规则。
//
// Mode 决定时刻怎么定：
//   - fixed：固定在 Start
//   - window：在 [Start, End] 内按天确定性随机（旧 window 行为）
//   - allDay：全天随机（Start/End 均空时的等价写法）
//   - human：仿人分布——避开凌晨、偏好白天与晚间（推荐，默认给新规则的值）
//
// Offset 是用户微调（分钟，-120..120）：在最终时刻上叠加，夹到当天范围内。
// Start/End 统一按北京时间（UTC+8）。
type Rule struct {
	AccountID string `json:"accountId"`
	Enabled   bool   `json:"enabled"`
	Start     string `json:"start"`          // HH:MM；human 模式忽略
	End       string `json:"end"`            // HH:MM 或空；human 模式忽略
	Mode      string `json:"mode,omitempty"` // fixed / window / allDay / human
	Offset    int    `json:"offset,omitempty"`
}

const maxOffsetMinutes = 120

// CheckinModeDerived 从旧字段推导 mode：Start 与 End 都空 → allDay；
// End 为空或等于 Start → fixed；否则 → window。显式 Mode 优先（human 时忽略时间窗）。
func CheckinModeDerived(rule Rule) string {
	if m := strings.TrimSpace(rule.Mode); m != "" {
		return m
	}
	if strings.TrimSpace(rule.Start) == "" {
		return "allDay"
	}
	if strings.TrimSpace(rule.End) == "" {
		return "fixed"
	}
	return "window"
}

type Store struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

// normalizedRule 是校验后的规则，时间换成当天零点起的分钟数。
type normalizedRule struct {
	AccountID string
	Mode      string
	StartMin  int // window 起点；fixed 时等于 EndMin；allDay/human 忽略
	EndMin    int // window 终点；fixed 时等于 StartMin；allDay/human 忽略
	OffsetMin int // 用户微调（已夹取）
}

func normalizeRules(rules []Rule) ([]normalizedRule, error) {
	out := make([]normalizedRule, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		id := strings.TrimSpace(rule.AccountID)
		if id == "" {
			return nil, fmt.Errorf("checkin: 规则缺少 accountId")
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("checkin: 账号 %s 重复配置", id)
		}
		seen[id] = struct{}{}
		mode := CheckinModeDerived(rule)
		// 未知 mode 立即拒绝，避免打错字静默变成 window。
		switch mode {
		case "fixed", "window", "allDay", "human":
		default:
			return nil, fmt.Errorf("checkin: 账号 %s 模式 %q 无效（fixed/window/allDay/human）", id, mode)
		}
		// 微调夹取：±maxOffsetMinutes。
		offset := rule.Offset
		if offset > maxOffsetMinutes {
			offset = maxOffsetMinutes
		}
		if offset < -maxOffsetMinutes {
			offset = -maxOffsetMinutes
		}
		// human 模式不看时间窗口。
		if mode == "human" {
			if !rule.Enabled {
				continue
			}
			out = append(out, normalizedRule{AccountID: id, Mode: mode, OffsetMin: offset})
			continue
		}
		// 其余模式先解时间窗。allDay 要求两个字段都空；
		// 只填结束没有意义，明确拒绝，避免用户以为设了个「到 22:00 为止」的窗口。
		startText, endText := strings.TrimSpace(rule.Start), strings.TrimSpace(rule.End)
		if mode == "allDay" && endText != "" {
			return nil, fmt.Errorf("checkin: 账号 %s 全天模式下开始/结束都应留空", id)
		}
		if startText == "" && endText == "" {
			if !rule.Enabled {
				continue
			}
			out = append(out, normalizedRule{AccountID: id, Mode: "allDay", StartMin: 0, EndMin: 23*60 + 59, OffsetMin: offset})
			continue
		}
		if startText == "" {
			return nil, fmt.Errorf("checkin: 账号 %s 开始时间为空但结束时间有值，请同时填写或都留空", id)
		}
		start, err := parseClock(startText)
		if err != nil {
			return nil, fmt.Errorf("checkin: 账号 %s 开始时间 %q 无效: %w", id, rule.Start, err)
		}
		end := start
		if endText != "" {
			end, err = parseClock(endText)
			if err != nil {
				return nil, fmt.Errorf("checkin: 账号 %s 结束时间 %q 无效: %w", id, rule.End, err)
			}
		}
		if end < start {
			return nil, fmt.Errorf("checkin: 账号 %s 结束时间不能早于开始时间（不支持跨零点）", id)
		}
		if !rule.Enabled {
			continue
		}
		out = append(out, normalizedRule{AccountID: id, Mode: mode, StartMin: start, EndMin: end, OffsetMin: offset})
	}
	return out, nil
}

func parseClock(raw string) (int, error) {
	hText, mText, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok || len(mText) != 2 {
		return 0, fmt.Errorf("格式应为 HH:MM")
	}
	hour, errH := strconv.Atoi(hText)
	minute, errM := strconv.Atoi(mText)
	if errH != nil || errM != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("格式应为 HH:MM")
	}
	return hour*60 + minute, nil
}

// File 是带锁的规则文件。文件不存在视为空规则。
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
		return Store{}, fmt.Errorf("checkin: 解析 %s 失败: %w", f.path, err)
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
	if _, err := normalizeRules(store.Rules); err != nil {
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

// writeFile 仅供测试使用。
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
