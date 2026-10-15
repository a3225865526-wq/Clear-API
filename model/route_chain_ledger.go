package model

import (
	"errors"

	"gorm.io/gorm"
)

// Persistent Attempt Ledger (Stage 4 design:
// docs/superpowers/specs/2026-10-06-resilient-route-chain-stage4-attempt-ledger-design.md).
// One request record per chain-routed user request, one attempt record per
// real upstream send, one decision record per selection skip / failover
// verdict. The ledger is observability, never billing authority: user
// billing stays exactly-once in billing_reservations, and no ledger write
// may change routing or settlement behavior. No credential material of any
// kind may be stored in these tables.

const (
	// RouteChainLedgerProviderCostState values (§2.7): amount=0 is never a
	// synonym for unknown or unconfigured — the state carries that meaning.
	RouteChainCostNone          = "NONE"
	RouteChainCostZeroConfirmed = "ZERO_CONFIRMED"
	RouteChainCostKnown         = "KNOWN"
	RouteChainCostUnknownReview = "UNKNOWN_REVIEW"
	RouteChainCostNotApplicable = "NOT_APPLICABLE"

	// RouteChainAttemptUsageState values (§2.8).
	RouteChainUsagePending     = "pending"
	RouteChainUsageObserved    = "observed"
	RouteChainUsageUnavailable = "unavailable"

	// RouteChainRequestBillingState values (§2.6): one-way mirror of the
	// canonical billing_reservations terminal state.
	RouteChainBillingPending          = "pending"
	RouteChainBillingSettled          = "settled"
	RouteChainBillingSettledUncertain = "settled_uncertain"
	RouteChainBillingRefunded         = "refunded"
)

// RouteChainRequestRecord is the request-level routing row. FinalStatus and
// the final_* columns are written exactly once; UserBillingState is a
// derived mirror updated one-way from the settlement sites.
type RouteChainRequestRecord struct {
	Id                   int64  `json:"id"`
	RequestId            string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	RouteChainId         int    `json:"route_chain_id" gorm:"index"`
	RouteChainVersion    int    `json:"route_chain_version"`
	RequestedModel       string `json:"requested_model" gorm:"type:varchar(255)"`
	CanonicalModel       string `json:"canonical_model" gorm:"type:varchar(255)"`
	Group                string `json:"group" gorm:"type:varchar(64)"`
	Transport            string `json:"transport" gorm:"type:varchar(16)"`
	StartedAt            int64  `json:"started_at" gorm:"index"`
	FinishedAt           int64  `json:"finished_at"`
	FinalStatus          string `json:"final_status" gorm:"type:varchar(32);index"`
	FinalCandidateId     int    `json:"final_candidate_id"`
	FinalAttemptId       string `json:"final_attempt_id" gorm:"type:varchar(64)"`
	FinalClassification  string `json:"final_classification" gorm:"type:varchar(48)"`
	UserBillingState     string `json:"user_billing_state" gorm:"type:varchar(24);index"`
	BillingReservationId int64  `json:"billing_reservation_id"`
	CreatedTime          int64  `json:"created_time"`
	UpdatedTime          int64  `json:"updated_time"`
}

// RouteChainAttemptRecord is the attempt-level row. DispatchState is the
// coarse forward-only phase; TerminalState is written exactly once by CAS;
// the evidence flags accumulate; FinalOutcome carries the full classified
// routechain outcome. Terminal regressions (SUCCEEDED→FAILED,
// OUTCOME_UNCERTAIN→safe) are structurally impossible: no terminal→any
// write exists.
type RouteChainAttemptRecord struct {
	Id                     int64  `json:"id"`
	AttemptId              string `json:"attempt_id" gorm:"type:varchar(64);uniqueIndex"`
	RequestId              string `json:"request_id" gorm:"type:varchar(64);uniqueIndex:idx_route_attempt_req_seq,priority:1"`
	Sequence               int    `json:"sequence" gorm:"uniqueIndex:idx_route_attempt_req_seq,priority:2"`
	RouteChainId           int    `json:"route_chain_id" gorm:"index"`
	RouteChainVersion      int    `json:"route_chain_version"`
	CandidateId            int    `json:"candidate_id" gorm:"index"`
	CandidateType          string `json:"candidate_type" gorm:"type:varchar(16)"`
	ChannelId              int    `json:"channel_id" gorm:"index"`
	ChannelType            int    `json:"channel_type"`
	SafeAccountRef         string `json:"safe_account_ref" gorm:"type:varchar(128)"`
	RequestedModel         string `json:"requested_model" gorm:"type:varchar(255)"`
	CanonicalModel         string `json:"canonical_model" gorm:"type:varchar(255)"`
	UpstreamModel          string `json:"upstream_model" gorm:"type:varchar(255)"`
	StartedAt              int64  `json:"started_at"`
	DispatchAt             int64  `json:"dispatch_at"`
	FinishedAt             int64  `json:"finished_at"`
	DispatchState          string `json:"dispatch_state" gorm:"type:varchar(16);index"`
	TerminalState          string `json:"terminal_state" gorm:"type:varchar(32);index"`
	FinalOutcome           string `json:"final_outcome" gorm:"type:varchar(48)"`
	RequestWritten         bool   `json:"request_written"`
	AuthoritativeRejection bool   `json:"authoritative_rejection"`
	WorkObserved           bool   `json:"work_observed"`
	UsageObserved          bool   `json:"usage_observed"`
	ClientOutputObserved   bool   `json:"client_output_observed"`
	ToolOutputObserved     bool   `json:"tool_output_observed"`
	OutcomeUncertain       bool   `json:"outcome_uncertain"`
	HttpStatus             int    `json:"http_status"`
	FailureClass           string `json:"failure_class" gorm:"type:varchar(64)"`
	FailureCode            string `json:"failure_code" gorm:"type:varchar(128)"`
	UpstreamAttemptRef     string `json:"upstream_attempt_ref" gorm:"type:varchar(128)"`
	UpstreamResponseId     string `json:"upstream_response_id" gorm:"type:varchar(128)"`
	UsageState             string `json:"usage_state" gorm:"type:varchar(16)"`
	InputTokens            int64  `json:"input_tokens"`
	CachedInputTokens      int64  `json:"cached_input_tokens"`
	OutputTokens           int64  `json:"output_tokens"`
	ReasoningTokens        int64  `json:"reasoning_tokens"`
	TotalTokens            int64  `json:"total_tokens"`
	ProviderCostState      string `json:"provider_cost_state" gorm:"type:varchar(24);index"`
	ProviderCostAmount     string `json:"provider_cost_amount" gorm:"type:varchar(64)"`
	ProviderCostCurrency   string `json:"provider_cost_currency" gorm:"type:varchar(8)"`
	// Stage 5 health observability (§四, §二十八): the independent
	// provider-health verdict and what the breaker did with it, recorded at
	// finalize time. Empty on pre-Stage-5 rows and when the breaker gate is
	// off — the ledger stays observability, never the breaker's authority.
	HealthOutcome    string `json:"health_outcome" gorm:"type:varchar(24)"`
	BreakerEffect    string `json:"breaker_effect" gorm:"type:varchar(16)"`
	StreamCompletion string `json:"stream_completion" gorm:"type:varchar(16)"`
	LateEventAt      int64  `json:"late_event_at"`
	LateEventNote    string `json:"late_event_note" gorm:"type:varchar(255)"`
	CreatedTime      int64  `json:"created_time"`
	UpdatedTime      int64  `json:"updated_time"`
}

// RouteChainDecisionRecord persists selection-phase skips and failover
// verdicts (C5: a skipped candidate is a route decision, never an attempt).
type RouteChainDecisionRecord struct {
	Id           int64  `json:"id"`
	RequestId    string `json:"request_id" gorm:"type:varchar(64);index"`
	ChainId      int    `json:"chain_id"`
	ChainVersion int    `json:"chain_version"`
	CandidateId  int    `json:"candidate_id" gorm:"index"`
	Phase        string `json:"phase" gorm:"type:varchar(24)"`
	Decision     string `json:"decision" gorm:"type:varchar(16)"`
	Reason       string `json:"reason" gorm:"type:varchar(160)"`
	Outcome      string `json:"outcome" gorm:"type:varchar(48)"`
	At           int64  `json:"at" gorm:"index"`
	CreatedTime  int64  `json:"created_time"`
}

// MigrateRouteChainLedger is explicit and idempotent; it is invoked from the
// startup AutoMigrate path so fresh, upgrade and re-run migrations all pass
// on the three supported dialects.
func MigrateRouteChainLedger(db *gorm.DB) error {
	return db.AutoMigrate(&RouteChainRequestRecord{}, &RouteChainAttemptRecord{}, &RouteChainDecisionRecord{})
}

// RouteChainLedgerReady verifies the ledger schema beyond table existence.
func RouteChainLedgerReady(db *gorm.DB) error {
	if db == nil {
		return errors.New("ledger database unavailable")
	}
	var rows []RouteChainAttemptRecord
	if err := db.Select("id", "attempt_id", "request_id", "sequence", "dispatch_state", "terminal_state", "usage_state", "provider_cost_state").Limit(0).Find(&rows).Error; err != nil {
		return err
	}
	var requests []RouteChainRequestRecord
	return db.Select("id", "request_id", "final_status", "user_billing_state").Limit(0).Find(&requests).Error
}
