package model

import (
	"context"
	"time"
)

// Publication is an explicit operator opt-in. None of the upstream configuration
// is serialized. These tables are deliberately excluded from startup AutoMigrate.
type ConsoleMonitor struct {
	ID          int    `gorm:"primaryKey" json:"id"`
	ChannelID   int    `gorm:"index" json:"channel_id"`
	DisplayName string `json:"display_name"`
	Platform    string `json:"platform"`
	Model       string `json:"model"`
	Published   bool   `gorm:"default:false" json:"published"`
	// Nullable identity preserves legacy rows; new targets use a canonical hash.
	Identity        *string `gorm:"size:64;uniqueIndex" json:"identity"`
	TargetKind      string  `gorm:"size:32" json:"target_kind"`
	TargetRef       string  `gorm:"size:255" json:"target_ref"`
	Protocol        string  `gorm:"size:32" json:"protocol"`
	Enabled         bool    `json:"enabled"`
	Archived        bool    `json:"archived"`
	IntervalSeconds int64   `json:"interval_seconds"`
	TimeoutSeconds  int64   `json:"timeout_seconds"`
	StaleSeconds    int64   `json:"stale_seconds"`
	DailyRequests   int64   `json:"daily_requests"`
	// Monitoring budget reservation only: this is NOT a provider-enforced spending
	// ceiling or a retail billing price. Real transport must separately enforce one.
	RequestCostMicros int64 `json:"request_cost_micros"`

	// Allowlisted is administrative eligibility, never an execution switch.
	Allowlisted bool  `json:"-"`
	CreatedBy   int   `json:"-"`
	CreatedAt   int64 `gorm:"autoCreateTime:false" json:"-"`
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
	// Administrative attribution only, never exposed on the public status page.
	SourceChannelID int    `json:"-"`
	SourceKind      string `gorm:"size:32" json:"-"`
}

// SafeResult also protects legacy records and future adapters at API boundaries.
func (p *ConsoleProbe) SafeResult() {
	switch p.Status {
	case "normal", "degraded", "error", "unknown", "skipped":
	default:
		p.Status = "unknown"
	}
	switch p.ErrorCode {
	case "", "probe_failed", "probe_timeout", "authorization_denied", "invalid_result", "MONITOR_COST_UNVERIFIED", "MONITOR_COST_EXCEEDED":
	default:
		p.ErrorCode = "probe_failed"
	}
}

// MigrateConsoleMonitoring is opt-in tooling, never called during application startup.
func MigrateConsoleMonitoring() error { return MigrateConsoleMonitoringDB(DB) }

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
	ID         int                  `json:"id"`
	Name       string               `json:"name"`
	Platform   string               `json:"platform"`
	Models     []ConsoleMonitorView `json:"models"`
	TargetKind string               `json:"target_kind,omitempty"`
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
	query := DB.WithContext(ctx).Where("published = ?", true)
	if DB.Migrator().HasColumn(&ConsoleMonitor{}, "archived") {
		query = query.Where("archived = ?", false)
	}
	if err := query.Order("channel_id, id").Find(&monitors).Error; err != nil {
		return nil, err
	}
	channelIndices := make(map[int]int)
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
		for i := range view.History {
			view.History[i].SafeResult()
		}
		if len(view.History) > 0 {
			latest := view.History[0]
			view.Latest = &latest
			staleSeconds := result.StaleAfterSeconds
			if monitor.StaleSeconds > 0 {
				staleSeconds = monitor.StaleSeconds
			}
			if now.Unix()-latest.CheckedAt <= staleSeconds {
				view.Status = latest.Status
			}
		}
		for left, right := 0, len(view.History)-1; left < right; left, right = left+1, right-1 {
			view.History[left], view.History[right] = view.History[right], view.History[left]
		}
		for _, days := range []int{7, 15, 30} {
			view.Windows[days] = ConsoleProbeWindow(records, days, now.In(ConsoleBusinessZone))
		}
		publicID := monitor.ChannelID
		if monitor.TargetKind != "" && monitor.TargetKind != "channel" {
			publicID = -monitor.ID
		}
		index, found := channelIndices[publicID]
		if !found {
			index = len(result.Channels)
			channelIndices[publicID] = index
			result.Channels = append(result.Channels, ConsoleChannelView{ID: publicID, Name: monitor.DisplayName, Platform: monitor.Platform, TargetKind: monitor.TargetKind, Models: []ConsoleMonitorView{}})
		}
		result.Channels[index].Models = append(result.Channels[index].Models, view)
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
		// New control-plane targets may only receive records from the independent
		// executor; legacy channel_test cannot populate a group or managed target.
		if monitor.Identity != nil {
			continue
		}
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
