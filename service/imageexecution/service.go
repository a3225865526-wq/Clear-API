// Package imageexecution owns the authoritative state of controlled PG03 image
// generations on the Gateway side.
//
// The Route Chain ledger is observational: it cannot suppress a duplicate
// dispatch, prove a dispatch boundary or decide settlement. This package adds
// the small authoritative record that can — one request row binding the user,
// token, idempotency key, frozen operator source policy and billing
// reservation, plus one attempt row per dispatch with BOTH boundaries recorded
// separately (Gateway→Executor and Executor→provider).
package imageexecution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/accountpool"

	"github.com/gin-gonic/gin"
)

var (
	// ErrIdempotencyConflict: the same key was reused with a different body.
	ErrIdempotencyConflict = errors.New("image idempotency key reused with a different request")
	// ErrControlledRefused: the controlled record could not be established, so
	// no dispatch may happen.
	ErrControlledRefused = errors.New("controlled image execution refused")
	// ErrUnknownOutcome: the attempt may have reached the provider but no
	// trusted terminal exists. It must not be replayed.
	ErrUnknownOutcome = errors.New("image execution outcome unknown")
)

const (
	PolicyHeader    = "X-ClearAPI-PG03-Images-Policy"
	SignatureHeader = "X-ClearAPI-PG03-Images-Signature"
	RequestHeader   = "X-ClearAPI-PG03-Images-Request"
	AttemptHeader   = "X-ClearAPI-PG03-Images-Attempt"

	imagesSignatureDomain = "clearapi-pg03-images-v1\n"
	ImagesOperation       = "images.generate"
	ImagesPath            = "/v1/images/generations"
	imagesModeEnv         = "CLEARAPI_PG03_IMAGES"
	imagesBaseModelEnv    = "CLEARAPI_PG03_IMAGES_BASE_MODEL"
	imagesPoolIDEnv       = "CLEARAPI_PG03_IMAGES_POOL_ID"
	imagesFallbackEnv     = "CLEARAPI_PG03_IMAGES_MAX_FALLBACK"
)

// Enabled reports whether controlled image execution is switched on. Off by
// default, so existing image routing is unchanged until an operator opts in.
func Enabled() bool { return os.Getenv(imagesModeEnv) == "true" }

// SourcePolicy is the frozen operator decision for one request.
type SourcePolicy struct {
	Strategy   string
	Policy     *model.ImageSourcePolicy
	ChainID    int
	ChainVer   int
	AllowCross bool
}

// ResolvePolicy reads the operator decision for the requested model. A missing
// policy returns (nil, nil): the request keeps legacy routing. A corrupt policy
// fails closed; controlled execution never silently degrades.
func ResolvePolicy(c *gin.Context, modelName string) (*SourcePolicy, error) {
	policy, err := model.GetImageSourcePolicy(modelName)
	if err != nil {
		return nil, ErrControlledRefused
	}
	if policy == nil {
		return nil, nil
	}
	resolved := &SourcePolicy{Strategy: policy.Strategy, Policy: policy}
	if c != nil {
		resolved.ChainID = c.GetInt("route_chain_snapshot_id")
		resolved.ChainVer = c.GetInt("route_chain_snapshot_version")
		resolved.AllowCross = c.GetBool("route_chain_snapshot_allow_cross")
	}
	return resolved, nil
}

// PublishChainIdentity lets the distributor expose the frozen chain identity
// without an import cycle. It carries no routing authority.
func PublishChainIdentity(c *gin.Context, chainID, version int, allowCross bool) {
	if c == nil {
		return
	}
	c.Set("route_chain_snapshot_id", chainID)
	c.Set("route_chain_snapshot_version", version)
	c.Set("route_chain_snapshot_allow_cross", allowCross)
}

// ImagesBaseModel is the operator-declared Responses base model the image tool
// runs on. v1 refuses to guess: an undeclared base model means controlled image
// generation is not enabled.
func ImagesBaseModel() (string, error) {
	value := strings.TrimSpace(os.Getenv(imagesBaseModelEnv))
	if value == "" || len(value) > 128 {
		return "", ErrControlledRefused
	}
	return value, nil
}

// ImagesPoolID is the operator-declared pool binding signed into the policy.
func ImagesPoolID() (string, error) {
	value := strings.TrimSpace(os.Getenv(imagesPoolIDEnv))
	if value == "" || len(value) > 128 {
		return "", ErrControlledRefused
	}
	return value, nil
}

// MaxFallback is the operator-declared fallback budget. It is bounded to the
// same range the Executor accepts.
func MaxFallback() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(imagesFallbackEnv)))
	if err != nil || value < 0 || value > 3 {
		return 0
	}
	return value
}

// Begin registers the authoritative request identity before any dispatch. A
// repeated key with the identical body resolves to the stored request; a
// different body is refused.
func Begin(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest, policy *SourcePolicy, bodyDigest string, idempotencyKey string) (*Execution, *types.NewAPIError) {
	if info == nil || request == nil || policy == nil {
		return nil, types.NewError(ErrControlledRefused, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if len(idempotencyKey) > 256 {
		return nil, types.NewError(ErrIdempotencyConflict, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	scope := ""
	if idempotencyKey != "" {
		// Scope by user+token so one tenant's key can never collide with another.
		scope = "u" + strconv.Itoa(info.UserId) + ":t" + strconv.Itoa(info.TokenId)
	}
	entry := &model.ImageExecution{
		RequestID:            info.RequestId,
		UserID:               info.UserId,
		TokenID:              info.TokenId,
		IdempotencyScope:     scope,
		IdempotencyKeyDigest: model.ImageRequestIdempotencyDigest(idempotencyKey),
		BodyDigest:           bodyDigest,
		Operation:            ImagesOperation,
		Model:                request.Model,
		RequestedCount:       1,
		SourceStrategy:       policy.Strategy,
		ChainID:              policy.ChainID,
		ChainVersion:         policy.ChainVer,
		BillingState:         "pending",
	}
	if err := model.CreateImageExecutionRequest(entry); err != nil {
		switch {
		case errors.Is(err, model.ErrImageExecutionIdempotent):
			return nil, types.NewErrorWithStatusCode(ErrIdempotencyConflict, types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry())
		case errors.Is(err, model.ErrImageExecutionConflict):
			existing, loadErr := model.LoadImageExecution(entry.RequestID)
			if loadErr != nil {
				return nil, types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			return &Execution{entry: existing, policy: policy, replay: true}, nil
		default:
			return nil, types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}
	if err := model.AdvanceImageExecution(info.RequestId, model.ImageRequestCreated, model.ImageRequestValidated, nil); err != nil {
		return nil, types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	entry.State = model.ImageRequestValidated
	return &Execution{entry: entry, policy: policy}, nil
}

// Execution is the request-scoped handle.
type Execution struct {
	entry  *model.ImageExecution
	policy *SourcePolicy
	replay bool
}

func (e *Execution) RequestID() string { return e.entry.RequestID }
func (e *Execution) Replay() bool      { return e.replay }
func (e *Execution) Strategy() string  { return e.policy.Strategy }

// MarkReserved records the durable reservation amount.
func (e *Execution) MarkReserved(reservation int) *types.NewAPIError {
	if err := model.AdvanceImageExecution(e.entry.RequestID, model.ImageRequestValidated, model.ImageRequestReserved, map[string]any{
		"billing_state": "reserved", "reserved_quota": reservation,
	}); err != nil {
		return types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	e.entry.State = model.ImageRequestReserved
	return nil
}

// Attempt is one dispatch attempt.
type Attempt struct {
	execution *Execution
	entry     *model.ImageExecutionAttempt
}

func (e *Execution) NewAttempt(sequence int, candidateType string, channelID int) (*Attempt, *types.NewAPIError) {
	attempt := &model.ImageExecutionAttempt{
		RequestID:     e.entry.RequestID,
		AttemptID:     e.entry.RequestID + "-a" + strconv.Itoa(sequence),
		Sequence:      sequence,
		CandidateType: candidateType,
		ChannelID:     channelID,
	}
	if err := model.CreateImageExecutionAttempt(attempt); err != nil {
		return nil, types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	return &Attempt{execution: e, entry: attempt}, nil
}

func (a *Attempt) AttemptID() string { return a.entry.AttemptID }

// MarkDispatching records the Gateway's send intent. It must be durable before
// the outbound request is written so a crash cannot lose the fact that a send
// may have happened. A repeat is a no-op, not a second send.
func (a *Attempt) MarkDispatching() *types.NewAPIError {
	if err := model.BeginImageAttemptDispatch(a.entry.RequestID, a.entry.AttemptID); err != nil {
		return types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if err := model.MarkImageAttemptGatewaySent(a.entry.RequestID, a.entry.AttemptID); err != nil {
		return types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	a.entry.State = model.ImageAttemptDispatching
	a.entry.GatewaySentAt = time.Now().Unix()
	return nil
}

// MarkExecutorDispatched promotes the second boundary once a receipt proves
// the Executor reached its provider call.
func (a *Attempt) MarkExecutorDispatched() {
	if err := model.MarkImageAttemptDispatched(a.entry.RequestID, a.entry.AttemptID); err != nil {
		common.SysError("image attempt executor-dispatch boundary could not be recorded")
	}
}

// Succeed finalizes the attempt and request from a trusted terminal result.
// A duplicate terminal for an already-succeeded attempt changes nothing.
func (a *Attempt) Succeed(result Result) (bool, *types.NewAPIError) {
	changed, err := model.FinalizeImageAttempt(a.entry.RequestID, a.entry.AttemptID, model.ImageAttemptSucceeded, map[string]any{
		"receipt_id": result.ReceiptID, "receipt_sha256": result.ReceiptSHA256,
		"result_count": result.ResultCount, "billable_image_count": result.BillableCount,
		"end_reason": result.EndReason,
	})
	if err != nil {
		return false, types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if !changed {
		return false, nil
	}
	err = model.AdvanceImageExecution(a.entry.RequestID, "", model.ImageRequestSucceeded, map[string]any{
		"billing_state": "settled", "result_state": "COMPLETED",
		"billable_image_count": result.BillableCount, "result_ref": result.ResultRef,
		"result_sha256": result.ResultSHA256, "end_reason": result.EndReason,
	})
	if err != nil {
		return false, types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	return true, nil
}

// Fail finalizes a definitively failed attempt. The request itself only becomes
// FAILED when the relay decides no candidate may legally run.
func (a *Attempt) Fail(result Result) *types.NewAPIError {
	if _, err := model.FinalizeImageAttempt(a.entry.RequestID, a.entry.AttemptID, model.ImageAttemptFailed, map[string]any{
		"receipt_id": result.ReceiptID, "result_count": result.ResultCount,
		"billable_image_count": 0, "end_reason": result.EndReason,
	}); err != nil {
		return types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	return nil
}

// HoldUnknown isolates an attempt whose outcome is not known. The request keeps
// its active reservation and is never retried on another source.
func (a *Attempt) HoldUnknown(reason string) *types.NewAPIError {
	_, err := model.FinalizeImageAttempt(a.entry.RequestID, a.entry.AttemptID, model.ImageAttemptUnknown, map[string]any{"end_reason": reason})
	if err != nil && !errors.Is(err, model.ErrImageExecutionState) {
		common.SysError("image unknown attempt state could not be recorded")
	}
	if err := model.AdvanceImageExecution(a.entry.RequestID, "", model.ImageRequestUnknown, map[string]any{
		"billing_state": "held_unknown", "result_state": "UNKNOWN", "end_reason": reason,
	}); err != nil && !errors.Is(err, model.ErrImageExecutionState) {
		return types.NewError(ErrControlledRefused, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	return nil
}

// Result is the trusted terminal description used to finalize an attempt.
type Result struct {
	State         string
	ReceiptID     string
	ReceiptSHA256 string
	Dispatched    bool
	ResultCount   int
	BillableCount int
	ResultRef     string
	ResultSHA256  string
	EndReason     string
}

// SignPolicy attaches the signed images policy to the outbound request. The
// signature covers the final outbound bytes, so any later override invalidates
// it and the Executor refuses the request.
func SignPolicy(req *http.Request, requestID, attemptID, modelName, baseModel, poolID string, policyID int, accountOrder []string, maxFallback int) error {
	key := os.Getenv("CLEARAPI_PG03_IMAGES_POLICY_KEY")
	if len(key) < 32 || req == nil || req.GetBody == nil {
		return ErrControlledRefused
	}
	if requestID == "" || attemptID == "" || modelName == "" || baseModel == "" || poolID == "" || policyID <= 0 || len(accountOrder) == 0 || len(accountOrder) > 4 || maxFallback < 0 || maxFallback > 3 {
		return ErrControlledRefused
	}
	body, err := req.GetBody()
	if err != nil {
		return ErrControlledRefused
	}
	defer func() { _ = body.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, body); err != nil {
		return ErrControlledRefused
	}
	payload := policyPayload{
		Schema: 2, Operation: ImagesOperation, PolicyID: policyID, PoolID: poolID,
		ModelName: modelName, BaseModel: baseModel, AccountOrder: accountOrder,
		MaxFallbackCount: maxFallback, RequestID: requestID, AttemptID: attemptID,
		Method: req.Method, Path: req.URL.Path, BodyDigest: hex.EncodeToString(digest.Sum(nil)),
		IssuedAt: time.Now().Unix(),
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return ErrControlledRefused
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(imagesSignatureDomain))
	mac.Write(encoded)
	req.Header.Set(PolicyHeader, base64.RawURLEncoding.EncodeToString(encoded))
	req.Header.Set(SignatureHeader, hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set(RequestHeader, requestID)
	req.Header.Set(AttemptHeader, attemptID)
	return nil
}

// policyPayload field order is the canonical wire format shared with the
// Executor. Changing it requires a schema bump on both sides.
type policyPayload struct {
	Schema           int      `json:"schema"`
	Operation        string   `json:"operation"`
	PolicyID         int      `json:"policy_id"`
	PoolID           string   `json:"pool_id"`
	ModelName        string   `json:"model_name"`
	BaseModel        string   `json:"base_model"`
	AccountOrder     []string `json:"account_order"`
	MaxFallbackCount int      `json:"max_fallback_count"`
	RequestID        string   `json:"request_id"`
	AttemptID        string   `json:"attempt_id"`
	Method           string   `json:"method"`
	Path             string   `json:"path"`
	BodyDigest       string   `json:"body_digest"`
	IssuedAt         int64    `json:"issued_at"`
}

// StripClientControlledHeaders removes client-supplied reserved headers so a
// request can never self-authorize a pool execution or a legacy receipt.
func StripClientControlledHeaders(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	for _, name := range []string{
		PolicyHeader, SignatureHeader, RequestHeader, AttemptHeader,
		"X-ClearAPI-PG02-Request", "X-ClearAPI-PG02-Attempt", "X-ClearAPI-PG02-Gateway-Proof",
		"X-ClearAPI-PG03-Policy", "X-ClearAPI-PG03-Signature", "X-ClearAPI-PG03-Request",
	} {
		c.Request.Header.Del(name)
	}
}

// BodyDigestOf hashes the client body for the idempotency binding.
func BodyDigestOf(body []byte) string { return model.ImageRequestBodyDigest(body) }

// LoadStatus returns the durable projection for the status endpoints.
func LoadStatus(requestID string) (model.ImageExecutionJSON, *types.NewAPIError) {
	entry, err := model.LoadImageExecution(requestID)
	if err != nil {
		if errors.Is(err, model.ErrImageExecutionNotFound) {
			return model.ImageExecutionJSON{}, types.NewErrorWithStatusCode(errors.New("image request not found"), types.ErrorCodeInvalidRequest, http.StatusNotFound, types.ErrOptionWithSkipRetry())
		}
		return model.ImageExecutionJSON{}, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	attempts, err := model.ListImageExecutionAttempts(requestID)
	if err != nil {
		return model.ImageExecutionJSON{}, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	return model.ProjectImageExecution(entry, attempts), nil
}

// OwnedBy reports whether the request belongs to the caller. The public status
// endpoint must not disclose another user's request.
func OwnedBy(requestID string, userID int) bool {
	entry, err := model.LoadImageExecution(requestID)
	if err != nil {
		return false
	}
	return userID > 0 && entry.UserID == userID
}

// OwnerID returns the stored owner without exposing the record.
func OwnerID(requestID string) (int, bool) {
	entry, err := model.LoadImageExecution(requestID)
	if err != nil {
		return 0, false
	}
	return entry.UserID, true
}

// QueryExecutor reads the Executor's durable record for one attempt. It is the
// authority for "was this dispatched and what was the result".
func QueryExecutor(ctx context.Context, requestID, attemptID string) (ExecutorRecord, error) {
	executor, err := accountpool.ExecutorClientFor(accountpool.ExecutorCliproxyAPI)
	if err != nil {
		return ExecutorRecord{}, ErrControlledRefused
	}
	querier, ok := executor.(accountpool.ImagesExecutionQuerier)
	if !ok {
		return ExecutorRecord{}, ErrControlledRefused
	}
	record, err := querier.ImagesExecution(ctx, requestID, attemptID)
	if err != nil {
		return ExecutorRecord{}, ErrControlledRefused
	}
	return ExecutorRecord{
		Found: record.Found, State: record.State, ResultState: record.ResultState,
		ReceiptID: record.ReceiptID, ResultCount: record.ResultCount,
		BillableCount: record.BillableImageCount, ResultRef: record.ResultRef,
		ResultSHA256: record.ResultSHA256, EndReason: record.EndReason,
		Executed: record.Found && record.State != "" && record.State != "NOT_STARTED",
	}, nil
}

// ExecutorRecord is the transport view of the Executor's answer.
type ExecutorRecord struct {
	Found         bool
	State         string
	ResultState   string
	ReceiptID     string
	ResultCount   int
	BillableCount int
	ResultRef     string
	ResultSHA256  string
	EndReason     string
	Executed      bool
}

// ToResult maps a trusted executor record onto a terminal Result.
func (r ExecutorRecord) ToResult() Result {
	return Result{
		State: r.ResultState, ReceiptID: r.ReceiptID, Dispatched: r.Executed,
		ResultCount: r.ResultCount, BillableCount: r.BillableCount,
		ResultRef: r.ResultRef, ResultSHA256: r.ResultSHA256, EndReason: r.EndReason,
	}
}
