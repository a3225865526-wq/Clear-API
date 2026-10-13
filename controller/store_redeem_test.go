package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupStoreControllerTest(t *testing.T) (*gin.Engine, *model.User, string) {
	t.Helper()

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Redemption{},
		&model.RedemptionBatch{},
		&model.VIPLevel{},
		&model.BalanceTransaction{},
		&model.AuditLog{},
	))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
		sqlDB.Close()
	})

	previousFlag := operation_setting.StoreRechargeEnabled
	operation_setting.StoreRechargeEnabled = true
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = previousFlag })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RequestId())
	router.POST("/api/clearapi/store/redeem", middleware.UserAuth(), RedeemStoreCode)

	token := "store-redeem-test-token"
	user := model.User{
		Username:    "store-redeem-user",
		Password:    "store-test-password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "store-redeem-user",
		AccessToken: &token,
	}
	require.NoError(t, db.Create(&user).Error)

	code := model.Redemption{
		Name:           "store-test",
		Key:            "controller-code-0000000000000000000001",
		Status:         common.RedemptionCodeStatusEnabled,
		Quota:          1000000,
		CreatedTime:    common.GetTimestamp(),
		FaceValueCents: 10000,
		Type:           model.RedemptionTypeBalance,
		Source:         model.RedemptionSourceStore,
	}
	require.NoError(t, db.Create(&code).Error)
	return router, &user, code.Key
}

func TestCreditApprovalHTTPPermissionsAndUnifiedWallet(t *testing.T) {
	admin, adminPAT := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.StoreProduct{}, &model.CreditApprovalEvent{}, &model.RedemptionBatch{}, &model.Redemption{}, &model.AuthFlow{}, &model.TwoFA{}, &model.PasskeyCredential{}))
	require.NoError(t, model.InitCreditCatalog())
	require.NoError(t, model.DB.Create(&model.StoreProduct{Id: 1, Name: "legacy", FaceValueCents: 100, StoreUrl: model.LegacyCreditPurchaseURL, Enabled: true}).Error)
	require.NoError(t, model.PrepareCreditCatalog())
	priorFlag := operation_setting.StoreRechargeEnabled
	operation_setting.StoreRechargeEnabled = true
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = priorFlag })
	rootPAT := "sample-root-pat-fixture"
	root := model.User{Username: "store-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, Password: "fixture-password", AccessToken: &rootPAT}
	require.NoError(t, model.DB.Create(&root).Error)
	userPAT := "store-user-pat-fixture"
	user := model.User{Username: "store-common", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "store-common-fixture", AuthVersion: 1, AccessToken: &userPAT}
	require.NoError(t, model.DB.Create(&user).Error)
	now := time.Now().Unix()
	session := &model.UserSession{SID: "store-approval-session", UserID: root.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: "fixture-refresh-hash", LoginMethod: "password", LastActiveAt: now, ExpiresAt: now + 3600}
	require.NoError(t, model.CreateUserSession(session))
	identity := service.AuthIdentity{UserID: root.Id, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1}
	jwt, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	catalog, err := model.GetCreditCatalog()
	require.NoError(t, err)
	catalog.Products = append(catalog.Products, model.CreditProduct{ID: "explicit", Name: "explicit", PriceCents: 100, CreditCents: 100, BaseQuota: 500000, PurchaseURL: model.LegacyCreditPurchaseURL, DeliveryEvidence: "delivery/reference", DeliveryBinding: "catfk/item/qmcse2"})
	require.NoError(t, model.SaveCreditCatalog(catalog, root.Id))
	g := gin.New()
	g.POST("/products/:id/:action", middleware.RootAuth(), ChangeCreditProductState)
	g.POST("/legacy", middleware.AdminAuth(), AddStoreProduct)
	g.PUT("/edit", middleware.RootAuth(), SaveCreditCatalog)
	g.GET("/purchase/:kind/:id", middleware.DisableCache(), ResolveCreditPurchase)
	g.POST("/samples", middleware.AdminAuth(), AddRedemptionBatch)
	request := func(path, token string, body any, proof string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if proof != "" {
			req.Header.Set("X-Security-Proof", proof)
		}
		response := httptest.NewRecorder()
		g.ServeHTTP(response, req)
		return response
	}
	sampleInput := map[string]any{"name": "verified sample", "face_value_cents": 100, "count": 1, "product_id": "explicit", "product_revision": 1, "evidence_only": true}
	assert.Contains(t, request("/samples", adminPAT, sampleInput, "").Body.String(), "Only root")
	assert.Equal(t, http.StatusUnauthorized, request("/samples", "", sampleInput, "").Code)
	assert.Contains(t, request("/samples", rootPAT, sampleInput, "").Body.String(), "live root session")
	assert.Contains(t, request("/samples", jwt, sampleInput, "").Body.String(), `"success":true`)
	duplicate := request("/samples", jwt, sampleInput, "").Body.String()
	assert.Contains(t, duplicate, `"success":false`)
	assert.NotContains(t, duplicate, `"keys"`)
	for _, action := range []string{"submit", "approve", "publish", "revoke", "map"} {
		assert.Equal(t, http.StatusForbidden, request("/products/explicit/"+action, adminPAT, map[string]any{}, "").Code)
		assert.Equal(t, http.StatusForbidden, request("/products/explicit/"+action, userPAT, map[string]any{}, "").Code)
		assert.Equal(t, http.StatusUnauthorized, request("/products/explicit/"+action, "", map[string]any{}, "").Code)
	}
	assert.Equal(t, common.RoleAdminUser, admin.Role)
	legacyResponse := request("/legacy", adminPAT, map[string]any{"name": "bypass", "enabled": true}, "")
	assert.Contains(t, legacyResponse.Body.String(), "Legacy products are frozen")
	catalog, err = model.GetCreditCatalog()
	require.NoError(t, err)
	review, err := model.GetCreditProductReview("explicit")
	require.NoError(t, err)
	input := map[string]any{"version": catalog.Version, "revision": review.Product.Revision}
	response := request("/products/explicit/submit", jwt, input, "")
	assert.Contains(t, response.Body.String(), `"success":true`)
	catalog, err = model.GetCreditCatalog()
	require.NoError(t, err)
	review, err = model.GetCreditProductReview("explicit")
	require.NoError(t, err)
	input = map[string]any{"version": catalog.Version, "revision": review.Product.Revision, "digest": review.Digest, "confirmation": "APPROVE " + review.Confirmation}
	assert.Contains(t, request("/products/explicit/approve", jwt, input, "").Body.String(), "SECURITY_PROOF_REQUIRED")
	raw, err := common.Marshal(service.StoreProductOperationContext{ProductID: "explicit", Version: catalog.Version, Revision: review.Product.Revision, Action: "approve", Digest: review.Digest})
	require.NoError(t, err)
	binding, err := service.BindVerificationOperation(service.VerificationOperation{Scope: service.VerificationScopeStoreProductOperation, Context: raw})
	require.NoError(t, err)
	proof, _, err := service.IssueSecurityProof(identity, "password", binding)
	require.NoError(t, err)
	response = request("/products/explicit/approve", jwt, input, proof)
	assert.Contains(t, response.Body.String(), `"success":true`)
	assert.NotContains(t, request("/products/explicit/approve", jwt, input, proof).Body.String(), `"success":true`)
	// Explicit mapping and cutover remain local service operations in this test.
	catalog, err = model.GetCreditCatalog()
	require.NoError(t, err)
	require.NoError(t, model.MapLegacyCreditProduct("explicit", catalog.Version, review.Product.Revision, 1, root.Id))
	_, _, err = model.CreateRedemptionBatch(model.CreateRedemptionBatchInput{Name: "mapped sample", FaceValueCents: 100, Count: 1, AdminUserID: root.Id, ProductID: "explicit", ProductRevision: review.Product.Revision + 1, EvidenceOnly: true})
	require.NoError(t, err)
	for _, action := range []string{"submit", "approve", "publish"} {
		catalog, err = model.GetCreditCatalog()
		require.NoError(t, err)
		review, err = model.GetCreditProductReview("explicit")
		require.NoError(t, err)
		confirmation := strings.ToUpper(action) + " " + review.Confirmation
		require.NoError(t, model.ChangeCreditProductState("explicit", catalog.Version, review.Product.Revision, action, confirmation, root.Id))
	}
	catalog, err = model.GetCreditCatalog()
	require.NoError(t, err)
	require.NoError(t, model.ActivateUnifiedCreditView(catalog.Version, root.Id))
	public, err := model.GetPublicCreditCatalog()
	require.NoError(t, err)
	wallet, err := getStoreProductsForUser()
	require.NoError(t, err)
	require.Len(t, wallet, 1)
	require.Len(t, public.Products, 1)
	assert.Equal(t, public.Products[0].ID, wallet[0]["id"])
	assert.Equal(t, public.Products[0].PurchaseURL, wallet[0]["store_url"])
	getPurchase := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		g.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	response = getPurchase("/purchase/product/explicit?revision=2")
	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, model.LegacyCreditPurchaseURL, response.Header().Get("Location"))
	assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
	assert.Equal(t, http.StatusConflict, getPurchase("/purchase/product/explicit?revision=1").Code)
	assert.Equal(t, http.StatusNotFound, getPurchase("/purchase/legacy/1").Code)
	catalog, err = model.GetCreditCatalog()
	require.NoError(t, err)
	review, err = model.GetCreditProductReview("explicit")
	require.NoError(t, err)
	require.NoError(t, model.ChangeCreditProductState("explicit", catalog.Version, review.Product.Revision, "revoke", "REVOKE "+review.Confirmation, root.Id))
	assert.Equal(t, http.StatusNotFound, getPurchase("/purchase/product/explicit?revision=2").Code)
}

func postStoreRedeem(router *gin.Engine, token string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/clearapi/store/redeem", bytes.NewBufferString(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestRedeemStoreCodeHandler(t *testing.T) {
	router, user, key := setupStoreControllerTest(t)

	t.Run("unauthenticated rejected", func(t *testing.T) {
		response := postStoreRedeem(router, "", `{"key":"controller-code-0000000000000000000001"}`)
		assert.Equal(t, http.StatusUnauthorized, response.Code)
	})

	t.Run("redeem success", func(t *testing.T) {
		response := postStoreRedeem(router, "store-redeem-test-token", `{"key":"controller-code-0000000000000000000001"}`)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				BaseQuota      int    `json:"base_quota"`
				BonusQuota     int    `json:"bonus_quota"`
				TotalQuota     int    `json:"total_quota"`
				Balance        int    `json:"balance"`
				FaceValueCents int64  `json:"face_value_cents"`
				VIPName        string `json:"vip_name"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.True(t, result.Success)
		assert.Equal(t, 1000000, result.Data.BaseQuota)
		assert.Equal(t, 1000000, result.Data.Balance)
		assert.EqualValues(t, 10000, result.Data.FaceValueCents)

		var entries []model.BalanceTransaction
		require.NoError(t, model.DB.Where("user_id = ?", user.Id).Find(&entries).Error)
		require.Len(t, entries, 1)
		assert.Equal(t, model.BalanceTxTypeRecharge, entries[0].Type)
	})

	t.Run("second redeem fails with generic message", func(t *testing.T) {
		response := postStoreRedeem(router, "store-redeem-test-token", `{"key":"controller-code-0000000000000000000001"}`)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.False(t, result.Success)
		assert.NotContains(t, result.Message, key)
	})

	t.Run("missing key rejected", func(t *testing.T) {
		response := postStoreRedeem(router, "store-redeem-test-token", `{}`)
		assert.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.False(t, result.Success)
	})
}

func TestRedeemStoreCodeHandlerDisabledFlag(t *testing.T) {
	router, _, _ := setupStoreControllerTest(t)
	operation_setting.StoreRechargeEnabled = false
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = true })

	response := postStoreRedeem(router, "store-redeem-test-token", `{"key":"controller-code-0000000000000000000001"}`)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "商城充值当前未开启")
}

func TestStoreAdminRoutesRequireAdmin(t *testing.T) {
	_, _, _ = setupStoreControllerTest(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/redemption/batches", middleware.AdminAuth(), GetAllRedemptionBatches)
	router.POST("/api/clearapi/store/redeem", middleware.UserAuth(), RedeemStoreCode)

	// 普通用户 token 调用管理员批次接口被拒。
	request := httptest.NewRequest(http.MethodGet, "/api/redemption/batches", nil)
	request.Header.Set("Authorization", "Bearer store-redeem-test-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.NotEqual(t, http.StatusOK, response.Code)

	// 未登录调用商城兑换被拒。
	request = httptest.NewRequest(http.MethodPost, "/api/clearapi/store/redeem", bytes.NewBufferString(`{"key":"x"}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
}

// STEP 5: current_threshold_cents must reflect the matched level's threshold
// so the wallet can render the segment progress bar. Display-only field: it
// must not affect any bonus/total_recharge/quota calculation.
func TestGetUserVipInfoCurrentThreshold(t *testing.T) {
	_, user, _ := setupStoreControllerTest(t)

	now := common.GetTimestamp()
	require.NoError(t, model.DB.Create(&[]model.VIPLevel{
		{Name: "Base", ThresholdCents: 0, BonusBasisPoints: 0, Enabled: true, Sort: 0, CreatedTime: now, UpdatedTime: now},
		{Name: "TestVIP1", ThresholdCents: 1000, BonusBasisPoints: 200, Enabled: true, Sort: 1, CreatedTime: now, UpdatedTime: now},
		{Name: "TestVIP2", ThresholdCents: 2000, BonusBasisPoints: 400, Enabled: true, Sort: 2, CreatedTime: now, UpdatedTime: now},
	}).Error)

	// Below TestVIP1: base level, current threshold 0, next is TestVIP1.
	info := getUserVipInfo(user.Id)
	require.NotNil(t, info)
	assert.Equal(t, "Base", info["name"])
	assert.EqualValues(t, 0, info["current_threshold_cents"])
	next, ok := info["next_level"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "TestVIP1", next["name"])
	assert.EqualValues(t, 1000, next["remaining_cents"])

	// Cross into TestVIP1: current threshold follows the level.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).
		Update("total_recharge_cents", 1100).Error)
	info = getUserVipInfo(user.Id)
	assert.Equal(t, "TestVIP1", info["name"])
	assert.EqualValues(t, 1000, info["current_threshold_cents"])
	next = info["next_level"].(map[string]any)
	assert.Equal(t, "TestVIP2", next["name"])
	assert.EqualValues(t, 900, next["remaining_cents"])

	// Highest enabled level: next_level stays nil.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).
		Update("total_recharge_cents", 9900).Error)
	info = getUserVipInfo(user.Id)
	assert.Equal(t, "TestVIP2", info["name"])
	assert.EqualValues(t, 2000, info["current_threshold_cents"])
	assert.Nil(t, info["next_level"])
}
