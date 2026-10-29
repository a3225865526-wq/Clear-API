package model

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
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

// ResolveChannelUpstreamModel resolves the final upstream model name a request
// would carry after the channel's model mapping (chain resolution with cycle
// detection and the same thinking-suffix base fallback the relay helper uses).
// The resolved name — not the requested alias — is what operator admission and
// logging must key on for pool channels, so an alias can never bypass a
// publication decision made on the canonical model.
func ResolveChannelUpstreamModel(channel *Channel, modelName string) string {
	if channel == nil {
		return modelName
	}
	mapping := strings.TrimSpace(channel.GetModelMapping())
	if mapping == "" || mapping == "{}" {
		return modelName
	}
	modelMap := make(map[string]string)
	if common.UnmarshalJsonStr(mapping, &modelMap) != nil {
		// A corrupt mapping fails closed at relay time; the gate resolves to
		// the requested name and the executor still re-checks the real model.
		return modelName
	}
	current := modelName
	visited := map[string]bool{current: true}
	for {
		mapped, exists := modelMap[current]
		baseModel := hostreasoning.BaseModelName(current)
		if (!exists || mapped == "") && baseModel != current {
			mapped, exists = modelMap[baseModel]
		}
		if !exists || mapped == "" || visited[mapped] {
			break
		}
		visited[mapped] = true
		current = mapped
	}
	return current
}

// CheckPoolChannelPoolAdmission is the request-time gate for a pool-bound
// channel. Publication is anchored on the resolved canonical model: the
// requested name is an input/mapping-layer identity and does not need its own
// publication record to reach a published canonical. An explicit operator
// decision about the requested name itself (source=api or paused) still wins,
// so a name the operator pinned away from the pool can never re-enter through
// a mapping or a filter-less retry path. Non-pool channels are not gated here.
func (channel *Channel) CheckPoolChannelPoolAdmission(requestedModel string) error {
	if !channel.PoolChannelBound() {
		return nil
	}
	if route, err := GetPoolModelRoute(requestedModel); err != nil {
		return err
	} else if route != nil && route.Source == "api" {
		return errors.New("MODEL_SOURCE_MISMATCH")
	} else if route != nil && !route.Enabled {
		return errors.New("MODEL_SOURCE_PAUSED")
	}
	canonical := ResolveChannelUpstreamModel(channel, requestedModel)
	return channel.CheckPoolModelRoute(canonical)
}

// channelPoolCanonicalPublished reports whether the canonical model this
// request resolves to on the given channel carries an active pool publication
// (enabled, source=pool, master switch on). Corrupt route state fails closed.
// It is the per-channel routing-side mirror of CheckPoolChannelPoolAdmission
// and keeps pool channels from being selected for requests they would only
// reject after dispatch.
func channelPoolCanonicalPublished(channel *Channel, requestedModel string) bool {
	if !channel.PoolRoutingEnabled() {
		return false
	}
	canonical := ResolveChannelUpstreamModel(channel, requestedModel)
	route, err := GetPoolModelRoute(canonical)
	if err != nil || route != nil && (!route.Enabled || route.Source != "pool") {
		return false
	}
	return PoolMasterEnabled()
}

// PoolRouteDecisions captures the gate inputs for one request so denial logs
// can be audited without re-deriving them. No credential material belongs in
// this structure.
type PoolRouteDecisions struct {
	PoolMasterEnabled  bool   `json:"pool_master_enabled"`
	ChannelPoolRouting bool   `json:"channel_pool_routing"`
	ModelPublished     string `json:"model_published"`
	CanonicalModel     string `json:"canonical_model"`
}

// CheckPoolModelRoute evaluates the model-source policy for one channel and
// model. A nil channel validates the route-level policy before candidate
// selection (preflight); a non-nil channel gets the full request-time gate.
//
// Fail-closed semantics:
//   - corrupt route state: error (never serve)
//   - unconfigured model: use the existing channel/group/model routing; pool
//     channels still require master, channel permission and executor admission
//   - paused route: denied for both sources
//   - api source: pool-bound channels are excluded, others pass
//   - pool source: the master switch must be on, the channel must be
//     pool-bound with routing enabled, and non-pool channels are excluded —
//     a published pool model never falls back to an API channel
func (channel *Channel) CheckPoolModelRoute(modelName string) error {
	route, err := GetPoolModelRoute(modelName)
	if err != nil || route == nil {
		if err != nil {
			return err
		}
		if channel != nil && channel.PoolChannelBound() {
			if !PoolMasterEnabled() {
				return ErrPoolGloballyDisabled
			}
			if !channel.PoolRoutingEnabled() {
				return ErrPoolChannelRoutingOff
			}
		}
		return nil
	}
	if !route.Enabled {
		return errors.New("MODEL_SOURCE_PAUSED")
	}
	if route.Source == "api" {
		if channel != nil && channel.PoolChannelBound() {
			return errors.New("MODEL_SOURCE_MISMATCH")
		}
		return nil
	}
	// route.Source == "pool"
	if !PoolMasterEnabled() {
		return ErrPoolGloballyDisabled
	}
	if channel == nil {
		return nil
	}
	if !channel.PoolChannelBound() {
		return errors.New("MODEL_SOURCE_MISMATCH")
	}
	if !channel.PoolRoutingEnabled() {
		return ErrPoolChannelRoutingOff
	}
	return nil
}
