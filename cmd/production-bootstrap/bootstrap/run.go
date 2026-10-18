package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// Dispatch routes the parsed flags to one of the three modes: dry-run
// (default, zero writes), --verify-only (zero writes) or --execute.
func Dispatch(cfg *Config) int {
	if cfg.Execute && cfg.VerifyOnly {
		Phase("--execute and --verify-only are mutually exclusive")
		Emit("input_error", nil)
		return ExitInput
	}
	switch {
	case cfg.VerifyOnly:
		return RunVerifyOnly(cfg)
	case cfg.Execute:
		return RunExecute(cfg)
	default:
		return RunDryRun(cfg)
	}
}

func closeQuietly() {
	if err := model.CloseDB(); err != nil {
		Phase("failed to close database: %v", err)
	}
}

func emitEnvironmentIssues(issues []EnvIssue) {
	fields := make(map[string]any, len(issues))
	for _, issue := range issues {
		fields[issue.Name] = issue.Detail
	}
	Emit("environment_error", fields)
}

// RunDryRun validates inputs and environment, inspects the database read-only
// and prints what --execute would do. It performs zero writes: in particular
// it never touches the migrating InitDB path (Amendment 2).
func RunDryRun(cfg *Config) int {
	if err := CheckSQLDSN(); err != nil {
		Phase("%v", err)
		Emit("input_error", nil)
		return ExitEnvironment
	}
	envIssues := CheckProductionEnv()
	if err := CheckTokenKeyFile(); err != nil {
		envIssues = append(envIssues, EnvIssue{Name: "CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE", Detail: err.Error()})
	}
	if err := OpenReadonly(); err != nil {
		Phase("database probe failed: %v", err)
		Emit("probe_failed", nil)
		return ExitEnvironment
	}
	defer closeQuietly()
	snapshot := TakeSnapshot()

	needAdmin := snapshot.UserCount == 0 && snapshot.State == nil
	needLegal := !snapshot.LegalUserAgreementPresent || !snapshot.LegalPrivacyPolicyPresent || !snapshot.LegalManifestPresent
	missingInputs := []string{}
	if needAdmin {
		if cfg.AdminUsername == "" {
			missingInputs = append(missingInputs, "--admin-username")
		}
		if cfg.AdminPasswordFile == "" {
			missingInputs = append(missingInputs, "--admin-password-file")
		}
	}
	if needLegal {
		if cfg.TermsFile == "" || cfg.TermsVersion == "" {
			missingInputs = append(missingInputs, "--terms-file/--terms-version")
		}
		if cfg.PrivacyFile == "" || cfg.PrivacyVersion == "" {
			missingInputs = append(missingInputs, "--privacy-file/--privacy-version")
		}
		if cfg.ApprovedBy == "" {
			missingInputs = append(missingInputs, "--approved-by")
		}
	}

	plan := map[string]any{
		"database_type":          snapshot.DatabaseType,
		"users_table_present":    snapshot.UsersTablePresent,
		"user_count":             snapshot.UserCount,
		"root_user_exists":       snapshot.RootUserExists,
		"bootstrap_state":        snapshot.State != nil,
		"completion_marker":      snapshot.Marker != nil,
		"legal_present":          snapshot.LegalUserAgreementPresent && snapshot.LegalPrivacyPolicyPresent && snapshot.LegalManifestPresent,
		"setup_row_present":      snapshot.SetupRowPresent,
		"finance_schema_version": snapshot.FinanceSchemaVersion,
		"schema_readiness": map[string]bool{
			"billing_reservations": snapshot.ReservationsReady,
			"billing_outbox":       snapshot.OutboxReady,
			"reconciliation":       snapshot.ReconciliationReady,
			"task_settlement":      snapshot.SettlementReady,
			"task_settlement_logs": snapshot.SettlementLogsReady,
			"token_encryption":     snapshot.TokenEncryptionReady,
			"image_executions":     snapshot.ImageExecutionsReady,
		},
		"pending": map[string]bool{
			"base_and_finance_and_billing_migrations": !(snapshot.FinanceSchemaPresent && snapshot.FinanceSchemaVersion == 9 &&
				snapshot.ReservationsReady && snapshot.OutboxReady && snapshot.ReconciliationReady &&
				snapshot.SettlementReady && snapshot.SettlementLogsReady),
			"admin_creation":    needAdmin,
			"legal_write":       needLegal,
			"completion_marker": true,
		},
		"missing_inputs":       missingInputs,
		"environment_findings": environmentIssueMap(envIssues),
	}
	Emit("dry_run", plan)
	return ExitOK
}

// RunVerifyOnly checks an already bootstrapped deployment through the real
// production gate without writing anything (Amendment 2).
func RunVerifyOnly(cfg *Config) int {
	if err := CheckSQLDSN(); err != nil {
		Phase("%v", err)
		Emit("input_error", nil)
		return ExitEnvironment
	}
	if issues := CheckProductionEnv(); len(issues) > 0 {
		for _, issue := range issues {
			Phase("environment: %s %s", issue.Name, issue.Detail)
		}
		emitEnvironmentIssues(issues)
		return ExitEnvironment
	}
	if err := CheckTokenKeyFile(); err != nil {
		Phase("%v", err)
		Emit("environment_error", nil)
		return ExitEnvironment
	}
	if err := OpenReadonly(); err != nil {
		Phase("database probe failed: %v", err)
		Emit("probe_failed", nil)
		return ExitEnvironment
	}
	defer closeQuietly()
	// Populate the option map and derived config globals from the database —
	// exactly what a fresh server process does before the gate runs. Without
	// this, ValidateProductionReadiness would read empty legal settings in a
	// fresh process even on a correctly bootstrapped database.
	model.InitOptionMap()
	snapshot := TakeSnapshot()

	if snapshot.MarkerCorrupt {
		Phase("bootstrap completion marker is corrupt; fail closed (no automatic repair)")
		Emit("marker_corrupt", nil)
		return ExitCorruption
	}
	if snapshot.Marker == nil {
		Emit("not_bootstrapped", map[string]any{"database_type": snapshot.DatabaseType})
		return ExitOK
	}
	if err := service.ValidateProductionReadiness(); err != nil {
		Phase("production readiness failed: %v", err)
		Emit("readiness_failed", map[string]any{"error": err.Error()})
		return ExitReadiness
	}
	Emit("verified", markerFields(snapshot.Marker))
	return ExitOK
}

// RunExecute performs the one-shot bootstrap. Branch decisions come from the
// read-only probe; already-bootstrapped exits 0 (Amendment 1) before any
// migrating initialization runs, and a populated foreign database is refused
// before anything is written.
func RunExecute(cfg *Config) int {
	if err := CheckSQLDSN(); err != nil {
		Phase("%v", err)
		Emit("input_error", nil)
		return ExitEnvironment
	}
	if issues := CheckProductionEnv(); len(issues) > 0 {
		for _, issue := range issues {
			Phase("environment: %s %s", issue.Name, issue.Detail)
		}
		emitEnvironmentIssues(issues)
		return ExitEnvironment
	}
	if err := CheckTokenKeyFile(); err != nil {
		Phase("%v", err)
		Emit("environment_error", nil)
		return ExitEnvironment
	}

	if err := OpenReadonly(); err != nil {
		Phase("database probe failed: %v", err)
		Emit("probe_failed", nil)
		return ExitEnvironment
	}
	snapshot := TakeSnapshot()
	if snapshot.MarkerCorrupt {
		Phase("bootstrap completion marker is corrupt; fail closed (no automatic repair)")
		Emit("marker_corrupt", nil)
		closeQuietly()
		return ExitCorruption
	}
	if snapshot.Marker != nil {
		Phase("bootstrap completion marker present; nothing to do")
		Emit("already_bootstrapped", markerFields(snapshot.Marker))
		closeQuietly()
		return ExitOK
	}
	if snapshot.StateCorrupt {
		Phase("bootstrap state is corrupt; fail closed (no automatic repair)")
		Emit("state_corrupt", nil)
		closeQuietly()
		return ExitCorruption
	}
	if snapshot.UserCount > 0 && snapshot.State == nil {
		Phase("database already contains %d user(s) without bootstrap state; refusing to bootstrap a foreign database", snapshot.UserCount)
		Emit("populated_db_refusal", map[string]any{"user_count": snapshot.UserCount})
		closeQuietly()
		return ExitPopulatedDBRefusal
	}
	closeQuietly()

	fresh := snapshot.UserCount == 0 && snapshot.State == nil
	needAdmin := fresh
	needLegal := !snapshot.LegalUserAgreementPresent || !snapshot.LegalPrivacyPolicyPresent || !snapshot.LegalManifestPresent

	var password string
	if needAdmin {
		if cfg.AdminUsername == "" || cfg.AdminPasswordFile == "" {
			Phase("--admin-username and --admin-password-file are required on a fresh database")
			Emit("input_error", nil)
			return ExitInput
		}
		value, err := readSecretFile(cfg.AdminPasswordFile)
		if err != nil {
			Phase("%v", err)
			Emit("input_error", nil)
			return ExitInput
		}
		password = value
	}
	// Legal inputs are deliberately NOT required here: stage 1 (migrations +
	// admin + state) must be able to complete and stop safely at the finance
	// owner gate even when no approved legal content exists yet; the legal
	// phase reads its inputs right before it runs.

	// Server-parity initialization: base schema, authz, log DB, option map.
	// model.CheckSetup() is intentionally not called; the setup row is
	// written inside the bootstrap legal transaction.
	Phase("running base schema migrations (server InitDB path)")
	if err := model.InitDB(); err != nil {
		Phase("base migration failed: %v", err)
		Emit("migration_failed", map[string]any{"phase": "base"})
		return ExitMigration
	}
	defer closeQuietly()
	if err := authz.Init(model.DB); err != nil {
		Phase("authorization initialization failed: %v", err)
		Emit("migration_failed", map[string]any{"phase": "authz"})
		return ExitMigration
	}
	if err := model.InitLogDB(); err != nil {
		Phase("log database initialization failed: %v", err)
		Emit("migration_failed", map[string]any{"phase": "log_db"})
		return ExitMigration
	}
	model.InitOptionMap()

	Phase("applying approved finance schema migrations")
	for _, step := range financeMigrationSteps() {
		if err := step.fn(model.DB); err != nil {
			Phase("finance migration %s failed: %v", step.name, err)
			Emit("migration_failed", map[string]any{"phase": step.name})
			return ExitMigration
		}
		Phase("finance migration %s applied", step.name)
	}

	Phase("applying approved billing schema migrations")
	appliedBilling := []string{}
	for _, step := range billingMigrationSteps() {
		if err := step.fn(); err != nil {
			Phase("billing migration %s failed: %v", step.name, err)
			Emit("migration_failed", map[string]any{"phase": step.name})
			return ExitMigration
		}
		appliedBilling = append(appliedBilling, step.name)
		Phase("billing migration %s applied", step.name)
	}

	// Data phase: admin + bootstrap state in one transaction.
	var state *model.BootstrapState
	if fresh {
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			user, err := model.CreateRootAccount(tx, model.RootAccountInput{
				Username:     cfg.AdminUsername,
				Password:     password,
				DisplayName:  cfg.AdminDisplayName,
				InitialQuota: cfg.AdminInitialQuota,
			})
			if err != nil {
				return err
			}
			password = "" // keep the plaintext only as long as needed
			st := &model.BootstrapState{
				StartedAt:            time.Now().UTC().Format(time.RFC3339),
				AdminUserID:          user.Id,
				AdminUsername:        user.Username,
				BootstrapToolVersion: toolVersion(),
				CandidateCommit:      cfg.CandidateCommit,
			}
			return model.WriteBootstrapStateTx(tx, st)
		})
		if err != nil {
			password = ""
			if isRootInputError(err) {
				Phase("administrator input rejected: %v", err)
				Emit("input_error", map[string]any{"error": err.Error()})
				return ExitInput
			}
			Phase("administrator creation failed: %v", err)
			Emit("persistence_failed", map[string]any{"phase": "admin"})
			return ExitPersistence
		}
		state, err = model.ReadBootstrapState(model.DB)
		if err != nil || state == nil {
			Phase("bootstrap state unreadable after admin creation: %v", err)
			Emit("state_corrupt", nil)
			return ExitCorruption
		}
		Phase("administrator %q created (real user id %d, initial quota %d)", state.AdminUsername, state.AdminUserID, cfg.AdminInitialQuota)
	} else {
		state = snapshot.State
		var user model.User
		if err := model.DB.Where(&model.User{Username: state.AdminUsername}).First(&user).Error; err != nil {
			Phase("bootstrap state references missing administrator %q", state.AdminUsername)
			Emit("state_corrupt", nil)
			return ExitCorruption
		}
		if user.Id != state.AdminUserID || user.Role != common.RoleRootUser || user.Status != common.UserStatusEnabled {
			Phase("existing administrator does not match the recorded bootstrap state; refusing to modify it")
			Emit("state_mismatch", nil)
			return ExitCorruption
		}
		Phase("existing administrator %q (id %d) verified; not recreating or resetting credentials", user.Username, user.Id)
	}

	// Finance owner gate (Amendment 6): the real owner ID must already be
	// part of the runtime configuration; it is never forged.
	ownerID, parseErr := strconv.Atoi(strings.TrimSpace(os.Getenv("CLEARAPI_FINANCE_OWNER_USER_ID")))
	if parseErr != nil || ownerID != state.AdminUserID {
		Phase("FINANCE_OWNER_CONFIGURATION_REQUIRED: set CLEARAPI_FINANCE_ENABLED=true and CLEARAPI_FINANCE_OWNER_USER_ID=%d in the production configuration, then rerun this command", state.AdminUserID)
		Emit("finance_owner_configuration_required", map[string]any{
			"finance_owner_user_id": state.AdminUserID,
			"admin_username":        state.AdminUsername,
		})
		return ExitFinanceOwnerConfigRequired
	}

	// Legal phase: content + approval manifest + safe modes + setup row in
	// one transaction.
	if !needLegal && (cfg.TermsFile != "" || cfg.PrivacyFile != "") {
		// Amendment J: when legal content already exists and the operator
		// still supplies files on a rerun, their hashes must match the
		// recorded approval manifest exactly — otherwise fail closed.
		matches, err := suppliedLegalMatchesManifest(cfg)
		if err != nil {
			Phase("legal rerun verification failed: %v", err)
			Emit("legal_mismatch", nil)
			return ExitLegalMismatch
		}
		if !matches {
			Phase("supplied legal files do not match the recorded approval manifest; refusing to overwrite")
			Emit("legal_mismatch", nil)
			return ExitLegalMismatch
		}
	}
	if needLegal {
		termsContent, privacyContent, err := readLegalInputs(cfg)
		if err != nil {
			Phase("%v", err)
			Emit("input_error", map[string]any{"phase": "legal"})
			return ExitInput
		}
		err = model.DB.Transaction(func(tx *gorm.DB) error {
			return model.WriteBootstrapLegalTx(tx, model.BootstrapLegalInput{
				UserAgreement:        termsContent,
				PrivacyPolicy:        privacyContent,
				TermsVersion:         cfg.TermsVersion,
				PrivacyVersion:       cfg.PrivacyVersion,
				ApprovedBy:           cfg.ApprovedBy,
				CandidateCommit:      cfg.CandidateCommit,
				BootstrapToolVersion: toolVersion(),
				SelfUseMode:          cfg.SelfUseMode,
				DemoSite:             cfg.DemoSite,
			})
		})
		if err != nil {
			switch {
			case errors.Is(err, model.ErrLegalContentMismatch):
				Phase("legal content does not match the recorded approval manifest; refusing to overwrite")
				Emit("legal_mismatch", nil)
				return ExitLegalMismatch
			case errors.Is(err, model.ErrLegalManifestCorrupt):
				Phase("legal approval manifest is corrupt; fail closed")
				Emit("manifest_corrupt", nil)
				return ExitCorruption
			case errors.Is(err, model.ErrLegalInputInvalid):
				Phase("%v", err)
				Emit("input_error", nil)
				return ExitInput
			default:
				Phase("legal write failed: %v", err)
				Emit("persistence_failed", map[string]any{"phase": "legal"})
				return ExitPersistence
			}
		}
		// Refresh the in-memory option map (the same path the server uses) so
		// ValidateProductionReadiness sees the committed legal content.
		if err := model.ApplyCommittedOptionValues(map[string]string{
			model.LegalUserAgreementOptionKey: termsContent,
			model.LegalPrivacyPolicyOptionKey: privacyContent,
			"SelfUseModeEnabled":              fmt.Sprintf("%t", cfg.SelfUseMode),
			"DemoSiteEnabled":                 fmt.Sprintf("%t", cfg.DemoSite),
		}); err != nil {
			Phase("option map refresh failed: %v", err)
			Emit("persistence_failed", map[string]any{"phase": "option_map"})
			return ExitPersistence
		}
		operation_setting.SelfUseModeEnabled = cfg.SelfUseMode
		operation_setting.DemoSiteEnabled = cfg.DemoSite
		Phase("legal content, approval manifest and safe modes committed")
	} else {
		Phase("legal content already present and verified; skipping")
	}

	// Final verification through the real production gate.
	Phase("validating production readiness (service.ValidateProductionReadiness)")
	if err := service.ValidateProductionReadiness(); err != nil {
		Phase("production readiness failed: %v", err)
		Emit("readiness_failed", map[string]any{"error": err.Error()})
		return ExitReadiness
	}

	// Completion marker: written last, then read back and re-validated.
	manifest, err := model.ReadBootstrapLegalManifest(model.DB)
	if err != nil || manifest == nil {
		Phase("approval manifest missing after legal phase: %v", err)
		Emit("persistence_failed", map[string]any{"phase": "manifest"})
		return ExitPersistence
	}
	financeVersion, ok, err := model.ReadFinanceSchemaVersion(model.DB)
	if err != nil || !ok {
		Phase("finance schema version unreadable after migrations: %v", err)
		Emit("persistence_failed", map[string]any{"phase": "finance_schema"})
		return ExitPersistence
	}
	marker := &model.BootstrapCompletionMarker{
		CompletedAt:              time.Now().UTC().Format(time.RFC3339),
		BootstrapToolVersion:     toolVersion(),
		CandidateCommit:          cfg.CandidateCommit,
		AdminUserID:              state.AdminUserID,
		AdminUsername:            state.AdminUsername,
		FinanceOwnerUserID:       ownerID,
		TermsVersion:             manifest.TermsVersion,
		TermsSHA256:              manifest.TermsSHA256,
		PrivacyVersion:           manifest.PrivacyVersion,
		PrivacySHA256:            manifest.PrivacySHA256,
		ApprovedBy:               manifest.ApprovedBy,
		ApprovedAt:               manifest.ApprovedAt,
		FinanceSchemaVersion:     financeVersion,
		BillingMigrationsApplied: appliedBilling,
		DatabaseType:             string(common.MainDatabaseType()),
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error { return model.WriteBootstrapMarkerTx(tx, marker) }); err != nil {
		Phase("completion marker write failed: %v", err)
		Emit("persistence_failed", map[string]any{"phase": "marker"})
		return ExitPersistence
	}
	written, err := model.ReadBootstrapMarker(model.DB)
	if err != nil || written == nil {
		Phase("completion marker read-back validation failed: %v", err)
		Emit("marker_corrupt", nil)
		return ExitCorruption
	}
	Phase("bootstrap completed; completion marker written and re-verified")
	Emit("bootstrapped", markerFields(written))
	return ExitOK
}

func markerFields(marker *model.BootstrapCompletionMarker) map[string]any {
	return map[string]any{
		"admin_user_id":         marker.AdminUserID,
		"admin_username":        marker.AdminUsername,
		"finance_owner_user_id": marker.FinanceOwnerUserID,
		"completed_at":          marker.CompletedAt,
		"candidate_commit":      marker.CandidateCommit,
		"database_type":         marker.DatabaseType,
	}
}

func environmentIssueMap(issues []EnvIssue) map[string]string {
	out := make(map[string]string, len(issues))
	for _, issue := range issues {
		out[issue.Name] = issue.Detail
	}
	return out
}

func isRootInputError(err error) bool {
	return errors.Is(err, model.ErrRootUsernameRequired) ||
		errors.Is(err, model.ErrRootUsernameTooLong) ||
		errors.Is(err, model.ErrRootQuotaInvalid) ||
		errors.Is(err, common.ErrAccountPasswordLength)
}

type migrationStep struct {
	name string
	fn   func(*gorm.DB) error
}

func financeMigrationSteps() []migrationStep {
	return []migrationStep{
		{"finance-v1", model.MigrateFinance},
		{"finance-revenue-v2", model.MigrateFinanceRevenue},
		{"finance-requests-v3", model.MigrateFinanceRequests},
		{"finance-receipts-v4", model.MigrateFinanceReceipts},
		{"finance-supplier-v5", model.MigrateFinanceSupplier},
		{"finance-refunds-v6", model.MigrateFinanceRefunds},
		{"finance-sync-v7", model.MigrateFinanceSync},
		{"finance-rebuilds-v8", model.MigrateFinanceRebuilds},
		{"finance-supplier-lines-v9", model.MigrateFinanceSupplierLines},
	}
}

type billingStep struct {
	name string
	fn   func() error
}

func billingMigrationSteps() []billingStep {
	return []billingStep{
		{"billing-reservations-v1", func() error { return model.MigrateBillingReservations(model.DB) }},
		{"billing-auth-snapshots-v1", func() error { return model.MigrateBillingAuthSnapshots(model.DB) }},
		{"billing-reconciliation-v1", func() error { return model.MigrateBillingReconciliation(model.DB) }},
		{"task-settlement-v1", func() error { return model.MigrateTaskSettlements(model.DB) }},
		{"task-settlement-logs-v1", func() error { return model.MigrateTaskSettlementLogs(model.LOG_DB) }},
		{"token-encryption-v1", func() error { return model.MigrateTokenEncryption(model.DB) }},
		{"billing-outbox-v1", func() error { return model.MigrateBillingCompletionOutbox(model.DB) }},
		{"billing-receipts-v1", func() error { return model.MigrateBillingCompletionReceipts(model.DB) }},
		// PG03 Images Contract v1. Controlled image execution fails closed
		// without these tables, so an enabled deployment must migrate them
		// explicitly; they are never created by an implicit startup AutoMigrate.
		{"image-executions-v1", func() error { return model.MigrateImageExecutions(model.DB) }},
	}
}

func readLegalInputs(cfg *Config) (string, string, error) {
	if strings.TrimSpace(cfg.TermsVersion) == "" || strings.TrimSpace(cfg.PrivacyVersion) == "" || strings.TrimSpace(cfg.ApprovedBy) == "" {
		return "", "", errors.New("--terms-version, --privacy-version and --approved-by are required for the legal phase")
	}
	terms, err := readLegalInput(cfg.TermsFile)
	if err != nil {
		return "", "", err
	}
	privacy, err := readLegalInput(cfg.PrivacyFile)
	if err != nil {
		return "", "", err
	}
	return terms, privacy, nil
}

// suppliedLegalMatchesManifest compares operator-supplied rerun files with the
// recorded approval manifest and the stored legal content.
func suppliedLegalMatchesManifest(cfg *Config) (bool, error) {
	manifest, err := model.ReadBootstrapLegalManifest(model.DB)
	if err != nil {
		return false, err
	}
	if manifest == nil {
		return false, errors.New("approval manifest is missing")
	}
	if cfg.TermsFile != "" {
		content, err := readLegalInput(cfg.TermsFile)
		if err != nil {
			return false, err
		}
		stored, found, err := model.ReadBootstrapOption(model.DB, model.LegalUserAgreementOptionKey)
		if err != nil || !found || stored != content {
			return false, err
		}
		if legalSHA256(content) != manifest.TermsSHA256 {
			return false, nil
		}
	}
	if cfg.PrivacyFile != "" {
		content, err := readLegalInput(cfg.PrivacyFile)
		if err != nil {
			return false, err
		}
		stored, found, err := model.ReadBootstrapOption(model.DB, model.LegalPrivacyPolicyOptionKey)
		if err != nil || !found || stored != content {
			return false, err
		}
		if legalSHA256(content) != manifest.PrivacySHA256 {
			return false, nil
		}
	}
	return true, nil
}

func legalSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// toolVersion reports the bootstrap tool version for audit records. The
// release build stamps common.Version via ldflags; fall back to the package
// default when the VERSION file is empty so markers always carry a version.
func toolVersion() string {
	if common.Version != "" {
		return common.Version
	}
	return "v0.0.0"
}
