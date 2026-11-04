package checkin

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/billing"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// 统一北京时间，不依赖进程本地时区（Docker 默认 UTC）。
var beijing = time.FixedZone("UTC+8", 8*60*60)

const tickInterval = 20 * time.Second

// Runner 每天按规则逐账号签到。规则与总开关在运行时可从管理台更新。
type Runner struct {
	file     *File
	pool     *accounts.Pool
	provider *provider.Client
	log      *slog.Logger
	config   func() config.Config
	auto     atomic.Bool
}

func NewRunner(file *File, pool *accounts.Pool, client *provider.Client, log *slog.Logger, cfg func() config.Config) *Runner {
	if log == nil {
		log = slog.Default()
	}
	r := &Runner{file: file, pool: pool, provider: client, log: log, config: cfg}
	r.auto.Store(cfg().CheckinAuto)
	return r
}

func (r *Runner) Path() string { return r.file.Path() }

func (r *Runner) Auto() bool { return r.auto.Load() }

func (r *Runner) Rules() (Store, error) { return r.file.Read() }

func (r *Runner) SetRules(store Store) error { return r.file.Write(store) }

// SetAuto 持久化总开关并让运行中的调度器立即生效（管理台保存时调用）。
func (r *Runner) SetAuto(enabled bool) error {
	value := strconv.FormatBool(enabled)
	if err := config.UpsertEnvFile(config.ResolveEnvFilePath(), map[string]string{
		"CODEBUDDY_PROXY_CHECKIN_AUTO": value,
	}); err != nil {
		return fmt.Errorf("checkin: 保存总开关失败: %w", err)
	}
	_ = os.Setenv("CODEBUDDY_PROXY_CHECKIN_AUTO", value)
	r.auto.Store(enabled)
	return nil
}

// Run 每 20 秒检查一次是否有账号到点，直到 ctx 结束。
//
// 每个账号在当天按「日期 + 账号ID + 窗口」确定性地掷出一个时刻，跨重启稳定；
// 只在该时刻落在本次 tick 区间内时执行。启动不补签：进程刚起来时把 last 设为
// 当前时刻，之前错过的窗口等明天。上游 todayCheckedIn / code=10001 负责幂等。
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			r.due(ctx, last, now)
			last = now
		}
	}
}

// due 只处理 (prev, now] 之间到点的规则，不落盘「上次跑到哪天」。
func (r *Runner) due(ctx context.Context, prev, now time.Time) {
	if !r.auto.Load() {
		return
	}
	store, err := r.file.Read()
	if err != nil {
		r.log.Warn("checkin: 规则读取失败", "error", err)
		return
	}
	rules, err := normalizeRules(store.Rules)
	if err != nil {
		r.log.Warn("checkin: 规则无效", "error", err)
		return
	}
	if len(rules) == 0 {
		return
	}
	pool, err := r.pool.Read()
	if err != nil {
		r.log.Warn("checkin: 号池读取失败", "error", err)
		return
	}
	day := now.In(beijing).Format("2006-01-02")

	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		at := dueAt(rule, day)
		if !at.After(prev) || at.After(now) {
			continue
		}
		account, found := findAccount(pool, rule.AccountID)
		if !found || !account.Enabled || !accounts.HasCredentials(account) {
			r.log.Info("checkin: 跳过不可用账号", "accountId", rule.AccountID)
			continue
		}
		result := billing.DailyCheckinForAccount(ctx, r.provider, account, r.config())
		if result.OK {
			r.log.Info("checkin: 自动签到完成", "accountId", account.ID, "already", result.AlreadyCheckedIn, "credits", result.RewardCredits)
		} else {
			r.log.Warn("checkin: 自动签到未成功", "accountId", account.ID, "message", result.Message)
		}
	}
}

// dueAt 按日期 + 账号 + 窗口播种，保证同一天每次 tick 得到同一时刻。
// End == Start 时就是固定时刻；否则在窗口内按分钟随机。
func dueAt(rule normalizedRule, day string) time.Time {
	date, err := time.ParseInLocation("2006-01-02", day, beijing)
	if err != nil {
		return time.Time{}
	}
	offset := 0
	if rule.EndMin > rule.StartMin {
		h := fnv.New64a()
		_, _ = h.Write([]byte(fmt.Sprintf("%s|%s|%d|%d", day, rule.AccountID, rule.StartMin, rule.EndMin)))
		offset = int(h.Sum64() % uint64(rule.EndMin-rule.StartMin+1))
	}
	return date.Add(time.Duration(rule.StartMin+offset) * time.Minute)
}

func findAccount(store accounts.Store, id string) (accounts.Account, bool) {
	for _, account := range store.Accounts {
		if account.ID == id {
			return account, true
		}
	}
	return accounts.Account{}, false
}
