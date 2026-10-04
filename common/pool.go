package common

import "os"

// The upstream account pool is a site-owner-only management module. The owner
// identity is fixed by policy: user ID 1 holding the root role. It is
// intentionally not configurable — CLEARAPI_POOL_ENABLED may only switch the
// module on or off and can never grant management to a different account.
const PoolOwnerUserID = 1

var poolEnabled bool

func InitPoolConfig() {
	poolEnabled = os.Getenv("CLEARAPI_POOL_ENABLED") == "true"
}

func PoolEnabled() bool { return poolEnabled }

func IsPoolOwner(userID int) bool {
	return PoolEnabled() && userID == PoolOwnerUserID
}

// PoolCliproxyDedicated is the operator attestation that the configured
// CLIProxyAPI endpoint is a dedicated pool executor instance: separate
// credential directory, separate management key, referenced by no channel,
// and with inbound client keys configured (the executor serves an unauthenticated
// /v1 API when none are set). Until it is attested the authorization paths
// fail closed, because the executor (v8.0.4, source-confirmed) hot-reloads
// every new credential file into its scheduler with disabled:false and has no
// quarantine mechanism — a shared instance would make freshly authorized
// accounts schedulable before admission.
func PoolCliproxyDedicated() bool {
	return os.Getenv("CLEARAPI_POOL_CLIPROXY_DEDICATED") == "true"
}
