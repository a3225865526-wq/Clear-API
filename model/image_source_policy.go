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

// Operator image source policy.
//
// The site owner alone decides whether an image generation may be served by the
// account pool, by an ordinary API channel, or by both with a priority. The
// presets are projections over the existing Route Chain candidate model
// (pool / api_channel plus allow_cross_source); this store adds no second
// routing engine, it only records which preset is frozen for one model.
//
// The policy is read from the option snapshot and never from client input.
const (
	ImageSourcePolicyPrefix = "ClearAPIImageSourcePolicy:"

	ImageSourcePoolFirst = "pool_first"
	ImageSourceAPIFirst  = "api_first"
	ImageSourcePoolOnly  = "pool_only"
	ImageSourceAPIOnly   = "api_only"
)

var ErrImageSourcePolicyInvalid = errors.New("image source policy invalid")

// ImageSourcePolicy is one operator decision for one model.
type ImageSourcePolicy struct {
	Model     string `json:"model"`
	Strategy  string `json:"strategy"`
	Group     string `json:"group"`
	Enabled   bool   `json:"enabled"`
	Version   int    `json:"version"`
	UpdatedAt int64  `json:"updated_at"`
}

type imageSourcePolicyRecord struct {
	Policy      ImageSourcePolicy `json:"policy"`
	OperationID string            `json:"operation_id"`
}

// NormalizeImageSourceStrategy validates the four supported presets. Unknown
// values are rejected instead of being coerced to a default.
func NormalizeImageSourceStrategy(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case ImageSourcePoolFirst:
		return ImageSourcePoolFirst, nil
	case ImageSourceAPIFirst:
		return ImageSourceAPIFirst, nil
	case ImageSourcePoolOnly:
		return ImageSourcePoolOnly, nil
	case ImageSourceAPIOnly:
		return ImageSourceAPIOnly, nil
	}
	return "", ErrImageSourcePolicyInvalid
}

// AllowsPool and AllowsAPI describe which sources a preset may use at all. The
// *_first presets permit both (the second one is the fallback); the *_only
// presets forbid the other source entirely.
func (policy ImageSourcePolicy) AllowsPool() bool {
	return policy.Enabled && policy.Strategy != ImageSourceAPIOnly
}

func (policy ImageSourcePolicy) AllowsAPI() bool {
	return policy.Enabled && policy.Strategy != ImageSourcePoolOnly
}

// PoolFirst reports the preset's priority. A caller that may only use one
// source gets the same answer as its only-source preset.
func (policy ImageSourcePolicy) PoolFirst() bool {
	return policy.Strategy == ImageSourcePoolFirst || policy.Strategy == ImageSourcePoolOnly
}

func ImageSourcePolicyOptionKey(modelName string) string {
	return ImageSourcePolicyPrefix + modelName
}

func IsImageSourcePolicyOption(key string) bool {
	return strings.HasPrefix(key, ImageSourcePolicyPrefix)
}

// GetImageSourcePolicy reads the frozen operator decision for one model. A
// missing row means "no controlled image policy": existing routing is
// untouched. A corrupt row fails closed with an error, never a silent default.
func GetImageSourcePolicy(modelName string) (*ImageSourcePolicy, error) {
	common.OptionMapRWMutex.RLock()
	raw, exists := common.OptionMap[ImageSourcePolicyOptionKey(modelName)]
	common.OptionMapRWMutex.RUnlock()
	if !exists {
		return nil, nil
	}
	var record imageSourcePolicyRecord
	if common.UnmarshalJsonStr(raw, &record) != nil || record.Policy.Model != modelName || record.Policy.Version < 1 {
		return nil, ErrImageSourcePolicyInvalid
	}
	if _, err := NormalizeImageSourceStrategy(record.Policy.Strategy); err != nil {
		return nil, ErrImageSourcePolicyInvalid
	}
	policy := record.Policy
	return &policy, nil
}

// ListImageSourcePolicies returns every configured decision, ordered by model.
func ListImageSourcePolicies() ([]ImageSourcePolicy, error) {
	common.OptionMapRWMutex.RLock()
	keys := make([]string, 0, len(common.OptionMap))
	for key := range common.OptionMap {
		if IsImageSourcePolicyOption(key) {
			keys = append(keys, key)
		}
	}
	common.OptionMapRWMutex.RUnlock()
	sort.Strings(keys)
	policies := make([]ImageSourcePolicy, 0, len(keys))
	for _, key := range keys {
		policy, err := GetImageSourcePolicy(strings.TrimPrefix(key, ImageSourcePolicyPrefix))
		if err != nil {
			return nil, err
		}
		if policy != nil {
			policies = append(policies, *policy)
		}
	}
	return policies, nil
}

// SaveImageSourcePolicy writes one decision with the same compare-and-set and
// read-back discipline as pool publication. It never changes account trust,
// capability records or channel availability.
func SaveImageSourcePolicy(policy ImageSourcePolicy, expectedVersion int) (*ImageSourcePolicy, error) {
	if DB == nil {
		return nil, ErrPoolUnavailable
	}
	modelName := strings.TrimSpace(policy.Model)
	strategy, err := NormalizeImageSourceStrategy(policy.Strategy)
	if err != nil || modelName == "" || len(modelName) > 128 || strings.ContainsAny(modelName, "*\x00\r\n") || expectedVersion < 0 {
		return nil, ErrImageSourcePolicyInvalid
	}
	group := strings.TrimSpace(policy.Group)
	if len(group) > 64 {
		return nil, ErrImageSourcePolicyInvalid
	}
	key := ImageSourcePolicyOptionKey(modelName)
	policy.Model, policy.Strategy, policy.Group = modelName, strategy, group
	policy.Version = expectedVersion + 1
	policy.UpdatedAt = time.Now().Unix()
	record := imageSourcePolicyRecord{Policy: policy, OperationID: common.GetUUID()}
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
		var previous imageSourcePolicyRecord
		if common.UnmarshalJsonStr(stored.Value, &previous) != nil || previous.Policy.Version != expectedVersion {
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
	var current imageSourcePolicyRecord
	_ = common.UnmarshalJsonStr(common.OptionMap[key], &current)
	if current.Policy.Version < policy.Version {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	return &policy, nil
}
