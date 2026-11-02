package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/egress"
	"github.com/wnddd839/codebuddy-proxy/internal/httputil"
	"github.com/wnddd839/codebuddy-proxy/internal/proxies"
)

type proxyCandidateView struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Label   string `json:"label,omitempty"`
	AddedAt int64  `json:"addedAt,omitempty"`
}

type proxyAccountRef struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Site    string `json:"site"`
	Enabled bool   `json:"enabled"`
}

type proxyUsageView struct {
	ID       string            `json:"id"`
	Proxy    string            `json:"proxy"`
	Count    int               `json:"count"`
	Accounts []proxyAccountRef `json:"accounts"`
}

// handleProxiesList 汇总「候选出口」与「账号实际绑定情况」。
// 出口 URL 一律打码后回显，明文只存在磁盘与内存里。
func (s *Server) handleProxiesList(w http.ResponseWriter, r *http.Request) {
	store, err := s.Svc.Pool.Read()
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	candidates, err := s.Proxies.Read()
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	views := make([]proxyCandidateView, 0, len(candidates.Proxies))
	for _, entry := range candidates.Proxies {
		views = append(views, proxyCandidateView{
			ID:      proxies.ID(entry.URL),
			URL:     egress.Mask(entry.URL),
			Label:   entry.Label,
			AddedAt: entry.AddedAt,
		})
	}
	grouped := map[string][]proxyAccountRef{}
	direct := 0
	for _, account := range store.Accounts {
		if strings.TrimSpace(account.Proxy) == "" {
			direct++
			continue
		}
		key := strings.TrimSpace(account.Proxy)
		grouped[key] = append(grouped[key], proxyAccountRef{
			ID:      account.ID,
			Label:   account.Label,
			Site:    config.NormalizeSite(account.Site),
			Enabled: account.Enabled,
		})
	}
	used := make([]proxyUsageView, 0, len(grouped))
	for raw, list := range grouped {
		sort.Slice(list, func(i, j int) bool { return list[i].Label < list[j].Label })
		used = append(used, proxyUsageView{
			ID:       proxies.ID(raw),
			Proxy:    egress.Mask(raw),
			Count:    len(list),
			Accounts: list,
		})
	}
	sort.Slice(used, func(i, j int) bool { return used[i].Proxy < used[j].Proxy })
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"candidates":  views,
		"used":        used,
		"direct":      direct,
		"total":       len(store.Accounts),
		"proxiesPath": s.Proxies.Path(),
	})
}

// handleProxyCheck 用真实请求验证一条出口（或某个账号当前出口）。
// body: {"url":"socks5h://..."} 或 {"accountId":"..."}；两者都空 = 直连对照。
func (s *Server) handleProxyCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL         string `json:"url"`
		CandidateID string `json:"candidateId"`
		AccountID   string `json:"accountId"`
		Compare     *bool  `json:"compare"`
	}
	if err := httputil.ReadJSON(r, &body); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	raw, resolveErr := s.resolveProxyRef(body.URL, body.CandidateID)
	if resolveErr != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": resolveErr.Error()})
		return
	}
	if raw == "" && strings.TrimSpace(body.AccountID) != "" {
		store, err := s.Svc.Pool.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		found := false
		for _, account := range store.Accounts {
			if account.ID == body.AccountID {
				raw = account.Proxy
				found = true
				break
			}
		}
		if !found {
			httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "account not found"})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	compare := raw != ""
	if body.Compare != nil {
		compare = *body.Compare
	}
	var via, direct egress.ProbeResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		via = egress.Probe(ctx, raw)
	}()
	if compare {
		wg.Add(1)
		go func() {
			defer wg.Done()
			direct = egress.Probe(ctx, "")
		}()
	}
	wg.Wait()
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        via.OK,
		"proxy":     egress.Mask(raw),
		"ip":        via.IP,
		"elapsedMs": via.ElapsedMS,
		"error":     via.Error,
		"direct":    direct,
	})
}

// handleProxyCandidate 维护候选出口清单（proxies.json）。
func (s *Server) handleProxyCandidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL   string `json:"url"`
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if err := httputil.ReadJSON(r, &body); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	target := strings.TrimSpace(body.URL)
	if target == "" && strings.TrimSpace(body.ID) != "" {
		entry, ok, err := s.Proxies.Find(body.ID)
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if !ok {
			httputil.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "出口不存在"})
			return
		}
		target = entry.URL
	}
	if target == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "url 不能为空"})
		return
	}
	var (
		next proxies.Store
		err  error
	)
	if r.Method == http.MethodDelete {
		next, err = s.Proxies.Remove(target)
	} else {
		next, err = s.Proxies.Add(target, body.Label)
	}
	if err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	views := make([]proxyCandidateView, 0, len(next.Proxies))
	for _, entry := range next.Proxies {
		views = append(views, proxyCandidateView{
			ID:      proxies.ID(entry.URL),
			URL:     egress.Mask(entry.URL),
			Label:   entry.Label,
			AddedAt: entry.AddedAt,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "candidates": views})
}

// handleProxyBalance 把一批账号轮流分配到候选出口上（同一出口上的账号数尽量平均）。
// body: {"site":"domestic","accountIds":[...],"proxies":[...],"onlyEnabled":true}
func (s *Server) handleProxyBalance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Site        string   `json:"site"`
		AccountIDs  []string `json:"accountIds"`
		Proxies     []string `json:"proxies"`
		OnlyEnabled *bool    `json:"onlyEnabled"`
	}
	if err := httputil.ReadJSON(r, &body); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	pool := append([]string(nil), body.Proxies...)
	if len(pool) == 0 {
		candidates, err := s.Proxies.Read()
		if err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		for _, entry := range candidates.Proxies {
			pool = append(pool, entry.URL)
		}
	}
	clean := make([]string, 0, len(pool))
	seen := map[string]bool{}
	for _, candidate := range pool {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		if _, err := egress.Parse(candidate); err != nil {
			httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		seen[candidate] = true
		clean = append(clean, candidate)
	}
	if len(clean) == 0 {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "没有可用出口：先在候选清单里添加，或在 proxies 里传 URL"})
		return
	}
	store, err := s.Svc.Pool.Read()
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	site := config.NormalizeSite(body.Site)
	wanted := map[string]bool{}
	for _, id := range body.AccountIDs {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	onlyEnabled := true
	if body.OnlyEnabled != nil {
		onlyEnabled = *body.OnlyEnabled
	}
	targets := make([]accounts.Account, 0, len(store.Accounts))
	for _, account := range store.Accounts {
		if len(wanted) > 0 {
			if !wanted[account.ID] {
				continue
			}
		} else {
			if site != "" && config.NormalizeSite(account.Site) != site {
				continue
			}
			if onlyEnabled && !account.Enabled {
				continue
			}
		}
		targets = append(targets, account)
	}
	if len(targets) == 0 {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "没有匹配的账号"})
		return
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	assignments := make(map[string]string, len(targets))
	for i, account := range targets {
		assignments[account.ID] = clean[i%len(clean)]
	}
	_, snapshot, err := s.Svc.Pool.AssignProxies(assignments)
	if err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	result := make([]map[string]any, 0, len(targets))
	for _, account := range targets {
		result = append(result, map[string]any{
			"id":    account.ID,
			"label": account.Label,
			"proxy": egress.Mask(assignments[account.ID]),
		})
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"assigned":    len(targets),
		"proxies":     len(clean),
		"assignments": result,
		"accounts":    accounts.SummarizeStore(snapshot, s.Svc.Pool.Path()),
	})
}

// resolveProxyRef 把管理台传来的引用解析成真实 URL：明文 url 优先，其次按候选清单短 ID 查。
// 两者都为空时返回空串，表示直连。
func (s *Server) resolveProxyRef(raw, candidateID string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" || strings.TrimSpace(candidateID) == "" {
		return raw, nil
	}
	entry, ok, err := s.Proxies.Find(candidateID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("出口不存在：%s", candidateID)
	}
	return entry.URL, nil
}
