package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func TestAggregateUserWeeklyQuota_跨平台汇总且忽略过期窗口(t *testing.T) {
	now := time.Date(2026, time.August, 19, 15, 0, 0, 0, timezone.Location())
	weekStart := timezone.StartOfWeek(now)
	previousWeekStart := weekStart.AddDate(0, 0, -7)
	limit := 50.0

	quota := AggregateUserWeeklyQuota([]UserPlatformQuotaRecord{
		{
			Platform:          UserWeeklyQuotaAnchorPlatform,
			WeeklyLimitUSD:    &limit,
			WeeklyUsageUSD:    12.5,
			WeeklyWindowStart: &weekStart,
		},
		{
			Platform:          "openai",
			WeeklyUsageUSD:    7.25,
			WeeklyWindowStart: &weekStart,
		},
		{
			Platform:          "gemini",
			WeeklyUsageUSD:    99,
			WeeklyWindowStart: &previousWeekStart,
		},
	}, now)

	if quota.WeeklyLimitUSD == nil || *quota.WeeklyLimitUSD != 50 {
		t.Fatalf("weekly limit = %v, want 50", quota.WeeklyLimitUSD)
	}
	if quota.WeeklyUsageUSD != 19.75 {
		t.Fatalf("weekly usage = %v, want 19.75", quota.WeeklyUsageUSD)
	}
	if !quota.WeeklyWindowStart.Equal(weekStart) {
		t.Fatalf("window start = %s, want %s", quota.WeeklyWindowStart, weekStart)
	}
	if !quota.WeeklyResetsAt.Equal(weekStart.AddDate(0, 0, 7)) {
		t.Fatalf("resets at = %s, want next Monday", quota.WeeklyResetsAt)
	}
}

func TestAggregateUserWeeklyQuota_无限额用户(t *testing.T) {
	now := time.Date(2026, time.August, 19, 15, 0, 0, 0, timezone.Location())
	weekStart := timezone.StartOfWeek(now)
	quota := AggregateUserWeeklyQuota([]UserPlatformQuotaRecord{{
		Platform:          "openai",
		WeeklyUsageUSD:    3.5,
		WeeklyWindowStart: &weekStart,
	}}, now)

	if quota.WeeklyLimitUSD != nil {
		t.Fatalf("weekly limit = %v, want nil", *quota.WeeklyLimitUSD)
	}
	if quota.WeeklyUsageUSD != 3.5 {
		t.Fatalf("weekly usage = %v, want 3.5", quota.WeeklyUsageUSD)
	}
}
