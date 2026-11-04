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

// Rule 是一个账号的一天一次签到窗口。End 为空或与 Start 相同表示固定时刻；
// 否则在 [Start, End] 内随机取一个分钟级时刻。时间统一按北京时间（UTC+8）。
type Rule struct {
	AccountID string `json:"accountId"`
	Enabled   bool   `json:"enabled"`
	Start     string `json:"start"` // HH:MM
	End       string `json:"end"`   // HH:MM 或空
}

type Store struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

// normalizedRule 是校验后的规则，时间换成当天零点起的分钟数。
type normalizedRule struct {
	AccountID string
	StartMin  int
	EndMin    int
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
		start, err := parseClock(rule.Start)
		if err != nil {
			return nil, fmt.Errorf("checkin: 账号 %s 开始时间 %q 无效: %w", id, rule.Start, err)
		}
		end := start
		if strings.TrimSpace(rule.End) != "" {
			end, err = parseClock(rule.End)
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
		out = append(out, normalizedRule{AccountID: id, StartMin: start, EndMin: end})
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
