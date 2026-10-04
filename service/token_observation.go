package service

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const tokenObservationKey = "private_token_observation"

// TokenObservation is an accounting observation, not a new billing rule.
// Pointers distinguish explicit upstream zero from an omitted field. Raw only
// contains numeric meter fields, never response text, credentials or tools.
type TokenObservation struct {
	Source        string           `json:"source"`
	Quality       string           `json:"quality"`
	Input         *int64           `json:"input"`
	Output        *int64           `json:"output"`
	CacheRead     *int64           `json:"cache_read"`
	CacheWrite    *int64           `json:"cache_write"`
	Reasoning     *int64           `json:"reasoning"`
	Total         *int64           `json:"total"`
	TotalDerived  bool             `json:"total_derived"`
	Raw           map[string]int64 `json:"raw"`
	Protocol      string           `json:"protocol"`
	Interrupted   bool             `json:"interrupted"`
	InternalModel string           `json:"internal_model,omitempty"`
	ModelAlias    string           `json:"model_alias,omitempty"`
}

// ParseTokenObservation reads meter presence before typed DTOs default omitted
// fields to zero. Caches/reasoning are subsets in OpenAI, extra input counters
// in Anthropic. They are never blindly added to both input and total.
func ParseTokenObservation(protocol string, payload []byte) TokenObservation {
	o := TokenObservation{Source: "upstream_response", Quality: "missing", Protocol: protocol, Raw: map[string]int64{}}
	var root map[string]json.RawMessage
	if common.Unmarshal(payload, &root) != nil {
		o.Quality = "inconsistent"
		return o
	}
	usage := root["usage"]
	for _, parent := range []string{"response", "message"} {
		if len(usage) != 0 {
			break
		}
		var nested map[string]json.RawMessage
		if common.Unmarshal(root[parent], &nested) == nil {
			usage = nested["usage"]
		}
	}
	var fields map[string]json.RawMessage
	if len(usage) == 0 || string(usage) == "null" {
		return o
	}
	if common.Unmarshal(usage, &fields) != nil {
		o.Quality = "inconsistent"
		return o
	}
	invalid := false
	read := func(path string) *int64 {
		parts := strings.Split(path, ".")
		node := fields
		for _, part := range parts[:len(parts)-1] {
			var next map[string]json.RawMessage
			if common.Unmarshal(node[part], &next) != nil {
				if value, exists := node[part]; exists && string(value) != "null" {
					invalid = true
				}
				return nil
			}
			node = next
		}
		raw, exists := node[parts[len(parts)-1]]
		if !exists || string(raw) == "null" {
			return nil
		}
		var value int64
		if common.Unmarshal(raw, &value) != nil || value < 0 || value > 1_000_000_000_000 {
			invalid = true
			return nil
		}
		o.Raw[path] = value
		return &value
	}
	o.Input = read("input_tokens")
	if other := read("prompt_tokens"); other != nil {
		if o.Input != nil && *o.Input != *other {
			invalid = true
		}
		o.Input = other
	}
	o.Output = read("output_tokens")
	if other := read("completion_tokens"); other != nil {
		if o.Output != nil && *o.Output != *other {
			invalid = true
		}
		o.Output = other
	}
	o.Total = read("total_tokens")
	if protocol == "anthropic" {
		o.CacheRead, o.CacheWrite = read("cache_read_input_tokens"), read("cache_creation_input_tokens")
		o.Reasoning = read("output_tokens_details.thinking_tokens")
	} else {
		o.CacheRead, o.CacheWrite = read("prompt_tokens_details.cached_tokens"), read("prompt_tokens_details.cache_write_tokens")
		if o.CacheRead == nil {
			o.CacheRead = read("input_tokens_details.cached_tokens")
		}
		if o.CacheWrite == nil {
			o.CacheWrite = read("input_tokens_details.cache_write_tokens")
		}
		o.Reasoning = read("completion_tokens_details.reasoning_tokens")
		if o.Reasoning == nil {
			o.Reasoning = read("output_tokens_details.reasoning_tokens")
		}
	}
	if invalid {
		o.Quality = "inconsistent"
		return o
	}
	classifyTokenObservation(&o)
	return o
}

func classifyTokenObservation(o *TokenObservation) {
	if o.Quality == "inconsistent" || o.Quality == "local_estimate" {
		return
	}
	o.Quality = "missing"
	if len(o.Raw) > 0 {
		o.Quality = "upstream_partial"
	}
	if o.Input == nil || o.Output == nil {
		return
	}
	sum := *o.Input + *o.Output
	if o.Protocol == "anthropic" {
		if o.CacheRead == nil || o.CacheWrite == nil {
			return
		}
		sum += *o.CacheRead + *o.CacheWrite
	} else if o.CacheRead != nil && *o.CacheRead > *o.Input {
		o.Quality = "inconsistent"
		return
	}
	if o.Reasoning != nil && *o.Reasoning > *o.Output {
		o.Quality = "inconsistent"
		return
	}
	if o.Protocol != "anthropic" && o.CacheRead != nil && o.CacheWrite != nil && *o.CacheRead+*o.CacheWrite > *o.Input {
		o.Quality = "inconsistent"
		return
	}
	if o.TotalDerived {
		o.Total = nil
		o.TotalDerived = false
	}
	if o.Total == nil {
		o.Total, o.TotalDerived = &sum, true
	} else if *o.Total != sum {
		o.Quality = "inconsistent"
		return
	}
	if o.CacheRead != nil && o.CacheWrite != nil && o.Reasoning != nil && !o.Interrupted {
		o.Quality = "upstream_complete"
	}
}

func ObserveTokenUsage(c *gin.Context, protocol string, payload []byte) {
	if c == nil {
		return
	}
	next := ParseTokenObservation(protocol, payload)
	previous := TokenObservationForLog(c, false)
	if len(next.Raw) == 0 && next.Quality != "inconsistent" {
		return
	}
	// Stream usage frames are cumulative snapshots or disjoint fields, never
	// per-frame amounts. Merge present fields without summing tool rounds.
	if previous.Protocol == protocol && len(previous.Raw) > 0 {
		for key, value := range next.Raw {
			previous.Raw[key] = value
		}
		if next.Input != nil {
			previous.Input = next.Input
		}
		if next.Output != nil {
			previous.Output = next.Output
		}
		if next.CacheRead != nil {
			previous.CacheRead = next.CacheRead
		}
		if next.CacheWrite != nil {
			previous.CacheWrite = next.CacheWrite
		}
		if next.Reasoning != nil {
			previous.Reasoning = next.Reasoning
		}
		if next.Total != nil && !next.TotalDerived {
			previous.Total, previous.TotalDerived = next.Total, false
		}
		if next.Quality == "inconsistent" {
			previous.Quality = "inconsistent"
		}
		classifyTokenObservation(&previous)
		c.Set(tokenObservationKey, previous)
		return
	}
	c.Set(tokenObservationKey, next)
}

func MarkTokenEstimate(c *gin.Context) {
	if c == nil {
		return
	}
	o := TokenObservationForLog(c, false)
	if o.Quality != "inconsistent" {
		o.Quality = "local_estimate"
	}
	o.Source = "local_estimate"
	if len(o.Raw) > 0 {
		o.Source = "mixed_upstream_local"
	}
	c.Set(tokenObservationKey, o)
}

func MarkTokenInterrupted(c *gin.Context) {
	if c == nil {
		return
	}
	o := TokenObservationForLog(c, false)
	o.Interrupted = true
	if o.Quality == "upstream_complete" {
		o.Quality = "upstream_partial"
	}
	c.Set(tokenObservationKey, o)
}

func TokenObservationForLog(c *gin.Context, localEstimate bool) TokenObservation {
	o := TokenObservation{Source: "unknown", Quality: "missing", Raw: map[string]int64{}}
	if c == nil {
		return o
	}
	if value, exists := c.Get(tokenObservationKey); exists {
		if saved, ok := value.(TokenObservation); ok {
			o = saved
		}
	}
	if localEstimate && o.Quality != "inconsistent" {
		o.Quality = "local_estimate"
		o.Source = "local_estimate"
		if len(o.Raw) > 0 {
			o.Source = "mixed_upstream_local"
		}
	}
	return o
}

func ResetTokenObservation(c *gin.Context) {
	if c != nil {
		c.Set(tokenObservationKey, nil)
	}
}
