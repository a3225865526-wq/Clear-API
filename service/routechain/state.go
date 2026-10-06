package routechain

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

const requestStateKey = "route_chain_request_state"

// RequestState drives one request through an immutable Snapshot. It is the
// only cross-candidate scheduler for chain-routed requests (Amendment 2 C1):
// candidate indices move strictly forward, so a candidate can never be
// revisited (R20) and the legacy retry ladder must not be consulted in chain
// mode.
type RequestState struct {
	Snapshot *Snapshot

	nextIndex      int
	attemptsMade   int
	visited        map[int]bool // candidate ids consumed this request (attempts + skips)
	succeeded      bool
	lastAttemptIdx int
	hasLastAttempt bool
}

// BeginRequest resolves the chain snapshot for the request and stores the
// request state. It returns nil when the request must keep legacy routing
// (gate off / no chain / corrupt config) — callers must treat nil as "chain
// fully inert" (R1/R2).
func BeginRequest(c *gin.Context, modelName string, groupName string) *RequestState {
	snapshot := Resolve(c, modelName, groupName)
	if snapshot == nil {
		return nil
	}
	state := &RequestState{Snapshot: snapshot, visited: make(map[int]bool)}
	c.Set(requestStateKey, state)
	logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=engine event=snapshot_loaded chain_id=%d chain_version=%d candidates=%d cross_source=%t", snapshot.ChainId, snapshot.Version, len(snapshot.Candidates), snapshot.AllowCrossSource))
	return state
}

// CrossSourceChainActive reports whether the request is chain-routed with the
// operator's explicit allow_cross_source_failover permission (design §2.8).
// Only such requests may relax the blanket pool/api source isolation at the
// per-candidate admission gate; legacy requests and same-source chains stay
// fully fail-closed.
func CrossSourceChainActive(c *gin.Context) bool {
	state := StateFrom(c)
	return state != nil && state.Snapshot != nil && state.Snapshot.AllowCrossSource
}

// StateFrom returns the active request state or nil for legacy requests.
func StateFrom(c *gin.Context) *RequestState {
	value, exists := c.Get(requestStateKey)
	if !exists {
		return nil
	}
	state, _ := value.(*RequestState)
	return state
}

// AttemptBudget is the number of upstream attempts the chain may make: every
// enabled candidate once, capped by the global safety option (R21, C1).
// Selection-phase skips do not consume the budget — a candidate that was
// never dispatched is a route decision record, not an attempt (C5).
func (s *RequestState) AttemptBudget() int {
	if s == nil || s.Snapshot == nil {
		return 0
	}
	budget := len(s.Snapshot.Candidates)
	if max := common.RouteChainMaxCandidates; max > 0 && budget > max {
		budget = max
	}
	if budget < 0 {
		budget = 0
	}
	return budget
}

// NextCandidate claims the next unvisited candidate index. It returns false
// when the chain is exhausted (attempt budget consumed or candidates ran
// out). The scan is strictly forward, so A→B→A is structurally impossible.
func (s *RequestState) NextCandidate() (int, bool) {
	if s == nil || s.Snapshot == nil || s.attemptsMade >= s.AttemptBudget() {
		return 0, false
	}
	for idx := s.nextIndex; idx < len(s.Snapshot.Candidates); idx++ {
		if s.visited[s.Snapshot.Candidates[idx].Id] {
			continue
		}
		s.nextIndex = idx
		return idx, true
	}
	return 0, false
}

// MarkAttempt records that the candidate at index is about to be dispatched
// (C5: only a real upstream send counts as an attempt).
func (s *RequestState) MarkAttempt(c *gin.Context, index int) {
	if s == nil || s.Snapshot == nil || index < 0 || index >= len(s.Snapshot.Candidates) {
		return
	}
	candidate := s.Snapshot.Candidates[index]
	if s.visited == nil {
		s.visited = make(map[int]bool)
	}
	s.visited[candidate.Id] = true
	s.attemptsMade++
	s.nextIndex = index + 1
	s.lastAttemptIdx = index
	s.hasLastAttempt = true
	logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=engine event=candidate_selected chain_id=%d chain_version=%d candidate_id=%d type=%s target=%d attempt=%d", s.Snapshot.ChainId, s.Snapshot.Version, candidate.Id, candidate.Type, candidate.TargetId, s.attemptsMade))
}

// MarkSkipped records a selection-phase exclusion (C5): the candidate was
// never dispatched, so this produces a decision record only.
func (s *RequestState) MarkSkipped(c *gin.Context, index int, reason string) {
	if s == nil || s.Snapshot == nil || index < 0 || index >= len(s.Snapshot.Candidates) {
		return
	}
	candidate := s.Snapshot.Candidates[index]
	if s.visited == nil {
		s.visited = make(map[int]bool)
	}
	s.visited[candidate.Id] = true
	if s.nextIndex <= index {
		s.nextIndex = index + 1
	}
	logger.LogInfo(c, fmt.Sprintf("route_chain subsystem=engine event=candidate_skipped chain_id=%d chain_version=%d candidate_id=%d type=%s target=%d reason=%q", s.Snapshot.ChainId, s.Snapshot.Version, candidate.Id, candidate.Type, candidate.TargetId, reason))
}

// MarkSuccess records that the request completed on the current candidate.
func (s *RequestState) MarkSuccess() {
	if s != nil {
		s.succeeded = true
	}
}

// Succeeded reports whether the request finished successfully.
func (s *RequestState) Succeeded() bool { return s == nil || s.succeeded }

// AttemptsMade reports how many upstream sends the chain made.
func (s *RequestState) AttemptsMade() int { return s.attemptsMade }

// ConsumedCandidates counts candidates the request consumed (attempts plus
// selection-phase skips). Route-exhausted sanitization applies once more than
// one candidate was consumed, keeping single-candidate chains byte-identical
// to legacy error behavior.
func (s *RequestState) ConsumedCandidates() int { return len(s.visited) }

// FailingCandidate returns the candidate the last attempt was made on — its
// failure_policy governs whether the request may fail over.
func (s *RequestState) FailingCandidate() *Candidate {
	if s == nil || s.Snapshot == nil || !s.hasLastAttempt {
		return nil
	}
	if s.lastAttemptIdx < 0 || s.lastAttemptIdx >= len(s.Snapshot.Candidates) {
		return nil
	}
	return &s.Snapshot.Candidates[s.lastAttemptIdx]
}
