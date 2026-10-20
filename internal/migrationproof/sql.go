package migrationproof

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
)

// SQL observes the driver boundary, including errors raised only by Rows.Next
// or transaction completion. It does not parse SQL, values or application logs.
func SQL(connector driver.Connector) *sql.DB {
	return sql.OpenDB(proofConnector{connector})
}

// Open preserves the registered driver's connector and DSN parsing. The probe
// pool is never pinged and never opens a network connection.
func Open(name, dsn string) (*sql.DB, error) {
	probe, err := sql.Open(name, dsn)
	if err != nil {
		observe(err)
		return nil, err
	}
	base := probe.Driver()
	if err := probe.Close(); err != nil {
		observe(err)
		return nil, err
	}
	if contextual, ok := base.(driver.DriverContext); ok {
		connector, err := contextual.OpenConnector(dsn)
		observe(err)
		if err != nil {
			return nil, err
		}
		return SQL(connector), nil
	}
	return SQL(dsnConnector{base: base, dsn: dsn}), nil
}

type dsnConnector struct {
	base driver.Driver
	dsn  string
}

func (c dsnConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.base.Open(c.dsn)
}

func (c dsnConnector) Driver() driver.Driver { return c.base }

func observe(err error) {
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, driver.ErrSkip) && !errors.Is(err, driver.ErrRemoveArgument) {
		Fail()
	}
}

type proofConnector struct{ driver.Connector }

func (c proofConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	observe(err)
	if err != nil {
		return nil, err
	}
	return proofConn{conn}, nil
}

type proofConn struct{ driver.Conn }

func (c proofConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	observe(err)
	if err != nil {
		return nil, err
	}
	return proofStmt{Stmt: stmt, conn: c}, nil
}

func (c proofConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err := preparer.PrepareContext(ctx, query)
		observe(err)
		if err != nil {
			return nil, err
		}
		return proofStmt{Stmt: stmt, conn: c}, nil
	}
	if err := ctx.Err(); err != nil {
		observe(err)
		return nil, err
	}
	return c.Prepare(query)
}

func (c proofConn) Close() error {
	err := c.Conn.Close()
	observe(err)
	return err
}

func (c proofConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	observe(err)
	if err != nil {
		return nil, err
	}
	return proofTx{tx}, nil
}

func (c proofConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		tx, err := beginner.BeginTx(ctx, opts)
		observe(err)
		if err != nil {
			return nil, err
		}
		return proofTx{tx}, nil
	}
	if err := ctx.Err(); err != nil {
		observe(err)
		return nil, err
	}
	if opts.Isolation != 0 || opts.ReadOnly {
		Fail()
		return nil, errors.New("migration proof driver does not support transaction options")
	}
	return c.Begin()
}

func (c proofConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip // database/sql falls back to Prepare.
	}
	result, err := execer.ExecContext(ctx, query, args)
	observe(err)
	if err != nil {
		return nil, err
	}
	return proofResult{result}, nil
}

func (c proofConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := queryer.QueryContext(ctx, query, args)
	observe(err)
	if err != nil {
		return nil, err
	}
	return proofRows{Rows: rows, ctx: ctx}, nil
}

func (c proofConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		err := checker.CheckNamedValue(value)
		observe(err)
		return err
	}
	return driver.ErrSkip
}

func (c proofConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		err := pinger.Ping(ctx)
		observe(err)
		return err
	}
	return nil
}

func (c proofConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		err := resetter.ResetSession(ctx)
		observe(err)
		return err
	}
	return nil
}

func (c proofConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

type proofStmt struct {
	driver.Stmt
	conn proofConn
}

func (s proofStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := s.Stmt.(driver.StmtExecContext); ok {
		result, err := execer.ExecContext(ctx, args)
		observe(err)
		if err != nil {
			return nil, err
		}
		return proofResult{result}, nil
	}
	values, err := statementValues(ctx, args)
	if err != nil {
		observe(err)
		return nil, err
	}
	return s.Exec(values)
}

func (s proofStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := s.Stmt.(driver.StmtQueryContext); ok {
		rows, err := queryer.QueryContext(ctx, args)
		observe(err)
		if err != nil {
			return nil, err
		}
		return proofRows{Rows: rows, ctx: ctx}, nil
	}
	values, err := statementValues(ctx, args)
	if err != nil {
		observe(err)
		return nil, err
	}
	rows, err := s.Query(values)
	if err != nil {
		return nil, err
	}
	return proofRows{Rows: rows, ctx: ctx}, nil
}

func statementValues(ctx context.Context, args []driver.NamedValue) ([]driver.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			return nil, errors.New("driver does not support named parameters")
		}
		values[i] = arg.Value
	}
	return values, nil
}

func (s proofStmt) ColumnConverter(index int) driver.ValueConverter {
	if converter, ok := s.Stmt.(driver.ColumnConverter); ok {
		return converter.ColumnConverter(index)
	}
	return driver.DefaultParameterConverter
}

func (s proofStmt) Close() error {
	err := s.Stmt.Close()
	observe(err)
	return err
}

func (s proofStmt) Exec(args []driver.Value) (driver.Result, error) {
	result, err := s.Stmt.Exec(args)
	observe(err)
	if err != nil {
		return nil, err
	}
	return proofResult{result}, nil
}

func (s proofStmt) Query(args []driver.Value) (driver.Rows, error) {
	rows, err := s.Stmt.Query(args)
	observe(err)
	if err != nil {
		return nil, err
	}
	return proofRows{Rows: rows, ctx: context.Background()}, nil
}

func (s proofStmt) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := s.Stmt.(driver.NamedValueChecker); ok {
		err := checker.CheckNamedValue(value)
		observe(err)
		return err
	}
	return s.conn.CheckNamedValue(value)
}

type proofTx struct{ driver.Tx }

func (t proofTx) Commit() error {
	err := t.Tx.Commit()
	observe(err)
	return err
}

func (t proofTx) Rollback() error {
	err := t.Tx.Rollback()
	observe(err)
	return err
}

type proofResult struct{ driver.Result }

func (r proofResult) LastInsertId() (int64, error) {
	value, err := r.Result.LastInsertId()
	observe(err)
	return value, err
}

func (r proofResult) RowsAffected() (int64, error) {
	value, err := r.Result.RowsAffected()
	observe(err)
	return value, err
}

type proofRows struct {
	driver.Rows
	ctx context.Context
}

func (r proofRows) Close() error {
	observe(r.ctx.Err())
	err := r.Rows.Close()
	observe(err)
	return err
}

func (r proofRows) Next(values []driver.Value) error {
	if err := r.ctx.Err(); err != nil {
		observe(err)
		return err
	}
	err := r.Rows.Next(values)
	observe(err)
	if canceled := r.ctx.Err(); canceled != nil {
		observe(canceled)
		return canceled
	}
	return err
}

func (r proofRows) HasNextResultSet() bool {
	rows, ok := r.Rows.(driver.RowsNextResultSet)
	return ok && rows.HasNextResultSet()
}

func (r proofRows) NextResultSet() error {
	if rows, ok := r.Rows.(driver.RowsNextResultSet); ok {
		err := rows.NextResultSet()
		observe(err)
		return err
	}
	return io.EOF
}

func (r proofRows) ColumnTypeDatabaseTypeName(index int) string {
	if rows, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return rows.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (r proofRows) ColumnTypeLength(index int) (int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeLength); ok {
		return rows.ColumnTypeLength(index)
	}
	return 0, false
}

func (r proofRows) ColumnTypeNullable(index int) (bool, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeNullable); ok {
		return rows.ColumnTypeNullable(index)
	}
	return false, false
}

func (r proofRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return rows.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

func (r proofRows) ColumnTypeScanType(index int) reflect.Type {
	if rows, ok := r.Rows.(driver.RowsColumnTypeScanType); ok {
		return rows.ColumnTypeScanType(index)
	}
	return reflect.TypeFor[any]()
}
