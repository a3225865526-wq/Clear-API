package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Existing and newly configured groups are enabled unless explicitly disabled.
// Store one row per group in the existing options table: no schema rewrite and
// no changes to bindings, prices, orders or historical accounting records.
const groupStatusPrefix = "GroupStatus:"

var ErrGroupDisabled = errors.New("GROUP_DISABLED: group temporarily unavailable")

type persistedGroupStatus struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled"`
}

// Option keys can use case/accent-insensitive MySQL collations. Hash the exact
// identifier so disabling one group never changes a differently named group.
func groupStatusKey(name string) string {
	sum := sha256.Sum256([]byte(name))
	return groupStatusPrefix + hex.EncodeToString(sum[:])
}

func parseGroupStatus(row Option) (persistedGroupStatus, error) {
	var status persistedGroupStatus
	if common.UnmarshalJsonStr(row.Value, &status) != nil || status.Name == "" || status.Enabled == nil || row.Key != groupStatusKey(status.Name) {
		return status, errors.New("invalid persisted group status")
	}
	return status, nil
}

func GetDisabledGroups() (map[string]bool, error) {
	if DB == nil {
		return nil, errors.New("group status database unavailable")
	}
	var rows []Option
	if err := DB.Where(clause.Like{Column: clause.Column{Name: "key"}, Value: groupStatusPrefix + "%"}).Find(&rows).Error; err != nil {
		return nil, err
	}
	disabled := make(map[string]bool, len(rows))
	for _, row := range rows {
		status, err := parseGroupStatus(row)
		if err != nil {
			return nil, err
		}
		disabled[status.Name] = !*status.Enabled
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
			keys = append(keys, groupStatusKey(group))
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
		status, err := parseGroupStatus(row)
		if err != nil {
			return err
		}
		if !*status.Enabled {
			return ErrGroupDisabled
		}
	}
	return nil
}

func SetGroupEnabled(name string, enabled bool) error {
	value, err := common.Marshal(persistedGroupStatus{Name: name, Enabled: &enabled})
	if err != nil {
		return err
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Option{Key: groupStatusKey(name), Value: string(value)}).Error
}
