package accountpool

import (
	"context"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

type FallbackMemberInput struct {
	AccountId int  `json:"account_id"`
	Priority  int  `json:"priority"`
	Enabled   bool `json:"enabled"`
}

type FallbackPolicyInput struct {
	PoolId           string                `json:"pool_id"`
	ModelName        string                `json:"model_name"`
	MaxFallbackCount int                   `json:"max_fallback_count"`
	Enabled          bool                  `json:"enabled"`
	Members          []FallbackMemberInput `json:"members"`
}

// ValidateFallbackScope disallows wildcard policies and unbounded input keys.
func ValidateFallbackScope(poolID, modelName string) error {
	if poolID == "" || modelName == "" || len(poolID) > 64 || len(modelName) > 128 ||
		poolID != strings.TrimSpace(poolID) || modelName != strings.TrimSpace(modelName) ||
		strings.ContainsAny(poolID+modelName, "*\x00\r\n") {
		return ErrPoolInvalid
	}
	return nil
}

func GetFallbackPolicy(ctx context.Context, poolID, modelName string) (*model.PoolFallbackPolicy, error) {
	if err := ValidateFallbackScope(poolID, modelName); err != nil {
		return nil, err
	}
	return (model.PoolFallbackRepository{DB: db()}).Get(ctx, poolID, modelName)
}

func SaveFallbackPolicy(ctx context.Context, input FallbackPolicyInput) (*model.PoolFallbackPolicy, error) {
	if err := ValidateFallbackScope(input.PoolId, input.ModelName); err != nil {
		return nil, err
	}
	// The MVP has a four-account list as well as a three-fallback hard limit.
	if input.MaxFallbackCount < 0 || input.MaxFallbackCount > model.MaxPoolFallbackCount || len(input.Members) > model.MaxPoolFallbackCount+1 || (input.Enabled && len(input.Members) == 0) {
		return nil, ErrPoolInvalid
	}
	seen := make(map[int]bool, len(input.Members))
	ids := make([]int, 0, len(input.Members))
	enabled := false
	for _, member := range input.Members {
		if member.AccountId <= 0 || seen[member.AccountId] || member.Priority < 0 || member.Priority > 1000000 {
			return nil, ErrPoolInvalid
		}
		seen[member.AccountId] = true
		ids = append(ids, member.AccountId)
		enabled = enabled || member.Enabled
	}
	if input.Enabled && !enabled {
		return nil, ErrPoolInvalid
	}
	if db() == nil {
		return nil, ErrPoolUnavailable
	}
	if len(ids) > 0 {
		var accounts []model.UpstreamPoolAccount
		if err := db().WithContext(ctx).Where("id IN ?", ids).Find(&accounts).Error; err != nil {
			return nil, err
		}
		if len(accounts) != len(ids) {
			return nil, ErrPoolInvalid
		}
		for _, account := range accounts {
			if account.Platform != "codex" || account.ExecutorId != ExecutorCliproxyAPI {
				return nil, ErrPoolInvalid
			}
		}
	}
	policy := model.PoolFallbackPolicy{
		PoolId: input.PoolId, ModelName: input.ModelName,
		MaxFallbackCount: input.MaxFallbackCount, Enabled: input.Enabled,
		Members: make([]model.PoolFallbackMember, 0, len(input.Members)),
	}
	for _, member := range input.Members {
		policy.Members = append(policy.Members, model.PoolFallbackMember{AccountId: member.AccountId, Priority: member.Priority, Enabled: member.Enabled})
	}
	slices.SortStableFunc(policy.Members, func(a, b model.PoolFallbackMember) int {
		return b.Priority - a.Priority
	})
	return (model.PoolFallbackRepository{DB: db()}).Replace(ctx, policy)
}
