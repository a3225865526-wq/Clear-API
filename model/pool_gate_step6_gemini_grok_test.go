package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// STEP 6: real Gemini/Grok model names must flow through the same generic
// pool gate as every other name — no provider-specific bypass into the Codex
// OAuth pool, and no special treatment in the other direction either. The
// executor-side provider deny is proven separately (CLIProxyAPI
// pool_gemini_xai_isolation_test.go); this file proves the gateway side.

// Model routing does not infer provider support from names. Channel abilities
// select candidates and the executor validates actual account support.
func TestPoolGateUnconfiguredNamesUseChannelRouting(t *testing.T) {
	setPoolMasterOption(t, "true")
	pool := boundPoolChannel()
	ordinary := &Channel{Id: 7, Name: "synthetic-gemini-channel", Models: "gemini-2.5-pro,grok-4"}

	// Unpublished names select with the api filter plus canonical-pool
	// allowance (service/channel_select.go).
	canonicalFilter := dto.ChannelFilter{Kind: dto.FilterModelSource, Source: "api", AllowCanonicalPool: true}
	poolFilter := dtoPoolFilter("pool")
	for _, name := range []string{"gemini-2.5-pro", "gemini-3-flash-preview", "grok-4", "grok-3-mini"} {
		require.NoError(t, pool.CheckPoolModelRoute(name))
		require.NoError(t, pool.CheckPoolChannelPoolAdmission(name))
		require.True(t, channelMatchesFilter(pool, name, canonicalFilter),
			"unconfigured %q uses channel routing and executor capability checks", name)
		require.True(t, channelMatchesFilter(pool, name, poolFilter))
		require.True(t, channelMatchesFilter(ordinary, name, canonicalFilter),
			"unpublished %q must keep ordinary channels eligible", name)
	}
}

// If an operator publishes a provider model name to the pool, the selection
// filter inverts: only the routing-enabled pool channel stays eligible.
func TestPoolGatePublishedGeminiNameExcludesApiChannels(t *testing.T) {
	setPoolMasterOption(t, "true")
	pool := boundPoolChannel()
	ordinary := &Channel{Id: 7, Name: "synthetic-gemini-channel", Models: "gemini-2.5-pro"}

	installPoolRoute(t, "gemini-2.5-pro", "pool", true, 1)
	require.NoError(t, pool.CheckPoolModelRoute("gemini-2.5-pro"))
	require.Error(t, ordinary.CheckPoolModelRoute("gemini-2.5-pro"),
		"an api channel must never serve a pool-published model name")

	poolFilter := dtoPoolFilter("pool")
	require.True(t, channelMatchesFilter(pool, "gemini-2.5-pro", poolFilter))
	require.False(t, channelMatchesFilter(ordinary, "gemini-2.5-pro", poolFilter))
}

// A Codex canonical name published to the pool never admits an ordinary
// (non-pool-bound) Gemini/Grok channel that happens to list the same name;
// such a name can only be served by the pool channel itself, and the
// executor's provider deny is the second fence.
func TestPoolGatePublishedCodexNameNeverAdmitsGeminiGrokChannels(t *testing.T) {
	setPoolMasterOption(t, "true")
	installPoolRoute(t, "gpt-5-codex", "pool", true, 1)

	geminiChannel := &Channel{Id: 9, Name: "synthetic-gemini", Models: "gpt-5-codex"}
	grokChannel := &Channel{Id: 10, Name: "synthetic-grok", Models: "gpt-5-codex"}

	require.Error(t, geminiChannel.CheckPoolModelRoute("gpt-5-codex"))
	require.Error(t, grokChannel.CheckPoolModelRoute("gpt-5-codex"))

	poolFilter := dtoPoolFilter("pool")
	require.False(t, channelMatchesFilter(geminiChannel, "gpt-5-codex", poolFilter))
	require.False(t, channelMatchesFilter(grokChannel, "gpt-5-codex", poolFilter))
}
