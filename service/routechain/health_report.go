package routechain

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// Health report orchestration (Stage 5 design §2.6): turn one finished
// attempt into a provider-health verdict and apply it to the candidate's
// circuit. Memory-only on the breaker side (§二十九) — a ledger failure can
// never block these decisions, and nothing here can influence billing.

// UpstreamRetryAfterKey holds the raw Retry-After header value the transport
// captured on a 429 response (relay/channel/api_request.go). The value is
// parsed — and bounded — only here, never trusted verbatim.
const UpstreamRetryAfterKey = "upstream_retry_after"

const probeHeldKey = "route_chain_probe_held"

// retryAfterFallbackSeconds is the short conservative cooldown for a missing,
// negative, unparseable or overflowed Retry-After (§十八).
const retryAfterFallbackSeconds = 5

func markProbeHeld(c *gin.Context, candidateId int) {
	c.Set(probeHeldKey, candidateId)
}

func probeHeldFor(c *gin.Context, candidateId int) bool {
	value, exists := c.Get(probeHeldKey)
	if !exists {
		return false
	}
	held, _ := value.(int)
	return held == candidateId && candidateId > 0
}

func releaseProbeHeld(c *gin.Context) {
	c.Set(probeHeldKey, 0)
}

// ParseRetryAfterSeconds resolves a Retry-After value into a bounded cooldown
// (§十七/§十八): delta-seconds and HTTP-date forms are supported; anything
// negative, unparseable or overflowing falls back to the short conservative
// default; anything above maxSeconds is capped so a provider can never make
// a candidate permanently unavailable.
func ParseRetryAfterSeconds(raw string, maxSeconds int) int {
	if maxSeconds < 1 {
		maxSeconds = 1
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return retryAfterFallbackSeconds
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if seconds <= 0 {
			return retryAfterFallbackSeconds
		}
		if seconds > int64(maxSeconds) {
			return maxSeconds
		}
		return int(seconds)
	}
	if when, err := http.ParseTime(raw); err == nil {
		until := int64(time.Until(when).Seconds())
		if until <= 0 {
			return retryAfterFallbackSeconds
		}
		if until > int64(maxSeconds) {
			return maxSeconds
		}
		return int(until)
	}
	return retryAfterFallbackSeconds
}

// upstreamRetryAfterCooldown resolves the cooldown effect seconds for this
// attempt: only a 429 response feeds the cooldown, and only from the
// transport-captured header.
func upstreamRetryAfterCooldown(c *gin.Context, httpStatus int) int {
	if httpStatus != http.StatusTooManyRequests {
		return 0
	}
	return ParseRetryAfterSeconds(c.GetString(UpstreamRetryAfterKey), ResolveCircuitConfig(nil).RetryAfterMax)
}

func reportHealth(c *gin.Context, state *RequestState, signals HealthSignals) string {
	if state == nil || state.Snapshot == nil || !common.RouteCircuitBreakerEnabled() {
		return ""
	}
	candidate := state.FailingCandidate()
	if candidate == nil {
		return ""
	}
	verdict, reason := ClassifyHealthOutcome(signals)
	cooldownSeconds := upstreamRetryAfterCooldown(c, signals.Evidence.HTTPStatus)
	probeHeld := probeHeldFor(c, candidate.Id)
	effect := Breaker.ReportAttemptHealth(c, state.Snapshot.ChainId, candidate.Id, candidate.Policy,
		verdict, string(signals.Outcome), signals.Evidence.UpstreamErrorCode, cooldownSeconds, probeHeld)
	if probeHeld {
		releaseProbeHeld(c)
	}
	logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=breaker event=health_report request_id=%s chain_id=%d candidate_id=%d outcome=%s verdict=%s reason=%s effect=%s cooldown_seconds=%d",
		c.GetString(common.RequestIdKey), state.Snapshot.ChainId, candidate.Id, signals.Outcome, verdict, reason, effect, cooldownSeconds))
	return effect
}

// ReportAttemptHealthFailure applies the health verdict of a failed attempt.
// Called from chainRetryDecision while the attempt's evidence is fresh.
func ReportAttemptHealthFailure(c *gin.Context, state *RequestState, signals HealthSignals) string {
	return reportHealth(c, state, signals)
}

// ReportAttemptHealthSuccess applies the health verdict of the attempt that
// completed the request. The stream completion status decides between a real
// success and the SSE-abnormal failure (§四 A/B) — billing success never
// implies provider health (§三).
func ReportAttemptHealthSuccess(c *gin.Context, state *RequestState, relayInfo *relaycommon.RelayInfo) string {
	signals := HealthSignals{
		Outcome: OutcomeSuccess,
		Evidence: Evidence{
			CandidateSelected:    true,
			DispatchStarted:      true,
			RequestWritten:       true,
			ClientOutputObserved: c.Writer.Written(),
		},
		ContextErr: c.Request.Context().Err(),
	}
	if relayInfo != nil {
		signals.Stream = relayInfo.StreamStatus.OutcomeSnapshot()
	}
	return reportHealth(c, state, signals)
}
