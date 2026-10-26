package service_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const ProtectedLunaCurrentExpr = `len <= 272000 ? tier("upstream_1", p * 0.02 + c * 0.1 + cr * 0.002 + cc1h * 0.025 + cc * 0.025) : tier("upstream_2", p * 0.04 + c * 0.15 + cc * 0.05 + cc1h * 0.05 + cr * 0.04)`
const ProtectedLunaCorrectedExpr = `len <= 272000 ? tier("upstream_1", p * 0.02 + c * 0.1 + cr * 0.002 + cc1h * 0.025 + cc * 0.025) : tier("upstream_2", p * 0.04 + c * 0.15 + cc * 0.05 + cc1h * 0.05 + cr * 0.004)`

var protectedEncoderInit sync.Once

func protectedLunaPolicy(t *testing.T, change func(map[string]any)) {
	t.Helper()
	protectedEncoderInit.Do(service.InitTokenEncoders)
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	old := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = old })
	configureTieredModel(t, h2ModelA, ProtectedLunaCorrectedExpr)
}

func protectedLunaRequest(t *testing.T, body string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	var request dto.GeneralOpenAIRequest
	require.NoError(t, json.Unmarshal([]byte(body), &request))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	// Exercise production ordering: GenRelayInfo has no ChannelMeta during pricing.
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, h2ModelA)
	info := &relaycommon.RelayInfo{Request: &request, OriginModelName: h2ModelA,
		UserGroup: "default", UsingGroup: "default", TokenGroup: "default",
		RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI}
	info.UserSetting.BillingPreference = "wallet_only"
	info.SetEstimatePromptTokens(10)
	return c, info
}

func protectedLunaFlow(t *testing.T, u hardeningUser) *hardeningFlow {
	t.Helper()
	c, info := protectedLunaRequest(t, h2OutboundBodyA)
	info.UserId, info.TokenId = u.UserID, u.TokenID
	info.RequestId = fmt.Sprintf("protected-%d-%d", u.UserID, h2FlowCounter.Add(1))
	c.Set(common.RequestIdKey, info.RequestId)
	price, err := helper.ModelPriceHelper(c, info, 10, info.Request.GetTokenCountMeta())
	require.NoError(t, err)
	require.Nil(t, info.ChannelMeta, "pricing must preserve the controller's first-attempt channel selection")
	require.Nil(t, service.PreConsumeBilling(c, price.QuotaToPreConsume, info))
	info.InitChannelMeta(c) // TextHelper initializes the selected attempt after wallet admission.
	f := &hardeningFlow{RequestID: info.RequestId, Info: info, Ctx: c, OutboundBody: h2OutboundBodyA, InitialPrompt: 10}
	require.NoError(t, f.dispatch(t))
	return f
}

func TestProtectedTieredReplayAndRetention(t *testing.T) {
	newHardeningDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}))
	syntheticChannel := model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "synthetic", Key: "synthetic-not-a-provider-key"}
	require.NoError(t, model.DB.FirstOrCreate(&syntheticChannel, 1).Error)
	fixtureID := fmt.Sprintf("%x", time.Now().UnixNano())
	protectedLunaPolicy(t, nil)
	for _, output := range []int{6, 5} {
		u := createHardeningUser(t, model.DB, fmt.Sprintf("p%d-%s", output, fixtureID), 10000)
		f := protectedLunaFlow(t, u)
		usage := &dto.Usage{PromptTokens: 4391, CompletionTokens: output, TotalTokens: 4391 + output,
			PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 3840}}
		params := service.BuildTieredTokenParams(usage, false, billingexpr.UsedVarsByHash(f.Info.TieredBillingSnapshot.ExprString, f.Info.TieredBillingSnapshot.ExprHash))
		require.EqualValues(t, 3840, params.CR)
		require.EqualValues(t, 551, params.P)
		require.EqualValues(t, 4391, params.Len)
		reserved := reservationOf(t, f.RequestID).Reserved
		require.LessOrEqual(t, reserved, 1000)
		f.settle(t, usage)
		row := reservationOf(t, f.RequestID)
		require.Equal(t, "settled", row.State)
		require.Equal(t, 10, row.Applied)
		_, reviewed := markerOf(t, f.RequestID)
		require.False(t, reviewed, "cold profile must accept the historical cached input within its hidden envelope")
		t.Logf("historical replay output=%d input=4391 cached=3840 ordinary=551 reserved=%d settled=%d review=%v", output, reserved, row.Applied, reviewed)
		require.Equal(t, walletState{9990, 9990}, walletOf(t, u))
		// Concurrent duplicate completion must have exactly one wallet effect.
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() { errs <- f.Info.Billing.(*service.BillingSession).Settle(10) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		require.Equal(t, walletState{9990, 9990}, walletOf(t, u))
	}
	u := createHardeningUser(t, model.DB, "pc-"+fixtureID, 10000)
	f := protectedLunaFlow(t, u)
	var first sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		first.Go(func() { results <- f.Info.Billing.(*service.BillingSession).Settle(10) })
	}
	first.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	f.Info.Billing.Refund(f.Ctx)
	require.Equal(t, walletState{9990, 9990}, walletOf(t, u))
	require.Equal(t, 10, reservationOf(t, f.RequestID).Applied)
	for _, partial := range []bool{false, true} {
		label := "pf-"
		if partial {
			label = "pt-"
		}
		u := createHardeningUser(t, model.DB, label+fixtureID, 10000)
		f := protectedLunaFlow(t, u)
		f.Info.IsStream = true
		f.Ctx.Set("upstream_attempted", true)
		if partial {
			service.ObserveTokenUsage(f.Ctx, "openai", []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":0,"total_tokens":100}}`))
		}
		service.MarkTokenInterrupted(f.Ctx)
		reserved := reservationOf(t, f.RequestID).Reserved
		f.settle(t, &dto.Usage{})
		f.Info.Billing.Refund(f.Ctx)
		require.Equal(t, reserved, reservationOf(t, f.RequestID).Applied)
		marker, ok := markerOf(t, f.RequestID)
		require.True(t, ok)
		require.Equal(t, "interrupted_unknown_usage", marker.Reason)
		require.Equal(t, walletState{10000 - reserved, 10000 - reserved}, walletOf(t, u))
	}
}
