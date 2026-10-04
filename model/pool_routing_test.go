package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPoolRoutingOptionSyncDoesNotRestoreOlderEnabledRevision(t *testing.T) {
	key := PoolRoutingOptionPrefix + "synthetic-source-model"
	current, err := common.Marshal(poolRouteRecord{Route: PoolModelRoute{Model: "synthetic-source-model", Source: "api", Enabled: false, Version: 2}})
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{key: string(current)}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = previous; common.OptionMapRWMutex.Unlock() })
	stale, err := common.Marshal(poolRouteRecord{Route: PoolModelRoute{Model: "synthetic-source-model", Source: "api", Enabled: true, Version: 1}})
	require.NoError(t, err)
	require.NoError(t, updateOptionMap(key, string(stale)))
	route, err := GetPoolModelRoute("synthetic-source-model")
	require.NoError(t, err)
	require.False(t, route.Enabled)
	require.Equal(t, 2, route.Version)
	require.Error(t, (&Channel{}).CheckPoolModelRoute("synthetic-source-model"))
	common.OptionMapRWMutex.Lock()
	common.OptionMap[key] = "{"
	common.OptionMapRWMutex.Unlock()
	require.ErrorIs(t, (&Channel{}).CheckPoolModelRoute("synthetic-source-model"), ErrPoolUnavailable)
}
