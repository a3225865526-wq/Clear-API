package router_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func poolRoutingFixture(t *testing.T) *poolTestSetup {
	t.Helper()
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Option{}))
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = previous; common.OptionMapRWMutex.Unlock() })
	return setup
}

func TestPoolModelRoutingOwnerAndAdmission(t *testing.T) {
	setup := poolRoutingFixture(t)
	input := map[string]any{"model": "synthetic-model", "platform": "codex", "access_type": "oauth_browser", "source": "api", "enabled": true, "expected_version": 0}
	for _, token := range []string{"", "synthetic-user-pat", "synthetic-admin-pat", "synthetic-other-root-pat"} {
		response := setup.request(http.MethodPut, "/api/owner/pool/model-routes", token, input)
		require.NotEqual(t, http.StatusOK, response.Code)
	}
	response := setup.request(http.MethodPut, "/api/owner/pool/model-routes", "synthetic-owner-pat", input)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	route, err := model.GetPoolModelRoute("synthetic-model")
	require.NoError(t, err)
	require.True(t, route.Enabled)
	require.Equal(t, "api", route.Source)
	input["source"], input["expected_version"] = "pool", 1
	response = setup.request(http.MethodPut, "/api/owner/pool/model-routes", "synthetic-owner-pat", input)
	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "POOL_NOT_ADMITTED")
	route, err = model.GetPoolModelRoute("synthetic-model")
	require.NoError(t, err)
	require.Equal(t, "api", route.Source)
	require.Equal(t, 1, route.Version)
	input["enabled"] = false
	response = setup.request(http.MethodPut, "/api/owner/pool/model-routes", "synthetic-owner-pat", input)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	selected := &model.Channel{Id: 123, Type: 1, Status: 1}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.NotNil(t, middleware.SetupContextForSelectedChannel(c, selected, "synthetic-model"))
	_, _, selectErr := service.SelectChannelForRequest(c, "synthetic-model", &service.RetryParam{Ctx: c, TokenGroup: "default"})
	require.NotNil(t, selectErr)
	require.Contains(t, selectErr.Message, "PAUSED")
	require.NoError(t, selected.CheckPoolModelRoute("unconfigured-model"))
	input["source"], input["enabled"], input["expected_version"] = "api", true, 2
	require.Equal(t, http.StatusOK, setup.request(http.MethodPut, "/api/owner/pool/model-routes", "synthetic-owner-pat", input).Code)
	require.NoError(t, selected.CheckPoolModelRoute("synthetic-model"))
	poolSetting := `{"upstream_pool":false}`
	poolChannel := &model.Channel{Id: 456, Setting: &poolSetting}
	matched, _ := model.ChannelSatisfiesFilters(poolChannel, "synthetic-model", []dto.ChannelFilter{{Kind: dto.FilterModelSource, Source: "api"}})
	require.False(t, matched)
	require.Error(t, poolChannel.CheckPoolModelRoute("synthetic-model"))
	require.ErrorIs(t, model.UpdateOption(model.PoolRoutingOptionPrefix+"synthetic-model", "{}"), model.ErrPoolInvalid)
	require.ErrorIs(t, model.UpdateOptionsBulk(map[string]string{model.PoolRoutingOptionPrefix + "synthetic-model": "{}"}), model.ErrPoolInvalid)
}

func TestPoolModelRoutingConcurrentCreationAndUpdate(t *testing.T) {
	poolRoutingFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := model.SavePoolModelRoute(model.PoolModelRoute{Model: "synthetic-race", Platform: "codex", AccessType: "oauth_browser", Source: "api", Enabled: true}, 0)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	route, err := model.GetPoolModelRoute("synthetic-race")
	require.NoError(t, err)
	require.Equal(t, 1, route.Version)
	_, err = model.SavePoolModelRoute(*route, 1)
	require.NoError(t, err)
	_, err = model.SavePoolModelRoute(*route, 1)
	require.ErrorIs(t, err, model.ErrPoolConflict)
	routes, err := model.ListPoolModelRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 1)
	require.Equal(t, 2, routes[0].Version)
}
