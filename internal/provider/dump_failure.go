package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var failureDumpMu sync.Mutex

const (
	// 只留最近若干份，避免长期开启把磁盘写满。
	maxFailureDumps = 6
	// 单份上限。真实排障样本一般 1-3MB，超长对话可能到几十 MB。
	maxFailureDumpBytes = 64 << 20
)

// dumpUpstreamFailure 把一次上游失败的完整请求体与工具调用诊断落盘。
// 返回写出的文件路径（失败时为空），只用于日志，不影响主流程。
func dumpUpstreamFailure(dir string, status int, body map[string]any, upstreamMsg string) (string, error) {
	if dir == "" || body == nil {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	failureDumpMu.Lock()
	defer failureDumpMu.Unlock()
	now := time.Now()
	file, err := os.CreateTemp(dir, fmt.Sprintf("buddy-failure-%s-%d-*.request.json", now.Format("20060102T150405.000000000"), status))
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(file.Name(), ".request.json")

	payload, err := json.MarshalIndent(body, "", " ")
	if err != nil {
		// 出现不可序列化的值时不放弃：用 %v 兜底，至少留下证据。
		payload = []byte(fmt.Sprintf("%v", body))
	}
	if len(payload) > maxFailureDumpBytes {
		payload = append(payload[:maxFailureDumpBytes], []byte("\n\n/* TRUNCATED: exceeded dump size limit */\n")...)
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	diag := buildToolCallDiagnostics(body)
	diag["status"] = status
	diag["upstreamMessage"] = upstreamMsg
	diag["dumpedAt"] = now.Format(time.RFC3339Nano)
	if raw, err := json.MarshalIndent(diag, "", " "); err == nil {
		_ = os.WriteFile(base+".diagnostics.json", raw, 0o600)
	}

	pruneFailureDumps(dir)
	return base, nil
}

// buildToolCallDiagnostics 汇总请求里每个 tool_call.arguments 的形态。
//
// 上游 11129「invalid function call parameters」唯一可能的触发源就是这些
// arguments：它们的长度、是否是合法 JSON、是否被截断在半个对象上。
func buildToolCallDiagnostics(body map[string]any) map[string]any {
	issues := make([]map[string]any, 0, 8)
	total := 0
	invalid := 0
	empty := 0
	kinds := map[string]int{}

	eachMessage(body["messages"], func(i int, msg map[string]any) {
		if role := fmt.Sprint(msg["role"]); role != "assistant" {
			return
		}
		calls := asSlice(msg["tool_calls"])
		for ci, raw := range calls {
			call, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			total++
			fn, _ := call["function"].(map[string]any)
			name := fmt.Sprint(fn["name"])
			args := fn["arguments"]

			var text string
			kind := "unknown"
			switch v := args.(type) {
			case nil:
				kind = "nil"
				empty++
			case string:
				kind = "string"
				text = v
				if strings.TrimSpace(v) == "" {
					empty++
				}
			default:
				kind = fmt.Sprintf("%T", v)
			}
			kinds[kind]++

			valid := true
			errText := ""
			if kind == "string" && strings.TrimSpace(text) != "" {
				var parsed any
				if err := json.Unmarshal([]byte(text), &parsed); err != nil {
					valid = false
					errText = err.Error()
				}
			}
			if !valid {
				invalid++
			}
			// 只记录异常项与空参数，避免把整份对话再抄一遍。
			if valid && kind == "string" && strings.TrimSpace(text) != "" {
				continue
			}
			item := map[string]any{
				"msgIndex":  i,
				"callIndex": ci,
				"id":        fmt.Sprint(call["id"]),
				"name":      name,
				"argsKind":  kind,
				"argsLen":   len(text),
				"validJson": valid,
			}
			if errText != "" {
				item["error"] = errText
			}
			if text != "" {
				if len(text) > 400 {
					item["head"] = text[:200]
					item["tail"] = text[len(text)-200:]
				} else {
					item["args"] = text
				}
			}
			issues = append(issues, item)
		}
	})

	return map[string]any{
		"messageCount":    messageCountOf(body["messages"]),
		"toolCallsTotal":  total,
		"toolCallsEmpty":  empty,
		"toolCallsBad":    invalid,
		"argumentsKinds":  kinds,
		"problemCalls":    issues,
		"toolsDeclared":   len(asSlice(body["tools"])),
		"messagesCharsum": charsOf(body["messages"]),
	}
}

func eachMessage(value any, fn func(i int, msg map[string]any)) {
	switch list := value.(type) {
	case []any:
		for i, raw := range list {
			if msg, ok := raw.(map[string]any); ok {
				fn(i, msg)
			}
		}
	case []map[string]any:
		for i, msg := range list {
			fn(i, msg)
		}
	}
}

func asSlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out
	default:
		return nil
	}
}

func messageCountOf(value any) int {
	n := 0
	eachMessage(value, func(int, map[string]any) { n++ })
	return n
}

func charsOf(value any) int {
	total := 0
	eachMessage(value, func(_ int, msg map[string]any) {
		if s, ok := msg["content"].(string); ok {
			total += len(s)
		}
	})
	return total
}

// pruneFailureDumps 只保留最近 maxFailureDumps 组（按 .request.json 计）。
func pruneFailureDumps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	stamps := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "buddy-failure-") || !strings.HasSuffix(name, ".request.json") {
			continue
		}
		stamps = append(stamps, strings.TrimSuffix(name, ".request.json"))
	}
	if len(stamps) <= maxFailureDumps {
		return
	}
	sort.Strings(stamps)
	for _, base := range stamps[:len(stamps)-maxFailureDumps] {
		_ = os.Remove(filepath.Join(dir, base+".request.json"))
		_ = os.Remove(filepath.Join(dir, base+".diagnostics.json"))
	}
}
