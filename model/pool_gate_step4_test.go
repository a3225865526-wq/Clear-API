package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func dtoPoolFilter(source string) dto.ChannelFilter {
	return dto.ChannelFilter{Kind: dto.FilterModelSource, Source: source}
}

// installPoolRoute installs one synthetic route record directly in the option
// snapshot the gate reads. The model package TestMain never populates
// common.OptionMap, so the helpers must tolerate a nil map.
func installPoolRoute(t *testing.T, model string, source string, enabled bool, version int) {
	t.Helper()
	key := PoolRoutingOptionPrefix + model
	encoded, err := common.Marshal(poolRouteRecord{Route: PoolModelRoute{Model: model, Source: source, Enabled: enabled, Version: version}})
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMap[key] = string(encoded)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, key)
		common.OptionMapRWMutex.Unlock()
	})
}

func setPoolMasterOption(t *testing.T, value string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	if value == "" {
		delete(common.OptionMap, PoolMasterOptionKey)
	} else {
		common.OptionMap[PoolMasterOptionKey] = value
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, PoolMasterOptionKey)
		common.OptionMapRWMutex.Unlock()
	})
}

func boundPoolChannel() *Channel {
	settings := `{"upstream_pool":{},"pool_routing_enabled":true}`
	return &Channel{Id: 42, Name: "synthetic-pool", OtherSettings: settings, Models: "synthetic-pool-model,synthetic-pool-alias"}
}

// A. Global master switch: anything but the exact stored string "true" reads
// as closed, including a missing row and a corrupt value.
func TestPoolMasterFailClosed(t *testing.T) {
	setPoolMasterOption(t, "")
	require.False(t, PoolMasterEnabled())
	setPoolMasterOption(t, "false")
	require.False(t, PoolMasterEnabled())
	setPoolMasterOption(t, "True")
	require.False(t, PoolMasterEnabled())
	setPoolMasterOption(t, "1")
	require.False(t, PoolMasterEnabled())
	setPoolMasterOption(t, "{corrupt")
	require.False(t, PoolMasterEnabled())
	setPoolMasterOption(t, "true")
	require.True(t, PoolMasterEnabled())
}

func TestPoolMasterPersistenceRoundTrip(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}))
	setPoolMasterOption(t, "")
	t.Cleanup(func() {
		DB.Where("key = ?", PoolMasterOptionKey).Delete(&Option{})
	})

	require.NoError(t, SetPoolMasterEnabled(true))
	require.True(t, PoolMasterEnabled())
	var stored Option
	require.NoError(t, DB.Where(&Option{Key: PoolMasterOptionKey}).First(&stored).Error)
	require.Equal(t, "true", stored.Value)

	require.NoError(t, SetPoolMasterEnabled(false))
	require.False(t, PoolMasterEnabled())
	require.NoError(t, DB.Where(&Option{Key: PoolMasterOptionKey}).First(&stored).Error)
	require.Equal(t, "false", stored.Value)

	// A direct DB edit must be visible after reload, and a corrupt DB value
	// reads as closed (the runtime never opens the pool on unknown state).
	require.NoError(t, DB.Model(&Option{}).Where(&Option{Key: PoolMasterOptionKey}).Update("value", "{corrupt").Error)
	require.NoError(t, updateOptionMap(PoolMasterOptionKey, "{corrupt"))
	require.False(t, PoolMasterEnabled())
}

// B/C/D. The layered gate: master switch × channel routing × publication,
// with deterministic denial reasons.
func TestPoolGateMatrix(t *testing.T) {
	pool := boundPoolChannel()
	ordinary := &Channel{Id: 7, Name: "synthetic-ordinary", Models: "synthetic-pool-model"}

	t.Run("unconfigured-model", func(t *testing.T) {
		setPoolMasterOption(t, "true")
		require.ErrorIs(t, pool.CheckPoolModelRoute("unpublished-model"), ErrPoolModelNotPublished)
		require.ErrorIs(t, pool.CheckPoolChannelPoolAdmission("unpublished-model"), ErrPoolModelNotPublished)
		require.NoError(t, ordinary.CheckPoolModelRoute("unpublished-model"))
		require.NoError(t, ordinary.CheckPoolChannelPoolAdmission("unpublished-model"))
	})
	t.Run("published-but-paused", func(t *testing.T) {
		setPoolMasterOption(t, "true")
		installPoolRoute(t, "synthetic-pool-model", "pool", false, 1)
		require.Error(t, pool.CheckPoolModelRoute("synthetic-pool-model"))
		require.Error(t, ordinary.CheckPoolModelRoute("synthetic-pool-model"))
	})
	t.Run("published-master-off", func(t *testing.T) {
		installPoolRoute(t, "synthetic-pool-model", "pool", true, 1)
		setPoolMasterOption(t, "")
		require.ErrorIs(t, pool.CheckPoolModelRoute("synthetic-pool-model"), ErrPoolGloballyDisabled)
		// Preflight (nil channel) denies before any channel is selected.
		require.ErrorIs(t, (*Channel)(nil).CheckPoolModelRoute("synthetic-pool-model"), ErrPoolGloballyDisabled)
	})
	t.Run("published-master-on", func(t *testing.T) {
		installPoolRoute(t, "synthetic-pool-model", "pool", true, 1)
		setPoolMasterOption(t, "true")
		// An api channel must never serve a pool-published model.
		require.Error(t, ordinary.CheckPoolModelRoute("synthetic-pool-model"))
		// A bound channel without the routing opt-in stays denied.
		routingOff := &Channel{Id: 8, OtherSettings: `{"upstream_pool":{}}`}
		require.ErrorIs(t, routingOff.CheckPoolModelRoute("synthetic-pool-model"), ErrPoolChannelRoutingOff)
		require.ErrorIs(t, routingOff.CheckPoolChannelPoolAdmission("synthetic-pool-model"), ErrPoolChannelRoutingOff)
		// The routing-enabled pool channel passes.
		require.NoError(t, pool.CheckPoolModelRoute("synthetic-pool-model"))
		require.NoError(t, pool.CheckPoolChannelPoolAdmission("synthetic-pool-model"))
	})
	t.Run("api-source-excludes-pool-channel", func(t *testing.T) {
		installPoolRoute(t, "synthetic-pool-model", "api", true, 1)
		setPoolMasterOption(t, "true")
		require.Error(t, pool.CheckPoolModelRoute("synthetic-pool-model"))
		require.NoError(t, ordinary.CheckPoolModelRoute("synthetic-pool-model"))
	})
	t.Run("unpublish-x-denies-x-only", func(t *testing.T) {
		installPoolRoute(t, "synthetic-pool-model", "pool", true, 1)
		setPoolMasterOption(t, "true")
		// Remove the route again: the same channel loses only model X.
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, PoolRoutingOptionPrefix+"synthetic-pool-model")
		common.OptionMapRWMutex.Unlock()
		require.ErrorIs(t, pool.CheckPoolModelRoute("synthetic-pool-model"), ErrPoolModelNotPublished)
		installPoolRoute(t, "synthetic-other-model", "pool", true, 1)
		require.NoError(t, pool.CheckPoolModelRoute("synthetic-other-model"))
	})
}

// E. Alias safety: operator admission is keyed on the resolved canonical model.
// A publication on the alias alone never admits, and a published canonical
// alone never lets an unpublished alias through either.
func TestPoolGateCanonicalAlias(t *testing.T) {
	channel := &Channel{
		Id:           42,
		OtherSettings: `{"upstream_pool":{},"pool_routing_enabled":true}`,
		Models:       "synthetic-pool-alias,synthetic-pool-model",
		ModelMapping: func() *string { s := `{"synthetic-pool-alias":"synthetic-pool-model"}`; return &s }(),
	}
	setPoolMasterOption(t, "true")
	t.Run("alias-published-canonical-not", func(t *testing.T) {
		installPoolRoute(t, "synthetic-pool-alias", "pool", true, 1)
		require.NoError(t, channel.CheckPoolModelRoute("synthetic-pool-alias"))
		require.ErrorIs(t, channel.CheckPoolChannelPoolAdmission("synthetic-pool-alias"), ErrPoolModelNotPublished)
	})
	t.Run("canonical-published-alias-not", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, PoolRoutingOptionPrefix+"synthetic-pool-alias")
		common.OptionMapRWMutex.Unlock()
		installPoolRoute(t, "synthetic-pool-model", "pool", true, 1)
		require.Error(t, channel.CheckPoolModelRoute("synthetic-pool-alias"))
	})
	t.Run("both-published-allows", func(t *testing.T) {
		installPoolRoute(t, "synthetic-pool-alias", "pool", true, 2)
		installPoolRoute(t, "synthetic-pool-model", "pool", true, 2)
		require.NoError(t, channel.CheckPoolChannelPoolAdmission("synthetic-pool-alias"))
	})
	t.Run("requested-name-cannot-bypass", func(t *testing.T) {
		// An unknown requested name resolves through the mapping, but the
		// requested name itself is unpublished: the route check denies.
		require.Error(t, channel.CheckPoolChannelPoolAdmission("synthetic-unknown-alias"))
	})
}

// The selection filter: source=pool keeps only routing-enabled channels,
// source=api (and the default) excludes every pool-bound channel.
func TestPoolFilterModelSource(t *testing.T) {
	poolOn := boundPoolChannel()
	poolOff := &Channel{Id: 8, OtherSettings: `{"upstream_pool":{}}`}
	ordinary := &Channel{Id: 7}
	poolFilter := dtoPoolFilter("pool")
	apiFilter := dtoPoolFilter("api")
	require.True(t, channelMatchesFilter(poolOn, "m", poolFilter))
	require.False(t, channelMatchesFilter(poolOff, "m", poolFilter))
	require.False(t, channelMatchesFilter(ordinary, "m", poolFilter))
	require.False(t, channelMatchesFilter(poolOn, "m", apiFilter))
	require.False(t, channelMatchesFilter(poolOff, "m", apiFilter))
	require.True(t, channelMatchesFilter(ordinary, "m", apiFilter))
}
