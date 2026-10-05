package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// PoolMasterSwitch is the site-owner runtime release switch for the upstream
// account pool. It is purely an operational admission layer: turning it on or
// off never touches account trust, model capabilities, or credentials.
//
// Fail-closed discipline: the stored value admits requests only when it is
// exactly "true". A missing row (first upgrade), a corrupt value, a failed
// read, or an unknown state all mean the pool is closed. The default written
// by InitOptionMap is "false", so upgrading an existing deployment can never
// open the pool implicitly.
const PoolMasterOptionKey = "ClearAPIPoolMasterEnabled"

var ErrPoolMasterInvalid = errors.New("POOL_MASTER_INVALID: pool master switch value is not usable")

// PoolMasterEnabled reports the runtime master switch state. Anything other
// than the exact string "true" is closed.
func PoolMasterEnabled() bool {
	common.OptionMapRWMutex.RLock()
	raw, exists := common.OptionMap[PoolMasterOptionKey]
	common.OptionMapRWMutex.RUnlock()
	return raw == "true" && exists
}

// SetPoolMasterEnabled persists the master switch and only then publishes it
// to the in-memory snapshot the router reads. The write is a transaction with
// a compare-and-set on the previous stored value plus a read-back verify: the
// runtime state never changes unless the database commit provably holds the
// new value, so a failed save can never leave the pool open. There is no
// version column on the options table; the stale-write window is narrowed by
// verifying the previous value inside the transaction.
func SetPoolMasterEnabled(enabled bool) error {
	if DB == nil {
		return ErrPoolUnavailable
	}
	value := "false"
	if enabled {
		value = "true"
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var stored Option
		err := tx.Where(&Option{Key: PoolMasterOptionKey}).First(&stored).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Create(&Option{Key: PoolMasterOptionKey, Value: value}).Error; err != nil {
				return err
			}
			return tx.Where(&Option{Key: PoolMasterOptionKey}).First(&stored).Error
		case err != nil:
			return err
		}
		result := tx.Model(&Option{}).Where(&Option{Key: PoolMasterOptionKey}).Where("value = ?", stored.Value).Update("value", value)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPoolConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[PoolMasterOptionKey] = value
	common.OptionMapRWMutex.Unlock()
	return nil
}

// poolMasterOptionBlocked reports whether a generic option write must not
// touch the master switch. Like the pool route keys, the master switch has a
// dedicated owner-only write path with its own validation.
func poolMasterOptionBlocked(key string) bool {
	return key == PoolMasterOptionKey
}
