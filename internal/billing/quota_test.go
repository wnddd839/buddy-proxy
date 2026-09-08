package billing

import (
	"errors"
	"testing"
	"time"
)

func TestQuotaStateFromUsageUsesCycleEnd(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reset := now.Add(5 * time.Hour)
	usage := UsageResult{
		Credits: Credits{
			Remaining: floatPtr(0),
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
	if !IsQuotaExhaustedError(errors.New("quota exhausted")) {
		t.Fatal("quota message should match")
	}
}

func TestParseBillingTimeMillisUsesChinaLocal(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	raw := "2026-09-08T18:00:00"
	want := time.Date(2026, 9, 8, 18, 0, 0, 0, loc).UnixMilli()
	got := parseBillingTimeMillis(raw)
	if got != want {
		t.Fatalf("got=%d want=%d", got, want)
	}
}
