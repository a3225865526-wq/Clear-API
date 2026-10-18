package imageexecution

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/accountpool"

	"github.com/gin-gonic/gin"
)

// ErrUnknownOutcomeCode is the fixed client-facing code for an unknown result.
const ErrUnknownOutcomeCode = "image_outcome_unknown"

// UnknownAPIError builds the stable UNKNOWN envelope. It carries the original
// request identity, no account identity and no source topology, and marks the
// request as not retryable by a new request.
func UnknownAPIError(c *gin.Context, requestID string) *types.NewAPIError {
	encoded, err := common.Marshal(UnknownPayload(c, requestID))
	if err != nil {
		return types.NewError(ErrUnknownOutcome, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	apiErr := types.NewErrorWithStatusCode(errors.New(ErrUnknownOutcomeCode), types.ErrorCodeUpdateDataError, http.StatusBadGateway, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	// Keep the exact envelope: the generic OpenAI error conversion would drop
	// request_id, retryable and status_path.
	apiErr.RelayError = types.OpenAIError{
		Message:  "生成结果暂时无法确认，请查询原请求状态",
		Type:     "image_execution_error",
		Code:     ErrUnknownOutcomeCode,
		Metadata: encoded,
	}
	apiErr.StatusCode = http.StatusBadGateway
	return apiErr
}

// FetchResult loads the persisted image payload from the executor and verifies
// that its digest still matches the durable receipt before returning it.
func FetchResult(ctx context.Context, requestID, attemptID string) ([]byte, error) {
	executor, err := accountpool.ExecutorClientFor(accountpool.ExecutorCliproxyAPI)
	if err != nil {
		return nil, ErrControlledRefused
	}
	loader, ok := executor.(accountpool.ImagesResultLoader)
	if !ok {
		return nil, ErrControlledRefused
	}
	return loader.ImagesResult(ctx, requestID, attemptID)
}

// EnsureUnknownDoesNotSettle documents the invariant at the call site: an
// UNKNOWN request keeps its active reservation, so the coordinator never calls
// SettleUncertain or Refund for it.
func EnsureUnknownDoesNotSettle(ctrl *Controlled) bool {
	return ctrl != nil && ctrl.Unknown
}

var _ = relaycommon.RelayInfo{}
