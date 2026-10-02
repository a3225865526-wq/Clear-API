package service

import (
	"errors"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Protection is opt-in and never migrates a running database automatically.
func BillingProtectionEnabled() bool { return os.Getenv("CLEARAPI_BILLING_PROTECTION") == "true" }

func ValidateBillingProtection() error {
	if !BillingProtectionEnabled() {
		return nil
	}
	if common.RedisEnabled || common.BatchUpdateEnabled || os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		return errors.New("billing protection requires direct database accounting without Redis or batch updates")
	}
	return model.BillingReservationsReady(model.DB)
}

func StartBillingProtectionWorker() error {
	if !BillingProtectionEnabled() {
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
			time.Sleep(10 * time.Second)
		}
	}()
	return nil
}
