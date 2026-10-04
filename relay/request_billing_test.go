package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type cancellationBilling struct {
	refunded bool
	charged  int
}

func (b *cancellationBilling) Settle(quota int) error   { b.charged = quota; return nil }
func (b *cancellationBilling) Refund(*gin.Context)      { b.refunded = true }
func (b *cancellationBilling) NeedsRefund() bool        { return !b.refunded }
func (b *cancellationBilling) GetPreConsumedQuota() int { return 100 }
func (b *cancellationBilling) Reserve(int) error        { return nil }

// fakeTextBilling records what the settlement decision asked for without
// touching pricing or the database; the durable session path is covered by
// TestProtectedBillingDatabase in the service package.
type fakeTextBilling struct {
	preConsumed int
	settled     []int
	refunds     int
}

func (b *fakeTextBilling) Settle(quota int) error   { b.settled = append(b.settled, quota); return nil }
func (b *fakeTextBilling) Refund(*gin.Context)      { b.refunds++ }
func (b *fakeTextBilling) NeedsRefund() bool        { return b.refunds == 0 }
func (b *fakeTextBilling) GetPreConsumedQuota() int { return b.preConsumed }
func (b *fakeTextBilling) Reserve(int) error        { return nil }

const poolRevokeExactMeter = `{"status":"exact","source":"codex_upstream","final":true,"input_tokens":100,"cached_input_tokens":30,"output_tokens":20,"reasoning_tokens":7,"total_tokens":120,"total_derived":false}`

// poolRevokeError mirrors the proxy's security termination body: an explicit
// dispatch flag plus, when the lease already observed one, the exact receipt.
func poolRevokeError(t *testing.T, dispatched bool, meter string) *types.NewAPIError {
	t.Helper()
	metadata := map[string]any{"codex_upstream_dispatched": dispatched}
	if meter != "" {
		metadata["codex_metering"] = json.RawMessage(meter)
	}
	raw, err := common.Marshal(metadata)
	require.NoError(t, err)
	apiErr := types.NewOpenAIError(errors.New("account_revoked"), types.ErrorCodeBadResponse, http.StatusForbidden)
	apiErr.RelayError = types.OpenAIError{
		Message:  "account_revoked",
		Type:     "security_error",
		Code:     "account_revoked",
		Metadata: raw,
	}
	return apiErr
}

func newPoolRevokeContext(dispatched bool) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("upstream_attempted", dispatched)
	return c
}

// newSettlementDatabase installs an isolated SQLite database for the reconciliation
// path, which writes billing state and a consume log through real SQL.
func newSettlementDatabase(t *testing.T) {
	t.Helper()
	previousDB, previousBatch := model.DB, common.BatchUpdateEnabled
	previousLogDB := model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	previousConsumeLog := common.LogConsumeEnabled
	common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = false, false, false
	gormDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settlement.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	model.DB, model.LOG_DB = gormDB, gormDB
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, gormDB.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.BatchUpdateEnabled = previousBatch
		common.RedisEnabled = previousRedis
		common.LogConsumeEnabled = previousConsumeLog
		common.SetDatabaseTypes(previousMain, previousLog)
		if sqlDB, errDB := gormDB.DB(); errDB == nil {
			_ = sqlDB.Close()
		}
	})
}

// A pre-dispatch revoke produced no upstream request and no authoritative usage,
// so the reservation is released instead of settled from invented tokens.
func TestPoolPreDispatchRevokeRefundsWithoutUsage(t *testing.T) {
	c := newPoolRevokeContext(false)
	billing := &fakeTextBilling{preConsumed: 100}
	RefundFailedRequestBilling(c, &relaycommon.RelayInfo{Billing: billing}, poolRevokeError(t, false, ""))
	assert.Empty(t, billing.settled)
	assert.Equal(t, 1, billing.refunds)
}

// A dispatched revoke without an authoritative receipt must retain the reserved
// charge for reconciliation, and must not treat unknown usage as zero.
func TestPoolPostDispatchRevokeWithoutReceiptRetainsReservation(t *testing.T) {
	newSettlementDatabase(t)
	for _, meter := range []string{"", `{"status":"unavailable","source":"codex_upstream","final":true,"reason":"missing_usage"}`} {
		c := newPoolRevokeContext(true)
		billing := &fakeTextBilling{preConsumed: 100}
		RefundFailedRequestBilling(c, &relaycommon.RelayInfo{Billing: billing}, poolRevokeError(t, true, meter))
		assert.Equal(t, []int{100}, billing.settled)
		assert.Zero(t, billing.refunds)
		assert.True(t, c.GetBool("upstream_usage_uncertain"))
	}
}

func TestCancelledDispatchDoesNotCreateFreeUpstreamCalls(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
		c.Set("upstream_attempted", dispatched)
		billing := &cancellationBilling{}
		info := &relaycommon.RelayInfo{Billing: billing}
		RefundFailedRequestBilling(c, info, types.NewError(errors.New("cancelled fixture"), types.ErrorCodeDoRequestFailed))
		assert.Equal(t, !dispatched, billing.refunded)
		if dispatched {
			assert.Equal(t, 100, billing.charged)
		} else {
			assert.Zero(t, billing.charged)
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("upstream_usage_uncertain", true)
	billing := &cancellationBilling{}
	RefundFailedRequestBilling(c, &relaycommon.RelayInfo{Billing: billing}, types.NewError(errors.New("timeout fixture"), types.ErrorCodeDoRequestFailed))
	assert.False(t, billing.refunded)
	assert.Equal(t, 100, billing.charged)
}
