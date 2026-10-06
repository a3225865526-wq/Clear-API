package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/routemgmt"

	"github.com/gin-gonic/gin"
)

// Stage 6 Route Chain Operator Console API (design:
// docs/superpowers/specs/2026-10-07-resilient-route-chain-stage6-operator-console-design.md).
// Owner-only management surface for route chains: config CRUD with CAS,
// archive/delete semantics, diagnostics and recent activity. Handlers are
// thin: parse → routemgmt service → model/repository — never direct DB access.
// Config CRUD performs no upstream request, and no response ever contains
// channel keys, base URLs or credential material.

// routeChainManagementError maps service errors onto the HTTP contract:
// 409 for stale CAS versions and history-guarded deletes, 400 with issue
// codes for validation, 200-envelope business errors for the rest (§三十七).
// Returns true when the error was handled.
func routeChainManagementError(c *gin.Context, err error, issues []routemgmt.RouteIssue) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, routemgmt.ErrVersionConflict):
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "ROUTE_CHAIN_VERSION_CONFLICT", "message": "route chain version conflict"})
	case errors.Is(err, routemgmt.ErrChainHasHistory):
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "ROUTE_CHAIN_HAS_HISTORY", "message": "route chain has attempt history; archive or disable it instead of deleting"})
	case errors.Is(err, routemgmt.ErrChainExists):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "ROUTE_CHAIN_EXISTS", "message": "route chain already exists for model and group"})
	case errors.Is(err, routemgmt.ErrInvalid):
		if issues == nil {
			issues = []routemgmt.RouteIssue{}
		}
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "ROUTE_CHAIN_INVALID", "message": "route chain configuration invalid", "errors": issues})
	case errors.Is(err, routemgmt.ErrNotFound):
		common.ApiErrorMsg(c, "route chain not found")
	default:
		common.ApiErrorMsg(c, "failed to update route chain configuration")
	}
	return true
}

// GetRouteChainGates reports the runtime feature gates (§二十四): the console
// must show "configuration exists" and "runtime active" as different facts.
func GetRouteChainGates(c *gin.Context) {
	common.ApiSuccess(c, routemgmt.CurrentGateStates())
}

// GetRouteChainTargets lists the candidate target catalog: secret-free
// channel summaries for the editor picker (§七).
func GetRouteChainTargets(c *gin.Context) {
	targets, err := routemgmt.TargetCatalog()
	if err != nil {
		common.ApiErrorMsg(c, "failed to load channel targets")
		return
	}
	common.ApiSuccess(c, targets)
}

// ListRouteChains returns all chains with presentation summaries
// (include_archived=true adds retired chains).
func ListRouteChains(c *gin.Context) {
	includeArchived := c.Query("include_archived") == "true"
	chains, gates, err := routemgmt.ChainSummaryList(includeArchived)
	if err != nil {
		common.ApiErrorMsg(c, "failed to load route chains")
		return
	}
	common.ApiSuccess(c, gin.H{"items": chains, "gates": gates})
}

// CreateRouteChain validates and persists a new chain (§六): at least one
// candidate, no silent empty chains, no draft state — the data model has none.
func CreateRouteChain(c *gin.Context) {
	var spec routemgmt.ChainSpec
	if err := c.ShouldBindJSON(&spec); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	blocked, warnings, chainId, err := routemgmt.CreateChain(&spec)
	if routeChainManagementError(c, err, blocked) {
		return
	}
	recordManageAudit(c, "route_chain.create", map[string]any{
		"chain_id": chainId, "model": spec.Model, "group": spec.Group,
		"candidate_count": len(spec.Candidates),
	})
	detail, err := routemgmt.GetChainDetail(chainId)
	if err != nil {
		routeChainManagementError(c, err, nil)
		return
	}
	common.ApiSuccess(c, gin.H{"chain": detail, "warnings": presentWarnings(warnings)})
}

// GetRouteChainDetail returns one chain with the five-dimension diagnostics.
func GetRouteChainDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	detail, err := routemgmt.GetChainDetail(id)
	if routeChainManagementError(c, err, nil) {
		return
	}
	common.ApiSuccess(c, gin.H{"chain": detail})
}

// UpdateRouteChain persists a full chain update under CAS (§五): the client
// echoes expected_version; a stale one is a 409, never a silent overwrite.
func UpdateRouteChain(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	var body struct {
		routemgmt.ChainSpec
		ExpectedVersion int `json:"expected_version"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	spec := body.ChainSpec
	blocked, warnings, err := routemgmt.UpdateChain(id, &spec, body.ExpectedVersion)
	if routeChainManagementError(c, err, blocked) {
		return
	}
	recordManageAudit(c, "route_chain.update", map[string]any{
		"chain_id": id, "old_version": body.ExpectedVersion, "new_version": body.ExpectedVersion + 1,
	})
	detail, err := routemgmt.GetChainDetail(id)
	if routeChainManagementError(c, err, nil) {
		return
	}
	common.ApiSuccess(c, gin.H{"chain": detail, "version": body.ExpectedVersion + 1, "warnings": presentWarnings(warnings)})
}

type setRouteChainStateRequest struct {
	Enabled         bool `json:"enabled"`
	ExpectedVersion int  `json:"expected_version"`
}

// SetRouteChainState flips the chain's persistent enabled flag under CAS.
func SetRouteChainState(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	var body setRouteChainStateRequest
	if c.Request == nil || c.Request.Body == nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := routemgmt.SetChainEnabled(id, body.Enabled, body.ExpectedVersion); routeChainManagementError(c, err, nil) {
		return
	}
	recordManageAudit(c, "route_chain.enable", map[string]any{
		"chain_id": id, "enabled": body.Enabled, "old_version": body.ExpectedVersion, "new_version": body.ExpectedVersion + 1,
	})
	common.ApiSuccess(c, gin.H{"chain_id": id, "enabled": body.Enabled, "version": body.ExpectedVersion + 1})
}

type reorderRouteChainRequest struct {
	OrderedCandidateIds []int `json:"ordered_candidate_ids"`
	ExpectedVersion     int   `json:"expected_version"`
}

// ReorderRouteChain persists the candidate priority order under CAS (§十八).
func ReorderRouteChain(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	var body reorderRouteChainRequest
	if c.Request == nil || c.Request.Body == nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := routemgmt.ReorderCandidates(id, body.OrderedCandidateIds, body.ExpectedVersion); routeChainManagementError(c, err, nil) {
		return
	}
	recordManageAudit(c, "route_chain.reorder", map[string]any{
		"chain_id": id, "order": body.OrderedCandidateIds, "old_version": body.ExpectedVersion, "new_version": body.ExpectedVersion + 1,
	})
	common.ApiSuccess(c, gin.H{"chain_id": id, "version": body.ExpectedVersion + 1})
}

type archiveRouteChainRequest struct {
	ExpectedVersion int `json:"expected_version"`
}

// ArchiveRouteChain retires a chain: persistent disable + archived marker.
// The row stays for ledger interpretability (§十七, §三十五).
func ArchiveRouteChain(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	var body archiveRouteChainRequest
	if c.Request == nil || c.Request.Body == nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := routemgmt.ArchiveChain(id, body.ExpectedVersion); routeChainManagementError(c, err, nil) {
		return
	}
	recordManageAudit(c, "route_chain.archive", map[string]any{
		"chain_id": id, "old_version": body.ExpectedVersion, "new_version": body.ExpectedVersion + 1,
	})
	common.ApiSuccess(c, gin.H{"chain_id": id, "archived": true, "version": body.ExpectedVersion + 1})
}

// DeleteRouteChain hard-deletes a chain — refused with 409 when the
// attempt ledger references it (§十七).
func DeleteRouteChain(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	var body archiveRouteChainRequest
	if c.Request != nil && c.Request.Body != nil {
		if err := c.ShouldBindJSON(&body); err != nil {
			common.ApiErrorMsg(c, "invalid request body")
			return
		}
	}
	if err := routemgmt.DeleteChain(id, body.ExpectedVersion); routeChainManagementError(c, err, nil) {
		return
	}
	recordManageAudit(c, "route_chain.delete", map[string]any{"chain_id": id})
	common.ApiSuccess(c, gin.H{"chain_id": id, "deleted": true})
}

// GetRouteChainAttempts returns the recent attempt ledger page (§二十六).
func GetRouteChainAttempts(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	limit, candidateId := activityParams(c)
	views, err := routemgmt.RecentAttempts(id, limit, candidateId)
	if err != nil {
		common.ApiErrorMsg(c, "failed to load route chain attempts")
		return
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

// GetRouteChainDecisions returns the recent decision record page (§二十五).
func GetRouteChainDecisions(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid route chain id")
		return
	}
	limit, candidateId := activityParams(c)
	views, err := routemgmt.RecentDecisions(id, limit, candidateId)
	if err != nil {
		common.ApiErrorMsg(c, "failed to load route chain decisions")
		return
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

func activityParams(c *gin.Context) (limit int, candidateId int) {
	limit = 20
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = routemgmt.ClampLimit(parsed, 20)
		}
	}
	if raw := c.Query("candidate_id"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			candidateId = parsed
		}
	}
	return limit, candidateId
}

func presentWarnings(warnings []routemgmt.RouteIssue) []routemgmt.RouteIssue {
	if warnings == nil {
		return []routemgmt.RouteIssue{}
	}
	return warnings
}
