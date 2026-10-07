package service

import (
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type stage72Funding struct {
	settlements, refunds atomic.Int32
	delta                atomic.Int64
}

func (*stage72Funding) Source() string       { return "synthetic" }
func (*stage72Funding) PreConsume(int) error { return nil }
func (f *stage72Funding) Settle(delta int) error {
	f.settlements.Add(1)
	f.delta.Add(int64(delta))
	return nil
}
func (f *stage72Funding) Refund() error { f.refunds.Add(1); return nil }

func TestStage72LegacyFinalizationOnce(t *testing.T) {
	for _, refund := range []bool{true, false} {
		funding := &stage72Funding{}
		info := &relaycommon.RelayInfo{IsPlayground: true}
		session := &BillingSession{relayInfo: info, funding: funding, preConsumedQuota: 100, tokenConsumed: 100}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				if refund {
					session.Refund(ctx)
				} else {
					if err := session.Settle(20); err != nil {
						t.Error(err)
					}
				}
			})
		}
		wg.Wait()
		if refund {
			if funding.refunds.Load() != 1 || funding.settlements.Load() != 0 || session.NeedsRefund() {
				t.Fatal("duplicate refund")
			}
		} else {
			session.Refund(ctx)
			if funding.settlements.Load() != 1 || funding.refunds.Load() != 0 || funding.delta.Load() != -80 || session.ChargedQuota() != 20 {
				t.Fatal("duplicate settlement or refund after success")
			}
		}
	}
}
