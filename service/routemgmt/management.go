// Package routemgmt implements the Stage 6 Route Chain Operator Console
// service layer (design: docs/superpowers/specs/2026-10-07-resilient-route-chain-stage6-operator-console-design.md).
//
// It sits between the owner-only HTTP handlers and the model/repository CAS
// layer — handlers never touch the database directly. Everything here is
// configuration and observation: no function dials an upstream, no function
// writes billing state, and nothing may flip channel status, pool master,
// publication, pricing or candidate enabled as a side effect of another
// dimension (state separation, requirement §二). The circuit breaker stays a
// runtime availability filter and never an authorization mechanism (§三).
//
// This package is intentionally separate from service/routechain: the engine
// package stays dependency-light for the request path, while console
// diagnostics may read pricing settings freely.
package routemgmt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// Domain errors the controller maps onto HTTP statuses.
var (
	ErrVersionConflict = errors.New("route chain version conflict")
	ErrNotFound        = errors.New("route chain not found")
	ErrChainExists     = errors.New("route chain already exists for model and group")
	ErrInvalid         = errors.New("route chain configuration invalid")
	ErrChainHasHistory = errors.New("route chain has attempt history and cannot be deleted")
)

// RouteIssue is one validation finding. Codes are stable strings the console
// maps to localized operator copy (§三十七); ids locate the offending
// candidate. Issues are never raw Go/SQL error text.
type RouteIssue struct {
	Code        string `json:"code"`
	CandidateId int    `json:"candidate_id,omitempty"`
	TargetId    int    `json:"target_id,omitempty"`
}

// Blocking validation codes (HTTP 400) and advisory warning codes (surfaced,
// never block) — stage 6 §2.3.
const (
	IssueModelRequired                  = "route_chain.model_required"
	IssueGroupRequired                  = "route_chain.group_required"
	IssueCandidateEmpty                 = "route_chain.candidate_empty"
	IssueCandidateOverflow              = "route_chain.candidate_overflow"
	IssueCandidateTypeInvalid           = "route_chain.candidate_type_invalid"
	IssueTargetInvalid                  = "route_chain.target_invalid"
	IssueTargetMissing                  = "route_chain.target_missing"
	IssueTypeTargetMismatch             = "route_chain.type_target_mismatch"
	IssueDuplicateTarget                = "route_chain.duplicate_target"
	IssueDuplicatePriority              = "route_chain.duplicate_priority"
	IssuePolicyInvalid                  = "route_chain.policy_invalid"
	IssueMixedSourcesWithoutCrossSource = "route_chain.mixed_sources_without_cross_source"
	IssueModelGroupImmutable            = "route_chain.model_group_immutable"
	IssueForeignCandidate               = "route_chain.foreign_candidate"
	IssueChainArchived                  = "route_chain.chain_archived"
	IssuePricingMissing                 = "route_chain.pricing_missing"
	IssueModelNotServed                 = "route_chain.model_not_served"
	IssueSourceMismatch                 = "route_chain.source_mismatch"
	IssueEngineAttemptCap               = "route_chain.engine_attempt_cap"
)

// GateStates mirrors the runtime feature gates (§二十四): the console must
// make "configuration exists" visibly different from "runtime active".
type GateStates struct {
	RouteChainEnabled     bool `json:"route_chain_enabled"`
	CircuitBreakerEnabled bool `json:"circuit_breaker_enabled"`
	MaxCandidates         int  `json:"max_candidates"`
	OverallDeadlineMs     int  `json:"overall_deadline_ms"`
}

func CurrentGateStates() GateStates {
	return GateStates{
		RouteChainEnabled:     common.RouteChainEnabled(),
		CircuitBreakerEnabled: common.RouteCircuitBreakerEnabled(),
		MaxCandidates:         common.RouteChainMaxCandidates,
		OverallDeadlineMs:     common.RouteChainOverallDeadlineMs,
	}
}

// TargetInfo is the secret-free view of a candidate target channel (§2.1.6).
type TargetInfo struct {
	Id        int      `json:"id"`
	Name      string   `json:"name"`
	Type      int      `json:"type"`
	TypeName  string   `json:"type_name"`
	Status    int      `json:"status"`
	Enabled   bool     `json:"enabled"`
	Group     string   `json:"group"`
	Models    []string `json:"models"`
	PoolBound bool     `json:"pool_bound"`
	Source    string   `json:"source"`
}

func targetInfo(summary model.RouteChainTargetSummary) TargetInfo {
	models := []string{}
	for _, name := range strings.Split(summary.Models, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			models = append(models, trimmed)
		}
	}
	poolBound := poolBoundFor(summary.Id)
	source := "api"
	if poolBound {
		source = "pool"
	}
	return TargetInfo{
		Id:        summary.Id,
		Name:      summary.Name,
		Type:      summary.Type,
		TypeName:  constant.GetChannelTypeName(summary.Type),
		Status:    summary.Status,
		Enabled:   summary.Status == common.ChannelStatusEnabled,
		Group:     summary.Group,
		Models:    models,
		PoolBound: poolBound,
		Source:    source,
	}
}

// poolBoundFor resolves pool-boundedness for one channel. The full channel
// row is needed for PoolChannelBound (settings fields); it is loaded with the
// key omitted and never leaves this function — only the boolean derives from it.
func poolBoundFor(channelId int) bool {
	channel, err := model.GetChannelById(channelId, false)
	if err != nil || channel == nil {
		return false
	}
	return channel.PoolChannelBound()
}

// TargetCatalog lists every channel as a candidate target (§七): the picker
// source of truth — operators never type raw target ids, and pool/API
// filtering is backed by authoritative validation at save time (§八).
func TargetCatalog() ([]TargetInfo, error) {
	summaries, err := model.ListRouteChainTargetSummaries()
	if err != nil {
		return nil, err
	}
	result := make([]TargetInfo, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, targetInfo(summary))
	}
	return result, nil
}

// CandidateSpec is the allowlisted console input for one candidate (§三十二):
// created_at, health counters, billing state, circuit counters and friends
// are not submittable — anything absent from this struct is rejected by the
// JSON decoder's unknown-field-agnostic strictness at the controller plus
// the model layer's fixed column update sets.
type CandidateSpec struct {
	Id            int    `json:"id"`
	CandidateType string `json:"candidate_type"`
	TargetId      int    `json:"target_id"`
	Priority      int    `json:"priority"`
	Enabled       bool   `json:"enabled"`
	Policy        string `json:"policy"`
}

// ChainSpec is the allowlisted create/update payload.
type ChainSpec struct {
	Model            string          `json:"model"`
	Group            string          `json:"group"`
	Enabled          bool            `json:"enabled"`
	AllowCrossSource bool            `json:"allow_cross_source"`
	Candidates       []CandidateSpec `json:"candidates"`
}

func (s *ChainSpec) toRepo() *model.RouteChainWithCandidates {
	chain := &model.RouteChainWithCandidates{
		RouteChain: model.RouteChain{
			Model:            strings.TrimSpace(s.Model),
			Group:            strings.TrimSpace(s.Group),
			Enabled:          s.Enabled,
			AllowCrossSource: s.AllowCrossSource,
		},
	}
	for i := range s.Candidates {
		chain.Candidates = append(chain.Candidates, model.RouteChainCandidate{
			Id:            s.Candidates[i].Id,
			CandidateType: s.Candidates[i].CandidateType,
			TargetId:      s.Candidates[i].TargetId,
			Priority:      s.Candidates[i].Priority,
			Enabled:       s.Candidates[i].Enabled,
			Policy:        s.Candidates[i].Policy,
		})
	}
	return chain
}

// Validate checks a chain spec without persisting anything (§四十四: dry
// validation is part of save; no separate upstream-dialing endpoint). Returns
// blocking errors and non-blocking warnings; empty blocked means the spec may
// be saved, warnings travel with the save response either way.
func Validate(spec *ChainSpec) (blocked []RouteIssue, warnings []RouteIssue) {
	if strings.TrimSpace(spec.Model) == "" {
		blocked = append(blocked, RouteIssue{Code: IssueModelRequired})
	}
	if strings.TrimSpace(spec.Group) == "" {
		blocked = append(blocked, RouteIssue{Code: IssueGroupRequired})
	}
	if len(spec.Candidates) == 0 {
		blocked = append(blocked, RouteIssue{Code: IssueCandidateEmpty})
	}
	if len(spec.Candidates) > model.RouteChainMaxCandidateRows {
		blocked = append(blocked, RouteIssue{Code: IssueCandidateOverflow})
		return blocked, warnings
	}
	seenTarget := make(map[string]bool, len(spec.Candidates))
	seenPriority := make(map[int]bool, len(spec.Candidates))
	seenType := make(map[string]bool, len(spec.Candidates))
	for i := range spec.Candidates {
		candidate := &spec.Candidates[i]
		at := func(code string) RouteIssue {
			return RouteIssue{Code: code, CandidateId: candidate.Id, TargetId: candidate.TargetId}
		}
		if candidate.CandidateType != model.RouteChainCandidateTypePool && candidate.CandidateType != model.RouteChainCandidateTypeAPIChannel {
			blocked = append(blocked, at(IssueCandidateTypeInvalid))
			continue
		}
		if candidate.TargetId <= 0 {
			blocked = append(blocked, at(IssueTargetInvalid))
			continue
		}
		if _, err := model.ParseRouteCandidatePolicy(candidate.Policy); err != nil {
			blocked = append(blocked, at(IssuePolicyInvalid))
			continue
		}
		targetKey := candidate.CandidateType + ":" + fmt.Sprint(candidate.TargetId)
		if seenTarget[targetKey] {
			blocked = append(blocked, at(IssueDuplicateTarget))
			continue
		}
		seenTarget[targetKey] = true
		if seenPriority[candidate.Priority] {
			blocked = append(blocked, at(IssueDuplicatePriority))
			continue
		}
		seenPriority[candidate.Priority] = true
		seenType[candidate.CandidateType] = true

		// Authoritative target checks — the frontend type filter is UX only
		// (§八); the backend re-derives pool-boundedness from the channel row.
		channel, err := model.GetChannelById(candidate.TargetId, false)
		if err != nil || channel == nil {
			blocked = append(blocked, at(IssueTargetMissing))
			continue
		}
		poolBound := channel.PoolChannelBound()
		if (candidate.CandidateType == model.RouteChainCandidateTypePool) != poolBound {
			blocked = append(blocked, at(IssueTypeTargetMismatch))
			continue
		}
		if candidate.Enabled && strings.TrimSpace(spec.Model) != "" &&
			!model.IsChannelEnabledForGroupModel(strings.TrimSpace(spec.Group), strings.TrimSpace(spec.Model), channel.Id) {
			warnings = append(warnings, at(IssueModelNotServed))
		}
	}
	if blocked != nil {
		return blocked, warnings
	}
	// Deterministic fail-closed config: the engine inerts a mixed-source
	// chain without the cross-source flag (snapshot.go) — refuse to save it
	// instead (§十一), while mixed sources WITH the flag stay operator intent.
	if len(seenType) > 1 && !spec.AllowCrossSource {
		blocked = append(blocked, RouteIssue{Code: IssueMixedSourcesWithoutCrossSource})
	}
	// Model-level diagnostics that need the stored state (warnings only).
	if modelName := strings.TrimSpace(spec.Model); modelName != "" {
		if !pricingConfigured(modelName) {
			warnings = append(warnings, RouteIssue{Code: IssuePricingMissing})
		}
		route, routeErr := model.GetPoolModelRoute(modelName)
		for i := range spec.Candidates {
			candidate := &spec.Candidates[i]
			at := RouteIssue{Code: IssueSourceMismatch, CandidateId: candidate.Id, TargetId: candidate.TargetId}
			isPool := candidate.CandidateType == model.RouteChainCandidateTypePool
			switch {
			case routeErr != nil:
				// Corrupt route state fails closed at runtime — warn.
				warnings = append(warnings, at)
			case route == nil:
				// No publication record: non-pool candidates match the
				// default api source, pool candidates would always be
				// rejected (ErrPoolModelNotPublished).
				if isPool {
					warnings = append(warnings, at)
				}
			case !route.Enabled:
				warnings = append(warnings, at)
			case isPool != (route.Source == "pool"):
				warnings = append(warnings, at)
			}
		}
	}
	// The engine's per-request attempt cap (default 3) is separate from the
	// config row cap (10): candidates beyond the cap can never be reached.
	enabledCount := 0
	for i := range spec.Candidates {
		if spec.Candidates[i].Enabled {
			enabledCount++
		}
	}
	if enabledCount > common.RouteChainMaxCandidates {
		warnings = append(warnings, RouteIssue{Code: IssueEngineAttemptCap})
	}
	return blocked, warnings
}

// pricingConfigured reports whether user-billing pricing exists for the
// model (per-call price or ratio). User billing pricing and provider cost
// rules are different domains (§十) — this diagnostic is only about the
// user-facing side, displayed as PRICING MISSING, never as ￥0.
func pricingConfigured(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	_, ok, _ := ratio_setting.GetModelRatio(modelName)
	return ok
}

// CreateChain validates then persists a new chain. Duplicate (model, group)
// is rejected (U4). The response carries save-time warnings so the editor can
// surface PRICING MISSING etc. immediately (§十, §九).
func CreateChain(spec *ChainSpec) (blocked []RouteIssue, warnings []RouteIssue, chainId int, err error) {
	blocked, warnings = Validate(spec)
	if len(blocked) > 0 {
		return blocked, warnings, 0, ErrInvalid
	}
	repo := spec.toRepo()
	if err := model.UpsertRouteChain(repo, 0); err != nil {
		return blocked, warnings, 0, mapRepoError(err)
	}
	return blocked, warnings, repo.Id, nil
}

// UpdateChain validates then persists a full chain update under CAS
// (expectedVersion). Model and group are immutable after creation and
// archived chains are read-only — both reported as blocking issues; the
// model layer re-enforces the archived rule inside its own CAS transaction.
func UpdateChain(id int, spec *ChainSpec, expectedVersion int) (blocked []RouteIssue, warnings []RouteIssue, err error) {
	blocked, warnings = Validate(spec)
	if len(blocked) > 0 {
		return blocked, warnings, ErrInvalid
	}
	stored, err := model.GetRouteChainById(id)
	if err != nil {
		return blocked, warnings, mapRepoError(err)
	}
	if strings.TrimSpace(spec.Model) != stored.Model || strings.TrimSpace(spec.Group) != stored.Group {
		return append(blocked, RouteIssue{Code: IssueModelGroupImmutable}), warnings, ErrInvalid
	}
	// IDOR guard (§三十一): a payload row referencing a candidate id that does
	// not belong to this chain is rejected — it must never silently become a
	// new row nor touch another chain's candidate.
	existingIds := make(map[int]bool, len(stored.Candidates))
	for i := range stored.Candidates {
		existingIds[stored.Candidates[i].Id] = true
	}
	for i := range spec.Candidates {
		if id := spec.Candidates[i].Id; id > 0 && !existingIds[id] {
			return append(blocked, RouteIssue{Code: IssueForeignCandidate, CandidateId: id}), warnings, ErrInvalid
		}
	}
	repo := spec.toRepo()
	repo.Id = id
	if err := model.UpsertRouteChain(repo, expectedVersion); err != nil {
		return blocked, warnings, mapRepoError(err)
	}
	return blocked, warnings, nil
}

// SetChainEnabled flips the whole chain's persistent enabled flag under CAS.
func SetChainEnabled(id int, enabled bool, expectedVersion int) error {
	return mapRepoError(model.SetRouteChainEnabled(id, enabled, expectedVersion))
}

// ReorderCandidates persists a new candidate order under CAS (§十八): the
// payload order becomes descending priority; the server order is canonical
// afterwards.
func ReorderCandidates(id int, orderedCandidateIds []int, expectedVersion int) error {
	return mapRepoError(model.ReorderRouteChainCandidates(id, orderedCandidateIds, expectedVersion))
}

// ArchiveChain retires a chain (persistent disable + archived marker, CAS).
// Historical ledger rows keep a config row to point at (§十七).
func ArchiveChain(id int, expectedVersion int) error {
	return mapRepoError(model.ArchiveRouteChain(id, expectedVersion))
}

// DeleteChain hard-deletes a chain — only when no ledger row references it
// (§十七). History-bearing chains must be archived or disabled instead.
func DeleteChain(id int, expectedVersion int) error {
	hasHistory, err := model.ChainHasLedgerHistory(id)
	if err != nil {
		return err
	}
	if hasHistory {
		return ErrChainHasHistory
	}
	return mapRepoError(model.DeleteRouteChain(id, expectedVersion))
}

func mapRepoError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrRouteChainVersionConflict):
		return ErrVersionConflict
	case errors.Is(err, model.ErrRouteChainNotFound):
		return ErrNotFound
	case errors.Is(err, model.ErrRouteChainExists):
		return ErrChainExists
	case errors.Is(err, model.ErrRouteChainInvalid), errors.Is(err, model.ErrRouteChainCandidateSet):
		return ErrInvalid
	default:
		return err
	}
}
