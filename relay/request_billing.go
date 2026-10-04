package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// PrepareRequestBilling estimates and reserves one request's charge. Transports
// provide the current request body through BodyStorage or BillingRequestInput;
// channel retries retain the resulting billing session and pricing snapshot.
func PrepareRequestBilling(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer}
	if info.Request != nil && (needSensitiveCheck || constant.CountToken) {
		meta = info.Request.GetTokenCountMeta()
	} else {
		// Avoid building CombineText when only the pricing quantities are needed.
		switch request := info.Request.(type) {
		case *dto.GeneralOpenAIRequest:
			meta.MaxTokens = int(max(lo.FromPtr(request.MaxTokens), lo.FromPtr(request.MaxCompletionTokens)))
		case *dto.OpenAIResponsesRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxOutputTokens))
		case *dto.ClaudeRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxTokens))
		case *dto.ImageRequest:
			meta = request.GetTokenCountMeta()
		}
	}

	if needSensitiveCheck && meta != nil {
		if contains, words := service.CheckSensitiveText(meta.CombineText); contains {
			service.RequestPolicy(c).AddEvent(service.PolicyEvent{ErrorCode: string(types.ErrorCodeSensitiveWordsDetected), ErrorSource: "local", Decision: service.PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "global"}, Health: "unchanged"})
			message := fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", "))
			logger.LogWarn(c, message)
			return types.NewError(errors.New(message), types.ErrorCodeSensitiveWordsDetected)
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, info)
	if err != nil {
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	info.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, info, tokens, meta)
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", info.OriginModelName))
		return nil
	}
	return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info)
}

// RefundFailedRequestBilling applies the common final-failure policy after all
// eligible attempts have ended. A settled BillingSession never refunds again.
func RefundFailedRequestBilling(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil {
		return nil
	}
	apiErr = service.NormalizeViolationFeeError(apiErr)
	if info.Billing != nil {
		upstream := apiErr.ToOpenAIError()
		code, _ := upstream.Code.(string)
		if code == "account_revoked" || code == "pool_admission_required" {
			var evidence struct {
				Dispatched *bool              `json:"codex_upstream_dispatched"`
				Meter      *dto.CodexMetering `json:"codex_metering"`
			}
			_ = common.Unmarshal(upstream.Metadata, &evidence)
			if evidence.Meter != nil && evidence.Meter.Status == "exact" && evidence.Meter.Validate() {
				raw, err := common.Marshal(map[string]any{"usage": map[string]any{"codex_metering": evidence.Meter}})
				if err == nil {
					service.ObserveCodexMetering(c, info, raw, true)
					if exact := service.CodexUsageForSettlement(c, info); exact != nil {
						service.PostTextConsumeQuota(c, info, exact, nil)
						return apiErr
					}
				}
			}
			if evidence.Dispatched != nil {
				c.Set("upstream_usage_uncertain", *evidence.Dispatched)
				if !*evidence.Dispatched {
					c.Set("upstream_attempted", false)
				}
			} else if c.GetBool("upstream_attempted") {
				c.Set("upstream_usage_uncertain", true)
			}
		}
		if c.GetBool("upstream_usage_uncertain") || c.GetBool("upstream_attempted") {
			if usage := service.CodexUsageForSettlement(c, info); usage != nil {
				service.PostTextConsumeQuota(c, info, usage, nil)
				return apiErr
			}
			// A disconnect does not establish that the provider did no work.
			// Keep the reserved charge for reconciliation instead of offering a
			// free invocation by aborting the stream before usage is received.
			var err error
			if session, ok := info.Billing.(*service.BillingSession); ok {
				err = session.SettleUncertain()
			} else {
				err = info.Billing.Settle(info.Billing.GetPreConsumedQuota())
			}
			if err != nil {
				logger.LogError(c, "cancelled upstream request requires billing reconciliation")
			}
			logger.LogWarn(c, "upstream usage unknown after dispatch; reserved quota retained pending usage reconciliation")
		} else {
			info.Billing.Refund(c)
		}
	}
	service.ChargeViolationFeeIfNeeded(c, info, apiErr)
	return apiErr
}
