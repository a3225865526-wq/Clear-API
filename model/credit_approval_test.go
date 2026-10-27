package model

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// This fixture accepts no external DSN and cannot target a production database.
func creditApprovalFixture(t *testing.T) {
	t.Helper()
	priorDB, priorLog := DB, LOG_DB
	priorFlag := operation_setting.StoreRechargeEnabled
	var dialect gorm.Dialector = sqlite.Open(t.TempDir() + "/approval.db")
	databaseType := common.DatabaseTypeSQLite
	switch os.Getenv("STORE_APPROVAL_DIALECT") {
	case "mysql":
		dialect = mysql.Open("root:stage2a-fixture@tcp(clearapi-store-approval-mysql:3306)/store_approval_test?charset=utf8mb4&parseTime=True&loc=UTC")
		databaseType = common.DatabaseTypeMySQL
	case "postgres":
		dialect = postgres.Open("host=clearapi-store-approval-pg user=postgres password=stage2a-fixture dbname=store_approval_test port=5432 sslmode=disable")
		databaseType = common.DatabaseTypePostgreSQL
	case "", "sqlite":
	default:
		t.Fatal("unsupported isolated test dialect")
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(databaseType, databaseType)
	initCol()
	operation_setting.StoreRechargeEnabled = true
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if databaseType == common.DatabaseTypeSQLite {
		sqlDB.SetMaxOpenConns(1)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		DB, LOG_DB = priorDB, priorLog
		operation_setting.StoreRechargeEnabled = priorFlag
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		initCol()
	})
	var version string
	query := "SELECT version()"
	if databaseType == common.DatabaseTypeSQLite {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("%s %s", db.Dialector.Name(), version)
	for _, table := range []any{&FinanceCashReceipt{}, &OfflineOrderCode{}, &OfflineOrder{}, &BalanceTransaction{}, &FinanceSyncEvent{}, &ReferralReward{}, &Log{}, &User{}, &CreditApprovalEvent{}, &Option{}, &StoreProduct{}, &RedemptionBatch{}, &Redemption{}} {
		require.NoError(t, db.Migrator().DropTable(table))
	}
	for range 2 {
		require.NoError(t, db.AutoMigrate(&CreditApprovalEvent{}, &Option{}, &StoreProduct{}, &RedemptionBatch{}, &Redemption{}))
	}
	legacy := StoreProduct{Id: 1, Name: "existing", FaceValueCents: 100, StoreUrl: LegacyCreditPurchaseURL, Enabled: true, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, db.Create(&RedemptionBatch{Id: 77, Name: "preparation legacy", FaceValueCents: 100, Quota: 500000, Source: RedemptionSourceStore, Type: RedemptionTypeBalance, Status: RedemptionBatchStatusEnabled, Count: 1}).Error)
	require.NoError(t, db.Create(&Redemption{Key: "legacy-fixture-only", BatchId: 77, Quota: 500000, FaceValueCents: 100, Source: RedemptionSourceStore, Type: RedemptionTypeBalance, Status: common.RedemptionCodeStatusEnabled}).Error)
	for range 2 {
		require.NoError(t, migrateCreditCatalogSchema())
	}
}

type creditApprovalLegacyBatch struct {
	Id             int
	Name           string `gorm:"type:varchar(128);index"`
	Source         string `gorm:"type:varchar(16);default:'STORE'"`
	Type           string `gorm:"type:varchar(16);default:'BALANCE'"`
	FaceValueCents int64  `gorm:"type:bigint"`
	Quota          int    `gorm:"default:0"`
	Count          int    `gorm:"default:0"`
	ExpiredTime    int64  `gorm:"bigint"`
	Status         int    `gorm:"default:1;index"`
	Note           string `gorm:"type:varchar(255);default:''"`
	CreatedBy      int
	CreatedTime    int64 `gorm:"bigint;index"`
	UpdatedTime    int64 `gorm:"bigint"`
}

func (creditApprovalLegacyBatch) TableName() string { return "redemption_batches" }

func TestCreditApprovalReleasedSchemaUpgradeAndConcurrentReview(t *testing.T) {
	creditApprovalFixture(t)
	require.NoError(t, DB.Migrator().DropTable(&RedemptionBatch{}))
	require.NoError(t, DB.AutoMigrate(&creditApprovalLegacyBatch{}))
	old := creditApprovalLegacyBatch{Id: 77, Name: "historical", Quota: 123456, FaceValueCents: 100, Source: RedemptionSourceStore, Type: RedemptionTypeBalance, Count: 3, Note: "frozen history"}
	require.NoError(t, DB.Create(&old).Error)
	for range 2 {
		require.NoError(t, migrateCreditCatalogSchema())
	}
	var upgraded RedemptionBatch
	require.NoError(t, DB.First(&upgraded, 77).Error)
	assert.Equal(t, old.Quota, upgraded.Quota)
	assert.Equal(t, old.FaceValueCents, upgraded.FaceValueCents)
	assert.Empty(t, upgraded.ProductID)
	assert.Zero(t, upgraded.ProductRevision)
	assert.Equal(t, old.Note, upgraded.Note)
	var row Option
	require.NoError(t, DB.Where(&Option{Key: CreditCatalogOptionKey}).First(&row).Error)
	var previousImageView struct {
		Products []CreditProduct `json:"products"`
	}
	require.NoError(t, common.UnmarshalJsonStr(row.Value, &previousImageView))
	assert.Equal(t, DefaultCreditCatalog().Products, previousImageView.Products, "previous image retains its original product projection before cutover")
	p := creditApprovalProduct(t, "concurrent", 100)
	creditApprovalAction(t, p.ID, "submit")
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			results <- ChangeCreditProductState(p.ID, c.Version, p.Revision, "approve", "APPROVE "+review.Confirmation, 7)
		})
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	var events int64
	require.NoError(t, DB.Model(&CreditApprovalEvent{}).Where("product_id = ? AND decision = ?", p.ID, "approve").Count(&events).Error)
	assert.EqualValues(t, 1, events)
}

func creditApprovalProduct(t *testing.T, id string, price int64) CreditProduct {
	t.Helper()
	catalog, err := GetCreditCatalog()
	require.NoError(t, err)
	p := CreditProduct{ID: id, Name: id, Currency: "CNY", PriceCents: price, CreditCents: 100, BaseQuota: 500000, PurchaseURL: LegacyCreditPurchaseURL, DeliveryEvidence: "delivery-check/2026-10", DeliveryBinding: "catfk/item/qmcse2"}
	catalog.Products = append(catalog.Products, p)
	require.NoError(t, SaveCreditCatalog(catalog, 7))
	return creditApprovalSample(t, id)
}

func creditApprovalSample(t *testing.T, id string) CreditProduct {
	t.Helper()
	review, err := GetCreditProductReview(id)
	require.NoError(t, err)
	_, _, err = CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "delivery evidence", FaceValueCents: review.Product.PriceCents, Count: 1, AdminUserID: 7, ProductID: id, ProductRevision: review.Product.Revision, EvidenceOnly: true})
	require.NoError(t, err)
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	for _, product := range c.Products {
		if product.ID == id {
			return product
		}
	}
	t.Fatal("saved product missing")
	return CreditProduct{}
}

func creditApprovalAction(t *testing.T, id, action string) {
	t.Helper()
	catalog, err := GetCreditCatalog()
	require.NoError(t, err)
	review, err := GetCreditProductReview(id)
	require.NoError(t, err)
	require.NoError(t, ChangeCreditProductState(id, catalog.Version, review.Product.Revision, action, strings.ToUpper(action)+" "+review.Confirmation, 7))
}

func TestCreditApprovalLifecycleAndBatchBinding(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "one", 100)
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	assert.Equal(t, "draft", p.State)
	assert.False(t, p.Published)
	require.Error(t, ChangeCreditProductState(p.ID, c.Version, p.Revision, "publish", "", 7))
	creditApprovalAction(t, p.ID, "submit")
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	require.Error(t, ChangeCreditProductState(p.ID, c.Version, p.Revision, "approve", "", 7))
	creditApprovalAction(t, p.ID, "approve")
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "approved", review.Product.State)
	assert.False(t, review.Product.Published)
	event := review.Events[len(review.Events)-1]
	assert.True(t, event.SoloOperator)
	assert.Equal(t, 7, event.EditedBy)
	assert.Equal(t, 7, event.ActorID)
	require.Error(t, ChangeCreditProductState(p.ID, c.Version, p.Revision, "approve", "APPROVE "+review.Confirmation, 7))
	creditApprovalAction(t, p.ID, "publish")
	effective, err := GetEffectiveCreditProducts()
	require.NoError(t, err)
	require.Len(t, effective.Products, 1)
	assert.Equal(t, "1", effective.Products[0].BaseCredit)
	priorPrice := operation_setting.Price
	operation_setting.Price = 7
	t.Cleanup(func() { operation_setting.Price = priorPrice })
	batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "new stock", FaceValueCents: 100, Count: 2, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision})
	require.NoError(t, err)
	assert.Equal(t, 500000, batch.Quota)
	assert.Equal(t, p.ID, batch.ProductID)
	assert.Positive(t, batch.ApprovalID)
	assert.Len(t, keys, 2)
	var codes []Redemption
	require.NoError(t, DB.Where("batch_id = ?", batch.Id).Find(&codes).Error)
	for _, code := range codes {
		assert.Equal(t, 500000, code.Quota)
	}
	modified := codes[0]
	modified.Quota++
	require.Error(t, modified.Update())
	require.Error(t, modified.Delete())
	_, err = BatchDeleteRedemptions([]int{modified.Id})
	require.Error(t, err)
	creditApprovalAction(t, p.ID, "unpublish")
	creditApprovalAction(t, p.ID, "publish")
	creditApprovalAction(t, p.ID, "revoke")
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	require.Error(t, ChangeCreditProductState(p.ID, c.Version, p.Revision, "publish", review.Confirmation, 7))
	_, _, err = CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "stale stock", FaceValueCents: 100, Count: 1, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision})
	require.Error(t, err)
	var unchanged RedemptionBatch
	require.NoError(t, DB.First(&unchanged, batch.Id).Error)
	assert.Equal(t, 500000, unchanged.Quota)
}

func TestCreditApprovalCASInvalidationAndIndependentProducts(t *testing.T) {
	creditApprovalFixture(t)
	a := creditApprovalProduct(t, "one", 100)
	b := creditApprovalProduct(t, "five", 500)
	creditApprovalAction(t, a.ID, "submit")
	creditApprovalAction(t, a.ID, "approve")
	creditApprovalAction(t, a.ID, "publish")
	creditApprovalAction(t, b.ID, "submit")
	creditApprovalAction(t, b.ID, "approve")
	creditApprovalAction(t, b.ID, "publish")
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	stale := c
	for i := range c.Products {
		if c.Products[i].ID == a.ID {
			c.Products[i].Description = "changed delivery promise"
		}
	}
	require.NoError(t, SaveCreditCatalog(c, 7))
	require.Error(t, SaveCreditCatalog(stale, 7))
	effective, err := GetEffectiveCreditProducts()
	require.NoError(t, err)
	require.Len(t, effective.Products, 1)
	assert.Equal(t, b.ID, effective.Products[0].ID)
	review, err := GetCreditProductReview(a.ID)
	require.NoError(t, err)
	assert.Equal(t, a.Revision+1, review.Product.Revision)
	assert.Zero(t, review.Product.ApprovalID)
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	for i := range c.Products {
		if c.Products[i].ID == b.ID {
			c.Products[i].Sort++
		}
	}
	require.NoError(t, SaveCreditCatalog(c, 7))
	review, err = GetCreditProductReview(b.ID)
	require.NoError(t, err)
	assert.Equal(t, b.Revision, review.Product.Revision)
	assert.Equal(t, "published", review.Product.State)
}

func TestCreditApprovalAuditFailureAndBypass(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "one", 100)
	creditApprovalAction(t, p.ID, "submit")
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("approval-failure", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*CreditApprovalEvent); ok && event.Decision == "approve" {
			tx.AddError(errors.New("isolated audit failure"))
		}
	}))
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	require.Error(t, ChangeCreditProductState(p.ID, c.Version, p.Revision, "approve", "APPROVE "+review.Confirmation, 7))
	after, err := GetCreditCatalog()
	require.NoError(t, err)
	assert.Equal(t, c.Version, after.Version)
	require.NoError(t, DB.Callback().Create().Remove("approval-failure"))
	for i := range c.Products {
		if c.Products[i].ID == p.ID {
			c.Products[i].Published = true
			c.Products[i].DeliveryVerified = true
			c.Products[i].State = "published"
		}
	}
	require.Error(t, SaveCreditCatalog(c, 7))
	require.Error(t, UpdateOption(CreditCatalogOptionKey, "{}"))
	// A direct database publication claim must produce an error, not an empty list.
	raw, err := common.Marshal(c)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&Option{}).Where(commonKeyCol+" = ?", CreditCatalogOptionKey).Update("value", string(raw)).Error)
	_, err = GetEffectiveCreditProducts()
	require.Error(t, err)
}

func TestCreditApprovalLegacyCutoverAndFrozenCompatibility(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "explicit-one", 100)
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	require.Error(t, ActivateUnifiedCreditView(c.Version, 7))
	require.NoError(t, MapLegacyCreditProduct(p.ID, c.Version, p.Revision, 1, 7))
	p = creditApprovalSample(t, p.ID)
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	require.Error(t, ActivateUnifiedCreditView(c.Version, 7))
	creditApprovalAction(t, p.ID, "submit")
	creditApprovalAction(t, p.ID, "approve")
	creditApprovalAction(t, p.ID, "publish")
	// New legacy rows cannot enter the frozen compatibility snapshot.
	require.NoError(t, DB.Create(&StoreProduct{Id: 2, Name: "bypass", FaceValueCents: 500, StoreUrl: LegacyCreditPurchaseURL, Enabled: true}).Error)
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	require.Len(t, c.LegacyProducts, 1)
	assert.Equal(t, 1, c.LegacyProducts[0].Id)
	require.Error(t, ActivateUnifiedCreditView(c.Version, 7))
	require.NoError(t, DB.Where("id = ?", 2).Delete(&StoreProduct{}).Error)
	require.NoError(t, ActivateUnifiedCreditView(c.Version, 7))
	public, err := GetPublicCreditCatalog()
	require.NoError(t, err)
	require.Len(t, public.Products, 1)
	assert.Empty(t, public.Products[0].DeliveryEvidence)
	assert.Zero(t, public.Products[0].EvidenceBatchID)
	creditApprovalAction(t, p.ID, "revoke")
	public, err = GetPublicCreditCatalog()
	require.NoError(t, err)
	assert.Empty(t, public.Products)
}

func TestCreditApprovalURLsAndMissingEvidence(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "one", 100)
	for _, raw := range []string{"https://catfk.com.evil/item/a", "https://user@catfk.com/item/a", "https://catfk.com:443/item/a", "https://catfk.com/item/%61", "https://catfk.com/item/a?", "https://catfk.com/item/a#x", "https://catfk.com/item/../item/a", "http://catfk.com/item/a", "https://catfk.com/item/a\n"} {
		assert.False(t, TrustedCreditPurchaseURL(raw), raw)
	}
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	for i := range c.Products {
		if c.Products[i].ID == p.ID {
			c.Products[i].BaseQuota++
		}
	}
	require.NoError(t, SaveCreditCatalog(c, 7))
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	require.Error(t, ChangeCreditProductState(p.ID, c.Version, review.Product.Revision, "submit", "", 7))
}

func TestCreditApprovalPreparationSampleAndDowngradeProtection(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "sample", 100)
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	for i := range c.Products {
		if c.Products[i].ID == p.ID {
			c.Products[i].Description = "new sample revision"
		}
	}
	require.NoError(t, SaveCreditCatalog(c, 7))
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	p = review.Product
	sample, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "delivery trial", FaceValueCents: 100, Count: 1, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision, EvidenceOnly: true})
	require.NoError(t, err)
	assert.True(t, sample.EvidenceOnly)
	assert.Zero(t, sample.ApprovalID)
	assert.Equal(t, p.ID, sample.ProductID)
	assert.Equal(t, p.BaseQuota, sample.Quota)
	assert.Len(t, keys, 1)
	_, _, err = CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "not inventory", FaceValueCents: 100, Count: 2, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision, EvidenceOnly: true})
	require.Error(t, err)
	_, _, err = CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "unreviewed inventory", FaceValueCents: 100, Count: 1, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision})
	require.Error(t, err)
	// Only metadata is inspected; missing code quota must never pass approval.
	require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", sample.Id).Update("quota", nil).Error)
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	require.Error(t, ChangeCreditProductState(p.ID, c.Version, p.Revision, "submit", "", 7))
	raw, err := common.Marshal(DefaultCreditCatalog())
	require.NoError(t, err)
	require.NoError(t, DB.Model(&Option{}).Where(&Option{Key: CreditCatalogOptionKey}).Update("value", string(raw)).Error)
	require.Error(t, PrepareCreditCatalog(), "old-image writes cannot recapture new legacy products")
}

func TestCreditApprovalEvidenceIsolation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes map[string]any
	}{
		{"other product", map[string]any{"product_id": "another"}},
		{"other revision", map[string]any{"product_revision": 99}},
		{"other delivery", map[string]any{"delivery_binding": "another"}},
		{"unbound new batch", map[string]any{"product_id": ""}},
		{"null product", map[string]any{"product_id": nil}},
		{"null revision", map[string]any{"product_revision": nil}},
		{"null binding", map[string]any{"delivery_binding": nil}},
		{"missing binding", map[string]any{"delivery_binding": ""}},
		{"null sample flag", map[string]any{"evidence_only": nil}},
		{"null approval", map[string]any{"approval_id": nil}},
		{"disabled batch", map[string]any{"status": RedemptionBatchStatusDisabled}},
		{"expired batch", map[string]any{"expired_time": common.GetTimestamp() - 1}},
		{"missing batch", map[string]any{"id": 999999}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creditApprovalFixture(t)
			p := creditApprovalProduct(t, "isolation", 100)
			creditApprovalAction(t, p.ID, "submit")
			creditApprovalAction(t, p.ID, "approve")
			require.NoError(t, validateCreditDelivery(DB, p))
			require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Updates(tc.changes).Error)
			require.Error(t, validateCreditDelivery(DB, p))
			batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "rejected evidence stock", FaceValueCents: p.PriceCents, Count: 1, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision})
			require.Error(t, err)
			assert.Nil(t, batch)
			assert.Nil(t, keys)
			var issued int64
			require.NoError(t, DB.Model(&RedemptionBatch{}).Where("name = ?", "rejected evidence stock").Count(&issued).Error)
			assert.Zero(t, issued)
		})
	}
	t.Run("revoked sales approval", func(t *testing.T) {
		creditApprovalFixture(t)
		p := creditApprovalProduct(t, "inventory", 100)
		creditApprovalAction(t, p.ID, "submit")
		creditApprovalAction(t, p.ID, "approve")
		batch, _, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "approved", FaceValueCents: 100, Count: 1, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision})
		require.NoError(t, err)
		p.EvidenceBatchID = batch.Id
		require.NoError(t, validateCreditDelivery(DB, p))
		creditApprovalAction(t, p.ID, "revoke")
		require.Error(t, validateCreditDelivery(DB, p))
	})
	t.Run("frozen legacy adoption", func(t *testing.T) {
		creditApprovalFixture(t)
		p := creditApprovalProduct(t, "legacy", 100)
		c, err := GetCreditCatalog()
		require.NoError(t, err)
		for i := range c.Products {
			if c.Products[i].ID == p.ID {
				c.Products[i].EvidenceBatchID = 77
			}
		}
		require.NoError(t, SaveCreditCatalog(c, 7))
		review, err := GetCreditProductReview(p.ID)
		require.NoError(t, err)
		p = review.Product
		var before RedemptionBatch
		require.NoError(t, DB.First(&before, 77).Error)
		creditApprovalAction(t, p.ID, "submit")
		creditApprovalAction(t, p.ID, "approve")
		var after RedemptionBatch
		require.NoError(t, DB.First(&after, 77).Error)
		assert.Equal(t, before, after, "legacy financial data must remain unchanged")
		p.Revision++
		require.Error(t, validateCreditDelivery(DB, p))
		p.Revision--
		p.ID = "another"
		require.Error(t, validateCreditDelivery(DB, p))
	})
}

func TestCreditDeliveryEvidenceRecovery(t *testing.T) {
	creditApprovalFixture(t)
	catalog, err := GetCreditCatalog()
	require.NoError(t, err)
	for i := range catalog.Products {
		if catalog.Products[i].ID == "credit-1" {
			catalog.Products[i].BaseQuota = 500000
			catalog.Products[i].DeliveryBinding = "qmcse2"
			catalog.Products[i].DeliveryEvidence = "isolated-delivery-fixture"
			catalog.Products[i].PurchaseURL = LegacyCreditPurchaseURL
		}
	}
	require.NoError(t, SaveCreditCatalog(catalog, 7))
	product := creditApprovalSample(t, "credit-1")
	creditApprovalAction(t, product.ID, "submit")
	creditApprovalAction(t, product.ID, "approve")
	creditApprovalAction(t, product.ID, "publish")
	review, err := GetCreditProductReview(product.ID)
	require.NoError(t, err)
	product = review.Product
	oldApproval := product.ApprovalID
	// The compatibility projection deliberately has no current evidence.
	var row Option
	require.NoError(t, DB.Where(&Option{Key: CreditCatalogOptionKey}).First(&row).Error)
	var stored CreditCatalog
	require.NoError(t, common.UnmarshalJsonStr(row.Value, &stored))
	assert.Zero(t, stored.Products[0].EvidenceBatchID)
	assert.Equal(t, product.EvidenceBatchID, review.Product.EvidenceBatchID)
	assert.Positive(t, review.Product.EvidenceBatchID)

	_, err = DisableRedemptionBatchById(product.EvidenceBatchID)
	require.NoError(t, err)
	var frozenEvidence RedemptionBatch
	require.NoError(t, DB.First(&frozenEvidence, product.EvidenceBatchID).Error)
	input := CreateRedemptionBatchInput{Name: "recovery stock", FaceValueCents: product.PriceCents, Count: 1, AdminUserID: 7, ProductID: product.ID, ProductRevision: product.Revision}
	_, _, err = CreateRedemptionBatch(input)
	require.EqualError(t, err, "evidence batch does not match product promise")

	// Existing draft save creates a new revision and invalidates its approval.
	// This is an isolated rehearsal; no production evidence is written.
	catalog, err = GetCreditCatalog()
	require.NoError(t, err)
	for i := range catalog.Products {
		if catalog.Products[i].ID == product.ID {
			catalog.Products[i].EvidenceBatchID = 0
		}
	}
	require.NoError(t, SaveCreditCatalog(catalog, 7))
	review, err = GetCreditProductReview(product.ID)
	require.NoError(t, err)
	assert.Equal(t, product.Revision+1, review.Product.Revision)
	assert.Equal(t, "draft", review.Product.State)
	assert.Zero(t, review.Product.ApprovalID)
	assert.False(t, review.Product.Published)
	assert.False(t, review.Product.DeliveryVerified)
	require.EqualError(t, validateCreditDelivery(DB, review.Product), "verified batch and delivery references are required")
	input.ProductRevision = review.Product.Revision
	_, _, err = CreateRedemptionBatch(input)
	require.EqualError(t, err, "invalid product approval state")
	replacement := creditApprovalSample(t, product.ID)
	assert.NotEqual(t, product.EvidenceBatchID, replacement.EvidenceBatchID)
	require.NoError(t, validateCreditDelivery(DB, replacement))
	_, _, err = CreateRedemptionBatch(input)
	require.EqualError(t, err, "invalid product approval state", "a new sample cannot bypass approval")
	creditApprovalAction(t, product.ID, "submit")
	creditApprovalAction(t, product.ID, "approve")
	creditApprovalAction(t, product.ID, "publish")
	batch, keys, err := CreateRedemptionBatch(input)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, product.BaseQuota, batch.Quota)
	assert.Equal(t, product.DeliveryBinding, batch.DeliveryBinding)
	assert.NotEqual(t, oldApproval, batch.ApprovalID)
	var retained RedemptionBatch
	require.NoError(t, DB.First(&retained, frozenEvidence.Id).Error)
	assert.Equal(t, frozenEvidence, retained)
	var approval CreditApprovalEvent
	require.NoError(t, DB.First(&approval, oldApproval).Error)
	assert.Equal(t, "approve", approval.Decision)
	assert.Equal(t, product.Revision, approval.Revision)
	assert.Equal(t, product.EvidenceBatchID, approval.EvidenceBatchID)
}

func TestCreditProductMultipleBatchesAndArchive(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "lifecycle", 100)
	creditApprovalAction(t, p.ID, "submit")
	creditApprovalAction(t, p.ID, "approve")
	creditApprovalAction(t, p.ID, "publish")
	before, err := GetCreditCatalog()
	require.NoError(t, err)
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	p = review.Product
	choices, err := GetRedemptionBatchProducts()
	require.NoError(t, err)
	for _, choice := range choices {
		if choice.ID == p.ID {
			assert.True(t, choice.StockReady)
			assert.False(t, choice.SampleReady)
			assert.Equal(t, p.BaseQuota, choice.BaseQuota)
		}
	}
	var issued []RedemptionBatch
	for i, count := range []int{2, 3} {
		batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: fmt.Sprintf("independent-%d", i), FaceValueCents: p.PriceCents, Count: count, ExpiredTime: int64(i) * (common.GetTimestamp() + 86400), ProductID: p.ID, ProductRevision: p.Revision, AdminUserID: 7})
		require.NoError(t, err)
		assert.Len(t, keys, count)
		assert.Equal(t, p.ApprovalID, batch.ApprovalID)
		assert.Equal(t, p.DeliveryBinding, batch.DeliveryBinding)
		assert.Equal(t, p.Revision, batch.ProductRevision)
		assert.Equal(t, p.BaseQuota, batch.Quota)
		issued = append(issued, *batch)
	}
	after, err := GetCreditCatalog()
	require.NoError(t, err)
	assert.Equal(t, before, after, "stock issuance must not create a new revision or approval")
	require.NoError(t, DB.AutoMigrate(&OfflineOrder{}, &OfflineOrderCode{}, &BalanceTransaction{}, &FinanceSyncEvent{}, &FinanceCashReceipt{}))
	var code Redemption
	require.NoError(t, DB.Where("batch_id = ?", issued[0].Id).First(&code).Error)
	require.NoError(t, DB.Model(&code).Updates(map[string]any{"status": common.RedemptionCodeStatusUsed, "used_user_id": 99, "redeemed_time": common.GetTimestamp()}).Error)
	order := OfflineOrder{OrderNo: "retained-product-order", Status: OfflineOrderPaid}
	require.NoError(t, DB.Create(&order).Error)
	require.NoError(t, DB.Create(&OfflineOrderCode{OrderID: order.Id, RedemptionID: code.Id}).Error)
	require.NoError(t, DB.Create(&BalanceTransaction{ReferenceType: BalanceTxReferenceRedemption, ReferenceId: code.Id, AmountQuota: code.Quota}).Error)
	require.NoError(t, DB.Create(&FinanceSyncEvent{EventKey: "lifecycle-finance", Topic: "offline_redemption", RefID: int64(code.Id)}).Error)
	require.NoError(t, DB.Create(&FinanceCashReceipt{OrderID: order.Id, AmountCNY: "1"}).Error)
	review, err = GetCreditProductReview(p.ID)
	require.NoError(t, err)
	assert.False(t, review.Deletion.CanDelete)
	assert.EqualValues(t, 1, review.Deletion.UsedCodes)
	assert.EqualValues(t, 1, review.Deletion.Orders)
	assert.EqualValues(t, 1, review.Deletion.BalanceEntries)
	assert.EqualValues(t, 1, review.Deletion.CashReceipts)
	assert.EqualValues(t, 1, review.Deletion.FinanceEvents)
	creditApprovalAction(t, p.ID, "archive")
	review, err = GetCreditProductReview(p.ID)
	require.NoError(t, err)
	assert.True(t, review.Product.Archived)
	choices, err = GetRedemptionBatchProducts()
	require.NoError(t, err)
	for _, choice := range choices {
		assert.NotEqual(t, p.ID, choice.ID)
	}
	assert.EqualValues(t, 1, review.Deletion.Orders)
	assert.EqualValues(t, 1, review.Deletion.BalanceEntries)
	assert.EqualValues(t, 1, review.Deletion.UsedCodes)
	assert.EqualValues(t, 1, review.Deletion.CashReceipts)
	assert.False(t, review.Product.Published)
	assert.Equal(t, p.Revision, review.Product.Revision)
	assert.Equal(t, p.ApprovalID, review.Product.ApprovalID)
	assert.Equal(t, p.BaseQuota, review.Product.BaseQuota)
	assert.Equal(t, p.PriceCents, review.Product.PriceCents)
	current, err := GetCreditCatalog()
	require.NoError(t, err)
	for i := range current.Products {
		if current.Products[i].ID == p.ID {
			current.Products[i].Archived = false
		}
	}
	require.ErrorContains(t, SaveCreditCatalog(current, 7), "dedicated approval")
	err = ChangeCreditProductState(p.ID, current.Version, p.Revision, "delete", "DELETE "+review.Confirmation, 7)
	require.ErrorContains(t, err, "Archive it instead")
	public, err := GetPublicCreditCatalog()
	require.NoError(t, err)
	for _, item := range public.Products {
		assert.NotEqual(t, p.ID, item.ID)
	}
	_, _, err = CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "archived-stock", FaceValueCents: p.PriceCents, Count: 1, ProductID: p.ID, ProductRevision: p.Revision, AdminUserID: 7})
	require.ErrorContains(t, err, "archived")
	creditApprovalAction(t, p.ID, "restore")
	review, err = GetCreditProductReview(p.ID)
	require.NoError(t, err)
	assert.False(t, review.Product.Archived)
	assert.False(t, review.Product.Published, "restore must not resume sales")
	creditApprovalAction(t, p.ID, "publish")
	creditApprovalAction(t, p.ID, "archive")
	_, err = DisableRedemptionBatchById(p.EvidenceBatchID)
	require.NoError(t, err)
	creditApprovalAction(t, p.ID, "restore")
	current, err = GetCreditCatalog()
	require.NoError(t, err)
	review, err = GetCreditProductReview(p.ID)
	require.NoError(t, err)
	require.Error(t, ChangeCreditProductState(p.ID, current.Version, p.Revision, "publish", "PUBLISH "+review.Confirmation, 7))
	assert.False(t, review.Product.Published)
	require.NoError(t, DB.First(&order, order.Id).Error)
	var retained Redemption
	require.NoError(t, DB.First(&retained, code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, retained.Status)
	assert.Equal(t, 99, retained.UsedUserId)
}

func TestCreditProductPermanentDeleteInitialDraft(t *testing.T) {
	creditApprovalFixture(t)
	review, err := GetCreditProductReview("credit-5")
	require.NoError(t, err)
	require.True(t, review.Deletion.CanDelete)
	catalog, err := GetCreditCatalog()
	require.NoError(t, err)
	require.NoError(t, ChangeCreditProductState("credit-5", catalog.Version, review.Product.Revision, "delete", "DELETE "+review.Confirmation, 7))
	_, err = GetCreditProductReview("credit-5")
	require.Error(t, err)
	var audit CreditApprovalEvent
	require.NoError(t, DB.Where("product_id = ? AND decision = ?", "credit-5", "delete").First(&audit).Error)
	catalog, err = GetCreditCatalog()
	require.NoError(t, err)
	catalog.Products = append(catalog.Products, review.Product)
	require.ErrorContains(t, SaveCreditCatalog(catalog, 7), "permanently deleted")
}

func TestCreditApprovalSampleLifetimeAndConcurrency(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "repeat", 100)
	input := CreateRedemptionBatchInput{Name: "sample", FaceValueCents: 100, Count: 1, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision, EvidenceOnly: true}
	_, _, err := CreateRedemptionBatch(input)
	require.Error(t, err)
	// Consumption or batch withdrawal cannot clear the permanent issuance event.
	require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", p.EvidenceBatchID).Update("status", common.RedemptionCodeStatusUsed).Error)
	_, _, err = CreateRedemptionBatch(input)
	require.Error(t, err)
	_, err = DisableRedemptionBatchById(p.EvidenceBatchID)
	require.NoError(t, err)
	_, _, err = CreateRedemptionBatch(input)
	require.Error(t, err)
	other := creditApprovalProduct(t, "other", 100)
	assert.Equal(t, p.Revision, other.Revision)
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	for i := range c.Products {
		if c.Products[i].ID == p.ID {
			c.Products[i].Description = "next revision"
		}
	}
	require.NoError(t, SaveCreditCatalog(c, 7))
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	input.ProductRevision = review.Product.Revision
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	start := make(chan struct{})
	results := make(chan error, 3)
	var workers sync.WaitGroup
	for range 3 {
		workers.Go(func() {
			<-start
			_, _, err := CreateRedemptionBatch(input)
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	var batches, events, codes int64
	require.NoError(t, DB.Model(&RedemptionBatch{}).Where("product_id = ? AND product_revision = ?", p.ID, input.ProductRevision).Count(&batches).Error)
	require.NoError(t, DB.Model(&CreditApprovalEvent{}).Where("product_id = ? AND revision = ? AND decision = ?", p.ID, input.ProductRevision, "sample").Count(&events).Error)
	require.NoError(t, DB.Model(&Redemption{}).Where("batch_id IN (?)", DB.Model(&RedemptionBatch{}).Select("id").Where("product_id = ? AND product_revision = ?", p.ID, input.ProductRevision)).Count(&codes).Error)
	assert.EqualValues(t, 1, batches)
	assert.EqualValues(t, 1, events)
	assert.EqualValues(t, 1, codes)
}

func TestCreditApprovalSampleRollback(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "rollback", 100)
	c, err := GetCreditCatalog()
	require.NoError(t, err)
	for i := range c.Products {
		if c.Products[i].ID == p.ID {
			c.Products[i].Description = "next revision"
		}
	}
	require.NoError(t, SaveCreditCatalog(c, 7))
	c, err = GetCreditCatalog()
	require.NoError(t, err)
	review, err := GetCreditProductReview(p.ID)
	require.NoError(t, err)
	input := CreateRedemptionBatchInput{Name: "sample", FaceValueCents: 100, Count: 1, AdminUserID: 7, ProductID: p.ID, ProductRevision: review.Product.Revision, EvidenceOnly: true}
	const privateFixtureKey = "duplicate-key-fixture-only"
	require.NoError(t, DB.Create(&Redemption{Key: privateFixtureKey, Name: "failure fixture"}).Error)
	var captured bytes.Buffer
	priorLogger := DB.Logger
	DB.Logger = gormlogger.New(log.New(&captured, "", 0), gormlogger.Config{LogLevel: gormlogger.Info})
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("sample-code-failure", func(tx *gorm.DB) {
		if code, ok := tx.Statement.Dest.(*Redemption); ok {
			code.Key = privateFixtureKey
		}
	}))
	batch, keys, err := CreateRedemptionBatch(input)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), privateFixtureKey)
	assert.NotContains(t, captured.String(), privateFixtureKey)
	DB.Logger = priorLogger
	assert.Nil(t, batch)
	assert.Nil(t, keys)
	require.NoError(t, DB.Callback().Create().Remove("sample-code-failure"))
	after, err := GetCreditCatalog()
	require.NoError(t, err)
	assert.Equal(t, c, after)
	var count int64
	require.NoError(t, DB.Model(&RedemptionBatch{}).Where("product_id = ? AND product_revision = ?", p.ID, input.ProductRevision).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&CreditApprovalEvent{}).Where("product_id = ? AND revision = ? AND decision = ?", p.ID, input.ProductRevision, "sample").Count(&count).Error)
	assert.Zero(t, count)
	_, _, err = CreateRedemptionBatch(input)
	require.NoError(t, err)
}

func TestCommercialBatchCreationValidationAndRollback(t *testing.T) {
	creditApprovalFixture(t)
	p := creditApprovalProduct(t, "commercial", 100)
	input := CreateRedemptionBatchInput{Name: "normal-stock", FaceValueCents: 100, Count: 100, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision}
	for _, tc := range []struct {
		name   string
		change func(*CreateRedemptionBatchInput)
		want   string
	}{
		{"unknown product", func(i *CreateRedemptionBatchInput) { i.ProductID = "1" }, "approved product not found"},
		{"draft product", func(i *CreateRedemptionBatchInput) {}, "invalid product approval state"},
		{"revision", func(i *CreateRedemptionBatchInput) { i.ProductRevision++ }, "product revision or price mismatch"},
		{"price", func(i *CreateRedemptionBatchInput) { i.FaceValueCents++ }, "product revision or price mismatch"},
		{"zero count", func(i *CreateRedemptionBatchInput) { i.Count = 0 }, "生成数量须为 1-100"},
		{"excess count", func(i *CreateRedemptionBatchInput) { i.Count = 101 }, "生成数量须为 1-100"},
		{"empty name", func(i *CreateRedemptionBatchInput) { i.Name = " " }, "批次名称长度须为 1-64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := input
			tc.change(&candidate)
			batch, keys, err := CreateRedemptionBatch(candidate)
			require.EqualError(t, err, tc.want)
			assert.Nil(t, batch)
			assert.Nil(t, keys)
		})
	}
	creditApprovalAction(t, p.ID, "submit")
	creditApprovalAction(t, p.ID, "approve")
	var before int64
	require.NoError(t, DB.Model(&Redemption{}).Count(&before).Error)
	insertions := 0
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("commercial-code-failure", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*Redemption); ok {
			insertions++
			if insertions == 2 {
				tx.AddError(errors.New("isolated insertion failure"))
			}
		}
	}))
	_, _, err := CreateRedemptionBatch(input)
	require.EqualError(t, err, "redemption code insertion failed")
	require.NoError(t, DB.Callback().Create().Remove("commercial-code-failure"))
	var after int64
	require.NoError(t, DB.Model(&Redemption{}).Count(&after).Error)
	assert.Equal(t, before, after)
	require.NoError(t, DB.Model(&RedemptionBatch{}).Where("name = ?", input.Name).Count(&after).Error)
	assert.Zero(t, after)
	batch, keys, err := CreateRedemptionBatch(input)
	require.NoError(t, err)
	assert.Zero(t, batch.ExpiredTime)
	assert.Len(t, keys, 100)
	_, _, err = CreateRedemptionBatch(input)
	require.EqualError(t, err, "batch name already exists for this product")
	require.NoError(t, DB.Model(&Redemption{}).Count(&after).Error)
	assert.Equal(t, before+100, after)
	input.Name, input.Count = "concurrent-stock", 1
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			_, _, err := CreateRedemptionBatch(input)
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	require.NoError(t, DB.Model(&RedemptionBatch{}).Where("name = ?", input.Name).Count(&after).Error)
	assert.EqualValues(t, 1, after)
	_, err = DisableRedemptionBatchById(p.EvidenceBatchID)
	require.NoError(t, err)
	input.Name = "after-disabled-evidence"
	_, _, err = CreateRedemptionBatch(input)
	require.EqualError(t, err, "evidence batch does not match product promise")
}

func TestCommercialBatchArchivePreservesHistory(t *testing.T) {
	creditApprovalFixture(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &VIPLevel{}, &Log{}, &OfflineOrder{}, &OfflineOrderCode{}, &BalanceTransaction{}, &FinanceSyncEvent{}, &ReferralReward{}))
	p := creditApprovalProduct(t, "archive", 100)
	creditApprovalAction(t, p.ID, "submit")
	creditApprovalAction(t, p.ID, "approve")
	_, err := ArchiveRedemptionBatch(p.EvidenceBatchID)
	require.ErrorIs(t, err, ErrRedemptionBatchEvidenceInUse)
	batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "archive-stock", FaceValueCents: 100, Count: 3, AdminUserID: 7, ProductID: p.ID, ProductRevision: p.Revision})
	require.NoError(t, err)
	user := User{Username: "commercial-redeemer", AffCode: "commercial-redeemer", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	_, err = RedeemStoreCode(keys[0], user.Id)
	require.NoError(t, err)
	var used Redemption
	require.NoError(t, DB.Where(commonKeyCol+" = ?", keys[0]).First(&used).Error)
	order := OfflineOrder{OrderNo: "historical-order", Status: OfflineOrderPaid}
	require.NoError(t, DB.Create(&order).Error)
	require.NoError(t, DB.Create(&OfflineOrderCode{OrderID: order.Id, RedemptionID: used.Id}).Error)
	require.NoError(t, DB.Create(&FinanceSyncEvent{EventKey: "archive-fixture", Topic: "offline_redemption", RefID: int64(used.Id)}).Error)
	require.NoError(t, DB.Create(&ReferralReward{RedemptionID: used.Id, Status: "paid"}).Error)
	preview, err := GetRedemptionBatchDeletion(batch.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 3, preview.Total)
	assert.EqualValues(t, 1, preview.Used)
	assert.EqualValues(t, 2, preview.Unused)
	assert.EqualValues(t, 1, preview.Orders)
	assert.EqualValues(t, 1, preview.BalanceEntries)
	assert.EqualValues(t, 1, preview.FinanceEvents)
	assert.EqualValues(t, 1, preview.Rewards)
	require.False(t, preview.Blocked)
	_, err = ArchiveRedemptionBatch(batch.Id)
	require.NoError(t, err)
	_, err = ArchiveRedemptionBatch(batch.Id)
	require.NoError(t, err)
	_, err = DisableRedemptionBatchById(batch.Id)
	require.NoError(t, err)
	var stored RedemptionBatch
	require.NoError(t, DB.First(&stored, batch.Id).Error)
	assert.Equal(t, RedemptionBatchStatusArchived, stored.Status)
	_, err = GetUnusedRedemptionBatchKeys(batch.Id)
	require.Error(t, err)
	_, err = RedeemStoreCode(keys[1], user.Id)
	require.Error(t, err)
	var unchanged Redemption
	require.NoError(t, DB.First(&unchanged, used.Id).Error)
	assert.Equal(t, used, unchanged)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.Equal(t, batch.Quota, user.Quota)
	items, _, err := GetRedemptionBatches(0, 100)
	require.NoError(t, err)
	for _, item := range items {
		assert.NotEqual(t, batch.Id, item.Id)
	}
	// Archive protects every code against individual edit/delete and cleanup.
	var unused Redemption
	require.NoError(t, DB.Where(commonKeyCol+" = ?", keys[1]).First(&unused).Error)
	unused.Status = common.RedemptionCodeStatusEnabled
	require.Error(t, unused.Update())
	require.Error(t, unused.Delete())
	_, err = DeleteInvalidRedemptions()
	require.NoError(t, err)
	after, err := GetRedemptionBatchDeletion(batch.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 3, after.Total)
	assert.EqualValues(t, 1, after.Used)
	assert.Equal(t, preview.Orders, after.Orders)
	assert.Equal(t, preview.BalanceEntries, after.BalanceEntries)
	assert.Equal(t, preview.FinanceEvents, after.FinanceEvents)
	assert.Equal(t, preview.Rewards, after.Rewards)
}

func TestCommercialBatchArchiveRollback(t *testing.T) {
	creditApprovalFixture(t)
	require.NoError(t, DB.AutoMigrate(&OfflineOrder{}, &OfflineOrderCode{}, &BalanceTransaction{}, &FinanceSyncEvent{}, &ReferralReward{}))
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("archive-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "redemption_batches" {
			tx.AddError(errors.New("isolated archive failure"))
		}
	}))
	_, err := ArchiveRedemptionBatch(77)
	require.Error(t, err)
	require.NoError(t, DB.Callback().Update().Remove("archive-failure"))
	var code Redemption
	require.NoError(t, DB.Where("batch_id = ?", 77).First(&code).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, code.Status)
	var batch RedemptionBatch
	require.NoError(t, DB.First(&batch, 77).Error)
	assert.Equal(t, RedemptionBatchStatusEnabled, batch.Status)
}

func TestCreditAutomaticActivationAndRetrySafeStock(t *testing.T) {
	creditApprovalFixture(t)
	catalog, err := GetCreditCatalog()
	require.NoError(t, err)
	catalog.Products = append(catalog.Products, CreditProduct{ID: "automatic", Name: "Automatic credit", PriceCents: 100, BaseQuota: 500000, PurchaseURL: LegacyCreditPurchaseURL})
	require.NoError(t, SaveCreditCatalog(catalog, 7))
	creditApprovalAction(t, "automatic", "activate")
	review, err := GetCreditProductReview("automatic")
	require.NoError(t, err)
	assert.Equal(t, "published", review.Product.State)
	assert.Equal(t, "qmcse2", review.Product.DeliveryBinding)
	assert.True(t, review.Product.Published)
	require.Positive(t, review.Product.EvidenceBatchID)
	require.NoError(t, validateCreditApproval(DB, review.Product))
	var approval CreditApprovalEvent
	require.NoError(t, DB.First(&approval, review.Product.ApprovalID).Error)
	assert.Equal(t, "auto_validate", approval.Decision)
	assert.Equal(t, CreditAutomaticValidationPolicy, approval.Policy)
	assert.False(t, approval.SoloOperator)
	eventCount := len(review.Events)
	creditApprovalAction(t, "automatic", "activate")
	again, err := GetCreditProductReview("automatic")
	require.NoError(t, err)
	assert.Len(t, again.Events, eventCount)
	input := CreateRedemptionBatchInput{Name: "stable-download-request", FaceValueCents: 100, Count: 2, AdminUserID: 7, ProductID: "automatic", ProductRevision: review.Product.Revision, RetrySafe: true}
	batch, keys, err := CreateRedemptionBatch(input)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	retried, repeatedKeys, err := CreateRedemptionBatch(input)
	require.NoError(t, err)
	assert.Equal(t, batch.Id, retried.Id)
	assert.Equal(t, keys, repeatedKeys)
	input.Count = 3
	_, _, err = CreateRedemptionBatch(input)
	require.ErrorContains(t, err, "already exists")
	input.Count = 2
	input.AdminUserID = 8
	_, _, err = CreateRedemptionBatch(input)
	require.ErrorContains(t, err, "already exists")
	input.AdminUserID = 7
	var firstCode Redemption
	require.NoError(t, DB.Where("batch_id = ?", batch.Id).Order("id").First(&firstCode).Error)
	require.NoError(t, DB.Model(&firstCode).Update("status", common.RedemptionCodeStatusUsed).Error)
	_, remaining, err := CreateRedemptionBatch(input)
	require.NoError(t, err)
	assert.Equal(t, keys[1:], remaining)
	catalog, err = GetCreditCatalog()
	require.NoError(t, err)
	for i := range catalog.Products {
		if catalog.Products[i].ID == "automatic" {
			catalog.Products[i].BaseQuota = 1000000
		}
	}
	require.NoError(t, SaveCreditCatalog(catalog, 7))
	draft, err := GetCreditProductReview("automatic")
	require.NoError(t, err)
	assert.Zero(t, draft.Product.EvidenceBatchID)
	assert.Equal(t, review.Product.Revision+1, draft.Product.Revision)
	creditApprovalAction(t, "automatic", "activate")
	updated, err := GetCreditProductReview("automatic")
	require.NoError(t, err)
	assert.NotEqual(t, review.Product.EvidenceBatchID, updated.Product.EvidenceBatchID)
	require.NoError(t, validateCreditApproval(DB, updated.Product))
	var original RedemptionBatch
	require.NoError(t, DB.First(&original, batch.Id).Error)
	assert.Equal(t, 500000, original.Quota)
	assert.Equal(t, review.Product.Revision, original.ProductRevision)
}

func TestCreditAutomaticActivationRejectsInvalidEvidence(t *testing.T) {
	for _, issue := range []string{"missing", "disabled", "expired", "product", "revision", "binding", "quota", "withdrawn", "archived"} {
		t.Run(issue, func(t *testing.T) {
			creditApprovalFixture(t)
			p := creditApprovalProduct(t, "invalid-auto", 100)
			switch issue {
			case "missing":
				require.NoError(t, DB.Delete(&RedemptionBatch{}, p.EvidenceBatchID).Error)
			case "disabled":
				require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Update("status", RedemptionBatchStatusDisabled).Error)
			case "expired":
				require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Update("expired_time", common.GetTimestamp()-1).Error)
			case "product":
				require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Update("product_id", "other").Error)
			case "revision":
				require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Update("product_revision", p.Revision+1).Error)
			case "binding":
				require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Update("delivery_binding", "other").Error)
			case "quota":
				require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", p.EvidenceBatchID).Update("quota", 1).Error)
			case "withdrawn":
				_, err := appendCreditEvent(DB, p, 7, "revoke")
				require.NoError(t, err)
			case "archived":
				creditApprovalAction(t, p.ID, "archive")
			}
			before, err := GetCreditProductReview(p.ID)
			require.NoError(t, err)
			catalog, err := GetCreditCatalog()
			require.NoError(t, err)
			var batchCount int64
			require.NoError(t, DB.Model(&RedemptionBatch{}).Count(&batchCount).Error)
			err = ChangeCreditProductState(p.ID, catalog.Version, p.Revision, "activate", "ACTIVATE "+before.Confirmation, 7)
			require.Error(t, err)
			after, err := GetCreditProductReview(p.ID)
			require.NoError(t, err)
			assert.Equal(t, before.Product, after.Product)
			assert.Equal(t, before.Events, after.Events)
			var remaining int64
			require.NoError(t, DB.Model(&RedemptionBatch{}).Count(&remaining).Error)
			assert.Equal(t, batchCount, remaining)
		})
	}
}

func TestCreditAutomaticActivationRollsBackSampleAndAudit(t *testing.T) {
	creditApprovalFixture(t)
	catalog, err := GetCreditCatalog()
	require.NoError(t, err)
	catalog.Products = append(catalog.Products, CreditProduct{ID: "atomic-auto", Name: "Atomic", PriceCents: 100, BaseQuota: 500000, PurchaseURL: LegacyCreditPurchaseURL})
	require.NoError(t, SaveCreditCatalog(catalog, 7))
	before, err := GetCreditProductReview("atomic-auto")
	require.NoError(t, err)
	catalog, err = GetCreditCatalog()
	require.NoError(t, err)
	var batches int64
	require.NoError(t, DB.Model(&RedemptionBatch{}).Count(&batches).Error)
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("fail_auto_audit", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*CreditApprovalEvent); ok && event.Decision == "auto_validate" {
			tx.AddError(errors.New("fixture audit failure"))
		}
	}))
	err = ChangeCreditProductState("atomic-auto", catalog.Version, before.Product.Revision, "activate", "ACTIVATE "+before.Confirmation, 7)
	require.ErrorContains(t, err, "fixture audit failure")
	require.NoError(t, DB.Callback().Create().Remove("fail_auto_audit"))
	after, err := GetCreditProductReview("atomic-auto")
	require.NoError(t, err)
	assert.Equal(t, before.Product, after.Product)
	assert.Equal(t, before.Events, after.Events)
	var remaining int64
	require.NoError(t, DB.Model(&RedemptionBatch{}).Count(&remaining).Error)
	assert.Equal(t, batches, remaining)
	creditApprovalAction(t, "atomic-auto", "activate")
}
