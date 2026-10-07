package routechain

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// SetupChannelFunc mirrors middleware.SetupContextForSelectedChannel so the
// engine can run per-candidate channel admission (pool gates included)
// without importing middleware (dependency direction: middleware/controller
// orchestrate, this package decides).
type SetupChannelFunc func(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError

// PoolChannelProvenanceScopeKey records on the request context that the
// currently bound channel is pool-bound (Stage 7.1 provenance scope).
const PoolChannelProvenanceScopeKey = "route_chain_pool_provenance_scope"

// SetPoolProvenanceScope marks whether the last bound attempt ran on a
// pool-bound executor channel. BindCandidate sets it from the bound channel;
// the decision-matrix test sets it explicitly to model the real path.
func SetPoolProvenanceScope(c *gin.Context, poolBound bool) {
	if c == nil {
		return
	}
	c.Set(PoolChannelProvenanceScopeKey, poolBound)
}

// PoolProvenanceScopeFrom reports whether the last bound attempt ran on a
// pool-bound executor channel. Only then may the executor provenance header
// classify a pool-exhaustion outcome.
func PoolProvenanceScopeFrom(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return c.GetBool(PoolChannelProvenanceScopeKey)
}

// ChainFilters strips the model_source filter: in chain mode source
// compatibility is owned by the candidate-type check (R28) plus the
// per-candidate STEP-4 admission gates, so the blanket isolation filter must
// not also reject the cross-source candidates an operator explicitly chained.
func ChainFilters(filters []dto.ChannelFilter) []dto.ChannelFilter {
	kept := make([]dto.ChannelFilter, 0, len(filters))
	for _, filter := range filters {
		if filter.Kind == dto.FilterModelSource {
			continue
		}
		kept = append(kept, filter)
	}
	return kept
}

// RouteExhaustedError is the internal reason string for a fully consumed
// chain; it never reaches the client body.
func RouteExhaustedError() string {
	return "all route candidates exhausted for the requested model"
}

// RouteExhaustedClientError renders the deterministic client-facing error for
// an exhausted chain (R29): stable, protocol-compatible, free of internal
// channel ids, credentials, pool accounts and topology. Details stay in the
// server logs under the request id.
func RouteExhaustedClientError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(RouteExhaustedError()),
		types.ErrorCodeRouteExhausted,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

// BindCandidate selects and binds the next eligible candidate to its exact
// target channel (Amendment 2 C2). Selection failures skip forward as route
// decision records (C5); the first candidate that passes exact-binding checks
// AND channel admission AND the runtime circuit breaker is marked as the
// active attempt. A nil return means a candidate is bound and ready to
// dispatch; a non-nil return means the chain is exhausted and the request
// must end with RouteExhaustedClientError.
//
// The breaker is the LAST availability filter (Stage 5 §2.5): it runs after
// the config/trust/admission checks, can only remove availability, and an
// open candidate is skipped as a route decision (no attempt row, no budget
// consumption, no upstream dispatch).
func (s *RequestState) BindCandidate(c *gin.Context, setup SetupChannelFunc, groups []string, filters []dto.ChannelFilter) *types.NewAPIError {
	for {
		index, ok := s.NextCandidate()
		if !ok {
			logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=engine event=chain_exhausted chain_id=%d chain_version=%d attempts=%d consumed=%d", s.Snapshot.ChainId, s.Snapshot.Version, s.attemptsMade, len(s.visited)))
			return RouteExhaustedClientError()
		}
		candidate := s.Snapshot.Candidates[index]
		channel, selectErr := s.SelectCandidate(c, index, groups, filters)
		if selectErr != nil {
			s.MarkSkipped(c, index, selectErr.Error())
			continue
		}
		if setupErr := setup(c, channel, s.Snapshot.Model); setupErr != nil {
			// Admission failure is pre-dispatch (zero risk): skip forward.
			s.MarkSkipped(c, index, "admission_failed:"+setupErr.Error())
			continue
		}
		// Stage 7.1 provenance scope: the executor provenance header is only
		// trusted on the pool-executor path. Record whether the bound channel
		// is pool-bound (executor-authority match) so evidence assembly can
		// scope the pool-exhaustion classification; an API_CHANNEL upstream
		// returning a spoofed header never enters the trust scope.
		SetPoolProvenanceScope(c, channel.PoolChannelBound())
		if common.RouteCircuitBreakerEnabled() {
			allowed, reason, probeHeld := Breaker.AllowDispatch(c, s.Snapshot.ChainId, candidate.Id, candidate.Policy)
			if !allowed {
				s.MarkSkipped(c, index, reason)
				continue
			}
			if probeHeld {
				markProbeHeld(c, candidate.Id)
			}
		}
		s.MarkAttempt(c, index)
		return nil
	}
}

// Gin context flag keys shared with the relay transport layer. They are
// defined at the write sites (relay/channel/api_request.go,
// common/upstream_body.go, service/relay_retry_safety.go) as raw strings.
const (
	flagUpstreamAttempted      = "upstream_attempted"
	flagUpstreamUsageUncertain = "upstream_usage_uncertain"
)

// ResetPerAttemptFlags clears the cross-attempt dispatch/uncertainty residue
// between chain candidates or before the final zero-work refund. This is safe
// ONLY when ShouldRefundZeroWork proves the attempt did no work,
// so hiding its flags from the request-end billing read
// can never mask uncertain consumption. Chain mode only — legacy requests
// never call this.
func ResetPerAttemptFlags(c *gin.Context) {
	c.Set(flagUpstreamAttempted, false)
	c.Set(flagUpstreamUsageUncertain, false)
}

// LogDecision emits the structured failover decision record (requirement §30):
// request id and candidate decisions with reason codes, never credentials.
func LogDecision(c *gin.Context, state *RequestState, decision Decision, outcome Outcome) {
	if state == nil || state.Snapshot == nil {
		return
	}
	candidateId := 0
	if failing := state.FailingCandidate(); failing != nil {
		candidateId = failing.Id
	}
	record := fmt.Sprintf("route_chain subsystem=decision event=failover_decision chain_id=%d chain_version=%d candidate_id=%d outcome=%s decision=%s reason=%s terminal=%t remaining_candidates=%d",
		state.Snapshot.ChainId, state.Snapshot.Version, candidateId, outcome, allowedLabel(decision), decision.Reason, decision.Terminal, state.AttemptBudget()-state.AttemptsMade())
	if decision.Allowed {
		logger.LogInfo(c, record)
		return
	}
	logger.LogWarn(c, record)
}

func allowedLabel(decision Decision) string {
	if decision.Allowed {
		return "advance"
	}
	return "stop"
}
