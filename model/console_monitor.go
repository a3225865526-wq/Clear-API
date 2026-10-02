package model

import (
	"context"
	"time"
)

// Publication is an explicit operator opt-in. None of the upstream configuration
// is serialized. These tables are deliberately excluded from startup AutoMigrate.
type ConsoleMonitor struct {
	ID          int `gorm:"primaryKey"`
	ChannelID   int `gorm:"index"`
	DisplayName string
	Platform    string
	Model       string
	Published   bool `gorm:"default:false"`
}

type ConsoleProbe struct {
	ID         int64  `gorm:"primaryKey" json:"id"`
	MonitorID  int    `gorm:"index:idx_console_probe_time,priority:1" json:"-"`
	CheckedAt  int64  `gorm:"index:idx_console_probe_time,priority:2" json:"checked_at"`
	Status     string `json:"status"`
	LatencyMS  *int64 `json:"latency_ms"`
	EndpointMS *int64 `json:"endpoint_ms"`
	// Only fixed safe codes, never raw upstream error text.
	ErrorCode string `json:"error_code"`
}

// MigrateConsoleMonitoring is opt-in tooling, never called during application startup.
func MigrateConsoleMonitoring() error { return DB.AutoMigrate(&ConsoleMonitor{}, &ConsoleProbe{}) }

type ConsoleAvailability struct {
	Percent     *float64 `json:"percent"`
	Samples     int      `json:"samples"`
	FirstSample *int64   `json:"first_sample"`
	AverageMS   *float64 `json:"average_ms"`
}

type ConsoleMonitorView struct {
	ID      int                         `json:"id"`
	Model   string                      `json:"model"`
	Status  string                      `json:"status"`
	Latest  *ConsoleProbe               `json:"latest"`
	History []ConsoleProbe              `json:"history"`
	Windows map[int]ConsoleAvailability `json:"windows"`
}

type ConsoleChannelView struct {
	ID       int                  `json:"id"`
	Name     string               `json:"name"`
	Platform string               `json:"platform"`
	Models   []ConsoleMonitorView `json:"models"`
}

type ConsoleMonitorData struct {
	Ready             bool                 `json:"ready"`
	ReadAt            int64                `json:"read_at"`
	StaleAfterSeconds int64                `json:"stale_after_seconds"`
	Channels          []ConsoleChannelView `json:"channels"`
}

func ConsoleProbeWindow(records []ConsoleProbe, days int, now time.Time) ConsoleAvailability {
	result := ConsoleAvailability{}
	start := now.AddDate(0, 0, -days).Unix()
	var successful, latencyCount int
	var latencyTotal int64
	for _, record := range records {
		if record.CheckedAt < start || record.CheckedAt > now.Unix() {
			continue
		}
		if record.Status != "normal" && record.Status != "degraded" && record.Status != "error" {
			continue
		}
		result.Samples++
		if result.FirstSample == nil || record.CheckedAt < *result.FirstSample {
			value := record.CheckedAt
			result.FirstSample = &value
		}
		if record.Status == "normal" || record.Status == "degraded" {
			successful++
			if record.LatencyMS != nil {
				latencyTotal += *record.LatencyMS
				latencyCount++
			}
		}
	}
	if result.Samples > 0 {
		value := float64(successful) * 100 / float64(result.Samples)
		result.Percent = &value
	}
	if latencyCount > 0 {
		value := float64(latencyTotal) / float64(latencyCount)
		result.AverageMS = &value
	}
	return result
}

func GetConsoleMonitoring(ctx context.Context, now time.Time) (*ConsoleMonitorData, error) {
	result := &ConsoleMonitorData{ReadAt: now.Unix(), StaleAfterSeconds: 900, Channels: []ConsoleChannelView{}}
	if !DB.Migrator().HasTable(&ConsoleMonitor{}) || !DB.Migrator().HasTable(&ConsoleProbe{}) {
		return result, nil
	}
	result.Ready = true
	var monitors []ConsoleMonitor
	if err := DB.WithContext(ctx).Where("published = ?", true).Order("channel_id, id").Find(&monitors).Error; err != nil {
		return nil, err
	}
	for _, monitor := range monitors {
		view := ConsoleMonitorView{ID: monitor.ID, Model: monitor.Model, Status: "unknown", History: []ConsoleProbe{}, Windows: map[int]ConsoleAvailability{}}
		var records []ConsoleProbe
		if err := DB.WithContext(ctx).Where("monitor_id = ? AND checked_at >= ? AND checked_at <= ?", monitor.ID, now.AddDate(0, 0, -30).Unix(), now.Unix()).Order("checked_at DESC, id DESC").Find(&records).Error; err != nil {
			return nil, err
		}
		// Last 60 are independent of the selected availability window.
		if err := DB.WithContext(ctx).Where("monitor_id = ? AND checked_at <= ?", monitor.ID, now.Unix()).Order("checked_at DESC, id DESC").Limit(60).Find(&view.History).Error; err != nil {
			return nil, err
		}
		if len(view.History) > 0 {
			latest := view.History[0]
			view.Latest = &latest
			if now.Unix()-latest.CheckedAt <= result.StaleAfterSeconds {
				view.Status = latest.Status
			}
		}
		for left, right := 0, len(view.History)-1; left < right; left, right = left+1, right-1 {
			view.History[left], view.History[right] = view.History[right], view.History[left]
		}
		for _, days := range []int{7, 15, 30} {
			view.Windows[days] = ConsoleProbeWindow(records, days, now.In(ConsoleBusinessZone))
		}
		if len(result.Channels) == 0 || result.Channels[len(result.Channels)-1].ID != monitor.ChannelID {
			result.Channels = append(result.Channels, ConsoleChannelView{ID: monitor.ChannelID, Name: monitor.DisplayName, Platform: monitor.Platform, Models: []ConsoleMonitorView{}})
		}
		result.Channels[len(result.Channels)-1].Models = append(result.Channels[len(result.Channels)-1].Models, view)
	}
	return result, nil
}

// Save only results of an existing scheduled test, without creating extra requests.
func RecordConsoleProbe(ctx context.Context, channelID int, testModel, status string, milliseconds int64) error {
	if !DB.Migrator().HasTable(&ConsoleMonitor{}) || !DB.Migrator().HasTable(&ConsoleProbe{}) {
		return nil
	}
	var monitors []ConsoleMonitor
	if err := DB.WithContext(ctx).Where("channel_id = ? AND model = ? AND published = ?", channelID, testModel, true).Find(&monitors).Error; err != nil {
		return err
	}
	for _, monitor := range monitors {
		record := ConsoleProbe{MonitorID: monitor.ID, CheckedAt: time.Now().Unix(), Status: status, LatencyMS: &milliseconds}
		if status == "error" {
			record.ErrorCode = "probe_failed"
		}
		if err := DB.WithContext(ctx).Create(&record).Error; err != nil {
			return err
		}
	}
	return nil
}
