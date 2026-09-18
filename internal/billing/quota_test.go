package billing

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestQuotaStateFromUsageUsesCycleEnd(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reset := now.Add(5 * time.Hour)
	usage := UsageResult{
		Credits: Credits{
			Remaining:    floatPtr(0),
			CycleEndTime: reset.UTC().Format(time.RFC3339),
			Packages: []Package{{
				CycleEndTime: reset.UTC().Format(time.RFC3339),
			}},
		},
		Notify: &Notify{DosageNotifyCode: 3},
	}
	state := QuotaStateFromUsage(usage, now)
	if !state.Exhausted(now) {
		t.Fatalf("expected exhausted state %+v", state)
	}
	if state.ResetAt != reset.UnixMilli() {
		t.Fatalf("resetAt=%d want %d", state.ResetAt, reset.UnixMilli())
	}
	if state.CooldownDuration(now) < 4*time.Hour+59*time.Minute {
		t.Fatalf("cooldown=%s", state.CooldownDuration(now))
	}
}

func TestQuotaStateFromUsageFallsBackToFiveHours(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	usage := UsageResult{
		Credits: Credits{Remaining: floatPtr(0)},
		Notify:  &Notify{DosageNotifyCode: 3},
	}
	state := QuotaStateFromUsage(usage, now)
	if state.ResetAt <= now.UnixMilli() {
		t.Fatalf("expected fallback reset, got %+v", state)
	}
}

func TestCreditsNextResetAtPicksEarliestSliceEnd(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	soon := now.Add(2 * time.Hour)
	later := now.Add(5 * time.Hour)
	got := creditsNextResetAt(Credits{
		CycleEndTime: later.UTC().Format(time.RFC3339),
		Packages: []Package{{
			SlicePeriodEndTime: soon.UTC().Format(time.RFC3339),
		}},
	}, now)
	if got != soon.UnixMilli() {
		t.Fatalf("resetAt=%d want %d", got, soon.UnixMilli())
	}
}

func TestIsQuotaExhaustedErrorRejectsPlain429(t *testing.T) {
	if IsQuotaExhaustedError(errors.New("429 too many requests")) {
		t.Fatal("plain 429 should not trigger quota probe")
	}
	if IsQuotaExhaustedError(errors.New("failed with 429: 6004 will reset at 2099-01-01 00:00:00")) {
		t.Fatal("6004 must not be treated as quota exhausted")
	}
	if !IsQuotaExhaustedError(errors.New("quota exhausted")) {
		t.Fatal("quota message should match")
	}
}

func TestQuotaStateJSONMarshalOmitzero(t *testing.T) {
	tests := []struct {
		name  string
		state QuotaState
		want  string
	}{
		{
			name:  "zero values omit bool and numeric fields",
			state: QuotaState{},
			want:  `{"remaining":null}`,
		},
		{
			name:  "zero remaining only",
			state: QuotaState{Remaining: floatPtr(0)},
			want:  `{"remaining":0}`,
		},
		{
			name: "exhausted with reset metadata",
			state: QuotaState{
				Remaining:  floatPtr(0),
				ResetAt:    1_800_000_000_000,
				CheckedAt:  1_700_000_000_000,
				NotifyCode: 2,
			},
			want: `{"remaining":0,"resetAt":1800000000000,"checkedAt":1700000000000,"notifyCode":2}`,
		},
		{
			name: "unlimited keeps true",
			state: QuotaState{
				Remaining: floatPtr(99),
				Unlimited: true,
			},
			want: `{"remaining":99,"unlimited":true}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("marshal=%s want=%s", got, tt.want)
			}
		})
	}
}

func TestParseBillingTimeMillisUsesChinaLocal(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	raw := "2026-09-08T18:00:00"
	want := time.Date(2026, 9, 8, 18, 0, 0, 0, loc).UnixMilli()
	got := ParseBillingTimeMillis(raw)
	if got != want {
		t.Fatalf("got=%d want=%d", got, want)
	}
}

func TestModelRateLimitCooldownStripsChatErrorMetadata(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		err  string
	}{
		{
			name: "live ChatError envelope",
			err:  "CodeBuddy chat completion failed with 429: 6004 rate-model will reset at 2099-01-01 00:00:00 UTC+8 [region=global site=global endpoint=https://www.codebuddy.ai/v2/chat/completions domain=www.codebuddy.ai model=DS-V4.1-Flash]",
		},
		{
			name: "UTC+8 before region bracket",
			err:  "failed with 429: 6004 rate-model will reset at 2099-01-01 00:00:00 UTC+8 [region=global site=global]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := ModelRateLimitCooldown(errors.New(tc.err), now)
			if !ok {
				t.Fatalf("ModelRateLimitCooldown ok=false, parse miss would fall back to 2m")
			}
			if d != 48*time.Hour {
				t.Fatalf("cooldown=%s want 48h clamp", d)
			}
		})
	}
}

func TestModelRateLimitCooldownRealWorld6004Tail(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 14, 41, 0, 0, loc)
	// 生产实收报文（9/18 沙箱与现网逐字一致）：时间戳后紧跟 ", alternatively…"，
	// 再往后是 "(code 6004)" 与 ChatError 的 "[region=…]" 信封。
	msg := errors.New("CodeBuddy chat completion failed with 429: usage exceeds frequency limit, but don't worry, your usage will reset at 2026-09-19 04:23:04 UTC+8, alternatively, you can switch to the other models to continue using it. (code 6004) [region=global site=global endpoint=https://www.workbuddy.ai/v2/chat/completions domain=www.workbuddy.ai model=deepseek-v4.1-flash]")
	d, ok := ModelRateLimitCooldown(msg, now)
	if !ok {
		t.Fatal("real-world 6004 message must parse; ok=false falls back to the fixed 2m cooldown")
	}
	want := time.Date(2026, 9, 19, 4, 23, 4, 0, loc).Sub(now)
	if d != want {
		t.Fatalf("cooldown=%s want=%s", d, want)
	}
}

// 提取器换成"按时间戳形状"后，旧实现能吃下的非日期形态（epoch 数字、带空格的
// UTC +8）不能丢——它们由 ParseBillingTimeMillis 的数字分支与旧截断路径兜底。
func TestModelRateLimitCooldownKeepsLegacyInputForms(t *testing.T) {
	now := time.Date(2026, 9, 18, 14, 41, 0, 0, time.UTC)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	reset := time.Date(2026, 9, 19, 4, 23, 4, 0, loc)
	tests := []struct {
		name string
		err  string
	}{
		{
			name: "epoch millis",
			err:  "failed with 429: 6004 rate-model will reset at 1789762984000, alternatively, switch models",
		},
		{
			name: "epoch seconds",
			err:  "failed with 429: 6004 rate-model will reset at 1789762984; retry later",
		},
		{
			name: "UTC +8 with spaces",
			err:  "failed with 429: 6004 rate-model will reset at 2026-09-19 04:23:04 UTC +8, alternatively, switch",
		},
		{
			name: "lowercase gmt with spaces",
			err:  "failed with 429: 6004 rate-model will reset at 2026-09-19 04:23:04 gmt +08:00, alternatively",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := ModelRateLimitCooldown(errors.New(tc.err), now)
			if !ok {
				t.Fatalf("legacy input form lost: ok=false would fall back to the fixed 2m cooldown")
			}
			if d != reset.Sub(now) {
				t.Fatalf("cooldown=%s want=%s", d, reset.Sub(now))
			}
		})
	}
}

// 非 +8 偏移不能被当成中国时区剥离：宁可解析失败回落固定冷却，也不能按错误时区换算。
func TestModelRateLimitCooldownRejectsNonChinaZone(t *testing.T) {
	now := time.Date(2026, 9, 18, 14, 41, 0, 0, time.UTC)
	for _, msg := range []string{
		"failed with 429: 6004 rate-model will reset at 2026-09-19 04:23:04 UTC-8, alternatively",
		"failed with 429: 6004 rate-model will reset at 2026-09-19 04:23:04 UTC+9, alternatively",
	} {
		if _, ok := ModelRateLimitCooldown(errors.New(msg), now); ok {
			t.Fatalf("non-+8 offset must not be parsed as China time: %q", msg)
		}
	}
}
