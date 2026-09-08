package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/strutil"
)

var (
	ErrNoAccounts      = errors.New("no enabled CodeBuddy accounts with credentials available")
	ErrAccountNotFound = errors.New("CodeBuddy account not found")
	ErrAccountDisabled = errors.New("CodeBuddy account is disabled")
	ErrNoCredentials   = errors.New("CodeBuddy account has no credentials")
)

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
	ID                  string     `json:"id"`
	Provider            string     `json:"provider"`
	Label               string     `json:"label"`
	Enabled             bool       `json:"enabled"`
	enabledSet          bool       `json:"-"`
	Source              string     `json:"source"`
	Site                string     `json:"site"`
	BaseURL             string     `json:"baseUrl"`
	InternetEnvironment string     `json:"internetEnvironment"`
	APIEndpoint         string     `json:"apiEndpoint,omitempty"`
	ChatCompletionsPath string     `json:"chatCompletionsPath,omitempty"`
	Domain              string     `json:"domain,omitempty"`
	EnterpriseID        string     `json:"enterpriseId,omitempty"`
	TenantID            string     `json:"tenantId,omitempty"`
	DepartmentFullName  string     `json:"departmentFullName,omitempty"`
	Transport           string     `json:"transport"`
	AuthType            string     `json:"authType"`
	APIKey              string     `json:"apiKey,omitempty"`
	BearerToken         string     `json:"bearerToken,omitempty"`
	RefreshToken        string     `json:"refreshToken,omitempty"`
	TokenExpiresAt      int64      `json:"tokenExpiresAt,omitempty"`
	CredentialHash      string     `json:"credentialHash,omitempty"`
	AuthStatus          AuthStatus `json:"authStatus"`
	CreatedAt           int64      `json:"createdAt"`
	UpdatedAt           int64      `json:"updatedAt"`
	LastUsedAt          int64      `json:"lastUsedAt,omitempty"`
	LastSelectedAt      int64      `json:"lastSelectedAt,omitempty"`
	SuccessRequests     int64      `json:"successRequests"`
	FailedRequests      int64      `json:"failedRequests"`
	LastError           string     `json:"lastError,omitempty"`
	CooldownUntil       int64      `json:"cooldownUntil,omitempty"`
	QuotaRemaining      *float64   `json:"quotaRemaining,omitempty"`
	QuotaUnlimited      bool       `json:"quotaUnlimited,omitempty"`
	QuotaResetAt        int64      `json:"quotaResetAt,omitempty"`
	QuotaCheckedAt      int64      `json:"quotaCheckedAt,omitempty"`
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
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
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
	if err := p.writeDisk(snap); err != nil {
		p.mu.Lock()
		p.dirty = true
		p.mu.Unlock()
		return err
	}
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
		QuotaRemaining:      copyFloatPtr(raw.QuotaRemaining),
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

func copyFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

const quotaCacheMaxAge = 6 * time.Hour

// QuotaBlocked reports whether cached billing says the account is exhausted until QuotaResetAt.
func QuotaBlocked(account Account, nowMillis int64) bool {
	if account.QuotaUnlimited {
		return false
	}
	if account.QuotaRemaining == nil {
		return false
	}
	if *account.QuotaRemaining > 0 {
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

func unavailableUntil(account Account, nowMillis int64) int64 {
	until := account.CooldownUntil
	if QuotaBlocked(account, nowMillis) && account.QuotaResetAt > until {
		until = account.QuotaResetAt
	}
	return until
}

func applyQuotaFields(account Account, remaining *float64, unlimited bool, resetAt, checkedAt int64, nowMillis int64) Account {
	account.QuotaRemaining = copyFloatPtr(remaining)
	account.QuotaUnlimited = unlimited
	account.QuotaResetAt = resetAt
	account.QuotaCheckedAt = checkedAt
	if !unlimited && remaining != nil && *remaining <= 0 && resetAt > nowMillis && resetAt > account.CooldownUntil {
		account.CooldownUntil = resetAt
	}
	return account
}

func (p *Pool) Select(opts SelectOptions) (Selection, error) {
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
			store.Accounts[i].LastSelectedAt = now
			p.mem = store
			p.markDirtyLocked()
			return Selection{Source: "pool", Account: store.Accounts[i], Index: i, Store: cloneStore(store)}, nil
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
	var (
		fallbackIdx   = -1
		fallbackUntil int64
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
		if blockedUntil := unavailableUntil(account, now); blockedUntil > now {
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
		return Selection{Source: "pool", Account: store.Accounts[idx], Index: idx, Store: cloneStore(store)}, nil
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
		}, nil
	}
	if opts.Site != "" {
		return Selection{}, fmt.Errorf("%w for site=%s", ErrNoAccounts, config.NormalizeSite(opts.Site))
	}
	return Selection{}, ErrNoAccounts
}

type SelectOptions struct {
	AccountID  string
	Site       string
	ExcludeIDs []string
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
			p.mem.Accounts[i].QuotaRemaining = nil
			p.mem.Accounts[i].QuotaUnlimited = false
			p.mem.Accounts[i].QuotaResetAt = 0
			p.mem.Accounts[i].QuotaCheckedAt = 0
		} else {
			p.mem.Accounts[i].FailedRequests++
			p.mem.Accounts[i].LastError = strutil.Truncate(errMsg, 600)
			if cooldown > 0 {
				p.mem.Accounts[i].CooldownUntil = now + cooldown.Milliseconds()
			}
		}
		p.markDirtyLocked()
		return nil
	}
	return nil
}

// ApplyQuotaState 将上游 billing 快照写入账号，并在配额耗尽时对齐 cooldownUntil。
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
		p.mem.Accounts[i] = applyQuotaFields(p.mem.Accounts[i], remaining, unlimited, resetAt, checkedAt, now)
		updated = p.mem.Accounts[i]
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
			normalized.QuotaRemaining = copyFloatPtr(existing.QuotaRemaining)
			normalized.QuotaUnlimited = existing.QuotaUnlimited
			normalized.QuotaResetAt = existing.QuotaResetAt
			normalized.QuotaCheckedAt = existing.QuotaCheckedAt
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

func (p *Pool) ReplaceAccount(account Account) (Account, Store, error) {
	p.mu.Lock()
	if err := p.ensureLoadedLocked(); err != nil {
		p.mu.Unlock()
		return Account{}, Store{}, err
	}
	for i := range p.mem.Accounts {
		if p.mem.Accounts[i].ID != account.ID {
			continue
		}
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
	return Account{}, out, fmt.Errorf("%w: %s", ErrAccountNotFound, account.ID)
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
	LastError           string   `json:"lastError,omitempty"`
	TokenExpiresAt      int64    `json:"tokenExpiresAt,omitempty"`
	TokenExpired        bool     `json:"tokenExpired"`
	CooldownUntil       int64    `json:"cooldownUntil,omitempty"`
	QuotaRemaining      *float64 `json:"quotaRemaining,omitempty"`
	QuotaUnlimited      bool     `json:"quotaUnlimited,omitempty"`
	QuotaResetAt        int64    `json:"quotaResetAt,omitempty"`
	QuotaCheckedAt      int64    `json:"quotaCheckedAt,omitempty"`
	QuotaExhausted      bool     `json:"quotaExhausted"`
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
		QuotaRemaining:      copyFloatPtr(account.QuotaRemaining),
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
		OK:                 true,
		Provider:           "codebuddy",
		Version:            store.Version,
		NextIndex:          store.NextIndex,
		AccountsPath:       path,
		Count:              len(accounts),
		EnabledCount:       enabled,
		DisabledCount:      len(accounts) - enabled,
		DomesticCount:      domestic,
		GlobalCount:        global,
		ActiveSite:         preferredSite,
		ActiveEnabledCount: activeEnabled,
		LoggedIn:           loggedIn,
		Primary:            primary,
		Accounts:           accounts,
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
