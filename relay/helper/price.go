package helper

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func modelPriceNotConfiguredError(modelName string, userId int) error {
	if model.IsAdmin(userId) {
		return fmt.Errorf(
			"模型 %s 的价格未配置。请前往「系统设置 → 运营设置」开启自用模式，或在「系统设置 → 分组与模型定价设置」中为该模型配置价格；"+
				"Model %s price not configured. Go to System Settings → Operation Settings to enable self-use mode, or configure the model price in System Settings → Group & Model Pricing.",
			modelName, modelName,
		)
	}
	return fmt.Errorf(
		"模型 %s 的价格尚未由管理员配置，暂时无法使用，请联系站点管理员开启该模型；"+
			"Model %s has not been priced by the administrator yet. Please contact the site administrator to enable this model.",
		modelName, modelName,
	)
}

// https://docs.claude.com/en/docs/build-with-claude/prompt-caching#1-hour-cache-duration
const claudeCacheCreation1hMultiplier = 6 / 3.75

// HandleGroupRatio checks for "auto_group" in the context and updates the group ratio and relayInfo.UsingGroup if present
func HandleGroupRatio(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) hosttypes.GroupRatioInfo {
	groupRatioInfo := hosttypes.GroupRatioInfo{
		GroupRatio:        1.0, // default ratio
		GroupSpecialRatio: -1,
	}

	// check auto group
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, "final group: %s", autoGroup)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	// check user group special ratio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		// user group special ratio
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		// normal group ratio
		groupRatioInfo.GroupRatio = ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	}

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (hosttypes.PriceData, error) {
	protectedOutput := 0
	if billingProtectionEnabled() {
		var err error
		protectedOutput, err = protectedTextLimit(info)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
	}
	if info != nil {
		if matched := resolveBillingModelName(info.GetOriginModelName()); matched != "" && matched != info.OriginModelName {
			info.BillingModelName = matched
		}
	}
	billingModelName := info.GetBillingModelName()
	modelPrice, usePrice := ratio_setting.GetModelPrice(billingModelName, false)

	groupRatioInfo := HandleGroupRatio(c, info)

	// Check if this model uses tiered_expr billing
	if billing_setting.GetBillingMode(billingModelName) == billing_setting.BillingModeTieredExpr {
		if billingProtectionEnabled() {
			return hosttypes.PriceData{}, fmt.Errorf("protected billing has no proven upper bound for expression-priced models")
		}
		return modelPriceHelperTiered(c, info, billingModelName, promptTokens, meta, groupRatioInfo)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	if !usePrice {
		preConsumeMultiplier, err := operation_setting.InputPreConsumeMultiplier()
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedTokens := float64(promptTokens) * preConsumeMultiplier
		var success bool
		var matchName string
		modelRatio, success, matchName = ratio_setting.GetModelRatio(billingModelName)
		if !success {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
		completionRatio = ratio_setting.GetCompletionRatio(billingModelName)
		cacheRatio, _ = ratio_setting.GetCacheRatio(billingModelName)
		cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(billingModelName)
		cacheCreationRatio5m = cacheCreationRatio
		// 固定1h和5min缓存写入价格的比例
		cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier
		imageRatio, _ = ratio_setting.GetImageRatio(billingModelName)
		audioRatio = ratio_setting.GetAudioRatio(billingModelName)
		audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(billingModelName)
		ratio := modelRatio * groupRatioInfo.GroupRatio
		output, err := requestOutputBudget(info, meta)
		if err != nil && ratio != 0 {
			return hosttypes.PriceData{}, err
		}
		inputCategoryRatio := max(1.0, cacheRatio, cacheCreationRatio5m, cacheCreationRatio1h, imageRatio, audioRatio)
		inputBudget := max(preConsumedTokens, float64(promptTokens)*inputCategoryRatio)
		outputRatio := max(completionRatio, audioRatio*audioCompletionRatio)
		quota, err := common.QuotaFromFloatStrict(math.Ceil((inputBudget + output*outputRatio) * ratio))
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedQuota = quota
		if _, image := info.Request.(*dto.ImageRequest); image {
			info.ImageQuotaBeforeGroup = preConsumedTokens * modelRatio
		}
	} else {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
		if _, image := info.Request.(*dto.ImageRequest); image {
			info.ImageQuotaBeforeGroup = modelPrice * common.QuotaPerUnit
		}
	}

	// check if free model pre-consume is disabled
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		// if model price or ratio is 0, do not pre-consume quota
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		} else if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else {
			if modelRatio == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		}
	}

	priceData := hosttypes.PriceData{
		FreeModel:            freeModel,
		ModelPrice:           modelPrice,
		ModelRatio:           modelRatio,
		CompletionRatio:      completionRatio,
		GroupRatioInfo:       groupRatioInfo,
		UsePrice:             usePrice,
		CacheRatio:           cacheRatio,
		ImageRatio:           imageRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		CacheCreationRatio:   cacheCreationRatio,
		CacheCreation5mRatio: cacheCreationRatio5m,
		CacheCreation1hRatio: cacheCreationRatio1h,
		QuotaToPreConsume:    preConsumedQuota,
	}
	if usePrice {
		for name, ratio := range meta.BillingRatios {
			priceData.AddOtherRatio(name, ratio)
		}
	}
	if request, image := info.Request.(*dto.ImageRequest); image {
		count, err := request.ImageCount(false)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if usePrice {
			priceData.AddOtherRatio("n", float64(count))
		}
		if !usePrice {
			quota, err := common.QuotaFromFloatStrict(priceData.ApplyOtherRatiosToFloat(info.ImageQuotaBeforeGroup * groupRatioInfo.GroupRatio))
			if err != nil {
				return hosttypes.PriceData{}, err
			}
			priceData.QuotaToPreConsume = quota
		}
	}
	if usePrice {
		quotaToPreConsume := priceData.ApplyOtherRatiosToFloat(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		quota, err := common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
	}

	if billingProtectionEnabled() {
		if err := protectTextReservation(info, &priceData, promptTokens, protectedOutput); err != nil {
			return hosttypes.PriceData{}, err
		}
	}
	toolQuota, err := requestToolBudget(info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if toolQuota > 0 {
		quota, err := common.QuotaFromFloatStrict(math.Ceil(float64(priceData.QuotaToPreConsume) + toolQuota*priceData.GroupRatioInfo.GroupRatio))
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
		priceData.FreeModel = false
	}
	if common.DebugEnabled {
		logger.LogDebug(c, "model_price_helper result: %s", priceData.ToSetting())
	}
	info.PriceData = priceData
	return priceData, nil
}

// ModelPriceHelperPerCall 按次/按量计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)

	modelPrice, success := ratio_setting.GetModelPrice(info.OriginModelName, true)
	usePrice := success
	var modelRatio float64

	if !success {
		defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
		if ok {
			modelPrice = defaultPrice
			usePrice = true
		} else {
			var ratioSuccess bool
			var matchName string
			modelRatio, ratioSuccess, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !ratioSuccess && !acceptUnsetRatio {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
	}

	var quota int
	freeModel := false

	if usePrice {
		var err error
		quota, err = common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelPrice == 0 {
				quota = 0
				freeModel = true
			}
		}
	} else {
		// 按量计费：以模型倍率的一半作为预扣额度
		var err error
		quota, err = common.QuotaFromFloatStrict(modelRatio / 2 * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		modelPrice = -1
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelRatio == 0 {
				quota = 0
				freeModel = true
			}
		}
	}

	priceData := hosttypes.PriceData{
		FreeModel:      freeModel,
		ModelPrice:     modelPrice,
		ModelRatio:     modelRatio,
		UsePrice:       usePrice,
		Quota:          quota,
		GroupRatioInfo: groupRatioInfo,
	}
	return priceData, nil
}

func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expr, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expr) != ""
}

// HasPriceOrRatioEntry reports whether name has a configured price, ratio, or
// tiered billing-mode entry after a single wildcard normalization. Self-use
// fallback does not count as a configured ratio.
func HasPriceOrRatioEntry(name string) bool {
	formatted := ratio_setting.FormatMatchingModelName(name)
	if _, ok := ratio_setting.GetModelPrice(formatted, false); ok {
		return true
	}
	if ratio_setting.HasConfiguredModelRatio(formatted) {
		return true
	}
	return billing_setting.GetBillingMode(formatted) == billing_setting.BillingModeTieredExpr
}

func resolveBillingModelName(origin string) string {
	var candidates []string
	if !reasoning.ParseModelModifiers(origin).HasModifiers() {
		candidates = append(candidates, origin)
	}
	candidates = append(candidates, hostreasoning.CanonicalBillingModelNames(origin)...)
	base := hostreasoning.BaseModelName(origin)
	candidates = append(candidates, base)

	seen := make(map[string]struct{}, len(candidates))
	matched := ""
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if HasPriceOrRatioEntry(name) {
			matched = name
			break
		}
	}
	if matched == "" {
		matched = base
	}
	return matched
}

func modelPriceHelperTiered(c *gin.Context, info *relaycommon.RelayInfo, billingModelName string, promptTokens int, meta *types.TokenCountMeta, groupRatioInfo hosttypes.GroupRatioInfo) (hosttypes.PriceData, error) {
	exprStr, ok := billing_setting.GetBillingExpr(billingModelName)
	if !ok {
		return hosttypes.PriceData{}, fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", billingModelName)
	}
	exprHash := billingexpr.ExprHashString(exprStr)
	if info.RelayFormat == types.RelayFormatOpenAIRealtime && billingexpr.UsesFixedPricingByHash(exprStr, exprHash) {
		return hosttypes.PriceData{}, fmt.Errorf("fixed pricing is not supported for Realtime requests")
	}

	preConsumeMultiplier, err := operation_setting.InputPreConsumeMultiplier()
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if billingexpr.UsedVarsByHash(exprStr, exprHash)["image_count"] {
		requestInput, err = ResolveImageBillingRequestInput(c, info, requestInput)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
	}

	rawCost, trace, err := billingexpr.RunExprByHashWithRequest(exprStr, exprHash, billingexpr.TokenParams{
		P:   float64(promptTokens),
		C:   0,
		Len: float64(promptTokens),
	}, requestInput)
	if err != nil {
		return hosttypes.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", billingModelName, err)
	}

	// Expression coefficients are $/1M tokens prices; convert to quota the same way per-call billing does.
	quotaBeforeGroup := rawCost / 1_000_000 * common.QuotaPerUnit
	// Scale the reservation, preserving the expression's context-length tier
	// and leaving actual settlement and fixed request prices unchanged.
	if trace.BillingUnit != billingexpr.BillingUnitRequest {
		quotaBeforeGroup *= preConsumeMultiplier
	}
	output, budgetErr := requestOutputBudget(info, meta)
	usedVars := billingexpr.UsedVarsByHash(exprStr, exprHash)
	if budgetErr != nil && groupRatioInfo.GroupRatio != 0 && (usedVars["c"] || usedVars["ao"] || usedVars["img_o"]) {
		return hosttypes.PriceData{}, budgetErr
	}
	imageCount := 1
	if requestInput.ImageCount != nil {
		imageCount = *requestInput.ImageCount
	}
	upper, err := billingexpr.ReservationUpperBound(exprStr, float64(promptTokens)*max(1.0, preConsumeMultiplier), output, imageCount)
	if err != nil && groupRatioInfo.GroupRatio != 0 {
		return hosttypes.PriceData{}, err
	}
	quotaBeforeGroup = max(quotaBeforeGroup, upper/1_000_000*common.QuotaPerUnit)
	toolQuota, err := requestToolBudget(info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	quotaBeforeGroup += toolQuota
	preConsumedQuota, err := billingexpr.QuotaRoundStrict(quotaBeforeGroup * groupRatioInfo.GroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		}
	}

	snapshot := &billingexpr.BillingSnapshot{
		EstimatedImageCount:       trace.ImageCount,
		BillingMode:               billing_setting.BillingModeTieredExpr,
		ModelName:                 billingModelName,
		ExprString:                exprStr,
		ExprHash:                  exprHash,
		GroupRatio:                groupRatioInfo.GroupRatio,
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: 0,
		PreConsumeMultiplier:      preConsumeMultiplier,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup,
		EstimatedQuotaAfterGroup:  preConsumedQuota,
		EstimatedTier:             trace.MatchedTier,
		EstimatedBillingUnit:      trace.BillingUnit,
		EstimatedFixedPrice:       trace.FixedPrice,
		QuotaPerUnit:              common.QuotaPerUnit,
		ExprVersion:               billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := hosttypes.PriceData{
		FreeModel:         freeModel,
		GroupRatioInfo:    groupRatioInfo,
		QuotaToPreConsume: preConsumedQuota,
	}

	logger.LogDebug(c, "model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeGroup=%.2f groupRatio=%.2f tier=%s", billingModelName, preConsumedQuota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier)

	info.PriceData = priceData
	return priceData, nil
}

// requestOutputBudget retains multi-output requests, reserving all candidates.
// An omitted cap on a paid generation request has no enforceable cost bound.
func requestOutputBudget(info *relaycommon.RelayInfo, meta *types.TokenCountMeta) (float64, error) {
	if info == nil || meta == nil {
		return 0, fmt.Errorf("request billing metadata is missing")
	}
	count := 1
	generation := false
	switch request := info.Request.(type) {
	case *dto.GeneralOpenAIRequest:
		generation = info.RelayMode == relayconstant.RelayModeChatCompletions || info.RelayMode == relayconstant.RelayModeCompletions
		if request.N != nil {
			count = *request.N
		}
	case *dto.ClaudeRequest, *dto.OpenAIResponsesRequest:
		generation = true
	case *dto.GeminiChatRequest:
		generation = !strings.Contains(info.RequestURLPath, ":embedContent") && !strings.Contains(info.RequestURLPath, ":batchEmbedContents")
		if !generation {
			return 0, nil
		}
		if len(request.Requests) > 0 {
			if !constant.CountToken || len(request.Requests) > dto.MaxImageN || len(request.Contents) > 0 {
				return 0, fmt.Errorf("invalid Gemini batch billing budget")
			}
			total := 0.0
			for i := range request.Requests {
				child := request.Requests[i]
				if len(child.Requests) > 0 {
					return 0, fmt.Errorf("nested Gemini batches are not supported")
				}
				copyInfo := *info
				copyInfo.Request = &child
				budget, err := requestOutputBudget(&copyInfo, child.GetTokenCountMeta())
				if err != nil {
					return 0, err
				}
				total += budget
			}
			return total, nil
		}
		if request.GenerationConfig.CandidateCount != nil {
			count = *request.GenerationConfig.CandidateCount
		}
	}
	if count < 1 || count > dto.MaxImageN {
		return 0, fmt.Errorf("output candidate count is out of range")
	}
	if !generation {
		return 0, nil
	}
	if generation && (!constant.CountToken || meta.MaxTokens <= 0 || meta.MaxTokens > maxTokensLimit) {
		return 0, fmt.Errorf("paid generation requires token counting and an explicit bounded output limit")
	}
	return float64(max(0, meta.MaxTokens)) * float64(count), nil
}

func requestToolBudget(info *relaycommon.RelayInfo) (float64, error) {
	if info.Request == nil {
		return 0, nil
	}
	var raw json.RawMessage
	var err error
	var maxCalls uint
	webSearch := false
	switch request := info.Request.(type) {
	case *dto.GeneralOpenAIRequest:
		raw, err = common.Marshal(request.Tools)
		webSearch = request.WebSearchOptions != nil
	case *dto.ClaudeRequest:
		raw, err = common.Marshal(request.Tools)
	case *dto.OpenAIResponsesRequest:
		raw = request.Tools
		if request.MaxToolCalls != nil {
			maxCalls = *request.MaxToolCalls
		}
	case *dto.GeminiChatRequest:
		if len(request.Requests) > 0 {
			if len(request.Requests) > dto.MaxImageN {
				return 0, fmt.Errorf("Gemini batch is too large")
			}
			total := 0.0
			for i := range request.Requests {
				child := request.Requests[i]
				if len(child.Requests) > 0 {
					return 0, fmt.Errorf("nested Gemini batch is invalid")
				}
				copyInfo := *info
				copyInfo.Request = &child
				quota, err := requestToolBudget(&copyInfo)
				if err != nil {
					return 0, err
				}
				total += quota
			}
			return total, nil
		}
		raw = request.Tools
	default:
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var tools []struct {
		Type                  string          `json:"type"`
		MaxUses               int             `json:"max_uses"`
		GoogleSearch          json.RawMessage `json:"googleSearch"`
		GoogleSearchSnake     json.RawMessage `json:"google_search"`
		GoogleSearchRetrieval json.RawMessage `json:"googleSearchRetrieval"`
		CodeExecution         json.RawMessage `json:"codeExecution"`
		CodeExecutionSnake    json.RawMessage `json:"code_execution"`
		URLContext            json.RawMessage `json:"url_context"`
	}
	if len(raw) > 0 {
		if err = common.Unmarshal(raw, &tools); err != nil {
			return 0, err
		}
	}
	counts := map[string]int{}
	if strings.HasSuffix(info.GetBillingModelName(), "search-preview") || webSearch {
		counts[dto.BuildInToolWebSearchPreview] = 1
	}
	for _, tool := range tools {
		if len(tool.CodeExecution) > 0 || len(tool.CodeExecutionSnake) > 0 || len(tool.URLContext) > 0 {
			return 0, fmt.Errorf("cannot bound this provider-hosted tool before dispatch")
		}
		if tool.Type != "" && tool.Type != "function" && tool.Type != "custom" && !strings.HasPrefix(tool.Type, "web_search") {
			return 0, fmt.Errorf("cannot bound provider-hosted tool %s before dispatch", tool.Type)
		}
		name, count := "", 1
		switch {
		case strings.HasPrefix(tool.Type, "web_search"):
			name = dto.BuildInToolWebSearch
			if strings.HasPrefix(tool.Type, "web_search_preview") {
				name = dto.BuildInToolWebSearchPreview
			}
			count = tool.MaxUses
			if _, ok := info.Request.(*dto.OpenAIResponsesRequest); ok {
				if maxCalls > maxTokensLimit {
					return 0, fmt.Errorf("tool call limit is out of range")
				}
				count = int(maxCalls)
			}
		case len(tool.GoogleSearch) > 0 || len(tool.GoogleSearchSnake) > 0 || len(tool.GoogleSearchRetrieval) > 0:
			name = dto.BuildInToolGoogleSearch
		}
		if name == "" || operation_setting.GetToolPriceForModel(name, info.GetBillingModelName()) <= 0 {
			continue
		}
		if count <= 0 || count > maxTokensLimit {
			return 0, fmt.Errorf("paid built-in tools require an explicit bounded call limit")
		}
		counts[name] += count
	}
	quota := 0.0
	for name, count := range counts {
		quota += float64(count) * operation_setting.GetToolPriceForModel(name, info.GetBillingModelName()) / 1000 * common.QuotaPerUnit
	}
	if math.IsInf(quota, 0) || math.IsNaN(quota) || quota < 0 {
		return 0, fmt.Errorf("invalid tool billing budget")
	}
	return quota, nil
}

func RequiresOutboundTextBudget(info *relaycommon.RelayInfo) bool {
	if info != nil && (strings.Contains(info.RequestURLPath, ":embedContent") || strings.Contains(info.RequestURLPath, ":batchEmbedContents")) {
		return false
	}
	return info != nil && info.Request != nil && !info.IsChannelTest && !info.PriceData.FreeModel && (info.RelayMode == relayconstant.RelayModeChatCompletions || info.RelayMode == relayconstant.RelayModeCompletions || info.RelayMode == relayconstant.RelayModeResponses || info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini)
}

// PrepareOutboundTextBudget repeats reservation after conversion, injected
// prompts and administrator overrides, using the frozen pricing snapshot.
// With reservation hardening enforced it resolves the approved hidden usage
// envelope for the dispatch candidate and rejects before dispatch when the
// required reservation is unobtainable — it never clamps and sends.
func PrepareOutboundTextBudget(c *gin.Context, info *relaycommon.RelayInfo, body []byte) error {
	if !RequiresOutboundTextBudget(info) {
		return nil
	}
	copyInfo := *info
	switch info.GetFinalRequestRelayFormat() {
	case types.RelayFormatOpenAI:
		copyInfo.Request = &dto.GeneralOpenAIRequest{}
	case types.RelayFormatClaude:
		copyInfo.Request = &dto.ClaudeRequest{}
	case types.RelayFormatGemini:
		copyInfo.Request = &dto.GeminiChatRequest{}
	case types.RelayFormatOpenAIResponses:
		copyInfo.Request = &dto.OpenAIResponsesRequest{}
	default:
		return fmt.Errorf("cannot verify outbound generation budget for this provider")
	}
	if err := common.Unmarshal(body, copyInfo.Request); err != nil {
		return fmt.Errorf("cannot verify outbound request budget")
	}
	meta := copyInfo.Request.GetTokenCountMeta()
	output, err := requestOutputBudget(&copyInfo, meta)
	if err != nil {
		return err
	}
	prompt, err := service.EstimateRequestToken(c, meta, &copyInfo)
	if err != nil {
		return err
	}
	price := info.PriceData
	quota := 0.0
	images := 1
	if snap := info.TieredBillingSnapshot; snap != nil {
		if snap.EstimatedImageCount != nil {
			images = *snap.EstimatedImageCount
		}
		upper, err := billingexpr.ReservationUpperBound(snap.ExprString, float64(prompt)*max(1.0, snap.PreConsumeMultiplier), output, images)
		if err != nil {
			return err
		}
		quota = upper / 1_000_000 * common.QuotaPerUnit * snap.GroupRatio
	} else if !price.UsePrice {
		inputRatio := max(1.0, price.CacheRatio, price.CacheCreation5mRatio, price.CacheCreation1hRatio, price.ImageRatio, price.AudioRatio)
		outputRatio := max(price.CompletionRatio, price.AudioRatio*price.AudioCompletionRatio)
		quota = (float64(prompt)*inputRatio + output*outputRatio) * price.ModelRatio * price.GroupRatioInfo.GroupRatio
	} else {
		quota = price.ApplyOtherRatiosToFloat(price.ModelPrice * common.QuotaPerUnit * price.GroupRatioInfo.GroupRatio)
	}
	toolQuota, err := requestToolBudget(&copyInfo)
	if err != nil {
		return err
	}
	quota = max(float64(price.QuotaToPreConsume), quota+toolQuota*price.GroupRatioInfo.GroupRatio)
	legacyTarget, err := common.QuotaFromFloatStrict(math.Ceil(quota))
	if err != nil {
		return err
	}
	if info.Billing == nil {
		return fmt.Errorf("paid outbound request has no billing reservation")
	}

	if hardening := operation_setting.ReservationHardeningMode(); hardening != operation_setting.ReserveModeOff {
		if session, ok := info.Billing.(*service.BillingSession); ok && session.Durable() {
			estimate, estimateErr := service.EstimateDispatchReservation(info, prompt, output, images, toolQuota)
			switch hardening {
			case operation_setting.ReserveModeEnforce:
				if estimateErr != nil {
					return fmt.Errorf("billing admission rejected before dispatch: %w", estimateErr)
				}
				target := max(legacyTarget, estimate.RequiredQuota)
				if target > estimate.CapQuota {
					return fmt.Errorf("billing admission rejected before dispatch: required reservation %d exceeds authorization cap %d (profile %s)", target, estimate.CapQuota, estimate.ProfileSource)
				}
				estimate.ReservedQuota = target
				session.SetDispatchAuth(estimate.ToAuthSnapshot(info.RequestId))
				if err := info.Billing.Reserve(target); err != nil {
					return fmt.Errorf("billing admission reservation failed before dispatch: %w", err)
				}
				if err := session.PersistDispatchAuth(); err != nil {
					return fmt.Errorf("billing authorization snapshot could not be persisted: %w", err)
				}
				return nil
			case operation_setting.ReserveModeMonitor:
				if estimateErr != nil {
					logger.LogWarn(c, "reserve hardening monitor: dispatch would be rejected: "+estimateErr.Error())
				} else {
					estimate.ReservedQuota = max(legacyTarget, estimate.RequiredQuota)
					wouldReject := estimate.ReservedQuota > estimate.CapQuota
					logger.LogInfo(c, fmt.Sprintf("reserve hardening monitor: profile=%s required=%d cap=%d current=%d would_reject=%t",
						estimate.ProfileSource, estimate.ReservedQuota, estimate.CapQuota, info.Billing.GetPreConsumedQuota(), wouldReject))
					session.SetDispatchAuth(estimate.ToAuthSnapshot(info.RequestId))
				}
			}
		}
	}
	return info.Billing.Reserve(legacyTarget)
}
