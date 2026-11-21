package systemprompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/dailyjsonl"
)

const storeFilePrefix = "proxy-systemprompt-"
const storeFileSuffix = ".jsonl"
const storeDateLayout = "20060102"

// Store 按天把事件追加到 jsonl 文件。
// dir 为空时退化为纯内存 store（测试与未配置场景）。
type Store struct {
	mu          sync.Mutex
	fileMu      sync.RWMutex
	dir         string
	retention   time.Duration
	mem         []Event
	memDates    map[string]struct{}
	writeFailed bool
}

// IsPersistent 报告当前 Store 是否处于健康的磁盘落盘模式。
func (s *Store) IsPersistent() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir != "" && !s.writeFailed
}

// OpenStore 打开（或创建）dir 下的按天事件存储。
// retention<=0 时用 7 天。
func OpenStore(dir string, retention time.Duration) (*Store, error) {
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	s := &Store{dir: dir, retention: retention}
	if dir == "" {
		s.memDates = make(map[string]struct{})
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("systemprompt: 创建事件目录 %s 失败: %w", dir, err)
	}
	return s, nil
}

// Close 目前无资源需要释放，保留以便调用方统一 defer。
func (s *Store) Close() error { return nil }

func (s *Store) pathFor(date string) string {
	return dailyjsonl.Path(s.dir, storeFilePrefix, date)
}

func eventDate(e Event) string {
	return time.UnixMilli(e.At).In(time.Local).Format(storeDateLayout)
}

// Append 写入一条事件。单次 Write 写完整行，并发下不会出现半行交错。
func (s *Store) Append(e Event) error {
	if s == nil {
		return nil
	}
	date := eventDate(e)
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("systemprompt: 序列化事件失败: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		s.mem = append(s.mem, e)
		if s.memDates == nil {
			s.memDates = make(map[string]struct{})
		}
		s.memDates[date] = struct{}{}
		return nil
	}

	f, err := os.OpenFile(s.pathFor(date), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.writeFailed = true
		return fmt.Errorf("systemprompt: 打开事件文件失败: %w", err)
	}
	defer f.Close()
	// 一次 Write 一整行：O_APPEND 下单次写是原子的，避免高并发行交错。
	if _, err := f.Write(line); err != nil {
		s.writeFailed = true
		return fmt.Errorf("systemprompt: 写入事件失败: %w", err)
	}
	s.writeFailed = false
	return nil
}

// Dates 返回升序的日期列表（20060102）。
func (s *Store) Dates() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		out := make([]string, 0, len(s.memDates))
		for d := range s.memDates {
			out = append(out, d)
		}
		sort.Strings(out)
		return out
	}

	return dailyjsonl.Dates(s.dir, storeFilePrefix)
}

// Read 返回指定日期的事件页（最新在前）与当日总条数。
func (s *Store) Read(date string, offset, limit int) ([]Event, int, error) {
	if s == nil {
		return nil, 0, nil
	}
	date = strings.TrimSpace(date)
	if _, err := time.ParseInLocation(storeDateLayout, date, time.Local); err != nil {
		return nil, 0, fmt.Errorf("systemprompt: 非法日期 %q", date)
	}

	// 锁内只取「磁盘路径」或「内存切片快照」，文件 I/O 与逐行反序列化一律放锁外。
	// Append 在每条请求结束时都要拿同一把锁；若把整日全量读+解析压在锁内，
	// 管理员面板每 5 秒一轮的历史拉取会阻塞写事件，日文件越大越明显。
	// 磁盘并发安全性：Append 用 O_APPEND 单次 Write 整行，文件系统保证不交错；
	// 锁外读一个正在追加的文件最坏是最后一截半行，readFileEvents 会跳过坏行。
	// Prune 可能在读期间删文件 → os.ReadFile 返回 ErrNotExist，按「空」处理即可。
	s.mu.Lock()
	memoryOnly := s.dir == ""
	var memSnapshot []Event
	path := ""
	if memoryOnly {
		memSnapshot = append([]Event(nil), s.mem...)
	} else {
		path = s.pathFor(date)
	}
	s.mu.Unlock()

	var all []Event
	if memoryOnly {
		for _, e := range memSnapshot {
			if eventDate(e) == date {
				all = append(all, e)
			}
		}
	} else {
		s.fileMu.RLock()
		raw, readErr := os.ReadFile(path)
		s.fileMu.RUnlock()
		events, err := parseFileEvents(raw, readErr)
		if err != nil {
			return nil, 0, err
		}
		all = events
	}

	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = total
	}
	// 反序：最新在前。
	out := make([]Event, 0, limit)
	for i := total - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, all[i])
	}
	return out, total, nil
}

// StatsForDate 从某一天的 jsonl 全量统计（不受进程重启影响）。
// date 为 20060102；文件不存在返回零值 Stats。
// 复用 statsFrom 与 Journal.Stats24h 完全相同的统计逻辑（cutoff=0 表示全量），
// 保证实时视图与按天历史视图口径一致。
func (s *Store) StatsForDate(date string) (Stats, error) {
	if s == nil {
		return Stats{}, nil
	}
	date = strings.TrimSpace(date)
	if _, err := time.ParseInLocation(storeDateLayout, date, time.Local); err != nil {
		return Stats{}, fmt.Errorf("systemprompt: 非法日期 %q", date)
	}

	// 与 Read 一致：锁内只取磁盘路径或内存切片快照，文件 I/O 与反序列化在锁外。
	s.mu.Lock()
	memoryOnly := s.dir == ""
	var memSnapshot []Event
	path := ""
	if memoryOnly {
		memSnapshot = append([]Event(nil), s.mem...)
	} else {
		path = s.pathFor(date)
	}
	s.mu.Unlock()

	var all []Event
	if memoryOnly {
		for _, e := range memSnapshot {
			if eventDate(e) == date {
				all = append(all, e)
			}
		}
	} else {
		s.fileMu.RLock()
		raw, readErr := os.ReadFile(path)
		s.fileMu.RUnlock()
		events, err := parseFileEvents(raw, readErr)
		if err != nil {
			return Stats{}, err
		}
		all = events
	}
	return statsFrom(all, 0), nil
}

func parseFileEvents(raw []byte, err error) ([]Event, error) {
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("systemprompt: 读取事件文件失败: %w", err)
	}
	var out []Event
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// 坏行跳过：半行/手工编辑都不该让整页读不出来。
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Prune 删除超过 retention 的按天文件。
func (s *Store) Prune(now time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		cutoffDate := now.Add(-s.retention).Format(storeDateLayout)
		kept := s.mem[:0]
		dates := make(map[string]struct{})
		for _, e := range s.mem {
			d := eventDate(e)
			if d < cutoffDate {
				continue
			}
			kept = append(kept, e)
			dates[d] = struct{}{}
		}
		s.mem = kept
		s.memDates = dates
		return nil
	}

	s.fileMu.Lock()
	_, err := dailyjsonl.Prune(s.dir, storeFilePrefix, s.retention, now)
	s.fileMu.Unlock()
	if err != nil {
		return fmt.Errorf("systemprompt: 清理过期事件文件失败: %w", err)
	}
	return nil
}
