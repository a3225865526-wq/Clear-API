package service

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// Production mode is explicit and fails closed. It never upgrades business
// data or enables a legacy billing fallback to make startup succeed.
func ValidateProductionReadiness() error {
	if os.Getenv("CLEARAPI_PRODUCTION") != "true" {
		return nil
	}
	for name, expected := range map[string]string{
		"GIN_MODE": "release", "SESSION_COOKIE_SECURE": "true",
		"CLEARAPI_DURABLE_WALLET": "true", "CLEARAPI_BILLING_OUTBOX": "true",
		"CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE": "true",
		"CLEARAPI_TASK_SETTLEMENT":                        "true", "CLEARAPI_STRICT_PREPAID": "true",
		"CLEARAPI_TOKEN_ENCRYPTION": "true", "CLEARAPI_DEPLOYMENT_INSTANCES": "1",
		"CLEARAPI_ENABLE_REALTIME": "false",
	} {
		if os.Getenv(name) != expected {
			return errors.New("production configuration requirement missing: " + name)
		}
	}
	for _, name := range []string{"DEBUG", "DIFY_DEBUG", "ENABLE_PPROF", "BATCH_UPDATE_ENABLED"} {
		if os.Getenv(name) != "false" {
			return errors.New("production flag must be false: " + name)
		}
	}
	if common.DebugEnabled || common.RedisEnabled || common.BatchUpdateEnabled {
		return errors.New("production accounting requires direct single-instance database access")
	}
	if len(os.Getenv("SESSION_SECRET")) < 32 {
		return errors.New("production requires an explicit persistent session secret of at least 32 bytes")
	}
	if !common.FinanceEnabled() {
		return errors.New("production requires an explicitly configured finance owner")
	}
	origin, err := url.Parse(os.Getenv("SESSION_COOKIE_TRUSTED_URL"))
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" {
		return errors.New("production requires exact HTTPS origin")
	}
	if strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")) == "" {
		return errors.New("production trusted proxies must be explicit")
	}
	for _, name := range []string{"MAX_UPSTREAM_RESPONSE_MB", "MAX_REQUEST_BODY_MB", "RELAY_CONCURRENCY_GLOBAL", "RELAY_CONCURRENCY_USER", "RELAY_CONCURRENCY_TOKEN", "RELAY_CONCURRENCY_IP"} {
		n, err := strconv.Atoi(os.Getenv(name))
		if err != nil || n < 1 || n > 128 {
			return errors.New("production resource limit invalid: " + name)
		}
	}
	if err := ValidateBillingProtection(); err != nil {
		return errors.New("production billing schema unavailable")
	}
	if !model.BillingReconciliationEnabled() || model.BillingReconciliationReady(model.DB) != nil {
		return errors.New("production reconciliation unavailable")
	}
	if model.TaskSettlementReady(model.DB) != nil || model.TaskSettlementLogsReady(model.LOG_DB) != nil {
		return errors.New("production task settlement unavailable")
	}
	if err := model.TokenEncryptionReady(model.DB); err != nil {
		return err
	}
	legal := system_setting.GetLegalSettings()
	if strings.TrimSpace(legal.UserAgreement) == "" || strings.TrimSpace(legal.PrivacyPolicy) == "" {
		return errors.New("production requires published operator-approved terms and privacy policy")
	}
	return nil
}
