package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/atomicwrite"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/egress"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

var (
	ErrNoAccounts         = errors.New("no enabled CodeBuddy accounts with credentials available")
	ErrAccountNotFound    = errors.New("CodeBuddy account not found")
	ErrAccountDisabled    = errors.New("CodeBuddy account is disabled")
	ErrNoCredentials      = errors.New("CodeBuddy account has no credentials")
	ErrAccountCoolingDown = errors.New("CodeBuddy account is cooling down")
	ErrQuotaBelowFloor    = errors.New("CodeBuddy account is below the quota floor")
)

// CooldownUntil 的两种来源。见 Account.CooldownSource。
const (
	CooldownSourceQuota    = "quota"
	CooldownSourceUpstream = "upstream"
)

// normalizeCooldownSource 归一冷却来源，并把加字段之前写入的旧数据分类。
// 旧数据没有标记，但额度冷却的 CooldownUntil 与 QuotaResetAt 精确相等，
// 可以据此无歧义地认出来。
func normalizeCooldownSource(source string, cooldownUntil, quotaResetAt int64) string {
	if cooldownUntil <= 0 {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case CooldownSourceQuota:
		return CooldownSourceQuota
	case CooldownSourceUpstream:
		return CooldownSourceUpstream
	}
	if quotaResetAt > 0 && cooldownUntil == quotaResetAt {
		return CooldownSourceQuota
	}
	return CooldownSourceUpstream
}

// ModelCooldownError 表示当前模型的所有候选账号仍在上游限流窗口内。
// 与「账号整体冷却」区分：换模型还有救，不该降级打上游。
type ModelCooldownError struct {
	Model string
	Until int64
}

func (e *ModelCooldownError) Error() string {
	return fmt.Sprintf("CodeBuddy model rate limit: model=%s will reset at %s",
		e.Model, time.UnixMilli(e.Until).UTC().Format(time.RFC3339))
}

func (e *ModelCooldownError) Unwrap() error { return ErrAccountCoolingDown }

// RetryAfterHeader 给出建议的等待秒数（至少 1 秒）。
func (e *ModelCooldownError) RetryAfterHeader() string {
	secs := (e.Until - time.Now().UnixMilli()) / 1000
	if secs < 1 {
		secs = 1
	}
	return fmt.Sprintf("%d", secs)
}

func (a *Account) UnmarshalJSON(data []byte) error {
	type alias Account
	aux := &struct {
		Enabled *bool `json:"enabled"`
		*alias
	}{alias: (*alias)(a)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Enabled == nil {
		a.Enabled = true
		a.enabledSet = false
	} else {
		a.Enabled = *aux.Enabled
		a.enabledSet = true
	}
	return nil
}

type AuthStatus struct {
	LoggedIn     *bool  `json:"loggedIn,omitempty"`
	UserID       string `json:"userId,omitempty"`
	UserName     string `json:"userName,omitempty"`
	UserNickname string `json:"userNickname,omitempty"`
	AuthMode     string `json:"authMode,omitempty"`
}

type Account struct {
	ID                  string `json:"id"`
	Provider            string `json:"provider"`
	Label               string `json:"label"`
	Enabled             bool   `json:"enabled"`
	enabledSet          bool   `json:"-"`
	Source              string `json:"source"`
	Site                string `json:"site"`
	BaseURL             string `json:"baseUrl"`
	InternetEnvironment string `json:"internetEnvironment"`
	APIEndpoint         string `json:"apiEndpoint,omitempty"`
	ChatCompletionsPath string `json:"chatCompletionsPath,omitempty"`
	Domain              string `json:"domain,omitempty"`
	EnterpriseID        string `json:"enterpriseId,omitempty"`
	TenantID            string `json:"tenantId,omitempty"`
	DepartmentFullName  string `json:"departmentFullName,omitempty"`
	Transport           string `json:"transport"`
	// Proxy 是该账号的专用出口（空 = 直连）。形如 socks5h://user:pass@host:port。
	// 绑死后不随请求漂移：同号换 IP 比同 IP 多号更容易触发风控。
	Proxy           string     `json:"proxy,omitempty"`
	AuthType        string     `json:"authType"`
	APIKey          string     `json:"apiKey,omitempty"`
	BearerToken     string     `json:"bearerToken,omitempty"`
	RefreshToken    string     `json:"refreshToken,omitempty"`
	TokenExpiresAt  int64      `json:"tokenExpiresAt,omitempty"`
	CredentialHash  string     `json:"credentialHash,omitempty"`
	AuthStatus      AuthStatus `json:"authStatus"`
	CreatedAt       int64      `json:"createdAt"`
	UpdatedAt       int64      `json:"updatedAt"`
	LastUsedAt      int64      `json:"lastUsedAt,omitempty"`
	LastSelectedAt  int64      `json:"lastSelectedAt,omitempty"`
	SuccessRequests int64      `json:"successRequests"`
	FailedRequests  int64      `json:"failedRequests"`
	LastError       string     `json:"lastError,omitempty"`
	CooldownUntil   int64      `json:"cooldownUntil,omitempty"`
	// CooldownSource 记录冷却的来由，决定它能不能自动解除：
	//   quota    —— 上游额度耗尽，冷却到 QuotaResetAt；余额恢复就成了脏数据，必须清掉。
	//   upstream —— 请求被上游限流，只能靠一次成功请求（MarkResult ok）解除。
	// 空值按 upstream 处理，兼容加字段之前写入的数据。
	CooldownSource string   `json:"cooldownSource,omitempty"`
	QuotaRemaining *float64 `json:"quotaRemaining,omitempty"`
	QuotaUnlimited bool     `json:"quotaUnlimited,omitempty"`
	QuotaResetAt   int64    `json:"quotaResetAt,omitempty"`
	QuotaCheckedAt int64    `json:"quotaCheckedAt,omitempty"`

	ModelCooldowns map[string]int64 `json:"modelCooldowns,omitempty"`
}

type Store struct {
	Version   int       `json:"version"`
	Provider  string    `json:"provider"`
	NextIndex int       `json:"nextIndex"`
	Accounts  []Account `json:"accounts"`
}

type Selection struct {
	Source           string
	Account          Account
	Index            int
	Store            Store
	BypassedCooldown bool // 全部账号冷却时降级选最快恢复者
	Model            string
	ModelRateLimited bool
}

type Pool struct {
	path string

	// mu 保护内存态；persistMu 与写盘串行，避免并发 rename 冲突。
	mu       sync.RWMutex
	mem      Store
	loaded   bool
	dirty    bool
	dirReady bool

	persistMu sync.Mutex
	wakeCh    chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}
	closeOnce sync.Once

	// quotaFloorMilli 是选号时积分低于该值的账号直接跳过（0 = 不启用）。
	// 存「积分×1000」的整数（atomic 无 Float64 类型），管理台热更新时写入。
	quotaFloorMilli atomic.Int64
}

// SetQuotaFloor 热更新积分阈值（并发安全）。负值按 0 处理。
func (p *Pool) SetQuotaFloor(credits float64) {
	if p == nil {
		return
	}
	if credits < 0 {
		credits = 0
	}
	p.quotaFloorMilli.Store(int64(credits * 1000))
}

// QuotaFloor 返回当前阈值（积分）。
func (p *Pool) QuotaFloor() float64 {
	if p == nil {
		return 0
	}
	return float64(p.quotaFloorMilli.Load()) / 1000
}

func NewPool(path string) *Pool {
	p := &Pool{
		path:   path,
		mem:    EmptyStore(),
		wakeCh: make(chan struct{}, 1),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	go p.flushLoop()
	return p
}

func (p *Pool) Path() string { return p.path }

func EmptyStore() Store {
	return Store{Version: 1, Provider: "codebuddy", NextIndex: 0, Accounts: []Account{}}
}

func cloneStore(store Store) Store {
	out := store
	if store.Accounts != nil {
		out.Accounts = append([]Account(nil), store.Accounts...)
		for i := range out.Accounts {
			out.Accounts[i].ModelCooldowns = maps.Clone(out.Accounts[i].ModelCooldowns)
		}
	}
	return out
}

// mergeByID 把磁盘上本进程还没有的账号吸收进内存快照。
// 同一 id 以 mem 为准（热字段和凭据都是本进程正在服务的）；磁盘独有 id 追加；内存独有 id 保留。
func mergeByID(disk, mem Store) Store {
	out := cloneStore(mem)
	index := make(map[string]struct{}, len(out.Accounts))
	for _, acc := range out.Accounts {
		if acc.ID != "" {
			index[acc.ID] = struct{}{}
		}
	}
	for _, acc := range disk.Accounts {
		if acc.ID == "" || !HasCredentials(acc) {
			continue
		}
		if _, exists := index[acc.ID]; exists {
			continue
		}
		index[acc.ID] = struct{}{}
		out.Accounts = append(out.Accounts, acc)
	}
	return out
}

func (p *Pool) Read() (Store, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureLoadedLocked(); err != nil {
		return Store{}, err
	}
	return cloneStore(p.mem), nil
}

func (p *Pool) Write(store Store) (Store, error) {
	normalized := NormalizeStore(store)
	p.mu.Lock()
	p.mem = cloneStore(normalized)
	p.loaded = true
	p.dirty = false
	snap := cloneStore(p.mem)
	p.mu.Unlock()
	if err := p.writeDisk(snap); err != nil {
		p.mu.Lock()
		p.dirty = true
		p.mu.Unlock()
		p.kickFlush()
		return Store{}, err
	}
	return cloneStore(normalized), nil
}

func (p *Pool) ensureLoadedLocked() error {
	if p.loaded {
		return nil
	}
	store, err := p.readDisk()
	if err != nil {
		return err
	}
	p.mem = store
	p.loaded = true
	p.dirty = false
	return nil
}

func (p *Pool) readDisk() (Store, error) {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return EmptyStore(), nil
	}
	if err != nil {
		return Store{}, err
	}
	var raw Store
	if err := json.Unmarshal(data, &raw); err != nil {
		return Store{}, err
	}
	return NormalizeStore(raw), nil
}

func (p *Pool) writeDisk(store Store) error {
	p.persistMu.Lock()
	defer p.persistMu.Unlock()
	if !p.dirReady {
		if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
			return err
		}
		p.dirReady = true
	}
	payload, err := json.Marshal(store)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return atomicwrite.Write(p.path, payload, 0o600)
}

func (p *Pool) markDirtyLocked() {
	p.dirty = true
	p.kickFlush()
}

func (p *Pool) kickFlush() {
	select {
	case p.wakeCh <- struct{}{}:
	default:
	}
}

func (p *Pool) Flush() error {
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return err
	}
	if !p.dirty {
		p.mu.Unlock()
		return nil
	}
	snap := cloneStore(p.mem)
	p.dirty = false
	p.mu.Unlock()

	disk, err := p.readDisk()
	merged := snap
	if err == nil {
		merged = mergeByID(disk, snap)
	}

	if err := p.writeDisk(merged); err != nil {
		p.mu.Lock()
		p.dirty = true
		p.mu.Unlock()
		return err
	}
	p.mu.Lock()
	p.mem = mergeByID(merged, p.mem)
	p.loaded = true
	p.mu.Unlock()
	return nil
}

func (p *Pool) Close() error {
	var err error
	p.closeOnce.Do(func() {
		close(p.stopCh)
		<-p.doneCh
		err = p.Flush()
	})
	return err
}

func (p *Pool) flushLoop() {
	// 250ms 防抖合并高频 MarkResult；Stop 时正确 drain timer，避免 goroutine 泄漏。
	defer close(p.doneCh)
	for {
		select {
		case <-p.stopCh:
			_ = p.Flush()
			return
		case <-p.wakeCh:
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-timer.C:
			case <-p.stopCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				_ = p.Flush()
				return
			}
			for {
				select {
				case <-p.wakeCh:
				default:
					goto flushed
				}
			}
		flushed:
			_ = p.Flush()
		}
	}
}

func NormalizeStore(store Store) Store {
	out := EmptyStore()
	if store.Version > 0 {
		out.Version = store.Version
	}
	for _, raw := range store.Accounts {
		account := NormalizeAccount(raw, time.Now().UnixMilli())
		if !HasCredentials(account) {
			continue
		}
		out.Accounts = append(out.Accounts, account)
	}
	if len(out.Accounts) == 0 {
		out.NextIndex = 0
		return out
	}
	next := store.NextIndex % len(out.Accounts)
	if next < 0 {
		next += len(out.Accounts)
	}
	out.NextIndex = next
	return out
}

func NormalizeAccount(raw Account, now int64) Account {
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	site := config.NormalizeSite(raw.Site)
	if site == "" {
		site = inferSite(raw)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/")
	if baseURL == "" {
		if site == "domestic" {
			baseURL = "https://www.codebuddy.cn"
		} else {
			baseURL = "https://www.codebuddy.ai"
		}
	}
	internet := strings.TrimSpace(raw.InternetEnvironment)
	if internet == "" {
		if site == "domestic" {
			internet = "domestic"
		} else {
			internet = "public"
		}
	}
	auth := AuthStatus{
		LoggedIn:     raw.AuthStatus.LoggedIn,
		UserID:       strutil.Compact(raw.AuthStatus.UserID),
		UserName:     strutil.Compact(raw.AuthStatus.UserName),
		UserNickname: strutil.Compact(raw.AuthStatus.UserNickname),
		AuthMode:     strutil.Compact(raw.AuthStatus.AuthMode),
	}
	bearer := strutil.Compact(raw.BearerToken)
	apiKey := strutil.Compact(raw.APIKey)
	authType := ""
	switch {
	case bearer != "":
		authType = "bearer"
	case apiKey != "":
		authType = "api_key"
	}
	credHash := strutil.Compact(raw.CredentialHash)
	if credHash == "" {
		credHash = hashSecret(bearer + apiKey)
	}
	identity := strutil.First(auth.UserID, auth.UserName, raw.Label)
	id := strutil.Compact(raw.ID)
	if id == "" {
		id = hashSecret(fmt.Sprintf("%s|%s|%s|%s", identity, credHash, baseURL, site))
	}
	created := raw.CreatedAt
	if created == 0 {
		created = now
	}
	updated := raw.UpdatedAt
	if updated == 0 {
		updated = now
	}
	label := strutil.Compact(raw.Label)
	if label == "" {
		label = strutil.First(auth.UserName, auth.UserID, "CodeBuddy "+id[:min(6, len(id))])
	}
	enabled := raw.Enabled
	if !raw.enabledSet && raw.ID == "" && raw.CreatedAt == 0 {
		enabled = true
	}
	transport := strutil.Compact(raw.Transport)
	if transport == "" {
		transport = config.DefaultTransport
	}
	if transport != "protocol_direct" {
		transport = config.DefaultTransport
	}

	// 保留非空出口配置，读取旧文件时也不能因配置错误悄悄回落到直连。
	proxy := strings.TrimSpace(raw.Proxy)

	expiresAt := raw.TokenExpiresAt
	account := Account{
		ID:                  id,
		Provider:            "codebuddy",
		Label:               label,
		Enabled:             enabled,
		Source:              strutil.First(raw.Source, "pool"),
		Site:                site,
		BaseURL:             baseURL,
		InternetEnvironment: internet,
		APIEndpoint:         strings.TrimRight(strutil.Compact(raw.APIEndpoint), "/"),
		ChatCompletionsPath: strutil.Compact(raw.ChatCompletionsPath),
		Domain:              strutil.Compact(raw.Domain),
		EnterpriseID:        strutil.Compact(raw.EnterpriseID),
		TenantID:            strutil.First(strutil.Compact(raw.TenantID), strutil.Compact(raw.EnterpriseID)),
		DepartmentFullName:  strutil.Compact(raw.DepartmentFullName),
		Transport:           transport,
		Proxy:               proxy,
		AuthType:            authType,
		APIKey:              apiKey,
		BearerToken:         bearer,
		RefreshToken:        strutil.Compact(raw.RefreshToken),
		TokenExpiresAt:      expiresAt,
		CredentialHash:      credHash,
		AuthStatus:          auth,
		CreatedAt:           created,
		UpdatedAt:           updated,
		LastUsedAt:          raw.LastUsedAt,
		LastSelectedAt:      raw.LastSelectedAt,
		SuccessRequests:     raw.SuccessRequests,
		FailedRequests:      raw.FailedRequests,
		LastError:           strutil.Compact(raw.LastError),
		CooldownUntil:       raw.CooldownUntil,
		CooldownSource:      normalizeCooldownSource(raw.CooldownSource, raw.CooldownUntil, raw.QuotaResetAt),
		ModelCooldowns:      maps.Clone(raw.ModelCooldowns),
		QuotaRemaining:      cloneFloatPtr(raw.QuotaRemaining),
		QuotaUnlimited:      raw.QuotaUnlimited,
		QuotaResetAt:        raw.QuotaResetAt,
		QuotaCheckedAt:      raw.QuotaCheckedAt,
	}
	return account
}

// CreateAccount 构建规范化账号；未显式设置 enabled 时默认为 true。
func CreateAccount(raw Account) Account {
	now := time.Now().UnixMilli()
	if !raw.enabledSet {
		raw.Enabled = true
		raw.enabledSet = true
	}
	if raw.CreatedAt == 0 {
		raw.CreatedAt = now
	}
	raw.UpdatedAt = now
	if raw.Transport == "" {
		raw.Transport = config.DefaultTransport
	}
	return NormalizeAccount(raw, now)
}

func HasCredentials(account Account) bool {
	return strutil.Compact(account.BearerToken) != "" || strutil.Compact(account.APIKey) != ""
}

func cloneFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return new(*v)
}

const quotaCacheMaxAge = 6 * time.Hour

// QuotaBlocked reports whether cached billing says the account is exhausted until QuotaResetAt.
func QuotaBlocked(account Account, nowMillis int64) bool {
	return quotaBlockedFloor(account, nowMillis, 0)
}

// QuotaBelowFloor 报告账号积分是否低于阈值（floor>0 时启用）。
// 低于阈值 = 不再参与选号，即使还没到 0——避免把号打到负数或触发上游风控。
// 与 QuotaBlocked 同一数据源（缓存的 billing credits）与同一时效约束。
func QuotaBelowFloor(account Account, nowMillis int64, floor float64) bool {
	if floor <= 0 || account.QuotaUnlimited || account.QuotaRemaining == nil {
		return false
	}
	// Unknown/stale billing is not proof of a low balance; refresh it before selection.
	if account.QuotaCheckedAt <= 0 || nowMillis-account.QuotaCheckedAt > quotaCacheMaxAge.Milliseconds() {
		return false
	}
	if account.QuotaResetAt > 0 && account.QuotaResetAt <= nowMillis {
		return false
	}
	return *account.QuotaRemaining < floor
}

func quotaBlockedFloor(account Account, nowMillis int64, floor float64) bool {
	if account.QuotaUnlimited || account.QuotaRemaining == nil || *account.QuotaRemaining > floor {
		return false
	}
	if account.QuotaResetAt <= nowMillis {
		return false
	}
	if account.QuotaCheckedAt > 0 && nowMillis-account.QuotaCheckedAt > quotaCacheMaxAge.Milliseconds() {
		return false
	}
	return true
}

func unavailableUntil(account Account, nowMillis int64, model string) int64 {
	until := max(account.CooldownUntil, account.ModelCooldowns[model])
	if QuotaBlocked(account, nowMillis) {
		until = max(until, account.QuotaResetAt)
	}
	return until
}

// UsableForModel 是「这个号现在能不能真的被 Select 选中」的唯一判据。
//
// 之前网关里有 5 份各自实现的候选筛选（listSelectableAccounts /
// nextSelectableAccount / hasAlternativeAccount / siteHasUsableAccount /
// listQuotaRefreshCandidates），检查项各不相同，导致同一个号在一处算「可用」、
// 在另一处算「不可用」。典型后果：换号重试挑中一个额度已耗尽的号，
// 白打一次上游再失败。任何判断候选的代码都必须走这里。
//
// 注意：这里不包含 Select 的「全冷却兜底」——那个兜底只在确实无号时降级，
// 不属于「有可用候选」的语义。
func UsableForModel(account Account, nowMillis int64, model string, floor float64) bool {
	if !account.Enabled || !HasCredentials(account) {
		return false
	}
	if QuotaBelowFloor(account, nowMillis, floor) {
		return false
	}
	return unavailableUntil(account, nowMillis, model) <= nowMillis
}

func clearQuotaCache(account *Account) {
	account.QuotaRemaining = nil
	account.QuotaUnlimited = false
	account.QuotaResetAt = 0
	account.QuotaCheckedAt = 0
}

func (p *Pool) Select(opts SelectOptions) (Selection, error) {
	floor := p.QuotaFloor()
	return p.selectFloor(opts, floor)
}

func (p *Pool) selectFloor(opts SelectOptions, floor float64) (Selection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureLoadedLocked(); err != nil {
		return Selection{}, err
	}
	store := p.mem
	now := time.Now().UnixMilli()
	if opts.AccountID != "" {
		for i, account := range store.Accounts {
			if account.ID != opts.AccountID {
				continue
			}
			if !account.Enabled {
				return Selection{}, fmt.Errorf("%w: %s", ErrAccountDisabled, opts.AccountID)
			}
			if !HasCredentials(account) {
				return Selection{}, fmt.Errorf("%w: %s", ErrNoCredentials, opts.AccountID)
			}
			if opts.Site != "" && config.NormalizeSite(account.Site) != config.NormalizeSite(opts.Site) {
				return Selection{}, fmt.Errorf("CodeBuddy account site mismatch: account=%s, configured=%s", account.Site, config.NormalizeSite(opts.Site))
			}
			if QuotaBelowFloor(account, now, floor) {
				return Selection{}, fmt.Errorf("%w: %s", ErrQuotaBelowFloor, opts.AccountID)
			}
			if opts.CheckCooldown && unavailableUntil(account, now, opts.Model) > now {
				return Selection{}, fmt.Errorf("%w: %s", ErrAccountCoolingDown, opts.AccountID)
			}
			store.Accounts[i].LastSelectedAt = now
			p.mem = store
			p.markDirtyLocked()
			return Selection{Source: "pool", Account: store.Accounts[i], Index: i, Store: cloneStore(store), Model: opts.Model}, nil
		}
		return Selection{}, fmt.Errorf("%w: %s", ErrAccountNotFound, opts.AccountID)
	}

	if len(store.Accounts) == 0 {
		return Selection{}, ErrNoAccounts
	}
	exclude := map[string]struct{}{}
	for _, id := range opts.ExcludeIDs {
		exclude[id] = struct{}{}
	}
	if opts.PreferQuota {
		if sel, ok := p.pickHighestQuotaLocked(&store, now, opts.Site, opts.Model, exclude); ok {
			p.mem = store
			p.markDirtyLocked()
			return sel, nil
		}
	}
	var (
		fallbackIdx   = -1
		fallbackUntil int64
		// modelResetAt 记录「该模型」最早解除限流的时刻，用于区分
		// 「号池真空了」与「只是这个模型在限流窗口内」。
		modelResetAt int64
	)
	for offset := 0; offset < len(store.Accounts); offset++ {
		idx := (store.NextIndex + offset) % len(store.Accounts)
		account := store.Accounts[idx]
		if !account.Enabled || !HasCredentials(account) {
			continue
		}
		if opts.Site != "" && config.NormalizeSite(account.Site) != config.NormalizeSite(opts.Site) {
			continue
		}
		if _, skip := exclude[account.ID]; skip {
			continue
		}
		// 上游已明确限制该模型：不能借「全冷却降级」提前再打一次上游，
		// 只记录解除时刻，供调用方返回 Retry-After。
		if until := account.ModelCooldowns[opts.Model]; until > now {
			if modelResetAt == 0 || until < modelResetAt {
				modelResetAt = until
			}
			continue
		}
		// The floor is a hard exclusion, never an all-cooling fallback candidate.
		if QuotaBelowFloor(account, now, floor) {
			continue
		}
		if blockedUntil := unavailableUntil(account, now, opts.Model); blockedUntil > now {
			if blockedUntil > 0 && (fallbackIdx < 0 || blockedUntil < fallbackUntil) {
				fallbackIdx = idx
				fallbackUntil = blockedUntil
			}
			continue
		}
		store.Accounts[idx].LastSelectedAt = now
		store.NextIndex = (idx + 1) % len(store.Accounts)
		p.mem = store
		p.markDirtyLocked()
		return Selection{Source: "pool", Account: store.Accounts[idx], Index: idx, Store: cloneStore(store), Model: opts.Model}, nil
	}
	// 全部候选都只是「这个模型」在限流窗口内 ⇒ 不降级、明确报错。
	if modelResetAt > 0 && fallbackIdx < 0 {
		return Selection{}, &ModelCooldownError{Model: opts.Model, Until: modelResetAt}
	}
	if fallbackIdx >= 0 {
		store.Accounts[fallbackIdx].LastSelectedAt = now
		store.NextIndex = (fallbackIdx + 1) % len(store.Accounts)
		p.mem = store
		p.markDirtyLocked()
		return Selection{
			Source:           "pool",
			Account:          store.Accounts[fallbackIdx],
			Index:            fallbackIdx,
			Store:            cloneStore(store),
			BypassedCooldown: true,
			Model:            opts.Model,
		}, nil
	}
	if opts.Site != "" {
		return Selection{}, fmt.Errorf("%w for site=%s", ErrNoAccounts, config.NormalizeSite(opts.Site))
	}
	return Selection{}, ErrNoAccounts
}

func quotaScore(account Account) (float64, bool) {
	if account.QuotaUnlimited {
		return 1e18, true
	}
	if account.QuotaRemaining == nil {
		return 0, false
	}
	return *account.QuotaRemaining, true
}

func (p *Pool) pickHighestQuotaLocked(store *Store, now int64, site, model string, exclude map[string]struct{}) (Selection, bool) {
	floor := p.QuotaFloor()
	bestIdx := -1
	var bestRemaining float64
	hasQuota := false
	for i, account := range store.Accounts {
		if !account.Enabled || !HasCredentials(account) {
			continue
		}
		if site != "" && config.NormalizeSite(account.Site) != config.NormalizeSite(site) {
			continue
		}
		if _, skip := exclude[account.ID]; skip {
			continue
		}
		if QuotaBelowFloor(account, now, floor) || unavailableUntil(account, now, model) > now {
			continue
		}
		remaining, ok := quotaScore(account)
		if !ok {
			continue
		}
		if !hasQuota || remaining > bestRemaining {
			hasQuota = true
			bestRemaining = remaining
			bestIdx = i
		}
	}
	if !hasQuota || bestIdx < 0 {
		return Selection{}, false
	}
	store.Accounts[bestIdx].LastSelectedAt = now
	store.NextIndex = (bestIdx + 1) % len(store.Accounts)
	return Selection{Source: "pool", Account: store.Accounts[bestIdx], Index: bestIdx, Store: cloneStore(*store), Model: model}, true
}

type SelectOptions struct {
	AccountID     string
	Site          string
	ExcludeIDs    []string
	PreferQuota   bool
	Model         string
	CheckCooldown bool // 会话软钉号检查冷却；显式固定账号仍可直接请求。
}

// InCooldown reports whether the account is temporarily excluded from pool rotation.
func InCooldown(account Account, nowMillis int64) bool {
	return account.CooldownUntil > 0 && account.CooldownUntil > nowMillis
}

func (p *Pool) MarkResult(selection Selection, ok bool, errMsg string, cooldown time.Duration) error {
	if selection.Source != "pool" || selection.Account.ID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureLoadedLocked(); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for i := range p.mem.Accounts {
		if p.mem.Accounts[i].ID != selection.Account.ID {
			continue
		}
		p.mem.Accounts[i].LastUsedAt = now
		if ok {
			p.mem.Accounts[i].SuccessRequests++
			p.mem.Accounts[i].LastError = ""
			p.mem.Accounts[i].CooldownUntil = 0
			p.mem.Accounts[i].CooldownSource = ""
			p.mem.Accounts[i].ModelCooldowns = maps.Clone(p.mem.Accounts[i].ModelCooldowns)
			delete(p.mem.Accounts[i].ModelCooldowns, selection.Model)
			// 全冷却降级打成功，说明「耗尽」快照已经过期；正常成功不能清额度，否则 PreferQuota 立刻失效。
			if selection.BypassedCooldown {
				clearQuotaCache(&p.mem.Accounts[i])
			}
		} else {
			p.mem.Accounts[i].FailedRequests++
			p.mem.Accounts[i].LastError = strutil.Truncate(errMsg, 600)
			if cooldown > 0 {
				if selection.ModelRateLimited && selection.Model != "" {
					cooldowns := maps.Clone(p.mem.Accounts[i].ModelCooldowns)
					if cooldowns == nil {
						cooldowns = make(map[string]int64)
					}
					cooldowns[selection.Model] = now + cooldown.Milliseconds()
					p.mem.Accounts[i].ModelCooldowns = cooldowns
				} else {
					until := now + cooldown.Milliseconds()
					// 额度类失败时调用方（resolveFailureCooldown）已经用上游 billing
					// 快照把 CooldownUntil 对齐到 QuotaResetAt 并标了 quota 来源。
					// 这里必须让位，否则每次真实 429 都会把可自愈的额度冷却改回
					// upstream，自愈逻辑等于没写（旧实现就是这么废掉两个号的）。
					//
					// 判据不能比时间戳：until 是 now+cooldown，而 quota 冷却记的是
					// 上游给的 resetAt，两次取 now 之间有毫秒级漂移，精确比较会误判。
					// 只要现有冷却还没到期、且来源是 quota，就说明额度探测给出的
					// 时间窗仍然有效，保留它。
					keepQuota := p.mem.Accounts[i].CooldownSource == CooldownSourceQuota &&
						p.mem.Accounts[i].CooldownUntil > now
					if !keepQuota {
						p.mem.Accounts[i].CooldownUntil = until
						p.mem.Accounts[i].CooldownSource = CooldownSourceUpstream
					}
				}
			}
		}
		p.markDirtyLocked()
		return nil
	}
	return nil
}

// ApplyQuotaState 将上游 billing 快照写入账号，并让 cooldownUntil 跟着额度走。
//
// 只设不清是旧实现的硬伤：上游提前补回额度后，账号仍会冷却到 resetAt，
// 而冷却号选不中 ⇒ 永远拿不到一次成功 ⇒ 永远解不开（实测两个号 886 积分
// 白废 4 天）。现在按来源区分：quota 冷却在余额恢复的瞬间自动解除，
// upstream 冷却（真实限流）不受影响。
func (p *Pool) ApplyQuotaState(accountID string, remaining *float64, unlimited bool, resetAt, checkedAt int64) (Account, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Account{}, fmt.Errorf("%w: empty id", ErrAccountNotFound)
	}
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return Account{}, err
	}
	now := time.Now().UnixMilli()
	found := false
	var updated Account
	for i := range p.mem.Accounts {
		if p.mem.Accounts[i].ID != accountID {
			continue
		}
		account := &p.mem.Accounts[i]
		// 没有来源标记的旧数据：额度冷却的指纹是 CooldownUntil 与 QuotaResetAt
		// 精确相等（MarkResult 写的是 now+固定时长，撞不上上游给的整点重置）。
		legacyQuotaCooldown := account.CooldownSource == "" &&
			account.CooldownUntil > 0 && account.CooldownUntil == account.QuotaResetAt
		exhausted := !unlimited && remaining != nil && *remaining <= 0
		recovered := unlimited || (remaining != nil && *remaining > 0)
		account.QuotaRemaining = cloneFloatPtr(remaining)
		account.QuotaUnlimited = unlimited
		account.QuotaResetAt = resetAt
		account.QuotaCheckedAt = checkedAt
		switch {
		case exhausted && resetAt > now && resetAt > account.CooldownUntil:
			account.CooldownUntil = resetAt
			account.CooldownSource = CooldownSourceQuota
		case recovered && (account.CooldownSource == CooldownSourceQuota || legacyQuotaCooldown):
			account.CooldownUntil = 0
			account.CooldownSource = ""
		}
		updated = *account
		found = true
		break
	}
	if !found {
		p.mu.Unlock()
		return Account{}, fmt.Errorf("%w: %s", ErrAccountNotFound, accountID)
	}
	p.markDirtyLocked()
	p.mu.Unlock()
	return updated, nil
}

func (p *Pool) Upsert(account Account) (Account, Store, error) {
	if !HasCredentials(account) {
		return Account{}, Store{}, ErrNoCredentials
	}
	if _, err := egress.Parse(account.Proxy); err != nil {
		return Account{}, Store{}, err
	}
	normalized := CreateAccount(account)
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return Account{}, Store{}, err
	}
	store := p.mem
	replaced := false
	for i, existing := range store.Accounts {
		if existing.ID == normalized.ID ||
			(normalized.CredentialHash != "" && existing.CredentialHash == normalized.CredentialHash) ||
			(normalized.AuthStatus.UserID != "" &&
				existing.AuthStatus.UserID == normalized.AuthStatus.UserID &&
				config.NormalizeSite(existing.Site) == config.NormalizeSite(normalized.Site)) {
			normalized.ID = existing.ID
			normalized.CreatedAt = existing.CreatedAt
			normalized.SuccessRequests = existing.SuccessRequests
			normalized.FailedRequests = existing.FailedRequests
			normalized.LastUsedAt = existing.LastUsedAt
			normalized.LastSelectedAt = existing.LastSelectedAt
			normalized.CooldownUntil = existing.CooldownUntil
			normalized.ModelCooldowns = maps.Clone(existing.ModelCooldowns)
			normalized.QuotaRemaining = cloneFloatPtr(existing.QuotaRemaining)
			normalized.QuotaUnlimited = existing.QuotaUnlimited
			normalized.QuotaResetAt = existing.QuotaResetAt
			normalized.QuotaCheckedAt = existing.QuotaCheckedAt
			// 导入数据里没有出口字段时保留原绑定：重新导入不该把号扔回本机 IP。
			// 要解绑请走 POST /accounts/{id}/proxy {clear:true}。
			if strings.TrimSpace(normalized.Proxy) == "" {
				normalized.Proxy = existing.Proxy
			}
			store.Accounts[i] = normalized
			replaced = true
			break
		}
	}
	if !replaced {
		store.Accounts = append(store.Accounts, normalized)
	}
	store = NormalizeStore(store)
	p.mem = store
	p.dirty = false
	snap := cloneStore(store)
	p.mu.Unlock()
	if err := p.writeDisk(snap); err != nil {
		p.mu.Lock()
		p.dirty = true
		p.mu.Unlock()
		return Account{}, Store{}, err
	}
	for _, item := range snap.Accounts {
		if item.ID == normalized.ID {
			return item, cloneStore(snap), nil
		}
	}
	return normalized, cloneStore(snap), nil
}

func (p *Pool) Delete(id string) (Store, error) {
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return Store{}, err
	}
	store := p.mem
	next := make([]Account, 0, len(store.Accounts))
	found := false
	for _, account := range store.Accounts {
		if account.ID == id {
			found = true
			continue
		}
		next = append(next, account)
	}
	if !found {
		out := cloneStore(store)
		p.mu.Unlock()
		return out, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	store.Accounts = next
	store = NormalizeStore(store)
	p.mem = store
	p.dirty = false
	snap := cloneStore(store)
	p.mu.Unlock()
	if err := p.writeDisk(snap); err != nil {
		p.mu.Lock()
		p.dirty = true
		p.mu.Unlock()
		return Store{}, err
	}
	return cloneStore(snap), nil
}

func (p *Pool) SetEnabled(id string, enabled bool) (Account, Store, error) {
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return Account{}, Store{}, err
	}
	for i := range p.mem.Accounts {
		if p.mem.Accounts[i].ID != id {
			continue
		}
		p.mem.Accounts[i].Enabled = enabled
		p.mem.Accounts[i].UpdatedAt = time.Now().UnixMilli()
		acc := p.mem.Accounts[i]
		p.dirty = false
		snap := cloneStore(p.mem)
		p.mu.Unlock()
		if err := p.writeDisk(snap); err != nil {
			p.mu.Lock()
			p.dirty = true
			p.mu.Unlock()
			return Account{}, Store{}, err
		}
		return acc, cloneStore(snap), nil
	}
	out := cloneStore(p.mem)
	p.mu.Unlock()
	return Account{}, out, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
}

// SetProxy 给单个账号绑定出口（空字符串 = 改回直连）。
func (p *Pool) SetProxy(id, proxy string) (Account, Store, error) {
	return p.AssignProxies(map[string]string{id: proxy})
}

// AssignProxies 批量绑定出口，只落一次盘。proxy 为空表示改回直连；
// 未出现在 map 里的账号保持原状。
func (p *Pool) AssignProxies(proxies map[string]string) (Account, Store, error) {
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return Account{}, Store{}, err
	}
	now := time.Now().UnixMilli()
	target := map[string]string{}
	for id, raw := range proxies {
		proxy := strings.TrimSpace(raw)
		if proxy != "" {
			if _, err := egress.Parse(proxy); err != nil {
				out := cloneStore(p.mem)
				p.mu.Unlock()
				return Account{}, out, err
			}
		}
		target[id] = proxy
	}
	var first Account
	changed := 0
	found := false
	for i := range p.mem.Accounts {
		proxy, ok := target[p.mem.Accounts[i].ID]
		if !ok {
			continue
		}
		if !found {
			first = p.mem.Accounts[i]
			found = true
		}
		p.mem.Accounts[i].Proxy = proxy
		p.mem.Accounts[i].UpdatedAt = now
		first = p.mem.Accounts[i]
		changed++
	}
	if changed == 0 {
		out := cloneStore(p.mem)
		p.mu.Unlock()
		return Account{}, out, fmt.Errorf("%w: %s", ErrAccountNotFound, "no matching account")
	}
	p.dirty = false
	snap := cloneStore(p.mem)
	p.mu.Unlock()
	if err := p.writeDisk(snap); err != nil {
		p.mu.Lock()
		p.dirty = true
		p.mu.Unlock()
		return Account{}, Store{}, err
	}
	return first, cloneStore(snap), nil
}

func (p *Pool) ReplaceAccount(account Account) (Account, Store, error) {
	return p.UpdateAccount(account.ID, func(Account) Account { return account })
}

// UpdateAccount applies a local field update to the latest account under the pool lock.
// The callback must not perform network or disk I/O.
func (p *Pool) UpdateAccount(id string, update func(Account) Account) (Account, Store, error) {
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return Account{}, Store{}, err
	}
	for i := range p.mem.Accounts {
		if p.mem.Accounts[i].ID != id {
			continue
		}
		account := update(p.mem.Accounts[i])
		if !HasCredentials(account) {
			p.mu.Unlock()
			return Account{}, Store{}, ErrNoCredentials
		}
		account.ID = id
		account.UpdatedAt = time.Now().UnixMilli()
		p.mem.Accounts[i] = account
		p.mem = NormalizeStore(p.mem)
		p.dirty = false
		snap := cloneStore(p.mem)
		p.mu.Unlock()
		if err := p.writeDisk(snap); err != nil {
			p.mu.Lock()
			p.dirty = true
			p.mu.Unlock()
			return Account{}, Store{}, err
		}
		return account, cloneStore(snap), nil
	}
	out := cloneStore(p.mem)
	p.mu.Unlock()
	return Account{}, out, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
}

type Summary struct {
	ID                  string `json:"id"`
	Provider            string `json:"provider"`
	Label               string `json:"label"`
	Enabled             bool   `json:"enabled"`
	Source              string `json:"source"`
	Site                string `json:"site"`
	BaseURL             string `json:"baseUrl"`
	InternetEnvironment string `json:"internetEnvironment"`
	APIEndpoint         string `json:"apiEndpoint,omitempty"`
	ChatCompletionsPath string `json:"chatCompletionsPath,omitempty"`
	Transport           string `json:"transport"`
	// Proxy 回显为打码后的 URL（密码替换成 ***），前端据此显示「已绑出口」。
	Proxy               string `json:"proxy,omitempty"`
	AuthType            string `json:"authType"`
	HasCredentials      bool   `json:"hasCredentials"`
	LoggedIn            bool   `json:"loggedIn"`
	UserID              string `json:"userId,omitempty"`
	UserName            string `json:"userName,omitempty"`
	UserNickname        string `json:"userNickname,omitempty"`
	AuthMode            string `json:"authMode,omitempty"`
	BearerTokenPreview  string `json:"bearerTokenPreview,omitempty"`
	RefreshTokenPreview string `json:"refreshTokenPreview,omitempty"`
	APIKeyPreview       string `json:"apiKeyPreview,omitempty"`
	CreatedAt           int64  `json:"createdAt"`
	UpdatedAt           int64  `json:"updatedAt"`
	LastUsedAt          int64  `json:"lastUsedAt,omitempty"`
	LastSelectedAt      int64  `json:"lastSelectedAt,omitempty"`
	SuccessRequests     int64  `json:"successRequests"`
	FailedRequests      int64  `json:"failedRequests"`
	LastError           string `json:"lastError,omitempty"`
	TokenExpiresAt      int64  `json:"tokenExpiresAt,omitempty"`
	TokenExpired        bool   `json:"tokenExpired"`
	CooldownUntil       int64  `json:"cooldownUntil,omitempty"`
	// CooldownSource 让管理台区分「额度冷却（会自动解除）」与「限流冷却」。
	CooldownSource string   `json:"cooldownSource,omitempty"`
	QuotaRemaining *float64 `json:"quotaRemaining,omitempty"`
	QuotaUnlimited bool     `json:"quotaUnlimited,omitempty"`
	QuotaResetAt   int64    `json:"quotaResetAt,omitempty"`
	QuotaCheckedAt int64    `json:"quotaCheckedAt,omitempty"`
	QuotaExhausted bool     `json:"quotaExhausted"`

	// 签到状态由调用方（服务端）注入：账号池本身不读签到日志，
	// 保持 accounts 包与 checkin 包互不依赖。
	// 取值 none / done / already / failed。
	CheckinToday    string `json:"checkinToday,omitempty"`
	CheckinCount48h int    `json:"checkinCount48h"`
	CheckinLastErr  string `json:"checkinLastError,omitempty"`
	// CheckinNextAt 是下一次自动签到的时刻（毫秒时间戳）。0 表示未启用规则。
	CheckinNextAt int64 `json:"checkinNextAt,omitempty"`

	ModelCooldowns map[string]int64 `json:"modelCooldowns,omitempty"`
}

func SummarizeAccount(account Account) Summary {
	loggedIn := HasCredentials(account)
	if account.AuthStatus.LoggedIn != nil {
		loggedIn = *account.AuthStatus.LoggedIn && HasCredentials(account)
	}
	return Summary{
		ID:                  account.ID,
		Provider:            "codebuddy",
		Label:               account.Label,
		Enabled:             account.Enabled,
		Source:              account.Source,
		Site:                account.Site,
		BaseURL:             account.BaseURL,
		InternetEnvironment: account.InternetEnvironment,
		APIEndpoint:         account.APIEndpoint,
		ChatCompletionsPath: account.ChatCompletionsPath,
		Transport:           account.Transport,
		Proxy:               egress.Mask(account.Proxy),
		AuthType:            account.AuthType,
		HasCredentials:      HasCredentials(account),
		LoggedIn:            loggedIn,
		UserID:              account.AuthStatus.UserID,
		UserName:            account.AuthStatus.UserName,
		UserNickname:        account.AuthStatus.UserNickname,
		AuthMode:            account.AuthStatus.AuthMode,
		BearerTokenPreview:  strutil.MaskSecret(account.BearerToken, 6),
		RefreshTokenPreview: strutil.MaskSecret(account.RefreshToken, 4),
		APIKeyPreview:       strutil.MaskSecret(account.APIKey, 6),
		CreatedAt:           account.CreatedAt,
		UpdatedAt:           account.UpdatedAt,
		LastUsedAt:          account.LastUsedAt,
		LastSelectedAt:      account.LastSelectedAt,
		SuccessRequests:     account.SuccessRequests,
		FailedRequests:      account.FailedRequests,
		LastError:           account.LastError,
		TokenExpiresAt:      account.TokenExpiresAt,
		TokenExpired:        account.TokenExpiresAt > 0 && account.TokenExpiresAt <= time.Now().UnixMilli(),
		CooldownUntil:       account.CooldownUntil,
		CooldownSource:      account.CooldownSource,
		ModelCooldowns:      maps.Clone(account.ModelCooldowns),
		QuotaRemaining:      cloneFloatPtr(account.QuotaRemaining),
		QuotaUnlimited:      account.QuotaUnlimited,
		QuotaResetAt:        account.QuotaResetAt,
		QuotaCheckedAt:      account.QuotaCheckedAt,
		QuotaExhausted:      QuotaBlocked(account, time.Now().UnixMilli()),
	}
}

type StoreSummary struct {
	OK                 bool      `json:"ok"`
	Provider           string    `json:"provider"`
	Version            int       `json:"version"`
	NextIndex          int       `json:"nextIndex"`
	AccountsPath       string    `json:"accountsPath"`
	Count              int       `json:"count"`
	EnabledCount       int       `json:"enabledCount"`
	DisabledCount      int       `json:"disabledCount"`
	DomesticCount      int       `json:"domesticCount"`
	GlobalCount        int       `json:"globalCount"`
	ActiveSite         string    `json:"activeSite,omitempty"`
	ActiveEnabledCount int       `json:"activeEnabledCount"`
	LoggedIn           bool      `json:"loggedIn"`
	Primary            *Summary  `json:"primary"`
	Accounts           []Summary `json:"accounts"`

	ActiveAvailableCount int `json:"activeAvailableCount"` // 不含整号冷却/配额阻塞；模型限制另计。
}

func SummarizeStore(store Store, path string) StoreSummary {
	return SummarizeStoreForSite(store, path, "")
}

func SummarizeStoreForSite(store Store, path, preferredSite string) StoreSummary {
	preferredSite = strings.TrimSpace(preferredSite)
	if preferredSite != "" {
		preferredSite = config.NormalizeSite(preferredSite)
	}
	accounts := make([]Summary, 0, len(store.Accounts))
	enabled := 0
	domestic := 0
	global := 0
	activeEnabled := 0
	activeAvailable := 0
	now := time.Now().UnixMilli()
	loggedIn := false
	var primary *Summary
	var fallback *Summary
	for _, account := range store.Accounts {
		summary := SummarizeAccount(account)
		accounts = append(accounts, summary)
		site := config.NormalizeSite(summary.Site)
		if site == "domestic" {
			domestic++
		} else {
			global++
		}
		if summary.Enabled {
			enabled++
			if preferredSite == "" || site == preferredSite {
				activeEnabled++
				// 未指定模型的汇总只排除整号阻塞；模型冷却单独展示。
				if summary.HasCredentials && unavailableUntil(account, now, "") <= now {
					activeAvailable++
				}
			}
			if summary.HasCredentials && summary.LoggedIn {
				if preferredSite == "" || site == preferredSite {
					loggedIn = true
				}
			}
			if summary.HasCredentials {
				copy := summary
				if preferredSite != "" && site == preferredSite && primary == nil {
					primary = &copy
				}
				if fallback == nil {
					fallback = &copy
				}
				if preferredSite == "" && primary == nil {
					primary = &copy
				}
			}
		}
	}
	if primary == nil {
		primary = fallback
	}
	if primary == nil && len(accounts) > 0 {
		copy := accounts[0]
		primary = &copy
	}
	return StoreSummary{
		OK:                   true,
		Provider:             "codebuddy",
		Version:              store.Version,
		NextIndex:            store.NextIndex,
		AccountsPath:         path,
		Count:                len(accounts),
		EnabledCount:         enabled,
		DisabledCount:        len(accounts) - enabled,
		DomesticCount:        domestic,
		GlobalCount:          global,
		ActiveSite:           preferredSite,
		ActiveEnabledCount:   activeEnabled,
		ActiveAvailableCount: activeAvailable,
		LoggedIn:             loggedIn,
		Primary:              primary,
		Accounts:             accounts,
	}
}

func inferSite(raw Account) string {
	env := strings.ToLower(strutil.Compact(raw.InternetEnvironment))
	if env == "internal" {
		return "domestic"
	}
	url := strings.ToLower(strutil.Compact(raw.BaseURL))
	if strings.Contains(url, "codebuddy.cn") || strings.Contains(url, "copilot.tencent.com") {
		return "domestic"
	}
	return config.NormalizeSite(raw.Site)
}

func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}
