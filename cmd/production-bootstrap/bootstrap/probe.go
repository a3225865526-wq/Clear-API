package bootstrap

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Snapshot is the read-only view of the database used for branch decisions.
// Every access is a plain SELECT / information-schema probe: the probe never
// migrates, backfills or creates anything.
type Snapshot struct {
	DatabaseType         string
	OptionsTablePresent  bool
	UsersTablePresent    bool
	UserCount            int64
	RootUserExists       bool
	SetupRowPresent      bool
	FinanceSchemaPresent bool
	FinanceSchemaVersion int

	State         *model.BootstrapState
	StateCorrupt  bool
	Marker        *model.BootstrapCompletionMarker
	MarkerCorrupt bool

	LegalUserAgreementPresent bool
	LegalPrivacyPolicyPresent bool
	LegalManifestPresent      bool

	// Read-only schema readiness probes (mirrors what the gate inspects).
	ReservationsReady    bool
	AuthSnapshotsReady   bool
	OutboxReady          bool
	ReconciliationReady  bool
	SettlementReady      bool
	SettlementLogsReady  bool
	TokenEncryptionReady bool
	ImageExecutionsReady bool
}

// OpenReadonly initializes the DB/LOG_DB globals without running any
// migration and initializes the Redis global exactly like the server does
// (production requires Redis to be absent, so this is a no-op there).
func OpenReadonly() error {
	if err := model.InitDBWithoutMigration(); err != nil {
		return err
	}
	return common.InitRedisClient()
}

// TakeSnapshot collects the read-only view. Read failures on missing tables
// are reported as "absent", never as triggers for repair.
func TakeSnapshot() *Snapshot {
	snapshot := &Snapshot{DatabaseType: string(common.MainDatabaseType())}
	db := model.DB
	if db == nil {
		return snapshot
	}
	snapshot.OptionsTablePresent = db.Migrator().HasTable(&model.Option{})
	snapshot.UsersTablePresent = db.Migrator().HasTable(&model.User{})
	if snapshot.UsersTablePresent {
		_ = db.Model(&model.User{}).Count(&snapshot.UserCount).Error
		var root model.User
		snapshot.RootUserExists = db.Where(&model.User{Role: common.RoleRootUser}).First(&root).Error == nil
	}

	if state, err := model.ReadBootstrapState(db); err != nil {
		snapshot.StateCorrupt = true
	} else {
		snapshot.State = state
	}
	if marker, err := model.ReadBootstrapMarker(db); err != nil {
		snapshot.MarkerCorrupt = true
	} else {
		snapshot.Marker = marker
	}

	if snapshot.OptionsTablePresent {
		_, snapshot.LegalUserAgreementPresent, _ = model.ReadBootstrapOption(db, model.LegalUserAgreementOptionKey)
		_, snapshot.LegalPrivacyPolicyPresent, _ = model.ReadBootstrapOption(db, model.LegalPrivacyPolicyOptionKey)
		_, snapshot.LegalManifestPresent, _ = model.ReadBootstrapOption(db, model.BootstrapLegalManifestOptionKey)
	}
	if db.Migrator().HasTable(&model.Setup{}) {
		var count int64
		_ = db.Model(&model.Setup{}).Count(&count).Error
		snapshot.SetupRowPresent = count > 0
	}
	if version, ok, err := model.ReadFinanceSchemaVersion(db); err == nil && ok {
		snapshot.FinanceSchemaPresent = true
		snapshot.FinanceSchemaVersion = version
	}

	snapshot.ReservationsReady = probe(func() error { return model.BillingReservationsReady(db) })
	snapshot.AuthSnapshotsReady = probe(func() error { return model.BillingAuthSnapshotsReady(db) })
	snapshot.OutboxReady = probe(func() error { return model.BillingCompletionOutboxReady(db, model.LOG_DB) })
	snapshot.ReconciliationReady = probe(func() error { return model.BillingReconciliationReady(db) })
	snapshot.SettlementReady = probe(func() error { return model.TaskSettlementReady(db) })
	if model.LOG_DB != nil {
		snapshot.SettlementLogsReady = probe(func() error { return model.TaskSettlementLogsReady(model.LOG_DB) })
	}
	snapshot.TokenEncryptionReady = probe(func() error { return model.TokenEncryptionReady(db) })
	snapshot.ImageExecutionsReady = probe(func() error { return model.ImageExecutionsReady(db) })
	return snapshot
}

func probe(fn func() error) bool {
	return fn() == nil
}
