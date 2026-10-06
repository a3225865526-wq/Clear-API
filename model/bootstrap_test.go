package model

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const bootstrapTestPassword = "bootstrap-test-password"

func openBootstrapTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bootstrap.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Option{}, &Setup{}))
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestCreateRootAccountValidation(t *testing.T) {
	db := openBootstrapTestDB(t)
	_, err := CreateRootAccount(nil, RootAccountInput{Username: "root", Password: bootstrapTestPassword})
	require.Error(t, err)

	cases := []struct {
		name string
		in   RootAccountInput
		want error
	}{
		{"empty username", RootAccountInput{Username: "", Password: bootstrapTestPassword}, ErrRootUsernameRequired},
		{"long username", RootAccountInput{Username: "thirteen-chars", Password: bootstrapTestPassword}, ErrRootUsernameTooLong},
		{"negative quota", RootAccountInput{Username: "root", Password: bootstrapTestPassword, InitialQuota: -1}, ErrRootQuotaInvalid},
		{"oversized quota", RootAccountInput{Username: "root", Password: bootstrapTestPassword, InitialQuota: BootstrapMaxInitialQuota + 1}, ErrRootQuotaInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreateRootAccount(db, tc.in)
			require.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("weak password", func(t *testing.T) {
		_, err := CreateRootAccount(db, RootAccountInput{Username: "root", Password: "short"})
		require.ErrorIs(t, err, common.ErrAccountPasswordLength)
	})
}

func TestCreateRootAccountDefaults(t *testing.T) {
	db := openBootstrapTestDB(t)
	user, err := CreateRootAccount(db, RootAccountInput{Username: "root", Password: bootstrapTestPassword})
	require.NoError(t, err)
	require.NotZero(t, user.Id)
	require.Equal(t, common.RoleRootUser, user.Role)
	require.Equal(t, common.UserStatusEnabled, user.Status)
	require.Equal(t, "Root User", user.DisplayName)
	require.Equal(t, 0, user.Quota, "bootstrap default initial quota must be 0")
	require.Nil(t, user.AccessToken)
	require.True(t, strings.HasPrefix(user.Password, "$argon2id$"))
	require.NotContains(t, user.Password, bootstrapTestPassword)

	var count int64
	require.NoError(t, db.Model(&User{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestAdminAndBootstrapStateRollBackTogether(t *testing.T) {
	db := openBootstrapTestDB(t)
	err := db.Transaction(func(tx *gorm.DB) error {
		user, err := CreateRootAccount(tx, RootAccountInput{Username: "root", Password: bootstrapTestPassword})
		if err != nil {
			return err
		}
		if err := WriteBootstrapStateTx(tx, &BootstrapState{StartedAt: time.Now().UTC().Format(time.RFC3339), AdminUserID: user.Id, AdminUsername: user.Username}); err != nil {
			return err
		}
		return errors.New("injected failure")
	})
	require.ErrorContains(t, err, "injected failure")

	var userCount int64
	require.NoError(t, db.Model(&User{}).Count(&userCount).Error)
	require.Zero(t, userCount)
	_, found, err := readOptionRow(db, BootstrapStateOptionKey)
	require.NoError(t, err)
	require.False(t, found)
}

func TestBootstrapStateRoundTripAndCorruption(t *testing.T) {
	db := openBootstrapTestDB(t)

	state, err := ReadBootstrapState(db)
	require.NoError(t, err)
	require.Nil(t, state, "absent state must not be an error")

	require.NoError(t, WriteBootstrapStateTx(db, &BootstrapState{StartedAt: time.Now().UTC().Format(time.RFC3339), AdminUserID: 7, AdminUsername: "root", CandidateCommit: "c" + "0ffee"}))
	state, err = ReadBootstrapState(db)
	require.NoError(t, err)
	require.Equal(t, 7, state.AdminUserID)
	require.Equal(t, "root", state.AdminUsername)

	require.NoError(t, writeOptionTx(db, BootstrapStateOptionKey, "{not-json"))
	_, err = ReadBootstrapState(db)
	require.ErrorIs(t, err, ErrBootstrapStateCorrupt)

	require.NoError(t, writeOptionTx(db, BootstrapStateOptionKey, `{"schema_version":1,"admin_user_id":0}`))
	_, err = ReadBootstrapState(db)
	require.ErrorIs(t, err, ErrBootstrapStateCorrupt)
}

func bootstrapLegalInput() BootstrapLegalInput {
	return BootstrapLegalInput{
		UserAgreement:        "terms-body",
		PrivacyPolicy:        "privacy-body",
		TermsVersion:         "terms-v1",
		PrivacyVersion:       "privacy-v1",
		ApprovedBy:           "operator",
		CandidateCommit:      "candidate",
		BootstrapToolVersion: "tool",
	}
}

func TestWriteBootstrapLegalTxRoundTripAndMismatch(t *testing.T) {
	db := openBootstrapTestDB(t)
	in := bootstrapLegalInput()

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return WriteBootstrapLegalTx(tx, in) }))

	terms, found, err := readOptionRow(db, LegalUserAgreementOptionKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "terms-body", terms)
	privacy, found, err := readOptionRow(db, LegalPrivacyPolicyOptionKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "privacy-body", privacy)

	manifest, err := ReadBootstrapLegalManifest(db)
	require.NoError(t, err)
	require.Equal(t, "terms-v1", manifest.TermsVersion)
	require.Equal(t, "privacy-v1", manifest.PrivacyVersion)
	require.Equal(t, "operator", manifest.ApprovedBy)
	require.NotEmpty(t, manifest.ApprovedAt)
	require.Equal(t, 64, len(manifest.TermsSHA256))
	require.Equal(t, 64, len(manifest.PrivacySHA256))

	var setupCount int64
	require.NoError(t, db.Model(&Setup{}).Count(&setupCount).Error)
	require.Equal(t, int64(1), setupCount)

	// Rerun with identical content is a no-op.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return WriteBootstrapLegalTx(tx, in) }))

	// Rerun with different content fails closed and changes nothing.
	tampered := in
	tampered.UserAgreement = "tampered-terms"
	err = db.Transaction(func(tx *gorm.DB) error { return WriteBootstrapLegalTx(tx, tampered) })
	require.ErrorIs(t, err, ErrLegalContentMismatch)
	terms, _, err = readOptionRow(db, LegalUserAgreementOptionKey)
	require.NoError(t, err)
	require.Equal(t, "terms-body", terms)

	tamperedPrivacy := in
	tamperedPrivacy.PrivacyPolicy = "tampered-privacy"
	err = db.Transaction(func(tx *gorm.DB) error { return WriteBootstrapLegalTx(tx, tamperedPrivacy) })
	require.ErrorIs(t, err, ErrLegalContentMismatch)

	require.NoError(t, writeOptionTx(db, BootstrapLegalManifestOptionKey, "{broken"))
	err = db.Transaction(func(tx *gorm.DB) error { return WriteBootstrapLegalTx(tx, in) })
	require.ErrorIs(t, err, ErrLegalManifestCorrupt)
}

func TestWriteBootstrapLegalTxRollsBackAtomically(t *testing.T) {
	db := openBootstrapTestDB(t)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := WriteBootstrapLegalTx(tx, bootstrapLegalInput()); err != nil {
			return err
		}
		return errors.New("injected failure")
	})
	require.ErrorContains(t, err, "injected failure")

	for _, key := range []string{LegalUserAgreementOptionKey, LegalPrivacyPolicyOptionKey, BootstrapLegalManifestOptionKey} {
		_, found, err := readOptionRow(db, key)
		require.NoError(t, err)
		require.False(t, found, key)
	}
	var setupCount int64
	require.NoError(t, db.Model(&Setup{}).Count(&setupCount).Error)
	require.Zero(t, setupCount)
}

func TestBootstrapMarkerRoundTripAndCorruption(t *testing.T) {
	db := openBootstrapTestDB(t)

	marker, err := ReadBootstrapMarker(db)
	require.NoError(t, err)
	require.Nil(t, marker)

	valid := &BootstrapCompletionMarker{
		CompletedAt:              time.Now().UTC().Format(time.RFC3339),
		BootstrapToolVersion:     "tool",
		CandidateCommit:          "candidate",
		AdminUserID:              1,
		AdminUsername:            "root",
		FinanceOwnerUserID:       1,
		TermsSHA256:              strings.Repeat("a", 64),
		PrivacySHA256:            strings.Repeat("b", 64),
		FinanceSchemaVersion:     9,
		BillingMigrationsApplied: []string{"billing-reservations-v1"},
		DatabaseType:             "postgresql",
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return WriteBootstrapMarkerTx(tx, valid) }))
	readBack, err := ReadBootstrapMarker(db)
	require.NoError(t, err)
	require.Equal(t, valid.AdminUserID, readBack.AdminUserID)
	require.Equal(t, valid.FinanceOwnerUserID, readBack.FinanceOwnerUserID)

	incomplete := *valid
	incomplete.FinanceOwnerUserID = 0
	err = db.Transaction(func(tx *gorm.DB) error { return WriteBootstrapMarkerTx(tx, &incomplete) })
	require.ErrorIs(t, err, ErrBootstrapMarkerCorrupt)

	require.NoError(t, writeOptionTx(db, BootstrapMarkerOptionKey, `{"schema_version":1,"completed_at":"x"}`))
	_, err = ReadBootstrapMarker(db)
	require.ErrorIs(t, err, ErrBootstrapMarkerCorrupt)
}

func TestInitDBWithoutMigrationCreatesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	oldPath := common.SQLitePath
	oldDB, oldLOG := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		common.SQLitePath = oldPath
		DB, LOG_DB = oldDB, oldLOG
		common.SetMainDatabaseType(oldMainType)
		common.SetLogDatabaseType(oldLogType)
	})
	common.SQLitePath = path
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")

	require.NoError(t, InitDBWithoutMigration())
	require.False(t, DB.Migrator().HasTable(&Option{}), "no table may be created without migrations")
	require.False(t, DB.Migrator().HasTable(&User{}))
	require.Same(t, DB, LOG_DB)

	value, found, err := readOptionRow(DB, BootstrapMarkerOptionKey)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, value)
}

func TestApplyCommittedOptionValuesUpdatesLegalSettings(t *testing.T) {
	legal := system_setting.GetLegalSettings()
	old := *legal
	t.Cleanup(func() { *legal = old })

	require.NoError(t, ApplyCommittedOptionValues(map[string]string{
		LegalUserAgreementOptionKey: "agreed-body",
		LegalPrivacyPolicyOptionKey: "privacy-body",
	}))
	require.Equal(t, "agreed-body", system_setting.GetLegalSettings().UserAgreement)
	require.Equal(t, "privacy-body", system_setting.GetLegalSettings().PrivacyPolicy)
}
