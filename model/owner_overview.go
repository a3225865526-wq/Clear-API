package model

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type OwnerUsage struct {
	ConsoleUsage
	ActiveUsers int64 `json:"active_users"`
	Errors      int64 `json:"errors"`
	RefundQuota int64 `json:"refund_quota"`
}

type OwnerDay struct {
	OwnerUsage
	Date     string `json:"date"`
	NewUsers int64  `json:"new_users"`
}

type OwnerRanking struct {
	ConsoleUsage
	Name string `json:"name"`
}

type OwnerOverview struct {
	OwnerUsage
	From                int64          `json:"from"`
	To                  int64          `json:"to"`
	ReadAt              int64          `json:"read_at"`
	Timezone            string         `json:"timezone"`
	TotalUsers          int64          `json:"total_users"`
	NewUsers            int64          `json:"new_users"`
	UnknownRegistration int64          `json:"unknown_registration"`
	PaidCreditUsers     *int64         `json:"paid_credit_users"`
	PendingReview       *int64         `json:"pending_review"`
	Trend               []OwnerDay     `json:"trend"`
	Models              []OwnerRanking `json:"models"`
	Channels            []OwnerRanking `json:"channels"`
}

func OwnerOverviewRange(period string, from, to int64, now time.Time) (int64, int64, error) {
	if period != "" {
		if from != 0 || to != 0 {
			return 0, 0, ErrFinanceInvalid
		}
		local := now.In(ConsoleBusinessZone)
		today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, ConsoleBusinessZone)
		days := 1
		switch period {
		case "today":
		case "7d":
			days = 7
		case "30d":
			days = 30
		default:
			return 0, 0, ErrFinanceInvalid
		}
		from, to = today.AddDate(0, 0, 1-days).Unix(), today.AddDate(0, 0, 1).Unix()
	}
	if from <= 0 || to <= from || to-from > 366*86400 {
		return 0, 0, ErrFinanceInvalid
	}
	return from, to, nil
}

// Calendar buckets never depend on the database session's timezone. Division
// needs an explicit fractional operand on PostgreSQL (integer division truncates).
func ownerDayExpression(kind common.DatabaseType) string {
	switch kind {
	case common.DatabaseTypePostgreSQL, common.DatabaseTypeMySQL:
		return "FLOOR((created_at + 28800) / 86400.0)"
	case common.DatabaseTypeClickHouse:
		return "intDiv(created_at + 28800, 86400)"
	default:
		return "CAST((created_at + 28800) / 86400 AS INTEGER)"
	}
}

const ownerUsageSelect = "COALESCE(SUM(CASE WHEN type = 2 THEN 1 ELSE 0 END),0) AS requests, COALESCE(SUM(CASE WHEN type = 5 THEN 1 ELSE 0 END),0) AS errors, COUNT(DISTINCT CASE WHEN type = 2 THEN user_id END) AS active_users, COALESCE(SUM(CASE WHEN type = 2 THEN quota ELSE 0 END),0) AS quota, COALESCE(SUM(CASE WHEN type = 6 THEN quota ELSE 0 END),0) AS refund_quota, COALESCE(SUM(CASE WHEN type = 2 THEN prompt_tokens ELSE 0 END),0) AS input, COALESCE(SUM(CASE WHEN type = 2 THEN completion_tokens ELSE 0 END),0) AS output, AVG(CASE WHEN type = 2 THEN use_time END) AS average_seconds"

// Only aggregate fields are read; there are no credential, log-content or
// username reads. Primary and log databases may be separate, so this is not
// advertised as a cross-database transaction snapshot.
func GetOwnerOverview(ctx context.Context, db, logDB *gorm.DB, from, to int64, now time.Time) (*OwnerOverview, error) {
	if _, _, err := OwnerOverviewRange("", from, to, now); err != nil {
		return nil, err
	}
	if db == nil || logDB == nil {
		return nil, ErrFinanceUnavailable
	}
	result := &OwnerOverview{From: from, To: to, ReadAt: now.Unix(), Timezone: "Asia/Shanghai", Trend: []OwnerDay{}, Models: []OwnerRanking{}, Channels: []OwnerRanking{}}
	// Do not include facts timestamped after this read, even for today's open day.
	end := min(to, now.Unix()+1)
	users := db.WithContext(ctx).Unscoped().Model(&User{}).Where("created_at <= ? OR created_at IS NULL", now.Unix())
	if err := users.Count(&result.TotalUsers).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Unscoped().Model(&User{}).Where("created_at <= 0 OR created_at IS NULL").Count(&result.UnknownRegistration).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Unscoped().Model(&User{}).Where("created_at >= ? AND created_at < ?", from, end).Count(&result.NewUsers).Error; err != nil {
		return nil, err
	}
	logs := logDB.WithContext(ctx).Model(&Log{}).Where("created_at >= ? AND created_at < ? AND type IN ?", from, end, []int{LogTypeConsume, LogTypeError, LogTypeRefund})
	if err := logs.Select(ownerUsageSelect).Scan(&result.OwnerUsage).Error; err != nil {
		return nil, err
	}
	var usageDays []struct {
		Bucket int64
		OwnerUsage
	}
	logBucket := ownerDayExpression(common.LogDatabaseType())
	if err := logDB.WithContext(ctx).Model(&Log{}).Where("created_at >= ? AND created_at < ? AND type IN ?", from, end, []int{LogTypeConsume, LogTypeError, LogTypeRefund}).Select(logBucket + " AS bucket, " + ownerUsageSelect).Group(logBucket).Scan(&usageDays).Error; err != nil {
		return nil, err
	}
	var userDays []struct {
		Bucket   int64
		NewUsers int64
	}
	userBucket := ownerDayExpression(common.MainDatabaseType())
	if err := db.WithContext(ctx).Unscoped().Model(&User{}).Where("created_at >= ? AND created_at < ?", from, end).Select(userBucket + " AS bucket, COUNT(*) AS new_users").Group(userBucket).Scan(&userDays).Error; err != nil {
		return nil, err
	}
	usageByDay := map[int64]OwnerUsage{}
	for _, row := range usageDays {
		usageByDay[row.Bucket] = row.OwnerUsage
	}
	usersByDay := map[int64]int64{}
	for _, row := range userDays {
		usersByDay[row.Bucket] = row.NewUsers
	}
	local := time.Unix(from, 0).In(ConsoleBusinessZone)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, ConsoleBusinessZone)
	for day.Unix() < to {
		bucket := (day.Unix() + 28800) / 86400
		result.Trend = append(result.Trend, OwnerDay{Date: day.Format("2006-01-02"), OwnerUsage: usageByDay[bucket], NewUsers: usersByDay[bucket]})
		day = day.AddDate(0, 0, 1)
	}
	for _, ranking := range []struct {
		column string
		target *[]OwnerRanking
	}{{"model_name", &result.Models}, {"channel_id", &result.Channels}} {
		if err := logDB.WithContext(ctx).Model(&Log{}).Where("type = ? AND created_at >= ? AND created_at < ?", LogTypeConsume, from, end).Select(ranking.column + " AS name, " + consoleUsageSelect).Group(ranking.column).Order("quota DESC, requests DESC, name ASC").Limit(10).Scan(ranking.target).Error; err != nil {
			return nil, err
		}
	}
	if FinanceRevenueReady(db) == nil {
		var count int64
		if err := db.WithContext(ctx).Model(&FinanceRevenueRecord{}).Where("kind = ? AND source = ? AND value_cny IS NOT NULL AND at >= ? AND at < ?", "credit", "paid", from, end).Distinct("user_id").Count(&count).Error; err != nil {
			return nil, err
		}
		result.PaidCreditUsers = &count
	}
	// The review schema is optional and may be supplied by a later release.
	// Read its aggregate contract without depending on unshipped reconciliation code.
	if BillingReservationsReady(db) == nil && db.Migrator().HasTable("billing_review_markers") {
		var count int64
		markers := db.Table("billing_review_markers").Select("reservation_id")
		if err := db.WithContext(ctx).Model(&BillingReservation{}).Where("state IN ? OR (state = ? AND id IN (?))", []string{"needs_review", "settle_pending", "refund_pending"}, "settled", markers).Count(&count).Error; err != nil {
			return nil, err
		}
		result.PendingReview = &count
	}
	return result, nil
}
