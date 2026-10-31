package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Existing and newly configured groups are enabled unless explicitly disabled.
// Store one row per group in the existing options table: no schema rewrite and
// no changes to bindings, prices, orders or historical accounting records.
const groupDisabledPrefix = "GroupDisabled:"

var ErrGroupDisabled = errors.New("GROUP_DISABLED: group temporarily unavailable")

func GetDisabledGroups() (map[string]bool, error) {
	if DB == nil {
		return nil, errors.New("group status database unavailable")
	}
	var rows []Option
	if err := DB.Where(clause.Like{Column: clause.Column{Name: "key"}, Value: groupDisabledPrefix + "%"}).Find(&rows).Error; err != nil {
		return nil, err
	}
	disabled := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Value != "true" && row.Value != "false" {
			return nil, errors.New("invalid persisted group status")
		}
		disabled[strings.TrimPrefix(row.Key, groupDisabledPrefix)] = row.Value == "true"
	}
	return disabled, nil
}

// Always read the primary database, never the option/user/token/Redis cache.
// A request admitted before a disable commit may finish normal settlement.
func CheckGroupsEnabled(db *gorm.DB, groups ...string) error {
	if db == nil {
		return errors.New("group status database unavailable")
	}
	keys := make([]string, 0, len(groups))
	for _, group := range groups {
		if group != "" {
			keys = append(keys, groupDisabledPrefix+group)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	var rows []Option
	if err := db.Where(map[string]any{"key": keys}).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if row.Value == "true" {
			return ErrGroupDisabled
		}
		if row.Value != "false" {
			return errors.New("invalid persisted group status")
		}
	}
	return nil
}

func SetGroupEnabled(name string, enabled bool) error {
	value := "true"
	if enabled {
		value = "false"
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Option{Key: groupDisabledPrefix + name, Value: value}).Error
}
