// Command invariant-audit 回放决策日志，回答「以前有没有中过这些不变量」。
//
// 独立二进制而不是塞进主程序：回放是离线动作，不该把参数、依赖与
// 启动路径混进服务进程。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
	"github.com/wnddd839/codebuddy-proxy/internal/invariant"
)

func main() {
	dir := flag.String("dir", "", "决策日志目录（通常是 /opt/buddy-proxy）")
	since := flag.String("since", "", "起始日期 YYYY-MM-DD（默认 7 天前）")
	until := flag.String("until", "", "结束日期 YYYY-MM-DD（默认今天）")
	only := flag.String("only", "", "只看这些断言，逗号分隔（如 I3,I5）")
	asJSON := flag.Bool("json", false, "输出 JSON")
	flag.Parse()

	if strings.TrimSpace(*dir) == "" {
		fmt.Fprintln(os.Stderr, "需要 -dir（决策日志目录）")
		os.Exit(2)
	}

	now := time.Now()
	from, err := parseDay(*since, now.AddDate(0, 0, -7))
	if err != nil {
		fmt.Fprintf(os.Stderr, "-since 格式错误：%v\n", err)
		os.Exit(2)
	}
	to, err := parseDay(*until, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "-until 格式错误：%v\n", err)
		os.Exit(2)
	}
	// 结束日取当天末尾，否则「今天」会漏掉此刻之后的记录。
	to = to.Add(24*time.Hour - time.Nanosecond)

	var onlyIDs []string
	for _, part := range strings.Split(*only, ",") {
		if id := strings.TrimSpace(part); id != "" {
			onlyIDs = append(onlyIDs, id)
		}
	}

	// 只读打开：审计工具不得因为 -dir 拼错就在别人机器上建出目录。
	store, err := decision.OpenStoreReadOnly(*dir, 30*24*time.Hour)
	if err != nil {
		fmt.Fprintf(os.Stderr, "目录不可读：%v\n", err)
		os.Exit(1)
	}

	report, err := invariant.Replay(store, from, to, onlyIDs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "回放失败：%v\n", err)
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "输出失败：%v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Print(invariant.FormatReplay(report))
}

func parseDay(s string, fallback time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Date(fallback.Year(), fallback.Month(), fallback.Day(), 0, 0, 0, 0, time.Local), nil
	}
	return time.ParseInLocation("2006-01-02", s, time.Local)
}
