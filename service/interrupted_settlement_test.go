package service_test

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// R7 (Final Launch Gate §3): an interrupted stream without upstream exact
// usage has unknowable hidden usage. Settlement must fail closed — retain the
// approved reservation and park a review marker (Codex SettleUncertain
// semantics) — instead of silently undercharging on a local estimate or a
// partial upstream frame. One settlement-side hook covers every text-stream
// provider; these tests pin the OpenAI-format flow.
// ---------------------------------------------------------------------------

func markerOf(t *testing.T, requestID string) (model.BillingReviewMarker, bool) {
	t.Helper()
	var row model.BillingReviewMarker
	var reservation model.BillingReservation
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&reservation).Error)
	err := model.DB.Where("reservation_id = ?", reservation.ID).First(&row).Error
	if err != nil {
		return model.BillingReviewMarker{}, false
	}
	return row, true
}

// TestInterruptedStreamLocalEstimateRetainsReservation: interrupted stream,
// no upstream usage frame at all, settlement would use the local estimate —
// the reservation must be retained at its full approved amount and a review
// marker persisted instead of charging the low estimate.
func TestInterruptedStreamLocalEstimateRetainsReservation(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "interrupted-estimate", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	flow.Info.IsStream = true
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved
	require.Greater(t, reserved, 10)

	// The relay handlers flag the observation before settlement.
	service.MarkTokenInterrupted(flow.Ctx)
	service.MarkTokenEstimate(flow.Ctx)

	// The undercharged path: a tiny local estimate for a much larger
	// pre-authorized budget.
	flow.settle(t, &dto.Usage{PromptTokens: flow.InitialPrompt, CompletionTokens: 2, TotalTokens: flow.InitialPrompt + 2})

	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Equal(t, reserved, row.Applied, "retained settlement charges the full reservation, not the estimate")
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-reserved, wallet.UserQuota)
	marker, ok := markerOf(t, flow.RequestID)
	require.True(t, ok, "interrupted unknown usage must leave a review marker")
	assert.Equal(t, "interrupted_unknown_usage", marker.Reason)
}

// TestInterruptedStreamUpstreamPartialRetainsReservation: interrupted stream
// with a partial (non-final) upstream usage frame — still unknown hidden
// usage, still fail-closed retention.
func TestInterruptedStreamUpstreamPartialRetainsReservation(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "interrupted-partial", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	flow.Info.IsStream = true
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved

	// A mid-stream upstream usage frame arrives, then the stream is
	// interrupted: observation quality becomes upstream_partial.
	const partialFrame = `{"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105}}`
	service.ObserveTokenUsage(flow.Ctx, "openai", []byte(partialFrame))
	service.MarkTokenInterrupted(flow.Ctx)

	flow.settle(t, &dto.Usage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105})

	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Equal(t, reserved, row.Applied)
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-reserved, wallet.UserQuota)
	marker, ok := markerOf(t, flow.RequestID)
	require.True(t, ok)
	assert.Equal(t, "interrupted_unknown_usage", marker.Reason)
}

// TestInterruptedStreamExactMeterSettlesExactly: interruption with upstream
// exact metering settles the exact amount — retention must not overcharge.
func TestInterruptedStreamExactMeterSettlesExactly(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "interrupted-exact", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	flow.Info.IsStream = true
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved

	service.MarkTokenInterrupted(flow.Ctx)
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	usage.CodexMetering = dto.ParseCodexMetering([]byte(`{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":7}}`))
	require.NotNil(t, usage.CodexMetering)
	assert.Equal(t, "exact", usage.CodexMetering.Status)

	flow.settle(t, usage)

	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Less(t, row.Applied, reserved, "exact metering must not be retained at the reserved amount")
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-row.Applied, wallet.UserQuota)
	_, ok := markerOf(t, flow.RequestID)
	assert.False(t, ok, "exact settlement must not leave an interrupted-usage marker")
}

// TestCompletedStreamEstimateSettlesEstimate: negative control — the same
// local-estimate settlement without the interrupted flag keeps ordinary
// settlement semantics (no retention, no marker).
func TestCompletedStreamEstimateSettlesEstimate(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)
	enforceColdPolicy(t, nil)
	u := createHardeningUser(t, model.DB, "completed-estimate", 10000)

	flow := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	flow.Info.IsStream = true
	require.NoError(t, flow.dispatch(t))
	reserved := reservationOf(t, flow.RequestID).Reserved

	service.MarkTokenEstimate(flow.Ctx)
	flow.settle(t, &dto.Usage{PromptTokens: flow.InitialPrompt, CompletionTokens: 2, TotalTokens: flow.InitialPrompt + 2})

	row := reservationOf(t, flow.RequestID)
	assert.Equal(t, "settled", row.State)
	assert.Less(t, row.Applied, reserved)
	wallet := walletOf(t, u)
	assert.Equal(t, u.Balance-row.Applied, wallet.UserQuota)
	_, ok := markerOf(t, flow.RequestID)
	assert.False(t, ok)
}
