package model

import (
	"context"
	"time"
)

// ConsoleBusinessZone matches the existing finance reporting business day.
var ConsoleBusinessZone = time.FixedZone("Asia/Shanghai", 8*3600)

type ConsoleUsage struct {
	Requests       int64    `json:"requests"`
	Quota          int64    `json:"quota"`
	Input          int64    `json:"input"`
	Output         int64    `json:"output"`
	AverageSeconds *float64 `json:"average_seconds"`
}

type ConsoleModelUsage struct {
	ConsoleUsage
	Model string `json:"model"`
}

type ConsoleTrend struct {
	Date string `json:"date"`
	ConsoleUsage
}

type ConsolePlatformUsage struct {
	Name  string       `json:"name"`
	Total ConsoleUsage `json:"total"`
	Today ConsoleUsage `json:"today"`
}

type ConsoleRecentUsage struct {
	ID        int64  `json:"id"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"`
	Quota     int64  `json:"quota"`
	Input     int64  `json:"input"`
	Output    int64  `json:"output"`
}

type ConsoleDashboard struct {
	ReadAt          int64                  `json:"read_at"`
	Timezone        string                 `json:"timezone"`
	Start           int64                  `json:"start"`
	End             int64                  `json:"end"`
	Balance         int64                  `json:"balance"`
	AccountQuota    int64                  `json:"account_quota"`
	AccountRequests int64                  `json:"account_requests"`
	Keys            int64                  `json:"keys"`
	EnabledKeys     int64                  `json:"enabled_keys"`
	Today           ConsoleUsage           `json:"today"`
	Retained        ConsoleUsage           `json:"retained"`
	Minute          ConsoleUsage           `json:"minute"`
	Models          []ConsoleModelUsage    `json:"models"`
	Platforms       []ConsolePlatformUsage `json:"platforms"`
	Trend           []ConsoleTrend         `json:"trend"`
	Recent          []ConsoleRecentUsage   `json:"recent"`
}

const consoleUsageSelect = "count(*) AS requests, COALESCE(sum(quota), 0) AS quota, COALESCE(sum(prompt_tokens), 0) AS input, COALESCE(sum(completion_tokens), 0) AS output, avg(use_time) AS average_seconds"

// Every query is scoped by the authenticated user ID, including administrator accounts.
func GetConsoleDashboard(ctx context.Context, userID, days int, now time.Time) (*ConsoleDashboard, error) {
	local := now.In(ConsoleBusinessZone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, ConsoleBusinessZone)
	start := today.AddDate(0, 0, -(days - 1)).Unix()
	result := &ConsoleDashboard{ReadAt: now.Unix(), Timezone: "Asia/Shanghai", Start: start, End: now.Unix(), Models: []ConsoleModelUsage{}, Platforms: []ConsolePlatformUsage{}, Trend: []ConsoleTrend{}, Recent: []ConsoleRecentUsage{}}
	var account struct {
		Quota        int64
		UsedQuota    int64
		RequestCount int64
	}
	if err := DB.WithContext(ctx).Model(&User{}).Select("quota, used_quota, request_count").Where("id = ?", userID).Take(&account).Error; err != nil {
		return nil, err
	}
	result.Balance, result.AccountQuota, result.AccountRequests = account.Quota, account.UsedQuota, account.RequestCount
	tokens := DB.WithContext(ctx).Model(&Token{}).Where("user_id = ?", userID)
	if err := tokens.Count(&result.Keys).Error; err != nil {
		return nil, err
	}
	if err := DB.WithContext(ctx).Model(&Token{}).Where("user_id = ? AND status = ? AND (expired_time = ? OR expired_time > ?) AND (unlimited_quota = ? OR remain_quota > ?)", userID, 1, -1, now.Unix(), true, 0).Count(&result.EnabledKeys).Error; err != nil {
		return nil, err
	}
	for _, window := range []struct {
		from   int64
		target *ConsoleUsage
	}{{0, &result.Retained}, {today.Unix(), &result.Today}, {now.Add(-time.Minute).Unix(), &result.Minute}} {
		query := LOG_DB.WithContext(ctx).Model(&Log{}).Where("user_id = ? AND type = ? AND created_at <= ?", userID, LogTypeConsume, now.Unix())
		if window.from > 0 {
			query = query.Where("created_at >= ?", window.from)
		}
		if err := query.Select(consoleUsageSelect).Scan(window.target).Error; err != nil {
			return nil, err
		}
	}
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userID, LogTypeConsume, start, now.Unix()).Select("model_name AS model, " + consoleUsageSelect).Group("model_name").Order("requests DESC").Scan(&result.Models).Error; err != nil {
		return nil, err
	}
	// Bucket timestamps in Go so the same timezone and query work across database dialects.
	var points []struct {
		CreatedAt int64
		ConsoleUsage
	}
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userID, LogTypeConsume, start, now.Unix()).Select("created_at, " + consoleUsageSelect).Group("created_at").Order("created_at ASC").Scan(&points).Error; err != nil {
		return nil, err
	}
	for _, point := range points {
		date := time.Unix(point.CreatedAt, 0).In(ConsoleBusinessZone).Format("2006-01-02")
		if len(result.Trend) == 0 || result.Trend[len(result.Trend)-1].Date != date {
			result.Trend = append(result.Trend, ConsoleTrend{Date: date})
		}
		bucket := &result.Trend[len(result.Trend)-1].ConsoleUsage
		bucket.Requests += point.Requests
		bucket.Quota += point.Quota
		bucket.Input += point.Input
		bucket.Output += point.Output
	}
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userID, LogTypeConsume, start, now.Unix()).Select("id, model_name AS model, created_at, quota, prompt_tokens AS input, completion_tokens AS output").Order("created_at DESC").Limit(5).Scan(&result.Recent).Error; err != nil {
		return nil, err
	}
	// Platforms are exact model-to-vendor metadata assignments, never channel names or guesses.
	var metadata []Model
	var vendors []Vendor
	if err := DB.WithContext(ctx).Select("model_name, vendor_id, name_rule").Where("name_rule = ?", 0).Find(&metadata).Error; err != nil {
		return nil, err
	}
	if err := DB.WithContext(ctx).Select("id, name").Find(&vendors).Error; err != nil {
		return nil, err
	}
	vendorNames := map[int]string{}
	for _, vendor := range vendors {
		vendorNames[vendor.Id] = vendor.Name
	}
	assignments := map[string]string{}
	for _, item := range metadata {
		assignments[item.ModelName] = vendorNames[item.VendorID]
	}
	var totals []ConsoleModelUsage
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("user_id = ? AND type = ? AND created_at <= ?", userID, LogTypeConsume, now.Unix()).Select("model_name AS model, " + consoleUsageSelect).Group("model_name").Order("model_name").Scan(&totals).Error; err != nil {
		return nil, err
	}
	var todayModels []ConsoleModelUsage
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userID, LogTypeConsume, today.Unix(), now.Unix()).Select("model_name AS model, " + consoleUsageSelect).Group("model_name").Scan(&todayModels).Error; err != nil {
		return nil, err
	}
	platforms := map[string]*ConsolePlatformUsage{}
	for _, rows := range []struct {
		items []ConsoleModelUsage
		today bool
	}{{totals, false}, {todayModels, true}} {
		for _, item := range rows.items {
			name := assignments[item.Model]
			if platforms[name] == nil {
				platforms[name] = &ConsolePlatformUsage{Name: name}
			}
			bucket := &platforms[name].Total
			if rows.today {
				bucket = &platforms[name].Today
			}
			bucket.Requests += item.Requests
			bucket.Quota += item.Quota
			bucket.Input += item.Input
			bucket.Output += item.Output
		}
	}
	for _, item := range platforms {
		result.Platforms = append(result.Platforms, *item)
	}
	return result, nil
}
