package model

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PoolRoutingOptionPrefix = "ClearAPIPoolRoute:"

type PoolModelRoute struct {
	Model      string `json:"model"`
	Platform   string `json:"platform"`
	AccessType string `json:"access_type"`
	Source     string `json:"source"`
	Enabled    bool   `json:"enabled"`
	Version    int    `json:"version"`
	UpdatedAt  int64  `json:"updated_at"`
}

type poolRouteRecord struct {
	Route       PoolModelRoute `json:"route"`
	OperationID string         `json:"operation_id"`
}

func IsPoolRoutingOption(key string) bool { return strings.HasPrefix(key, PoolRoutingOptionPrefix) }

// Pool routes use the existing option snapshot, loaded at startup and updated
// after the database commit. This local deployment is a single application
// instance; multi-instance publishing requires a shared revision barrier.
func GetPoolModelRoute(modelName string) (*PoolModelRoute, error) {
	common.OptionMapRWMutex.RLock()
	raw, exists := common.OptionMap[PoolRoutingOptionPrefix+modelName]
	common.OptionMapRWMutex.RUnlock()
	if !exists {
		return nil, nil
	}
	var record poolRouteRecord
	if common.UnmarshalJsonStr(raw, &record) != nil || record.Route.Model != modelName || record.Route.Version < 1 || (record.Route.Source != "api" && record.Route.Source != "pool") {
		return nil, ErrPoolUnavailable
	}
	return &record.Route, nil
}

func ListPoolModelRoutes() ([]PoolModelRoute, error) {
	common.OptionMapRWMutex.RLock()
	keys := []string{}
	for key := range common.OptionMap {
		if IsPoolRoutingOption(key) {
			keys = append(keys, strings.TrimPrefix(key, PoolRoutingOptionPrefix))
		}
	}
	common.OptionMapRWMutex.RUnlock()
	sort.Strings(keys)
	routes := make([]PoolModelRoute, 0, len(keys))
	for _, key := range keys {
		route, err := GetPoolModelRoute(key)
		if err != nil {
			return nil, err
		}
		if route != nil {
			routes = append(routes, *route)
		}
	}
	return routes, nil
}

func SavePoolModelRoute(route PoolModelRoute, expectedVersion int) (*PoolModelRoute, error) {
	if DB == nil {
		return nil, ErrPoolUnavailable
	}
	if route.Model != strings.TrimSpace(route.Model) || route.Model == "" || len(route.Model) > 128 || strings.ContainsAny(route.Model, "*\x00\r\n") || expectedVersion < 0 || (route.Source != "api" && route.Source != "pool") {
		return nil, ErrPoolInvalid
	}
	key := PoolRoutingOptionPrefix + route.Model
	route.Version = expectedVersion + 1
	route.UpdatedAt = time.Now().Unix()
	record := poolRouteRecord{Route: route, OperationID: common.GetUUID()}
	encoded, err := common.Marshal(record)
	if err != nil {
		return nil, err
	}
	value := string(encoded)
	err = DB.Transaction(func(tx *gorm.DB) error {
		if expectedVersion == 0 {
			candidate := Option{Key: key, Value: value}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate).Error; err != nil {
				return err
			}
			var stored Option
			if err := tx.Where(&Option{Key: key}).First(&stored).Error; err != nil {
				return err
			}
			if stored.Value != value {
				return ErrPoolConflict
			}
			return nil
		}
		var stored Option
		if err := tx.Where(&Option{Key: key}).First(&stored).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPoolConflict
			}
			return err
		}
		var previous poolRouteRecord
		if common.UnmarshalJsonStr(stored.Value, &previous) != nil || previous.Route.Version != expectedVersion {
			return ErrPoolConflict
		}
		result := tx.Model(&Option{}).Where(&Option{Key: key}).Where("value = ?", stored.Value).Update("value", value)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPoolConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	var current poolRouteRecord
	_ = common.UnmarshalJsonStr(common.OptionMap[key], &current)
	if current.Route.Version < route.Version {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	return &route, nil
}

func (channel *Channel) CheckPoolModelRoute(modelName string) error {
	route, err := GetPoolModelRoute(modelName)
	if err != nil || route == nil {
		return err
	}
	if !route.Enabled {
		return errors.New("MODEL_SOURCE_PAUSED")
	}
	if route.Source == "pool" {
		return ErrPoolChannelAdmission
	}
	// A nil channel validates the source policy before candidate selection.
	if channel != nil && channel.PoolChannelBound() {
		return errors.New("MODEL_SOURCE_MISMATCH")
	}
	return nil
}
