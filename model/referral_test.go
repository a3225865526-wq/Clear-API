package model

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func referralFixture(t *testing.T) (int, int) {
	t.Helper()
	oldDB, oldLog := DB, LOG_DB
	var dialector gorm.Dialector = sqlite.Open(t.TempDir() + "/referral.db")
	dialect := common.DatabaseTypeSQLite
	switch os.Getenv("REFERRAL_TEST_DIALECT") {
	case "mysql":
		require.Contains(t, os.Getenv("REFERRAL_TEST_DSN"), "@tcp(clearapi-referral-test-mysql:3306)/referral_rewards_test?")
		dialector = mysql.Open(os.Getenv("REFERRAL_TEST_DSN"))
		dialect = common.DatabaseTypeMySQL
	case "postgres":
		require.True(t, strings.Contains(os.Getenv("REFERRAL_TEST_DSN"), "host=clearapi-referral-test-postgres ") && strings.Contains(os.Getenv("REFERRAL_TEST_DSN"), "dbname=referral_rewards_test "))
		dialector = postgres.Open(os.Getenv("REFERRAL_TEST_DSN"))
		dialect = common.DatabaseTypePostgreSQL
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	var engineVersion string
	versionQuery := "SELECT version()"
	if dialect == common.DatabaseTypeSQLite {
		versionQuery = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(versionQuery).Scan(&engineVersion).Error)
	t.Logf("database=%s version=%s", db.Dialector.Name(), engineVersion)
	common.SetDatabaseTypes(dialect, dialect)
	initCol()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if dialect == common.DatabaseTypeSQLite {
		sqlDB.SetMaxOpenConns(1)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		DB, LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		initCol()
	})
	// Dedicated, disposable database only, never a caller's production DSN.
	for _, table := range []any{&BalanceTransaction{}, &VIPLevel{}, &CreditApprovalEvent{}, &RedemptionBatch{}, &StoreProduct{}, &ReferralLedger{}, &ReferralReward{}, &ReferralAudit{}, &Option{}, &Redemption{}, &User{}, &Log{}} {
		require.NoError(t, db.Migrator().DropTable(table))
	}
	for range 2 {
		require.NoError(t, db.AutoMigrate(&User{}, &Redemption{}, &Option{}, &ReferralReward{}, &ReferralLedger{}, &ReferralAudit{}, &Log{}, &BalanceTransaction{}, &VIPLevel{}, &RedemptionBatch{}, &CreditApprovalEvent{}, &StoreProduct{}))
	}
	require.NoError(t, InitReferralConfig())
	require.NoError(t, InitCreditCatalog())
	a := User{Username: "inviter", AffCode: "stable-old-code", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&a).Error)
	b := User{Username: "invitee", AffCode: "invitee-code", Status: common.UserStatusEnabled, InviterId: a.Id}
	require.NoError(t, db.Create(&b).Error)
	return a.Id, b.Id
}

func qualifiedCode(t *testing.T, name string, quota int, kind string) Redemption {
	t.Helper()
	r := Redemption{Name: name, Key: name, Quota: quota, Source: RedemptionSourceLegacy, Type: RedemptionTypeBalance, Status: common.RedemptionCodeStatusEnabled, ReferralKind: kind}
	require.NoError(t, r.Insert())
	return r
}

func TestReferralRulesAndQualification(t *testing.T) {
	a, b := referralFixture(t)
	cfg, err := GetReferralConfig()
	require.NoError(t, err)
	assert.False(t, cfg.RewardsEnabled)
	assert.EqualValues(t, 1000, cfg.BasisPoints)
	for _, tc := range []struct {
		name     string
		quota    int64
		mode     string
		bp       int64
		fixed    int64
		expected int64
	}{
		{"ten dollars", 5000000, "percentage", 1000, 0, 500000},
		{"hundred dollars", 50000000, "percentage", 1000, 0, 5000000},
		{"thousand dollars", 500000000, "percentage", 1000, 0, 50000000},
		{"fraction floors", 12345, "percentage", 825, 0, 1018},
		{"fixed ignores percentage", 50000000, "fixed", 1500, 500000, 500000},
		{"large safe multiplication", common.MaxWalletQuota, "percentage", 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			c.Mode, c.BasisPoints, c.FixedQuota = tc.mode, tc.bp, tc.fixed
			q, err := CalculateReferralReward(tc.quota, c)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, q)
		})
	}
	for _, kind := range []string{"", "free", "test", "compensation", "paid"} {
		code := qualifiedCode(t, "off-"+kind, 5000000, kind)
		q, err := Redeem(code.Key, b)
		require.NoError(t, err)
		assert.Equal(t, 5000000, q)
	}
	cfg.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(cfg, a))
	cfg, err = GetReferralConfig()
	require.NoError(t, err)
	for _, kind := range []string{"", "free", "test", "compensation"} {
		code := qualifiedCode(t, "excluded-"+kind, 5000000, kind)
		_, err := Redeem(code.Key, b)
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)
	code := qualifiedCode(t, "paid-ten", 5000000, "paid")
	q, err := Redeem(code.Key, b)
	require.NoError(t, err)
	assert.Equal(t, 5000000, q)
	var reward ReferralReward
	require.NoError(t, DB.Where("redemption_id = ?", code.Id).First(&reward).Error)
	assert.EqualValues(t, 500000, reward.RewardQuota)
	var buyer User
	require.NoError(t, DB.First(&buyer, b).Error)
	before := buyer.Quota
	require.NoError(t, PayReferralReward(reward.ID))
	require.NoError(t, PayReferralReward(reward.ID))
	_, err = Redeem(code.Key, b)
	require.Error(t, err)
	var inviter User
	require.NoError(t, DB.First(&inviter, a).Error)
	assert.Equal(t, 500000, inviter.Quota)
	assert.EqualValues(t, 500000, inviter.ReferralPaidQuota)
	require.NoError(t, DB.First(&buyer, b).Error)
	assert.Equal(t, before, buyer.Quota)
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, DB.Model(&ReferralLedger{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	// Changes apply to new events; the already paid entry remains unchanged.
	cfg.BasisPoints = 1500
	require.NoError(t, SaveReferralConfig(cfg, a))
	code = qualifiedCode(t, "paid-hundred", 50000000, "paid")
	_, err = Redeem(code.Key, b)
	require.NoError(t, err)
	require.NoError(t, DB.Where("redemption_id = ?", code.Id).First(&ReferralReward{}).Error)
	var next ReferralReward
	require.NoError(t, DB.Where("redemption_id = ?", code.Id).First(&next).Error)
	assert.EqualValues(t, 7500000, next.RewardQuota)
	cfg, err = GetReferralConfig()
	require.NoError(t, err)
	cfg.Mode = "fixed"
	cfg.FixedQuota = 500000
	require.NoError(t, SaveReferralConfig(cfg, a))
	code = qualifiedCode(t, "paid-fixed", 50000000, "paid")
	_, err = Redeem(code.Key, b)
	require.NoError(t, err)
	var fixed ReferralReward
	require.NoError(t, DB.Where("redemption_id = ?", code.Id).First(&fixed).Error)
	assert.EqualValues(t, 500000, fixed.RewardQuota)
}

// Representative pre-feature schema: real saved balances, inviter relations,
// stable unique codes and already redeemed card facts survive the upgrade.
type legacyReferralUser struct {
	Id        int
	Password  string `gorm:"not null"`
	Username  string `gorm:"type:varchar(64);uniqueIndex"`
	AffCode   string `gorm:"type:varchar(32);uniqueIndex"`
	InviterId int    `gorm:"type:int;index"`
	Quota     int    `gorm:"type:int;default:0"`
}

func (legacyReferralUser) TableName() string { return "users" }

type legacyReferralRedemption struct {
	Id           int
	Key          string `gorm:"type:char(32);uniqueIndex"`
	Quota        int    `gorm:"default:100"`
	Status       int    `gorm:"default:1"`
	Source       string `gorm:"type:varchar(16);default:'LEGACY';index"`
	Type         string `gorm:"type:varchar(16);default:'BALANCE';index"`
	UsedUserId   int
	RedeemedTime int64 `gorm:"bigint"`
}

func (legacyReferralRedemption) TableName() string { return "redemptions" }

func TestReferralMigrationPreservesHistoryAndUniqueConstraints(t *testing.T) {
	referralFixture(t)
	require.NoError(t, DB.Migrator().DropTable(&User{}, &Redemption{}))
	require.NoError(t, DB.AutoMigrate(&legacyReferralUser{}, &legacyReferralRedemption{}))
	require.NoError(t, DB.Create(&legacyReferralUser{Username: "legacy-owner", AffCode: "legacy-stable", Quota: 12345}).Error)
	require.NoError(t, DB.Create(&legacyReferralUser{Username: "legacy-invitee", AffCode: "legacy-invitee", InviterId: 1, Quota: 5000000}).Error)
	require.NoError(t, DB.Create(&legacyReferralRedemption{Key: "legacy-used-key", Quota: 5000000, Status: common.RedemptionCodeStatusUsed, Source: RedemptionSourceLegacy, Type: RedemptionTypeBalance, UsedUserId: 2, RedeemedTime: 100}).Error)
	for range 2 {
		require.NoError(t, DB.AutoMigrate(&User{}, &Redemption{}, &ReferralReward{}, &ReferralLedger{}, &ReferralAudit{}, &Option{}))
		require.NoError(t, InitReferralConfig())
		require.NoError(t, InitCreditCatalog())
	}
	var a, b User
	require.NoError(t, DB.First(&a, 1).Error)
	require.NoError(t, DB.First(&b, 2).Error)
	assert.Equal(t, "legacy-stable", a.AffCode)
	assert.Equal(t, 12345, a.Quota)
	assert.Equal(t, 1, b.InviterId)
	assert.Equal(t, 5000000, b.Quota)
	assert.Zero(t, a.ReferralPaidQuota)
	var code Redemption
	require.NoError(t, DB.First(&code, 1).Error)
	assert.Empty(t, code.ReferralKind)
	assert.EqualValues(t, 100, code.RedeemedTime)
	cfg, err := GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(cfg, 1))
	_, err = Redeem(code.Key, 2)
	require.Error(t, err)
	var count int64
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)
	require.Error(t, DB.Create(&User{Username: "duplicate", AffCode: "legacy-stable"}).Error)
	reward := ReferralReward{RedemptionID: 7, InviterID: 1, InviteeID: 2, Status: "pending"}
	require.NoError(t, DB.Create(&reward).Error)
	reward.ID = 0
	require.Error(t, DB.Create(&reward).Error)
}

func TestReferralPauseRecoveryAndAtomicFailure(t *testing.T) {
	a, b := referralFixture(t)
	cfg, err := GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(cfg, a))
	code := qualifiedCode(t, "recover", 5000000, "paid")
	_, err = Redeem(code.Key, b)
	require.NoError(t, err)
	var r ReferralReward
	require.NoError(t, DB.First(&r).Error)
	// A ledger write failure rolls back quota, lifetime counter, and paying state.
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("referral-ledger-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "referral_ledgers" {
			tx.AddError(fmt.Errorf("injected ledger failure"))
		}
	}))
	require.Error(t, PayReferralReward(r.ID))
	require.NoError(t, DB.Callback().Create().Remove("referral-ledger-failure"))
	var inviter User
	require.NoError(t, DB.First(&inviter, a).Error)
	assert.Zero(t, inviter.Quota)
	assert.Zero(t, inviter.ReferralPaidQuota)
	require.NoError(t, DB.First(&r, r.ID).Error)
	assert.Equal(t, "pending", r.Status)
	cfg, err = GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = false
	require.NoError(t, SaveReferralConfig(cfg, a))
	require.NoError(t, PayReferralReward(r.ID))
	require.NoError(t, DB.First(&r, r.ID).Error)
	assert.Equal(t, "paused", r.Status)
	cfg, err = GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(cfg, a))
	require.NoError(t, PayReferralReward(r.ID))
	require.NoError(t, DB.First(&r, r.ID).Error)
	assert.Equal(t, "paused", r.Status)
	require.NoError(t, ResumeReferralReward(r.ID, a, "operator reconciled paused reward"))
	// Reopening the database exercises persistent recovery, not memory state.
	if os.Getenv("REFERRAL_TEST_DIALECT") == "" {
		sqlDB, err := DB.DB()
		require.NoError(t, err)
		var dbPath string
		require.NoError(t, DB.Raw("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&dbPath).Error)
		require.NoError(t, sqlDB.Close())
		DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		require.NoError(t, err)
		LOG_DB = DB
		sqlDB, err = DB.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	require.NoError(t, PayReferralReward(r.ID))
	require.NoError(t, PayReferralReward(r.ID))
	require.NoError(t, DB.First(&inviter, a).Error)
	assert.Equal(t, 500000, inviter.Quota)
	var count int64
	require.NoError(t, DB.Model(&ReferralLedger{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestReferralConcurrentPayoutCapsAndDuplicateRedemption(t *testing.T) {
	a, b := referralFixture(t)
	cfg, err := GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = true
	cfg.LifetimeCap = 500000
	require.NoError(t, SaveReferralConfig(cfg, a))
	first := qualifiedCode(t, "concurrent-first", 5000000, "paid")
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Go(func() { _, err := Redeem(first.Key, b); results <- err })
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	assert.Equal(t, 1, success)
	second := qualifiedCode(t, "concurrent-second", 5000000, "paid")
	_, err = Redeem(second.Key, b)
	require.NoError(t, err)
	var rewards []ReferralReward
	require.NoError(t, DB.Find(&rewards).Error)
	require.Len(t, rewards, 2)
	for _, r := range rewards {
		wg.Go(func() { _ = PayReferralReward(r.ID) })
		wg.Go(func() { _ = PayReferralReward(r.ID) })
	}
	wg.Wait()
	var inviter, buyer User
	require.NoError(t, DB.First(&inviter, a).Error)
	require.NoError(t, DB.First(&buyer, b).Error)
	assert.Equal(t, 500000, inviter.Quota)
	assert.EqualValues(t, 500000, inviter.ReferralPaidQuota)
	assert.Equal(t, 10000000, buyer.Quota)
	var count int64
	require.NoError(t, DB.Model(&ReferralLedger{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestReferralCatalogValidationAndImmutableRelations(t *testing.T) {
	a, b := referralFixture(t)
	// Exercise the shared storefront startup schema, including batch evidence.
	for range 2 {
		require.NoError(t, migrateCreditCatalogSchema())
	}
	code, err := EnsureReferralCode(a)
	require.NoError(t, err)
	assert.Equal(t, "stable-old-code", code)
	var user User
	require.NoError(t, DB.First(&user, b).Error)
	user.InviterId = b
	user.AffCode = "changed"
	user.ReferralPaidQuota = 123
	require.NoError(t, user.Update(false))
	require.NoError(t, DB.First(&user, b).Error)
	assert.Equal(t, a, user.InviterId)
	assert.Equal(t, "invitee-code", user.AffCode)
	assert.Zero(t, user.ReferralPaidQuota)
	newUser := User{Id: a}
	require.Error(t, validateRegistrationInviter(DB, &newUser, a))
	catalog, err := GetCreditCatalog()
	require.NoError(t, err)
	require.Len(t, catalog.Products, 8)
	for _, p := range catalog.Products {
		assert.False(t, p.Published)
		assert.Equal(t, p.PriceCents, p.CreditCents)
	}
	catalog.Products[0].Published = true
	require.Error(t, SaveCreditCatalog(catalog, a))
	catalog.Products[0].PurchaseURL = LegacyCreditPurchaseURL
	catalog.Products[0].DeliveryVerified = true
	require.Error(t, SaveCreditCatalog(catalog, a), "legacy flags cannot authorize publication")
	catalog.Products[0].Published, catalog.Products[0].DeliveryVerified = false, false
	require.NoError(t, SaveCreditCatalog(catalog, a))
	require.Error(t, SaveCreditCatalog(catalog, a))
	for _, url := range []string{"http://catfk.com/item/qmcse2", "https://user@catfk.com/item/qmcse2", "https://catfk.com.evil.com/item/qmcse2", "https://catfk.com/item/qmcse2?redirect=x", "https://catfk.com:8443/item/qmcse2", "javascript:alert(1)"} {
		assert.False(t, TrustedCreditPurchaseURL(url))
	}
	assert.True(t, TrustedCreditPurchaseURL(LegacyCreditPurchaseURL))
	c := qualifiedCode(t, "qualification", 5000000, "")
	require.NoError(t, SetRedemptionReferralKind([]int{c.Id}, "paid", "verified paid external sale", a))
	_, err = Redeem(c.Key, b)
	require.NoError(t, err)
	require.Error(t, SetRedemptionReferralKind([]int{c.Id}, "paid", "cannot rewrite used code", a))
}

func referralStoreCode(t *testing.T, name, kind string, evidence bool) Redemption {
	t.Helper()
	batch := RedemptionBatch{Name: name, Source: RedemptionSourceStore, Type: RedemptionTypeBalance, FaceValueCents: 1000, Quota: 5000000, Count: 1, Status: RedemptionBatchStatusEnabled, EvidenceOnly: evidence}
	require.NoError(t, DB.Create(&batch).Error)
	code := Redemption{Name: name, Key: name, Source: RedemptionSourceStore, Type: RedemptionTypeBalance, Status: common.RedemptionCodeStatusEnabled, FaceValueCents: batch.FaceValueCents, Quota: batch.Quota, BatchId: batch.Id, ReferralKind: kind}
	require.NoError(t, DB.Create(&code).Error)
	return code
}

func TestReferralStoreIssuanceAndQualification(t *testing.T) {
	a, _ := referralFixture(t)
	require.NoError(t, PrepareCreditCatalog())
	product := creditApprovalProduct(t, "referral-store", 100)
	var sample Redemption
	require.NoError(t, DB.Where("batch_id = ?", product.EvidenceBatchID).First(&sample).Error)
	assert.Equal(t, "test", sample.ReferralKind)
	require.Error(t, SetRedemptionReferralKind([]int{sample.Id}, "paid", "exclude actual delivery sample", a))
	creditApprovalAction(t, product.ID, "submit")
	creditApprovalAction(t, product.ID, "approve")
	creditApprovalAction(t, product.ID, "publish")
	batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "formal-sale", FaceValueCents: 100, Count: 2, AdminUserID: 7, ProductID: product.ID, ProductRevision: product.Revision})
	require.NoError(t, err)
	require.Len(t, keys, 2)
	var codes []Redemption
	require.NoError(t, DB.Where("batch_id = ?", batch.Id).Order("id").Find(&codes).Error)
	require.Len(t, codes, 2)
	for _, code := range codes {
		assert.Equal(t, "paid", code.ReferralKind)
	}
	// Existing inventory needs explicit audited qualification. A mixed request
	// containing evidence must leave every code unchanged.
	require.NoError(t, DB.Model(&codes[0]).Update("referral_kind", "").Error)
	require.Error(t, SetRedemptionReferralKind([]int{codes[0].Id, sample.Id}, "paid", "user confirmed paid-sale purpose", a))
	require.NoError(t, DB.First(&codes[0], codes[0].Id).Error)
	assert.Empty(t, codes[0].ReferralKind)
	require.NoError(t, SetRedemptionReferralKind([]int{codes[0].Id}, "paid", "user confirmed paid-sale purpose", a))
	require.NoError(t, SetRedemptionReferralKind([]int{codes[0].Id}, "paid", "safe retry of paid qualification", a))
	require.NoError(t, DB.First(&codes[0], codes[0].Id).Error)
	assert.Equal(t, "paid", codes[0].ReferralKind)
	var audit ReferralAudit
	require.NoError(t, DB.Where("action = ?", "qualification").First(&audit).Error)
	assert.Contains(t, audit.Details, "user confirmed paid-sale purpose")
	assert.NotContains(t, audit.Details, codes[0].Key)
	require.NoError(t, DB.Model(&codes[0]).Update("status", common.RedemptionCodeStatusUsed).Error)
	require.Error(t, SetRedemptionReferralKind([]int{codes[0].Id}, "paid", "must not requalify redeemed code", a))
	for _, change := range []map[string]any{
		{"status": common.RedemptionCodeStatusDisabled},
		{"status": common.RedemptionCodeStatusEnabled, "expired_time": common.GetTimestamp() - 60},
		{"expired_time": 0, "quota": batch.Quota + 1},
	} {
		require.NoError(t, DB.Model(&codes[1]).Updates(change).Error)
		require.Error(t, SetRedemptionReferralKind([]int{codes[1].Id}, "paid", "invalid store inventory rejected", a))
	}
}

func TestReferralStoreRedemptionAndPayoutAreIdempotent(t *testing.T) {
	a, b := referralFixture(t)
	priorFlag := operation_setting.StoreRechargeEnabled
	operation_setting.StoreRechargeEnabled = true
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = priorFlag })
	seedStoreVIPLevels(t)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", b).Update("total_recharge_cents", 60000).Error)
	cfg, err := GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(cfg, a))
	code := referralStoreCode(t, "paid-store-duplicate", "paid", false)
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Go(func() { _, err := RedeemStoreCode(code.Key, b); results <- err })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	var reward ReferralReward
	require.NoError(t, DB.Where("redemption_id = ?", code.Id).First(&reward).Error)
	assert.EqualValues(t, 5000000, reward.SourceQuota)
	assert.EqualValues(t, 500000, reward.RewardQuota, "VIP bonus is excluded")
	for range 2 {
		wg.Go(func() { _ = PayReferralReward(reward.ID) })
	}
	wg.Wait()
	require.NoError(t, PayReferralReward(reward.ID))
	var inviter, buyer User
	require.NoError(t, DB.First(&inviter, a).Error)
	require.NoError(t, DB.First(&buyer, b).Error)
	assert.Equal(t, 500000, inviter.Quota)
	assert.EqualValues(t, 500000, inviter.ReferralPaidQuota)
	assert.Equal(t, 5100000, buyer.Quota)
	assert.EqualValues(t, 61000, buyer.TotalRechargeCents)
	for _, table := range []any{&ReferralReward{}, &ReferralLedger{}} {
		var count int64
		require.NoError(t, DB.Model(table).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	}
	var entries []BalanceTransaction
	require.NoError(t, DB.Where("user_id = ?", b).Order("id").Find(&entries).Error)
	require.Len(t, entries, 2)
	assert.Equal(t, 5000000, entries[0].AmountQuota)
	assert.Equal(t, 100000, entries[1].AmountQuota)
}

func TestReferralStoreEligibilityAndAtomicQueue(t *testing.T) {
	a, b := referralFixture(t)
	priorFlag := operation_setting.StoreRechargeEnabled
	operation_setting.StoreRechargeEnabled = true
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = priorFlag })
	cfg, err := GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(cfg, a))
	for _, tc := range []struct {
		name     string
		kind     string
		evidence bool
		inviter  int
		change   func(*Redemption)
	}{
		{"unqualified", "", false, a, nil},
		{"free", "free", false, a, nil},
		{"test", "test", false, a, nil},
		{"compensation", "compensation", false, a, nil},
		{"sample-even-if-paid", "paid", true, a, nil},
		{"unbound", "paid", false, a, func(c *Redemption) { c.BatchId = 0 }},
		{"mismatch", "paid", false, a, func(c *Redemption) { c.Quota++ }},
		{"no-inviter", "paid", false, 0, nil},
		{"self-invite", "paid", false, b, nil},
		{"missing-inviter", "paid", false, 99999, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := referralStoreCode(t, tc.name, tc.kind, tc.evidence)
			if tc.change != nil {
				tc.change(&code)
				require.NoError(t, DB.Save(&code).Error)
			}
			require.NoError(t, DB.Model(&User{}).Where("id = ?", b).Update("inviter_id", tc.inviter).Error)
			_, err := RedeemStoreCode(code.Key, b)
			require.NoError(t, err, "ineligible rewards must not prevent normal redemption")
			var count int64
			require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
	code := referralStoreCode(t, "disabled-inviter", "paid", false)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", b).Update("inviter_id", a).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", a).Update("status", common.UserStatusDisabled).Error)
	_, err = RedeemStoreCode(code.Key, b)
	require.NoError(t, err)
	var rewardCount int64
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&rewardCount).Error)
	assert.Zero(t, rewardCount)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", a).Update("status", common.UserStatusEnabled).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", b).Updates(map[string]any{"inviter_id": a, "quota": 0, "total_recharge_cents": 0}).Error)
	require.NoError(t, DB.Where("user_id = ?", b).Delete(&BalanceTransaction{}).Error)
	code = referralStoreCode(t, "queue-rollback", "paid", false)
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("store-referral-queue-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "referral_rewards" {
			tx.AddError(fmt.Errorf("isolated reward queue failure"))
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove("store-referral-queue-failure") })
	_, err = RedeemStoreCode(code.Key, b)
	require.Error(t, err)
	require.NoError(t, DB.Callback().Create().Remove("store-referral-queue-failure"))
	var buyer User
	require.NoError(t, DB.First(&buyer, b).Error)
	assert.Zero(t, buyer.Quota)
	assert.Zero(t, buyer.TotalRechargeCents)
	require.NoError(t, DB.First(&code, code.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, code.Status)
	var count int64
	require.NoError(t, DB.Model(&BalanceTransaction{}).Where("user_id = ?", b).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)
	// A disabled gate still accepts the recharge, without backfilling on enable.
	cfg, err = GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = false
	require.NoError(t, SaveReferralConfig(cfg, a))
	_, err = RedeemStoreCode(code.Key, b)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)
	cfg, err = GetReferralConfig()
	require.NoError(t, err)
	cfg.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(cfg, a))
	_, err = RedeemStoreCode(code.Key, b)
	require.Error(t, err)
	require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestReferralBulkQualificationAndRollback(t *testing.T) {
	a, b := referralFixture(t)
	require.NoError(t, PrepareCreditCatalog())
	product := creditApprovalProduct(t, "bulk-stock", 100)
	creditApprovalAction(t, product.ID, "submit")
	creditApprovalAction(t, product.ID, "approve")
	creditApprovalAction(t, product.ID, "publish")
	// 501 valid historical codes exercise the 500-bind write boundary with
	// real approved issuance (each batch remains within its 100-code limit).
	const stockCount = 501
	var firstKey string
	for start := 0; start < stockCount; start += 100 {
		batch, keys, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: fmt.Sprintf("bulk-%d", start), ProductID: product.ID, ProductRevision: product.Revision, FaceValueCents: 100, Count: min(100, stockCount-start), AdminUserID: a})
		require.NoError(t, err)
		if firstKey == "" {
			firstKey = keys[0]
		}
		require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", batch.Id).Update("referral_kind", "").Error)
	}
	// Both NULL and empty legacy qualifications are supported.
	require.NoError(t, DB.Model(&Redemption{}).Where(commonKeyCol+" = ?", firstKey).Update("referral_kind", nil).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", a).Update("quota", 123).Error)
	beforeRule, err := GetReferralConfig()
	require.NoError(t, err)
	preview, err := GetReferralStockPreview()
	require.NoError(t, err)
	assert.Equal(t, stockCount, preview.Count)
	assert.Len(t, preview.Batches, 6)
	var count int64
	require.NoError(t, DB.Model(&ReferralAudit{}).Where("action = ?", "qualification").Count(&count).Error)
	assert.Zero(t, count, "checking inventory is read-only")
	// A real classification change invalidates the complete reviewed set.
	require.NoError(t, DB.Model(&Redemption{}).Where(commonKeyCol+" = ?", firstKey).Update("referral_kind", "free").Error)
	updated, err := QualifyReferralStock(preview.Digest, a)
	require.ErrorIs(t, err, ErrReferralStockChanged)
	assert.Zero(t, updated)
	require.NoError(t, DB.Model(&Redemption{}).Where("referral_kind = ?", "paid").Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&Redemption{}).Where(commonKeyCol+" = ?", firstKey).Update("referral_kind", nil).Error)
	// An audit failure after both write chunks must roll back every code.
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("bulk-referral-audit-failure", func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*ReferralAudit); ok && audit.Action == "qualification" {
			tx.AddError(fmt.Errorf("isolated audit failure"))
		}
	}))
	updated, err = QualifyReferralStock(preview.Digest, a)
	require.Error(t, err)
	assert.Zero(t, updated)
	require.NoError(t, DB.Callback().Create().Remove("bulk-referral-audit-failure"))
	require.NoError(t, DB.Model(&Redemption{}).Where("referral_kind = ?", "paid").Count(&count).Error)
	assert.Zero(t, count)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	errors := make(chan error, 2)
	for range 2 {
		wg.Go(func() { n, err := QualifyReferralStock(preview.Digest, a); results <- n; errors <- err })
	}
	wg.Wait()
	close(results)
	close(errors)
	total := 0
	for n := range results {
		total += n
	}
	for err := range errors {
		require.NoError(t, err)
	}
	assert.Equal(t, stockCount, total)
	updated, err = QualifyReferralStock(preview.Digest, a)
	require.NoError(t, err)
	assert.Zero(t, updated)
	var audits []ReferralAudit
	require.NoError(t, DB.Where("action = ?", "qualification").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Equal(t, a, audits[0].ActorID)
	assert.NotContains(t, audits[0].Details, firstKey)
	afterRule, err := GetReferralConfig()
	require.NoError(t, err)
	assert.Equal(t, beforeRule, afterRule)
	var inviter, buyer User
	require.NoError(t, DB.First(&inviter, a).Error)
	require.NoError(t, DB.First(&buyer, b).Error)
	assert.Equal(t, 123, inviter.Quota)
	assert.Zero(t, buyer.Quota)
	for _, table := range []any{&ReferralReward{}, &ReferralLedger{}, &BalanceTransaction{}} {
		require.NoError(t, DB.Model(table).Count(&count).Error)
		assert.Zero(t, count, "qualification never issues a reward or changes balances")
	}
	preview, err = GetReferralStockPreview()
	require.NoError(t, err)
	assert.Zero(t, preview.Count)
	assert.NotNil(t, preview.Batches)
	// A subsequent real redemption in the isolated database follows the
	// existing reward queue and ledger, using base quota only.
	beforeRule.RewardsEnabled = true
	require.NoError(t, SaveReferralConfig(beforeRule, a))
	previousFlag := operation_setting.StoreRechargeEnabled
	operation_setting.StoreRechargeEnabled = true
	t.Cleanup(func() { operation_setting.StoreRechargeEnabled = previousFlag })
	_, err = RedeemStoreCode(firstKey, b)
	require.NoError(t, err)
	var reward ReferralReward
	require.NoError(t, DB.First(&reward).Error)
	assert.EqualValues(t, 50000, reward.RewardQuota)
	require.NoError(t, PayReferralReward(reward.ID))
	require.NoError(t, DB.First(&inviter, a).Error)
	assert.Equal(t, 50123, inviter.Quota)
}

func TestReferralBulkQualificationExcludesProtectedInventory(t *testing.T) {
	a, _ := referralFixture(t)
	require.NoError(t, PrepareCreditCatalog())
	product := creditApprovalProduct(t, "bulk-exclusions", 100)
	creditApprovalAction(t, product.ID, "activate")
	for _, tc := range []struct {
		name  string
		code  map[string]any
		batch map[string]any
	}{
		{"free", map[string]any{"referral_kind": "free"}, nil},
		{"test", map[string]any{"referral_kind": "test"}, nil},
		{"compensation", map[string]any{"referral_kind": "compensation"}, nil},
		{"already-qualified", map[string]any{"referral_kind": "paid"}, nil},
		{"used", map[string]any{"status": common.RedemptionCodeStatusUsed, "used_user_id": a, "redeemed_time": 123}, nil},
		{"disabled", map[string]any{"status": common.RedemptionCodeStatusDisabled}, nil},
		{"expired", map[string]any{"expired_time": common.GetTimestamp() - 60}, nil},
		{"source", map[string]any{"source": RedemptionSourceAdmin}, nil},
		{"type", map[string]any{"type": "SUBSCRIPTION"}, nil},
		{"soft-deleted", map[string]any{"deleted_at": time.Unix(1, 0)}, nil},
		{"quota-mismatch", map[string]any{"quota": 1}, nil},
		{"price-mismatch", map[string]any{"face_value_cents": 1}, nil},
		{"missing-batch", map[string]any{"batch_id": 999999}, nil},
		{"sample", nil, map[string]any{"evidence_only": true}},
		{"disabled-batch", nil, map[string]any{"status": RedemptionBatchStatusDisabled}},
		{"expired-batch", nil, map[string]any{"expired_time": common.GetTimestamp() - 60}},
		{"missing-approval", nil, map[string]any{"approval_id": 999999}},
		{"unapproved-batch", nil, map[string]any{"approval_id": 0}},
		{"batch-source", nil, map[string]any{"source": RedemptionSourceAdmin}},
		{"wrong-revision", nil, map[string]any{"product_revision": 99}},
	} {
		batch, _, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: tc.name, ProductID: product.ID, ProductRevision: product.Revision, FaceValueCents: 100, Count: 1, AdminUserID: a})
		require.NoError(t, err)
		require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", batch.Id).Update("referral_kind", "").Error)
		if tc.code != nil {
			require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", batch.Id).Updates(tc.code).Error)
		}
		if tc.batch != nil {
			require.NoError(t, DB.Model(&RedemptionBatch{}).Where("id = ?", batch.Id).Updates(tc.batch).Error)
		}
	}
	// One eligible code from automatic approval demonstrates that every
	// exclusion still applies to that policy's OR branch as well.
	batch, _, err := CreateRedemptionBatch(CreateRedemptionBatchInput{Name: "eligible-auto", ProductID: product.ID, ProductRevision: product.Revision, FaceValueCents: 100, Count: 1, AdminUserID: a})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", batch.Id).Update("referral_kind", "").Error)
	preview, err := GetReferralStockPreview()
	require.NoError(t, err)
	require.Equal(t, 1, preview.Count)
	require.Len(t, preview.Batches, 1)
	assert.Equal(t, batch.Id, preview.Batches[0].ID)
	var protectedBefore []Redemption
	require.NoError(t, DB.Unscoped().Where("batch_id <> ?", batch.Id).Order("id").Find(&protectedBefore).Error)
	updated, err := QualifyReferralStock(preview.Digest, a)
	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	require.NoError(t, DB.Model(&Redemption{}).Where("batch_id = ?", batch.Id).Update("status", common.RedemptionCodeStatusUsed).Error)
	updated, err = QualifyReferralStock(preview.Digest, a)
	require.NoError(t, err)
	assert.Zero(t, updated)
	var protectedAfter []Redemption
	require.NoError(t, DB.Unscoped().Where("batch_id <> ?", batch.Id).Order("id").Find(&protectedAfter).Error)
	assert.Equal(t, protectedBefore, protectedAfter)
}
