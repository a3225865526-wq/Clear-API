package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// CAS-guarded state transitions for the Route Chain attempt ledger (Stage 4
// design §2.5/§2.6/§2.13). Every write is single-statement and
// RowsAffected-guarded so two concurrent finalizers produce exactly one
// terminal transition; cross-table groups (request finalize + attempt
// finalize + billing claim) are intentionally independent idempotent writes
// rather than one transaction — the upstream HTTP call must never sit inside
// a DB transaction, and each write is replay-safe on its own.

var (
	// ErrRouteChainLedgerConflict reports a lost CAS race or a forbidden
	// transition (e.g. any terminal→any write; there are none by design).
	ErrRouteChainLedgerConflict = errors.New("route chain ledger conflict")
)

func nowUnix() int64 { return time.Now().Unix() }

// Route chain attempt coarse phases and terminal states. Coarse phases move
// strictly forward; terminals are final.
const (
	RouteChainPhaseCreated    = "created"
	RouteChainPhaseConnecting = "connecting"
	RouteChainPhaseDispatched = "dispatched"

	RouteChainTerminalSucceeded           = "SUCCEEDED"
	RouteChainTerminalAuthoritativeRej    = "AUTHORITATIVE_REJECTED"
	RouteChainTerminalFailedSafeToAdvance = "FAILED_SAFE_TO_ADVANCE"
	RouteChainTerminalFailedNoAdvance     = "FAILED_NO_ADVANCE"
	RouteChainTerminalOutcomeUncertain    = "OUTCOME_UNCERTAIN"
	RouteChainTerminalCancelled           = "CANCELLED"

	// Request-level terminal statuses.
	RouteChainRequestFinalSuccess           = "success"
	RouteChainRequestFinalFailed            = "failed"
	RouteChainRequestFinalRouteExhausted    = "route_exhausted"
	RouteChainRequestFinalIncompleteRestart = "incomplete_restart"
)

// CreateRouteChainRequestRecord inserts the request-level row. A duplicate
// request_id is a conflict (the caller treats repeated rows as a bug).
func CreateRouteChainRequestRecord(record *RouteChainRequestRecord) error {
	now := nowUnix()
	record.CreatedTime = now
	record.UpdatedTime = now
	return DB.Create(record).Error
}

// FinalizeRouteChainRequest writes the request's terminal routing facts
// exactly once. Later callers observe RowsAffected=0 (already finalized).
func FinalizeRouteChainRequest(requestId string, finalStatus string, finalCandidateId int, finalAttemptId string, finalClassification string, transport string) (bool, error) {
	result := DB.Model(&RouteChainRequestRecord{}).
		Where("request_id = ? AND final_status = ''", requestId).
		Updates(map[string]any{
			"final_status":         finalStatus,
			"final_candidate_id":   finalCandidateId,
			"final_attempt_id":     finalAttemptId,
			"final_classification": finalClassification,
			"transport":            transport,
			"finished_at":          nowUnix(),
			"updated_time":         nowUnix(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// NoteRouteChainRequestBilling mirrors the canonical billing terminal state
// one-way (pending → settled | settled_uncertain | refunded). It never
// overwrites a terminal value and never touches billing itself.
func NoteRouteChainRequestBilling(requestId string, billingState string, reservationId int64) (bool, error) {
	result := DB.Model(&RouteChainRequestRecord{}).
		Where("request_id = ? AND user_billing_state = ?", requestId, RouteChainBillingPending).
		Updates(map[string]any{
			"user_billing_state":     billingState,
			"billing_reservation_id": reservationId,
			"updated_time":           nowUnix(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// CreateRouteChainAttempt inserts the pre-dispatch attempt row. The row must
// exist before any upstream byte leaves (crash/restart requirement §十六).
func CreateRouteChainAttempt(record *RouteChainAttemptRecord) error {
	now := nowUnix()
	record.CreatedTime = now
	record.UpdatedTime = now
	if record.DispatchState == "" {
		record.DispatchState = RouteChainPhaseCreated
	}
	if record.UsageState == "" {
		record.UsageState = RouteChainUsagePending
	}
	if record.ProviderCostState == "" {
		record.ProviderCostState = RouteChainCostNone
	}
	return DB.Create(record).Error
}

// MarkRouteChainAttemptPhase advances the coarse phase one step
// (created→connecting, connecting→dispatched) with a CAS guard.
func MarkRouteChainAttemptPhase(attemptId string, from string, to string) (bool, error) {
	updates := map[string]any{"dispatch_state": to, "updated_time": nowUnix()}
	if to == RouteChainPhaseDispatched {
		updates["dispatch_at"] = nowUnix()
	}
	result := DB.Model(&RouteChainAttemptRecord{}).
		Where("attempt_id = ? AND dispatch_state = ? AND terminal_state = ''", attemptId, from).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// RouteChainAttemptTerminal is the full terminal write: evidence flags,
// classification, failure facts and provider-cost state, exactly once. The
// Stage 5 health fields are observability only — empty means "not observed"
// (pre-Stage-5 row or breaker gate off) and never blocks the terminal write.
type RouteChainAttemptTerminal struct {
	TerminalState          string
	FinalOutcome           string
	RequestWritten         bool
	AuthoritativeRejection bool
	WorkObserved           bool
	UsageObserved          bool
	ClientOutputObserved   bool
	ToolOutputObserved     bool
	OutcomeUncertain       bool
	HttpStatus             int
	FailureClass           string
	FailureCode            string
	UpstreamModel          string
	UpstreamResponseId     string
	ProviderCostState      string
	HealthOutcome          string
	BreakerEffect          string
	StreamCompletion       string
}

// FinalizeRouteChainAttempt applies the terminal transition exactly once
// (CAS on terminal_state=”). A RowsAffected=0 return means a concurrent or
// earlier finalizer won — the observation is a counted duplicate, never a
// second authoritative write (§十八).
func FinalizeRouteChainAttempt(attemptId string, terminal RouteChainAttemptTerminal) (bool, error) {
	updates := map[string]any{
		"terminal_state":          terminal.TerminalState,
		"final_outcome":           terminal.FinalOutcome,
		"request_written":         terminal.RequestWritten,
		"authoritative_rejection": terminal.AuthoritativeRejection,
		"work_observed":           terminal.WorkObserved,
		"usage_observed":          terminal.UsageObserved,
		"client_output_observed":  terminal.ClientOutputObserved,
		"tool_output_observed":    terminal.ToolOutputObserved,
		"outcome_uncertain":       terminal.OutcomeUncertain,
		"http_status":             terminal.HttpStatus,
		"failure_class":           terminal.FailureClass,
		"failure_code":            terminal.FailureCode,
		"upstream_model":          terminal.UpstreamModel,
		"upstream_response_id":    terminal.UpstreamResponseId,
		"finished_at":             nowUnix(),
		"updated_time":            nowUnix(),
	}
	// An empty cost state means "already refined at the authoritative usage
	// moment" — the terminal write must not clobber it with a default.
	if terminal.ProviderCostState != "" {
		updates["provider_cost_state"] = terminal.ProviderCostState
	}
	// Stage 5 health observability: recorded alongside the terminal write
	// when observed; empty never blocks or rewrites anything (§二十九).
	if terminal.HealthOutcome != "" {
		updates["health_outcome"] = terminal.HealthOutcome
	}
	if terminal.BreakerEffect != "" {
		updates["breaker_effect"] = terminal.BreakerEffect
	}
	if terminal.StreamCompletion != "" {
		updates["stream_completion"] = terminal.StreamCompletion
	}
	result := DB.Model(&RouteChainAttemptRecord{}).
		Where("attempt_id = ? AND terminal_state = ''", attemptId).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// RouteChainAttemptUsage is the canonical usage + provider-cost payload for
// one attempt.
type RouteChainAttemptUsage struct {
	UsageState           string
	InputTokens          int64
	CachedInputTokens    int64
	OutputTokens         int64
	ReasoningTokens      int64
	TotalTokens          int64
	SafeAccountRef       string
	UpstreamAttemptRef   string
	ProviderCostState    string
	ProviderCostAmount   string
	ProviderCostCurrency string
}

// ApplyRouteChainAttemptUsage backfills authoritative usage exactly once
// (CAS on usage_state='pending'). It is deliberately independent of the
// terminal state: a late final after the attempt is terminal (§十九) may
// still fill usage and provider-cost evidence, but can never change the
// terminal state, the request's final candidate, or user billing.
func ApplyRouteChainAttemptUsage(attemptId string, usage RouteChainAttemptUsage) (bool, error) {
	if usage.UsageState == RouteChainUsagePending {
		return false, errors.New("usage state must be observed or unavailable")
	}
	result := DB.Model(&RouteChainAttemptRecord{}).
		Where("attempt_id = ? AND usage_state = ?", attemptId, RouteChainUsagePending).
		Updates(map[string]any{
			"usage_state":            usage.UsageState,
			"input_tokens":           usage.InputTokens,
			"cached_input_tokens":    usage.CachedInputTokens,
			"output_tokens":          usage.OutputTokens,
			"reasoning_tokens":       usage.ReasoningTokens,
			"total_tokens":           usage.TotalTokens,
			"safe_account_ref":       usage.SafeAccountRef,
			"upstream_attempt_ref":   usage.UpstreamAttemptRef,
			"provider_cost_state":    usage.ProviderCostState,
			"provider_cost_amount":   usage.ProviderCostAmount,
			"provider_cost_currency": usage.ProviderCostCurrency,
			"updated_time":           nowUnix(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// RecordRouteChainLateEvent appends a late-callback audit note to an already
// terminal attempt (§十九): it can never reopen or rewrite the attempt.
func RecordRouteChainLateEvent(attemptId string, note string) (bool, error) {
	result := DB.Model(&RouteChainAttemptRecord{}).
		Where("attempt_id = ? AND terminal_state <> '' AND late_event_at = 0", attemptId).
		Updates(map[string]any{
			"late_event_at":   nowUnix(),
			"late_event_note": note,
			"updated_time":    nowUnix(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// SweepRouteChainAttemptUsage closes the usage of every attempt of one
// request that never produced an authoritative observation: pending →
// unavailable, explicitly not an exact zero (§2.8). Runs at request
// finalization; later usage arrivals are then duplicate no-ops by CAS.
func SweepRouteChainAttemptUsage(requestId string) (int64, error) {
	result := DB.Model(&RouteChainAttemptRecord{}).
		Where("request_id = ? AND usage_state = ?", requestId, RouteChainUsagePending).
		Updates(map[string]any{"usage_state": RouteChainUsageUnavailable, "updated_time": nowUnix()})
	return result.RowsAffected, result.Error
}

// CreateRouteChainDecision inserts one selection-skip or failover decision.
func CreateRouteChainDecision(record *RouteChainDecisionRecord) error {
	record.CreatedTime = nowUnix()
	return DB.Create(record).Error
}

// RecoverRouteChainLedgerInFlight closes records left non-terminal by a
// previous process life. Recovery classification is the hard rule §十七:
// every in-flight attempt becomes OUTCOME_UNCERTAIN — never
// safe-to-advance/SAFE_TO_RETRY — so a restart can never silently replay a
// dispatched request. Run once at startup before serving traffic.
func RecoverRouteChainLedgerInFlight() (int64, int64, error) {
	now := nowUnix()
	attempts := DB.Model(&RouteChainAttemptRecord{}).
		Where("terminal_state = ''").
		Updates(map[string]any{
			"terminal_state":      RouteChainTerminalOutcomeUncertain,
			"final_outcome":       "request_written_outcome_uncertain",
			"outcome_uncertain":   true,
			"failure_class":       "restart_recovery",
			"provider_cost_state": RouteChainCostUnknownReview,
			"usage_state":         RouteChainUsageUnavailable,
			"finished_at":         now,
			"updated_time":        now,
		})
	if attempts.Error != nil {
		return 0, 0, attempts.Error
	}
	requests := DB.Model(&RouteChainRequestRecord{}).
		Where("final_status = ''").
		Updates(map[string]any{
			"final_status":         RouteChainRequestFinalIncompleteRestart,
			"final_classification": "restart_recovery",
			"finished_at":          now,
			"updated_time":         now,
		})
	if requests.Error != nil {
		return attempts.RowsAffected, 0, requests.Error
	}
	return attempts.RowsAffected, requests.RowsAffected, nil
}

// GetRouteChainAttemptsByRequest returns the attempts of one request in
// dispatch order (repository query for tests and operators, §2.21).
func GetRouteChainAttemptsByRequest(requestId string) ([]RouteChainAttemptRecord, error) {
	var rows []RouteChainAttemptRecord
	err := DB.Where("request_id = ?", requestId).Order("sequence ASC").Find(&rows).Error
	return rows, err
}

// GetRouteChainRequestRecord returns the request-level row (nil when absent).
func GetRouteChainRequestRecord(requestId string) (*RouteChainRequestRecord, error) {
	var row RouteChainRequestRecord
	err := DB.Where("request_id = ?", requestId).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetRouteChainDecisionsByRequest returns one request's decision records in
// chronological order.
func GetRouteChainDecisionsByRequest(requestId string) ([]RouteChainDecisionRecord, error) {
	var rows []RouteChainDecisionRecord
	err := DB.Where("request_id = ?", requestId).Order("at ASC, id ASC").Find(&rows).Error
	return rows, err
}
