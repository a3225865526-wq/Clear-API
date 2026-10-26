package service

import (
	"errors"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// DurableWalletBillingEnabled selects the existing reservation ledger. The old
// protection flag remains a deployment alias so outstanding accounting keeps
// the same completion/recovery path when upgrading.
func DurableWalletBillingEnabled() bool {
	return os.Getenv("CLEARAPI_DURABLE_WALLET") == "true" || os.Getenv("CLEARAPI_BILLING_PROTECTION") == "true"
}

func ValidateWalletBillingLedger() error {
	if !DurableWalletBillingEnabled() {
		return nil
	}
	if common.RedisEnabled || common.BatchUpdateEnabled || os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		return errors.New("wallet ledger requires direct database accounting without Redis or batch updates")
	}
	if model.BillingOutboxEnabled() {
		if err := model.BillingCompletionOutboxReady(model.DB, model.LOG_DB); err != nil {
			return err
		}
	}
	return model.BillingReservationsReady(model.DB)
}

func StartWalletBillingRecovery() error {
	if !DurableWalletBillingEnabled() {
		return nil
	}
	if err := ValidateWalletBillingLedger(); err != nil {
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
