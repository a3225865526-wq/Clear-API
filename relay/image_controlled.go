package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/imageexecution"

	"github.com/gin-gonic/gin"
)

// controlledImageHelper is the Images Contract v1 execution path.
//
// It reuses the existing channel conversion, quantity reservation and image
// count rules. What it does not reuse is the generic retry ladder: the
// operator-frozen source policy, the durable attempt record and the executor's
// terminal receipt own the outcome, so a pool image is never dispatched twice.
func controlledImageHelper(c *gin.Context, info *relaycommon.RelayInfo, imageReq *dto.ImageRequest, controlled *imageexecution.Controlled) *types.NewAPIError {
	if controlled.Execution.Replay() {
		return controlledImageReplay(c, controlled)
	}
	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		controlled.FailTrusted("model_mapping_failed")
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	// v1 supports exactly one non-streaming image. Anything else is refused
	// before a reservation, so the client never pays for an unsupported shape.
	if request.N != nil && *request.N != 1 {
		controlled.FailTrusted("unsupported_image_count")
		return types.NewErrorWithStatusCode(errors.New("controlled image generation supports exactly n=1"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if err := validateControlledImageParameters(request); err != nil {
		controlled.FailTrusted("unsupported_parameter")
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		controlled.FailTrusted("invalid_api_type")
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
	if err != nil {
		controlled.FailTrusted("convert_request_failed")
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		controlled.FailTrusted("convert_request_failed")
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			controlled.FailTrusted("param_override_failed")
			return newAPIErrorFromParamOverride(err)
		}
	}
	// The outbound body is the billing quantity authority: an override must not
	// change the paid count behind the client's back.
	var outbound struct {
		N *uint `json:"n"`
	}
	if err := common.Unmarshal(jsonData, &outbound); err != nil {
		controlled.FailTrusted("invalid_image_billing_parameters")
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid image billing parameters: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if outbound.N != nil && *outbound.N != 1 {
		controlled.FailTrusted("unsupported_image_count")
		return types.NewErrorWithStatusCode(errors.New("controlled image generation supports exactly n=1"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	info.ImageRequestCount = 1
	if billingErr := service.PrepareImageBillingForRequest(c, info, 1); billingErr != nil {
		controlled.FailTrusted("billing_reservation_failed")
		return billingErr
	}
	if apiErr := controlled.Execution.MarkReserved(info.Billing.GetPreConsumedQuota()); apiErr != nil {
		return apiErr
	}
	candidateType := model.RouteChainCandidateTypeAPIChannel
	if channel, channelErr := model.GetChannelById(info.ChannelId, false); channelErr == nil && channel != nil && channel.PoolChannelBound() {
		candidateType = model.RouteChainCandidateTypePool
	}
	if apiErr := controlled.BeginAttempt(candidateType, info.ChannelId); apiErr != nil {
		return apiErr
	}

	body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		controlled.FailTrusted("outbound_body_failed")
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, body)
	if err != nil {
		// The send intent is already durable and may have reached the executor.
		// Never retry: query the durable record instead.
		return controlledFinalizeUnknown(c, controlled, "gateway_transport_error")
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			newAPIError := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return controlledFinalizeFailure(c, controlled, newAPIError)
		}
	}
	return controlledSettleAndPublish(c, info, controlled)
}

// controlledSettleAndPublish reads the executor's trusted terminal, settles the
// single reservation exactly once, and only then publishes the success.
func controlledSettleAndPublish(c *gin.Context, info *relaycommon.RelayInfo, controlled *imageexecution.Controlled) *types.NewAPIError {
	record, apiErr := controlled.Outcome(c.Request.Context())
	if apiErr != nil {
		controlled.RefuseUnknown("executor_query_failed")
		return unknownImageError(c, controlled)
	}
	if !record.Found {
		// The executor has no durable attempt: this is a trusted zero-dispatch
		// failure, and the generic refund path may release the reservation.
		controlled.FailTrusted("executor_no_attempt")
		return types.NewErrorWithStatusCode(errors.New("image execution was not accepted"), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	result := record.ToResult()
	switch record.ResultState {
	case "COMPLETED":
		if record.BillableCount != 1 {
			controlled.RefuseUnknown("billable_count_not_one")
			return unknownImageError(c, controlled)
		}
		if apiErr := controlled.SettleAndCommit(result, info); apiErr != nil {
			return unknownImageError(c, controlled)
		}
		return writeControlledImageResponse(c, controlled)
	case "FAILED":
		controlled.FailTrusted(record.EndReason)
		if record.Executed {
			// A dispatched failure may still have cost the provider; keep the
			// reconciliation hold instead of refunding automatically.
			controlled.RefuseUnknown("provider_failed_after_dispatch")
			return unknownImageError(c, controlled)
		}
		return types.NewErrorWithStatusCode(errors.New("image generation failed"), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	default:
		controlled.RefuseUnknown("executor_outcome_unknown")
		return unknownImageError(c, controlled)
	}
}

// controlledFinalizeFailure consults the durable record before choosing the
// client error: a trusted zero-dispatch failure is reported as such, while an
// attempt that may have reached the provider becomes UNKNOWN.
func controlledFinalizeFailure(c *gin.Context, controlled *imageexecution.Controlled, fallback *types.NewAPIError) *types.NewAPIError {
	record, queryErr := controlled.Outcome(c.Request.Context())
	if queryErr != nil || !record.Found {
		// An absent executor record proves the executor never started this
		// attempt, so the frozen strategy may still hand over to another
		// candidate. That decision stays with the Route Chain; this path only
		// records the zero-dispatch fact.
		if queryErr == nil && !record.Found {
			controlled.FailTrusted("executor_no_attempt")
			controlled.AllowTrustedHandover(controlled.CandidateType())
			controlled.AttachToResponse(c)
			return fallback
		}
		controlled.RefuseUnknown("executor_query_failed")
		return unknownImageError(c, controlled)
	}
	if record.Executed || record.State == "STARTED" {
		controlled.RefuseUnknown("executor_rejected_after_dispatch")
		return unknownImageError(c, controlled)
	}
	controlled.FailTrusted("executor_rejected_before_dispatch")
	controlled.AllowTrustedHandover(controlled.CandidateType())
	controlled.AttachToResponse(c)
	return fallback
}

func controlledFinalizeUnknown(c *gin.Context, controlled *imageexecution.Controlled, reason string) *types.NewAPIError {
	controlled.RefuseUnknown(reason)
	return unknownImageError(c, controlled)
}

func unknownImageError(c *gin.Context, controlled *imageexecution.Controlled) *types.NewAPIError {
	controlled.AttachToResponse(c)
	return imageexecution.UnknownAPIError(c, controlled.Execution.RequestID())
}

// writeControlledImageResponse returns the persisted image exactly once. If the
// payload cannot be retrieved, the outcome stays UNKNOWN — a success terminal is
// never fabricated.
func writeControlledImageResponse(c *gin.Context, controlled *imageexecution.Controlled) *types.NewAPIError {
	body, err := imageexecution.FetchResult(c.Request.Context(), controlled.Execution.RequestID(), controlled.AttemptID())
	if err != nil {
		controlled.RefuseUnknown("result_fetch_failed")
		return unknownImageError(c, controlled)
	}
	controlled.AttachToResponse(c)
	c.Data(http.StatusOK, "application/json", body)
	return nil
}

// controlledImageReplay returns the durable state of an already accepted
// idempotent request. It never starts a second generation.
func controlledImageReplay(c *gin.Context, controlled *imageexecution.Controlled) *types.NewAPIError {
	view, apiErr := imageexecution.LoadStatus(controlled.Execution.RequestID())
	if apiErr != nil {
		return apiErr
	}
	c.Header("X-ClearAPI-Image-Replay", "true")
	c.Header("X-ClearAPI-Image-Request", view.RequestID)
	switch view.State {
	case model.ImageRequestSucceeded:
		body, err := imageexecution.FetchResult(c.Request.Context(), view.RequestID, lastAttemptID(view))
		if err != nil {
			return unknownImageError(c, controlled)
		}
		c.Data(http.StatusOK, "application/json", body)
		return nil
	case model.ImageRequestUnknown:
		return unknownImageError(c, controlled)
	}
	c.JSON(http.StatusConflict, gin.H{"error": gin.H{
		"message": "the original request is still being processed", "type": "image_execution_error",
		"code": "image_outcome_pending", "request_id": view.RequestID, "retryable": false,
		"status_path": "/v1/images/requests/" + view.RequestID,
	}})
	return types.NewErrorWithStatusCode(errors.New("image request pending"), types.ErrorCodeUpdateDataError, http.StatusConflict, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

func lastAttemptID(view model.ImageExecutionJSON) string {
	if len(view.Attempts) == 0 {
		return ""
	}
	return view.Attempts[len(view.Attempts)-1].AttemptID
}

// validateControlledImageParameters refuses parameters the v1 pool path has not
// verified. A refusal is explicit; nothing is silently dropped.
func validateControlledImageParameters(request *dto.ImageRequest) error {
	if request == nil {
		return errors.New("image request is empty")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return errors.New("prompt is required")
	}
	if strings.TrimSpace(request.Model) == "" {
		return errors.New("model is required")
	}
	if request.Stream != nil && *request.Stream {
		return errors.New("controlled image generation does not support streaming")
	}
	if _, exists := request.Extra["parameters"]; exists {
		return errors.New("controlled image generation does not support provider parameter overrides")
	}
	return nil
}
