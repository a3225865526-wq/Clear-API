package routemgmt

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/routechain"
)

// Read-side diagnostics for the operator console (design §2.3): every
// candidate's displayed state is composed from five independent dimensions —
// persistent config, target channel state, route eligibility, pool authority
// and runtime health (§二). Colors never replace the text labels; the labels
// themselves are derived by the frontend from these typed fields.

// EffectiveBreakerView shows the per-candidate breaker configuration with an
// explicit Default/Override provenance per field (§十四) — the backend must
// never apply an override the console cannot see. Half-open probes and
// Retry-After max have no per-candidate override surface (stage 2 policy
// schema), so they always report Default.
type EffectiveBreakerView struct {
	FailureThreshold         int  `json:"failure_threshold"`
	FailureThresholdOverride bool `json:"failure_threshold_override"`
	CooldownSeconds          int  `json:"cooldown_seconds"`
	CooldownOverride         bool `json:"cooldown_override"`
	HalfOpenMaxProbes        int  `json:"half_open_max_probes"`
	HalfOpenProbesOverride   bool `json:"half_open_probes_override"`
	RetryAfterMax            int  `json:"retry_after_max"`
	RetryAfterOverride       bool `json:"retry_after_override"`
}

// PoolAuthorityView carries the pool-specific authority states of a POOL
// candidate (§二十二). No account tokens, credential paths or OAuth material —
// and no eligible-account count (no safe existing API: reported Unknown by the
// frontend, never inferred client-side).
type PoolAuthorityView struct {
	MasterEnabled  bool   `json:"master_enabled"`
	ChannelRouting bool   `json:"channel_routing"`
	Publication    string `json:"publication"`
}

// Publication state values for PoolAuthorityView.
const (
	PublicationPublished   = "published"
	PublicationUnpublished = "unpublished"
	PublicationPaused      = "paused"
	PublicationAPISource   = "api_source"
	PublicationUnknown     = "unknown"
)

// ChainCandidateView is one candidate's full diagnostic view. Channel is nil
// when the target channel row is gone (display falls back to the raw target
// id — the ledger's own columns keep attempts interpretable).
type ChainCandidateView struct {
	Id                int                        `json:"id"`
	ChainId           int                        `json:"chain_id"`
	CandidateType     string                     `json:"candidate_type"`
	TargetId          int                        `json:"target_id"`
	Archived          bool                       `json:"archived"`
	ArchivedTargetId  int                        `json:"archived_target_id,omitempty"`
	Priority          int                        `json:"priority"`
	Enabled           bool                       `json:"enabled"`
	Policy            string                     `json:"policy"`
	Channel           *TargetInfo                `json:"channel"`
	ModelServed       bool                       `json:"model_served"`
	PricingConfigured bool                       `json:"pricing_configured"`
	SourceMatch       bool                       `json:"source_match"`
	PoolAuthority     *PoolAuthorityView         `json:"pool_authority,omitempty"`
	Runtime           routechain.CircuitSnapshot `json:"runtime"`
	HealthSummary     string                     `json:"health_summary"`
	EffectiveBreaker  EffectiveBreakerView       `json:"effective_breaker"`
}

// ChainDetail is the GET /:id payload: configuration, both candidate groups
// (active + archived) and the gate states.
type ChainDetail struct {
	Id                 int                  `json:"id"`
	Model              string               `json:"model"`
	Group              string               `json:"group"`
	Enabled            bool                 `json:"enabled"`
	AllowCrossSource   bool                 `json:"allow_cross_source"`
	Archived           bool                 `json:"archived"`
	Version            int                  `json:"version"`
	CreatedTime        int64                `json:"created_time"`
	UpdatedTime        int64                `json:"updated_time"`
	Gates              GateStates           `json:"gates"`
	Candidates         []ChainCandidateView `json:"candidates"`
	ArchivedCandidates []ChainCandidateView `json:"archived_candidates"`
}

// ChainListItem is the GET / list row (§十九): presentation summary only —
// OverallHealth is a display value, never a routing input.
type ChainListItem struct {
	Id               int    `json:"id"`
	Model            string `json:"model"`
	Group            string `json:"group"`
	Enabled          bool   `json:"enabled"`
	Archived         bool   `json:"archived"`
	AllowCrossSource bool   `json:"allow_cross_source"`
	CandidateCount   int    `json:"candidate_count"`
	PrimaryCandidate *struct {
		CandidateId int    `json:"candidate_id"`
		Type        string `json:"type"`
		TargetId    int    `json:"target_id"`
	} `json:"primary_candidate"`
	FallbackCount int    `json:"fallback_count"`
	OverallHealth string `json:"overall_health"`
	Version       int    `json:"version"`
	UpdatedTime   int64  `json:"updated_time"`
}

// Overall health presentation values (§二十一): text labels, not just colors.
const (
	HealthArchived      = "ARCHIVED"
	HealthChainDisabled = "CHAIN_DISABLED"
	HealthCircuitOpen   = "CIRCUIT_OPEN"
	HealthHalfOpen      = "HALF_OPEN"
	HealthDegraded      = "DEGRADED"
	HealthNoCandidates  = "NO_CANDIDATES"
	HealthHealthy       = "HEALTHY"
	HealthUnknown       = "UNKNOWN"
)

// ChainSummaryList returns all chains with their presentation summaries.
// Archived chains are only included when includeArchived is set.
func ChainSummaryList(includeArchived bool) ([]ChainListItem, GateStates, error) {
	chains, err := model.GetRouteChains()
	if err != nil {
		return nil, GateStates{}, err
	}
	gates := CurrentGateStates()
	result := make([]ChainListItem, 0, len(chains))
	for i := range chains {
		chain := &chains[i]
		if chain.Archived && !includeArchived {
			continue
		}
		item := ChainListItem{
			Id:               chain.Id,
			Model:            chain.Model,
			Group:            chain.Group,
			Enabled:          chain.Enabled,
			Archived:         chain.Archived,
			AllowCrossSource: chain.AllowCrossSource,
			Version:          chain.Version,
			UpdatedTime:      chain.UpdatedTime,
		}
		item.CandidateCount = len(chain.Candidates)
		item.OverallHealth = overallHealth(chain, gates)
		for j := range chain.Candidates {
			candidate := &chain.Candidates[j]
			if candidate.Archived || !candidate.Enabled {
				continue
			}
			if item.PrimaryCandidate == nil {
				item.PrimaryCandidate = &struct {
					CandidateId int    `json:"candidate_id"`
					Type        string `json:"type"`
					TargetId    int    `json:"target_id"`
				}{CandidateId: candidate.Id, Type: candidate.CandidateType, TargetId: candidate.TargetId}
			} else {
				item.FallbackCount++
			}
		}
		result = append(result, item)
	}
	return result, gates, nil
}

// overallHealth derives the list-row presentation summary. Worst observed
// runtime state wins; persistent states only surface when no runtime signal
// exists. Ordering: archived > disabled > circuit_open > half_open >
// degraded > no_candidates > unknown > healthy.
func overallHealth(chain *model.RouteChainWithCandidates, gates GateStates) string {
	if chain.Archived {
		return HealthArchived
	}
	if !chain.Enabled {
		return HealthChainDisabled
	}
	worst := HealthHealthy
	observed := false
	enabledCandidates := 0
	for i := range chain.Candidates {
		candidate := &chain.Candidates[i]
		if candidate.Archived {
			continue
		}
		if candidate.Enabled {
			enabledCandidates++
		}
		policy, err := model.ParseRouteCandidatePolicy(candidate.Policy)
		if err != nil {
			policy = nil
		}
		snapshot := routechain.Breaker.CandidateSnapshot(chain.Id, candidate.Id, policy)
		observed = observed || snapshot.LastSuccessAt > 0 || snapshot.LastFailureAt > 0
		switch snapshot.State {
		case routechain.CircuitStateOpen:
			return HealthCircuitOpen
		case routechain.CircuitStateHalfOpen:
			worst = healthRank(worst, HealthHalfOpen)
		default:
			if snapshot.ConsecutiveFailures > 0 {
				worst = healthRank(worst, HealthDegraded)
			}
		}
	}
	if enabledCandidates == 0 {
		return HealthNoCandidates
	}
	if worst == HealthHealthy && !observed {
		return HealthUnknown
	}
	return worst
}

func healthRank(current string, candidate string) string {
	order := map[string]int{
		HealthHealthy: 0, HealthUnknown: 1, HealthDegraded: 2,
		HealthHalfOpen: 3, HealthCircuitOpen: 4,
	}
	if order[candidate] > order[current] {
		return candidate
	}
	return current
}

// ChainDetail loads one chain and enriches every candidate with the
// five-dimension diagnostic view. Diagnostics are read-only: no capability is
// granted, no model published, no pricing created, no channel modified (§九).
func GetChainDetail(id int) (*ChainDetail, error) {
	chain, err := model.GetRouteChainById(id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	detail := &ChainDetail{
		Id:                 chain.Id,
		Model:              chain.Model,
		Group:              chain.Group,
		Enabled:            chain.Enabled,
		AllowCrossSource:   chain.AllowCrossSource,
		Archived:           chain.Archived,
		Version:            chain.Version,
		CreatedTime:        chain.CreatedTime,
		UpdatedTime:        chain.UpdatedTime,
		Gates:              CurrentGateStates(),
		Candidates:         []ChainCandidateView{},
		ArchivedCandidates: []ChainCandidateView{},
	}
	targetIds := make([]int, 0, len(chain.Candidates))
	for i := range chain.Candidates {
		targetId := chain.Candidates[i].TargetId
		if chain.Candidates[i].Archived {
			targetId = chain.Candidates[i].ArchivedTargetId
		}
		if targetId > 0 {
			targetIds = append(targetIds, targetId)
		}
	}
	channels, err := model.GetRouteChainChannelSummaries(targetIds)
	if err != nil {
		return nil, err
	}
	for i := range chain.Candidates {
		candidate := &chain.Candidates[i]
		view := candidateView(chain, candidate, channels)
		if candidate.Archived {
			detail.ArchivedCandidates = append(detail.ArchivedCandidates, view)
		} else {
			detail.Candidates = append(detail.Candidates, view)
		}
	}
	return detail, nil
}

func candidateView(chain *model.RouteChainWithCandidates, candidate *model.RouteChainCandidate, channels map[int]model.RouteChainTargetSummary) ChainCandidateView {
	targetId := candidate.TargetId
	if candidate.Archived {
		targetId = candidate.ArchivedTargetId
	}
	view := ChainCandidateView{
		Id:               candidate.Id,
		ChainId:          chain.Id,
		CandidateType:    candidate.CandidateType,
		TargetId:         candidate.TargetId,
		Archived:         candidate.Archived,
		ArchivedTargetId: candidate.ArchivedTargetId,
		Priority:         candidate.Priority,
		Enabled:          candidate.Enabled,
		Policy:           candidate.Policy,
	}
	if summary, ok := channels[targetId]; ok {
		info := targetInfo(summary)
		view.Channel = &info
	}
	policy, policyErr := model.ParseRouteCandidatePolicy(candidate.Policy)
	if policyErr != nil {
		policy = nil // resolve-time validation rejects corrupt policies anyway
	}
	view.EffectiveBreaker = effectiveBreakerView(policy)
	if chain.Archived || candidate.Archived {
		// Archived rows are audit records: identity and original target
		// matter, runtime diagnostics do not (§三十五).
		view.HealthSummary = HealthArchived
		return view
	}
	view.Runtime = routechain.Breaker.CandidateSnapshot(chain.Id, candidate.Id, policy)
	view.HealthSummary = view.Runtime.HealthSummary()
	if !candidate.Enabled {
		// DISABLED is persistent configuration, not a breaker state (§六).
		view.HealthSummary = "DISABLED"
	}
	// Route eligibility (dimension C).
	if view.Channel != nil {
		view.ModelServed = model.IsChannelEnabledForGroupModel(chain.Group, chain.Model, view.Channel.Id)
	}
	view.PricingConfigured = pricingConfigured(chain.Model)
	routeSource := ""
	var routeErr error
	if route, err := model.GetPoolModelRoute(chain.Model); err != nil {
		routeErr = err
	} else if route != nil {
		routeSource = route.Source
		if !route.Enabled {
			routeSource += ":paused"
		}
	}
	if routeSource == "" || routeSource == "pool:paused" || routeSource == "api:paused" || routeErr != nil {
		view.SourceMatch = false
	} else {
		view.SourceMatch = (candidate.CandidateType == model.RouteChainCandidateTypePool) == (routeSource == "pool")
	}
	// Pool authority (dimension D) — POOL candidates only (§二十二).
	if candidate.CandidateType == model.RouteChainCandidateTypePool {
		view.PoolAuthority = &PoolAuthorityView{
			MasterEnabled:  model.PoolMasterEnabled(),
			ChannelRouting: view.Channel != nil && view.Channel.PoolBound && channelRoutingEnabled(view.Channel.Id),
			Publication:    publicationState(routeSource),
		}
	}
	return view
}

// channelRoutingEnabled re-reads the channel's pool routing flag. PoolBound
// in TargetInfo comes from the same row family; routing is a distinct
// permission that must be displayed separately (§二十二).
func channelRoutingEnabled(channelId int) bool {
	channel, err := model.GetChannelById(channelId, false)
	if err != nil || channel == nil {
		return false
	}
	return channel.PoolRoutingEnabled()
}

// publicationState maps the chain model's pool route state onto the display
// vocabulary: nil route = no publication record; paused routes surface
// distinctly; source=api means the operator pinned the model away from the
// pool; source=pool with the master switch off is still "published" (the
// master flag is its own dimension, not folded in here).
func publicationState(routeSource string) string {
	switch routeSource {
	case "":
		return PublicationUnpublished
	case "pool":
		return PublicationPublished
	case "api":
		return PublicationAPISource
	case "pool:paused", "api:paused":
		return PublicationPaused
	default:
		return PublicationUnknown
	}
}

func effectiveBreakerView(policy *model.RouteCandidatePolicy) EffectiveBreakerView {
	cfg := routechain.ResolveCircuitConfig(policy)
	view := EffectiveBreakerView{
		FailureThreshold:  cfg.FailureThreshold,
		CooldownSeconds:   cfg.CooldownSeconds,
		HalfOpenMaxProbes: cfg.HalfOpenMaxProbes,
		RetryAfterMax:     cfg.RetryAfterMax,
	}
	if policy != nil && policy.Health != nil {
		view.FailureThresholdOverride = policy.Health.FailureThreshold > 0
		view.CooldownOverride = policy.Health.CooldownSeconds > 0
	}
	return view
}

// AttemptView is one attempt-ledger row projected for the console (§二十六):
// safe request/attempt ids, classification, health and the two-column
// billing picture — UserBillingState (mirrored settlement state) is
// deliberately separate from ProviderCostState (§二十八). UNKNOWN_REVIEW /
// unavailable values are passed through verbatim so the UI can mark them
// 需要核对 instead of rendering zeros (§二十七).
type AttemptView struct {
	AttemptId            string `json:"attempt_id"`
	RequestId            string `json:"request_id"`
	Sequence             int    `json:"sequence"`
	CandidateId          int    `json:"candidate_id"`
	CandidateType        string `json:"candidate_type"`
	ChannelId            int    `json:"channel_id"`
	StartedAt            int64  `json:"started_at"`
	FinishedAt           int64  `json:"finished_at"`
	DispatchState        string `json:"dispatch_state"`
	TerminalState        string `json:"terminal_state"`
	FinalOutcome         string `json:"final_outcome"`
	HttpStatus           int    `json:"http_status"`
	FailureClass         string `json:"failure_class"`
	FailureCode          string `json:"failure_code"`
	UpstreamResponseId   string `json:"upstream_response_id"`
	UsageState           string `json:"usage_state"`
	UserBillingState     string `json:"user_billing_state"`
	ProviderCostState    string `json:"provider_cost_state"`
	ProviderCostAmount   string `json:"provider_cost_amount"`
	ProviderCostCurrency string `json:"provider_cost_currency"`
	HealthOutcome        string `json:"health_outcome"`
	BreakerEffect        string `json:"breaker_effect"`
	StreamCompletion     string `json:"stream_completion"`
	OutcomeUncertain     bool   `json:"outcome_uncertain"`
	SafeAccountRef       string `json:"safe_account_ref"`
}

// DecisionView is one decision record projected for the console (§二十五):
// phase, decision and the safe reason code — never raw upstream bodies.
type DecisionView struct {
	Id          int64  `json:"id"`
	RequestId   string `json:"request_id"`
	CandidateId int    `json:"candidate_id"`
	Phase       string `json:"phase"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	Outcome     string `json:"outcome"`
	At          int64  `json:"at"`
}

// RecentAttempts loads a chain's attempt ledger page with the billing mirror
// joined per request (one batch query, no N+1).
func RecentAttempts(chainId int, limit int, candidateId int) ([]AttemptView, error) {
	rows, err := model.GetRouteChainAttemptsByChain(chainId, limit, candidateId)
	if err != nil {
		return nil, err
	}
	requestIds := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for i := range rows {
		if !seen[rows[i].RequestId] {
			seen[rows[i].RequestId] = true
			requestIds = append(requestIds, rows[i].RequestId)
		}
	}
	requests, err := model.GetRouteChainRequestRecords(requestIds)
	if err != nil {
		return nil, err
	}
	billing := make(map[string]string, len(requests))
	for i := range requests {
		billing[requests[i].RequestId] = requests[i].UserBillingState
	}
	views := make([]AttemptView, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		views = append(views, AttemptView{
			AttemptId:            row.AttemptId,
			RequestId:            row.RequestId,
			Sequence:             row.Sequence,
			CandidateId:          row.CandidateId,
			CandidateType:        row.CandidateType,
			ChannelId:            row.ChannelId,
			StartedAt:            row.StartedAt,
			FinishedAt:           row.FinishedAt,
			DispatchState:        row.DispatchState,
			TerminalState:        row.TerminalState,
			FinalOutcome:         row.FinalOutcome,
			HttpStatus:           row.HttpStatus,
			FailureClass:         row.FailureClass,
			FailureCode:          row.FailureCode,
			UpstreamResponseId:   row.UpstreamResponseId,
			UsageState:           row.UsageState,
			UserBillingState:     billing[row.RequestId],
			ProviderCostState:    row.ProviderCostState,
			ProviderCostAmount:   row.ProviderCostAmount,
			ProviderCostCurrency: row.ProviderCostCurrency,
			HealthOutcome:        row.HealthOutcome,
			BreakerEffect:        row.BreakerEffect,
			StreamCompletion:     row.StreamCompletion,
			OutcomeUncertain:     row.OutcomeUncertain,
			SafeAccountRef:       row.SafeAccountRef,
		})
	}
	return views, nil
}

// RecentDecisions loads a chain's decision record page.
func RecentDecisions(chainId int, limit int, candidateId int) ([]DecisionView, error) {
	rows, err := model.GetRouteChainDecisionsByChain(chainId, limit, candidateId)
	if err != nil {
		return nil, err
	}
	views := make([]DecisionView, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		views = append(views, DecisionView{
			Id:          row.Id,
			RequestId:   row.RequestId,
			CandidateId: row.CandidateId,
			Phase:       row.Phase,
			Decision:    row.Decision,
			Reason:      row.Reason,
			Outcome:     row.Outcome,
			At:          row.At,
		})
	}
	return views, nil
}

// ClampLimit normalizes the ?limit query parameter for the activity panels.
func ClampLimit(raw int, fallback int) int {
	if raw <= 0 {
		return fallback
	}
	if raw > 100 {
		return 100
	}
	return raw
}
