package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
	"github.com/wnddd839/codebuddy-proxy/internal/systemprompt"
)

// systemPromptRetention 是按天事件文件的保留时长。
const systemPromptRetention = 7 * 24 * time.Hour

// systemPromptPruneInterval 是惰性清理的最小间隔。
const systemPromptPruneInterval = time.Hour

// SystemPromptOutcome 是一次请求**实际**使用的 system 形态。
//
// 由 CompleteFromPool 回填到调用方传入的指针里（opts.SystemOutcome 为 nil 则不回填）。
// 用共享指针而不是返回值：CompleteFromPool 内部会递归重试，只有指针能把
// 「最终形态 + 是否降级」带回最外层调用者，供 07 用量明细交叉对照。
type SystemPromptOutcome struct {
	Form        string `json:"form,omitempty"`        // keep / fold
	Fingerprint string `json:"fingerprint,omitempty"` // system 的 SHA-256 前 8 位
	Degraded    bool   `json:"degraded,omitempty"`    // 是否因 11128 降级过
}

// systemAttempt 是一次上游尝试的 system 侧快照，只算一次指纹。
// mode 是请求入口快照的模式（auto / keep / fold），不随管理员热切换变化：
// 事件归因与 11128 降级判定都必须用它，而不是请求结束时的实时 Config。
type systemAttempt struct {
	started  time.Time
	keepForm bool
	fp       string
	size     int
	mode     string
}

func formName(keep bool) string {
	if keep {
		return "keep"
	}
	return "fold"
}

// isChannelBlocked 判定上游是否以 11128（未授权渠道 / 竞品 CLI 指纹）拒收。
// 使用 upstreamErrorCode 提取业务码，避免把本地代理端口（如 127.0.0.1:11128）误判为上游拦截。
func isChannelBlocked(err error) bool {
	if err == nil {
		return false
	}
	raw := err.Error()
	return upstreamErrorCode(raw) == "11128" || strings.Contains(strings.ToLower(raw), "unapproved channel")
}

var (
	upstreamStatusPattern = regexp.MustCompile(`(?i)failed with (\d{3})`)
	upstreamCodePattern   = regexp.MustCompile(`\(code (\d+)\)`)
	// 上游有时只回裸码（没有 "(code N)" 包裹），且新码会不断出现。
	// 用通用 5 位 1xxxx 正则，别再维护一份会漏的手工白名单：
	// 线上出现过 11134，旧白名单漏它 → ErrorCode 为空 → 被误标成 Status=0（传输失败）。
	//
	// 约束要严，别把网络地址/端口里的数字当错误码：
	//  - 前面不能是 ':'（127.0.0.1:14080 里的 14080 是 WARP 本地端口）、
	//    也不能是 '.'（IP 片段）、数字或字母（避免拼在更长串里）；
	//  - 用 \b 边界挡住 3 位 HTTP 状态、6 位数字、长度/耗时、十六进制 ID。
	upstreamBareCodePattern = regexp.MustCompile(`(^|[^:\.\w])(1\d{4})\b`)
)

// systemPromptErrorStatus 把上游错误压成面板可读的 (status, code)。
// 上游有响应但拒绝 → 该 HTTP 状态；根本没拿到响应（建连 / 首内容前断流）→ 0。
func systemPromptErrorStatus(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	msg := err.Error()
	if m := upstreamStatusPattern.FindStringSubmatch(msg); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			return n, upstreamErrorCode(msg)
		}
	}
	if code := upstreamErrorCode(msg); code != "" {
		return http.StatusBadGateway, code
	}
	return 0, ""
}

func upstreamErrorCode(msg string) string {
	// 带 "(code N)" 的优先：这是上游最明确的口径。
	if m := upstreamCodePattern.FindStringSubmatch(msg); m != nil {
		return m[1]
	}
	// 否则取文本里第一个 1xxxx 裸码（捕获组 2，组 1 是前导分隔符）。
	if m := upstreamBareCodePattern.FindStringSubmatch(msg); m != nil {
		return m[2]
	}
	return ""
}

// systemPromptText 把 system content 压成文本：字符串零拷贝，其余走 JSON（键序稳定）。
func systemPromptText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

// systemPromptDigest 返回客户端 system 的指纹与总字节数。
//
// 多个 system/developer 按出现顺序用 "\n\n" 拼接后再取指纹，与折叠时
// instructions 的拼接方式一致，保证同一份输入得到同一个指纹。
// 只读 role==system 的消息：user 内容永远不进事件、也不进指纹。
func systemPromptDigest(messages []map[string]any) (string, int) {
	parts := make([]string, 0, 2)
	for _, m := range messages {
		if provider.NormalizeMessageRole(fmt.Sprint(m["role"])) != "system" {
			continue
		}
		text := strings.TrimSpace(systemPromptText(m["content"]))
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "", 0
	}
	joined := strings.Join(parts, "\n\n")
	return systemprompt.Fingerprint(joined), len(joined)
}

// openSystemPromptStore 在账号文件同目录打开按天事件存储。
// 目录不可用时退化为纯内存：面板观测绝不能拦住服务启动。
func openSystemPromptStore(accountsPath string, logger *slog.Logger) *systemprompt.Store {
	dir := ""
	if trimmed := strings.TrimSpace(accountsPath); trimmed != "" {
		dir = filepath.Dir(trimmed)
	}
	store, err := systemprompt.OpenStore(dir, systemPromptRetention)
	if err != nil {
		if logger != nil {
			logger.Warn("system prompt event store unavailable; running in memory", "dir", dir, "error", err.Error())
		}
		store, _ = systemprompt.OpenStore("", systemPromptRetention)
	}
	return store
}

// RecordSystemPromptEvent 把一条事件写进内存事件流与按天 jsonl。
// 写盘失败只 WARN：面板观测绝不能影响请求本身。
func (s *Service) RecordSystemPromptEvent(e systemprompt.Event) {
	if s == nil {
		return
	}
	if s.SystemPromptJournal != nil {
		s.SystemPromptJournal.Record(e)
	}
	if s.SystemPromptStore != nil {
		if err := s.SystemPromptStore.Append(e); err != nil && s.Log != nil {
			s.Log.Warn("system prompt event append failed", "error", err.Error())
		}
	}
	s.maybePruneSystemPromptStore()
}

// maybePruneSystemPromptStore 最多每小时清一次过期文件。
// 惰性清理而不是后台 goroutine：省掉 Close 时的生命周期管理，也不会在空转时占资源。
func (s *Service) maybePruneSystemPromptStore() {
	if s == nil || s.SystemPromptStore == nil {
		return
	}
	now := time.Now()
	last := s.systemPrunedAt.Load()
	if last > 0 && now.Sub(time.UnixMilli(last)) < systemPromptPruneInterval {
		return
	}
	if !s.systemPrunedAt.CompareAndSwap(last, now.UnixMilli()) {
		return
	}
	if err := s.SystemPromptStore.Prune(now); err != nil {
		s.systemPrunedAt.Store(last)
		if s.Log != nil {
			s.Log.Warn("system prompt store prune failed", "error", err.Error())
		}
	}
}

// recordSystemPromptAttempt 记一条上游尝试的 system 处理结果。
//
// 只记「客户端带了 system」或「本次按 keep 形态发出」的请求：
// 纯 fold 且没有 system 的请求没有观察价值，记了只会把信号淹掉。
func (s *Service) recordSystemPromptAttempt(
	opts CompleteOptions,
	account accounts.Account,
	site string,
	att systemAttempt,
	callErr error,
	degraded bool,
) {
	if s == nil {
		return
	}
	if att.fp == "" && !att.keepForm {
		return
	}
	status, code := systemPromptErrorStatus(callErr)
	// FoldedRetry 表示本次是 11128 降级后的重放；只有重放成功（callErr==nil）
	// 才算「降级恢复成功」。重放再次失败不置 Recovered。
	recovered := opts.FoldedRetry && callErr == nil
	s.RecordSystemPromptEvent(systemprompt.Event{
		At:          att.started.UnixMilli(),
		RequestID:   opts.RequestID,
		AccountID:   account.ID,
		Site:        site,
		Model:       opts.Model,
		Mode:        att.mode,
		Form:        formName(att.keepForm),
		Degraded:    degraded,
		Recovered:   recovered,
		Fingerprint: att.fp,
		SystemLen:   att.size,
		Status:      status,
		ErrorCode:   code,
		DurationMs:  time.Since(att.started).Milliseconds(),
	})
}
