package helper

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// RuntimePricingDiagnostic reads the same billing key and getters as ModelPriceHelper.
// It does not create a relay, reserve quota, or query the database.
func RuntimePricingDiagnostic() (any, error) {
	const model = "gpt-5.6-luna"
	key := resolveBillingModelName(model)
	ratio, configured, ratioKey := ratio_setting.GetModelRatio(key)
	price, pricePresent := ratio_setting.GetModelPrice(key, false)
	cache, cachePresent := ratio_setting.GetCacheRatio(key)
	group := ratio_setting.GetGroupRatio("default")
	override, overridePresent := ratio_setting.GetGroupGroupRatio("default", "default")
	if overridePresent {
		group = override
	}
	_, expressionPresent := billing_setting.GetBillingExpr(key)
	common.OptionMapRWMutex.Lock()
	retries := common.RetryTimes
	common.OptionMapRWMutex.Unlock()
	var priceValue *float64
	if pricePresent {
		priceValue = &price
	}
	return struct {
		Model                    string   `json:"model"`
		PricingKey               string   `json:"canonical_pricing_key"`
		RatioKey                 string   `json:"model_ratio_key"`
		ModelRatio               float64  `json:"model_ratio"`
		ModelRatioConfigured     bool     `json:"model_ratio_configured"`
		CompletionRatio          float64  `json:"completion_ratio"`
		CacheRatio               float64  `json:"cache_ratio"`
		CacheRatioPresent        bool     `json:"cache_ratio_present"`
		ModelPricePresent        bool     `json:"model_price_present"`
		ModelPrice               *float64 `json:"model_price"`
		GroupRatio               float64  `json:"group_ratio"`
		GroupOverridePresent     bool     `json:"group_override_present"`
		BillingMode              string   `json:"billing_mode"`
		BillingExpressionPresent bool     `json:"billing_expression_present"`
		RelayRetryTimes          int      `json:"relay_retry_times"`
	}{model, key, ratioKey, ratio, configured, ratio_setting.GetCompletionRatio(key), cache, cachePresent, pricePresent, priceValue, group, overridePresent, billing_setting.GetBillingMode(key), expressionPresent, retries}, nil
}
