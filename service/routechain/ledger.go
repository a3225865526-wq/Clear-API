package routechain

import (
	"fmt"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// Ledger orchestration (Stage 4): durable request/attempt/decision records
// for chain-routed requests. Every write is best-effort — a ledger failure
// is counted and logged, never allowed to change routing or billing. The
// billing_reservations row remains the only user-billing authority; the
// ledger mirrors observable state one-way.

const (
	// ledgerDisabledKey marks a request whose request-record insert failed;
	// every later ledger write for it becomes a no-op.
	ledgerDisabledKey = "route_chain_ledger_disabled"
	// ledgerRowKey holds the request id when the request record exists —
	// the cheap guard that keeps legacy requests at zero ledger cost.
	ledgerRowKey = "route_chain_ledger_request_row"
	// AttemptIDKey holds the canonical attempt id of the most recent
	// attempt; it propagates to the finance attempt (BeginFinanceAttempt
	// adopts it) so finance events join the ledger by attempt_id.
	AttemptIDKey = "route_chain_attempt_id"
	// lastOutcomeKey stashes the last classified outcome for the request
	// record's final_classification.
	lastOutcomeKey = "route_chain_last_outcome"
)

var ledgerCaptureFailures atomic.Int64

func recordLedgerFailure(c *gin.Context, what string, err error) {
	ledgerCaptureFailures.Add(1)
	logger.LogError(c, fmt.Sprintf("route_chain subsystem=ledger event=capture_failed what=%s err=%v", what, err))
}

// LedgerCaptureFailures reports best-effort write failures since process
// start (observability only).
func LedgerCaptureFailures() int64 { return ledgerCaptureFailures.Load() }

// BeginRequestLedger creates the request-level record for a chain-routed
// request. Called once from BeginRequest — only POST relay paths that the
// chain admits ever reach it (WS realtime/MJ/tasks are excluded upstream).
func BeginRequestLedger(c *gin.Context, snapshot *Snapshot) {
	requestId := c.GetString(common.RequestIdKey)
	if requestId == "" {
		// Without a request id there is nothing to correlate; production
		// always sets one (RequestId middleware), so this is a pure guard.
		c.Set(ledgerDisabledKey, true)
		return
	}
	record := &model.RouteChainRequestRecord{
		RequestId:         requestId,
		RouteChainId:      snapshot.ChainId,
		RouteChainVersion: snapshot.Version,
		RequestedModel:    snapshot.Model,
		CanonicalModel:    snapshot.Model,
		StartedAt:         common.GetTimestamp(),
		UserBillingState:  model.RouteChainBillingPending,
	}
	if err := model.CreateRouteChainRequestRecord(record); err != nil {
		c.Set(ledgerDisabledKey, true)
		recordLedgerFailure(c, "request_record", err)
		return
	}
	c.Set(ledgerRowKey, requestId)
}

// BeginAttemptLedger creates the pre-dispatch attempt record and publishes
// the canonical attempt id. Must run before the relay handler touches the
// network (crash/restart requirement: a dispatched attempt can never be
// missing from the ledger).
func BeginAttemptLedger(c *gin.Context, state *RequestState, info *relaycommon.RelayInfo, channel *model.Channel) {
	if c.GetBool(ledgerDisabledKey) || state == nil || state.Snapshot == nil || info == nil || channel == nil {
		return
	}
	candidate := state.FailingCandidate()
	if candidate == nil {
		return
	}
	record := &model.RouteChainAttemptRecord{
		AttemptId:         common.GetUUID(),
		RequestId:         c.GetString(common.RequestIdKey),
		Sequence:          state.AttemptsMade(),
		RouteChainId:      state.Snapshot.ChainId,
		RouteChainVersion: state.Snapshot.Version,
		CandidateId:       candidate.Id,
		CandidateType:     candidate.Type,
		ChannelId:         channel.Id,
		ChannelType:       channel.Type,
		RequestedModel:    info.OriginModelName,
		CanonicalModel:    state.Snapshot.Model,
		StartedAt:         common.GetTimestamp(),
		DispatchState:     model.RouteChainPhaseCreated,
	}
	if err := model.CreateRouteChainAttempt(record); err != nil {
		recordLedgerFailure(c, "attempt_record", err)
		return
	}
	c.Set(AttemptIDKey, record.AttemptId)
	logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=ledger event=attempt_created attempt_id=%s request_id=%s candidate_id=%d sequence=%d", record.AttemptId, record.RequestId, record.CandidateId, record.Sequence))
}

// ActiveAttemptID returns the canonical attempt id of the most recent
// attempt, or "" when the request is not chain-routed (cheap guard).
func ActiveAttemptID(c *gin.Context) string {
	return c.GetString(AttemptIDKey)
}

// NoteAttemptConnecting advances the coarse phase created→connecting at the
// transport entry point.
func NoteAttemptConnecting(c *gin.Context) {
	if attemptId := c.GetString(AttemptIDKey); attemptId != "" {
		if _, err := model.MarkRouteChainAttemptPhase(attemptId, model.RouteChainPhaseCreated, model.RouteChainPhaseConnecting); err != nil {
			recordLedgerFailure(c, "attempt_connecting", err)
		}
	}
}

// NoteAttemptDispatched advances connecting→dispatched at the WroteRequest
// fence point (the same evidence the dispatch fence uses).
func NoteAttemptDispatched(c *gin.Context) {
	if attemptId := c.GetString(AttemptIDKey); attemptId != "" {
		if _, err := model.MarkRouteChainAttemptPhase(attemptId, model.RouteChainPhaseConnecting, model.RouteChainPhaseDispatched); err != nil {
			recordLedgerFailure(c, "attempt_dispatched", err)
		}
	}
}

// terminalMapping derives (terminal state, provider cost state) from the
// classified outcome and the evidence (design §2.5/§2.7). provider cost for
// post-dispatch situations is UNKNOWN_REVIEW unless the classifier proved
// zero work; pre-dispatch situations never touched the upstream.
func terminalMapping(outcome Outcome, evidence Evidence, financeEnabled bool) (string, string) {
	switch outcome {
	case OutcomeSuccess:
		return model.RouteChainTerminalSucceeded, model.RouteChainCostNone
	case OutcomePreDispatchUnavailable, OutcomeRequestInvalid:
		return model.RouteChainTerminalFailedSafeToAdvance, model.RouteChainCostNone
	case OutcomePoolNoEligibleAccount, OutcomeAuthoritativeRejectionNoWork, OutcomeRateLimitRejectionNoWork, OutcomeExecutorLocalRejectedNoWork:
		return model.RouteChainTerminalAuthoritativeRej, model.RouteChainCostZeroConfirmed
	case OutcomeRequestWrittenOutcomeUncertain:
		return model.RouteChainTerminalOutcomeUncertain, model.RouteChainCostUnknownReview
	case OutcomeUpstreamWorkObserved, OutcomeUsageObserved, OutcomeClientOutputObserved, OutcomeToolOutputObserved:
		return model.RouteChainTerminalFailedNoAdvance, model.RouteChainCostUnknownReview
	case OutcomeRoutePolicyRejected:
		if evidence.RequestWritten {
			return model.RouteChainTerminalFailedNoAdvance, model.RouteChainCostUnknownReview
		}
		return model.RouteChainTerminalCancelled, model.RouteChainCostNone
	default:
		return model.RouteChainTerminalOutcomeUncertain, model.RouteChainCostUnknownReview
	}
}

func authoritativeOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomePoolNoEligibleAccount, OutcomeAuthoritativeRejectionNoWork, OutcomeRateLimitRejectionNoWork, OutcomeExecutorLocalRejectedNoWork:
		return true
	}
	return false
}

// streamCompletionLabel derives the ledger's stream-completion label from the
// StreamStatus snapshot (§四: the interrupted detail becomes durable data).
// Empty means the attempt had no stream observation (non-stream transport).
func streamCompletionLabel(stream relaycommon.StreamOutcome) string {
	if stream.EndReason == "" && stream.Response == "" {
		return ""
	}
	switch stream.EndReason {
	case relaycommon.StreamEndReasonClientGone, relaycommon.StreamEndReasonPingFail:
		return "client_gone"
	case relaycommon.StreamEndReasonTimeout:
		return "timeout"
	case relaycommon.StreamEndReasonDone, relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonHandlerStop:
		switch stream.Response {
		case relaycommon.ResponseOutcomeFailed, relaycommon.ResponseOutcomeIncomplete:
			return "abnormal"
		}
		return "normal"
	default:
		return "abnormal" // scanner_error, panic
	}
}

// streamSnapshotOf returns the StreamStatus outcome snapshot when available.
func streamSnapshotOf(relayInfo *relaycommon.RelayInfo) relaycommon.StreamOutcome {
	if relayInfo == nil {
		return relaycommon.StreamOutcome{}
	}
	return relayInfo.StreamStatus.OutcomeSnapshot()
}

// FinalizeAttemptForFailure writes the failed attempt's terminal record and
// the failover decision record. Called from chainRetryDecision before the
// per-attempt flag reset, while the attempt's evidence is still fresh.
// preDispatchAborted marks a pre-dispatch abort (overall deadline or client
// gone before any send) — recorded as CANCELLED, not a failure. health is the
// Stage 5 verdict computed before this call (empty when the breaker gate is
// off — the columns stay unobserved).
func FinalizeAttemptForFailure(c *gin.Context, state *RequestState, relayInfo *relaycommon.RelayInfo, relayErr *types.NewAPIError, evidence Evidence, outcome Outcome, preDispatchAborted bool, health AttemptHealth) {
	attemptId := c.GetString(AttemptIDKey)
	if attemptId == "" || c.GetBool(ledgerDisabledKey) {
		return
	}
	terminalState, costState := terminalMapping(outcome, evidence, common.FinanceEnabled())
	if preDispatchAborted {
		terminalState = model.RouteChainTerminalCancelled
	}
	failureCode := evidence.UpstreamErrorCode
	if failureCode == "" && relayErr != nil {
		failureCode = string(relayErr.GetErrorCode())
	}
	won, err := model.FinalizeRouteChainAttempt(attemptId, model.RouteChainAttemptTerminal{
		TerminalState:          terminalState,
		FinalOutcome:           string(outcome),
		RequestWritten:         evidence.RequestWritten,
		AuthoritativeRejection: authoritativeOutcome(outcome),
		WorkObserved:           evidence.WorkObserved,
		UsageObserved:          evidence.UsageObserved,
		ClientOutputObserved:   evidence.ClientOutputObserved,
		ToolOutputObserved:     evidence.ToolOutputObserved,
		OutcomeUncertain:       terminalState == model.RouteChainTerminalOutcomeUncertain,
		HttpStatus:             evidence.HTTPStatus,
		FailureClass:           string(outcome),
		FailureCode:            failureCode,
		UpstreamModel:          upstreamModelOf(relayInfo),
		UpstreamResponseId:     c.GetString(common.UpstreamRequestIdKey),
		ProviderCostState:      costState,
		HealthOutcome:          string(health.Verdict),
		BreakerEffect:          health.Effect,
		StreamCompletion:       streamCompletionLabel(streamSnapshotOf(relayInfo)),
	})
	if err != nil {
		recordLedgerFailure(c, "attempt_final", err)
	}
	if won {
		logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=ledger event=attempt_finalized attempt_id=%s terminal=%s outcome=%s cost=%s", attemptId, terminalState, outcome, costState))
	} else {
		logger.LogWarn(c, fmt.Sprintf("route_chain subsystem=ledger event=attempt_final_duplicate attempt_id=%s", attemptId))
	}
	c.Set(lastOutcomeKey, string(outcome))
}

func upstreamModelOf(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return info.UpstreamModelName
}

// FinalizeAttemptSuccess writes the SUCCEEDED terminal record for the
// attempt that completed the request. Usage and provider cost arrive
// separately at the authoritative usage moment (PostTextConsumeQuota).
// health is the Stage 5 verdict — a normally-ended stream records
// health_success; an abnormally cut stream records health_failure alongside
// the SUCCEEDED terminal (the §三 separation made durable).
func FinalizeAttemptSuccess(c *gin.Context, relayInfo *relaycommon.RelayInfo, health AttemptHealth) {
	attemptId := c.GetString(AttemptIDKey)
	if attemptId == "" || c.GetBool(ledgerDisabledKey) {
		return
	}
	won, err := model.FinalizeRouteChainAttempt(attemptId, model.RouteChainAttemptTerminal{
		TerminalState:      model.RouteChainTerminalSucceeded,
		FinalOutcome:       string(OutcomeSuccess),
		UpstreamModel:      upstreamModelOf(relayInfo),
		UpstreamResponseId: c.GetString(common.UpstreamRequestIdKey),
		HealthOutcome:      string(health.Verdict),
		BreakerEffect:      health.Effect,
		StreamCompletion:   streamCompletionLabel(streamSnapshotOf(relayInfo)),
		// Provider cost for a success is refined at the authoritative usage
		// moment (NoteAttemptUsage), which has already run inside the
		// handler; the terminal write must not overwrite it.
	})
	if err != nil {
		recordLedgerFailure(c, "attempt_final_success", err)
		return
	}
	if won {
		logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=ledger event=attempt_finalized attempt_id=%s terminal=%s", attemptId, model.RouteChainTerminalSucceeded))
	}
	c.Set(lastOutcomeKey, string(OutcomeSuccess))
}

// finishRequestLedgerStatus derives the request-level terminal status.
func finishRequestLedgerStatus(state *RequestState, apiErr *types.NewAPIError) string {
	if state != nil && state.Succeeded() {
		return model.RouteChainRequestFinalSuccess
	}
	if apiErr != nil && apiErr.GetErrorCode() == types.ErrorCodeRouteExhausted {
		return model.RouteChainRequestFinalRouteExhausted
	}
	return model.RouteChainRequestFinalFailed
}

// FinishRequestLedger finalizes the request record exactly once and sweeps
// any attempt whose usage never arrived to an explicit unavailable state.
// Registered before the billing defer so it runs after the request's final
// billing outcome is known. Panic-safe: it never panics itself.
func FinishRequestLedger(c *gin.Context, state *RequestState, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	defer func() {
		if r := recover(); r != nil {
			recordLedgerFailure(c, "finish_request_panic", fmt.Errorf("%v", r))
		}
	}()
	requestId := c.GetString(ledgerRowKey)
	if requestId == "" {
		return
	}
	transport := "http"
	if relayInfo != nil && relayInfo.IsStream {
		transport = "sse"
	}
	finalStatus := finishRequestLedgerStatus(state, apiErr)
	finalCandidateId := 0
	if state != nil {
		if candidate := state.FailingCandidate(); candidate != nil {
			finalCandidateId = candidate.Id
		}
	}
	won, err := model.FinalizeRouteChainRequest(requestId, finalStatus, finalCandidateId,
		c.GetString(AttemptIDKey), c.GetString(lastOutcomeKey), transport)
	if err != nil {
		recordLedgerFailure(c, "request_final", err)
	} else if won {
		logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=ledger event=request_finalized request_id=%s final_status=%s", requestId, finalStatus))
	}
	// Any attempt that never produced an authoritative usage observation is
	// explicitly marked unavailable — never an exact zero (§2.8). Late
	// provider-cost evidence can still arrive later through the late-event
	// path; usage itself is exactly-once.
	if _, err := model.SweepRouteChainAttemptUsage(requestId); err != nil {
		recordLedgerFailure(c, "usage_sweep", err)
	}
}

// minimal finance usage snapshot (JSON contract of
// service.FinanceUsageSnapshot — duplicated here to avoid a service import
// cycle; only the fields the ledger needs are declared).
type financeUsageEstimate struct {
	Amount *string `json:"amount"`
	Status string  `json:"status"`
}

type financeUsageSnapshotView struct {
	Quantities map[string]*string   `json:"quantities"`
	Source     string               `json:"source"`
	Estimate   financeUsageEstimate `json:"estimate"`
	CostCNY    *string              `json:"estimated_cost_cny"`
}

// canonicalUsage maps the authoritative dto.Usage onto the ledger's
// normalized token columns (§2.8). Observed values are written as-is,
// including legitimate zeros; absence is expressed by the caller through
// UsageState=unavailable, never by fabricated zeros.
func canonicalUsage(usage *dto.Usage) (input, cached, output, reasoning, total int64) {
	if usage == nil {
		return
	}
	input = int64(usage.PromptTokens)
	output = int64(usage.CompletionTokens)
	total = int64(usage.TotalTokens)
	cached = int64(usage.PromptTokensDetails.CachedTokens)
	reasoning = int64(usage.CompletionTokenDetails.ReasoningTokens)
	return
}

// providerCostFromEstimate refines the provider-cost state from the finance
// usage fact of this attempt (§2.7): KNOWN with a CNY-normalized amount when
// the estimate resolved, UNKNOWN_REVIEW when finance is on but the price is
// missing, NOT_APPLICABLE when provider-cost accounting is off.
func providerCostFromEstimate(financeEnabled bool, snapshot *financeUsageSnapshotView) (string, string, string) {
	if !financeEnabled {
		return model.RouteChainCostNotApplicable, "", ""
	}
	if snapshot != nil && snapshot.Estimate.Amount != nil {
		amount, currency := *snapshot.Estimate.Amount, ""
		if snapshot.CostCNY != nil {
			amount, currency = *snapshot.CostCNY, "CNY"
		}
		return model.RouteChainCostKnown, amount, currency
	}
	return model.RouteChainCostUnknownReview, "", ""
}

// loadFinanceUsageView reads the finance usage fact written for this
// attempt (phase=usage, attempt_id = the canonical id since BeginFinanceAttempt
// adopts it). Best-effort: a missing row is not an error.
func loadFinanceUsageView(c *gin.Context, attemptId string) *financeUsageSnapshotView {
	if !common.FinanceEnabled() {
		return nil
	}
	var event model.FinanceRequestEvent
	if err := model.DB.Where("attempt_id = ? AND phase = ?", attemptId, "usage").Order("id DESC").First(&event).Error; err != nil {
		return nil
	}
	var view financeUsageSnapshotView
	if common.UnmarshalJsonStr(event.Payload, &view) != nil {
		return nil
	}
	return &view
}

// NoteAttemptUsage persists the authoritative usage of the current attempt
// exactly once (CAS pending→filled; duplicate finals are counted no-ops,
// §十八). Called from PostTextConsumeQuota after the finance usage fact was
// captured, and from PostCodexUnavailableQuota for the explicit-unavailable
// path. usage==nil (or codex-unavailable) marks usage_state=unavailable —
// never an exact zero. safeAccountRef / upstreamAttemptRef carry the
// executor-annotated pool linkage (sha256 account ref; executor metering
// attempt id), empty when the executor never selected an account.
func NoteAttemptUsage(c *gin.Context, usage *dto.Usage, safeAccountRef string, upstreamAttemptRef string) {
	attemptId := c.GetString(AttemptIDKey)
	if attemptId == "" || c.GetBool(ledgerDisabledKey) {
		return
	}
	financeEnabled := common.FinanceEnabled()
	usageState := model.RouteChainUsageObserved
	input, cached, output, reasoning, total := canonicalUsage(usage)
	if usage == nil || (usage.CodexMetering != nil && usage.CodexMetering.Status != "exact") {
		usageState = model.RouteChainUsageUnavailable
		input, cached, output, reasoning, total = 0, 0, 0, 0, 0
	}
	costState, costAmount, costCurrency := providerCostFromEstimate(financeEnabled, loadFinanceUsageView(c, attemptId))
	// Keep a zero-work terminal cost classification: the usage backfill must
	// never weaken ZERO_CONFIRMED into UNKNOWN_REVIEW.
	if usageState == model.RouteChainUsageUnavailable {
		var attempt model.RouteChainAttemptRecord
		if err := model.DB.Where("attempt_id = ?", attemptId).First(&attempt).Error; err == nil {
			switch attempt.ProviderCostState {
			case model.RouteChainCostZeroConfirmed, model.RouteChainCostNone, model.RouteChainCostNotApplicable:
				costState, costAmount, costCurrency = attempt.ProviderCostState, "", ""
			}
		}
	}
	won, err := model.ApplyRouteChainAttemptUsage(attemptId, model.RouteChainAttemptUsage{
		UsageState:           usageState,
		InputTokens:          input,
		CachedInputTokens:    cached,
		OutputTokens:         output,
		ReasoningTokens:      reasoning,
		TotalTokens:          total,
		SafeAccountRef:       safeAccountRef,
		UpstreamAttemptRef:   upstreamAttemptRef,
		ProviderCostState:    costState,
		ProviderCostAmount:   costAmount,
		ProviderCostCurrency: costCurrency,
	})
	if err != nil {
		recordLedgerFailure(c, "attempt_usage", err)
		return
	}
	if !won {
		// Duplicate final: first authoritative usage won; this is a counted
		// no-op (§十八).
		logger.LogWarn(c, fmt.Sprintf("route_chain subsystem=ledger event=duplicate_final_usage attempt_id=%s", attemptId))
	}
}

// NoteRequestBilling mirrors the canonical billing terminal state one-way
// (§2.6). Called from the settlement sites; a no-op for legacy requests and
// for already-terminal mirrors. It never touches billing itself.
func NoteRequestBilling(c *gin.Context, billingState string) {
	requestId := c.GetString(ledgerRowKey)
	if requestId == "" {
		return
	}
	if _, err := model.NoteRouteChainRequestBilling(requestId, billingState, 0); err != nil {
		recordLedgerFailure(c, "billing_mirror", err)
	}
}

// NoteSkippedDecision persists a selection-phase skip (C5: a skipped
// candidate is a route decision, never an attempt).
func NoteSkippedDecision(c *gin.Context, state *RequestState, candidateId int, reason string) {
	noteDecision(c, state, candidateId, "selection_skip", "skipped", "", reason)
}

// NoteFailoverDecision persists the centralized policy verdict.
func NoteFailoverDecision(c *gin.Context, state *RequestState, candidateId int, decision Decision, outcome Outcome) {
	action := "stop"
	if decision.Allowed {
		action = "advance"
	}
	noteDecision(c, state, candidateId, "failover_decision", action, string(outcome), decision.Reason)
}

func noteDecision(c *gin.Context, state *RequestState, candidateId int, phase string, decision string, outcome string, reason string) {
	if state == nil || state.Snapshot == nil || c.GetBool(ledgerDisabledKey) {
		return
	}
	requestId := c.GetString(common.RequestIdKey)
	if requestId == "" {
		return
	}
	if len(reason) > 160 {
		reason = reason[:160]
	}
	record := &model.RouteChainDecisionRecord{
		RequestId:    requestId,
		ChainId:      state.Snapshot.ChainId,
		ChainVersion: state.Snapshot.Version,
		CandidateId:  candidateId,
		Phase:        phase,
		Decision:     decision,
		Reason:       reason,
		Outcome:      outcome,
		At:           common.GetTimestamp(),
	}
	if err := model.CreateRouteChainDecision(record); err != nil {
		recordLedgerFailure(c, "decision_record", err)
	}
}
