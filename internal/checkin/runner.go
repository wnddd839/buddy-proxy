package checkin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	results  *Log
	pool     *accounts.Pool
	provider *provider.Client
	log      *slog.Logger
	config   func() config.Config
	auto     atomic.Bool
	// jitterKey 是签到时刻抖动的服务端密钥（32 字节）。
	// 它只存在于本机，从不外发 —— 上游即使完全知道抖动算法，
	// 没有这把钥匙也算不出某账号某天几点几分几秒签到。
	jitterKey []byte
	jitterMu  sync.RWMutex
}

// NewRunner 装配调度器。results 为 nil 时退化成「不落盘」，
// 便于测试与「只要调度不要历史」的部署形态。
func NewRunner(file *File, results *Log, pool *accounts.Pool, client *provider.Client, logger *slog.Logger, cfg func() config.Config) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Runner{file: file, results: results, pool: pool, provider: client, log: logger, config: cfg}
	r.auto.Store(cfg().CheckinAuto)
	// 抖动密钥与规则文件同目录；加载失败不致命（退化为进程内随机），
	// 但会打警告 —— 那种情况下重启后排班表会变。
	keyPath := filepath.Join(filepath.Dir(file.Path()), "proxy-checkin-jitter.key")
	key, err := loadOrCreateJitterKey(keyPath)
	if err != nil {
		logger.Warn("checkin: 抖动密钥不可用，本次进程内随机（重启后时刻会变）", "error", err)
		key = randomKey()
	}
	r.jitterKey = key
	return r
}

// Log 暴露结果存储，供管理台查询端点使用。可能为 nil。
func (r *Runner) Log() *Log { return r.results }

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

// Run 等到下一个账号的到点时刻再执行，直到 ctx 结束。
//
// 时刻由 dueAt 掷出（HMAC 抖动，秒级、不可被上游反推），跨重启稳定。
//
// 这里**不能**用固定 ticker 轮询：ticker 会把实际执行时刻量化到栅格上，
// 例如 20 秒 ticker + 掷出的 09:12:37 → 实际在 09:12:40 才发请求，
// 秒级抖动被白白抹掉，剩下的仍是「整齐的整数秒」。改为用 Timer 精确等到
// 目标时刻（最多提前几毫秒，避免时钟漂移导致错过）。
//
// 启动不补签：进程刚起来时把 last 设为当前时刻，之前错过的窗口等明天。
// 上游 todayCheckedIn / code=10001 负责幂等。
func (r *Runner) Run(ctx context.Context) {
	// 启动补跑：把「今天计划时刻已过、但一条记录都没有」的账号补上。
	//
	// 为什么需要：last 初始化成 Now() 会跳过重启前所有窗口。若进程整天没跑，
	// 09:00 排班的账号到 23:00 一次都不会签 —— 实测 70b8cc3 连续两天如此。
	// 只补「完全没尝试过」的，不补失败过的（那是 retryFailed 的职责，且有自己的
	// 间隔与次数上限）；并且不晚于 23:30，避免深夜补签。
	r.catchUp(ctx)

	last := time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		// 先算「下一个到点时刻」；没有规则就退化成 20 秒轮询（等待规则被添加）。
		next, ok := r.nextDueAt(last)
		wait := tickInterval
		if ok {
			wait = time.Until(next)
			if wait < 0 {
				wait = 0
			}
			// 上限保护：规则可能被改到很远（明天），期间要能被新一轮计算唤醒。
			if wait > tickInterval {
				wait = tickInterval
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			now := time.Now()
			r.due(ctx, last, now)
			last = now
		}
	}
}

// nextDueAt 返回所有已启用规则中「严格晚于 after」的最早到点时刻。
// 只用于决定下一次唤醒时机，不改变 due() 的判定语义。
func (r *Runner) nextDueAt(after time.Time) (time.Time, bool) {
	store, err := r.file.Read()
	if err != nil {
		return time.Time{}, false
	}
	rules, err := normalizeRules(store.Rules)
	if err != nil || len(rules) == 0 {
		return time.Time{}, false
	}
	day := after.In(beijing).Format("2006-01-02")
	nextDay := after.In(beijing).AddDate(0, 0, 1).Format("2006-01-02")
	var best time.Time
	found := false
	for _, rule := range rules {
		for _, d := range []string{day, nextDay} {
			at := r.dueAt(rule, d)
			if at.IsZero() || !at.After(after) {
				continue
			}
			if !found || at.Before(best) {
				best, found = at, true
			}
			break // 今天的没到点就够了，不用看明天
		}
	}
	return best, found
}

// catchUp 在进程启动时补跑「今天该签但没签」的账号。
//
// 判定：今天计划时刻 <= now，且今天的签到日志里该账号完全没有记录。
// 有记录的一律跳过（成功的不必再签，失败的归 retryFailed 管）。
// 超过 23:30 不再补，避免深夜惊动上游。
func (r *Runner) catchUp(ctx context.Context) {
	if !r.auto.Load() {
		return
	}
	now := time.Now()
	day := now.In(beijing).Format("2006-01-02")
	// 用 time.Date 而不是 Truncate(24h)：Truncate 按 UTC 纪元对齐，
	// 在北京时区会算出次日 07:30（UTC 23:30），凌晨反而会补签。
	local := now.In(beijing)
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), 23, 30, 0, 0, beijing)
	if now.After(cutoff) {
		return
	}
	store, err := r.file.Read()
	if err != nil {
		r.log.Warn("checkin: 启动补跑读取规则失败", "error", err)
		return
	}
	rules, err := normalizeRules(store.Rules)
	if err != nil || len(rules) == 0 {
		return
	}
	pool, err := r.pool.Read()
	if err != nil {
		r.log.Warn("checkin: 启动补跑读取号池失败", "error", err)
		return
	}
	var pending []normalizedRule
	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		at := r.dueAt(rule, day)
		if at.IsZero() || at.After(now) {
			continue // 还没到点，正常调度会处理
		}
		if r.results.AttemptsToday(rule.AccountID, day) > 0 {
			continue // 今天动过了，交给 retryFailed
		}
		account, found := findAccount(pool, rule.AccountID)
		if !found || !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		pending = append(pending, rule)
	}
	if len(pending) == 0 {
		return
	}
	r.log.Info("checkin: 启动补跑当日漏签账号", "count", len(pending), "day", day)
	cfg := r.config()
	// 同样一轮只放一个，避免启动瞬间并发打上游。
	for _, rule := range pending {
		if ctx.Err() != nil {
			return
		}
		account, found := findAccount(pool, rule.AccountID)
		if !found {
			continue
		}
		at := r.dueAt(rule, day)
		result, degraded, egress := r.runOne(ctx, account, cfg)
		entry := Entry{
			AccountID: account.ID,
			Day:       day,
			At:        at.UnixMilli(),
			RanAt:     time.Now().UnixMilli(),
			OK:        result.OK,
			Already:   result.AlreadyCheckedIn,
			Credits:   result.RewardCredits,
			Message:   result.Message,
			Egress:    egress,
			Degraded:  degraded,
		}
		if err := r.results.Append(entry); err != nil {
			r.log.Warn("checkin: 补跑结果落盘失败", "accountId", account.ID, "error", err)
		}
		r.log.Info("checkin: 补跑完成", "accountId", account.ID,
			"planned", at.In(beijing).Format("15:04:05"),
			"lateMin", int(now.Sub(at).Minutes()),
			"ok", result.OK, "message", result.Message)
		// 一个就够；剩下的等下一轮 catchUp? 不——catchUp 只在启动跑一次，
		// 所以这里要全部补完（数量本来就是「今天漏掉的」，通常个位数）。
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

	// ⚠️ 单轮只放一个账号出去。
	//
	// Run() 的 wait 上限是 tickInterval(20s)，正常情况下每轮只有 1 个账号到点。
	// 但进程卡顿 / GC 停顿 / 时钟跳变会让某一轮的 (prev, now] 变宽，把几十分钟
	// 内的账号全圈进来 —— 实测就出现过 5 个账号在 19:43:47 同一秒签到，
	// 排班表完全失效。限制单轮 1 个之后，剩下的账号会在下一轮（≤20s 后）
	// 依次执行，既不会批量并发，也不会漏。
	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		at := r.dueAt(rule, day)
		if !at.After(prev) || at.After(now) {
			continue
		}
		account, found := findAccount(pool, rule.AccountID)
		if !found || !account.Enabled || !accounts.HasCredentials(account) {
			r.log.Info("checkin: 跳过不可用账号", "accountId", rule.AccountID)
			continue
		}
		cfg := r.config()
		result, degraded, egress := r.runOne(ctx, account, cfg)
		entry := Entry{
			AccountID: account.ID,
			Day:       day,
			// At = 计划时刻（排班表事实），RanAt = 真实执行时刻。
			// 旧实现两者都用 now，导致事后完全看不出是否按点执行。
			At:       at.UnixMilli(),
			RanAt:    now.UnixMilli(),
			OK:       result.OK,
			Already:  result.AlreadyCheckedIn,
			Credits:  result.RewardCredits,
			Message:  result.Message,
			Egress:   egress,
			Degraded: degraded,
		}
		if err := r.results.Append(entry); err != nil {
			r.log.Warn("checkin: 结果落盘失败", "accountId", account.ID, "error", err)
		}
		if result.OK {
			r.log.Info("checkin: 自动签到完成", "accountId", account.ID, "already", result.AlreadyCheckedIn, "credits", result.RewardCredits, "degraded", degraded)
		} else {
			r.log.Warn("checkin: 自动签到未成功", "accountId", account.ID, "message", result.Message, "degraded", degraded)
		}
		// 单轮只放一个「新到点」的账号：剩下的交给下一轮（≤20s 后重算）。
		// 这样即使 (prev, now] 窗口因为卡顿变宽，也不会把多个账号挤在同一秒。
		// 注意用 break 而不是 return —— 重试逻辑本轮仍要跑。
		break
	}
	r.retryFailed(ctx, rules, pool, day, now)
}

// retryFailed 处理当天失败的账号重试。
//
// 状态完全从签到日志推导，不存额外状态文件——这样进程重启后照样能继续重试
// （今天这个服务重启过多次，内存队列会静默丢失重试机会）。
//
// 判定条件（全部满足才重试）：
//  1. 今天有尝试记录
//  2. 今天还没有任何成功记录
//  3. 尝试次数 < 1 + CheckinRetryMax
//  4. 最后一次尝试距今 >= CheckinRetryInterval
//  5. 未超过当天截止时刻（23:30）
func (r *Runner) retryFailed(ctx context.Context, rules []normalizedRule, pool accounts.Store, day string, now time.Time) {
	cfg := r.config()
	if cfg.CheckinRetryInterval <= 0 {
		return
	}
	maxAttempts := 1 + cfg.CheckinRetryMax
	if maxAttempts <= 1 {
		return
	}
	// 当天太晚就不再重试，避免半夜跑签到。
	local := now.In(beijing)
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), 23, 30, 0, 0, beijing)
	if now.After(cutoff) {
		return
	}
	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		account, found := findAccount(pool, rule.AccountID)
		if !found || !account.Enabled || !accounts.HasCredentials(account) {
			continue
		}
		if r.results.SucceededToday(account.ID, day) {
			continue
		}
		attempts := r.results.AttemptsToday(account.ID, day)
		if attempts == 0 || attempts >= maxAttempts {
			continue
		}
		last, ok := r.results.LastAttemptToday(account.ID, day)
		if !ok {
			continue
		}
		if now.Sub(time.UnixMilli(last)) < cfg.CheckinRetryInterval {
			continue
		}

		r.log.Info("checkin: 重试上次失败的签到",
			"accountId", account.ID, "attempt", attempts+1, "maxAttempts", maxAttempts)
		result, degraded, egress := r.runOne(ctx, account, cfg)
		entry := Entry{
			AccountID: account.ID,
			Day:       day,
			// 重试没有排班计划时刻，At = RanAt = 本次执行时刻。
			At:       now.UnixMilli(),
			RanAt:    now.UnixMilli(),
			OK:       result.OK,
			Already:  result.AlreadyCheckedIn,
			Credits:  result.RewardCredits,
			Message:  result.Message,
			Egress:   egress,
			Degraded: degraded,
		}
		if err := r.results.Append(entry); err != nil {
			r.log.Warn("checkin: 重试结果落盘失败", "accountId", account.ID, "error", err)
		}
		if result.OK {
			r.log.Info("checkin: 重试成功", "accountId", account.ID, "credits", result.RewardCredits, "degraded", degraded)
		} else {
			r.log.Warn("checkin: 重试仍失败", "accountId", account.ID, "message", result.Message, "degraded", degraded)
		}
	}
}

// dueAt 计算某账号在某天的签到时刻（北京时间）。
//
// 设计目标（按重要性）：
//  1. **不可被上游反推** —— 上游能观察到签到请求的时刻，也能猜到日期与
//     账号标识；如果时刻是这些量的纯函数（旧实现：FNV-1a(day|id|start|end)），
//     等于把排班表交出去。现在用 HMAC-SHA256 + 服务端私钥，私钥不出本机。
//  2. **跨重启稳定** —— 同一账号同一天必须算回同一时刻，否则每次重启都可能
//     补签一次。密钥落盘保证这一点。
//  3. **时刻自然** —— 避开凌晨、避开整分整秒、按窗口宽度自适应抖动幅度。
func (r *Runner) dueAt(rule normalizedRule, day string) time.Time {
	date, err := time.ParseInLocation("2006-01-02", day, beijing)
	if err != nil {
		return time.Time{}
	}
	key := r.jitterKeySnapshot()

	// 固定时刻（fixed）不加任何抖动：用户明确指定了分秒，就该按那个点跑。
	if rule.Mode == "fixed" {
		base := rule.StartMin*60 + rule.OffsetMin*60
		return date.Add(clampSeconds(base))
	}

	var sec int
	switch rule.Mode {
	case "human":
		sec = humanSecond(key, seedOf(rule, day))
	case "allDay":
		sec = spreadAllDaySecond(key, seedOf(rule, day))
	default: // window
		if rule.EndMin > rule.StartMin {
			sec = windowSecond(key, seedOf(rule, day), rule.StartMin*60, rule.EndMin*60)
		} else {
			sec = rule.StartMin * 60
		}
	}
	// 用户微调：按分钟平移（fixed 已在上面处理过）。
	if rule.Mode != "fixed" {
		sec += rule.OffsetMin * 60
	}
	return date.Add(clampSeconds(sec))
}

// randomKey 生成 32 字节密码学随机密钥。
func randomKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// crypto/rand 不可用时退化为时间派生的弱随机：
		// 宁可弱一点也不能让签到崩掉（且仍有本机私密性）。
		now := time.Now().UnixNano()
		for i := range key {
			key[i] = byte(now >> (uint(i%8) * 8))
		}
	}
	return key
}

// loadOrCreateJitterKey 从磁盘读取抖动密钥，不存在则生成并落盘（0600）。
//
// 为什么必须落盘：签到时刻要跨重启稳定，否则每次重启都可能补签一次。
// 密钥只在本机，绝不写进日志、响应或配置导出 —— 这是「不可被上游反推」的前提。
func loadOrCreateJitterKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		text := strings.TrimSpace(string(raw))
		if key, decErr := hex.DecodeString(text); decErr == nil && len(key) == 32 {
			return key, nil
		}
		return nil, fmt.Errorf("抖动密钥文件格式无效: %s", path)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key := randomKey()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// 先写临时文件再 rename，避免半截写入。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return key, nil
}

// jitterKeySnapshot 安全读取当前密钥。
func (r *Runner) jitterKeySnapshot() []byte {
	r.jitterMu.RLock()
	defer r.jitterMu.RUnlock()
	if len(r.jitterKey) == 0 {
		return []byte("fallback-not-configured")
	}
	return r.jitterKey
}

// seedOf 把「账号 + 窗口」压成抖动的数据部分。
// 注意它不含密钥，所以即使泄露也不足以算出时刻。
func seedOf(rule normalizedRule, day string) string {
	return fmt.Sprintf("%s|%s|%d|%d", day, rule.AccountID, rule.StartMin, rule.EndMin)
}

// clampSeconds 把当日秒数夹进 [0, 24h-1]。
func clampSeconds(sec int) time.Duration {
	if sec < 0 {
		sec = 0
	}
	if max := 24*60*60 - 1; sec > max {
		sec = max
	}
	return time.Duration(sec) * time.Second
}

// hmacUnit 用服务端密钥对 seed 做 HMAC-SHA256，取前 8 字节映射到 [0,1)。
// 没有密钥就无法复现，因此上游无法反推。
func hmacUnit(key []byte, seed string) float64 {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(seed))
	sum := mac.Sum(nil)
	var v uint64
	for i := 0; i < 8; i++ {
		v = v<<8 | uint64(sum[i])
	}
	return float64(v>>11) / float64(1<<53)
}

// hmacUnitN 用不同标签取互相独立的随机量。
func hmacUnitN(key []byte, seed, label string) float64 {
	return hmacUnit(key, seed+"|"+label)
}

// spreadAllDaySecond 为「全天随机」挑一个体面的时刻（秒级）。
//   - 92% 落在 07:00–23:30，8% 落在 00:00–07:00（保留夜间不可预测性）
//   - 秒级精度，不会每天都是 xx:00:00
//
// dayHourWeight 是 24 小时的相对活跃权重。
//
// 旧实现把一天硬切成「夜间 0-7 点（8%）」和「白天 7:00-23:30（92%）」两段，
// 结果 06 时几乎没有样本、07 时突然满格 —— 这个台阶本身就是可识别特征，
// 真人不会有「所有人都在早上 7 点整上线」这种断崖。
//
// 现在改成按小时给权重（近似作息曲线）：深夜极低但不是 0，清晨逐渐上升，
// 白天平稳，晚间有一个小高峰，午夜前后回落。整表线性扫描取一个小时，
// 再在小时内按秒均匀取值。
var dayHourWeight = [24]int{
	2, 1, 1, 1, 1, 2, // 00-05 深夜，权重极低
	4, 9, //            06-07 清晨渐起
	12, 13, 13, 12, //   08-11 上午
	14, 12, //           12-13 午间
	12, 12, 13, //       14-16 下午
	14, 14, 13, //       17-19 晚间高峰
	12, 10, //           20-21
	7, 4, //             22-23 睡前回落
}

// spreadAllDaySecond 按小时权重挑一个时刻，秒级精度。
//
// 与旧实现的区别：不再有「夜间/白天」二元分支，而是 24 个小时统一按权重抽。
// 这样 06→07 是渐变的，深夜也不是严格为 0（保留少量不可预测性）。
func spreadAllDaySecond(key []byte, seed string) int {
	total := 0
	for _, w := range dayHourWeight {
		total += w
	}
	pick := int(hmacUnitN(key, seed, "hour") * float64(total))
	hour := 23
	for h, w := range dayHourWeight {
		if pick < w {
			hour = h
			break
		}
		pick -= w
	}
	// 小时内按秒均匀取值：不同 label 保证与上面的 hour 抽取互相独立。
	offset := int(hmacUnitN(key, seed, "sec") * 3600)
	sec := hour*3600 + offset
	if sec >= 24*3600 {
		sec = 24*3600 - 1
	}
	return sec
}

// windowSecond 在 [lo,hi) 秒内取值，混合均匀与三角分布。
//   - 纯均匀 → 整段铺开，像机器定时
//   - 纯三角 → 全挤在中心，短窗口会变成「都在同一小时」
//   - 混合 → 有峰但不挤
func windowSecond(key []byte, seed string, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	span := float64(hi - lo)
	// 窗口内**均匀**取值。
	//
	// 旧实现混了 0.45 权重的三角分量，把分布压成钟形：实测 09:00-11:00
	// 下前/后 10% 区间各只有 0.6% 的样本、中间 10% 占 17.9%。用户选
	// 「09:00-11:00」的意思就是「这个区间里随便一个点」，却几乎拿不到
	// 边缘时刻（比如想避开整点高峰时完全做不到）。
	//
	// 试过两种"加一点点中心偏好"的方案，都会削掉首尾：
	//   · 三角混合（含反射三角）：三角在两端为 0，混合后边缘必然被压低
	//   · 均匀向中心收缩：p 只覆盖 [0.06,0.94]，首尾各 6% 宽度取不到
	// 根因是任何两端归零的分量都会压低边缘。而窗口模式的语义本来就是
	// 均匀，需要拟人化请用 human 模式，那里有独立的时段权重表。
	u := hmacUnitN(key, seed, "u")
	sec := lo + int(u*span)
	if sec >= hi {
		sec = hi - 1
	}
	return sec
}

// humanActivityBands 是仿人签到的时段与权重：避开凌晨。
// 每段是 [起分钟, 止分钟, 权重]。权重按真实作息设计：
// 起床后、午休、晚间各有一个峰，凌晨与深夜不排。
var humanActivityBands = [][3]int{
	{7 * 60, 9 * 60, 3},   // 早 07:00–09:00 起床
	{9 * 60, 12 * 60, 4},  // 上午
	{12 * 60, 14 * 60, 6}, // 午休高峰
	{14 * 60, 18 * 60, 3}, // 午后
	{18 * 60, 21 * 60, 6}, // 晚间高峰
	{21 * 60, 23 * 60, 2}, // 睡前
}

// humanSecond 用仿人分布取时刻（秒级）：先按权重选时段，再在段内取值。
//
// 段内用「三角 + 均匀」混合，避免整点扎堆又不过度集中。
func humanSecond(key []byte, seed string) int {
	total := 0
	for _, b := range humanActivityBands {
		total += b[2]
	}
	pick := int(hmacUnitN(key, seed, "band") * float64(total))
	for _, b := range humanActivityBands {
		if pick < b[2] {
			return windowSecond(key, seed+"|"+strconv.Itoa(b[0]), b[0]*60, b[1]*60)
		}
		pick -= b[2]
	}
	// 理论到不了：兜底午间。
	return windowSecond(key, seed+"|fallback", 12*60*60, 13*60*60)
}

func findAccount(store accounts.Store, id string) (accounts.Account, bool) {
	for _, account := range store.Accounts {
		if account.ID == id {
			return account, true
		}
	}
	return accounts.Account{}, false
}

// runOne 执行一次签到，返回 (结果, 是否降级, 实际使用的出口)。
//
// 降级只在「账号绑定出口本身出问题」时发生：出口是账号的运维属性，
// 出口挂了不代表号不能签。但 429/额度/上游 5xx 属于账号或上游状态，
// 换出口重试没有意义，还会多烧一次请求。
func (r *Runner) runOne(ctx context.Context, account accounts.Account, cfg config.Config) (billing.CheckinResult, bool, string) {
	egress := account.Proxy
	result := billing.DailyCheckinForAccount(ctx, r.provider, account, cfg)
	if result.OK {
		return result, false, egress
	}
	if !cfg.CheckinAllowDirectFallback || egress == "" || !isEgressError(result.Message) {
		return result, false, egress
	}
	direct := account
	direct.Proxy = ""
	retry := billing.DailyCheckinForAccount(ctx, r.provider, direct, cfg)
	return retry, true, ""
}

// isEgressError 判断签到失败是否属于出口层问题。
// 只认「连不上」类错误；429 / 11140 / 上游 5xx 都不算。
func isEgressError(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	for _, marker := range []string{
		"egress:",
		"i/o timeout",
		"connection refused",
		"connection reset",
		"no route to host",
		"network is unreachable",
		"socks",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// NextRun 返回该账号下一次自动签到的时间（北京时间）。
// 用于管理台展示「下次 HH:MM」，让用户知道什么时候会发生，
// 而不是只能盯着日志猜。规则不存在或未启用时 ok=false。
//
// 注意：这里只按规则算出「理论上的下一次」，不反映上游是否已签到、
// 账号是否被冷却——那些属于运行态，前端另有徽章展示。
func (r *Runner) NextRun(accountID string, now time.Time) (time.Time, bool) {
	store, err := r.file.Read()
	if err != nil {
		return time.Time{}, false
	}
	rules, err := normalizeRules(store.Rules)
	if err != nil {
		return time.Time{}, false
	}
	for _, rule := range rules {
		if rule.AccountID != accountID {
			continue
		}
		day := now.In(beijing).Format("2006-01-02")
		at := r.dueAt(rule, day)
		if at.After(now) {
			return at, true
		}
		// 今天的已经过了，算明天
		next := now.In(beijing).AddDate(0, 0, 1).Format("2006-01-02")
		return r.dueAt(rule, next), true
	}
	return time.Time{}, false
}
