package decision

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/dailyjsonl"
)

const filePrefix = "proxy-decisions-"

// Store 按天追加决策记录。
type Store struct {
	dir       string
	retention time.Duration
	mu        sync.Mutex
}

// OpenStore 打开（或创建）dir 下的决策存储。retention<=0 时默认 30 天。
func OpenStore(dir string, retention time.Duration) (*Store, error) {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, retention: retention}, nil
}

// OpenStoreReadOnly 只打开已存在的目录，绝不创建。
//
// 审计工具必须用这个：`-dir` 拼错时不应该在别人机器上凭空建出目录，
// 那会让「空报告」看起来像「已初始化但没数据」。
func OpenStoreReadOnly(dir string, retention time.Duration) (*Store, error) {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	return &Store{dir: dir, retention: retention}, nil
}

// Append 追加一条记录。At==0 时填当前毫秒。
func (s *Store) Append(r Record) error {
	if s == nil {
		return nil
	}
	if r.At == 0 {
		r.At = time.Now().UnixMilli()
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	date := time.UnixMilli(r.At).In(time.Local).Format(dailyjsonl.DateLayout)
	f, err := os.OpenFile(dailyjsonl.Path(s.dir, filePrefix, date),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadRange 返回 [from, to] 内的记录，按 At 升序。坏行跳过。
func (s *Store) ReadRange(from, to time.Time) ([]Record, error) {
	return s.read(from, to, "")
}

// ReadKind 只返回指定 kind 的记录（流式过滤，不把其它 kind 留在内存里）。
//
// 回扫必须用它：一天的 request 记录是几万条，全读进来再过滤会
// 直接顶到 80MB 预算。
func (s *Store) ReadKind(from, to time.Time, kind string) ([]Record, error) {
	return s.read(from, to, kind)
}

func (s *Store) read(from, to time.Time, kind string) ([]Record, error) {
	if s == nil {
		return nil, nil
	}
	fromMs, toMs := from.UnixMilli(), to.UnixMilli()
	fromKey := from.In(time.Local).Format(dailyjsonl.DateLayout)
	toKey := to.In(time.Local).Format(dailyjsonl.DateLayout)

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Record
	for _, date := range dailyjsonl.Dates(s.dir, filePrefix) {
		if date < fromKey || date > toKey {
			continue
		}
		lines, err := dailyjsonl.ReadDate(s.dir, filePrefix, date)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			var r Record
			if err := json.Unmarshal(line, &r); err != nil {
				continue // 半行/手工编辑不该让整段读不出来
			}
			if r.At < fromMs || r.At > toMs {
				continue
			}
			if kind != "" && r.Kind != kind {
				continue
			}
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}

// Prune 删除超过 retention 的按日文件。
func (s *Store) Prune(now time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := dailyjsonl.Prune(s.dir, filePrefix, s.retention, now)
	return err
}

// Dir 返回存储目录（面板展示与回放用）。
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// EarliestDate 返回最早一天的日期键（20060102），无文件时返回空串。
func (s *Store) EarliestDate() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dates := dailyjsonl.Dates(s.dir, filePrefix)
	if len(dates) == 0 {
		return ""
	}
	return dates[0]
}
