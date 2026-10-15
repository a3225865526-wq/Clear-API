package routechain

import (
	"path/filepath"
	"testing"

	"net/http/httptest"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Stage 4 ledger orchestration tests (service level): exactly-once usage
// persistence with provider-cost refinement, zero-work cost preservation and
// the request-final sweep (L17/L22/L23 focus).

func newLedgerServiceTestDB(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ledger-service.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, model.MigrateRouteChainLedger(db))
	require.NoError(t, db.AutoMigrate(&model.FinanceRequestEvent{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })

	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "req-ledger-svc")
	return c
}

func seedLedgerServiceAttempt(t *testing.T, c *gin.Context, attemptId string, terminalState string, costState string) {
	t.Helper()
	require.NoError(t, model.CreateRouteChainRequestRecord(&model.RouteChainRequestRecord{
		RequestId: "req-ledger-svc", RouteChainId: 1, RouteChainVersion: 1,
		RequestedModel: "m", CanonicalModel: "m", Group: "default",
		StartedAt: common.GetTimestamp(), UserBillingState: model.RouteChainBillingPending,
	}))
	require.NoError(t, model.CreateRouteChainAttempt(&model.RouteChainAttemptRecord{
		AttemptId: attemptId, RequestId: "req-ledger-svc", Sequence: 1,
		CandidateId: 5, CandidateType: model.RouteChainCandidateTypeAPIChannel, ChannelId: 3,
		StartedAt: common.GetTimestamp(), DispatchState: model.RouteChainPhaseDispatched,
		UsageState: model.RouteChainUsagePending, ProviderCostState: costState,
	}))
	c.Set(AttemptIDKey, attemptId)
	c.Set(ledgerRowKey, "req-ledger-svc")
	if terminalState != "" {
		won, err := model.FinalizeRouteChainAttempt(attemptId, model.RouteChainAttemptTerminal{
			TerminalState: terminalState, FinalOutcome: "authoritative_rejection_no_work", ProviderCostState: costState,
		})
		require.NoError(t, err)
		require.True(t, won)
	}
}

// A finance usage fact with a resolvable estimate upgrades the provider cost
// to KNOWN with the CNY-normalized amount; a duplicate final usage call is a
// counted no-op.
func TestNoteAttemptUsageKnownCostAndDuplicateFinal(t *testing.T) {
	t.Cleanup(common.InitFinanceConfig)
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "1")
	common.InitFinanceConfig()
	c := newLedgerServiceTestDB(t)
	seedLedgerServiceAttempt(t, c, "att-svc-known", "", model.RouteChainCostNone)
	// The finance usage fact BeginFinanceAttempt would have written for this
	// attempt (attempt_id = canonical id by adoption).
	estimatePayload := `{"quantities":{"input":"100","output":"30"},"source":"upstream_usage_scalar_presence_limited","estimate":{"amount":"0.05","status":"known"},"estimated_cost_cny":"0.36"}`
	require.NoError(t, model.DB.Create(&model.FinanceRequestEvent{
		EventKey: "evt-svc-known", RequestID: "req-ledger-svc", AttemptID: "att-svc-known",
		Phase: "usage", UserID: 1, ChannelID: 3, UpstreamModel: "m", At: common.GetTimestamp(), Payload: estimatePayload,
	}).Error)

	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 30, TotalTokens: 130}
	NoteAttemptUsage(c, usage, "safe-sha256-ref", "exec-attempt-9")

	var attempt model.RouteChainAttemptRecord
	require.NoError(t, model.DB.Where("attempt_id = ?", "att-svc-known").First(&attempt).Error)
	assert.Equal(t, model.RouteChainUsageObserved, attempt.UsageState)
	assert.Equal(t, int64(100), attempt.InputTokens)
	assert.Equal(t, int64(130), attempt.TotalTokens)
	assert.Equal(t, model.RouteChainCostKnown, attempt.ProviderCostState)
	assert.Equal(t, "0.36", attempt.ProviderCostAmount)
	assert.Equal(t, "CNY", attempt.ProviderCostCurrency)
	assert.Equal(t, "safe-sha256-ref", attempt.SafeAccountRef)
	assert.Equal(t, "exec-attempt-9", attempt.UpstreamAttemptRef)

	// Duplicate final (L17): second authoritative usage is a counted no-op.
	NoteAttemptUsage(c, &dto.Usage{PromptTokens: 999, CompletionTokens: 999, TotalTokens: 999}, "", "")
	require.NoError(t, model.DB.Where("attempt_id = ?", "att-svc-known").First(&attempt).Error)
	assert.Equal(t, int64(100), attempt.InputTokens, "duplicate final must not accumulate usage")
	assert.Equal(t, "safe-sha256-ref", attempt.SafeAccountRef, "duplicate final must not overwrite the pool linkage")
}

// A zero-work terminal cost classification survives the unavailable usage
// backfill — usage unavailability never weakens ZERO_CONFIRMED (L22), and
// absent usage is an explicit state, never an exact zero (L23).
func TestNoteAttemptUsageUnavailablePreservesZeroWorkCost(t *testing.T) {
	t.Cleanup(common.InitFinanceConfig)
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "1")
	common.InitFinanceConfig()
	c := newLedgerServiceTestDB(t)
	seedLedgerServiceAttempt(t, c, "att-svc-zero", model.RouteChainTerminalAuthoritativeRej, model.RouteChainCostZeroConfirmed)

	NoteAttemptUsage(c, nil, "", "")

	var attempt model.RouteChainAttemptRecord
	require.NoError(t, model.DB.Where("attempt_id = ?", "att-svc-zero").First(&attempt).Error)
	assert.Equal(t, model.RouteChainUsageUnavailable, attempt.UsageState)
	assert.Equal(t, model.RouteChainCostZeroConfirmed, attempt.ProviderCostState)
	assert.Zero(t, attempt.InputTokens)
	assert.Zero(t, attempt.TotalTokens)
}

// FinishRequestLedger closes the request record exactly once and sweeps
// pending usage to explicit unavailable; a legacy request without a ledger
// row is a free no-op.
func TestFinishRequestLedgerSweepAndLegacyNoop(t *testing.T) {
	t.Cleanup(common.InitFinanceConfig)
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "false")
	common.InitFinanceConfig()
	c := newLedgerServiceTestDB(t)
	seedLedgerServiceAttempt(t, c, "att-svc-sweep", model.RouteChainTerminalOutcomeUncertain, model.RouteChainCostUnknownReview)

	FinishRequestLedger(c, nil, nil, nil)
	var request model.RouteChainRequestRecord
	require.NoError(t, model.DB.Where("request_id = ?", "req-ledger-svc").First(&request).Error)
	assert.Equal(t, model.RouteChainRequestFinalFailed, request.FinalStatus)
	var attempt model.RouteChainAttemptRecord
	require.NoError(t, model.DB.Where("attempt_id = ?", "att-svc-sweep").First(&attempt).Error)
	assert.Equal(t, model.RouteChainUsageUnavailable, attempt.UsageState)
	assert.Equal(t, model.RouteChainCostUnknownReview, attempt.ProviderCostState)

	// Idempotent: a second finalize is a no-op.
	FinishRequestLedger(c, nil, nil, nil)
	require.NoError(t, model.DB.Where("request_id = ?", "req-ledger-svc").First(&request).Error)
	assert.Equal(t, model.RouteChainRequestFinalFailed, request.FinalStatus)

	// Legacy request (no ledger row): no panic, no writes.
	legacyC, _ := gin.CreateTestContext(nil)
	legacyC.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	FinishRequestLedger(legacyC, nil, nil, nil)
	NoteAttemptUsage(legacyC, nil, "", "")
	NoteRequestBilling(legacyC, model.RouteChainBillingSettled)
}
