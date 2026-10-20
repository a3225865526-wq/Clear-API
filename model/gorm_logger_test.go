package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/migrationproof"
	"github.com/glebarez/sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 保护契约:数据库驱动错误消息可能内联数据值,非 DEBUG 下日志只保留错误码。
func TestSanitizeDBErrorStripsDriverMessage(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		want   string
		leaked string
	}{
		{
			name:   "mysql duplicate entry",
			err:    &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'secret-value' for key 'users.idx'"},
			want:   "mysql error 1062",
			leaked: "secret-value",
		},
		{
			name:   "postgres unique violation",
			err:    &pgconn.PgError{Code: "23505", Message: "duplicate key value", Detail: "Key (k)=(secret-value) already exists."},
			want:   "postgres error SQLSTATE 23505",
			leaked: "secret-value",
		},
		{
			name:   "postgres pooler prepared statement conflict gets remediation hint",
			err:    &pgconn.PgError{Severity: "FATAL", Code: "08P01", Message: "prepared statement name is already in use: stmt_secret-value"},
			want:   "postgres error SQLSTATE 08P01: prepared statement conflict with a transaction-pooling proxy (PgBouncer/Neon/Supabase); other clients sharing this database must disable prepared statements, or upgrade PgBouncer to >=1.21 with max_prepared_statements enabled",
			leaked: "secret-value",
		},
		{
			name:   "clickhouse exception",
			err:    &proto.Exception{Code: 241, Message: "Memory limit exceeded while processing 'secret-value'"},
			want:   "clickhouse error 241",
			leaked: "secret-value",
		},
		{
			name:   "wrapped driver error",
			err:    fmt.Errorf("exec failed: %w", &mysql.MySQLError{Number: 1064, Message: "syntax error near 'secret-value'"}),
			want:   "mysql error 1064",
			leaked: "secret-value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeDBError(tc.err)
			require.Error(t, got)
			assert.Equal(t, tc.want, got.Error())
			assert.NotContains(t, got.Error(), tc.leaked)
		})
	}
}

func TestMigrationProofObservesErrorsEvenWithSilentLogger(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proof persistence targets the Linux candidate")
	}
	failed := false
	output := migrationProofLogger{Interface: newGormLogger(&bytes.Buffer{}), fail: func() { failed = true }}
	quiet := output.LogMode(logger.Silent)
	query := func() (string, int64) { return "", 0 }
	quiet.Trace(context.Background(), time.Now(), query, gorm.ErrRecordNotFound)
	assert.False(t, failed, "an expected absent row is not a migration failure")
	quiet.Trace(context.Background(), time.Now(), query, errors.New("synthetic database error"))
	assert.True(t, failed, "actual errors are observed without parsing or writing logs")

	failed = false
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: quiet})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	proofPath := filepath.Join(t.TempDir(), "proof.json")
	require.NoError(t, migrationproof.Begin(true, strings.Repeat("a", 64), proofPath))
	t.Cleanup(migrationproof.End)
	require.NoError(t, observeMigrationQueries(db))
	// A healthy Row no longer fails merely because it uses deferred Scan.
	var value int
	require.NoError(t, db.Raw("SELECT 1").Row().Scan(&value))
	migrationproof.Complete("main_database")
	migrationproof.Complete("retired_options")
	migrationproof.Complete("log_database")
	require.NoError(t, migrationproof.Publish())
	assert.FileExists(t, proofPath)
	migrationproof.End()
	proofPath = filepath.Join(t.TempDir(), "closed-pool-proof.json")
	require.NoError(t, migrationproof.Begin(true, strings.Repeat("b", 64), proofPath))
	require.NoError(t, sqlDB.Close())
	// HasTable may swallow an error occurring after Trace. Refuse an attestation
	// for unobservable row results rather than infer success from the log hook.
	assert.False(t, db.Migrator().HasTable("synthetic_missing_table"))
	migrationproof.Complete("main_database")
	migrationproof.Complete("retired_options")
	migrationproof.Complete("log_database")
	require.Error(t, migrationproof.Publish())
	assert.NoFileExists(t, proofPath)
}

func TestSanitizeDBErrorSQLiteDriver(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	execErr := db.Exec("INSERT INTO missing_table (k) VALUES (?)", "secret-value").Error
	require.Error(t, execErr)

	got := sanitizeDBError(execErr)
	assert.Regexp(t, `^sqlite error \d+$`, got.Error())
	assert.NotContains(t, got.Error(), "secret-value")
}

func TestSanitizeDBErrorKeepsNonDriverErrors(t *testing.T) {
	err := fmt.Errorf("dial tcp 127.0.0.1:3306: connect: connection refused")
	assert.Equal(t, err, sanitizeDBError(err))
}

// 保护契约:经 gorm 真实链路,错误日志同时满足 SQL 参数化、驱动错误脱敏、
// 调用点归因到业务代码;DEBUG=true 恢复参数值与错误原文。
func TestGormLoggerEndToEndSanitizedOutput(t *testing.T) {
	previousDebug := common.DebugEnabled
	t.Cleanup(func() { common.DebugEnabled = previousDebug })

	execQuery := func() string {
		var buf bytes.Buffer
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: newGormLogger(&buf)})
		require.NoError(t, err)
		db.Exec("SELECT * FROM missing_table WHERE k = ?", "secret-value")
		return buf.String()
	}

	common.DebugEnabled = false
	out := execQuery()
	assert.Contains(t, out, "k = ?")
	assert.NotContains(t, out, "secret-value")
	assert.Contains(t, out, "sqlite error")
	assert.Contains(t, out, "gorm_logger_test.go")

	common.DebugEnabled = true
	debugOut := execQuery()
	assert.Contains(t, debugOut, "secret-value")
	assert.Contains(t, debugOut, "no such table")
}

// The CI service databases are dedicated to this test, never the commercial
// fixtures or any production DSN. Missing service inputs are errors when a
// non-SQLite dialect was explicitly requested.
func TestMigrationProofStartupDatabase(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("candidate proof targets Linux")
	}
	dialect := os.Getenv("TEST_PG03_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	mainDSN, logDSN := "local", ""
	switch dialect {
	case "sqlite":
	case "mysql":
		mainDSN, logDSN = os.Getenv("TEST_PG03_MYSQL_DSN"), os.Getenv("TEST_PG03_MYSQL_LOG_DSN")
		for index, dsn := range []string{mainDSN, logDSN} {
			config, err := mysql.ParseDSN(dsn)
			require.NoError(t, err)
			require.Equal(t, "127.0.0.1:3306", config.Addr)
			require.Equal(t, []string{"pg03_proof_main", "pg03_proof_log"}[index], config.DBName)
		}
	case "postgres":
		mainDSN, logDSN = os.Getenv("TEST_PG03_POSTGRES_DSN"), os.Getenv("TEST_PG03_POSTGRES_LOG_DSN")
		for index, dsn := range []string{mainDSN, logDSN} {
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			require.Equal(t, "127.0.0.1:5432", parsed.Host)
			require.Equal(t, []string{"/pg03_proof_main", "/pg03_proof_log"}[index], parsed.Path)
		}
	default:
		t.Fatal("unsupported proof test dialect")
	}
	oldDB, oldLog, oldPath, oldMaster := DB, LOG_DB, common.SQLitePath, common.IsMasterNode
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		migrationproof.End()
		DB, LOG_DB, common.SQLitePath, common.IsMasterNode = oldDB, oldLog, oldPath, oldMaster
		common.SetMainDatabaseType(oldMainType)
		common.SetLogDatabaseType(oldLogType)
		initCol()
	})
	common.SQLitePath = filepath.Join(t.TempDir(), "startup.db")
	common.IsMasterNode = true
	t.Setenv("SQL_DSN", mainDSN)
	t.Setenv("SKIP_64BIT_QUOTA_SCHEMA_CHECK", "false")
	profiles := []string{"shared"}
	if dialect != "sqlite" {
		profiles = append(profiles, "separate")
	}
	for _, profile := range profiles {
		t.Setenv("LOG_SQL_DSN", "")
		if profile == "separate" {
			t.Setenv("LOG_SQL_DSN", logDSN)
		}
		for pass := range 2 {
			path := filepath.Join(t.TempDir(), "proof.json")
			require.NoError(t, migrationproof.Begin(true, strings.Repeat("a", 64), path))
			require.NoError(t, InitDB())
			migrationproof.Complete("main_database")
			require.NoError(t, MigrateRetiredFrontendOptions())
			migrationproof.Complete("retired_options")
			require.NoError(t, InitLogDB())
			migrationproof.Complete("log_database")
			var version string
			versionSQL := "SELECT version()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			require.NoError(t, DB.Raw(versionSQL).Row().Scan(&version))
			t.Logf("engine=%s version=%s profile=%s pass=%d", dialect, version, profile, pass+1)
			if profile == "shared" && pass == 0 {
				require.NoError(t, DB.Create(&Option{Key: "pg03.synthetic.preserve", Value: "retained"}).Error)
			} else {
				var row Option
				require.NoError(t, DB.Where(&Option{Key: "pg03.synthetic.preserve"}).First(&row).Error)
				assert.Equal(t, "retained", row.Value)
			}
			require.NoError(t, migrationproof.Publish(), "real migrations and row reads must produce completion")
			assert.FileExists(t, path)
			migrationproof.End()
			if pass == 1 {
				failurePath := filepath.Join(t.TempDir(), "failure.json")
				require.NoError(t, migrationproof.Begin(true, strings.Repeat("b", 64), failurePath))
				pool, err := LOG_DB.DB()
				require.NoError(t, err)
				require.NoError(t, pool.Close())
				assert.False(t, LOG_DB.Migrator().HasTable(&AuditLog{}))
				for _, phase := range []string{"main_database", "retired_options", "log_database"} {
					migrationproof.Complete(phase)
				}
				require.Error(t, migrationproof.Publish(), "a swallowed log database failure vetoes completion")
				assert.NoFileExists(t, failurePath)
				migrationproof.End()
			}
			require.NoError(t, CloseDB())
		}
	}
}
