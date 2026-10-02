package model

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPaymentReceiptDatabase(t *testing.T) {
	dialect, dsn := os.Getenv("TEST_BILLING_DIALECT"), os.Getenv("TEST_BILLING_DSN")
	if paymentDSN := os.Getenv("TEST_PAYMENT_DSN"); paymentDSN != "" {
		dsn = paymentDSN
	}
	var driver gorm.Dialector = sqlite.Open(":memory:")
	dbType := common.DatabaseTypeSQLite
	switch dialect {
	case "mysql":
		require.NotEmpty(t, dsn)
		driver = mysql.Open(dsn)
		dbType = common.DatabaseTypeMySQL
	case "postgres":
		require.NotEmpty(t, dsn)
		driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		dbType = common.DatabaseTypePostgreSQL
	case "", "sqlite":
	default:
		t.Fatal("unsupported isolated test dialect")
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	oldDB, oldLog := DB, LOG_DB
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldQuota := common.QuotaPerUnit
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	common.QuotaPerUnit = 5
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMain, oldLogType)
		common.QuotaPerUnit = oldQuota
		initCol()
	})
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}, &TopUp{}, &SubscriptionOrder{}, &SubscriptionPlan{}, &UserSubscription{}))
	versionQuery := "select version()"
	if dbType == common.DatabaseTypeSQLite {
		versionQuery = "select sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Log("engine", version)

	// Reproduce the pre-change order schema, seed an existing pending order,
	// then apply the additive migration twice. No live database is used.
	for _, entity := range []any{&TopUp{}, &SubscriptionOrder{}} {
		for _, column := range []string{"payment_amount", "payment_currency", "payment_reference"} {
			require.NoError(t, db.Migrator().DropColumn(entity, column))
		}
	}
	require.NoError(t, db.Table("top_ups").Create(map[string]any{"user_id": 900, "trade_no": "legacy-pending", "amount": 2, "money": 10, "payment_provider": PaymentProviderEpay, "status": common.TopUpStatusPending}).Error)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&TopUp{}, &SubscriptionOrder{}))
	}
	legacy := GetTopUpByTradeNo("legacy-pending")
	require.NotNil(t, legacy)
	assert.Equal(t, common.TopUpStatusPending, legacy.Status)
	assert.True(t, db.Migrator().HasIndex(&TopUp{}, "TradeNo"))

	for i, provider := range []string{PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo} {
		t.Run(provider, func(t *testing.T) {
			user := User{Id: i + 901, Username: fmt.Sprintf("synthetic-%d", i), AffCode: fmt.Sprintf("receipt-%d", i), Status: common.UserStatusEnabled, Quota: 7}
			require.NoError(t, db.Create(&user).Error)
			expected := PaymentReceipt{Amount: "1000", Currency: "USD", Reference: "checkout-fixture"}
			if provider == PaymentProviderEpay {
				expected = PaymentReceipt{Amount: "10.00", Currency: "CNY"}
			}
			if provider == PaymentProviderWaffo {
				expected = PaymentReceipt{Amount: "10.00", Currency: "USD"}
			}
			order := TopUp{UserId: user.Id, TradeNo: "receipt-" + provider, Money: 10, Amount: 2, PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, PaymentReceipt: expected}
			require.NoError(t, order.Insert())
			complete := func(receipt PaymentReceipt) error {
				switch provider {
				case PaymentProviderEpay:
					_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1", receipt)
					return err
				case PaymentProviderStripe:
					return Recharge(order.TradeNo, "synthetic-customer", "127.0.0.1", receipt)
				case PaymentProviderWaffo:
					return RechargeWaffo(order.TradeNo, "127.0.0.1", receipt)
				default:
					return RechargeCreem(order.TradeNo, "", "", "127.0.0.1", receipt)
				}
			}
			for _, receipt := range []PaymentReceipt{{Amount: "1", Currency: expected.Currency, Reference: expected.Reference}, {Amount: expected.Amount, Currency: "EUR", Reference: expected.Reference}, {Amount: "NaN", Currency: expected.Currency, Reference: expected.Reference}} {
				require.ErrorIs(t, complete(receipt), ErrPaymentReceiptMismatch)
				assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
				assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
			}
			if expected.Reference != "" {
				wrong := expected
				wrong.Reference = "another-checkout"
				require.ErrorIs(t, complete(wrong), ErrPaymentReceiptMismatch)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for range 2 {
				wg.Go(func() { errs <- complete(expected) })
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			credited := 10
			if provider == PaymentProviderStripe {
				credited = 50
			}
			if provider == PaymentProviderCreem {
				credited = 2
			}
			assert.Equal(t, 7+credited, getUserQuotaForPaymentGuardTest(t, user.Id))
			assert.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo(order.TradeNo).Status)
		})
	}
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 901)
	order := SubscriptionOrder{UserId: 901, PlanId: plan.Id, Money: 9.99, TradeNo: "subscription-receipt", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Status: common.TopUpStatusPending, PaymentReceipt: PaymentReceipt{Amount: "999", Currency: "USD", Reference: "subscription-checkout"}}
	require.NoError(t, order.Insert())
	require.ErrorIs(t, CompleteSubscriptionOrder(order.TradeNo, "", PaymentProviderStripe, "", PaymentReceipt{Amount: "1", Currency: "USD", Reference: "subscription-checkout"}), ErrPaymentReceiptMismatch)
	assert.EqualValues(t, 0, countUserSubscriptionsForPaymentGuardTest(t, 901))
	for range 2 {
		require.NoError(t, CompleteSubscriptionOrder(order.TradeNo, "", PaymentProviderStripe, "", order.PaymentReceipt))
	}
	assert.EqualValues(t, 1, countUserSubscriptionsForPaymentGuardTest(t, 901))

	require.NoError(t, MigrateBillingReservations(db))
	token := Token{Id: 991, UserId: 901, Key: "synthetic-quota-key", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000}
	require.NoError(t, db.Create(&token).Error)
	initial := getUserQuotaForPaymentGuardTest(t, 901)
	require.NoError(t, ReserveBillingRequest("durable-grow", 901, 991, 3))
	require.NoError(t, RaiseBillingReservation("durable-grow", 6))
	require.NoError(t, RaiseBillingReservation("durable-grow", 6))
	assert.Equal(t, initial-6, getUserQuotaForPaymentGuardTest(t, 901))
	require.ErrorIs(t, RaiseBillingReservation("durable-grow", 100), ErrBillingReservationQuota)
	assert.Equal(t, initial-6, getUserQuotaForPaymentGuardTest(t, 901))
	require.NoError(t, RequestBillingCompletion("durable-grow", 0, true))
	require.NoError(t, RecoverBillingCompletions(10))
	require.NoError(t, RecoverBillingCompletions(10))
	assert.Equal(t, initial, getUserQuotaForPaymentGuardTest(t, 901))
}
