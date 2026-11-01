package model

import (
	"context"
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFinancialAnalyticsSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}, &User{}))
	user := User{Id: 24, Username: "fixture", AffCode: "fixture", Quota: 123456, UsedQuota: 218867}
	require.NoError(t, db.Create(&user).Error)
	file, err := os.Open("../evidence/billing-analytics-20261101/consume-request-reconciliation.csv")
	require.NoError(t, err)
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	logs := []Log{}
	for _, row := range records[1:] {
		numbers := map[int]int{}
		for _, index := range []int{0, 2, 3, 5, 6, 7} {
			numbers[index], err = strconv.Atoi(row[index])
			require.NoError(t, err)
		}
		// Reconstruct only the audited observation categories, not provider facts.
		other := `{"admin_info":{"token_observation":{"quality":"upstream_partial"}}}`
		if row[11] == "upstream_usage_unknown" {
			other = "{}"
		}
		if numbers[0] == 652 {
			other = `{"admin_info":{"token_observation":{"quality":"local_estimate"}}}`
		}
		logs = append(logs, Log{Id: numbers[0], RequestId: row[1], CreatedAt: int64(numbers[2]), UserId: numbers[3], Username: "fixture", ModelName: row[4], Quota: numbers[5], PromptTokens: numbers[6], CompletionTokens: numbers[7], Other: other, Type: LogTypeConsume})
	}
	require.NoError(t, db.Create(&logs).Error)
	for _, quota := range []int{2752, 2412, 2752, 2412, 2752, 16370} {
		require.NoError(t, db.Create(&Log{Type: LogTypeRefund, CreatedAt: 1793490000, Quota: quota, Content: "Durable reservation refund"}).Error)
	}
	ctx := context.Background()
	var before []Log
	require.NoError(t, db.Order("id").Find(&before).Error)
	rows, err := GetUsageAnalytics(ctx, db, db, 1793462400, 1793548799, "", 0)
	require.NoError(t, err)
	counts, amounts := map[string]int64{}, map[string]int64{}
	var total, tokens int64
	for _, row := range rows {
		counts[row.UsageQuality] += row.Count
		amounts[row.UsageQuality] += row.Quota
		total += row.Quota
		tokens += row.TokenUsed
	}
	assert.EqualValues(t, 42, counts["unknown"])
	assert.EqualValues(t, 407035, amounts["unknown"])
	assert.EqualValues(t, 1, counts["estimated"])
	assert.EqualValues(t, 442, counts["partial"])
	assert.Zero(t, counts["confirmed"])
	assert.EqualValues(t, 695331, total)
	assert.EqualValues(t, 31485753, tokens)
	repeated, err := GetUsageAnalytics(ctx, db, db, 1793462400, 1793548799, "", 0)
	require.NoError(t, err)
	assert.Equal(t, rows, repeated)
	private, err := GetUsageAnalytics(ctx, db, db, 1793462400, 1793548799, "", 999)
	require.NoError(t, err)
	assert.Empty(t, private)
	for _, refund := range []int{0, 100} {
		if refund > 0 {
			require.NoError(t, db.Create(&Log{Type: LogTypeRefund, CreatedAt: 1793490001, Quota: refund, Content: "Atomic legacy task adjustment"}).Error)
		}
		overview, err := GetOwnerOverview(ctx, db, db, 1793462400, 1793548800, time.Unix(1793498554, 0))
		require.NoError(t, err)
		assert.EqualValues(t, 29450, overview.ReleaseQuota)
		assert.EqualValues(t, refund, overview.RefundQuota)
		assert.EqualValues(t, 695331-refund, overview.Quota-overview.RefundQuota)
		assert.EqualValues(t, refund, overview.Trend[0].RefundQuota)
	}
	// A complete observation is distinct from partial, estimated and missing.
	require.NoError(t, db.Create(&Log{Type: LogTypeConsume, UserId: 999, CreatedAt: 1793490002, Quota: 12, Other: `{"admin_info":{"token_observation":{"quality":"upstream_complete"}}}`}).Error)
	confirmed, err := GetUsageAnalytics(ctx, db, db, 1793462400, 1793548799, "", 999)
	require.NoError(t, err)
	require.Len(t, confirmed, 1)
	assert.Equal(t, "confirmed", confirmed[0].UsageQuality)
	byName, err := GetUsageAnalytics(ctx, db, db, 1793462400, 1793548799, "fixture", 0)
	require.NoError(t, err)
	byID, err := GetUsageAnalytics(ctx, db, db, 1793462400, 1793548799, "", 24)
	require.NoError(t, err)
	assert.Equal(t, byID, byName)
	var after []Log
	require.NoError(t, db.Order("id").Limit(len(before)).Find(&after).Error)
	assert.Equal(t, before, after)
	var unchanged User
	require.NoError(t, db.First(&unchanged, 24).Error)
	assert.Equal(t, user.Quota, unchanged.Quota)
	assert.Equal(t, user.UsedQuota, unchanged.UsedQuota)
}
