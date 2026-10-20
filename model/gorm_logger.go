package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/migrationproof"
	sqlitedriver "github.com/glebarez/go-sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	defaultSlowThresholdMs = 200
	maxSlowThresholdMs     = 60 * 60 * 1000
)

func newGormConfig(prepareStmt bool) *gorm.Config {
	output := newGormLogger(os.Stdout)
	if migrationproof.Enabled() {
		output = migrationProofLogger{Interface: output, fail: migrationproof.Fail}
	}
	return &gorm.Config{
		PrepareStmt: prepareStmt,
		Logger:      output,
	}
}

// database/sql can reject a query before reaching the driver (closed pool or
// canceled context). RowQuery does not assign that deferred error to db.Error.
// Driver rows instrumentation covers the later Next/Close errors separately.
func observeMigrationQueries(db *gorm.DB) error {
	if !migrationproof.Enabled() {
		return nil
	}
	return db.Callback().Row().After("gorm:row").Register("clearapi:migration_proof_row_error", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*sql.Row); ok && row.Err() != nil {
			migrationproof.Fail()
		}
	})
}

// Observe errors delivered to GORM Trace independently of log level or writer
// success. Deferred driver reads are observed by the proof SQL connector.
type migrationProofLogger struct {
	logger.Interface
	fail func()
}

func (l migrationProofLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.Interface = l.Interface.LogMode(level)
	return l
}

func (l migrationProofLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.fail()
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

func (l migrationProofLogger) ParamsFilter(ctx context.Context, sql string, params ...any) (string, []any) {
	if filter, ok := l.Interface.(interface {
		ParamsFilter(context.Context, string, ...any) (string, []any)
	}); ok {
		return filter.ParamsFilter(ctx, sql, params...)
	}
	return sql, params
}

func newGormLogger(w io.Writer) logger.Interface {
	slowThresholdMs := common.GetEnvOrDefault("SQL_SLOW_THRESHOLD_MS", defaultSlowThresholdMs)
	if slowThresholdMs < 0 || slowThresholdMs > maxSlowThresholdMs {
		common.SysError(fmt.Sprintf("invalid SQL_SLOW_THRESHOLD_MS %d (allowed 0-%d, 0 disables slow query log), using default %d", slowThresholdMs, maxSlowThresholdMs, defaultSlowThresholdMs))
		slowThresholdMs = defaultSlowThresholdMs
	}
	// 在 Writer 层脱敏而非包装 logger.Interface:后者会让 gorm 的 FileWithLineNum
	// 把所有 SQL 日志的调用点归因到包装层自身,且需转发 ParamsFilter 类型断言。
	return logger.New(&sanitizedLogWriter{delegate: log.New(w, "\r\n", log.LstdFlags)}, logger.Config{
		SlowThreshold:             time.Duration(slowThresholdMs) * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      !common.DebugEnabled,
		Colorful:                  true,
	})
}

// ParameterizedQueries 只过滤 SQL 字符串,驱动错误消息(如 MySQL 1062)同样会
// 内联数据值,在这里收敛为错误码;DEBUG=true 保留原文。
type sanitizedLogWriter struct {
	delegate *log.Logger
}

func (s *sanitizedLogWriter) Printf(format string, args ...any) {
	if !common.DebugEnabled {
		for i, arg := range args {
			if err, ok := arg.(error); ok {
				args[i] = sanitizeDBError(err)
			}
		}
	}
	s.delegate.Printf(format, args...)
}

// 只收敛数据库服务端生成的驱动错误(消息可能内联数据值);网络/上下文等
// 其它错误不含查询数据,原样保留以便排障。
func sanitizeDBError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return fmt.Errorf("mysql error %d", mysqlErr.Number)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 08P01 是 PgBouncer 对同连接重名 Parse 的 FATAL,42P05 是原生 PostgreSQL 的
		// duplicate_prepared_statement;都指向预处理语句与事务池代理不兼容。
		if pgErr.Code == "08P01" || pgErr.Code == "42P05" {
			return fmt.Errorf("postgres error SQLSTATE %s: prepared statement conflict with a transaction-pooling proxy (PgBouncer/Neon/Supabase); other clients sharing this database must disable prepared statements, or upgrade PgBouncer to >=1.21 with max_prepared_statements enabled", pgErr.Code)
		}
		return fmt.Errorf("postgres error SQLSTATE %s", pgErr.Code)
	}
	var chErr *proto.Exception
	if errors.As(err, &chErr) {
		return fmt.Errorf("clickhouse error %d", chErr.Code)
	}
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) {
		return fmt.Errorf("sqlite error %d", sqliteErr.Code())
	}
	return err
}
