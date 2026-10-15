package controller_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// This implementation is compiled only by go test. No production source,
// environment switch or global setter can select it in the shipped API router.
type runtimeFixtureIdentityProvider struct {
	identity model.RuntimeIdentity
	err      error
}

func (p runtimeFixtureIdentityProvider) RuntimeIdentity(context.Context) (model.RuntimeIdentity, error) {
	return p.identity, p.err
}

func runtimeFixtureIdentity() runtimeFixtureIdentityProvider {
	return runtimeFixtureIdentityProvider{identity: model.RuntimeIdentity{
		InstanceID:    "6f6ebdbe-9e74-4d25-b608-64eecbe0c0b1",
		ContainerID:   strings.Repeat("c", 64),
		StartedAt:     "2026-10-13T00:00:00Z",
		BuildIdentity: "sha256:" + strings.Repeat("d", 64),
	}}
}

type runtimeSequenceIdentityProvider struct {
	identities []model.RuntimeIdentity
	index      int
}

func (p *runtimeSequenceIdentityProvider) RuntimeIdentity(context.Context) (model.RuntimeIdentity, error) {
	identity := p.identities[p.index]
	p.index++
	return identity, nil
}

// All server fixtures are fixed, isolated test containers with fixture-only
// credentials. No caller-supplied DSN can select an external database.
func runtimeProbeFixture(t *testing.T, emptyCatalog ...bool) (*gin.Engine, string, *gorm.DB) {
	t.Helper()
	priorDB, priorLog := model.DB, model.LOG_DB
	priorMain, priorLogType := common.MainDatabaseType(), common.LogDatabaseType()
	priorPath := common.SQLitePath
	common.SQLitePath = t.TempDir() + "/probe.db"
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	switch os.Getenv("RUNTIME_PROBE_DIALECT") {
	case "", "sqlite":
	case "mysql":
		t.Setenv("SQL_DSN", "root:probe-fixture@tcp(clearapi-runtime-probe-mysql:3306)/runtime_probe_test?charset=utf8mb4&parseTime=True&loc=UTC")
	case "postgres":
		t.Setenv("SQL_DSN", "postgres://postgres:probe-fixture@clearapi-runtime-probe-pg:5432/runtime_probe_test?sslmode=disable")
	default:
		t.Fatal("unsupported isolated dialect")
	}
	require.NoError(t, model.InitDBWithoutMigration())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	priorMap, priorSecret, priorRedis := common.OptionMap, common.SessionSecret, common.RedisEnabled
	priorStore, priorPayment := operation_setting.StoreRechargeEnabled, operation_setting.DisableOnlinePayments
	priorMode := operation_setting.GetBillingReservePolicy().Mode
	common.RedisEnabled = false
	common.SessionSecret = "runtime-probe-fixture-signing-secret"
	t.Cleanup(func() {
		model.DB, model.LOG_DB = priorDB, priorLog
		common.SetDatabaseTypes(priorMain, priorLogType)
		common.SQLitePath = priorPath
		common.OptionMap, common.SessionSecret, common.RedisEnabled = priorMap, priorSecret, priorRedis
		operation_setting.StoreRechargeEnabled, operation_setting.DisableOnlinePayments = priorStore, priorPayment
		// Restore through the existing config registry without a database write.
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_reserve_policy"), map[string]string{"mode": priorMode}))
		_ = sqlDB.Close()
	})
	var version string
	query := "SELECT version()"
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("database=%s version=%s", db.Dialector.Name(), version)
	for _, table := range []any{&model.UserSession{}, &model.User{}, &model.Option{}, &model.StoreProduct{}, &model.CreditApprovalEvent{}, &model.RedemptionBatch{}, &model.Redemption{}, &model.AuditLog{}} {
		require.NoError(t, db.Migrator().DropTable(table))
		require.NoError(t, db.AutoMigrate(table))
	}
	require.NoError(t, model.InitCreditCatalog())
	if len(emptyCatalog) == 0 || !emptyCatalog[0] {
		require.NoError(t, db.Create(&model.StoreProduct{Id: 1, Name: "private-sentinel", FaceValueCents: 100, StoreUrl: model.LegacyCreditPurchaseURL, Enabled: true}).Error)
	}
	require.NoError(t, model.PrepareCreditCatalog())
	require.NoError(t, model.InitReferralConfig())
	common.OptionMap = map[string]string{model.PoolMasterOptionKey: "false", "SMTPToken": "private-sentinel", "TopUpLink": "private-sentinel"}
	require.NoError(t, model.UpdateOption("DisableOnlinePayments", "true"))
	require.NoError(t, model.UpdateOption("StoreRechargeEnabled", "true"))
	require.NoError(t, model.UpdateOption("billing_reserve_policy.mode", "enforce"))
	user := model.User{Username: "probe-private-user", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, Group: "default", AffCode: "probe-fixture"}
	require.NoError(t, db.Create(&user).Error)
	session := model.UserSession{SID: uuid.NewString(), UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: strings.Repeat("0", 64), ExpiresAt: time.Now().Unix() + 3600}
	require.NoError(t, db.Create(&session).Error)
	token, _, err := service.IssueAccessToken(service.AuthIdentity{UserID: user.Id, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/clearapi/runtime-integrity", middleware.DisableCache(), middleware.RuntimeIntegrityAuth(), controller.RuntimeIntegrityHandler(runtimeFixtureIdentity()))
	return engine, token, db
}

func runtimeProbeRequest(engine http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/clearapi/runtime-integrity", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func runtimePublishedProduct(t *testing.T, db *gorm.DB) model.CreditProduct {
	t.Helper()
	catalog, err := model.GetCreditCatalog()
	require.NoError(t, err)
	p := model.CreditProduct{ID: "fixture-product", Name: "private-sentinel", Currency: "CNY", PriceCents: 100, CreditCents: 100, BaseQuota: 500000,
		Revision: 1, State: "published", Published: true, DeliveryVerified: true, EditedBy: 1, EvidenceBatchID: 1, DeliveryBinding: "private-sentinel", DeliveryEvidence: "private-sentinel", PurchaseURL: model.LegacyCreditPurchaseURL}
	digest, err := model.CreditProductDigest(p)
	require.NoError(t, err)
	approval := model.CreditApprovalEvent{ProductID: p.ID, Revision: 1, Digest: digest, Policy: model.CreditApprovalPolicy, EditedBy: 1, ActorID: 1, Decision: "approve", SoloOperator: true}
	require.NoError(t, db.Create(&approval).Error)
	p.ApprovalID = approval.ID
	require.NoError(t, db.Create(&model.RedemptionBatch{Id: 1, ProductID: p.ID, ProductRevision: 1, DeliveryBinding: p.DeliveryBinding, ApprovalID: p.ApprovalID,
		Name: "private-sentinel", Source: model.RedemptionSourceStore, Type: model.RedemptionTypeBalance, Status: model.RedemptionBatchStatusEnabled, FaceValueCents: 100, Quota: 500000, Count: 1}).Error)
	require.NoError(t, db.Create(&model.Redemption{BatchId: 1, Key: "private-sentinel-code", Source: model.RedemptionSourceStore, Type: model.RedemptionTypeBalance, FaceValueCents: 100, Quota: 500000}).Error)
	catalog.ApprovalProducts = []model.CreditProduct{p}
	raw, err := common.Marshal(catalog)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: model.CreditCatalogOptionKey}).Update("value", string(raw)).Error)
	return p
}

func TestRuntimeIntegrityStateAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *gorm.DB)
		code   string
	}{
		{"normal", nil, ""},
		{"valid published product", func(t *testing.T, db *gorm.DB) { runtimePublishedProduct(t, db) }, ""},
		{"unsynchronized database", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: "StoreRechargeEnabled"}).Update("value", "false").Error)
		}, "RUNTIME_CONFIG_MISMATCH"},
		{"unsynchronized billing mode", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: "billing_reserve_policy.mode"}).Update("value", "monitor").Error)
		}, "RUNTIME_CONFIG_MISMATCH"},
		{"unsynchronized payment switch", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: "DisableOnlinePayments"}).Update("value", "false").Error)
		}, "RUNTIME_CONFIG_MISMATCH"},
		{"missing persistent config", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Delete(&model.Option{}, &model.Option{Key: "StoreRechargeEnabled"}).Error)
		}, "PROBE_UNAVAILABLE"},
		{"missing process config", func(t *testing.T, db *gorm.DB) { delete(common.OptionMap, "StoreRechargeEnabled") }, "PROBE_UNAVAILABLE"},
		{"derived config mismatch", func(t *testing.T, db *gorm.DB) { operation_setting.StoreRechargeEnabled = false }, "RUNTIME_CONFIG_MISMATCH"},
		{"approval revoked outside workflow", func(t *testing.T, db *gorm.DB) {
			p := runtimePublishedProduct(t, db)
			require.NoError(t, db.Create(&model.CreditApprovalEvent{Decision: "revoke", ReferenceApprovalID: p.ApprovalID}).Error)
		}, "APPROVAL_VIEW_INVALID"},
		{"revision mismatch", func(t *testing.T, db *gorm.DB) {
			p := runtimePublishedProduct(t, db)
			require.NoError(t, db.Model(&model.CreditApprovalEvent{}).Where("id = ?", p.ApprovalID).Update("revision", 2).Error)
		}, "APPROVAL_VIEW_INVALID"},
		{"binding mismatch", func(t *testing.T, db *gorm.DB) {
			runtimePublishedProduct(t, db)
			require.NoError(t, db.Model(&model.RedemptionBatch{}).Where("id = ?", 1).Update("delivery_binding", "changed").Error)
		}, "APPROVAL_VIEW_INVALID"},
		{"unknown legacy product", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Create(&model.StoreProduct{Id: 2, Name: "private-sentinel", FaceValueCents: 100, StoreUrl: model.LegacyCreditPurchaseURL}).Error)
		}, "APPROVAL_VIEW_INVALID"},
		{"changed frozen legacy product", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Model(&model.StoreProduct{}).Where("id = ?", 1).Update("face_value_cents", 200).Error)
		}, "APPROVAL_VIEW_INVALID"},
		{"closed store still validates approvals", func(t *testing.T, db *gorm.DB) {
			p := runtimePublishedProduct(t, db)
			require.NoError(t, model.UpdateOption("StoreRechargeEnabled", "false"))
			require.NoError(t, db.Model(&model.CreditApprovalEvent{}).Where("id = ?", p.ApprovalID).Update("digest", "private-sentinel").Error)
		}, "APPROVAL_VIEW_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, token, db := runtimeProbeFixture(t)
			if tc.change != nil {
				tc.change(t, db)
			}
			writes := 0
			rejectWrite := func(tx *gorm.DB) { writes++; tx.AddError(fmt.Errorf("unexpected write")) }
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("probe_no_write", rejectWrite))
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("probe_no_write", rejectWrite))
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("probe_no_write", rejectWrite))
			rec := runtimeProbeRequest(engine, token)
			assert.Zero(t, writes)
			assert.NotContains(t, rec.Body.String(), "private-sentinel")
			assert.NotContains(t, rec.Body.String(), token)
			assert.NotContains(t, rec.Body.String(), "probe-private-user")
			if tc.code != "" {
				require.Equal(t, http.StatusServiceUnavailable, rec.Code)
				assert.JSONEq(t, `{"success":false,"status":"BLOCKED","code":"`+tc.code+`"}`, rec.Body.String())
				return
			}
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var body struct {
				Success bool           `json:"success"`
				Data    map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &body))
			assert.True(t, body.Success)
			assert.Equal(t, "1", body.Data["schema_version"])
			assert.Equal(t, "1", body.Data["report_version"])
			assert.Equal(t, "PASS", body.Data["status"])
			assert.Len(t, body.Data, 20)
			keys := make([]string, 0, len(body.Data))
			for key := range body.Data {
				keys = append(keys, key)
			}
			assert.ElementsMatch(t, []string{"instance_id", "build_identity", "config_generation", "online_payment_enabled", "store_enabled", "billing_mode", "referral_enabled", "pool_enabled", "approval_schema", "catalog_version", "product_view_digest", "generated_at", "schema_version", "report_version", "status", "container_id", "started_at", "config_digest", "approval_view_digest", "effective_product_count"}, keys)
			assert.Equal(t, false, body.Data["online_payment_enabled"])
			assert.Equal(t, true, body.Data["store_enabled"])
			assert.Equal(t, "enforce", body.Data["billing_mode"])
			assert.Equal(t, false, body.Data["referral_enabled"])
			assert.Equal(t, false, body.Data["pool_enabled"])
			for _, key := range []string{"instance_id", "build_identity", "config_generation", "product_view_digest", "generated_at", "schema_version", "report_version", "status", "container_id", "started_at", "config_digest", "approval_view_digest", "effective_product_count"} {
				assert.NotEmpty(t, body.Data[key])
			}
			first, err := model.ObserveRuntimeIntegrityWithProvider(context.Background(), runtimeFixtureIdentity())
			require.NoError(t, err)
			second, err := model.ObserveRuntimeIntegrityWithProvider(context.Background(), runtimeFixtureIdentity())
			require.NoError(t, err)
			assert.Equal(t, first.Report.ConfigGeneration, second.Report.ConfigGeneration)
			assert.Equal(t, first.Report.InstanceID, second.Report.InstanceID)
			assert.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
		})
	}
}

func TestRuntimeIntegrityPermissions(t *testing.T) {
	engine, token, db := runtimeProbeFixture(t)
	writes := 0
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("probe_permission_no_write", func(tx *gorm.DB) { writes++; tx.AddError(fmt.Errorf("unexpected write")) }))
	for _, credential := range []string{"", "private-sentinel-pat", "invalid.jwt.token"} {
		response := runtimeProbeRequest(engine, credential)
		assert.Equal(t, http.StatusUnauthorized, response.Code)
		assert.JSONEq(t, `{"success":false,"code":"AUTH_REQUIRED"}`, response.Body.String())
	}
	parts := strings.Split(token, ".")
	if parts[2][0] == 'A' {
		parts[2] = "B" + parts[2][1:]
	} else {
		parts[2] = "A" + parts[2][1:]
	}
	assert.Equal(t, http.StatusUnauthorized, runtimeProbeRequest(engine, strings.Join(parts, ".")).Code)
	assert.Zero(t, writes, "denied credentials must not create audits")
	common.RedisEnabled = true
	redisResponse := runtimeProbeRequest(engine, token)
	assert.Equal(t, http.StatusServiceUnavailable, redisResponse.Code)
	assert.JSONEq(t, `{"success":false,"code":"PROBE_UNAVAILABLE"}`, redisResponse.Body.String())
	common.RedisEnabled = false
	var user model.User
	require.NoError(t, db.First(&user).Error)
	require.NoError(t, db.Model(&user).Update("role", common.RoleAdminUser).Error)
	admin := runtimeProbeRequest(engine, token)
	assert.Equal(t, http.StatusForbidden, admin.Code)
	assert.JSONEq(t, `{"success":false,"code":"AUTH_REQUIRED"}`, admin.Body.String())
	require.NoError(t, db.Model(&user).Update("role", common.RoleRootUser).Error)
	require.NoError(t, db.Model(&user).Update("status", common.UserStatusDisabled).Error)
	denied := runtimeProbeRequest(engine, token)
	assert.Equal(t, http.StatusUnauthorized, denied.Code)
	assert.JSONEq(t, `{"success":false,"code":"AUTH_REQUIRED"}`, denied.Body.String())
	require.NoError(t, db.Model(&user).Update("status", common.UserStatusEnabled).Error)
	require.NoError(t, db.Model(&user).Update("auth_version", 2).Error)
	assert.Equal(t, http.StatusUnauthorized, runtimeProbeRequest(engine, token).Code)
	require.NoError(t, db.Model(&user).Update("auth_version", 1).Error)
	require.NoError(t, db.Model(&model.UserSession{}).Where("user_id = ?", user.Id).Update("expires_at", time.Now().Unix()-1).Error)
	assert.Equal(t, http.StatusUnauthorized, runtimeProbeRequest(engine, token).Code)
	assert.Zero(t, writes)
	assert.Equal(t, http.StatusNotFound, func() int {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/clearapi/runtime-integrity", nil))
		return rec.Code
	}())
}

func TestRuntimeIntegrityRevocationAndGeneration(t *testing.T) {
	engine, token, db := runtimeProbeFixture(t)
	p := runtimePublishedProduct(t, db)
	before := runtimeReport(t, engine, token)
	catalog, err := model.GetCreditCatalog()
	require.NoError(t, err)
	review, err := model.GetCreditProductReview(p.ID)
	require.NoError(t, err)
	require.NoError(t, model.ChangeCreditProductState(p.ID, catalog.Version, p.Revision, "revoke", "REVOKE "+review.Confirmation, 1))
	after := runtimeReport(t, engine, token)
	assert.NotEqual(t, before.ProductViewDigest, after.ProductViewDigest)
	assert.NotEqual(t, before.ApprovalViewDigest, after.ApprovalViewDigest)
	assert.Greater(t, after.CatalogVersion, before.CatalogVersion)
	assert.Equal(t, before.ConfigGeneration, after.ConfigGeneration)
	require.NoError(t, model.UpdateOption("StoreRechargeEnabled", "false"))
	changed := runtimeReport(t, engine, token)
	assert.False(t, changed.StoreEnabled)
	assert.NotEqual(t, after.ConfigGeneration, changed.ConfigGeneration)
	referral, err := model.GetReferralConfig()
	require.NoError(t, err)
	referral.RewardsEnabled = true
	raw, err := common.Marshal(referral)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: model.ReferralOptionKey}).Update("value", string(raw)).Error)
	reward := runtimeReport(t, engine, token)
	assert.True(t, reward.ReferralEnabled, "referral business uses the DB gate directly, without an option-map sync")
	assert.NotEqual(t, changed.ConfigGeneration, reward.ConfigGeneration)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = model.ObserveRuntimeIntegrityWithProvider(canceled, runtimeFixtureIdentity())
	assert.ErrorIs(t, err, model.ErrProbeUnavailable)
}

func runtimeReport(t *testing.T, engine http.Handler, token string) model.RuntimeIntegrity {
	t.Helper()
	response := runtimeProbeRequest(engine, token)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data model.RuntimeIntegrity `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	return body.Data
}

func TestRuntimeIntegrityDigests(t *testing.T) {
	engine, token, db := runtimeProbeFixture(t)
	legacy := runtimeReport(t, engine, token)
	p := runtimePublishedProduct(t, db)
	published := runtimeReport(t, engine, token)
	assert.NotEqual(t, legacy.ProductViewDigest, published.ProductViewDigest, "adding a purchasable approval changes the live projection")
	assert.NotEqual(t, legacy.ApprovalViewDigest, published.ApprovalViewDigest)
	require.NoError(t, db.Model(&model.CreditApprovalEvent{}).Where("id = ?", p.ApprovalID).Update("created_time", 1234).Error)
	eventChanged := runtimeReport(t, engine, token)
	assert.NotEqual(t, published.ApprovalViewDigest, eventChanged.ApprovalViewDigest)
	assert.Equal(t, published.ProductViewDigest, eventChanged.ProductViewDigest, "private approval history is separate from purchase presentation")

	catalog, err := model.GetCreditCatalog()
	require.NoError(t, err)
	p.Revision++
	catalog.ApprovalProducts = []model.CreditProduct{p}
	raw, err := common.Marshal(catalog)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: model.CreditCatalogOptionKey}).Update("value", string(raw)).Error)
	require.NoError(t, db.Model(&model.CreditApprovalEvent{}).Where("id = ?", p.ApprovalID).Update("revision", p.Revision).Error)
	require.NoError(t, db.Model(&model.RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Update("product_revision", p.Revision).Error)
	revisionChanged := runtimeReport(t, engine, token)
	assert.NotEqual(t, eventChanged.ProductViewDigest, revisionChanged.ProductViewDigest)
	assert.NotEqual(t, eventChanged.ApprovalViewDigest, revisionChanged.ApprovalViewDigest)

	// A valid mapped legacy claim retains compatibility until the existing
	// revocation workflow withdraws it. No unified-view activation is needed.
	p.LegacyStoreID = 1
	catalog.ApprovalProducts = []model.CreditProduct{p}
	raw, err = common.Marshal(catalog)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: model.CreditCatalogOptionKey}).Update("value", string(raw)).Error)
	digest, err := model.CreditProductDigest(p)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.CreditApprovalEvent{}).Where("id = ?", p.ApprovalID).Update("digest", digest).Error)
	mapped := runtimeReport(t, engine, token)
	assert.NotEqual(t, revisionChanged.ProductViewDigest, mapped.ProductViewDigest)
	review, err := model.GetCreditProductReview(p.ID)
	require.NoError(t, err)
	require.NoError(t, model.ChangeCreditProductState(p.ID, catalog.Version, p.Revision, "revoke", "REVOKE "+review.Confirmation, 1))
	withdrawn := runtimeReport(t, engine, token)
	assert.NotEqual(t, mapped.ProductViewDigest, withdrawn.ProductViewDigest, "legacy withdrawal changes actual wallet products")
	assert.NotEqual(t, mapped.ApprovalViewDigest, withdrawn.ApprovalViewDigest)
}

func TestRuntimeIntegrityContainerIdentity(t *testing.T) {
	engine, token, _ := runtimeProbeFixture(t)
	real := runtimeReport(t, engine, token)
	require.Len(t, real.ContainerID, 64)
	parsed, err := uuid.Parse(real.InstanceID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(4), parsed.Version())
	t.Setenv("HOSTNAME", strings.Repeat("a", 64))
	t.Setenv("CLEARAPI_INSTANCE_ID", strings.Repeat("b", 64))
	unchanged := runtimeReport(t, engine, token)
	assert.Equal(t, real.InstanceID, unchanged.InstanceID)
	assert.Equal(t, real.ContainerID, unchanged.ContainerID)
	// Missing or malformed identities still pass through the shared v1 checks.
	for _, test := range []struct {
		name     string
		provider runtimeFixtureIdentityProvider
	}{
		{"missing", runtimeFixtureIdentityProvider{err: model.ErrProbeUnavailable}},
		{"empty", runtimeFixtureIdentityProvider{}},
		{"wrong container", runtimeFixtureIdentityProvider{identity: model.RuntimeIdentity{InstanceID: real.InstanceID, ContainerID: "wrong", StartedAt: real.StartedAt, BuildIdentity: real.BuildIdentity}}},
		{"future", runtimeFixtureIdentityProvider{identity: model.RuntimeIdentity{InstanceID: real.InstanceID, ContainerID: real.ContainerID, StartedAt: "2999-01-01T00:00:00Z", BuildIdentity: real.BuildIdentity}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			deniedEngine := gin.New()
			deniedEngine.GET("/api/clearapi/runtime-integrity", middleware.DisableCache(), middleware.RuntimeIntegrityAuth(), controller.RuntimeIntegrityHandler(test.provider))
			response := runtimeProbeRequest(deniedEngine, token)
			assert.Equal(t, http.StatusServiceUnavailable, response.Code)
			assert.JSONEq(t, `{"success":false,"status":"BLOCKED","code":"PROBE_UNAVAILABLE"}`, response.Body.String())
		})
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/clearapi/runtime-integrity?instance_id="+real.InstanceID, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.JSONEq(t, `{"success":false,"status":"BLOCKED","code":"PROBE_UNAVAILABLE"}`, response.Body.String())
}

// This opt-in artifact contains only the API whitelist and bridges the real
// producer to the existing shell consumer test. It uses isolated fixtures only.
func TestRuntimeIntegrityContractArtifact(t *testing.T) {
	if os.Getenv("RUNTIME_PROBE_EXPORT") != "1" {
		t.Skip("contract artifact not requested")
	}
	engine, token, _ := runtimeProbeFixture(t, true)
	require.NoError(t, model.UpdateOption("StoreRechargeEnabled", "false"))
	response := runtimeProbeRequest(engine, token)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, os.WriteFile("/tmp/clearapi-runtime-v1-report.json", response.Body.Bytes(), 0o600))
}

func TestRuntimeIntegrityCollectionFence(t *testing.T) {
	for _, test := range []struct {
		name     string
		change   func(*testing.T, *gorm.DB)
		expected error
	}{
		{"config drift", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, model.UpdateOption("StoreRechargeEnabled", "false"))
		}, model.ErrRuntimeConfigMismatch},
		{"approval drift", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Model(&model.CreditApprovalEvent{}).Where("id > ?", 0).Update("created_time", 5678).Error)
		}, model.ErrApprovalViewInvalid},
		{"projection unavailable", nil, model.ErrProductViewInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, db := runtimeProbeFixture(t)
			runtimePublishedProduct(t, db)
			_, err := service.CollectRuntimeIntegrityReportWithProvider(context.Background(), func() ([]byte, error) {
				if test.change == nil {
					return nil, fmt.Errorf("private-sentinel")
				}
				test.change(t, db)
				return []byte("[]"), nil
			}, runtimeFixtureIdentity())
			assert.ErrorIs(t, err, test.expected)
		})
	}
}

func TestRuntimeIntegrityCollectionRejectsIdentityDrift(t *testing.T) {
	_, _, _ = runtimeProbeFixture(t)
	first := runtimeFixtureIdentity().identity
	second := first
	second.ContainerID = strings.Repeat("e", 64)
	provider := &runtimeSequenceIdentityProvider{identities: []model.RuntimeIdentity{first, second}}

	_, err := service.CollectRuntimeIntegrityReportWithProvider(context.Background(), func() ([]byte, error) {
		return []byte("[]"), nil
	}, provider)
	require.ErrorIs(t, err, model.ErrProbeUnavailable)
}
