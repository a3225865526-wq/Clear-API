package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Route Chain configuration (design: docs/superpowers/specs/2026-10-06-resilient-route-chain-design.md).
// A chain is an operator-authored ordered candidate list for one (model, group).
// Without a chain row, routing behavior is byte-for-byte unchanged.

const (
	RouteChainCandidateTypePool       = "pool"
	RouteChainCandidateTypeAPIChannel = "api_channel"

	// RouteChainMaxCandidateRows caps how many candidates one chain may hold.
	// It bounds configuration size only; the per-request attempt cap is a
	// separate engine-side limit (RouteChainMaxCandidates option, stage 3).
	RouteChainMaxCandidateRows = 10
)

var (
	ErrRouteChainVersionConflict = errors.New("route chain version conflict")
	ErrRouteChainNotFound        = errors.New("route chain not found")
	ErrRouteChainExists          = errors.New("route chain already exists for model and group")
	ErrRouteChainInvalid         = errors.New("route chain configuration invalid")
	ErrRouteChainCandidateSet    = errors.New("route chain candidate set mismatch")
)

type RouteChain struct {
	Id               int    `json:"id"`
	Model            string `json:"model" gorm:"type:varchar(255);uniqueIndex:idx_route_chain_model_group,priority:1"`
	Group            string `json:"group" gorm:"type:varchar(64);uniqueIndex:idx_route_chain_model_group,priority:2"`
	Enabled          bool   `json:"enabled" gorm:"default:false"`
	AllowCrossSource bool   `json:"allow_cross_source" gorm:"default:false"`
	Version          int    `json:"version" gorm:"default:1"`
	CreatedTime      int64  `json:"created_time"`
	UpdatedTime      int64  `json:"updated_time"`
}

type RouteChainCandidate struct {
	Id            int    `json:"id"`
	ChainId       int    `json:"chain_id" gorm:"index;uniqueIndex:idx_route_chain_candidate_target,priority:1"`
	CandidateType string `json:"candidate_type" gorm:"type:varchar(16);uniqueIndex:idx_route_chain_candidate_target,priority:2"`
	TargetId      int    `json:"target_id" gorm:"uniqueIndex:idx_route_chain_candidate_target,priority:3"`
	// Priority ordering is unique per chain by app-level validation; it is a
	// plain index on purpose — a DB unique constraint would reject the
	// transient states of reorder/replace flows (e.g. swapped priorities).
	Priority    int    `json:"priority" gorm:"index"`
	Enabled     bool   `json:"enabled" gorm:"default:true"`
	Policy      string `json:"policy" gorm:"type:text"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

// RouteChainWithCandidates pairs a chain row with its candidates ordered by
// descending priority. The association is filled manually so the table set
// stays plain AutoMigrate on all three dialects; gorm:"-" keeps GORM from
// interpreting the slice as an association.
type RouteChainWithCandidates struct {
	RouteChain
	Candidates []RouteChainCandidate `json:"candidates" gorm:"-"`
}

// RouteCandidatePolicy is the per-candidate JSON policy. Every field is
// optional; defaults are conservative and applied by the getter methods, so
// an absent policy behaves identically to the built-in defaults.
type RouteCandidatePolicy struct {
	Health       *RouteHealthPolicy  `json:"health_policy,omitempty"`
	Failure      *RouteFailurePolicy `json:"failure_policy,omitempty"`
	Timeout      *RouteTimeoutPolicy `json:"timeout_policy,omitempty"`
	Cost         map[string]any      `json:"cost_policy,omitempty"` // reserved, stage 6
	Capabilities []string            `json:"capabilities,omitempty"`
}

type RouteHealthPolicy struct {
	// FailureThreshold: consecutive attributable failures before OPEN. Default 3.
	FailureThreshold int `json:"failure_threshold,omitempty"`
	// CooldownSeconds: OPEN duration before HALF_OPEN. Default 60.
	CooldownSeconds int `json:"cooldown_seconds,omitempty"`
}

type RouteFailurePolicy struct {
	// FailoverOn429 defaults to false (production-conservative, requirement §7).
	FailoverOn429 *bool `json:"failover_on_429,omitempty"`
	// FailoverOn404 defaults to true: a mapped-channel model reject is a
	// safe candidate-local signal when the zero-work predicate holds.
	FailoverOn404 *bool `json:"failover_on_404,omitempty"`
}

type RouteTimeoutPolicy struct {
	// ConnectTimeoutMs bounds connection establishment per candidate; 0 inherits.
	ConnectTimeoutMs int `json:"connect_timeout_ms,omitempty"`
}

func (p *RouteCandidatePolicy) FailureThreshold() int {
	if p == nil || p.Health == nil || p.Health.FailureThreshold <= 0 {
		return 3
	}
	return p.Health.FailureThreshold
}

func (p *RouteCandidatePolicy) CooldownSeconds() int {
	if p == nil || p.Health == nil || p.Health.CooldownSeconds <= 0 {
		return 60
	}
	return p.Health.CooldownSeconds
}

func (p *RouteCandidatePolicy) FailoverOn429() bool {
	return p != nil && p.Failure != nil && p.Failure.FailoverOn429 != nil && *p.Failure.FailoverOn429
}

func (p *RouteCandidatePolicy) FailoverOn404() bool {
	return !(p != nil && p.Failure != nil && p.Failure.FailoverOn404 != nil && !*p.Failure.FailoverOn404)
}

func (p *RouteCandidatePolicy) ConnectTimeoutMs() int {
	if p == nil || p.Timeout == nil {
		return 0
	}
	return p.Timeout.ConnectTimeoutMs
}

var routeChainAllowedCapabilities = map[string]bool{
	"stream": true, "tools": true, "vision": true, "reasoning": true,
	"audio": true, "json_schema": true, "cache": true,
}

func ParseRouteCandidatePolicy(policyJSON string) (*RouteCandidatePolicy, error) {
	trimmed := strings.TrimSpace(policyJSON)
	if trimmed == "" {
		return &RouteCandidatePolicy{}, nil
	}
	policy := &RouteCandidatePolicy{}
	if err := common.UnmarshalJsonStr(trimmed, policy); err != nil {
		return nil, ErrRouteChainInvalid
	}
	if policy.Health != nil {
		if policy.Health.FailureThreshold < 0 || policy.Health.FailureThreshold > 20 ||
			policy.Health.CooldownSeconds < 0 || policy.Health.CooldownSeconds > 3600 {
			return nil, ErrRouteChainInvalid
		}
	}
	if policy.Timeout != nil {
		if policy.Timeout.ConnectTimeoutMs < 0 || policy.Timeout.ConnectTimeoutMs > 120000 {
			return nil, ErrRouteChainInvalid
		}
	}
	for _, capability := range policy.Capabilities {
		if !routeChainAllowedCapabilities[capability] {
			return nil, ErrRouteChainInvalid
		}
	}
	return policy, nil
}

func validateRouteChain(chain *RouteChainWithCandidates) error {
	if strings.TrimSpace(chain.Model) == "" || strings.TrimSpace(chain.Group) == "" {
		return ErrRouteChainInvalid
	}
	if len(chain.Candidates) == 0 || len(chain.Candidates) > RouteChainMaxCandidateRows {
		return ErrRouteChainInvalid
	}
	seenPriority := make(map[int]bool, len(chain.Candidates))
	seenTarget := make(map[string]bool, len(chain.Candidates))
	for i := range chain.Candidates {
		candidate := &chain.Candidates[i]
		if candidate.CandidateType != RouteChainCandidateTypePool && candidate.CandidateType != RouteChainCandidateTypeAPIChannel {
			return ErrRouteChainInvalid
		}
		if candidate.TargetId <= 0 {
			return ErrRouteChainInvalid
		}
		if seenPriority[candidate.Priority] {
			return ErrRouteChainInvalid
		}
		seenPriority[candidate.Priority] = true
		targetKey := candidate.CandidateType + ":" + strconv.Itoa(candidate.TargetId)
		if seenTarget[targetKey] {
			return ErrRouteChainInvalid
		}
		seenTarget[targetKey] = true
		if _, err := ParseRouteCandidatePolicy(candidate.Policy); err != nil {
			return err
		}
	}
	return nil
}

func MigrateRouteChain(db *gorm.DB) error {
	return db.AutoMigrate(&RouteChain{}, &RouteChainCandidate{})
}

func loadRouteChainCandidates(db *gorm.DB, chainId int) ([]RouteChainCandidate, error) {
	var candidates []RouteChainCandidate
	err := db.Where("chain_id = ?", chainId).Order("priority DESC, id ASC").Find(&candidates).Error
	return candidates, err
}

func GetRouteChainById(id int) (*RouteChainWithCandidates, error) {
	var chain RouteChain
	if err := DB.First(&chain, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRouteChainNotFound
		}
		return nil, err
	}
	candidates, err := loadRouteChainCandidates(DB, chain.Id)
	if err != nil {
		return nil, err
	}
	return &RouteChainWithCandidates{RouteChain: chain, Candidates: candidates}, nil
}

// GetRouteChainForModel returns the chain configured for (model, group) or
// nil when none exists. Callers decide what Enabled means for them.
func GetRouteChainForModel(modelName string, groupName string) (*RouteChainWithCandidates, error) {
	var chain RouteChain
	err := DB.Where("model = ? AND "+commonGroupCol+" = ?", modelName, groupName).First(&chain).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	candidates, err := loadRouteChainCandidates(DB, chain.Id)
	if err != nil {
		return nil, err
	}
	return &RouteChainWithCandidates{RouteChain: chain, Candidates: candidates}, nil
}

// GetEnabledRouteChainForModel is the engine-facing read: nil means the
// request keeps legacy routing.
func GetEnabledRouteChainForModel(modelName string, groupName string) (*RouteChainWithCandidates, error) {
	chain, err := GetRouteChainForModel(modelName, groupName)
	if err != nil || chain == nil || !chain.Enabled {
		return nil, err
	}
	return chain, nil
}

func GetRouteChains() ([]RouteChainWithCandidates, error) {
	var chains []RouteChain
	if err := DB.Order("model ASC, " + commonGroupCol + " ASC").Find(&chains).Error; err != nil {
		return nil, err
	}
	result := make([]RouteChainWithCandidates, 0, len(chains))
	for _, chain := range chains {
		candidates, err := loadRouteChainCandidates(DB, chain.Id)
		if err != nil {
			return nil, err
		}
		result = append(result, RouteChainWithCandidates{RouteChain: chain, Candidates: candidates})
	}
	return result, nil
}

// UpsertRouteChain creates or updates a chain under optimistic concurrency.
// expectedVersion must equal the stored version for updates; new chains use 0.
// Existing candidate rows are synced by id (update), rows absent from the
// payload are deleted, payload rows without id are inserted — so candidate
// ids stay stable for the enable/disable and circuit endpoints.
func UpsertRouteChain(chain *RouteChainWithCandidates, expectedVersion int) error {
	if err := validateRouteChain(chain); err != nil {
		return err
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		if chain.Id == 0 {
			var count int64
			if err := tx.Model(&RouteChain{}).Where("model = ? AND "+commonGroupCol+" = ?", chain.Model, chain.Group).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrRouteChainExists
			}
			chain.Version = 1
			chain.CreatedTime = now
			chain.UpdatedTime = now
			if err := tx.Create(&chain.RouteChain).Error; err != nil {
				return err
			}
			for i := range chain.Candidates {
				chain.Candidates[i].Id = 0
				chain.Candidates[i].ChainId = chain.Id
				chain.Candidates[i].CreatedTime = now
				chain.Candidates[i].UpdatedTime = now
			}
			if len(chain.Candidates) > 0 {
				return tx.Create(&chain.Candidates).Error
			}
			return nil
		}
		result := tx.Model(&RouteChain{}).
			Where("id = ? AND version = ?", chain.Id, expectedVersion).
			Select("model", "group", "enabled", "allow_cross_source", "version", "updated_time").
			Updates(RouteChain{
				Model:            chain.Model,
				Group:            chain.Group,
				Enabled:          chain.Enabled,
				AllowCrossSource: chain.AllowCrossSource,
				Version:          expectedVersion + 1,
				UpdatedTime:      now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRouteChainVersionConflict
		}
		existing, err := loadRouteChainCandidates(tx, chain.Id)
		if err != nil {
			return err
		}
		existingIds := make(map[int]bool, len(existing))
		for _, candidate := range existing {
			existingIds[candidate.Id] = true
		}
		keptIds := make(map[int]bool, len(chain.Candidates))
		for i := range chain.Candidates {
			if candidate := &chain.Candidates[i]; candidate.Id > 0 && existingIds[candidate.Id] {
				keptIds[candidate.Id] = true
			}
		}
		// Delete removed rows before touching kept ones so a new or updated
		// candidate can take over a freed target/priority slot.
		for _, candidate := range existing {
			if !keptIds[candidate.Id] {
				if err := tx.Delete(&RouteChainCandidate{}, candidate.Id).Error; err != nil {
					return err
				}
			}
		}
		for i := range chain.Candidates {
			candidate := &chain.Candidates[i]
			if candidate.Id > 0 && keptIds[candidate.Id] {
				if err := tx.Model(&RouteChainCandidate{}).
					Where("id = ? AND chain_id = ?", candidate.Id, chain.Id).
					Updates(map[string]any{
						"candidate_type": candidate.CandidateType,
						"target_id":      candidate.TargetId,
						"priority":       candidate.Priority,
						"enabled":        candidate.Enabled,
						"policy":         candidate.Policy,
						"updated_time":   now,
					}).Error; err != nil {
					return err
				}
			} else {
				candidate.Id = 0
				candidate.ChainId = chain.Id
				candidate.CreatedTime = now
				candidate.UpdatedTime = now
				if err := tx.Create(candidate).Error; err != nil {
					return err
				}
				keptIds[candidate.Id] = true
			}
		}
		chain.Version = expectedVersion + 1
		return nil
	})
}

// SetRouteChainCandidateEnabled toggles one candidate under the chain's CAS
// version so concurrent operators cannot interleave edits.
func SetRouteChainCandidateEnabled(chainId int, candidateId int, enabled bool, expectedVersion int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := bumpRouteChainVersion(tx, chainId, expectedVersion); err != nil {
			return err
		}
		result := tx.Model(&RouteChainCandidate{}).
			Where("id = ? AND chain_id = ?", candidateId, chainId).
			Updates(map[string]any{"enabled": enabled, "updated_time": common.GetTimestamp()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRouteChainNotFound
		}
		return nil
	})
}

// ReorderRouteChainCandidates assigns descending priorities following the
// payload order (first element gets the highest priority) under CAS.
func ReorderRouteChainCandidates(chainId int, orderedCandidateIds []int, expectedVersion int) error {
	if len(orderedCandidateIds) == 0 {
		return ErrRouteChainInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		existing, err := loadRouteChainCandidates(tx, chainId)
		if err != nil {
			return err
		}
		if len(existing) != len(orderedCandidateIds) {
			return ErrRouteChainCandidateSet
		}
		existingIds := make(map[int]bool, len(existing))
		for _, candidate := range existing {
			existingIds[candidate.Id] = true
		}
		seen := make(map[int]bool, len(orderedCandidateIds))
		for _, id := range orderedCandidateIds {
			if !existingIds[id] || seen[id] {
				return ErrRouteChainCandidateSet
			}
			seen[id] = true
		}
		if err := bumpRouteChainVersion(tx, chainId, expectedVersion); err != nil {
			return err
		}
		now := common.GetTimestamp()
		for offset, id := range orderedCandidateIds {
			result := tx.Model(&RouteChainCandidate{}).
				Where("id = ? AND chain_id = ?", id, chainId).
				Updates(map[string]any{"priority": len(orderedCandidateIds) - offset, "updated_time": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return ErrRouteChainCandidateSet
			}
		}
		return nil
	})
}

func DeleteRouteChain(id int, expectedVersion int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := bumpRouteChainVersion(tx, id, expectedVersion); err != nil {
			return err
		}
		if err := tx.Where("chain_id = ?", id).Delete(&RouteChainCandidate{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&RouteChain{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRouteChainNotFound
		}
		return nil
	})
}

func bumpRouteChainVersion(tx *gorm.DB, chainId int, expectedVersion int) error {
	result := tx.Model(&RouteChain{}).
		Where("id = ? AND version = ?", chainId, expectedVersion).
		Updates(map[string]any{"version": expectedVersion + 1, "updated_time": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRouteChainVersionConflict
	}
	return nil
}
