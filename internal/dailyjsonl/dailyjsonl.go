// Package dailyjsonl 提供「按天一个文件」的 jsonl 存储的通用文件操作。
//
// 只负责路径、枚举、读取与清理 —— 追加策略留给各家族自己决定
// （systemprompt 有自己的内存兜底与写失败标记）。
//
// 所有函数都带 prefix：同一个目录下会并存多个家族
// （proxy-systemprompt-*.jsonl、proxy-decisions-*.jsonl），
// 无前缀的枚举与清理会跨家族误读误删。
package dailyjsonl

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// DateLayout 是文件名里的日期格式。
	DateLayout = "20060102"
	// FileSuffix 是文件扩展名。
	FileSuffix = ".jsonl"
)

// Path 返回某一天的文件路径。
func Path(dir, prefix, date string) string {
	return filepath.Join(dir, prefix+date+FileSuffix)
}

// Today 返回 now 对应的日期键。
func Today(now time.Time) string {
	return now.Format(DateLayout)
}

// Dates 返回该前缀下升序的日期列表（20060102）。
func Dates(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, FileSuffix) {
			continue
		}
		date := strings.TrimSuffix(strings.TrimPrefix(name, prefix), FileSuffix)
		if _, err := time.ParseInLocation(DateLayout, date, time.Local); err != nil {
			continue
		}
		out = append(out, date)
	}
	sort.Strings(out)
	return out
}

// ReadDate 返回某一天的所有非空行（不含换行符）。
// 文件不存在时返回 (nil, nil)：回放允许中间缺天。
func ReadDate(dir, prefix, date string) ([][]byte, error) {
	raw, err := os.ReadFile(Path(dir, prefix, date))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := make([][]byte, 0, 64)
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, []byte(trimmed))
	}
	return lines, nil
}

// Prune 删除该前缀下超过 retention 的按天文件，返回删除个数。
// 以「该日结束时刻」判过期，避免把当天整天误删。
func Prune(dir, prefix string, retention time.Duration, now time.Time) (int, error) {
	cutoff := now.Add(-retention)
	removed := 0
	var firstErr error
	for _, date := range Dates(dir, prefix) {
		day, err := time.ParseInLocation(DateLayout, date, time.Local)
		if err != nil {
			continue
		}
		endOfDay := day.Add(24*time.Hour - time.Nanosecond)
		if !endOfDay.Before(cutoff) {
			continue
		}
		if err := os.Remove(Path(dir, prefix, date)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
