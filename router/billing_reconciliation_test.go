package router_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBillingReconciliationOwnerRoutes(t *testing.T) {
	t.Cleanup(common.InitFinanceConfig)
	gin.SetMode(gin.TestMode)
	db := financeTestDB(t)
	previousDB, previousRedis, previousBatch, previousMemory := model.DB, common.RedisEnabled, common.BatchUpdateEnabled, common.MemoryCacheEnabled
	model.DB, common.RedisEnabled, common.BatchUpdateEnabled, common.MemoryCacheEnabled = db, false, false, false
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	common.InitFinanceConfig()
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled, common.BatchUpdateEnabled, common.MemoryCacheEnabled = previousRedis, previousBatch, previousMemory
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Token{}))
	for _, id := range []int{5, 6, 7} {
		pat := fmt.Sprintf("synthetic-review-pat-%d", id)
		role := common.RoleRootUser
		if id == 7 {
			role = common.RoleCommonUser
		}
		require.NoError(t, db.Create(&model.User{Id: id, Username: fmt.Sprintf("api-review-%d", id), Role: role, Status: common.UserStatusEnabled, Quota: 1000, AccessToken: &pat, AffCode: fmt.Sprintf("api-review-aff-%d", id)}).Error)
	}
	api := gin.New()
	router.RegisterFinanceRoutes(api.Group("/api"))
	request := func(method, path string, user int, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/owner/finance"+path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if user > 0 {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer synthetic-review-pat-%d", user))
		}
		r := httptest.NewRecorder()
		api.ServeHTTP(r, req)
		return r
	}
	for _, endpoint := range []struct{ method, path string }{{"GET", "/billing-reservations"}, {"POST", "/billing-reservations/1/decision"}, {"GET", "/task-settlements"}, {"POST", "/task-settlements/1/retry"}} {
		require.Equal(t, http.StatusUnauthorized, request(endpoint.method, endpoint.path, 0, "{}").Code)
		for _, user := range []int{6, 7} {
			require.Equal(t, http.StatusForbidden, request(endpoint.method, endpoint.path, user, "{}").Code)
		}
	}
	require.Equal(t, http.StatusServiceUnavailable, request("GET", "/billing-reservations", 5, "").Code)
	require.False(t, db.Migrator().HasTable(&model.BillingReservationDecision{}))
	require.NoError(t, model.MigrateFinanceRequests(db))
	require.NoError(t, model.MigrateBillingReconciliation(db))
	key := model.Token{UserId: 5, Key: "synthetic-review-api-token", RemainQuota: 1000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&key).Error)
	require.NoError(t, model.ReserveBillingRequest("api-review-request", 5, key.Id, 100))
	model.EndBillingRequestExecution("api-review-request")
	var r model.BillingReservation
	require.NoError(t, db.Where("request_id = ?", "api-review-request").First(&r).Error)
	response := request("GET", "/billing-reservations", 5, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
	require.NotContains(t, response.Body.String(), key.Key)
	path := fmt.Sprintf("/billing-reservations/%d/decision", r.ID)
	body := `{"expected_state":"active","expected_applied":100,"expected_target":0,"refund_quota":40,"reason":"provider invoice checked","execution_ended":true}`
	response = request("POST", path, 5, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, http.StatusOK, request("POST", path, 5, body).Code)
	require.Equal(t, http.StatusConflict, request("POST", path, 5, `{"expected_state":"active","expected_applied":100,"expected_target":0,"refund_quota":50,"reason":"provider invoice checked","execution_ended":true}`).Code)
	require.Equal(t, http.StatusBadRequest, request("POST", path, 5, `{"execution_ended":false}`).Code)
	require.Equal(t, http.StatusBadRequest, request("POST", path, 5, string(bytes.Repeat([]byte("x"), 5000))).Code)
	var user model.User
	require.NoError(t, db.First(&user, 5).Error)
	require.Equal(t, 940, user.Quota)
	response = request("GET", "/billing-reservations?scope=reviewed", 5, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "provider invoice checked")
	// Task settlement is separately gated; authorization is enforced before
	// inspecting either its schema or its payload.
	require.Equal(t, http.StatusServiceUnavailable, request("GET", "/task-settlements", 5, "").Code)
	require.False(t, db.Migrator().HasTable(&model.TaskSettlement{}))
	t.Setenv("CLEARAPI_TASK_SETTLEMENT", "true")
	previousLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}, &model.UserSubscription{}, &model.Log{}, &model.QuotaData{}))
	require.NoError(t, model.MigrateTaskSettlements(db))
	require.NoError(t, model.MigrateTaskSettlementLogs(db))
	channel := model.Channel{Name: "synthetic-task-api-channel", UsedQuota: 100}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 5).Updates(map[string]any{"quota": 10, "used_quota": 100, "request_count": 1}).Error)
	require.NoError(t, db.Create(&model.BillingReservation{RequestID: "api-task-request", UserID: 5, TokenID: key.Id, Reserved: 100, Applied: 100, Target: 100, State: "settled"}).Error)
	task := model.Task{TaskID: "api-task", UserId: 5, ChannelId: channel.Id, Quota: 100, Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{TokenId: key.Id, BillingSource: "wallet", Execution: &model.TaskExecutionSnapshot{RequestID: "api-task-request"}}}
	require.NoError(t, task.Insert())
	settlement, err := model.FreezeTaskSettlement(db, task.ID, 140, "synthetic adjustment", "{}", model.Log{})
	require.NoError(t, err)
	_, err = model.ApplyTaskSettlement(db, settlement.ID)
	require.ErrorIs(t, err, model.ErrFinanceConflict)
	response = request("GET", "/task-settlements", 5, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
	require.Contains(t, response.Body.String(), "needs_review")
	require.NotContains(t, response.Body.String(), key.Key)
	path = fmt.Sprintf("/task-settlements/%d/retry", settlement.ID)
	require.Equal(t, http.StatusBadRequest, request("POST", path, 5, "{}").Code)
	require.Equal(t, http.StatusBadRequest, request("POST", path, 5, string(bytes.Repeat([]byte("x"), 5000))).Code)
	require.Equal(t, http.StatusConflict, request("POST", path, 5, `{"expected_attempts":99,"expected_target":140,"reason":"verified","confirmed":true}`).Code)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 5).Update("quota", 1000).Error)
	response = request("POST", path, 5, `{"expected_attempts":1,"expected_target":140,"reason":"restored synthetic balance checked","confirmed":true}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "applied")
	require.Equal(t, http.StatusConflict, request("POST", path, 5, `{"expected_attempts":1,"expected_target":140,"reason":"same","confirmed":true}`).Code)
	require.NoError(t, db.First(&user, 5).Error)
	require.Equal(t, 960, user.Quota)
	response = request("GET", "/task-settlements?scope=applied", 5, "")
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "delivered")
}
