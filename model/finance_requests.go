package model

import "gorm.io/gorm"

// Append-only observations, deliberately separate from public consume logs.
// Payload is a bounded typed snapshot of finance facts, never a request body,
// header map, provider error, token key or complete RelayInfo.
type FinanceRequestEvent struct {
	ID            int64  `json:"id"`
	EventKey      string `json:"event_key" gorm:"type:varchar(100);uniqueIndex"`
	RequestID     string `json:"request_id" gorm:"type:varchar(64);index"`
	AttemptID     string `json:"attempt_id" gorm:"type:varchar(64);index"`
	Phase         string `json:"phase" gorm:"type:varchar(32)"`
	UserID        int    `json:"user_id"`
	ChannelID     int    `json:"channel_id" gorm:"index"`
	UpstreamModel string `json:"upstream_model" gorm:"type:varchar(200)"`
	At            int64  `json:"at" gorm:"index"`
	Payload       string `json:"-" gorm:"type:text"`
}

func FinanceRequestsReady(db *gorm.DB) error {
	if db == nil {
		return ErrFinanceUnavailable
	}
	var schema FinanceSchema
	if err := db.First(&schema, 1).Error; err != nil || schema.Version < 3 || schema.Version > 9 {
		return ErrFinanceUnavailable
	}
	return nil
}

func MigrateFinanceRequests(db *gorm.DB) error {
	if err := MigrateFinanceRevenue(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&FinanceRequestEvent{}); err != nil {
		return err
	}
	return db.Model(&FinanceSchema{}).Where("id = ? AND version = ?", 1, 2).Update("version", 3).Error
}
