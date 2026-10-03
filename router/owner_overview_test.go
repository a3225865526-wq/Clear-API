package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type overviewMarkerFixture struct {
	ID            int64
	ReservationID int64
	Reason        string
	CreatedAt     int64
}

func (overviewMarkerFixture) TableName() string { return "billing_review_markers" }

func TestOwnerOverviewRoutes(t *testing.T) {
	db := financeTestDB(t)
	if common.DatabaseType(os.Getenv("FINANCE_TEST_DIALECT")) == common.DatabaseTypeSQLite || os.Getenv("FINANCE_TEST_DIALECT") == "" {
		var version string
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
		t.Logf("SQLite %s", version)
	}
	if dsn := os.Getenv("OVERVIEW_TEST_LOG_DSN"); dsn != "" {
		t.Setenv("FINANCE_TEST_DSN", dsn)
	}
	logDB := financeTestDB(t)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
	redis, memory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, logDB
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "5")
	common.InitFinanceConfig()
	kind := common.DatabaseTypeSQLite
	if os.Getenv("FINANCE_TEST_DIALECT") == "mysql" {
		kind = common.DatabaseTypeMySQL
	}
	if os.Getenv("FINANCE_TEST_DIALECT") == "postgres" {
		kind = common.DatabaseTypePostgreSQL
	}
	common.SetDatabaseTypes(kind, kind)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(mainType, logType)
		common.RedisEnabled, common.MemoryCacheEnabled = redis, memory
		common.InitFinanceConfig()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Log{}))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	from := time.Date(2026, 9, 28, 0, 0, 0, 0, model.ConsoleBusinessZone).Unix()
	to := from + 2*86400
	for _, user := range []struct {
		id   int
		role int
		at   int64
	}{
		{5, common.RoleRootUser, from - 1}, {6, common.RoleRootUser, from + 1}, {7, common.RoleAdminUser, from + 2},
		{8, common.RoleCommonUser, from + 3}, {9, common.RoleCommonUser, from + 86400}, {10, common.RoleCommonUser, to},
		{11, common.RoleCommonUser, from - 1}, {12, common.RoleCommonUser, from + 4},
	} {
		token := fmt.Sprintf("synthetic-overview-%d", user.id)
		require.NoError(t, db.Create(&model.User{Id: user.id, Username: fmt.Sprintf("overview-%d", user.id), Role: user.role, Status: common.UserStatusEnabled, CreatedAt: user.at, AccessToken: &token, AffCode: fmt.Sprintf("overview-aff-%d", user.id)}).Error)
	}
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 11).Update("created_at", 0).Error)
	require.NoError(t, db.Delete(&model.User{}, 12).Error)
	logs := []model.Log{
		{UserId: 8, CreatedAt: from, Type: model.LogTypeConsume, ModelName: "model-a", ChannelId: 3, Quota: 100, PromptTokens: 10, CompletionTokens: 5, UseTime: 2},
		{UserId: 8, CreatedAt: from + 60, Type: model.LogTypeConsume, ModelName: "model-a", ChannelId: 3, Quota: 200, PromptTokens: 20, CompletionTokens: 10, UseTime: 4},
		{UserId: 9, CreatedAt: from + 86400, Type: model.LogTypeConsume, ModelName: "model-b", ChannelId: 4, Quota: 50, PromptTokens: 7, CompletionTokens: 3, UseTime: 6},
		{UserId: 7, CreatedAt: from + 1, Type: model.LogTypeError, Quota: 999, PromptTokens: 999},
		{UserId: 8, CreatedAt: from + 2, Type: model.LogTypeRefund, Quota: 30, PromptTokens: 999},
		{UserId: 8, CreatedAt: from + 3, Type: model.LogTypeTopup, Quota: 999},
		{UserId: 8, CreatedAt: from - 1, Type: model.LogTypeConsume, Quota: 999},
		{UserId: 8, CreatedAt: to, Type: model.LogTypeConsume, Quota: 999},
	}
	require.NoError(t, logDB.Create(&logs).Error)
	// A poisoned main-database log would expose accidental reads of DB instead of LOG_DB.
	require.NoError(t, db.Create(&model.Log{CreatedAt: from, Type: model.LogTypeConsume, Quota: 888888}).Error)
	gin.SetMode(gin.TestMode)
	api := gin.New()
	router.RegisterFinanceRoutes(api.Group("/api"))
	request := func(query, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/owner/finance/overview"+query, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		api.ServeHTTP(res, req)
		return res
	}
	query := "?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
	_, err := model.GetOwnerOverview(context.Background(), db, logDB, from, to, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 401, request(query, "").Code)
	for _, id := range []int{6, 7, 8} {
		assert.Equal(t, 403, request(query, fmt.Sprintf("synthetic-overview-%d", id)).Code)
	}
	response := request(query, "synthetic-overview-5")
	require.Equal(t, 200, response.Code, response.Body.String())
	var body struct {
		Data model.OwnerOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	data := body.Data
	assert.Equal(t, "Asia/Shanghai", data.Timezone)
	assert.Equal(t, int64(8), data.TotalUsers)
	assert.Equal(t, int64(5), data.NewUsers)
	assert.Equal(t, int64(1), data.UnknownRegistration)
	assert.Equal(t, int64(2), data.ActiveUsers)
	assert.Equal(t, int64(3), data.Requests)
	assert.Equal(t, int64(1), data.Errors)
	assert.Equal(t, int64(350), data.Quota)
	assert.Equal(t, int64(30), data.RefundQuota)
	assert.Equal(t, int64(37), data.Input)
	assert.Equal(t, int64(18), data.Output)
	require.NotNil(t, data.AverageSeconds)
	assert.Equal(t, float64(4), *data.AverageSeconds)
	assert.Nil(t, data.PaidCreditUsers)
	assert.Nil(t, data.PendingReview)
	require.Len(t, data.Trend, 2)
	assert.Equal(t, "2026-09-28", data.Trend[0].Date)
	assert.Equal(t, int64(4), data.Trend[0].NewUsers)
	assert.Equal(t, int64(1), data.Trend[1].NewUsers)
	assert.Equal(t, int64(1), data.Trend[0].ActiveUsers)
	assert.Equal(t, int64(1), data.Trend[1].ActiveUsers)
	require.Len(t, data.Models, 2)
	assert.Equal(t, "model-a", data.Models[0].Name)
	assert.Equal(t, int64(300), data.Models[0].Quota)
	require.Len(t, data.Channels, 2)
	assert.Equal(t, "3", data.Channels[0].Name)
	for _, invalid := range []string{"?period=7d&from=0", "?period=nope", "?from=abc&to=100", "?from=1&to=9999999999999", "?from=100&to=100", "?to=100"} {
		assert.Equal(t, 400, request(invalid, "synthetic-overview-5").Code, invalid)
	}
	now := time.Date(2026, 10, 2, 0, 30, 0, 0, model.ConsoleBusinessZone)
	start, end, err := model.OwnerOverviewRange("7d", 0, 0, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-26", time.Unix(start, 0).In(model.ConsoleBusinessZone).Format("2006-01-02"))
	assert.Equal(t, "2026-10-03", time.Unix(end, 0).In(model.ConsoleBusinessZone).Format("2006-01-02"))
	empty, err := model.GetOwnerOverview(context.Background(), db, logDB, to+1, to+86400, now)
	require.NoError(t, err)
	require.Len(t, empty.Trend, 1)
	assert.Zero(t, empty.Requests)
	assert.Nil(t, empty.AverageSeconds)
	assert.Empty(t, empty.Models)
	// Optional ledgers becoming ready must not change operational totals.
	require.NoError(t, model.MigrateFinanceRequests(db))
	require.NoError(t, db.AutoMigrate(&model.BillingReservation{}, &overviewMarkerFixture{}))
	for i, event := range []model.FinanceRevenueRecord{
		{UserID: 8, At: from, Kind: "credit", Source: "paid"}, {UserID: 8, At: from + 1, Kind: "credit", Source: "paid"},
		{UserID: 9, At: from + 1, Kind: "credit", Source: "paid"}, {UserID: 7, At: from + 1, Kind: "credit", Source: "gift"},
	} {
		value := "10"
		event.ValueCNY, event.EventKey, event.Revision = &value, fmt.Sprintf("overview-%d", i), int64(i+1)
		require.NoError(t, db.Create(&event).Error)
	}
	for i, state := range []string{"needs_review", "settled", "active", "reviewed"} {
		r := model.BillingReservation{RequestID: fmt.Sprintf("overview-review-%d", i), State: state, UpdatedAt: from}
		require.NoError(t, db.Create(&r).Error)
		if state == "settled" {
			require.NoError(t, db.Create(&overviewMarkerFixture{ReservationID: r.ID, Reason: "upstream_usage_unknown", CreatedAt: from}).Error)
		}
	}
	response = request(query, "synthetic-overview-5")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	require.NotNil(t, body.Data.PaidCreditUsers)
	assert.Equal(t, int64(2), *body.Data.PaidCreditUsers)
	require.NotNil(t, body.Data.PendingReview)
	assert.Equal(t, int64(2), *body.Data.PendingReview)
	assert.Equal(t, data.Quota, body.Data.Quota)
}
