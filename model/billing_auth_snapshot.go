package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// BillingAuthSnapshot freezes the authorization facts of one reservation: the
// policy version, the approved hidden usage envelope, the pricing fingerprint
// and the cap chain that authorized the reserved quota. Only billing scalars
// and versions are stored — never prompts, responses, headers or credentials.
// A crash/restart reconciles against this snapshot instead of re-reading
// configuration that may have changed meanwhile.
type BillingAuthSnapshot struct {
	ID                    int64
	RequestID             string  `gorm:"type:varchar(128);uniqueIndex:idx_billing_auth_request_id"`
	PolicyVersion         int     `json:"policy_version"`
	Mode                  string  `gorm:"type:varchar(16)"json:"mode"`
	EstimatorVersion      string  `gorm:"type:varchar(32)"json:"estimator_version"`
	ProfileSource         string  `gorm:"type:varchar(255)"json:"profile_source"`
	ProfileVersion        int     `json:"profile_version"`
	ProfileApproved       bool    `json:"profile_approved"`
	CacheMode             string  `gorm:"type:varchar(16)"json:"cache_mode"`
	PricingMode           string  `gorm:"type:varchar(24)"json:"pricing_mode"`
	PricingFingerprint    string  `gorm:"type:varchar(80)"json:"pricing_fingerprint"`
	GroupRatio            float64 `json:"group_ratio"`
	QuotaPerUnit          float64 `json:"quota_per_unit"`
	Margin                float64 `json:"margin"`
	FloorQuota            int64   `json:"floor_quota"`
	VisibleInputMax       int64   `json:"visible_input_max"`
	HiddenInputMax        int64   `json:"hidden_input_max"`
	HiddenCacheReadMax    int64   `json:"hidden_cache_read_max"`
	HiddenCacheWriteMax   int64   `json:"hidden_cache_write_max"`
	HiddenCacheWrite1hMax int64   `json:"hidden_cache_write_1h_max"`
	OutputMax             int64   `json:"output_max"`
	ContextLenMax         int64   `json:"context_len_max"`
	RequiredQuota         int     `json:"required_quota"`
	CapQuota              int     `json:"cap_quota"`
	ReservedQuota         int     `json:"reserved_quota"`
	UpdatedAt             int64
}

// MigrateBillingAuthSnapshots is an additive, explicitly approved migration;
// it is not part of startup AutoMigrate and never touches existing rows.
func MigrateBillingAuthSnapshots(db *gorm.DB) error {
	if db == nil {
		return errors.New("billing database unavailable")
	}
	return db.AutoMigrate(&BillingAuthSnapshot{})
}

func BillingAuthSnapshotsReady(db *gorm.DB) error {
	if db == nil {
		return errors.New("billing database unavailable")
	}
	if !db.Migrator().HasIndex(&BillingAuthSnapshot{}, "idx_billing_auth_request_id") {
		return errors.New("billing auth snapshot uniqueness index is missing")
	}
	var rows []BillingAuthSnapshot
	return db.Select("id", "request_id", "policy_version", "mode", "estimator_version", "profile_source",
		"required_quota", "cap_quota", "reserved_quota", "updated_at").Limit(0).Find(&rows).Error
}

// UpsertBillingAuthSnapshotTx persists the snapshot inside the caller's
// transaction (reserve or raise) so money movement and authorization facts
// commit atomically. An existing row is overwritten: a raise is a new
// authorization and the latest envelope wins.
func UpsertBillingAuthSnapshotTx(tx *gorm.DB, snapshot *BillingAuthSnapshot) error {
	if snapshot == nil {
		return nil
	}
	if snapshot.RequestID == "" || len(snapshot.RequestID) > 128 {
		return errors.New("invalid billing auth snapshot")
	}
	snapshot.UpdatedAt = time.Now().Unix()
	var existing BillingAuthSnapshot
	err := tx.Where("request_id = ?", snapshot.RequestID).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(snapshot).Error
		}
		return err
	}
	snapshot.ID = existing.ID
	return tx.Model(&BillingAuthSnapshot{}).Where("id = ?", existing.ID).Select("*").Updates(snapshot).Error
}

// UpsertBillingAuthSnapshot records dispatch-time authorization facts when no
// quota raise was required. It is idempotent and additive; failure to record
// must fail the dispatch because enforce mode guarantees a durable snapshot.
func UpsertBillingAuthSnapshot(snapshot *BillingAuthSnapshot) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return UpsertBillingAuthSnapshotTx(tx, snapshot)
	})
}

func GetBillingAuthSnapshot(requestID string) (*BillingAuthSnapshot, error) {
	var snapshot BillingAuthSnapshot
	if err := DB.Where("request_id = ?", requestID).First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}
