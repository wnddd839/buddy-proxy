// Package update 实现仅由管理台点击触发的官方二进制更新。
package update

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

const latestURL = "https://api.github.com/repos/wnddd839/buddy-proxy/releases/latest"
const latestWebURL = "https://github.com/wnddd839/buddy-proxy/releases/latest"
const releaseTagBase = "https://github.com/wnddd839/buddy-proxy/releases/tag/"
const downloadBase = "https://github.com/wnddd839/buddy-proxy/releases/download/"
const maxBinary = 256 << 20

type asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type State struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	Supported bool   `json:"supported"`
	Phase     string `json:"phase"`
	Message   string `json:"message,omitempty"`
}

type Manager struct {
	mu         sync.Mutex
	busy       bool
	cancel     context.CancelFunc
	state      State
	release    release
	exe, asset string
	supported  bool
	client     *http.Client
	requests   chan string
}

// New 不访问网络；源码、容器及无法识别的架构明确拒绝自替换。
func New(current string) *Manager {
	m := &Manager{requests: make(chan string, 1)}
	m.state = State{Current: current, Phase: "idle"}
	m.client = &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("更新下载重定向过多")
		}
		if r.URL.Scheme != "https" || r.URL.User != nil || r.URL.Port() != "" {
			return errors.New("更新下载重定向不安全")
		}
		switch r.URL.Hostname() {
		case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			return nil
		default:
			return errors.New("更新下载重定向不属于 GitHub")
		}
	}}
	m.exe, _ = os.Executable()
	arm := ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" {
				arm = s.Value
			}
		}
	}
	m.asset, _ = assetName(runtime.GOOS, runtime.GOARCH, arm)
	switch {
	case container():
		m.state.Message = "Docker 请更新镜像并重建容器：docker compose pull && docker compose up -d"
	case parseVersion(current) == nil:
		m.state.Message = "源码/dev 构建不支持自更新；请先手动安装带版本号的官方发行版"
	case m.asset == "":
		m.state.Message = "无法确定支持的 OS/架构（ARM 必须含 GOARM=6 或 7 构建信息）"
	case m.exe == "" || strings.Contains(filepath.ToSlash(m.exe), "/go-build"):
		m.state.Message = "无法识别可更新的独立可执行文件"
	default:
		m.supported = true
	}
	m.state.Supported = m.supported
	if message := os.Getenv(resultEnv); message != "" {
		m.state.Phase = "error"
		m.state.Message = message
	}
	os.Unsetenv(resultEnv)
	return m
}

func container() bool {
	if os.Getenv("CODEBUDDY_PROXY_CONTAINER") == "1" {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return runtime.GOOS == "linux" && err == nil
}

func assetName(goos, arch, arm string) (string, error) {
	suffix := ""
	switch {
	case goos == "windows" && arch == "amd64":
		suffix = "windows-x64.exe"
	case (goos == "linux" || goos == "darwin") && (arch == "amd64" || arch == "arm64"):
		suffix = goos + "-" + arch
	case goos == "linux" && arch == "arm":
		arm, _, _ = strings.Cut(arm, ",")
		if arm == "6" || arm == "7" {
			suffix = "linux-armv" + arm
		}
	}
	if suffix == "" {
		return "", errors.New("不支持或无法确定目标架构")
	}
	return "codebuddy-proxy-" + suffix, nil
}

func parseVersion(s string) []uint64 {
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nil
	}
	n := make([]uint64, 4)
	for i, p := range parts {
		if p == "" || strings.IndexFunc(p, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return nil
		}
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil
		}
		n[i] = v
	}
	return n
}

func newer(a, b string) bool {
	x, y := parseVersion(a), parseVersion(b)
	if x == nil || y == nil {
		return false
	}
	for i := range 4 {
		if c := cmp.Compare(x[i], y[i]); c != 0 {
			return c > 0
		}
	}
	return false
}

func (m *Manager) Status() State           { m.mu.Lock(); defer m.mu.Unlock(); return m.state }
func (m *Manager) Requests() <-chan string { return m.requests }

func (m *Manager) begin(phase string) error {
	if m.busy {
		return errors.New("已有更新操作正在进行")
	}
	if !m.supported {
		return errors.New(m.state.Message)
	}
	m.busy = true
	m.state.Phase = phase
	m.state.Message = ""
	return nil
}

func (m *Manager) finish(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.busy = false
	m.cancel = nil
	if err != nil {
		m.state.Phase = "error"
		m.state.Message = err.Error()
	} else {
		m.state.Phase = "idle"
	}
}

func (m *Manager) Check(ctx context.Context) (State, error) {
	m.mu.Lock()
	err := m.begin("checking")
	m.mu.Unlock()
	if err != nil {
		return m.Status(), err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rel, err := m.latest(ctx)
	if err == nil && (rel.Draft || rel.Prerelease || parseVersion(rel.Tag) == nil) {
		err = errors.New("官方最新 Release 不是支持的正式版本")
	}
	if err == nil {
		foundBinary, foundSum := false, false
		for _, a := range rel.Assets {
			if a.Name == m.asset && a.Size > 0 && a.Size <= maxBinary {
				foundBinary = true
			}
			if a.Name == "SHA256SUMS.txt" {
				foundSum = true
			}
		}
		if !foundBinary || !foundSum {
			err = errors.New("官方 Release 缺少当前架构文件或 SHA256SUMS.txt")
		}
	}
	m.mu.Lock()
	if err == nil {
		m.release = rel
		m.state.Latest = rel.Tag
		m.state.Available = newer(rel.Tag, m.state.Current)
	} else {
		m.state.Available = false
		m.release = release{}
	}
	m.mu.Unlock()
	m.finish(err)
	return m.Status(), err
}

// Start 只使用此前点击检查得到的官方版本，客户端不能提交路径或 URL。
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return errors.New("已有更新操作正在进行")
	}
	if !m.state.Available {
		m.mu.Unlock()
		return errors.New("请先检查并确认有新版本")
	}
	if err := m.begin("downloading"); err != nil {
		m.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	m.cancel = cancel
	rel := m.release
	m.mu.Unlock()
	go func() {
		defer cancel()
		path, err := m.stage(ctx, rel)
		if err != nil {
			m.finish(err)
			return
		}
		m.mu.Lock()
		// 取消与交接互斥，避免取消成功后仍然重启。
		if ctx.Err() != nil {
			m.mu.Unlock()
			os.Remove(path)
			m.finish(ctx.Err())
			return
		}
		m.cancel = nil
		m.state.Phase = "restarting"
		m.state.Message = "正在排空请求并重启"
		m.requests <- path
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) Cancel() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return errors.New("当前没有可取消的下载")
	}
	m.cancel()
	return nil
}

func (m *Manager) Fail(err error) { m.finish(err) }

// get 读取固定来源的更新文件。GitHub Release 的响应体偶尔会被中途掐断，
// 表现为 io.ReadAll 返回 unexpected EOF / io.ErrUnexpectedEOF；此时整段重取，
// 避免把一次瞬断升级成「更新失败」。重试只在读完之前出错时进行，且受 ctx 约束。
const getAttempts = 3

func (m *Manager) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	var lastErr error
	for range getAttempts {
		b, err := m.getOnce(ctx, u, limit)
		if err == nil {
			return b, nil
		}
		lastErr = err
		// 非读取期错误（404 / 重定向被拒 / 响应头异常）重试无意义。
		if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("更新文件下载中断（已重试 %d 次）: %w", getAttempts, lastErr)
}

func (m *Manager) getOnce(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "buddy-proxy-manual-updater")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, responseError(resp)
	}
	if resp.ContentLength > limit {
		return nil, errors.New("更新响应超出大小限制")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("更新响应超出大小限制")
	}
	if resp.ContentLength >= 0 && int64(len(b)) != resp.ContentLength {
		return nil, io.ErrUnexpectedEOF
	}
	return b, nil
}

func (m *Manager) stage(ctx context.Context, rel release) (path string, err error) {
	f, err := os.CreateTemp(filepath.Dir(m.exe), ".buddy-update-stage-*.exe")
	if err != nil {
		return "", fmt.Errorf("安装目录不可写（不会提权）：%w", err)
	}
	path = f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(path)
		}
	}()
	base := downloadBase + url.PathEscape(rel.Tag) + "/"
	sums, err := m.get(ctx, base+"SHA256SUMS.txt", 1<<20)
	if err != nil {
		return path, err
	}
	expected, err := checksum(sums, m.asset)
	if err != nil {
		return path, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+m.asset, nil)
	if err != nil {
		return path, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return path, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return path, responseError(resp)
	}
	size := int64(0)
	for _, a := range rel.Assets {
		if a.Name == m.asset {
			size = a.Size
		}
	}
	if size <= 0 || size > maxBinary || resp.ContentLength > maxBinary {
		return path, errors.New("无效的可执行文件大小")
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, size+1))
	if err != nil {
		return path, err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != expected {
		return path, errors.New("下载大小或 SHA256 校验不匹配，原程序未修改")
	}
	if err = f.Chmod(0700); err != nil {
		return path, err
	}
	if err = f.Sync(); err != nil {
		return path, err
	}
	err = f.Close()
	return path, err
}
