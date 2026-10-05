package accountpool

import (
	"context"
	"sync"
)

// Pool model availability (STEP 4). For each canonical model the executor
// ledger shows how many accounts are simultaneously trusted and hold a
// verified capability bound to the account's current trust generation. The
// count is an owner-facing hint ("is this published model currently backed by
// a capable account?") — it never grants capability, never promotes trust, and
// never publishes or unpublishes a model.

// PoolModelSnapshotFetcher is the injection point for tests. The default reads
// the CLIProxyAPI management snapshot from the configured endpoint.
var PoolModelSnapshotFetcher = func(ctx context.Context) (PoolExecutorModelSnapshot, error) {
	if !CliproxyEndpointConfigured() {
		return PoolExecutorModelSnapshot{}, ErrPoolExecutorOffline
	}
	return NewCliproxyClientFromEnv().PoolModelSnapshot(ctx)
}

var poolAvailabilityMutex sync.Mutex

// PoolModelAvailability returns verified-capable trusted account counts per
// model. An executor error is returned as-is so the control plane can show an
// explicitly unavailable hint instead of a stale or zero answer.
func PoolModelAvailability(ctx context.Context) (map[string]int, error) {
	poolAvailabilityMutex.Lock()
	defer poolAvailabilityMutex.Unlock()
	snapshot, err := PoolModelSnapshotFetcher(ctx)
	if err != nil {
		return nil, err
	}
	trusted := make(map[string]PoolExecutorTrustIdentity, len(snapshot.Identities))
	for _, identity := range snapshot.Identities {
		if identity.State != "trusted" {
			continue
		}
		// STEP 1B: a trust record only authorizes while its authorization
		// version matches the policy revision it was promoted under.
		if snapshot.Revision == 0 || identity.AuthorizationVersion != snapshot.Revision {
			continue
		}
		trusted[identity.AuthID+"\x00"+identity.Provider+"\x00"+identity.CredentialDigest] = identity
	}
	availability := make(map[string]int)
	for _, capability := range snapshot.Capabilities {
		if capability.State != "verified" {
			continue
		}
		identity, ok := trusted[capability.AuthID+"\x00"+capability.Provider+"\x00"+capability.CredentialDigest]
		if !ok || identity.Generation == 0 || identity.Generation != capability.Generation {
			continue
		}
		availability[capability.Model]++
	}
	return availability, nil
}
