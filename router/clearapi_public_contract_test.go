package router

import (
	"net/http"
	"testing"

	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestClearAPIPublicRelayRoutes protects the documented source-level contract.
// It checks route registration only; no upstream or paid request is made.
func TestClearAPIPublicRelayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)

	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{
		http.MethodGet + " /v1/models",
		http.MethodGet + " /v1/models/:model",
		http.MethodPost + " /v1/chat/completions",
		http.MethodPost + " /v1/messages",
		http.MethodPost + " /v1beta/models/*path",
	} {
		require.Truef(t, routes[route], "documented route %q is not registered", route)
	}

	protocol, operation, found := pluginruntime.LookupHostProtocolOperation(http.MethodPost, "/v1/responses")
	require.True(t, found)
	require.Equal(t, "openai_responses", protocol)
	require.Equal(t, "create", operation.Name)
}
