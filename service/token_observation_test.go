package service

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestUsageObservationPresenceAndSemantics(t *testing.T) {
	cases := []struct {
		name, protocol, payload, quality string
		total                            *int64
	}{
		{"missing", "openai", `{}`, "missing", nil},
		{"null", "openai", `{"usage":null}`, "missing", nil},
		{"total-only", "openai", `{"usage":{"total_tokens":12}}`, "upstream_partial", number(12)},
		{"zero", "openai", `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`, "upstream_complete", number(0)},
		{"cache-subsets", "openai", `{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":4,"cache_write_tokens":2},"completion_tokens_details":{"reasoning_tokens":3}}}`, "upstream_complete", number(15)},
		{"cache-additional", "anthropic", `{"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":4,"cache_creation_input_tokens":2,"output_tokens_details":{"thinking_tokens":3}}}`, "upstream_complete", number(21)},
		{"unknown-cache", "openai", `{"usage":{"prompt_tokens":10,"completion_tokens":5}}`, "upstream_partial", number(15)},
		{"contradiction", "openai", `{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":99}}`, "inconsistent", number(99)},
		{"alias-contradiction", "openai", `{"usage":{"prompt_tokens":10,"input_tokens":11}}`, "inconsistent", nil},
		{"invalid-details", "openai", `{"usage":{"prompt_tokens":10,"prompt_tokens_details":"invalid"}}`, "inconsistent", nil},
		{"negative", "openai", `{"usage":{"prompt_tokens":-1}}`, "inconsistent", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTokenObservation(tc.protocol, []byte(tc.payload))
			if got.Quality != tc.quality {
				t.Fatalf("quality %s, want %s", got.Quality, tc.quality)
			}
			if (got.Total == nil) != (tc.total == nil) || (tc.total != nil && *got.Total != *tc.total) {
				t.Fatalf("total: %+v", got)
			}
			if tc.name == "missing" && got.Input != nil {
				t.Fatal("missing input became zero")
			}
		})
	}
}
func number(v int64) *int64 { return &v }

func TestUsageStreamToolRetryAndFailoverObservation(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ObserveTokenUsage(c, "anthropic", []byte(`{"message":{"usage":{"input_tokens":10,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}}`))
	for range 2 {
		ObserveTokenUsage(c, "anthropic", []byte(`{"usage":{"output_tokens":5,"output_tokens_details":{"thinking_tokens":0}}}`))
	}
	got := TokenObservationForLog(c, false)
	if got.Total == nil || *got.Total != 18 || got.Quality != "upstream_complete" {
		t.Fatalf("duplicate tool/stream frame accumulated: %+v", got)
	}
	MarkTokenInterrupted(c)
	if got = TokenObservationForLog(c, false); !got.Interrupted || got.Quality != "upstream_partial" {
		t.Fatal(got)
	}
	ResetTokenObservation(c) // next selected channel attempt, not the final bill
	if got = TokenObservationForLog(c, false); got.Quality != "missing" || got.Total != nil {
		t.Fatal("previous attempt leaked", got)
	}
	ObserveTokenUsage(c, "openai", []byte(`{"error":{"message":"fixed sample"}}`))
	if TokenObservationForLog(c, false).Quality != "missing" {
		t.Fatal("error synthesized usage")
	}
	MarkTokenEstimate(c)
	if got = TokenObservationForLog(c, false); got.Source != "local_estimate" || got.Total != nil {
		t.Fatal(got)
	}
	ResetTokenObservation(c)
	ObserveTokenUsage(c, "openai", []byte(`{"response":{"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`))
	MarkTokenEstimate(c)
	if got = TokenObservationForLog(c, false); got.Source != "mixed_upstream_local" || got.Quality != "local_estimate" || len(got.Raw) != 3 {
		t.Fatal(got)
	}
}
