// Package proxies 保存「候选出口清单」（proxies.json）。
//
// 账号实际使用哪个出口以 accounts.Account.Proxy 为准；
// 这个文件只是方便管理台下拉选择，避免每次手输 URL。
package proxies

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/atomicwrite"
	"github.com/wnddd839/codebuddy-proxy/internal/egress"
)

// ID 返回出口的短标识（URL 的 sha256 前 12 位十六进制）。
// 管理台只拿到这个 ID 与打码 URL，明文密码不出服务端。
func ID(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:6])
}

// Entry 是一条第候选出口。
type Entry struct {
	URL     string `json:"url"`
	Label   string `json:"label,omitempty"`
	AddedAt int64  `json:"addedAt,omitempty"`
}

// Store 是 proxies.json 的磁盘格式。
type Store struct {
	Version int     `json:"version"`
	Proxies []Entry `json:"proxies"`
}

// File 是带锁的候选出口清单。
type File struct {
	path string
	mu   sync.Mutex
}

// Open 打开（或初始化）清单文件。文件不存在不报错。
func Open(path string) *File {
	return &File{path: path}
}

// Path 返回清单文件路径。
func (f *File) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

// Read 读取清单。文件不存在返回空清单。
func (f *File) Read() (Store, error) {
	if f == nil {
		return Store{Version: 1}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readLocked()
}

func (f *File) readLocked() (Store, error) {
	raw, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: 1, Proxies: []Entry{}}, nil
		}
		return Store{}, err
	}
	var store Store
	if err := json.Unmarshal(raw, &store); err != nil {
		return Store{}, fmt.Errorf("proxies: 解析 %s 失败: %w", f.path, err)
	}
	if store.Version == 0 {
		store.Version = 1
	}
	cleaned := store.Proxies[:0]
	for _, entry := range store.Proxies {
		entry.URL = strings.TrimSpace(entry.URL)
		if entry.URL == "" {
			continue
		}
		cleaned = append(cleaned, entry)
	}
	store.Proxies = cleaned
	return store, nil
}

// Add 追加候选出口（同 URL 视为更新 label）。
func (f *File) Add(url, label string) (Store, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return Store{}, fmt.Errorf("proxies: URL 不能为空")
	}
	if _, err := egress.Parse(url); err != nil {
		return Store{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	store, err := f.readLocked()
	if err != nil {
		return Store{}, err
	}
	label = strings.TrimSpace(label)
	found := false
	for i := range store.Proxies {
		if store.Proxies[i].URL != url {
			continue
		}
		if label != "" {
			store.Proxies[i].Label = label
		}
		found = true
		break
	}
	if !found {
		store.Proxies = append(store.Proxies, Entry{URL: url, Label: label, AddedAt: time.Now().UnixMilli()})
	}
	if err := f.writeLocked(store); err != nil {
		return Store{}, err
	}
	return store, nil
}

// Remove 删除候选出口。
func (f *File) Remove(url string) (Store, error) {
	url = strings.TrimSpace(url)
	f.mu.Lock()
	defer f.mu.Unlock()
	store, err := f.readLocked()
	if err != nil {
		return Store{}, err
	}
	kept := make([]Entry, 0, len(store.Proxies))
	removed := false
	for _, entry := range store.Proxies {
		if entry.URL == url {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return store, nil
	}
	store.Proxies = kept
	if err := f.writeLocked(store); err != nil {
		return Store{}, err
	}
	return store, nil
}

// Find 按短 ID 查候选出口。
func (f *File) Find(id string) (Entry, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Entry{}, false, nil
	}
	store, err := f.Read()
	if err != nil {
		return Entry{}, false, err
	}
	for _, entry := range store.Proxies {
		if ID(entry.URL) == id {
			return entry, true, nil
		}
	}
	return Entry{}, false, nil
}

func (f *File) writeLocked(store Store) error {
	if store.Version == 0 {
		store.Version = 1
	}
	if store.Proxies == nil {
		store.Proxies = []Entry{}
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicwrite.Write(f.path, raw, 0o600)
}
