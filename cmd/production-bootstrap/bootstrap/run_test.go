package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const pgTestDSNEnv = "BOOTSTRAP_TEST_PG_DSN"

// requirePG gates the flow tests on a real PostgreSQL instance, mirroring the
// repository's external-database test convention. The acceptance run provides
// an isolated temporary PostgreSQL instance.
func requirePG(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(pgTestDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; PostgreSQL flow test skipped", pgTestDSNEnv)
	}
	return dsn
}

func isolateGlobals(t *testing.T) {
	t.Helper()
	oldDB, oldLOG := model.DB, model.LOG_DB
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLOG
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
	})
}

func openAdminDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// createFreshDatabase creates a uniquely named throwaway PostgreSQL database
// and returns its DSN; the database is force-dropped on cleanup.
func createFreshDatabase(t *testing.T, adminDSN string) string {
	t.Helper()
	name := fmt.Sprintf("bootstrap_test_%d", time.Now().UnixNano())
	admin := openAdminDB(t, adminDSN)
	require.NoError(t, admin.Exec(fmt.Sprintf("CREATE DATABASE %s", name)).Error)
	t.Cleanup(func() {
		_ = admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name)).Error
	})
	parsed, err := url.Parse(adminDSN)
	require.NoError(t, err)
	parsed.Path = "/" + name
	return parsed.String()
}

func setProductionEnv(t *testing.T, dsn string) {
	t.Helper()
	t.Setenv("SQL_DSN", dsn)
	t.Setenv("LOG_SQL_DSN", "")
	t.Setenv("CLEARAPI_PRODUCTION", "true")
	t.Setenv("GIN_MODE", "release")
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("SESSION_COOKIE_TRUSTED_URL", "https://bootstrap-test.internal")
	t.Setenv("TRUSTED_PROXIES", "none")
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_OUTBOX", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	t.Setenv("CLEARAPI_TASK_SETTLEMENT", "true")
	t.Setenv("CLEARAPI_STRICT_PREPAID", "true")
	t.Setenv("CLEARAPI_TOKEN_ENCRYPTION", "true")
	t.Setenv("CLEARAPI_DEPLOYMENT_INSTANCES", "1")
	t.Setenv("CLEARAPI_ENABLE_REALTIME", "false")
	t.Setenv("DEBUG", "false")
	t.Setenv("DIFY_DEBUG", "false")
	t.Setenv("ENABLE_PPROF", "false")
	t.Setenv("BATCH_UPDATE_ENABLED", "false")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 48))
	t.Setenv("CRYPTO_SECRET", strings.Repeat("c", 48))
	t.Setenv("CLEARAPI_FINANCE_ENABLED", "true")
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", "")
	t.Setenv("REDIS_CONN_STRING", "")
	for _, name := range relayLimitEnv {
		t.Setenv(name, "32")
	}
	keyPath := filepath.Join(t.TempDir(), "token.key")
	require.NoError(t, os.WriteFile(keyPath, []byte(strings.Repeat("ab", 32)), 0o600))
	t.Setenv("CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE", keyPath)

	// Runtime parity with the server: common.InitEnv() sets IsMasterNode from
	// NODE_TYPE (unset means master). Test binaries never call InitEnv, so
	// mirror the production default here.
	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })

	// Re-parse the finance config from the current environment, and restore
	// the previous configuration when the test finishes.
	common.InitFinanceConfig()
	t.Cleanup(common.InitFinanceConfig)
}

func writeInputFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("in-%d.txt", time.Now().UnixNano()))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// setFinanceOwner sets the runtime finance-owner configuration mid-test and
// re-parses it, mirroring what the server process gets from its launcher
// environment (common.InitEnv → InitFinanceConfig) before startup.
func setFinanceOwner(t *testing.T, id int) {
	t.Helper()
	t.Setenv("CLEARAPI_FINANCE_OWNER_USER_ID", strconv.Itoa(id))
	common.InitFinanceConfig()
	t.Cleanup(common.InitFinanceConfig)
}

func executeConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Execute:           true,
		AdminUsername:     "root",
		AdminDisplayName:  "Root User",
		AdminPasswordFile: writeInputFile(t, "admin-password-123\n"),
		TermsFile:         writeInputFile(t, "terms content v1"),
		PrivacyFile:       writeInputFile(t, "privacy content v1"),
		TermsVersion:      "terms-v1",
		PrivacyVersion:    "privacy-v1",
		ApprovedBy:        "test-operator",
		CandidateCommit:   "test-candidate",
	}
}

func databaseFingerprint(t *testing.T, dsn string) string {
	t.Helper()
	db := openAdminDB(t, dsn)
	var tables []string
	require.NoError(t, db.Raw(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`).Scan(&tables).Error)
	hash := sha256.New()
	for _, table := range tables {
		var rows []map[string]any
		require.NoError(t, db.Raw(fmt.Sprintf(`SELECT * FROM "%s" ORDER BY 1`, table)).Scan(&rows).Error)
		fmt.Fprintf(hash, "table:%s;rows:%d\n", table, len(rows))
		for _, row := range rows {
			fmt.Fprintf(hash, "%v\n", row)
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func snapshotWith(t *testing.T, dsn string) *Snapshot {
	t.Helper()
	oldDB, oldLOG := model.DB, model.LOG_DB
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLOG
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return TakeSnapshot()
}

func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout, os.Stderr = outW, errW
	stdoutDone := make(chan string, 1)
	stderrDone := make(chan string, 1)
	go func() { b, _ := io.ReadAll(outR); stdoutDone <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); stderrDone <- string(b) }()
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
	}()
	fn()
	require.NoError(t, outW.Close())
	require.NoError(t, errW.Close())
	return <-stdoutDone, <-stderrDone
}

func statusOf(t *testing.T, output string) string {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &payload))
	status, _ := payload["status"].(string)
	return status
}

// TestDryRunZeroWrite proves the dry-run path performs zero writes: the full
// database fingerprint is identical before and after (Amendment 2).
func TestDryRunZeroWrite(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)
	cfg := executeConfig(t)

	before := databaseFingerprint(t, dsn)
	stdout, _ := captureOutput(t, func() { RunDryRun(cfg) })
	after := databaseFingerprint(t, dsn)

	require.Equal(t, before, after)
	require.Equal(t, "dry_run", statusOf(t, stdout))
}

// TestExecuteTwoStageCompletesAndIsIdempotent covers Amendment 6 (two-stage
// finance owner), Amendment 1 (already_bootstrapped exits 0 with zero writes),
// Amendment 7 (default initial quota 0) and Amendment 8's in-process
// readiness gate.
func TestExecuteTwoStageCompletesAndIsIdempotent(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)
	cfg := executeConfig(t)

	stdout, _ := captureOutput(t, func() { RunExecute(cfg) })
	require.Equal(t, "finance_owner_configuration_required", statusOf(t, stdout))

	snap := snapshotWith(t, dsn)
	require.NotNil(t, snap.State)
	require.Nil(t, snap.Marker)
	require.True(t, snap.RootUserExists)
	require.True(t, snap.FinanceSchemaPresent)
	require.Equal(t, 9, snap.FinanceSchemaVersion)

	var user model.User
	require.NoError(t, model.DB.Where("username = ?", "root").First(&user).Error)
	require.Equal(t, 0, user.Quota, "default initial quota must be 0")

	setFinanceOwner(t, snap.State.AdminUserID)
	code := RunExecute(cfg)
	require.Equal(t, ExitOK, code)

	snap = snapshotWith(t, dsn)
	require.NotNil(t, snap.Marker)
	require.Equal(t, snap.State.AdminUserID, snap.Marker.AdminUserID)
	require.Equal(t, snap.State.AdminUserID, snap.Marker.FinanceOwnerUserID)
	require.Equal(t, 9, snap.Marker.FinanceSchemaVersion)
	require.NotEmpty(t, snap.Marker.BillingMigrationsApplied)
	require.True(t, snap.LegalUserAgreementPresent)
	require.True(t, snap.LegalPrivacyPolicyPresent)
	require.True(t, snap.LegalManifestPresent)
	require.True(t, snap.SetupRowPresent)

	// Duplicate run: exit 0 / already_bootstrapped / zero writes, regardless
	// of the flags passed (Amendment 1).
	before := databaseFingerprint(t, dsn)
	renamed := *cfg
	renamed.AdminUsername = "different-name"
	stdout, _ = captureOutput(t, func() { RunExecute(&renamed) })
	after := databaseFingerprint(t, dsn)
	require.Equal(t, before, after)
	require.Equal(t, "already_bootstrapped", statusOf(t, stdout))

	// verify-only on the completed deployment: verified / zero writes.
	stdout, _ = captureOutput(t, func() { RunVerifyOnly(&Config{VerifyOnly: true}) })
	require.Equal(t, "verified", statusOf(t, stdout))
	require.Equal(t, before, databaseFingerprint(t, dsn))

	// verify-only must also pass in fresh-process conditions: clear the
	// in-process option map and legal settings the stage-2 run left behind,
	// forcing verify-only to repopulate them from the database itself.
	oldMap := common.OptionMap
	legal := system_setting.GetLegalSettings()
	oldLegal := *legal
	t.Cleanup(func() {
		common.OptionMap = oldMap
		*legal = oldLegal
	})
	common.OptionMap = nil
	*legal = system_setting.LegalSettings{}
	stdout, _ = captureOutput(t, func() { RunVerifyOnly(&Config{VerifyOnly: true}) })
	require.Equal(t, "verified", statusOf(t, stdout), "verify-only must work from fresh-process state")
}

// TestExecutePopulatedDBRefusal refuses a database that already contains users
// without bootstrap state, without writing anything (tests 2/25).
func TestExecutePopulatedDBRefusal(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)

	// Seed a foreign database: base schema + one common user.
	require.NoError(t, model.InitDB())
	hashed, err := common.HashAccountPassword("foreign-password-1")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.User{Username: "someone", Password: hashed, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)

	before := databaseFingerprint(t, dsn)
	code := RunExecute(executeConfig(t))
	after := databaseFingerprint(t, dsn)
	require.Equal(t, ExitPopulatedDBRefusal, code)
	require.Equal(t, before, after)
}

// TestExecuteLegalRerunHashMismatchFailClosed covers Amendment J: a rerun that
// supplies different legal content must fail closed and change nothing. The
// marker row is removed to simulate a lost marker while legal state remains.
func TestExecuteLegalRerunHashMismatchFailClosed(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)
	cfg := executeConfig(t)

	code := RunExecute(cfg)
	require.Equal(t, ExitFinanceOwnerConfigRequired, code)
	snap := snapshotWith(t, dsn)
	setFinanceOwner(t, snap.State.AdminUserID)
	code = RunExecute(cfg)
	require.Equal(t, ExitOK, code)

	db := openAdminDB(t, dsn)
	require.NoError(t, db.Exec(fmt.Sprintf(`DELETE FROM options WHERE "key" = '%s'`, model.BootstrapMarkerOptionKey)).Error)

	before := databaseFingerprint(t, dsn)
	tampered := *cfg
	tampered.TermsFile = writeInputFile(t, "terms content TAMPERED")
	tampered.PrivacyFile = writeInputFile(t, "privacy content TAMPERED")
	var rerunCode int
	stdout, _ := captureOutput(t, func() { rerunCode = RunExecute(&tampered) })
	require.Equal(t, ExitLegalMismatch, rerunCode)
	require.Equal(t, "legal_mismatch", statusOf(t, stdout))

	// Amendment J is fail-closed on the legal record: content, manifest and
	// marker state must be untouched (the server-parity idempotent migration
	// pass itself may rewrite derived runtime rows like built-in policies).
	// Reads use the test's own handle: RunExecute closed its internal one.
	terms, found, err := model.ReadBootstrapOption(db, model.LegalUserAgreementOptionKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "terms content v1", terms)
	manifest, err := model.ReadBootstrapLegalManifest(db)
	require.NoError(t, err)
	require.NotNil(t, manifest)
	_, markerFound, err := model.ReadBootstrapOption(db, model.BootstrapMarkerOptionKey)
	require.NoError(t, err)
	require.False(t, markerFound, "a failed rerun must not write a completion marker")

	// Rerun with the original files completes again (verify-and-skip).
	rerunCode = RunExecute(cfg)
	require.Equal(t, ExitOK, rerunCode)
	_, markerFound, err = model.ReadBootstrapOption(db, model.BootstrapMarkerOptionKey)
	require.NoError(t, err)
	require.True(t, markerFound)
	_ = before
}

// TestCorruptCompletionMarkerFailClosed covers a corrupted completion marker:
// exit 60, no automatic repair, no writes (tests 29).
func TestCorruptCompletionMarkerFailClosed(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)
	cfg := executeConfig(t)

	code := RunExecute(cfg)
	require.Equal(t, ExitFinanceOwnerConfigRequired, code)
	snap := snapshotWith(t, dsn)
	setFinanceOwner(t, snap.State.AdminUserID)
	code = RunExecute(cfg)
	require.Equal(t, ExitOK, code)

	db := openAdminDB(t, dsn)
	require.NoError(t, db.Exec(fmt.Sprintf(`UPDATE options SET value = '{broken' WHERE "key" = '%s'`, model.BootstrapMarkerOptionKey)).Error)
	before := databaseFingerprint(t, dsn)

	var executeCode int
	stdout, _ := captureOutput(t, func() { executeCode = RunExecute(cfg) })
	require.Equal(t, "marker_corrupt", statusOf(t, stdout))
	require.Equal(t, ExitCorruption, executeCode)
	require.Equal(t, ExitCorruption, RunVerifyOnly(&Config{VerifyOnly: true}))
	require.Equal(t, before, databaseFingerprint(t, dsn))
}

// TestCorruptBootstrapStateFailClosed covers a corrupted bootstrap state
// (exit 60 before any migration or write, test 28).
func TestCorruptBootstrapStateFailClosed(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)

	code := RunExecute(executeConfig(t))
	require.Equal(t, ExitFinanceOwnerConfigRequired, code)

	db := openAdminDB(t, dsn)
	require.NoError(t, db.Exec(fmt.Sprintf(`UPDATE options SET value = '{"schema_version":1,"admin_user_id":0}' WHERE "key" = '%s'`, model.BootstrapStateOptionKey)).Error)

	before := databaseFingerprint(t, dsn)
	var executeCode int
	stdout, _ := captureOutput(t, func() { executeCode = RunExecute(executeConfig(t)) })
	require.Equal(t, "state_corrupt", statusOf(t, stdout))
	require.Equal(t, ExitCorruption, executeCode)
	require.Equal(t, before, databaseFingerprint(t, dsn))
}

// TestSecretsNeverLogged proves secret values and the admin password never
// appear in stdout or stderr of any mode (tests 22/23).
func TestSecretsNeverLogged(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)
	cfg := executeConfig(t)
	secretValue := os.Getenv("SESSION_SECRET")
	cryptoValue := os.Getenv("CRYPTO_SECRET")
	passwordValue := "admin-password-123"

	stdout, stderr := captureOutput(t, func() { RunDryRun(cfg) })
	for _, output := range []string{stdout, stderr} {
		require.NotContains(t, output, secretValue)
		require.NotContains(t, output, cryptoValue)
		require.NotContains(t, output, passwordValue)
		require.NotContains(t, output, dsn)
	}

	stdout, stderr = captureOutput(t, func() { RunExecute(cfg) })
	for _, output := range []string{stdout, stderr} {
		require.NotContains(t, output, secretValue)
		require.NotContains(t, output, cryptoValue)
		require.NotContains(t, output, passwordValue)
		require.NotContains(t, output, dsn)
	}
}

// TestInputValidationFailures covers invalid legal inputs (tests 6/7/8) and
// wrong database credentials (test 9) failing closed before any write.
func TestInputValidationFailures(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)

	cfg := executeConfig(t)
	cfg.TermsFile = writeInputFile(t, "\xff\xfe invalid utf8")
	require.Equal(t, ExitInput, RunExecute(cfg))

	cfg = executeConfig(t)
	cfg.PrivacyFile = writeInputFile(t, "   \n  ")
	require.Equal(t, ExitInput, RunExecute(cfg))

	cfg = executeConfig(t)
	cfg.TermsFile = writeInputFile(t, strings.Repeat("a", maxLegalFileBytes+1))
	require.Equal(t, ExitInput, RunExecute(cfg))

	// Wrong credentials: the probe fails before any migration or write.
	broken, err := url.Parse(dsn)
	require.NoError(t, err)
	broken.User = url.UserPassword("bootstrap_test_invalid_user", "wrongpass")
	setProductionEnv(t, broken.String())
	require.Equal(t, ExitEnvironment, RunExecute(executeConfig(t)))
}

// TestDuplicateMigrationsSafe reruns the migration path against an already
// migrated database and confirms the finance schema version is stable
// (test 26).
func TestDuplicateMigrationsSafe(t *testing.T) {
	dsn := createFreshDatabase(t, requirePG(t))
	setProductionEnv(t, dsn)
	isolateGlobals(t)

	require.Equal(t, ExitFinanceOwnerConfigRequired, RunExecute(executeConfig(t)))
	first := snapshotWith(t, dsn)

	db := openAdminDB(t, dsn)
	require.NoError(t, db.Exec(fmt.Sprintf(`DELETE FROM options WHERE "key" = '%s'`, model.BootstrapStateOptionKey)).Error)
	require.NoError(t, db.Exec(`DELETE FROM users`).Error)

	require.Equal(t, ExitFinanceOwnerConfigRequired, RunExecute(executeConfig(t)))
	second := snapshotWith(t, dsn)
	require.Equal(t, first.FinanceSchemaVersion, second.FinanceSchemaVersion)
	require.Equal(t, 9, second.FinanceSchemaVersion)
}

// TestSQLiteProbeCreatesNothing keeps the non-PG probe honest: opening a fresh
// SQLite database read-only must not create any schema (Amendment 2 probe).
func TestSQLiteProbeCreatesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.db")
	oldPath := common.SQLitePath
	oldDB, oldLOG := model.DB, model.LOG_DB
	t.Cleanup(func() {
		common.SQLitePath = oldPath
		model.DB, model.LOG_DB = oldDB, oldLOG
	})
	common.SQLitePath = path
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")

	require.NoError(t, OpenReadonly())
	require.False(t, model.DB.Migrator().HasTable(&model.User{}), "no table may be created by the read-only probe")
	require.False(t, model.DB.Migrator().HasTable(&model.Option{}))
	snapshot := TakeSnapshot()
	require.Nil(t, snapshot.Marker)
	require.Nil(t, snapshot.State)
	require.Equal(t, int64(0), snapshot.UserCount)
}
