package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// ErrUserPlatformQuotaNotFound service 层 sentinel：quota 记录不存在。
// adapter 将 repository.ErrUserPlatformQuotaNotFound 包装为此错误，
// handler 只需引用 service 包，无需直接依赖 repository 包。
var ErrUserPlatformQuotaNotFound = errors.New("user platform quota not found")

// ErrUserPlatformQuotaFKViolation service 层 sentinel：批量 snapshot UPSERT 时存在
// user_id 不在 users 表的记录（外键违反）。adapter 负责将 repository 层同名 sentinel 包装为此错误。
var ErrUserPlatformQuotaFKViolation = errors.New("user platform quota snapshot FK violation")

// UserWeeklyQuotaAnchorPlatform 是用户级周额度的配置锚点。周额度属于用户而非平台，
// 但为兼容既有 user_platform_quotas 表，额度值固定存放在 anthropic 这一已有行中。
// 其它平台行的 weekly_limit_usd 均应为 NULL，weekly_usage_usd 仍按平台落盘以保留审计颗粒度。
const UserWeeklyQuotaAnchorPlatform = "anthropic"

// UserWeeklyQuota 是跨平台聚合后的自然周额度视图。
type UserWeeklyQuota struct {
	WeeklyLimitUSD    *float64
	WeeklyUsageUSD    float64
	WeeklyResetsAt    time.Time
	WeeklyWindowStart time.Time
}

// UserPlatformQuotaSnapshot 是 service 层 flusher 向 DB 写入快照时使用的传输结构。
// 字段语义与 repository.UserPlatformQuotaSnapshot 完全对应，由 adapter 负责转换。
type UserPlatformQuotaSnapshot struct {
	UserID             int64
	Platform           string
	DailyUsageUSD      float64
	WeeklyUsageUSD     float64
	MonthlyUsageUSD    float64
	DailyWindowStart   time.Time
	WeeklyWindowStart  time.Time
	MonthlyWindowStart time.Time
}

// UserPlatformQuotaRecord service 层传输结构体（与 repository 层解耦）。
type UserPlatformQuotaRecord struct {
	UserID          int64
	Platform        string
	DailyLimitUSD   *float64
	WeeklyLimitUSD  *float64
	MonthlyLimitUSD *float64
	DailyUsageUSD   float64
	WeeklyUsageUSD  float64
	MonthlyUsageUSD float64
	// 窗口起始时间（可选，用于未来 reset 校验）
	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time
}

// AggregateUserWeeklyQuota 将用户各平台的本周用量合并成一个自然周桶。
// 窗口完全由 now 推导：过期行不计入本周，因此跨周一零点无需 cron 清零。
func AggregateUserWeeklyQuota(records []UserPlatformQuotaRecord, now time.Time) UserWeeklyQuota {
	start := timezone.StartOfWeek(now)
	result := UserWeeklyQuota{
		WeeklyWindowStart: start,
		WeeklyResetsAt:    start.AddDate(0, 0, 7),
	}
	for _, record := range records {
		if record.Platform == UserWeeklyQuotaAnchorPlatform {
			result.WeeklyLimitUSD = record.WeeklyLimitUSD
		}
		if record.WeeklyWindowStart != nil && record.WeeklyWindowStart.Equal(start) {
			result.WeeklyUsageUSD += record.WeeklyUsageUSD
		}
	}
	return result
}

// UserPlatformQuotaRepository 定义 service 层所需的 user × platform quota 数据访问端口。
// repository 包的 userPlatformQuotaRepository 实现此接口。
type UserPlatformQuotaRepository interface {
	// GetByUserPlatform 查询单条配额记录，未找到时返回 (nil, nil)。
	GetByUserPlatform(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaRecord, error)
	// BulkInsertInitial 幂等批量插入初始配额记录（ON CONFLICT DO NOTHING）。
	BulkInsertInitial(ctx context.Context, records []UserPlatformQuotaRecord) error
	// IncrementUsageWithReset 原子地累加用量，若窗口已过期则先重置再累加。
	IncrementUsageWithReset(ctx context.Context, userID int64, platform string, cost float64, now time.Time) error
	// ListByUser 查询用户的所有平台配额记录。
	ListByUser(ctx context.Context, userID int64) ([]UserPlatformQuotaRecord, error)
	// UpsertForUser 全量替换该用户所有平台限额配置（事务内）：
	//   1. 软删除未在 records 中出现的所有 active 行
	//   2. 对 records 中每条：UPDATE 已存在的（含重新激活软删行）；UPDATE 未命中时 INSERT
	//      仅改 *_limit_usd + deleted_at + updated_at，保留 *_usage_usd / *_window_start。
	// records 为空时仅执行步骤 1。
	UpsertForUser(ctx context.Context, userID int64, records []UserPlatformQuotaRecord) error
	// ResetExpiredWindow 重置指定窗口（"daily"|"weekly"|"monthly"）的用量与起始时间。
	// 未命中活跃记录时返回（service-side wrapper of repository.ErrUserPlatformQuotaNotFound）。
	ResetExpiredWindow(ctx context.Context, userID int64, platform string, window string, newStart time.Time) error
	// BatchSnapshotUsage 绝对值覆盖写入整批 usage 快照。FK 违反返回 ErrUserPlatformQuotaFKViolation。
	BatchSnapshotUsage(ctx context.Context, snapshots []UserPlatformQuotaSnapshot, now time.Time) error
}
