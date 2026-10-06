package routechain

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Runtime circuit breaker for route chain candidates (Stage 5 design §2.4).
//
// It is an availability filter ONLY — never an authorization mechanism
// (requirement §二). State is memory-only per process: restart resets every
// circuit to the probeable CLOSED/UNKNOWN state (§二十二), no ledger scan
// rebuilds OPEN at startup (§二十三), and multi-instance deployments each
// keep independent state — not cluster-coordinated, acceptable under the
// single-instance production constraint (§二十四, Part 1 Q17).
//
// DISABLED is not a breaker state (§六): candidate enabled=false is persistent
// configuration resolved at snapshot time; the breaker owns runtime
// availability only and never writes channel or candidate configuration.

const (
	CircuitStateClosed   = "closed"
	CircuitStateOpen     = "open"
	CircuitStateHalfOpen = "half_open"
)

// Breaker effect labels — recorded on the attempt ledger's breaker_effect
// column and in the observability counters (§二十八, §三十五).
const (
	BreakerEffectSuccess  = "success"
	BreakerEffectFailure  = "failure"
	BreakerEffectNeutral  = "neutral"
	BreakerEffectCooldown = "cooldown"
)

// Selection-skip reasons surfaced as route decision records through
// MarkSkipped (§二十七: a circuit-open skip is a route decision, never an
// attempt).
const (
	SkipReasonCircuitOpen    = "circuit_open"
	SkipReasonHalfOpenProbes = "circuit_half_open_probe_limit"
)

// CircuitConfig is the resolved per-candidate breaker configuration: global
// option defaults with the Stage 2 RouteCandidatePolicy.Health override
// applied (§二十 — no new policy surface).
type CircuitConfig struct {
	FailureThreshold  int
	CooldownSeconds   int
	HalfOpenMaxProbes int
	RetryAfterMax     int
}

func clampInt(v, min, max, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// ResolveCircuitConfig resolves the effective breaker configuration for one
// candidate. Clamps mirror the policy validation bounds
// (model/route_chain.go ParseRouteCandidatePolicy).
func ResolveCircuitConfig(policy *model.RouteCandidatePolicy) CircuitConfig {
	cfg := CircuitConfig{
		FailureThreshold:  clampInt(common.RouteChainCircuitFailureThreshold, 1, 20, 3),
		CooldownSeconds:   clampInt(common.RouteChainCircuitOpenSeconds, 1, 3600, 60),
		HalfOpenMaxProbes: clampInt(common.RouteChainCircuitHalfOpenMaxProbes, 1, 10, 1),
		RetryAfterMax:     clampInt(common.RouteChainRetryAfterMaxSeconds, 1, 3600, 300),
	}
	if policy != nil && policy.Health != nil {
		if policy.Health.FailureThreshold > 0 {
			cfg.FailureThreshold = clampInt(policy.Health.FailureThreshold, 1, 20, cfg.FailureThreshold)
		}
		if policy.Health.CooldownSeconds > 0 {
			cfg.CooldownSeconds = clampInt(policy.Health.CooldownSeconds, 1, 3600, cfg.CooldownSeconds)
		}
	}
	return cfg
}

// probeLease bounds how long a HALF_OPEN probe slot stays claimed when the
// probing attempt died without ever reporting (panic paths). Long enough for
// any live SSE attempt, short enough to self-heal deterministically.
func probeLease(cooldownSeconds int) int64 {
	lease := 2 * cooldownSeconds
	if lease < 60 {
		lease = 60
	}
	return int64(lease)
}

type circuit struct {
	state               string
	consecutiveFailures int
	openedAt            int64
	openUntil           int64
	activeProbes        int
	probeLeaseUntil     int64
	forced              bool
	everObserved        bool

	lastSuccessAt     int64
	lastFailureAt     int64
	lastFailureClass  string
	lastFailureCode   string
	lastHealthOutcome string

	// per-circuit observability counters (§三十五)
	opens               int64
	halfOpens           int64
	closes              int64
	skipsOpen           int64
	probeRejects        int64
	retryAfterCooldowns int64
	healthSuccess       int64
	healthFailure       int64
	healthNeutral       int64
}

func (c *circuit) counters() map[string]int64 {
	return map[string]int64{
		"circuit_open_total":          c.opens,
		"circuit_half_open_total":     c.halfOpens,
		"circuit_close_total":         c.closes,
		"candidate_skipped_open_total": c.skipsOpen,
		"half_open_probe_rejected_total": c.probeRejects,
		"retry_after_cooldown_total":  c.retryAfterCooldowns,
		"health_success_total":        c.healthSuccess,
		"health_failure_total":        c.healthFailure,
		"health_neutral_total":        c.healthNeutral,
	}
}

// CircuitSnapshot is the owner-facing copy of one candidate's runtime
// breaker state (§三十). No credential or topology material — channel ids on
// this view are acceptable because the consumer is the owner-only API.
type CircuitSnapshot struct {
	State               string           `json:"state"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	FailureThreshold    int              `json:"failure_threshold"`
	CooldownSeconds     int              `json:"cooldown_seconds"`
	OpenedAt            int64            `json:"opened_at"`
	OpenUntil           int64            `json:"open_until"`
	ActiveProbes        int              `json:"half_open_active_probes"`
	MaxProbes           int              `json:"half_open_max_probes"`
	Forced              bool             `json:"forced"`
	LastSuccessAt       int64            `json:"last_success_at"`
	LastFailureAt       int64            `json:"last_failure_at"`
	LastFailureClass    string           `json:"last_failure_class"`
	LastFailureCode     string           `json:"last_failure_code"`
	LastHealthOutcome   string           `json:"last_health_outcome"`
	Counters            map[string]int64 `json:"counters"`
}

// HealthSummary derives the presentation state (§三十六, §三十七) from the
// runtime state alone; the DISABLED summary is combined at the API layer from
// the persistent candidate config. It is a display value — never an
// authorization source and never a routing input.
func (s CircuitSnapshot) HealthSummary() string {
	switch s.State {
	case CircuitStateOpen:
		return "CIRCUIT_OPEN"
	case CircuitStateHalfOpen:
		return "HALF_OPEN"
	case CircuitStateClosed:
		if s.ConsecutiveFailures > 0 {
			return "DEGRADED"
		}
		if s.LastSuccessAt > 0 || s.LastFailureAt > 0 {
			return "HEALTHY"
		}
		return "UNKNOWN"
	}
	return "UNKNOWN"
}

// BreakerRegistry holds the per-candidate runtime circuits. One coarse mutex
// guards the map and every entry — deterministic under concurrency (§二十五);
// relay-path contention is irrelevant at chain-routed volumes.
type BreakerRegistry struct {
	mu       sync.Mutex
	circuits map[string]*circuit
	now      func() int64
}

// Breaker is the process-wide registry (memory-only, §二十二).
var Breaker = &BreakerRegistry{circuits: map[string]*circuit{}, now: func() int64 { return time.Now().Unix() }}

func circuitKey(chainId, candidateId int) string {
	return fmt.Sprintf("%d:%d", chainId, candidateId)
}

func (r *BreakerRegistry) entry(chainId, candidateId int) *circuit {
	key := circuitKey(chainId, candidateId)
	c, ok := r.circuits[key]
	if !ok {
		c = &circuit{state: CircuitStateClosed}
		r.circuits[key] = c
	}
	return c
}

func breakerLog(c *gin.Context, msg string) {
	// A typed-nil *gin.Context inside an interface is not nil — route nil
	// explicitly to the request-less logger (registry-owned transitions).
	if c == nil {
		common.SysLog(fmt.Sprintf("route_chain subsystem=breaker %s", msg))
		return
	}
	logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=breaker %s", msg))
}

// lazyTransition advances OPEN→HALF_OPEN once the cooldown window has
// elapsed (§九). Lazy evaluation keeps the state machine free of timers.
// Caller must hold r.mu.
func (r *BreakerRegistry) lazyTransition(c *circuit, now int64) {
	if c.state == CircuitStateOpen && c.openUntil > 0 && now >= c.openUntil {
		c.state = CircuitStateHalfOpen
		c.halfOpens++
		c.activeProbes = 0
		c.probeLeaseUntil = 0
		breakerLog(nil, fmt.Sprintf("event=half_open opened_at=%d cooldown_elapsed=true", c.openedAt))
	}
}

// AllowDispatch decides whether the candidate may take a real upstream send
// right now (§八, §九, §二十六). probeHeld=true means the caller acquired one
// HALF_OPEN probe slot and MUST report the attempt's health so the slot is
// released.
func (r *BreakerRegistry) AllowDispatch(c *gin.Context, chainId, candidateId int, policy *model.RouteCandidatePolicy) (allowed bool, reason string, probeHeld bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entry(chainId, candidateId)
	now := r.now()
	r.lazyTransition(entry, now)
	switch entry.state {
	case CircuitStateClosed:
		return true, "", false
	case CircuitStateOpen:
		entry.skipsOpen++
		return false, SkipReasonCircuitOpen, false
	default: // half_open
		cfg := ResolveCircuitConfig(policy)
		if entry.activeProbes > 0 && now >= entry.probeLeaseUntil {
			// Leaked probe self-heal: the lease expired, so whatever held it
			// can no longer be in flight — reclaim the slot deterministically.
			entry.activeProbes = 0
		}
		if entry.activeProbes >= cfg.HalfOpenMaxProbes {
			entry.probeRejects++
			return false, SkipReasonHalfOpenProbes, false
		}
		entry.activeProbes++
		entry.probeLeaseUntil = now + probeLease(cfg.CooldownSeconds)
		return true, "", true
	}
}

// applyCooldown records a 429-derived open-until window (§十七): it never
// touches consecutiveFailures. Caller must hold r.mu.
func (r *BreakerRegistry) applyCooldown(entry *circuit, now int64, cooldownSeconds int) {
	entry.state = CircuitStateOpen
	if entry.openedAt == 0 {
		entry.openedAt = now
	}
	if until := now + int64(cooldownSeconds); until > entry.openUntil {
		entry.openUntil = until
	}
	entry.activeProbes = 0
	entry.probeLeaseUntil = 0
}

// transitionOpen moves the circuit into OPEN on failure evidence. Caller must
// hold r.mu.
func (r *BreakerRegistry) transitionOpen(entry *circuit, now int64, cooldownSeconds int, reason string) {
	entry.state = CircuitStateOpen
	entry.openedAt = now
	entry.openUntil = now + int64(cooldownSeconds)
	entry.opens++
	entry.activeProbes = 0
	entry.probeLeaseUntil = 0
	breakerLog(nil, fmt.Sprintf("event=circuit_open reason=%s consecutive_failures=%d cooldown_seconds=%d open_until=%d", reason, entry.consecutiveFailures, cooldownSeconds, entry.openUntil))
}

func (r *BreakerRegistry) transitionClosed(entry *circuit, now int64, reason string) {
	entry.state = CircuitStateClosed
	entry.openUntil = 0
	entry.openedAt = 0
	entry.consecutiveFailures = 0
	entry.activeProbes = 0
	entry.probeLeaseUntil = 0
	entry.forced = false
	entry.closes++
	breakerLog(nil, fmt.Sprintf("event=circuit_close reason=%s", reason))
}

// ReportAttemptHealth applies one attempt's health verdict to the candidate's
// circuit and returns the breaker effect for the ledger's breaker_effect
// column. The breaker updates in memory unconditionally — a ledger write
// failure never blocks it (§二十九), and nothing here can influence billing.
//
// cooldownSeconds > 0 marks a 429-derived cooldown (§十七); it overrides the
// state machine's failure path only in the sense the design assigns it: it
// opens/extends the open window without incrementing failure counters.
func (r *BreakerRegistry) ReportAttemptHealth(c *gin.Context, chainId, candidateId int, policy *model.RouteCandidatePolicy, verdict HealthOutcome, failureClass, failureCode string, cooldownSeconds int, probeHeld bool) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entry(chainId, candidateId)
	cfg := ResolveCircuitConfig(policy)
	now := r.now()
	r.lazyTransition(entry, now)
	entry.everObserved = true
	if probeHeld && entry.activeProbes > 0 {
		entry.activeProbes--
	}
	if cooldownSeconds > 0 && cooldownSeconds > cfg.RetryAfterMax {
		cooldownSeconds = cfg.RetryAfterMax
	}
	effect := BreakerEffectNeutral
	switch verdict {
	case HealthSuccess:
		entry.healthSuccess++
		entry.lastSuccessAt = now
		entry.lastHealthOutcome = string(HealthSuccess)
		if entry.state != CircuitStateClosed {
			r.transitionClosed(entry, now, "health_success")
		}
		entry.consecutiveFailures = 0
		effect = BreakerEffectSuccess
	case HealthFailure:
		entry.healthFailure++
		entry.lastFailureAt = now
		entry.lastFailureClass = failureClass
		entry.lastFailureCode = failureCode
		entry.lastHealthOutcome = string(HealthFailure)
		effect = BreakerEffectFailure
		switch entry.state {
		case CircuitStateClosed:
			entry.consecutiveFailures++
			if entry.consecutiveFailures >= cfg.FailureThreshold {
				r.transitionOpen(entry, now, cfg.CooldownSeconds, "failure_threshold")
			}
		case CircuitStateHalfOpen:
			// Probe failed provider-attributably — reopen with a fresh
			// cooldown (§十一); the request-side no-replay rules are untouched
			// (§十二).
			r.transitionOpen(entry, now, cfg.CooldownSeconds, "half_open_probe_failed")
		default: // open: record evidence, state unchanged
		}
	default: // neutral (§三十八): neither increments nor resets failures
		entry.healthNeutral++
		entry.lastHealthOutcome = string(HealthNeutral)
	}
	if cooldownSeconds > 0 && verdict != HealthSuccess {
		// 429 cooldown effect (§十七/§十九): a rate-limit verdict proves the
		// provider answered — bounded open-until, no failure counting.
		r.applyCooldown(entry, now, cooldownSeconds)
		entry.retryAfterCooldowns++
		effect = BreakerEffectCooldown
		breakerLog(c, fmt.Sprintf("event=retry_after_cooldown chain_id=%d candidate_id=%d cooldown_seconds=%d open_until=%d", chainId, candidateId, cooldownSeconds, entry.openUntil))
	}
	return effect
}

// ResetCandidate clears the runtime breaker state back to the probeable
// CLOSED state (§三十一). Persistent configuration (candidate enabled, trust,
// capability, pricing, publication) is untouched. Aggregate counters survive
// for audit; the per-candidate last-observation fields are runtime state and
// clear with everything else.
func (r *BreakerRegistry) ResetCandidate(c *gin.Context, chainId, candidateId int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entry(chainId, candidateId)
	entry.state = CircuitStateClosed
	entry.consecutiveFailures = 0
	entry.openedAt = 0
	entry.openUntil = 0
	entry.activeProbes = 0
	entry.probeLeaseUntil = 0
	entry.forced = false
	entry.lastSuccessAt = 0
	entry.lastFailureAt = 0
	entry.lastFailureClass = ""
	entry.lastFailureCode = ""
	entry.lastHealthOutcome = ""
	breakerLog(c, fmt.Sprintf("event=circuit_reset chain_id=%d candidate_id=%d", chainId, candidateId))
}

// ForceOpen applies the owner's transient runtime override (§三十二). It is
// memory-only and clears on restart or when its window elapses; persistent
// disable is candidate enabled=false, never this.
func (r *BreakerRegistry) ForceOpen(c *gin.Context, chainId, candidateId int, durationSeconds int) {
	if durationSeconds <= 0 {
		durationSeconds = clampInt(common.RouteChainCircuitOpenSeconds, 1, 3600, 60)
	}
	if durationSeconds > 3600 {
		durationSeconds = 3600
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entry(chainId, candidateId)
	now := r.now()
	entry.state = CircuitStateOpen
	entry.openedAt = now
	entry.openUntil = now + int64(durationSeconds)
	entry.opens++
	entry.forced = true
	entry.activeProbes = 0
	entry.probeLeaseUntil = 0
	breakerLog(c, fmt.Sprintf("event=circuit_force_open chain_id=%d candidate_id=%d duration_seconds=%d open_until=%d", chainId, candidateId, durationSeconds, entry.openUntil))
}

// CandidateSnapshot copies one candidate's runtime breaker state.
func (r *BreakerRegistry) CandidateSnapshot(chainId, candidateId int, policy *model.RouteCandidatePolicy) CircuitSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entry(chainId, candidateId)
	cfg := ResolveCircuitConfig(policy)
	now := r.now()
	r.lazyTransition(entry, now)
	snapshot := CircuitSnapshot{
		State:               entry.state,
		ConsecutiveFailures: entry.consecutiveFailures,
		FailureThreshold:    cfg.FailureThreshold,
		CooldownSeconds:     cfg.CooldownSeconds,
		OpenedAt:            entry.openedAt,
		OpenUntil:           entry.openUntil,
		ActiveProbes:        entry.activeProbes,
		MaxProbes:           cfg.HalfOpenMaxProbes,
		Forced:              entry.forced,
		LastSuccessAt:       entry.lastSuccessAt,
		LastFailureAt:       entry.lastFailureAt,
		LastFailureClass:    entry.lastFailureClass,
		LastFailureCode:     entry.lastFailureCode,
		LastHealthOutcome:   entry.lastHealthOutcome,
		Counters:            entry.counters(),
	}
	return snapshot
}

// GlobalCounters aggregates the §三十五 counters over every circuit this
// process has observed.
func (r *BreakerRegistry) GlobalCounters() map[string]int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	totals := map[string]int64{}
	for _, entry := range r.circuits {
		for name, value := range entry.counters() {
			totals[name] += value
		}
	}
	return totals
}
