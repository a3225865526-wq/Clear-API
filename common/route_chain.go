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

// Stage 5 circuit breaker gate (fail-closed, design §2.4): the breaker is
// independent of the route chain gate so it can be gray-released alone. A
// missing or corrupt value means off; when off, routing behavior is
// byte-identical to the pre-breaker engine.
var routeCircuitBreakerEnabled = os.Getenv("CLEARAPI_ENABLE_ROUTE_CIRCUIT_BREAKER") == "true"

func RouteCircuitBreakerEnabled() bool {
	return routeCircuitBreakerEnabled
}

// SetRouteChainEnabledForTest overrides the compiled gate inside tests only.
func SetRouteChainEnabledForTest(enabled bool) {
	routeChainEnabled = enabled
}

// SetRouteCircuitBreakerEnabledForTest overrides the compiled breaker gate
// inside tests only.
func SetRouteCircuitBreakerEnabledForTest(enabled bool) {
	routeCircuitBreakerEnabled = enabled
}
