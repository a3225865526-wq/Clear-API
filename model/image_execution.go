package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
)

// PG03 Images Contract v1 authoritative execution state.
//
// The Route Chain ledger is an observational record and must never be used as
// the authority for duplicate suppression, dispatch boundaries or settlement.
// This model stores the request-scoped identity, the idempotency binding and
// one row per attempt with both dispatch boundaries recorded separately.
var (
	ErrImageExecutionConflict   = errors.New("image execution conflict")
	ErrImageExecutionNotFound   = errors.New("image execution not found")
	ErrImageExecutionState      = errors.New("image execution state rejected")
	ErrImageExecutionIdempotent = errors.New("image execution idempotency conflict")
)

const (
	// Request states.
	ImageRequestCreated    = "CREATED"
	ImageRequestValidated  = "VALIDATED"
	ImageRequestReserved   = "RESERVED"
	ImageRequestSucceeded  = "SUCCEEDED"
	ImageRequestFailed     = "FAILED"
	ImageRequestUnknown    = "UNKNOWN"
	ImageRequestCancelled  = "CANCELLED"
	ImageRequestCancelReq  = "CANCEL_REQUESTED"
	ImageRequestOperation  = "images.generate"
	ImageRequestResultLive = "LIVE"
)

// Attempt states. DISPATCHING means the Gateway registered the send intent;
// DISPATCHED additionally means the Executor reported its upstream boundary.
const (
	ImageAttemptCreated     = "CREATED"
	ImageAttemptReserved    = "RESERVED"
	ImageAttemptDispatching = "DISPATCHING"
	ImageAttemptDispatched  = "DISPATCHED"
	ImageAttemptSucceeded   = "SUCCEEDED"
	ImageAttemptFailed      = "FAILED"
	ImageAttemptUnknown     = "UNKNOWN"
	ImageAttemptCancelled   = "CANCELLED"
)

// ImageExecution is one user-facing image generation request. SourceStrategy is
// the operator-frozen policy snapshot; the client can never set it.
type ImageExecution struct {
	ID        int64  `json:"id"`
	RequestID string `gorm:"type:varchar(128);uniqueIndex:idx_image_execution_request_id"`
	UserID    int    `gorm:"index:idx_image_execution_user"`
	TokenID   int
	// IdempotencyScope and IdempotencyKeyDigest form the durable dedup identity.
	// An empty scope means the client supplied no key.
	IdempotencyScope     string `gorm:"type:varchar(64);uniqueIndex:idx_image_execution_idempotency,priority:1"`
	IdempotencyKeyDigest string `gorm:"type:varchar(64);uniqueIndex:idx_image_execution_idempotency,priority:2"`
	BodyDigest           string `gorm:"type:varchar(64)"`
	Operation            string `gorm:"type:varchar(32)"`
	Model                string `gorm:"type:varchar(128)"`
	RequestedCount       int
	// SourceStrategy is the frozen admin policy (pool_first/api_first/pool_only/api_only).
	SourceStrategy     string `gorm:"type:varchar(24)"`
	ChainID            int
	ChainVersion       int
	State              string `gorm:"type:varchar(24);index"`
	ReservationID      int64
	ReservedQuota      int
	SettledQuota       int
	BillingState       string `gorm:"type:varchar(24)"`
	ResultState        string `gorm:"type:varchar(24)"`
	BillableImageCount int
	ResultRef          string `gorm:"type:varchar(160)"`
	ResultSHA256       string `gorm:"type:varchar(64)"`
	EndReason          string `gorm:"type:varchar(64)"`
	CreatedTime        int64
	UpdatedTime        int64
}

// ImageExecutionAttempt is one dispatch attempt of a request. The two send
// boundaries are separate columns on purpose: gateway_sent_at only proves the
// Gateway wrote to the Executor; executor_dispatched_at proves the Executor
// reached its own upstream boundary.
type ImageExecutionAttempt struct {
	ID                   int64  `json:"id"`
	RequestID            string `gorm:"type:varchar(128);uniqueIndex:idx_image_attempt_identity,priority:1"`
	AttemptID            string `gorm:"type:varchar(128);uniqueIndex:idx_image_attempt_identity,priority:2"`
	Sequence             int
	CandidateType        string `gorm:"type:varchar(24)"`
	ChannelID            int
	AccountRef           string `gorm:"type:varchar(160)"`
	State                string `gorm:"type:varchar(24);index"`
	GatewaySentAt        int64
	ExecutorDispatchedAt int64
	ReceiptID            string `gorm:"type:varchar(160)"`
	ReceiptSHA256        string `gorm:"type:varchar(64)"`
	ResultCount          int
	BillableImageCount   int
	EndReason            string `gorm:"type:varchar(64)"`
	CreatedTime          int64
	UpdatedTime          int64
}

// MigrateImageExecutions is explicit; it is not part of startup AutoMigrate.
func MigrateImageExecutions(db *gorm.DB) error {
	return db.AutoMigrate(&ImageExecution{}, &ImageExecutionAttempt{})
}

// ImageExecutionsReady fails closed when the tables or uniqueness guarantees
// are missing, so a deployment cannot silently run without dedup authority.
func ImageExecutionsReady(db *gorm.DB) error {
	if db == nil {
		return errors.New("image execution database unavailable")
	}
	for index, value := range map[string]any{
		"idx_image_execution_request_id":  &ImageExecution{},
		"idx_image_execution_idempotency": &ImageExecution{},
		"idx_image_attempt_identity":      &ImageExecutionAttempt{},
	} {
		if !db.Migrator().HasIndex(value, index) {
			return errors.New("image execution uniqueness index is missing: " + index)
		}
	}
	var rows []ImageExecution
	return db.Select("id", "request_id", "user_id", "token_id", "operation", "state", "created_time").Limit(0).Find(&rows).Error
}

// CreateImageExecutionRequest registers the request identity before any
// dispatch. A duplicate idempotency identity with a different body is refused.
func CreateImageExecutionRequest(entry *ImageExecution) error {
	if entry == nil || entry.RequestID == "" || len(entry.RequestID) > 128 || entry.UserID <= 0 || entry.Operation != ImageRequestOperation {
		return ErrImageExecutionState
	}
	now := time.Now().Unix()
	entry.State = ImageRequestCreated
	entry.CreatedTime, entry.UpdatedTime = now, now
	return DB.Transaction(func(tx *gorm.DB) error {
		if entry.IdempotencyScope != "" && entry.IdempotencyKeyDigest != "" {
			var existing ImageExecution
			err := tx.Where("idempotency_scope = ? AND idempotency_key_digest = ?", entry.IdempotencyScope, entry.IdempotencyKeyDigest).First(&existing).Error
			if err == nil {
				if existing.BodyDigest != entry.BodyDigest {
					return ErrImageExecutionIdempotent
				}
				entry.ID, entry.RequestID, entry.State = existing.ID, existing.RequestID, existing.State
				entry.CreatedTime = existing.CreatedTime
				return ErrImageExecutionConflict
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		return tx.Create(entry).Error
	})
}

// LoadImageExecutionByIdempotency resolves a repeated client request.
func LoadImageExecutionByIdempotency(scope, digest string) (*ImageExecution, error) {
	if scope == "" || digest == "" {
		return nil, ErrImageExecutionNotFound
	}
	var entry ImageExecution
	if err := DB.Where("idempotency_scope = ? AND idempotency_key_digest = ?", scope, digest).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrImageExecutionNotFound
		}
		return nil, err
	}
	return &entry, nil
}

// LoadImageExecution returns one request by its authoritative request ID.
func LoadImageExecution(requestID string) (*ImageExecution, error) {
	var entry ImageExecution
	if err := DB.Where("request_id = ?", requestID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrImageExecutionNotFound
		}
		return nil, err
	}
	return &entry, nil
}

// ListImageExecutionAttempts returns attempts ordered by sequence.
func ListImageExecutionAttempts(requestID string) ([]ImageExecutionAttempt, error) {
	var attempts []ImageExecutionAttempt
	err := DB.Where("request_id = ?", requestID).Order("sequence asc").Find(&attempts).Error
	return attempts, err
}

// AdvanceImageExecution applies a state transition only when the row still
// holds from. It is a compare-and-set, so concurrent coordinators cannot both
// claim a request or resurrect a terminal one.
func AdvanceImageExecution(requestID, from, to string, updates map[string]any) error {
	if requestID == "" || to == "" {
		return ErrImageExecutionState
	}
	columns := map[string]any{"state": to, "updated_time": time.Now().Unix()}
	for key, value := range updates {
		columns[key] = value
	}
	query := DB.Model(&ImageExecution{}).Where("request_id = ?", requestID)
	if from != "" {
		query = query.Where("state = ?", from)
	}
	result := query.Updates(columns)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// An idempotent repeat of the same terminal target is not a failure; any
		// other zero-row result means the compare-and-set lost.
		var current ImageExecution
		if err := DB.Where("request_id = ?", requestID).First(&current).Error; err != nil {
			return ErrImageExecutionNotFound
		}
		if from == "" && current.State == to {
			return nil
		}
		return ErrImageExecutionState
	}
	return nil
}

// CreateImageExecutionAttempt registers one attempt identity. The unique
// (request, attempt) index is the duplicate-dispatch authority.
func CreateImageExecutionAttempt(attempt *ImageExecutionAttempt) error {
	if attempt == nil || attempt.RequestID == "" || attempt.AttemptID == "" {
		return ErrImageExecutionState
	}
	now := time.Now().Unix()
	attempt.State = ImageAttemptCreated
	attempt.CreatedTime, attempt.UpdatedTime = now, now
	return DB.Create(attempt).Error
}

// BeginImageAttemptDispatch moves a CREATED attempt to DISPATCHING. This is the
// durable send intent: it must commit before the outbound body is written, and
// a repeat is a no-op.
func BeginImageAttemptDispatch(requestID, attemptID string) error {
	result := DB.Model(&ImageExecutionAttempt{}).
		Where("request_id = ? AND attempt_id = ? AND state = ?", requestID, attemptID, ImageAttemptCreated).
		Updates(map[string]any{"state": ImageAttemptDispatching, "updated_time": time.Now().Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var attempt ImageExecutionAttempt
	if err := DB.Where("request_id = ? AND attempt_id = ?", requestID, attemptID).First(&attempt).Error; err != nil {
		return ErrImageExecutionNotFound
	}
	if attempt.State == ImageAttemptDispatching {
		return nil
	}
	return ErrImageExecutionState
}

// MarkImageAttemptGatewaySent records the Gateway→Executor boundary. It is
// called after the attempt is already DISPATCHING, so a repeated call is a
// no-op rather than an error.
func MarkImageAttemptGatewaySent(requestID, attemptID string) error {
	result := DB.Model(&ImageExecutionAttempt{}).
		Where("request_id = ? AND attempt_id = ? AND state = ? AND (gateway_sent_at IS NULL OR gateway_sent_at = 0)", requestID, attemptID, ImageAttemptDispatching).
		Updates(map[string]any{"gateway_sent_at": time.Now().Unix(), "updated_time": time.Now().Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// Already recorded (an idempotent repeat) or the attempt is not in the
		// dispatching state at all.
		var attempt ImageExecutionAttempt
		if err := DB.Where("request_id = ? AND attempt_id = ?", requestID, attemptID).First(&attempt).Error; err != nil {
			return ErrImageExecutionNotFound
		}
		if attempt.GatewaySentAt > 0 {
			return nil
		}
		return ErrImageExecutionState
	}
	return nil
}

// MarkImageAttemptDispatched records the Executor→provider boundary. It can
// also promote DISPATCHING straight to DISPATCHED when a receipt proves it.
func MarkImageAttemptDispatched(requestID, attemptID string) error {
	result := DB.Model(&ImageExecutionAttempt{}).
		Where("request_id = ? AND attempt_id = ? AND state IN ?", requestID, attemptID, []string{ImageAttemptDispatching, ImageAttemptDispatched}).
		Updates(map[string]any{"executor_dispatched_at": time.Now().Unix(), "state": ImageAttemptDispatched, "updated_time": time.Now().Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrImageExecutionState
	}
	return nil
}

// FinalizeImageAttempt is the single terminal write. Repeating the identical
// terminal is a no-op; a conflicting terminal is refused so a late or duplicate
// receipt can never rewrite a settled outcome.
func FinalizeImageAttempt(requestID, attemptID, state string, updates map[string]any) (bool, error) {
	columns := map[string]any{"state": state, "updated_time": time.Now().Unix()}
	for key, value := range updates {
		columns[key] = value
	}
	var changed bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		var attempt ImageExecutionAttempt
		if err := tx.Where("request_id = ? AND attempt_id = ?", requestID, attemptID).First(&attempt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrImageExecutionNotFound
			}
			return err
		}
		if attempt.State == state {
			return nil
		}
		if attempt.State == ImageAttemptSucceeded || attempt.State == ImageAttemptFailed {
			return ErrImageExecutionState
		}
		result := tx.Model(&ImageExecutionAttempt{}).
			Where("request_id = ? AND attempt_id = ? AND state = ?", requestID, attemptID, attempt.State).
			Updates(columns)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrImageExecutionState
		}
		changed = true
		return nil
	})
	return changed, err
}

// BillingHoldImageExecution marks a request as awaiting reconciliation without
// settling it. The reservation stays active; only an explicit trusted terminal
// or an operator reconciliation may release it.
func BillingHoldImageExecution(requestID string) error {
	return AdvanceImageExecution(requestID, "", ImageRequestUnknown, map[string]any{"billing_state": "held_unknown"})
}

// ImageExecutionsPending lists requests that still own an open reservation.
func ImageExecutionsPending(limit int) ([]ImageExecution, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []ImageExecution
	err := DB.Where("state IN ?", []string{ImageRequestCreated, ImageRequestValidated, ImageRequestReserved, ImageRequestCancelReq, ImageRequestUnknown}).
		Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

// ImageExecutionJSON is the safe projection returned by the internal query API.
// It excludes bodies, credentials and account identities.
type ImageExecutionJSON struct {
	RequestID          string             `json:"request_id"`
	State              string             `json:"state"`
	Operation          string             `json:"operation"`
	Model              string             `json:"model"`
	RequestedCount     int                `json:"requested_count"`
	SourceStrategy     string             `json:"source_strategy"`
	BillingState       string             `json:"billing_state"`
	ResultState        string             `json:"result_state"`
	BillableImageCount int                `json:"billable_image_count"`
	ResultSHA256       string             `json:"result_sha256,omitempty"`
	EndReason          string             `json:"end_reason,omitempty"`
	UpdatedTime        int64              `json:"updated_time"`
	Attempts           []ImageAttemptJSON `json:"attempts"`
}

type ImageAttemptJSON struct {
	AttemptID          string `json:"attempt_id"`
	Sequence           int    `json:"sequence"`
	State              string `json:"state"`
	CandidateType      string `json:"candidate_type"`
	GatewaySent        bool   `json:"gateway_sent"`
	ExecutorDispatched bool   `json:"executor_dispatched"`
	ReceiptID          string `json:"receipt_id,omitempty"`
	ResultCount        int    `json:"result_count"`
	BillableImageCount int    `json:"billable_image_count"`
	EndReason          string `json:"end_reason,omitempty"`
}

// ProjectImageExecution builds the internal query projection.
func ProjectImageExecution(entry *ImageExecution, attempts []ImageExecutionAttempt) ImageExecutionJSON {
	view := ImageExecutionJSON{
		RequestID: entry.RequestID, State: entry.State, Operation: entry.Operation, Model: entry.Model,
		RequestedCount: entry.RequestedCount, SourceStrategy: entry.SourceStrategy, BillingState: entry.BillingState,
		ResultState: entry.ResultState, BillableImageCount: entry.BillableImageCount, ResultSHA256: entry.ResultSHA256,
		EndReason: entry.EndReason, UpdatedTime: entry.UpdatedTime, Attempts: make([]ImageAttemptJSON, 0, len(attempts)),
	}
	for _, attempt := range attempts {
		view.Attempts = append(view.Attempts, ImageAttemptJSON{
			AttemptID: attempt.AttemptID, Sequence: attempt.Sequence, State: attempt.State, CandidateType: attempt.CandidateType,
			GatewaySent: attempt.GatewaySentAt > 0, ExecutorDispatched: attempt.ExecutorDispatchedAt > 0,
			ReceiptID: attempt.ReceiptID, ResultCount: attempt.ResultCount, BillableImageCount: attempt.BillableImageCount,
			EndReason: attempt.EndReason,
		})
	}
	return view
}

// ImageRequestIdempotencyDigest normalises the client key so the stored value
// never contains client text.
func ImageRequestIdempotencyDigest(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// ImageRequestBodyDigest is the canonical body binding used to reject a
// reused idempotency key with a different request body.
func ImageRequestBodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
