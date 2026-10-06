package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/routechain"

	"github.com/gin-gonic/gin"
)

// Stage 5 Operator Control API (design §2.8, requirement §三十–§三十六):
// owner-only runtime circuit control for route chains. Every endpoint runs
// behind RootAuth — a non-owner must never learn channel ids, provider
// topology, failure details or breaker state, and the breaker is explicitly
// NOT an authorization mechanism: these endpoints only observe and steer the
// runtime availability filter.

// GetRouteChainHealth returns the chain's per-candidate runtime health view.
// The health summary is a presentation state derived from config + breaker
// (§三十六) — never an authorization source and never a routing input.
func GetRouteChainHealth(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	chain, err := model.GetRouteChainById(id)
	if err != nil {
		common.ApiErrorMsg(c, "route chain not found")
		return
	}
	candidates := make([]gin.H, 0, len(chain.Candidates))
	for i := range chain.Candidates {
		cand := &chain.Candidates[i]
		policy, policyErr := model.ParseRouteCandidatePolicy(cand.Policy)
		if policyErr != nil {
			policy = nil // resolve-time validation rejects corrupt policies anyway
		}
		snapshot := routechain.Breaker.CandidateSnapshot(chain.Id, cand.Id, policy)
		summary := snapshot.HealthSummary()
		if !cand.Enabled {
			// DISABLED is persistent configuration, not a breaker state (§六).
			summary = "DISABLED"
		}
		candidates = append(candidates, gin.H{
			"id":             cand.Id,
			"type":           cand.CandidateType,
			"target_id":      cand.TargetId,
			"priority":       cand.Priority,
			"enabled":        cand.Enabled,
			"health_summary": summary,
			"circuit":        snapshot,
		})
	}
	common.ApiSuccess(c, gin.H{
		"chain": gin.H{
			"id":                 chain.Id,
			"model":              chain.Model,
			"group":              chain.Group,
			"enabled":            chain.Enabled,
			"version":            chain.Version,
			"allow_cross_source": chain.AllowCrossSource,
		},
		"candidates": candidates,
		"counters":   routechain.Breaker.GlobalCounters(),
		"runtime_note": "circuit state is memory-only per instance: a restart resets it to closed, " +
			"and multi-instance deployments are not cluster-coordinated",
	})
}

// parseChainCandidateParams resolves the :id and :candidateId path params.
func parseChainCandidateParams(c *gin.Context) (chainId int, candidateId int, ok bool) {
	chainId, err := strconv.Atoi(c.Param("id"))
	if err != nil || chainId <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return 0, 0, false
	}
	candidateId, err = strconv.Atoi(c.Param("candidateId"))
	if err != nil || candidateId <= 0 {
		common.ApiErrorMsg(c, "invalid candidate id")
		return 0, 0, false
	}
	return chainId, candidateId, true
}

// requireChainCandidate verifies the candidate belongs to the chain before
// any breaker operation, so ids from unrelated chains cannot be steered.
func requireChainCandidate(c *gin.Context, chainId, candidateId int) bool {
	chain, err := model.GetRouteChainById(chainId)
	if err != nil {
		common.ApiErrorMsg(c, "route chain not found")
		return false
	}
	for i := range chain.Candidates {
		if chain.Candidates[i].Id == candidateId {
			return true
		}
	}
	common.ApiErrorMsg(c, "candidate not found in this chain")
	return false
}

// ResetRouteChainCandidateCircuit clears the runtime breaker state back to
// the probeable CLOSED state (§三十一). It never touches candidate enabled,
// trust, capability, pricing or publication.
func ResetRouteChainCandidateCircuit(c *gin.Context) {
	chainId, candidateId, ok := parseChainCandidateParams(c)
	if !ok {
		return
	}
	if !requireChainCandidate(c, chainId, candidateId) {
		return
	}
	routechain.Breaker.ResetCandidate(c, chainId, candidateId)
	recordManageAudit(c, "route_chain.circuit_reset", map[string]any{
		"chain_id": chainId, "candidate_id": candidateId,
	})
	common.ApiSuccess(c, gin.H{
		"chain_id":     chainId,
		"candidate_id": candidateId,
		"circuit":      routechain.Breaker.CandidateSnapshot(chainId, candidateId, nil),
	})
}

type forceOpenCircuitRequest struct {
	DurationSeconds int `json:"duration_seconds"`
}

// ForceOpenRouteChainCandidateCircuit applies the transient runtime override
// (§三十二). It is memory-only: a restart clears it and its window elapses on
// its own; persistent deactivation is candidate enabled=false, never this.
func ForceOpenRouteChainCandidateCircuit(c *gin.Context) {
	chainId, candidateId, ok := parseChainCandidateParams(c)
	if !ok {
		return
	}
	if !requireChainCandidate(c, chainId, candidateId) {
		return
	}
	var body forceOpenCircuitRequest
	if c.Request != nil && c.Request.Body != nil {
		if err := c.ShouldBindJSON(&body); err != nil && !strings.Contains(err.Error(), "EOF") {
			common.ApiErrorMsg(c, "invalid request body")
			return
		}
	}
	routechain.Breaker.ForceOpen(c, chainId, candidateId, body.DurationSeconds)
	recordManageAudit(c, "route_chain.circuit_force_open", map[string]any{
		"chain_id": chainId, "candidate_id": candidateId, "duration_seconds": body.DurationSeconds,
	})
	common.ApiSuccess(c, gin.H{
		"chain_id":          chainId,
		"candidate_id":      candidateId,
		"circuit":           routechain.Breaker.CandidateSnapshot(chainId, candidateId, nil),
		"restart_semantics": "memory-only: cleared on process restart or when the window elapses; set candidate enabled=false for a persistent disable",
	})
}

type setCandidateEnabledRequest struct {
	Enabled         bool `json:"enabled"`
	ExpectedVersion int  `json:"expected_version"`
}

// SetRouteChainCandidateEnabled toggles one candidate through the repository's
// CAS version (§三十三) — persistent configuration, version-enforced; this is
// deliberately NOT the circuit endpoints' transient semantics.
func SetRouteChainCandidateEnabled(c *gin.Context) {
	chainId, candidateId, ok := parseChainCandidateParams(c)
	if !ok {
		return
	}
	var body setCandidateEnabledRequest
	if c.Request == nil || c.Request.Body == nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := model.SetRouteChainCandidateEnabled(chainId, candidateId, body.Enabled, body.ExpectedVersion); err != nil {
		if errors.Is(err, model.ErrRouteChainVersionConflict) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "route chain version conflict"})
			return
		}
		if errors.Is(err, model.ErrRouteChainNotFound) {
			common.ApiErrorMsg(c, "candidate not found in this chain")
			return
		}
		common.ApiErrorMsg(c, "failed to update candidate")
		return
	}
	recordManageAudit(c, "route_chain.candidate_enable", map[string]any{
		"chain_id": chainId, "candidate_id": candidateId, "enabled": body.Enabled,
	})
	common.ApiSuccess(c, gin.H{
		"chain_id":     chainId,
		"candidate_id": candidateId,
		"enabled":      body.Enabled,
		"version":      body.ExpectedVersion + 1,
	})
}
