package service

import (
	"errors"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Protection is opt-in and never migrates a running database automatically.
func BillingProtectionEnabled() bool { return os.Getenv("CLEARAPI_BILLING_PROTECTION") == "true" }

// Durable wallet accounting does not impose the narrow text-only admission
// rules of billing protection. It still requires an explicit schema migration.
func DurableWalletBillingEnabled() bool { return os.Getenv("CLEARAPI_DURABLE_WALLET") == "true" }

func ValidateBillingProtection() error {
	if !BillingProtectionEnabled() && !DurableWalletBillingEnabled() {
		return nil
	}
	if common.RedisEnabled || common.BatchUpdateEnabled || os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		return errors.New("billing protection requires direct database accounting without Redis or batch updates")
	}
	if model.BillingOutboxEnabled() {
		if err := model.BillingCompletionOutboxReady(model.DB, model.LOG_DB); err != nil {
			return err
		}
	}
	// Enforce-mode reservation hardening guarantees a durable authorization
	// snapshot for every dispatch; a missing additive schema fails closed.
	if operation_setting.ReservationHardeningMode() == operation_setting.ReserveModeEnforce {
		if err := model.BillingAuthSnapshotsReady(model.DB); err != nil {
			return err
		}
	}
	return model.BillingReservationsReady(model.DB)
}

func StartBillingProtectionWorker() error {
	if !BillingProtectionEnabled() && !DurableWalletBillingEnabled() {
		return nil
	}
	if err := ValidateBillingProtection(); err != nil {
		return err
	}
	go func() {
		for {
			if err := model.RecoverBillingCompletions(100); err != nil {
				// Do not leak driver errors, credentials, or request identifiers.
				common.SysError("billing completion recovery requires attention")
			}
			if err := model.RecoverBillingCompletionLogs(100); err != nil {
				common.SysError("billing log delivery requires attention")
			}
			time.Sleep(10 * time.Second)
		}
	}()
	return nil
}
