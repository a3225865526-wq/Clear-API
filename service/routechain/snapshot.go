// Package routechain implements the Resilient Route Chain engine (design:
// docs/superpowers/specs/2026-10-06-resilient-route-chain-design.md).
//
// The package stays dependency-light on purpose: it may import model, common,
// dto, constant, logger and relaykit/types, but never service or middleware —
// the relay loop (controller) and distributor (middleware) own the
// orchestration, so no import cycles can arise and every failover decision is
// forced through this package's centralized policy.
package routechain

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Candidate is one exact-bound route target inside an immutable snapshot.
type Candidate struct {
	Id       int
	Type     string // model.RouteChainCandidateTypePool | model.RouteChainCandidateTypeAPIChannel
	TargetId int
	Priority int
	Policy   *model.RouteCandidatePolicy
}

// Snapshot is the immutable per-request view of a route chain (Amendment 2
// C3): loaded once when a request begins, never re-read from the database for
// the lifetime of the request. Concurrent admin edits (CAS version bumps)
// affect only requests that begin afterwards.
type Snapshot struct {
	ChainId          int
	Version          int
	Model            string
	Group            string
	AllowCrossSource bool
	Candidates       []Candidate
}

// candidateSource maps a candidate type onto the STEP-4 model-source it may
// serve. API_CHANNEL candidates serve api-source models; POOL candidates
// serve pool-published models.
func candidateSourceMatches(candidateType string, routeSource string) bool {
	if candidateType == model.RouteChainCandidateTypePool {
		return routeSource == "pool"
	}
	return routeSource != "pool"
}

// Resolve loads the immutable route snapshot for (modelName, groupName).
// A nil snapshot means the request keeps legacy routing byte-for-byte.
// Fail-closed mapping (Amendment 2 / design §2.1):
//   - feature gate off, no chain row, chain disabled            ⇒ nil (inert)
//   - corrupt config (bad policy, unknown type, mixed sources
//     without allow_cross_source)                               ⇒ nil + warn
//   - chain enabled with zero enabled candidates                ⇒ ACTIVE
//     snapshot with no candidates ⇒ the iterator exhausts
//     deterministically into route_exhausted (fail closed, never a silent
//     legacy fallback the operator closed on purpose)
//
// Candidates are enabled-only and ordered by priority DESC then id ASC, so
// duplicate priorities from dirty or hand-edited data still resolve
// deterministically (Amendment 2 C6).
func Resolve(c *gin.Context, modelName string, groupName string) *Snapshot {
	if !common.RouteChainEnabled() {
		return nil
	}
	chain, err := model.GetEnabledRouteChainForModel(modelName, groupName)
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf("route_chain subsystem=resolve event=resolve_error model=%q group=%q err=%q", modelName, groupName, err.Error()))
		return nil
	}
	if chain == nil {
		return nil
	}
	snapshot := &Snapshot{
		ChainId:          chain.Id,
		Version:          chain.Version,
		Model:            chain.Model,
		Group:            chain.Group,
		AllowCrossSource: chain.AllowCrossSource,
	}
	seenTypes := map[string]bool{}
	for _, row := range chain.Candidates {
		if !row.Enabled {
			continue
		}
		if row.CandidateType != model.RouteChainCandidateTypePool && row.CandidateType != model.RouteChainCandidateTypeAPIChannel {
			logger.LogWarn(c, fmt.Sprintf("route_chain subsystem=resolve event=invalid_candidate_type chain_id=%d candidate_id=%d type=%q", chain.Id, row.Id, row.CandidateType))
			return nil
		}
		policy, err := model.ParseRouteCandidatePolicy(row.Policy)
		if err != nil {
			logger.LogWarn(c, fmt.Sprintf("route_chain subsystem=resolve event=invalid_policy chain_id=%d candidate_id=%d", chain.Id, row.Id))
			return nil
		}
		seenTypes[row.CandidateType] = true
		snapshot.Candidates = append(snapshot.Candidates, Candidate{
			Id:       row.Id,
			Type:     row.CandidateType,
			TargetId: row.TargetId,
			Priority: row.Priority,
			Policy:   policy,
		})
	}
	if len(seenTypes) > 1 && !snapshot.AllowCrossSource {
		logger.LogWarn(c, fmt.Sprintf("route_chain subsystem=resolve event=mixed_sources_without_flag chain_id=%d chain_version=%d", chain.Id, chain.Version))
		return nil
	}
	// Source isolation re-check (design §2.8, R25): without the cross-source
	// flag, every candidate must match the model's STEP-4 source resolution.
	// An unconfigured model name resolves to api; the per-candidate checks in
	// selectCandidate still enforce channel-side compatibility either way.
	if !snapshot.AllowCrossSource {
		route, _ := model.GetPoolModelRoute(modelName)
		routeSource := "api"
		if route != nil {
			routeSource = route.Source
		}
		for _, candidate := range snapshot.Candidates {
			if !candidateSourceMatches(candidate.Type, routeSource) {
				logger.LogWarn(c, fmt.Sprintf("route_chain subsystem=resolve event=candidate_source_mismatch chain_id=%d candidate_id=%d route_source=%q candidate_type=%q", chain.Id, candidate.Id, routeSource, candidate.Type))
				return nil
			}
		}
	}
	return snapshot
}
