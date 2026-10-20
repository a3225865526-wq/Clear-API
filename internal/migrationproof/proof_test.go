package migrationproof

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationProofCompletion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("receipt directory fsync targets the Linux candidate")
	}
	for _, scenario := range []string{"success", "incomplete", "wrong_order", "best_effort_error", "publish_failure", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			active = nil
			t.Cleanup(func() { active = nil })
			path := filepath.Join(t.TempDir(), "proof.json")
			challenge := strings.Repeat("a", 64)
			if scenario == "stale" {
				require.NoError(t, os.WriteFile(path, []byte("previous evidence"), 0600))
				require.Error(t, Begin(true, challenge, path))
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "previous evidence", string(data))
				return
			}
			require.NoError(t, Begin(true, challenge, path))
			Complete("main_database")
			if scenario != "incomplete" {
				if scenario == "wrong_order" {
					Complete("log_database")
				}
				Complete("retired_options")
				Complete("log_database")
			}
			if scenario == "best_effort_error" {
				Fail()
			}
			if scenario == "publish_failure" {
				require.NoError(t, os.Mkdir(path, 0700))
			}
			err := Publish()
			if scenario != "success" {
				require.Error(t, err)
				if scenario != "publish_failure" {
					_, statErr := os.Lstat(path)
					assert.True(t, os.IsNotExist(statErr))
				}
				return
			}
			require.NoError(t, err)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var receipt map[string]any
			require.NoError(t, common.Unmarshal(data, &receipt))
			assert.Equal(t, "success", receipt["status"])
			assert.Equal(t, challenge, receipt["challenge"])
			assert.Equal(t, []any{"main_database", "retired_options", "log_database"}, receipt["phases"])
			executable, err := os.Executable()
			require.NoError(t, err)
			binary, err := os.ReadFile(executable)
			require.NoError(t, err)
			hash := sha256.Sum256(binary)
			assert.Equal(t, hex.EncodeToString(hash[:]), receipt["binary_sha256"])
			require.Error(t, Publish(), "do not overwrite an existing completion")
			active = nil // simulate a new process in the same filesystem
			require.Error(t, Begin(true, challenge, path))
		})
	}
}

type proofFixture struct {
	nextErr   error
	closeErr  error
	commitErr error
}

type fixtureDriver struct{ fixture *proofFixture }

func (d fixtureDriver) Open(string) (driver.Conn, error) { return fixtureConn{d.fixture}, nil }

type fixtureConnector struct{ fixture *proofFixture }

func (c fixtureConnector) Connect(context.Context) (driver.Conn, error) {
	return fixtureConn{c.fixture}, nil
}
func (c fixtureConnector) Driver() driver.Driver { return fixtureDriver{c.fixture} }

type fixtureConn struct{ fixture *proofFixture }

func (c fixtureConn) Prepare(string) (driver.Stmt, error) { return fixtureStmt{c.fixture}, nil }
func (c fixtureConn) Close() error                        { return nil }
func (c fixtureConn) Begin() (driver.Tx, error)           { return fixtureTx{c.fixture}, nil }

type fixtureTx struct{ fixture *proofFixture }

func (t fixtureTx) Commit() error   { return t.fixture.commitErr }
func (t fixtureTx) Rollback() error { return nil }

type fixtureStmt struct{ fixture *proofFixture }

func (s fixtureStmt) Close() error                               { return nil }
func (s fixtureStmt) NumInput() int                              { return -1 }
func (s fixtureStmt) Exec([]driver.Value) (driver.Result, error) { return driver.RowsAffected(1), nil }
func (s fixtureStmt) Query([]driver.Value) (driver.Rows, error) {
	return &fixtureRows{fixture: s.fixture}, nil
}

type fixtureRows struct {
	fixture *proofFixture
	yielded bool
}

func (r *fixtureRows) Columns() []string { return []string{"value"} }
func (r *fixtureRows) Close() error      { return r.fixture.closeErr }
func (r *fixtureRows) Next(values []driver.Value) error {
	if r.fixture.nextErr != nil {
		return r.fixture.nextErr
	}
	if r.yielded {
		return io.EOF
	}
	r.yielded = true
	values[0] = int64(1)
	return nil
}

// Error injection happens at real database/sql driver boundaries: the query
// itself returns success, then a subsequent read/close or commit fails.
func TestMigrationProofDeferredSQLFailures(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proof persistence targets Linux")
	}
	for _, scenario := range []string{"healthy", "next", "rows_close", "commit", "cancel_after_query"} {
		t.Run(scenario, func(t *testing.T) {
			active = nil
			t.Cleanup(End)
			path := filepath.Join(t.TempDir(), "proof.json")
			require.NoError(t, Begin(true, strings.Repeat("a", 64), path))
			fixture := &proofFixture{}
			sentinel := errors.New("synthetic deferred driver failure")
			switch scenario {
			case "next":
				fixture.nextErr = sentinel
			case "rows_close":
				fixture.closeErr = sentinel
			case "commit":
				fixture.commitErr = sentinel
			}
			db := SQL(fixtureConnector{fixture})
			t.Cleanup(func() { assert.NoError(t, db.Close()) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rows, err := db.QueryContext(ctx, "synthetic query")
			require.NoError(t, err)
			if scenario == "cancel_after_query" {
				cancel()
			}
			for rows.Next() {
				var value int
				require.NoError(t, rows.Scan(&value))
				assert.Equal(t, 1, value)
			}
			readErr := rows.Err()
			closeErr := rows.Close()
			if scenario == "next" || scenario == "rows_close" || scenario == "cancel_after_query" {
				require.Error(t, readErr)
			} else {
				require.NoError(t, readErr)
				require.NoError(t, closeErr)
			}
			tx, err := db.Begin()
			require.NoError(t, err)
			err = tx.Commit()
			if scenario == "commit" {
				require.ErrorIs(t, err, sentinel)
			} else {
				require.NoError(t, err)
			}
			for _, phase := range phases {
				Complete(phase)
			}
			err = Publish()
			if scenario == "healthy" {
				require.NoError(t, err)
				assert.FileExists(t, path)
			} else {
				require.Error(t, err)
				assert.NoFileExists(t, path)
			}
		})
	}
}

func TestMigrationProofOptInAndMaster(t *testing.T) {
	active = nil
	t.Cleanup(func() { active = nil })
	path := filepath.Join(t.TempDir(), "proof.json")
	require.NoError(t, Begin(true, "", path))
	Complete("unexpected")
	Fail()
	require.NoError(t, Publish())
	assert.False(t, Enabled())
	_, err := os.Lstat(path + ".started")
	assert.True(t, os.IsNotExist(err))
	require.Error(t, Begin(false, strings.Repeat("a", 64), path))
	require.Error(t, Begin(true, "invalid", path))
}
