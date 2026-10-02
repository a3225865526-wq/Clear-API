package relay

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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
