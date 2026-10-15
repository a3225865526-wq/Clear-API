package routechain

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// SelectCandidate binds the candidate at index to its exact target channel
// (Amendment 2 C2). The candidate's target is never re-thrown into the
// weighted selection pool: the returned channel is the only channel this
// attempt may dispatch to.
//
// Every check is pre-dispatch (C5): a failing check excludes the candidate
// through a route decision record (caller: MarkSkipped) — never a fabricated
// upstream attempt.
//
// groups are the request's usable routing groups (resolved by the caller,
// which owns auto-group expansion); filters are the caller's channel filters
// MINUS the model_source filter — in chain mode source compatibility is
// owned by the candidate type check below plus the STEP-4 admission gates
// that run per candidate in SetupContextForSelectedChannel.
func (s *RequestState) SelectCandidate(c *gin.Context, index int, groups []string, filters []dto.ChannelFilter) (*model.Channel, error) {
	if s == nil || s.Snapshot == nil || index < 0 || index >= len(s.Snapshot.Candidates) {
		return nil, fmt.Errorf("unavailable:candidate_out_of_range")
	}
	candidate := s.Snapshot.Candidates[index]

	channel, err := model.CacheGetChannel(candidate.TargetId)
	if err != nil || channel == nil {
		return nil, fmt.Errorf("unavailable:channel_missing")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return nil, fmt.Errorf("unavailable:channel_disabled")
	}
	// Type/channel compatibility, fail closed (R28): a POOL candidate must
	// target a pool-bound channel and an API_CHANNEL candidate a non-pool
	// channel. A mismatch is a configuration error — never auto-corrected.
	poolBound := channel.PoolChannelBound()
	if candidate.Type == model.RouteChainCandidateTypePool && !poolBound {
		return nil, fmt.Errorf("unavailable:pool_candidate_on_api_channel")
	}
	if candidate.Type == model.RouteChainCandidateTypeAPIChannel && poolBound {
		return nil, fmt.Errorf("unavailable:api_candidate_on_pool_channel")
	}
	// The candidate must actually serve the requested model for one of the
	// request's usable groups (canonical-model support, requirement §19).
	served := false
	for _, group := range groups {
		if group != "" && model.IsChannelEnabledForGroupModel(group, s.Snapshot.Model, channel.Id) {
			served = true
			break
		}
	}
	if !served {
		return nil, fmt.Errorf("unavailable:model_not_served_by_channel")
	}
	if len(filters) > 0 {
		if ok, kind := model.ChannelSatisfiesFilters(channel, s.Snapshot.Model, filters); !ok {
			return nil, fmt.Errorf("unavailable:filter_rejected:%s", kind)
		}
	}
	return channel, nil
}
