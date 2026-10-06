package model

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Stage 4 attempt-ledger repository tests: CAS terminal transitions,
// exactly-once usage, one-way billing mirror, late-event append semantics
// and restart recovery (L17–L24 of the Stage 4 matrix).

func newLedgerTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ledger.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// Serialize access: concurrent writers target the same CAS rows and a
	// busy database would surface as driver lock errors, not lost races.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, MigrateRouteChainLedger(db))
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
}

func seedLedgerRequest(t *testing.T, requestId string) {
	t.Helper()
	require.NoError(t, CreateRouteChainRequestRecord(&RouteChainRequestRecord{
		RequestId: requestId, RouteChainId: 1, RouteChainVersion: 3,
		RequestedModel: "gpt-chain", CanonicalModel: "gpt-chain", Group: "default",
		StartedAt: common.GetTimestamp(), UserBillingState: RouteChainBillingPending,
	}))
}

func seedLedgerAttempt(t *testing.T, requestId string, attemptId string, sequence int) {
	t.Helper()
	require.NoError(t, CreateRouteChainAttempt(&RouteChainAttemptRecord{
		AttemptId: attemptId, RequestId: requestId, Sequence: sequence,
		RouteChainId: 1, RouteChainVersion: 3, CandidateId: 11, CandidateType: "api_channel",
		ChannelId: 21, RequestedModel: "gpt-chain", CanonicalModel: "gpt-chain",
		StartedAt: common.GetTimestamp(), DispatchState: RouteChainPhaseCreated,
		UsageState: RouteChainUsagePending, ProviderCostState: RouteChainCostNone,
	}))
}

func TestRouteChainLedgerPhasesMoveForwardOnly(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-phase")
	seedLedgerAttempt(t, "req-phase", "att-phase-1", 1)

	won, err := MarkRouteChainAttemptPhase("att-phase-1", RouteChainPhaseCreated, RouteChainPhaseConnecting)
	require.NoError(t, err)
	assert.True(t, won)
	// Out-of-order transition must be rejected.
	won, err = MarkRouteChainAttemptPhase("att-phase-1", RouteChainPhaseCreated, RouteChainPhaseConnecting)
	require.NoError(t, err)
	assert.False(t, won, "created→connecting cannot repeat after the phase moved on")
	won, err = MarkRouteChainAttemptPhase("att-phase-1", RouteChainPhaseConnecting, RouteChainPhaseDispatched)
	require.NoError(t, err)
	assert.True(t, won)
	var attempt RouteChainAttemptRecord
	require.NoError(t, DB.Where("attempt_id = ?", "att-phase-1").First(&attempt).Error)
	assert.Equal(t, RouteChainPhaseDispatched, attempt.DispatchState)
	assert.NotZero(t, attempt.DispatchAt)
}

func TestRouteChainLedgerFinalizeExactlyOnce(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-final")
	seedLedgerAttempt(t, "req-final", "att-final-1", 1)

	terminal := RouteChainAttemptTerminal{
		TerminalState: RouteChainTerminalAuthoritativeRej, FinalOutcome: "authoritative_rejection_no_work",
		RequestWritten: true, AuthoritativeRejection: true, HttpStatus: 401,
		FailureClass: "authoritative_rejection_no_work", FailureCode: "invalid_api_key",
		ProviderCostState: RouteChainCostZeroConfirmed,
	}
	won, err := FinalizeRouteChainAttempt("att-final-1", terminal)
	require.NoError(t, err)
	assert.True(t, won)
	// A second finalizer (§十八 duplicate) is a counted no-op that must not
	// rewrite the terminal state or cost classification.
	duplicate := terminal
	duplicate.TerminalState = RouteChainTerminalFailedNoAdvance
	duplicate.ProviderCostState = RouteChainCostUnknownReview
	won, err = FinalizeRouteChainAttempt("att-final-1", duplicate)
	require.NoError(t, err)
	assert.False(t, won)
	var attempt RouteChainAttemptRecord
	require.NoError(t, DB.Where("attempt_id = ?", "att-final-1").First(&attempt).Error)
	assert.Equal(t, RouteChainTerminalAuthoritativeRej, attempt.TerminalState)
	assert.Equal(t, RouteChainCostZeroConfirmed, attempt.ProviderCostState)
	assert.NotZero(t, attempt.FinishedAt)
}

func TestRouteChainLedgerUsageExactlyOnceAndUnavailableNotZero(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-usage")
	seedLedgerAttempt(t, "req-usage", "att-usage-1", 1)
	seedLedgerAttempt(t, "req-usage", "att-usage-2", 2)

	// Authoritative usage arrives exactly once (L17): the duplicate final is
	// a no-op and must not accumulate tokens again.
	won, err := ApplyRouteChainAttemptUsage("att-usage-1", RouteChainAttemptUsage{
		UsageState: RouteChainUsageObserved, InputTokens: 100, CachedInputTokens: 20,
		OutputTokens: 30, ReasoningTokens: 10, TotalTokens: 130,
		SafeAccountRef: "a1b2c3d4e5f6a7b8", UpstreamAttemptRef: "exec-attempt-1",
		ProviderCostState: RouteChainCostKnown, ProviderCostAmount: "0.012", ProviderCostCurrency: "CNY",
	})
	require.NoError(t, err)
	assert.True(t, won)
	won, err = ApplyRouteChainAttemptUsage("att-usage-1", RouteChainAttemptUsage{
		UsageState: RouteChainUsageObserved, InputTokens: 100, OutputTokens: 30, TotalTokens: 130,
		ProviderCostState: RouteChainCostKnown, ProviderCostAmount: "0.012",
	})
	require.NoError(t, err)
	assert.False(t, won, "duplicate final usage must be a no-op")
	var attempt RouteChainAttemptRecord
	require.NoError(t, DB.Where("attempt_id = ?", "att-usage-1").First(&attempt).Error)
	assert.Equal(t, RouteChainUsageObserved, attempt.UsageState)
	assert.Equal(t, int64(100), attempt.InputTokens)
	assert.Equal(t, int64(130), attempt.TotalTokens)
	assert.Equal(t, "a1b2c3d4e5f6a7b8", attempt.SafeAccountRef)

	// Usage unavailable (L23) is an explicit state, never an exact zero:
	// the sweep leaves token columns untouched while the state is
	// distinguishable.
	swept, err := SweepRouteChainAttemptUsage("req-usage")
	require.NoError(t, err)
	assert.Equal(t, int64(1), swept, "att-usage-2 was still pending")
	// A reused destination carries its primary key into the next GORM First
	// query; zero it before re-querying.
	attempt = RouteChainAttemptRecord{}
	require.NoError(t, DB.Where("attempt_id = ?", "att-usage-2").First(&attempt).Error)
	assert.Equal(t, RouteChainUsageUnavailable, attempt.UsageState)
	// Post-sweep usage arrivals (late finals) are exactly-once no-ops.
	won, err = ApplyRouteChainAttemptUsage("att-usage-2", RouteChainAttemptUsage{
		UsageState: RouteChainUsageObserved, InputTokens: 999,
	})
	require.NoError(t, err)
	assert.False(t, won, "usage after the request-final sweep must not overwrite the explicit unavailable marker")
}

func TestRouteChainLedgerProviderCostStatesDistinguishable(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-cost")
	cases := map[string]string{
		"att-cost-none":    RouteChainCostNone,
		"att-cost-zero":    RouteChainCostZeroConfirmed,
		"att-cost-known":   RouteChainCostKnown,
		"att-cost-unknown": RouteChainCostUnknownReview,
		"att-cost-na":      RouteChainCostNotApplicable,
	}
	sequence := 0
	for attemptId, state := range cases {
		sequence++
		seedLedgerAttempt(t, "req-cost", attemptId, sequence)
		terminal := RouteChainAttemptTerminal{TerminalState: RouteChainTerminalFailedNoAdvance, ProviderCostState: state}
		won, err := FinalizeRouteChainAttempt(attemptId, terminal)
		require.NoError(t, err)
		assert.True(t, won)
	}
	for attemptId, state := range cases {
		var attempt RouteChainAttemptRecord
		require.NoError(t, DB.Where("attempt_id = ?", attemptId).First(&attempt).Error)
		assert.Equal(t, state, attempt.ProviderCostState, attemptId)
	}
	// amount=0 is never a synonym for unknown: ZERO_CONFIRMED with an amount
	// and UNKNOWN_REVIEW without one are different rows.
	var zero, unknown RouteChainAttemptRecord
	require.NoError(t, DB.Where("attempt_id = ?", "att-cost-zero").First(&zero).Error)
	require.NoError(t, DB.Where("attempt_id = ?", "att-cost-unknown").First(&unknown).Error)
	assert.NotEqual(t, zero.ProviderCostState, unknown.ProviderCostState)
}

func TestRouteChainLedgerBillingMirrorOneWay(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-billing")
	won, err := NoteRouteChainRequestBilling("req-billing", RouteChainBillingSettled, 42)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = NoteRouteChainRequestBilling("req-billing", RouteChainBillingRefunded, 42)
	require.NoError(t, err)
	assert.False(t, won, "terminal mirror is one-way")
	var record RouteChainRequestRecord
	require.NoError(t, DB.Where("request_id = ?", "req-billing").First(&record).Error)
	assert.Equal(t, RouteChainBillingSettled, record.UserBillingState)
	assert.Equal(t, int64(42), record.BillingReservationId)
}

func TestRouteChainLedgerLateEventAppendOnly(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-late")
	seedLedgerAttempt(t, "req-late", "att-late-1", 1)
	// §十九: a late event can never reopen or rewrite a terminal attempt.
	won, err := RecordRouteChainLateEvent("att-late-1", "late usage before terminal")
	require.NoError(t, err)
	assert.False(t, won, "non-terminal attempts cannot take late events")
	won, err = FinalizeRouteChainAttempt("att-late-1", RouteChainAttemptTerminal{
		TerminalState: RouteChainTerminalSucceeded, FinalOutcome: "success",
	})
	require.NoError(t, err)
	assert.True(t, won)
	won, err = RecordRouteChainLateEvent("att-late-1", "late provider cost evidence")
	require.NoError(t, err)
	assert.True(t, won)
	// Only the first late event wins.
	won, err = RecordRouteChainLateEvent("att-late-1", "another late event")
	require.NoError(t, err)
	assert.False(t, won)
	var attempt RouteChainAttemptRecord
	require.NoError(t, DB.Where("attempt_id = ?", "att-late-1").First(&attempt).Error)
	assert.Equal(t, RouteChainTerminalSucceeded, attempt.TerminalState, "late events must not change the terminal state")
	assert.Equal(t, "late provider cost evidence", attempt.LateEventNote)
}

func TestRouteChainLedgerRestartRecovery(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-crash")
	seedLedgerAttempt(t, "req-crash", "att-crash-1", 1)
	won, err := MarkRouteChainAttemptPhase("att-crash-1", RouteChainPhaseCreated, RouteChainPhaseConnecting)
	require.NoError(t, err)
	assert.True(t, won)
	seedLedgerRequest(t, "req-done")
	seedLedgerAttempt(t, "req-done", "att-done-1", 1)
	won, err = FinalizeRouteChainAttempt("att-done-1", RouteChainAttemptTerminal{
		TerminalState: RouteChainTerminalSucceeded, FinalOutcome: "success",
	})
	require.NoError(t, err)
	assert.True(t, won)
	won, err = FinalizeRouteChainRequest("req-done", RouteChainRequestFinalSuccess, 11, "att-done-1", "success", "http")
	require.NoError(t, err)
	assert.True(t, won)

	// L19: crash after dispatch — recovery classifies in-flight attempts as
	// OUTCOME_UNCERTAIN (never safe-to-advance) and closes the request.
	attempts, requests, err := RecoverRouteChainLedgerInFlight()
	require.NoError(t, err)
	assert.Equal(t, int64(1), attempts)
	assert.Equal(t, int64(1), requests)
	var recovered RouteChainAttemptRecord
	require.NoError(t, DB.Where("attempt_id = ?", "att-crash-1").First(&recovered).Error)
	assert.Equal(t, RouteChainTerminalOutcomeUncertain, recovered.TerminalState)
	assert.Equal(t, "restart_recovery", recovered.FailureClass)
	assert.Equal(t, RouteChainCostUnknownReview, recovered.ProviderCostState)
	assert.Equal(t, RouteChainUsageUnavailable, recovered.UsageState)
	var unfinished RouteChainRequestRecord
	require.NoError(t, DB.Where("request_id = ?", "req-crash").First(&unfinished).Error)
	assert.Equal(t, RouteChainRequestFinalIncompleteRestart, unfinished.FinalStatus)
	// The finalized pair is untouched.
	var done RouteChainRequestRecord
	require.NoError(t, DB.Where("request_id = ?", "req-done").First(&done).Error)
	assert.Equal(t, RouteChainRequestFinalSuccess, done.FinalStatus)
	// Idempotent: a second pass finds nothing.
	attempts, requests, err = RecoverRouteChainLedgerInFlight()
	require.NoError(t, err)
	assert.Zero(t, attempts)
	assert.Zero(t, requests)
}

func TestRouteChainLedgerFinalizeRaceExactlyOneWinner(t *testing.T) {
	newLedgerTestDB(t)
	seedLedgerRequest(t, "req-race")
	seedLedgerAttempt(t, "req-race", "att-race-1", 1)

	// L20: concurrent terminal writers — exactly one final transition wins
	// and exactly one billing-mirror transition wins.
	const writers = 16
	wins := make(chan int, writers)
	mirrorWins := make(chan int, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			terminal := RouteChainAttemptTerminal{TerminalState: RouteChainTerminalFailedNoAdvance, FinalOutcome: "usage_observed", ProviderCostState: RouteChainCostUnknownReview}
			if i%2 == 0 {
				terminal = RouteChainAttemptTerminal{TerminalState: RouteChainTerminalFailedSafeToAdvance, FinalOutcome: "pre_dispatch_unavailable", ProviderCostState: RouteChainCostNone}
			}
			if won, err := FinalizeRouteChainAttempt("att-race-1", terminal); err == nil && won {
				wins <- i
			} else if err != nil {
				t.Errorf("finalize: %v", err)
			}
			if won, err := NoteRouteChainRequestBilling("req-race", RouteChainBillingSettled, 0); err == nil && won {
				mirrorWins <- i
			} else if err != nil {
				t.Errorf("mirror: %v", err)
			}
		}(i)
	}
	wg.Wait()
	close(wins)
	close(mirrorWins)
	assert.Len(t, wins, 1, "exactly one terminal attempt transition may win")
	assert.Len(t, mirrorWins, 1, "exactly one billing-mirror transition may win")
	var attempt RouteChainAttemptRecord
	require.NoError(t, DB.Where("attempt_id = ?", "att-race-1").First(&attempt).Error)
	assert.NotEmpty(t, attempt.TerminalState)
	var record RouteChainRequestRecord
	require.NoError(t, DB.Where("request_id = ?", "req-race").First(&record).Error)
	assert.NotEqual(t, RouteChainBillingPending, record.UserBillingState)
}
