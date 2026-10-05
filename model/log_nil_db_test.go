package model

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// PG-09 reproduction: billing has already committed by the time the consume
// log is written. A logging failure (nil LOG_DB) must surface as an
// observable error, never as a panic inside the request goroutine.
func newConsumeLogTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("username", "pg09-user")
	c.Set(common.RequestIdKey, "pg09-request-id")
	return c
}

func setupNilLogDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pg09.db")), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, nil
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestCreateLogWithoutLogDBReturnsErrorNotPanic(t *testing.T) {
	setupNilLogDB(t)
	log := &Log{Type: LogTypeConsume, UserId: 1, ModelName: "gpt-x", Quota: 7}
	require.NotPanics(t, func() {
		err := createLog(log)
		require.Error(t, err)
	})
}

func TestRecordConsumeLogToleratesMissingLogDB(t *testing.T) {
	setupNilLogDB(t)
	previousEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() { common.LogConsumeEnabled = previousEnabled })

	c := newConsumeLogTestContext(t)
	require.NotPanics(t, func() {
		RecordConsumeLog(c, 1, RecordConsumeLogParams{
			ModelName: "gpt-x",
			Quota:     7,
			Content:   "pg-09 verification",
			Other:     NewLogOther(),
		})
	})
}

func TestRecordErrorLogToleratesMissingLogDB(t *testing.T) {
	setupNilLogDB(t)
	c := newConsumeLogTestContext(t)
	require.NotPanics(t, func() {
		RecordErrorLog(c, 1, 3, "gpt-x", "tok", "pg-09 verification", 1, 0, false, "default", NewLogOther())
	})
}
