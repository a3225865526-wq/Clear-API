package common

import "os"

// Route Chain feature gate (fail-closed, requirement §54): route chains are
// inert unless the operator sets CLEARAPI_ENABLE_ROUTE_CHAIN=true AND
// configures chains. Mirror of the pool gate pattern in pool.go — evaluated
// once, never toggled per request. A missing or corrupt value means off.
var routeChainEnabled = os.Getenv("CLEARAPI_ENABLE_ROUTE_CHAIN") == "true"

func RouteChainEnabled() bool {
	return routeChainEnabled
}

// SetRouteChainEnabledForTest overrides the compiled gate inside tests only.
func SetRouteChainEnabledForTest(enabled bool) {
	routeChainEnabled = enabled
}
