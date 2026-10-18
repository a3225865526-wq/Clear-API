package imageexecution

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/accountpool"

	"github.com/gin-gonic/gin"
)

const contextKey = "image_execution_controlled"

// Controlled is the request-scoped binding between one public image request and
// its authoritative state, signed policy and dispatch attempt.
type Controlled struct {
	Execution *Execution
	Policy    *SourcePolicy
	// Frozen operator evidence for the signed wire payload.
	PolicyID     int
	AccountOrder []string
	BaseModel    string
	PoolID       string
	MaxFallback  int
	Attempt      *Attempt
	AttemptSeq   int
	// Unknown is set when the request must keep its reservation and may not be
	// retried or refunded by the generic failure path.
	Unknown bool
	// settled is set once the single reservation has been committed.
	settled bool
	// allowNext is armed only by a trusted zero-dispatch proof. It permits one
	// further attempt on the other permitted source.
	allowNext         bool
	lastCandidateType string
}

// Settled reports whether this request's charge was already committed. The
// generic refund path must not touch it.
func Settled(ctrl *Controlled) bool { return ctrl != nil && ctrl.settled }

// FromContext returns the controlled binding, if this request is controlled.
func FromContext(c *gin.Context) *Controlled {
	if c == nil {
		return nil
	}
	value, exists := c.Get(contextKey)
	if !exists {
		return nil
	}
	controlled, _ := value.(*Controlled)
	return controlled
}

// ModeRequested reports whether the operator enabled controlled images at all.
func ModeRequested() bool { return Enabled() }

// Prepare freezes the operator decision for one public image request and
// registers the durable request identity. It never runs for non-image relays.
//
// Returning (nil, nil) means "not controlled": the caller keeps the legacy
// path byte for byte.
func Prepare(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest, bodyDigest string) (*Controlled, *types.NewAPIError) {
	if !Enabled() || c == nil || info == nil || request == nil {
		return nil, nil
	}
	if info.RelayMode != relayconstant.RelayModeImagesGenerations {
		return nil, nil
	}
	policy, err := ResolvePolicy(c, request.Model)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(errors.New("image source policy unavailable"), types.ErrorCodeInvalidRequest, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if policy == nil {
		return nil, nil
	}
	// Controlled image execution spends the site owner's account pool, so it is
	// reserved to the site owner regardless of which token called the endpoint.
	// The decision is server-side only: it reads the authenticated user identity
	// that TokenAuth established and offers no client-influenced alternative.
	// A non-owner keeps the ordinary routing they had before, which is why this
	// refuses the controlled path rather than failing the request outright.
	//
	// Identity is checked directly rather than through common.IsPoolOwner because
	// that helper also requires the pool master switch: an `api_only` policy must
	// still be owner-only even while the account pool is switched off.
	// The relay (token) auth path does not publish a role on the context, so the
	// fixed owner user ID is the authoritative claim available here.
	if info.UserId != common.PoolOwnerUserID {
		return nil, nil
	}
	// A client may never supply reserved trust headers.
	StripClientControlledHeaders(c)

	baseModel, err := ImagesBaseModel()
	if err != nil {
		return nil, types.NewErrorWithStatusCode(ErrControlledRefused, types.ErrorCodeInvalidRequest, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	poolID, err := ImagesPoolID()
	if err != nil {
		return nil, types.NewErrorWithStatusCode(ErrControlledRefused, types.ErrorCodeInvalidRequest, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	execution, apiErr := Begin(c, info, request, policy, bodyDigest, c.GetHeader("Idempotency-Key"))
	if apiErr != nil {
		return nil, apiErr
	}
	controlled := &Controlled{
		Execution: execution, Policy: policy, BaseModel: baseModel, PoolID: poolID,
		MaxFallback: MaxFallback(),
	}
	if !execution.Replay() {
		order, policyID, orderErr := accountpool.ImagesPolicyOrder(c.Request.Context(), poolID, request.Model)
		if orderErr != nil {
			return nil, types.NewErrorWithStatusCode(ErrControlledRefused, types.ErrorCodeInvalidRequest, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
		controlled.PolicyID, controlled.AccountOrder = policyID, order
	}
	c.Set(contextKey, controlled)
	return controlled, nil
}

// FinishRequest is the single request-level epilogue. An UNKNOWN request keeps
// its active reservation and its reconciliation marker; a settled or trusted
// zero-dispatch failure is left to the normal billing path.
func FinishRequest(c *gin.Context, controlled *Controlled, info *relaycommon.RelayInfo) {
	if controlled == nil || info == nil {
		return
	}
	if controlled.Unknown {
		if err := model.BillingHoldImageExecution(controlled.Execution.RequestID()); err != nil {
			logger.LogError(c, "pg03 images unknown request could not persist its reconciliation hold")
		}
	}
}

// FinishRequestHeld is exposed for tests that assert the UNKNOWN billing hold.
func FinishRequestHeld(requestID string) error {
	return model.BillingHoldImageExecution(requestID)
}

// ValidateStrategy enforces the frozen operator preset against the actual
// request candidates. The Route Chain remains the only scheduler; this check
// only proves that the chain the operator configured can express the chosen
// preset, and refuses a request whose chain would use a forbidden source.
func ValidateStrategy(policy *SourcePolicy, chainCandidates []string) error {
	if policy == nil {
		return nil
	}
	switch policy.Strategy {
	case model.ImageSourcePoolOnly:
		for _, candidate := range chainCandidates {
			if candidate != model.RouteChainCandidateTypePool {
				return ErrControlledRefused
			}
		}
	case model.ImageSourceAPIOnly:
		for _, candidate := range chainCandidates {
			if candidate != model.RouteChainCandidateTypeAPIChannel {
				return ErrControlledRefused
			}
		}
	case model.ImageSourcePoolFirst, model.ImageSourceAPIFirst:
		// Ordering is owned by the chain; both sources must be permitted.
		if !policy.AllowCross {
			return ErrControlledRefused
		}
	default:
		return ErrControlledRefused
	}
	return nil
}

// BeginAttempt registers the dispatch attempt for the current candidate.
//
// A second attempt is only legal after the previous one ended with a trusted
// zero-dispatch proof recorded by the relay; an attempt that may have reached a
// provider can never be followed by another dispatch.
func (c *Controlled) BeginAttempt(candidateType string, channelID int) *types.NewAPIError {
	if c == nil || c.Execution == nil {
		return types.NewError(ErrControlledRefused, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if err := c.validateCandidate(candidateType); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	c.AttemptSeq++
	attempt, apiErr := c.Execution.NewAttempt(c.AttemptSeq, candidateType, channelID)
	if apiErr != nil {
		return apiErr
	}
	c.Attempt = attempt
	c.allowNext = false
	return nil
}

func (c *Controlled) validateCandidate(candidateType string) error {
	if c.Policy == nil {
		return nil
	}
	if c.AttemptSeq > 0 && !c.allowNext {
		return errors.New("the previous image attempt may have reached a provider; no second dispatch is allowed")
	}
	if c.AttemptSeq > 0 {
		// A trusted zero-dispatch handover may only cross to the other source
		// when the frozen preset permits that source at all.
		previous := c.lastCandidateType
		if previous == candidateType {
			return errors.New("a trusted zero-dispatch retry must select a different candidate")
		}
		if candidateType == model.RouteChainCandidateTypePool && !c.Policy.Policy.AllowsPool() {
			return errors.New("image source policy forbids the account pool")
		}
		if candidateType == model.RouteChainCandidateTypeAPIChannel && !c.Policy.Policy.AllowsAPI() {
			return errors.New("image source policy forbids api channels")
		}
		return nil
	}
	switch c.Policy.Strategy {
	case model.ImageSourcePoolOnly:
		if candidateType != model.RouteChainCandidateTypePool {
			return errors.New("image source policy forbids api channels")
		}
	case model.ImageSourceAPIOnly:
		if candidateType != model.RouteChainCandidateTypeAPIChannel {
			return errors.New("image source policy forbids the account pool")
		}
	case model.ImageSourcePoolFirst:
		if candidateType != model.RouteChainCandidateTypePool {
			return errors.New("image source policy requires the account pool first")
		}
	case model.ImageSourceAPIFirst:
		if candidateType != model.RouteChainCandidateTypeAPIChannel {
			return errors.New("image source policy requires an api channel first")
		}
	default:
		return errors.New("image source policy is invalid")
	}
	return nil
}

// HandoverArmed reports whether the coordinator armed exactly one further
// attempt after a trusted zero-dispatch proof. The relay reads it to stop a
// controlled image request from entering the generic retry ladder.
func (c *Controlled) HandoverArmed() bool { return c != nil && c.allowNext }

// CandidateType exposes the candidate type of the current attempt.
func (c *Controlled) CandidateType() string {
	if c == nil || c.Attempt == nil {
		return ""
	}
	return c.Attempt.entry.CandidateType
}

// AllowTrustedHandover permits exactly one further attempt after a proof that
// nothing was sent. The relay calls it only with a trusted zero-dispatch proof.
func (c *Controlled) AllowTrustedHandover(candidateType string) {
	if c == nil || c.AttemptSeq == 0 {
		return
	}
	c.allowNext = true
	c.lastCandidateType = candidateType
}

// MarkDispatching records the Gateway send intent durably. It must succeed
// before the request body is written; a failure forbids the dispatch.
func (c *Controlled) MarkDispatching() *types.NewAPIError {
	if c == nil || c.Attempt == nil {
		return types.NewError(ErrControlledRefused, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	return c.Attempt.MarkDispatching()
}

// SignOutbound signs the final outbound image request with the frozen policy.
// It is called after every header override, so an override that changes the
// body invalidates the signature at the executor.
func (ctrl *Controlled) SignOutbound(c *gin.Context, req *http.Request, imageModel string) error {
	if ctrl == nil {
		return nil
	}
	if ctrl.Attempt == nil {
		return ErrControlledRefused
	}
	if err := SignPolicy(req, ctrl.Execution.RequestID(), ctrl.Attempt.AttemptID(), imageModel, ctrl.BaseModel, ctrl.PoolID, ctrl.PolicyID, ctrl.AccountOrder, ctrl.MaxFallback); err != nil {
		return ErrControlledRefused
	}
	logger.LogDebug(c, "pg03 images policy signed request=%s attempt=%s", ctrl.Execution.RequestID(), ctrl.Attempt.AttemptID())
	return nil
}

// SettleAndCommit settles the request's single reservation exactly once and
// then finalizes the attempt as SUCCEEDED. Order matters: the durable settled
// charge exists before a success terminal can be published, and a repeated
// terminal cannot charge twice.
func (ctrl *Controlled) SettleAndCommit(result Result, info *relaycommon.RelayInfo) *types.NewAPIError {
	if ctrl == nil || ctrl.Attempt == nil || info == nil || info.Billing == nil {
		return types.NewError(ErrControlledRefused, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if result.BillableCount != 1 {
		return types.NewError(ErrUnknownOutcome, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	// Image-count billing only: exactly one verified billable image, and the
	// frozen price snapshot already reserved that quantity.
	charged := info.Billing.GetPreConsumedQuota()
	if err := info.Billing.Settle(charged); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	ctrl.settled = true
	if err := model.AdvanceImageExecution(ctrl.Execution.RequestID(), "", model.ImageRequestReserved, map[string]any{
		"billing_state": "settled", "settled_quota": charged,
	}); err != nil && !errors.Is(err, model.ErrImageExecutionState) {
		return types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if _, apiErr := ctrl.CommitSuccess(result); apiErr != nil {
		return apiErr
	}
	return nil
}

// AttemptID exposes the current attempt identity to the relay layer.
func (ctrl *Controlled) AttemptID() string {
	if ctrl == nil || ctrl.Attempt == nil {
		return ""
	}
	return ctrl.Attempt.AttemptID()
}

// Outcome queries the executor's durable record without mutating the local
// attempt state. Settlement and the local terminal are committed separately so
// a success terminal is only ever published after the charge is final.
func (ctrl *Controlled) Outcome(ctx context.Context) (ExecutorRecord, *types.NewAPIError) {
	if ctrl == nil || ctrl.Attempt == nil {
		return ExecutorRecord{}, types.NewError(ErrControlledRefused, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	record, err := QueryExecutor(ctx, ctrl.Execution.RequestID(), ctrl.Attempt.AttemptID())
	if err != nil {
		return ExecutorRecord{}, types.NewError(ErrUnknownOutcome, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	return record, nil
}

// CommitSuccess finalizes a verified success. It must be called only after the
// settlement for this request has been committed exactly once.
func (ctrl *Controlled) CommitSuccess(result Result) (bool, *types.NewAPIError) {
	if ctrl == nil || ctrl.Attempt == nil {
		return false, types.NewError(ErrControlledRefused, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if result.BillableCount < 1 || result.BillableCount > 1 {
		return false, types.NewError(ErrUnknownOutcome, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if result.Dispatched {
		ctrl.Attempt.MarkExecutorDispatched()
	}
	return ctrl.Attempt.Succeed(result)
}

// FailTrusted records a definitively failed attempt that never dispatched.
func (c *Controlled) FailTrusted(reason string) {
	if c == nil || c.Attempt == nil {
		return
	}
	_ = c.Attempt.Fail(Result{State: "FAILED", EndReason: reason})
}

// RefuseUnknown records an unknown outcome without consulting the executor
// (for example a transport error after the body was written). The request-level
// hold is persisted even when the attempt record is not yet available.
func (c *Controlled) RefuseUnknown(reason string) {
	if c == nil {
		return
	}
	c.Unknown = true
	if c.Attempt != nil {
		_ = c.Attempt.HoldUnknown(reason)
		return
	}
	if c.Execution != nil {
		if err := model.AdvanceImageExecution(c.Execution.RequestID(), "", model.ImageRequestUnknown, map[string]any{
			"billing_state": "held_unknown", "result_state": "UNKNOWN", "end_reason": reason,
		}); err != nil && !errors.Is(err, model.ErrImageExecutionState) {
			return
		}
	}
}

// AttachToResponse sets the public response headers that make the durable
// request identity discoverable without leaking topology.
func (c *Controlled) AttachToResponse(ginCtx *gin.Context) {
	if c == nil || ginCtx == nil {
		return
	}
	ginCtx.Header("X-ClearAPI-Image-Request", c.Execution.RequestID())
	if c.Attempt != nil {
		ginCtx.Header("X-ClearAPI-Image-Attempt", c.Attempt.AttemptID())
	}
}

// UnknownPayload is the client-facing envelope for an unknown outcome. It
// carries no account identity, no source and no internal policy.
func UnknownPayload(ginCtx *gin.Context, requestID string) gin.H {
	return gin.H{"error": gin.H{
		"message":     "生成结果暂时无法确认，请查询原请求状态",
		"type":        "image_execution_error",
		"code":        "image_outcome_unknown",
		"request_id":  requestID,
		"retryable":   false,
		"status_path": "/v1/images/requests/" + requestID,
	}}
}

// ParseEntityID validates a public request/attempt identifier.
func ParseEntityID(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// AttemptSequence is exposed for the status projection only.
func AttemptSequence(attemptID string) int {
	index := strings.LastIndex(attemptID, "-a")
	if index < 0 {
		return 0
	}
	value, err := strconv.Atoi(attemptID[index+2:])
	if err != nil {
		return 0
	}
	return value
}

var _ = common.GetUUID
