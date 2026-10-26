package model

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestConsoleProbeWindowExcludesUnknownSkippedFutureAndOldSamples(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, ConsoleBusinessZone)
	latency := int64(100)
	records := []ConsoleProbe{
		{CheckedAt: now.Add(-time.Hour).Unix(), Status: "normal", LatencyMS: &latency},
		{CheckedAt: now.Add(-2 * time.Hour).Unix(), Status: "degraded", LatencyMS: &latency},
		{CheckedAt: now.Add(-3 * time.Hour).Unix(), Status: "error", LatencyMS: &latency},
		{CheckedAt: now.Add(-time.Hour).Unix(), Status: "unknown"},
		{CheckedAt: now.Add(-time.Hour).Unix(), Status: "skipped"},
		{CheckedAt: now.Add(time.Hour).Unix(), Status: "normal"},
		{CheckedAt: now.AddDate(0, 0, -8).Unix(), Status: "error"},
	}
	window := ConsoleProbeWindow(records, 7, now)
	require.Equal(t, 3, window.Samples)
	require.InDelta(t, 200.0/3, *window.Percent, 0.0001)
	require.Equal(t, float64(100), *window.AverageMS)
	require.Equal(t, records[2].CheckedAt, *window.FirstSample)
	require.Nil(t, ConsoleProbeWindow(nil, 7, now).Percent)
}

// All fixtures and migrations run only in isolated test databases, never in a live database.
func TestConsoleDatabaseMatrix(t *testing.T) {
	dialects := []struct {
		name string
		open func() gorm.Dialector
	}{
		{"sqlite", func() gorm.Dialector { return sqlite.Open(":memory:") }},
		{"mysql", func() gorm.Dialector {
			if os.Getenv("CONSOLE_TEST_MYSQL_DSN") == "" {
				return nil
			}
			return mysql.Open(os.Getenv("CONSOLE_TEST_MYSQL_DSN"))
		}},
		{"postgres", func() gorm.Dialector {
			if os.Getenv("CONSOLE_TEST_POSTGRES_DSN") == "" {
				return nil
			}
			return postgres.Open(os.Getenv("CONSOLE_TEST_POSTGRES_DSN"))
		}},
	}
	for _, dialect := range dialects {
		t.Run(dialect.name, func(t *testing.T) {
			connector := dialect.open()
			if connector == nil {
				t.Skip("isolated database not configured")
			}
			db, err := gorm.Open(connector, &gorm.Config{})
			require.NoError(t, err)
			oldDB, oldLogDB := DB, LOG_DB
			DB, LOG_DB = db, db
			t.Cleanup(func() { DB, LOG_DB = oldDB, oldLogDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
			ctx := context.Background()
			now := time.Date(2026, 10, 1, 0, 30, 0, 0, ConsoleBusinessZone)
			empty, err := GetConsoleMonitoring(ctx, now)
			require.NoError(t, err)
			require.False(t, empty.Ready)
			require.Empty(t, empty.Channels)
			// Representative pre-feature tables exist before opt-in monitor migration.
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}, &Model{}, &Vendor{}, &Channel{}))
			require.NoError(t, db.Create(&User{Id: 1, Username: "console-user", AffCode: "fixture-one", Quota: 123, UsedQuota: 9, RequestCount: 2}).Error)
			require.NoError(t, db.Create(&User{Id: 2, Username: "other-user", AffCode: "fixture-two", Quota: 456}).Error)
			require.NoError(t, db.Create(&Token{UserId: 1, Key: "fixture-not-a-real-key", Status: 1, ExpiredTime: -1, UnlimitedQuota: true}).Error)
			require.NoError(t, db.Create(&Token{UserId: 1, Key: "fixture-disabled", Status: 2, ExpiredTime: -1}).Error)
			require.NoError(t, db.Create(&Token{UserId: 2, Key: "fixture-other-user", Status: 1, ExpiredTime: -1}).Error)
			require.NoError(t, db.Create(&Vendor{Id: 1, Name: "Configured provider"}).Error)
			require.NoError(t, db.Create(&Model{ModelName: "model-a", VendorID: 1, NameRule: 0}).Error)
			logs := []Log{
				{UserId: 1, Type: LogTypeConsume, ModelName: "model-a", CreatedAt: now.Unix() - 10, PromptTokens: 10, CompletionTokens: 5, Quota: 3, UseTime: 2},
				{UserId: 1, Type: LogTypeConsume, ModelName: "model-a", CreatedAt: now.Add(-time.Hour).Unix(), PromptTokens: 20, CompletionTokens: 10, Quota: 6, UseTime: 4},
				{UserId: 2, Type: LogTypeConsume, ModelName: "model-a", CreatedAt: now.Unix() - 10, PromptTokens: 999, Quota: 999},
				{UserId: 1, Type: LogTypeError, ModelName: "model-a", CreatedAt: now.Unix() - 10, PromptTokens: 999, Quota: 999},
			}
			require.NoError(t, db.Create(&logs).Error)
			snapshot, err := GetConsoleDashboard(ctx, 1, 7, now)
			require.NoError(t, err)
			require.EqualValues(t, 123, snapshot.Balance)
			require.EqualValues(t, 2, snapshot.Keys)
			require.EqualValues(t, 1, snapshot.EnabledKeys)
			require.EqualValues(t, 1, snapshot.Today.Requests)
			require.EqualValues(t, 3, snapshot.Today.Quota)
			require.EqualValues(t, 2, snapshot.Retained.Requests)
			require.EqualValues(t, 30, snapshot.Retained.Input)
			require.Equal(t, float64(3), *snapshot.Retained.AverageSeconds)
			require.EqualValues(t, 1, snapshot.Minute.Requests)
			require.Len(t, snapshot.Trend, 2)
			require.Equal(t, "2026-09-30", snapshot.Trend[0].Date)
			require.Len(t, snapshot.Recent, 2)
			require.EqualValues(t, 3, snapshot.Recent[0].Quota)
			require.Len(t, snapshot.Platforms, 1)
			require.Equal(t, "Configured provider", snapshot.Platforms[0].Name)
			require.EqualValues(t, 9, snapshot.Platforms[0].Total.Quota)
			// Fresh migration and repeated upgrade must preserve prior users and monitor records.
			require.NoError(t, MigrateConsoleMonitoring())
			require.NoError(t, db.Create(&ConsoleMonitor{ID: 1, ChannelID: 10, DisplayName: "Published channel", Platform: "Configured provider", Model: "model-a", Published: true}).Error)
			require.NoError(t, db.Create(&ConsoleMonitor{ID: 2, ChannelID: 11, DisplayName: "Private channel", Model: "secret-model", Published: false}).Error)
			latency := int64(120)
			require.NoError(t, db.Create(&ConsoleProbe{MonitorID: 1, CheckedAt: now.Add(-time.Hour).Unix(), Status: "normal", LatencyMS: &latency}).Error)
			require.NoError(t, MigrateConsoleMonitoring())
			require.NoError(t, MigrateConsoleMonitoring())
			monitoring, err := GetConsoleMonitoring(ctx, now)
			require.NoError(t, err)
			require.True(t, monitoring.Ready)
			require.Len(t, monitoring.Channels, 1)
			require.Equal(t, "Published channel", monitoring.Channels[0].Name)
			require.Equal(t, "unknown", monitoring.Channels[0].Models[0].Status)
			require.Len(t, monitoring.Channels[0].Models[0].History, 1)
			require.Equal(t, float64(100), *monitoring.Channels[0].Models[0].Windows[7].Percent)
			preserved, err := GetConsoleDashboard(ctx, 1, 7, now)
			require.NoError(t, err)
			require.Equal(t, snapshot.Today, preserved.Today)
		})
	}
}
