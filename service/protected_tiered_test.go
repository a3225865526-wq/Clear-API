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
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var protectedEncoderInit sync.Once

func protectedLunaPolicy(t *testing.T, change func(map[string]any)) {
	t.Helper()
	protectedEncoderInit.Do(service.InitTokenEncoders)
	t.Setenv("CLEARAPI_BILLING_PROTECTION", "true")
	old := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = old })
	profile := coldProfile(4480)
	profile["model_context_max"], profile["model_output_max"], profile["max_request_quota"] = 16384, 1024, 1000
	if change != nil {
		change(profile)
	}
	setPolicy(t, operation_setting.ReserveModeEnforce, profilesJSON(t, map[string]map[string]any{
		"channel::1::model::gpt-6-luna": profile,
	}))
	configureTieredModel(t, h2ModelA, service.ProtectedLunaCorrectedExpr)
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
	require.True(t, info.ProtectedTieredBudget)
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

func TestProtectedTieredGates(t *testing.T) {
	protectedLunaPolicy(t, nil)
	c, info := protectedLunaRequest(t, h2OutboundBodyA)
	_, err := helper.ModelPriceHelper(c, info, 10, info.Request.GetTokenCountMeta())
	require.NoError(t, err)
	info.InitChannelMeta(c)
	require.NoError(t, helper.ValidateProtectedTextBody(info, []byte(h2OutboundBodyA)))
	for _, body := range []string{
		`{"model":"gpt-6-luna","messages":[{"role":"user","content":"hi"}],"max_tokens":30,"tools":[{"type":"function"}]}`,
		`{"model":"gpt-6-luna","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"synthetic"}}]}],"max_tokens":30}`,
		`{"model":"gpt-6-luna","messages":[{"role":"user","content":"hi"}],"max_tokens":30,"unknown_fee":1}`,
		`{"model":"gpt-6-luna","messages":[{"role":"user","content":"hi"}],"max_tokens":30,"n":2}`,
		`{"model":"gpt-6-luna","messages":[{"role":"user","content":"hi"}]}`,
	} {
		require.Error(t, helper.ValidateProtectedTextBody(info, []byte(body)))
	}
	info.ChannelSetting.PassThroughBodyEnabled = true
	require.Error(t, helper.ValidateProtectedTextChannel(info))
	info.ChannelSetting.PassThroughBodyEnabled = false
	info.ParamOverride = map[string]any{"max_tokens": 999}
	require.Error(t, helper.ValidateProtectedTextChannel(info))
	info.ParamOverride = nil
	for _, mutate := range []func(*relaycommon.RelayInfo){
		func(i *relaycommon.RelayInfo) { i.ChannelId = 2 },
		func(i *relaycommon.RelayInfo) { i.UsingGroup = "vip" },
		func(i *relaycommon.RelayInfo) { i.RelayMode = relayconstant.RelayModeCompletions },
		func(i *relaycommon.RelayInfo) { i.TieredBillingSnapshot.QuotaPerUnit = 1000000 },
		func(i *relaycommon.RelayInfo) { i.TieredBillingSnapshot.ExprString = "p * 0.02" },
	} {
		copyInfo := *info
		copyMeta := *info.ChannelMeta
		copySnap := *info.TieredBillingSnapshot
		copyInfo.ChannelMeta, copyInfo.TieredBillingSnapshot = &copyMeta, &copySnap
		mutate(&copyInfo)
		require.Error(t, service.ValidateProtectedTieredSnapshot(&copyInfo))
	}
	_, err = service.EstimateDispatchReservation(info, 16384-4480+1, 30, 1, 0)
	require.Error(t, err, "context includes hidden input, independent of client output limit")
	_, err = service.EstimateDispatchReservation(info, 10, 1025, 1, 0)
	require.Error(t, err)
	t.Run("policy-cannot-be-disabled-after-admission", func(t *testing.T) {
		setPolicy(t, operation_setting.ReserveModeOff, "{}")
		require.Error(t, helper.ValidateProtectedTextChannel(info))
	})
	t.Run("cap-before-wallet-or-send", func(t *testing.T) {
		protectedLunaPolicy(t, func(p map[string]any) { p["max_request_quota"] = 1 })
		c, info := protectedLunaRequest(t, h2OutboundBodyA)
		_, err := helper.ModelPriceHelper(c, info, 10, info.Request.GetTokenCountMeta())
		require.Error(t, err)
		require.Nil(t, info.Billing)
		require.False(t, c.GetBool("upstream_attempted"))
	})
}

func TestProtectedTieredPricesAndBound(t *testing.T) {
	protectedLunaPolicy(t, nil)
	info := estimateRelayInfo(1, h2ModelA, service.ProtectedLunaCorrectedExpr, 1)
	info.UserGroup, info.UsingGroup, info.TokenGroup = "default", "default", "default"
	info.ChannelType = constant.ChannelTypeOpenAI
	info.RelayMode, info.RelayFormat = relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI
	for _, tc := range []struct {
		tokens billingexpr.TokenParams
		cost   float64
		tier   string
	}{
		{billingexpr.TokenParams{P: 551, CR: 3840, C: 6, Len: 4391}, 19.3, "upstream_1"},
		{billingexpr.TokenParams{P: 551, CR: 3840, C: 5, Len: 4391}, 19.2, "upstream_1"},
		{billingexpr.TokenParams{CR: 100000, Len: 272000}, 200, "upstream_1"},
		{billingexpr.TokenParams{CR: 100000, Len: 272001}, 400, "upstream_2"},
		{billingexpr.TokenParams{CC: 100000, Len: 272000}, 2500, "upstream_1"},
		{billingexpr.TokenParams{CC: 100000, Len: 272001}, 5000, "upstream_2"},
		{billingexpr.TokenParams{CC1h: 100000, Len: 272000}, 2500, "upstream_1"},
		{billingexpr.TokenParams{CC1h: 100000, Len: 272001}, 5000, "upstream_2"},
	} {
		result, err := billingexpr.ComputeTieredQuota(info.TieredBillingSnapshot, tc.tokens)
		require.NoError(t, err)
		require.InDelta(t, tc.cost*0.5, result.ActualQuotaBeforeGroup, 1e-9)
		require.Equal(t, tc.tier, result.MatchedTier)
	}
	oldSnap := *info.TieredBillingSnapshot
	oldSnap.ExprString = service.ProtectedLunaCurrentExpr
	oldSnap.ExprHash = billingexpr.ExprHashString(oldSnap.ExprString)
	for _, output := range []float64{6, 5} {
		result, err := billingexpr.ComputeTieredQuota(&oldSnap, billingexpr.TokenParams{P: 551, CR: 3840, C: output, Len: 4391})
		require.NoError(t, err)
		require.Equal(t, 10, result.ActualQuotaAfterGroup, "historical tariff also remains unchanged below the threshold")
	}
	oldLong, err := billingexpr.ComputeTieredQuota(&oldSnap, billingexpr.TokenParams{CR: 100000, Len: 272001})
	require.NoError(t, err)
	require.Equal(t, 2000, oldLong.ActualQuotaAfterGroup, "production's current cache-read coefficient is ten times the confirmed published rate")
	estimate, err := service.EstimateDispatchReservation(info, 11904, 1024, 1, 0)
	require.NoError(t, err)
	// All cache splits, including visible cache writes and unknown hidden splits,
	// must remain inside the authorization even though production omits them.
	for _, tokens := range []billingexpr.TokenParams{
		{P: 16384, C: 1024, Len: 16384}, {CR: 16384, C: 1024, Len: 16384},
		{CC: 16384, C: 1024, Len: 16384}, {CC1h: 16384, C: 1024, Len: 16384},
	} {
		result, err := billingexpr.ComputeTieredQuota(info.TieredBillingSnapshot, tokens)
		require.NoError(t, err)
		require.LessOrEqual(t, result.ActualQuotaAfterGroup, estimate.RequiredQuota)
	}
	t.Logf("launch envelope context=16384 output=1024 required=%d cap=%d", estimate.RequiredQuota, estimate.CapQuota)
}
