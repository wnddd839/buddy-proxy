package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/accounts"
	"github.com/wnddd839/codebuddy-proxy/internal/config"
	"github.com/wnddd839/codebuddy-proxy/internal/provider"
)

// Locks the JSON shape of the batch check-in response. bool/numeric fields use
// omitzero (per coding-standards); the admin UI reads them truthily, so absent
// and false/0 are equivalent there.
func TestCheckinBatchResultJSON(t *testing.T) {
	out := CheckinBatchResult{
		OK:       true,
		PoolSite: "domestic",
		Summary:  CheckinSummary{Total: 2, CheckedIn: 1, Unsupported: 1},
		Results:  []CheckinResult{{AccountID: "a1", Site: "domestic", Supported: true, OK: true, RewardCredits: 100, Message: "签到成功，+100 积分"}},
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	// note is omitempty and identity fields always present
	wantKeys := []string{"ok", "poolSite", "summary", "results"}
	for _, key := range wantKeys {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing %q in %s", key, raw)
		}
	}
	if _, ok := decoded["note"]; ok {
		t.Fatalf("empty note should be omitted: %s", raw)
	}
	summary, _ := decoded["summary"].(map[string]any)
	// zero counters are omitted, non-zero kept
	if _, ok := summary["alreadyDone"]; ok {
		t.Fatalf("zero alreadyDone should be omitted: %s", raw)
	}
	if summary["checkedIn"] != float64(1) {
		t.Fatalf("checkedIn=%v", summary["checkedIn"])
	}
	results, _ := decoded["results"].([]any)
	item, _ := results[0].(map[string]any)
	if _, ok := item["alreadyCheckedIn"]; ok {
		t.Fatalf("false bool should be omitted: %s", raw)
	}
	if item["rewardCredits"] != float64(100) {
		t.Fatalf("rewardCredits=%v", item["rewardCredits"])
	}
	// note is omitempty: empty stays out
	empty, err := json.Marshal(CheckinBatchResult{PoolSite: "global"})
	if err != nil {
		t.Fatal(err)
	}
	var decodedEmpty map[string]any
	if err := json.Unmarshal(empty, &decodedEmpty); err != nil {
		t.Fatal(err)
	}
	if _, ok := decodedEmpty["note"]; ok {
		t.Fatalf("empty note should be omitted: %s", empty)
	}
}

// The admin UI reads these fields truthily; omitted and false/0 must behave alike.
func TestCheckinResultZeroValuesStayAbsent(t *testing.T) {
	raw, err := json.Marshal(CheckinResult{AccountID: "a1", Site: "global", Message: "不支持"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ok", "supported", "alreadyCheckedIn", "rewardCredits", "streakDays", "isStreakDay"} {
		if _, ok := decoded[key]; ok {
			t.Fatalf("%q should be omitted when zero: %s", key, raw)
		}
	}
	if decoded["accountId"] != "a1" || decoded["site"] != "global" {
		t.Fatalf("identity fields missing: %s", raw)
	}
}

func TestMapCheckinActivity(t *testing.T) {
	activity := mapCheckinActivity(map[string]any{
		"active":           true,
		"today_checked_in": false,
		"streak_days":      float64(3),
		"daily_credit":     float64(100),
		"today_credit":     float64(100),
		"is_streak_day":    false,
	})
	if !activity.Active || activity.TodayCheckedIn || activity.StreakDays != 3 || activity.DailyCredit != 100 {
		t.Fatalf("activity=%+v", activity)
	}
}

func TestMapCheckinActivityStatusOneDoesNotMarkToday(t *testing.T) {
	activity := mapCheckinActivity(map[string]any{"status": "1"})
	if !activity.Active || activity.TodayCheckedIn {
		t.Fatalf("activity=%+v", activity)
	}
	activity = mapCheckinActivity(map[string]any{"status": "active"})
	if !activity.Active || activity.TodayCheckedIn {
		t.Fatalf("status active=%+v", activity)
	}
}

func TestMapCheckinActivityStatusAndEmptyFields(t *testing.T) {
	activity := mapCheckinActivity(map[string]any{
		"active":           "",
		"enabled":          "1",
		"today_checked_in": "",
		"status":           "已签到",
	})
	if !activity.Active {
		t.Fatal("expected active from enabled=1")
	}
	if !activity.TodayCheckedIn {
		t.Fatal("expected today checked in from status text")
	}
}

func TestIsCheckinAlreadyDone(t *testing.T) {
	if !isCheckinAlreadyDone(map[string]any{"code": float64(checkinAlreadyCode), "msg": "今天已签到，请明天再来"}) {
		t.Fatal("expected already done")
	}
	if isCheckinAlreadyDone(map[string]any{"code": float64(0)}) {
		t.Fatal("expected not done")
	}
}

func TestCheckinNoteForSite(t *testing.T) {
	if !strings.Contains(CheckinNoteForSite("global"), "国际站") {
		t.Fatalf("global note=%q", CheckinNoteForSite("global"))
	}
	if !strings.Contains(CheckinNoteForSite("domestic"), "国内站") {
		t.Fatalf("domestic note=%q", CheckinNoteForSite("domestic"))
	}
}

func TestClassifyCheckinResult(t *testing.T) {
	cases := []struct {
		name string
		item CheckinResult
		want checkinOutcome
	}{
		{"success", CheckinResult{OK: true}, checkinCheckedIn},
		{"already done wins over ok", CheckinResult{OK: true, AlreadyCheckedIn: true}, checkinAlreadyDone},
		{"unsupported beats empty message", CheckinResult{}, checkinUnsupported},
		{"unsupported even with message", CheckinResult{Message: "活动未开启"}, checkinUnsupported},
		{"failure keeps supported", CheckinResult{Supported: true, Message: "HTTP 401"}, checkinFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyCheckinResult(tc.item); got != tc.want {
				t.Fatalf("classify=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestCheckinBatchResultAddUnattempted(t *testing.T) {
	var out CheckinBatchResult
	out.OK = true
	out.addUnattempted(accounts.Account{ID: "a1", Site: "cn"}, "批次超时，未执行签到")
	if out.Summary.Total != 1 || out.Summary.Skipped != 1 || out.Summary.Failed != 0 {
		t.Fatalf("summary=%+v", out.Summary)
	}
	if len(out.Results) != 1 || out.Results[0].Site != "domestic" {
		t.Fatalf("results=%+v", out.Results)
	}
	// addUnattempted must not flip OK on its own; the caller decides.
	if !out.OK {
		t.Fatal("addUnattempted should not clear OK")
	}
}

func TestParseTriBool(t *testing.T) {
	if parseTriBool(true, false) != triTrue || parseTriBool(false, false) != triFalse {
		t.Fatal("bool")
	}
	if parseTriBool("1", false) != triTrue || parseTriBool("0", false) != triFalse {
		t.Fatal("numeric string")
	}
	if parseTriBool("", false) != triUnset {
		t.Fatal("empty string")
	}
	if parseTriBool("已签到", true) != triTrue {
		t.Fatal("checked in text")
	}
	if parseTriBool("已签到", false) != triUnset {
		t.Fatal("checked in text only when allowed")
	}
}

func TestCheckinBatchTimeout(t *testing.T) {
	if checkinBatchTimeout(0) != 0 {
		t.Fatalf("empty batch=%s", checkinBatchTimeout(0))
	}
	if checkinBatchTimeout(1) != checkinRequestTimeout {
		t.Fatal("single account")
	}
	if checkinBatchTimeout(20) != checkinBatchMaxTimeout {
		t.Fatalf("capped=%s", checkinBatchTimeout(20))
	}
}

func TestEligibleCheckinAccountsFiltersSite(t *testing.T) {
	store := accounts.Store{
		Accounts: []accounts.Account{
			{ID: "d1", Site: "domestic", Enabled: true, BearerToken: "t"},
			{ID: "g1", Site: "global", Enabled: true, BearerToken: "t"},
			{ID: "d2", Site: "domestic", Enabled: false, BearerToken: "t"},
		},
	}
	got := eligibleCheckinAccounts(store, "domestic")
	if len(got) != 1 || got[0].ID != "d1" {
		t.Fatalf("eligible=%v", got)
	}
}

func TestCheckinNoteUsesNormalizedSite(t *testing.T) {
	if config.NormalizeSite("cn") != "domestic" {
		t.Fatal("normalize")
	}
}

func TestDailyCheckinForAccountInactive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"active": false, "today_checked_in": false},
		})
	}))
	defer srv.Close()

	client := provider.NewClient(config.Config{})
	account := testCheckinAccount(srv.URL, "global")
	result := DailyCheckinForAccount(t.Context(), client, account, config.Config{})
	if result.Supported || result.OK {
		t.Fatalf("inactive=%+v", result)
	}
	if result.Message != unsupportedCheckinMessage() {
		t.Fatalf("message=%q", result.Message)
	}
}

func TestDailyCheckinForAccountAlreadyDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"active": true, "today_checked_in": true, "streak_days": 2, "today_credit": 100},
		})
	}))
	defer srv.Close()

	client := provider.NewClient(config.Config{})
	result := DailyCheckinForAccount(t.Context(), client, testCheckinAccount(srv.URL, "global"), config.Config{})
	if !result.OK || !result.AlreadyCheckedIn || !result.Supported {
		t.Fatalf("already=%+v", result)
	}
}

func TestDailyCheckinForAccountSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"active": true, "today_checked_in": false},
			})
		case "/v2/billing/meter/daily-checkin":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"credit": 100, "streak_days": 1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := provider.NewClient(config.Config{})
	result := DailyCheckinForAccount(t.Context(), client, testCheckinAccount(srv.URL, "global"), config.Config{})
	if !result.OK || result.RewardCredits != 100 || result.Message != "签到成功，+100 积分" {
		t.Fatalf("success=%+v", result)
	}
}

func TestDailyCheckinForAccountCode10001(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"active": true, "today_checked_in": false},
			})
		case "/v2/billing/meter/daily-checkin":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": checkinAlreadyCode, "msg": "今天已签到，请明天再来"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := provider.NewClient(config.Config{})
	result := DailyCheckinForAccount(t.Context(), client, testCheckinAccount(srv.URL, "global"), config.Config{})
	if !result.OK || !result.AlreadyCheckedIn {
		t.Fatalf("already code=%+v", result)
	}
}

func TestDailyCheckinForAccountFetchErrorIsFailedNotUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "msg": "unauthorized"})
	}))
	defer srv.Close()

	client := provider.NewClient(config.Config{})
	result := DailyCheckinForAccount(t.Context(), client, testCheckinAccount(srv.URL, "global"), config.Config{})
	if !result.Supported {
		t.Fatalf("fetch error should count as failed, not unsupported: %+v", result)
	}
	if result.OK {
		t.Fatalf("expected failure: %+v", result)
	}

	out := RunPoolCheckin(t.Context(), client, accounts.Store{
		Accounts: []accounts.Account{testCheckinAccount(srv.URL, "global")},
	}, config.Config{}, "global")
	if out.OK || out.Summary.Unsupported != 0 || out.Summary.Failed != 1 {
		t.Fatalf("batch=%+v", out)
	}
}

func TestRunPoolCheckinFiltersByPoolSite(t *testing.T) {
	client := provider.NewClient(config.Config{})
	store := accounts.Store{
		Accounts: []accounts.Account{
			testCheckinAccountWithID("http://unused", "domestic", "domestic-1"),
			testCheckinAccountWithID("http://unused", "global", "global-1"),
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 1)
	defer cancel()
	out := RunPoolCheckin(ctx, client, store, config.Config{}, "domestic")
	if out.Summary.Total != 1 {
		t.Fatalf("summary=%+v", out.Summary)
	}
	if len(out.Results) != 1 || out.Results[0].AccountID != "domestic-1" {
		t.Fatalf("results=%+v", out.Results)
	}
	if out.Summary.Failed != 1 || out.Summary.Unsupported != 0 {
		t.Fatalf("timeout should count as failed: %+v", out.Summary)
	}
}

func TestRunPoolCheckinStopsWhenBatchDeadlineExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"active": true}})
	}))
	defer srv.Close()
	client := provider.NewClient(config.Config{})
	var accs []accounts.Account
	for i := 0; i < 3; i++ {
		accs = append(accs, testCheckinAccountWithID(srv.URL+"/v2/chat/completions", "global", "acc"))
	}
	store := accounts.Store{Accounts: accs}

	// Already-cancelled parent: the batch must short-circuit instead of firing
	// doomed requests one by one.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out := RunPoolCheckin(ctx, client, store, config.Config{}, "global")
	// Never-attempted accounts are skipped, not failed — but the batch is a failure.
	if out.Summary.Total != 3 || out.Summary.Skipped != 3 || out.Summary.Failed != 0 {
		t.Fatalf("summary=%+v", out.Summary)
	}
	if out.OK {
		t.Fatal("exhausted batch should not report ok")
	}
	for _, item := range out.Results {
		if item.Message == "" {
			t.Fatalf("skipped account should explain why: %+v", item)
		}
	}
}

func TestRunPoolCheckinUsesConfiguredSiteWhenPoolSiteEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"active": false}})
	}))
	defer srv.Close()
	client := provider.NewClient(config.Config{})
	acc := testCheckinAccountWithID(srv.URL+"/v2/chat/completions", "global", "global-1")
	store := accounts.Store{Accounts: []accounts.Account{acc}}
	cfg := config.Config{Site: "global"}

	out := RunPoolCheckin(t.Context(), client, store, cfg, "")
	if out.PoolSite != "global" {
		t.Fatalf("poolSite=%q want global (from cfg.Site)", out.PoolSite)
	}
	if out.Summary.Total != 1 {
		t.Fatalf("summary=%+v", out.Summary)
	}
}

func testCheckinAccount(baseURL, site string) accounts.Account {
	return testCheckinAccountWithID(baseURL, site, "acc-1")
}

func testCheckinAccountWithID(baseURL, site, id string) accounts.Account {
	return accounts.Account{
		ID:          id,
		Label:       id,
		Site:        site,
		Enabled:     true,
		BearerToken: "test-token",
		APIEndpoint: strings.TrimRight(baseURL, "/") + "/v2/chat/completions",
		AuthStatus: accounts.AuthStatus{
			UserID: "user-1",
		},
	}
}
