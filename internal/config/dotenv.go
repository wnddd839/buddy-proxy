package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv 从就近 .env 加载 KEY=VALUE 到进程环境。
// 已存在的环境变量不会被覆盖。
//
// 搜索顺序：
//  1. CODEBUDDY_PROXY_ENV_FILE（若设置）
//  2. ./.env（当前工作目录）
//  3. <可执行文件目录>/.env
//  4. ../.env（从 go-codebuddy/ 运行时即仓库根目录）
func LoadDotEnv() (loaded []string, err error) {
	candidates := make([]string, 0, 4)
	if custom := strings.TrimSpace(os.Getenv("CODEBUDDY_PROXY_ENV_FILE")); custom != "" {
		candidates = append(candidates, expandHome(custom))
	}
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		candidates = append(candidates, filepath.Join(cwd, ".env"))
		candidates = append(candidates, filepath.Join(cwd, "..", ".env"))
	}
	if exe, exeErr := os.Executable(); exeErr == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}

	seen := map[string]struct{}{}
	for _, path := range candidates {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() {
			continue
		}
		if loadErr := loadDotEnvFile(abs); loadErr != nil {
			return loaded, fmt.Errorf("load %s: %w", abs, loadErr)
		}
		loaded = append(loaded, abs)
	}
	return loaded, nil
}

func loadDotEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// 允许较长值（如 JWT），避免 scanner 失败。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		value = unquoteEnvValue(value)
		// 不覆盖已有进程环境变量。
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("line %d: set %s: %w", lineNo, key, err)
		}
	}
	return scanner.Err()
}

func unquoteEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	// 无引号值去掉行内注释：KEY=val # comment
	if i := strings.Index(value, " #"); i >= 0 {
		return strings.TrimSpace(value[:i])
	}
	return value
}

// ResolveEnvFilePath 决定生成配置写入哪个 .env 文件。
func ResolveEnvFilePath() string {
	if custom := strings.TrimSpace(os.Getenv("CODEBUDDY_PROXY_ENV_FILE")); custom != "" {
		return expandHome(custom)
	}
	if cwd, err := os.Getwd(); err == nil {
		cwdEnv := filepath.Join(cwd, ".env")
		if _, err := os.Stat(cwdEnv); err == nil {
			return cwdEnv
		}
		parentEnv := filepath.Join(cwd, "..", ".env")
		if abs, absErr := filepath.Abs(parentEnv); absErr == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
		if exe, exeErr := os.Executable(); exeErr == nil {
			exeEnv := filepath.Join(filepath.Dir(exe), ".env")
			if _, err := os.Stat(exeEnv); err == nil {
				return exeEnv
			}
		}
		return cwdEnv
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), ".env")
	}
	return ".env"
}

// UpsertEnvFile 写入/更新 .env 键值（不存在则创建），保留其它行与注释。
// 安全时可不写引号。
func UpsertEnvFile(path string, values map[string]string) error {
	if path == "" {
		return fmt.Errorf("empty env file path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !os.IsExist(err) {
		// 目录可能是 "."，忽略即可。
		if filepath.Dir(path) != "." {
			return err
		}
	}

	existing := []string{}
	if raw, err := os.ReadFile(path); err == nil {
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		existing = strings.Split(text, "\n")
		// 去掉 split 产生的尾部空段。
		if len(existing) > 0 && existing[len(existing)-1] == "" {
			existing = existing[:len(existing)-1]
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	pending := map[string]string{}
	for k, v := range values {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		pending[k] = strings.TrimSpace(v)
	}

	out := make([]string, 0, len(existing)+len(pending)+2)
	seen := map[string]struct{}{}
	for _, line := range existing {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			out = append(out, line)
			continue
		}
		work := trimmed
		if strings.HasPrefix(work, "export ") {
			work = strings.TrimSpace(strings.TrimPrefix(work, "export "))
		}
		key, _, ok := strings.Cut(work, "=")
		if !ok {
			out = append(out, line)
			continue
		}
		key = strings.TrimSpace(key)
		if val, hit := pending[key]; hit {
			out = append(out, key+"="+escapeEnvValue(val))
			seen[key] = struct{}{}
			continue
		}
		out = append(out, line)
	}

	// 追加缺失的键。
	ordered := []string{
		"CODEBUDDY_PROXY_API_KEY",
		"CODEBUDDY_PROXY_ADMIN_PASSWORD",
		"CODEBUDDY_PROXY_REQUIRE_API_KEY",
		"CODEBUDDY_SITE",
		"CODEBUDDY_PRODUCT",
		"CODEBUDDY_BASE_URL",
		"CODEBUDDY_INTERNET_ENVIRONMENT",
	}
	for _, key := range ordered {
		if _, ok := seen[key]; ok {
			continue
		}
		val, ok := pending[key]
		if !ok {
			continue
		}
		out = append(out, key+"="+escapeEnvValue(val))
		seen[key] = struct{}{}
	}
	for key, val := range pending {
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, key+"="+escapeEnvValue(val))
	}

	content := strings.Join(out, "\n") + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func escapeEnvValue(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t#\"'`") || strings.Contains(value, "\n") {
		escaped := strings.ReplaceAll(value, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return value
}
