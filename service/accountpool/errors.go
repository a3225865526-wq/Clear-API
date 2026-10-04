package accountpool

import "github.com/QuantumNous/new-api/model"

// Package-level aliases of the pool error values so adapter code can reference
// them without repeating the model qualifier. They are the same values the
// controller maps to HTTP codes via errors.Is.
var (
	ErrPoolInvalid          = model.ErrPoolInvalid
	ErrPoolNotFound         = model.ErrPoolNotFound
	ErrPoolConflict         = model.ErrPoolConflict
	ErrPoolUnsupported      = model.ErrPoolUnsupported
	ErrPoolExecutorOffline  = model.ErrPoolExecutorOffline
	ErrPoolExecutorFailed   = model.ErrPoolExecutorFailed
	ErrPoolUnavailable      = model.ErrPoolUnavailable
	ErrPoolTaskExpired      = model.ErrPoolTaskExpired
	ErrPoolTaskConsumed     = model.ErrPoolTaskConsumed
	ErrPoolIdentityMismatch = model.ErrPoolIdentityMismatch
	ErrPoolNotAdmitted      = model.ErrPoolNotAdmitted
	ErrPoolIsolationReq     = model.ErrPoolIsolationRequired
	ErrPoolCorrelationFail  = model.ErrPoolCorrelationFailed
	ErrPoolAuthTimeout      = model.ErrPoolAuthTimeout
)
