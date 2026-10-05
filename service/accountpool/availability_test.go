package accountpool

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// Availability counts only accounts whose capability is verified AND bound to
// a trust record that is trusted, holds the same credential digest, and is
// effective at the current policy revision with a matching generation.
// Anything else is a missing hint, never an implicit permission.
func TestPoolModelAvailabilityBinding(t *testing.T) {
	const revision = uint64(9)
	snapshot := PoolExecutorModelSnapshot{
		Revision:      revision,
		TrustRevision: revision,
		Identities: []PoolExecutorTrustIdentity{
			{AuthID: "a1", Provider: "codex", CredentialDigest: "d1", State: "trusted", AuthorizationVersion: revision, Generation: 3},
			{AuthID: "a2", Provider: "codex", CredentialDigest: "d2", State: "trusted", AuthorizationVersion: revision - 1, Generation: 2},
			{AuthID: "a3", Provider: "codex", CredentialDigest: "d3", State: "pending", AuthorizationVersion: revision, Generation: 1},
			{AuthID: "a4", Provider: "codex", CredentialDigest: "d4", State: "trusted", AuthorizationVersion: revision, Generation: 5},
		},
		Capabilities: []PoolExecutorCapability{
			{AuthID: "a1", Provider: "codex", CredentialDigest: "d1", Model: "model-live", State: "verified", Generation: 3},
			{AuthID: "a1", Provider: "codex", CredentialDigest: "d1", Model: "model-stale-generation", State: "verified", Generation: 4},
			{AuthID: "a1", Provider: "codex", CredentialDigest: "d1", Model: "model-revoked", State: "revoked", Generation: 3},
			{AuthID: "a2", Provider: "codex", CredentialDigest: "d2", Model: "model-stale-authorization", State: "verified", Generation: 2},
			{AuthID: "a3", Provider: "codex", CredentialDigest: "d3", Model: "model-untrusted", State: "verified", Generation: 1},
			{AuthID: "a4", Provider: "codex", CredentialDigest: "wrong-digest", Model: "model-identity-mismatch", State: "verified", Generation: 5},
		},
	}
	previous := PoolModelSnapshotFetcher
	PoolModelSnapshotFetcher = func(context.Context) (PoolExecutorModelSnapshot, error) { return snapshot, nil }
	t.Cleanup(func() { PoolModelSnapshotFetcher = previous })

	availability, err := PoolModelAvailability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if availability["model-live"] != 1 {
		t.Fatalf("model-live = %d, want 1", availability["model-live"])
	}
	for _, model := range []string{"model-stale-generation", "model-revoked", "model-stale-authorization", "model-untrusted", "model-identity-mismatch"} {
		if availability[model] != 0 {
			t.Fatalf("%s = %d, want 0", model, availability[model])
		}
	}
}

func TestPoolModelAvailabilityExecutorOfflineFailsClosed(t *testing.T) {
	previous := PoolModelSnapshotFetcher
	PoolModelSnapshotFetcher = func(context.Context) (PoolExecutorModelSnapshot, error) {
		return PoolExecutorModelSnapshot{}, model.ErrPoolUnavailable
	}
	t.Cleanup(func() { PoolModelSnapshotFetcher = previous })
	if _, err := PoolModelAvailability(context.Background()); err == nil {
		t.Fatal("executor failure must surface, not report zero availability")
	}
}
