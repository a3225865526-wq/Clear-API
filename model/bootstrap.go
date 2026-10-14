package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Production bootstrap records live in the options table on purpose: the
// bootstrap must never touch the shared AutoMigrate registration, and option
// rows are loaded into the runtime option map automatically on startup.

const (
	BootstrapStateOptionKey         = "bootstrap.state"
	BootstrapLegalManifestOptionKey = "bootstrap.legal_approval_manifest"
	BootstrapMarkerOptionKey        = "bootstrap.completion_marker"

	// The DB representation of the registered "legal" config group
	// (setting/system_setting/legal.go).
	LegalUserAgreementOptionKey = "legal.user_agreement"
	LegalPrivacyPolicyOptionKey = "legal.privacy_policy"

	bootstrapRecordSchemaVersion = 1
)

// BootstrapMaxInitialQuota bounds the initial admin quota so a fresh
// production database can never be seeded with an unbounded spendable balance.
const BootstrapMaxInitialQuota = math.MaxInt32

var (
	ErrRootUsernameRequired   = errors.New("用户名不能为空")
	ErrRootUsernameTooLong    = errors.New("用户名长度不能超过12个字符")
	ErrRootQuotaInvalid       = errors.New("初始额度必须介于 0 与允许上限之间")
	ErrBootstrapStateMissing  = errors.New("bootstrap state record is missing")
	ErrBootstrapStateCorrupt  = errors.New("bootstrap state record is corrupt")
	ErrBootstrapMarkerMissing = errors.New("bootstrap completion marker is missing")
	ErrBootstrapMarkerCorrupt = errors.New("bootstrap completion marker is corrupt")
	ErrLegalManifestCorrupt   = errors.New("bootstrap legal approval manifest is corrupt")
	ErrLegalContentMismatch   = errors.New("legal content does not match the recorded approval manifest")
	ErrLegalInputInvalid      = errors.New("legal input is invalid")
)

// RootAccountInput is the shared first-root-account creation contract used by
// both the HTTP setup flow and the production bootstrap command.
type RootAccountInput struct {
	Username string
	// Password is the plaintext credential; it is validated and hashed here
	// and never leaves this function.
	Password string
	// DisplayName defaults to "Root User" when empty.
	DisplayName string
	// InitialQuota must be >= 0. The HTTP setup flow keeps its historical
	// default; the production bootstrap defaults to 0.
	InitialQuota int
}

// CreateRootAccount validates the root account input and creates the first
// root user inside the given transaction handle. Callers own the transaction:
// passing a *gorm.DB transaction keeps user creation atomic with any other
// bootstrap writes in the same transaction.
func CreateRootAccount(tx *gorm.DB, input RootAccountInput) (*User, error) {
	if tx == nil {
		return nil, errors.New("root account creation requires an explicit transaction handle")
	}
	if input.Username == "" {
		return nil, ErrRootUsernameRequired
	}
	if len(input.Username) > 12 {
		return nil, ErrRootUsernameTooLong
	}
	if input.InitialQuota < 0 || input.InitialQuota > BootstrapMaxInitialQuota {
		return nil, ErrRootQuotaInvalid
	}
	if err := common.ValidateNewAccountPassword(input.Password); err != nil {
		return nil, err
	}
	hashedPassword, err := common.HashAccountPassword(input.Password)
	if err != nil {
		return nil, err
	}
	displayName := input.DisplayName
	if displayName == "" {
		displayName = "Root User"
	}
	rootUser := User{
		Username:    input.Username,
		Password:    hashedPassword,
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
		DisplayName: displayName,
		AccessToken: nil,
		Quota:       input.InitialQuota,
	}
	if err := tx.Create(&rootUser).Error; err != nil {
		return nil, err
	}
	return &rootUser, nil
}

// BootstrapState records that the bootstrap data phase started and which real
// admin identity it created, so a rerun can verify instead of recreate.
type BootstrapState struct {
	SchemaVersion        int    `json:"schema_version"`
	StartedAt            string `json:"started_at"`
	AdminUserID          int    `json:"admin_user_id"`
	AdminUsername        string `json:"admin_username"`
	BootstrapToolVersion string `json:"bootstrap_tool_version"`
	CandidateCommit      string `json:"candidate_commit"`
}

func WriteBootstrapStateTx(tx *gorm.DB, state *BootstrapState) error {
	if tx == nil {
		return errors.New("bootstrap state write requires an explicit transaction handle")
	}
	if state == nil || state.SchemaVersion == 0 {
		state.SchemaVersion = bootstrapRecordSchemaVersion
	}
	if state.AdminUserID <= 0 || state.AdminUsername == "" {
		return errors.New("bootstrap state requires the real admin identity")
	}
	value, err := common.Marshal(state)
	if err != nil {
		return err
	}
	return writeOptionTx(tx, BootstrapStateOptionKey, string(value))
}

// ReadBootstrapState returns (nil, nil) when the record is absent and
// ErrBootstrapStateCorrupt when it exists but cannot be parsed.
func ReadBootstrapState(db *gorm.DB) (*BootstrapState, error) {
	value, found, err := readOptionRow(db, BootstrapStateOptionKey)
	if err != nil || !found {
		return nil, err
	}
	var state BootstrapState
	if err := common.Unmarshal([]byte(value), &state); err != nil {
		return nil, ErrBootstrapStateCorrupt
	}
	if state.SchemaVersion != bootstrapRecordSchemaVersion || state.AdminUserID <= 0 || state.AdminUsername == "" {
		return nil, ErrBootstrapStateCorrupt
	}
	return &state, nil
}

// BootstrapLegalInput carries the operator-approved legal content and the
// attestation reference recorded alongside it.
type BootstrapLegalInput struct {
	UserAgreement        string
	PrivacyPolicy        string
	TermsVersion         string
	PrivacyVersion       string
	ApprovedBy           string
	ApprovedAt           string // RFC3339 UTC; empty means now
	CandidateCommit      string
	BootstrapToolVersion string
	// Safe-mode options are written only when the option row is absent;
	// production defaults are false for both.
	SelfUseMode bool
	DemoSite    bool
}

// LegalApprovalManifest is an operator attestation / audit record, not a
// cryptographic legal signature.
type LegalApprovalManifest struct {
	SchemaVersion        int    `json:"schema_version"`
	TermsVersion         string `json:"terms_version"`
	TermsSHA256          string `json:"terms_sha256"`
	PrivacyVersion       string `json:"privacy_version"`
	PrivacySHA256        string `json:"privacy_sha256"`
	ApprovedAt           string `json:"approved_at"`
	ApprovedBy           string `json:"approved_by"`
	CandidateCommit      string `json:"candidate_commit"`
	BootstrapToolVersion string `json:"bootstrap_tool_version"`
}

// WriteBootstrapLegalTx writes the legal content, the approval manifest, the
// safe-mode options and the setup row inside one transaction. Reruns are
// verify-and-skip: already-written content must match by hash or the write
// fails closed; nothing is silently overwritten.
func WriteBootstrapLegalTx(tx *gorm.DB, in BootstrapLegalInput) error {
	if tx == nil {
		return errors.New("bootstrap legal write requires an explicit transaction handle")
	}
	if strings.TrimSpace(in.UserAgreement) == "" || strings.TrimSpace(in.PrivacyPolicy) == "" {
		return ErrLegalInputInvalid
	}
	if strings.TrimSpace(in.TermsVersion) == "" || strings.TrimSpace(in.PrivacyVersion) == "" || strings.TrimSpace(in.ApprovedBy) == "" {
		return ErrLegalInputInvalid
	}
	termsSHA := sha256Hex(in.UserAgreement)
	privacySHA := sha256Hex(in.PrivacyPolicy)

	existingTerms, found, err := readOptionRow(tx, LegalUserAgreementOptionKey)
	if err != nil {
		return err
	}
	if found && existingTerms != in.UserAgreement {
		return fmt.Errorf("%w: legal.user_agreement", ErrLegalContentMismatch)
	}
	existingPrivacy, found, err := readOptionRow(tx, LegalPrivacyPolicyOptionKey)
	if err != nil {
		return err
	}
	if found && existingPrivacy != in.PrivacyPolicy {
		return fmt.Errorf("%w: legal.privacy_policy", ErrLegalContentMismatch)
	}

	existingManifest, found, err := readOptionRow(tx, BootstrapLegalManifestOptionKey)
	if err != nil {
		return err
	}
	if found {
		var manifest LegalApprovalManifest
		if err := common.Unmarshal([]byte(existingManifest), &manifest); err != nil {
			return ErrLegalManifestCorrupt
		}
		if manifest.TermsSHA256 != termsSHA || manifest.PrivacySHA256 != privacySHA {
			return ErrLegalContentMismatch
		}
		// Keep the original attestation; do not rewrite approved_at on reruns.
	} else {
		approvedAt := in.ApprovedAt
		if approvedAt == "" {
			approvedAt = time.Now().UTC().Format(time.RFC3339)
		}
		manifest := LegalApprovalManifest{
			SchemaVersion:        bootstrapRecordSchemaVersion,
			TermsVersion:         in.TermsVersion,
			TermsSHA256:          termsSHA,
			PrivacyVersion:       in.PrivacyVersion,
			PrivacySHA256:        privacySHA,
			ApprovedAt:           approvedAt,
			ApprovedBy:           in.ApprovedBy,
			CandidateCommit:      in.CandidateCommit,
			BootstrapToolVersion: in.BootstrapToolVersion,
		}
		value, err := common.Marshal(manifest)
		if err != nil {
			return err
		}
		if err := writeOptionTx(tx, BootstrapLegalManifestOptionKey, string(value)); err != nil {
			return err
		}
	}

	if err := writeOptionIfAbsentTx(tx, LegalUserAgreementOptionKey, in.UserAgreement); err != nil {
		return err
	}
	if err := writeOptionIfAbsentTx(tx, LegalPrivacyPolicyOptionKey, in.PrivacyPolicy); err != nil {
		return err
	}

	if err := writeOptionIfAbsentTx(tx, "SelfUseModeEnabled", fmt.Sprintf("%t", in.SelfUseMode)); err != nil {
		return err
	}
	if err := writeOptionIfAbsentTx(tx, "DemoSiteEnabled", fmt.Sprintf("%t", in.DemoSite)); err != nil {
		return err
	}

	var setupCount int64
	if err := tx.Model(&Setup{}).Count(&setupCount).Error; err != nil {
		return err
	}
	if setupCount == 0 {
		if err := tx.Create(&Setup{Version: in.BootstrapToolVersion, InitializedAt: time.Now().Unix()}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ReadBootstrapLegalManifest returns (nil, nil) when the manifest is absent.
func ReadBootstrapLegalManifest(db *gorm.DB) (*LegalApprovalManifest, error) {
	value, found, err := readOptionRow(db, BootstrapLegalManifestOptionKey)
	if err != nil || !found {
		return nil, err
	}
	var manifest LegalApprovalManifest
	if err := common.Unmarshal([]byte(value), &manifest); err != nil {
		return nil, ErrLegalManifestCorrupt
	}
	return &manifest, nil
}

// BootstrapCompletionMarker is written last, only after every bootstrap step
// (migrations, admin, finance owner, legal, manifest, readiness) passed.
type BootstrapCompletionMarker struct {
	SchemaVersion            int    `json:"schema_version"`
	CompletedAt              string `json:"completed_at"`
	BootstrapToolVersion     string `json:"bootstrap_tool_version"`
	CandidateCommit          string `json:"candidate_commit"`
	AdminUserID              int    `json:"admin_user_id"`
	AdminUsername            string `json:"admin_username"`
	FinanceOwnerUserID       int    `json:"finance_owner_user_id"`
	TermsVersion             string `json:"terms_version"`
	TermsSHA256              string `json:"terms_sha256"`
	PrivacyVersion           string `json:"privacy_version"`
	PrivacySHA256            string `json:"privacy_sha256"`
	ApprovedBy               string `json:"approved_by"`
	ApprovedAt               string `json:"approved_at"`
	FinanceSchemaVersion     int    `json:"finance_schema_version"`
	BillingMigrationsApplied []string `json:"billing_migrations_applied"`
	DatabaseType             string `json:"database_type"`
}

func WriteBootstrapMarkerTx(tx *gorm.DB, marker *BootstrapCompletionMarker) error {
	if tx == nil {
		return errors.New("bootstrap marker write requires an explicit transaction handle")
	}
	if marker == nil {
		return errors.New("bootstrap marker is required")
	}
	marker.SchemaVersion = bootstrapRecordSchemaVersion
	if err := marker.Validate(); err != nil {
		return err
	}
	value, err := common.Marshal(marker)
	if err != nil {
		return err
	}
	return writeOptionTx(tx, BootstrapMarkerOptionKey, string(value))
}

// ReadBootstrapMarker returns (nil, nil) when the marker is absent and
// ErrBootstrapMarkerCorrupt when it exists but is not a valid marker.
func ReadBootstrapMarker(db *gorm.DB) (*BootstrapCompletionMarker, error) {
	value, found, err := readOptionRow(db, BootstrapMarkerOptionKey)
	if err != nil || !found {
		return nil, err
	}
	var marker BootstrapCompletionMarker
	if err := common.Unmarshal([]byte(value), &marker); err != nil {
		return nil, ErrBootstrapMarkerCorrupt
	}
	if err := marker.Validate(); err != nil {
		return nil, ErrBootstrapMarkerCorrupt
	}
	return &marker, nil
}

// Validate enforces the completeness contract of the completion marker.
func (m *BootstrapCompletionMarker) Validate() error {
	if m == nil {
		return ErrBootstrapMarkerCorrupt
	}
	if m.SchemaVersion != bootstrapRecordSchemaVersion ||
		m.CompletedAt == "" ||
		m.BootstrapToolVersion == "" ||
		m.AdminUserID <= 0 ||
		m.AdminUsername == "" ||
		m.FinanceOwnerUserID <= 0 ||
		len(m.TermsSHA256) != 64 || len(m.PrivacySHA256) != 64 ||
		m.FinanceSchemaVersion < 1 || m.FinanceSchemaVersion > 9 ||
		len(m.BillingMigrationsApplied) == 0 ||
		m.DatabaseType == "" {
		return ErrBootstrapMarkerCorrupt
	}
	return nil
}

// ApplyCommittedOptionValues dispatches committed option values through the
// in-memory option map. Bootstrap writes bypass UpdateOption for atomicity,
// so the caller refreshes the map (and derived config globals) after commit.
func ApplyCommittedOptionValues(values map[string]string) error {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()
	for key, value := range values {
		if err := updateOptionMap(key, value); err != nil {
			return err
		}
	}
	return nil
}

// InitDBWithoutMigration initializes the DB/LOG_DB globals with the same
// dialect selection as InitDB/InitLogDB but never runs migrations, backfills
// or any other write. It exists for read-only bootstrap probing and
// verification; production bootstrap execution uses the real InitDB path.
func InitDBWithoutMigration() (err error) {
	db, dbType, err := chooseDB("SQL_DSN", false)
	if err != nil {
		return err
	}
	common.SetMainDatabaseType(dbType)
	if os.Getenv("LOG_SQL_DSN") == "" {
		common.SetLogDatabaseType(dbType)
	}
	initCol()
	DB = db
	if os.Getenv("LOG_SQL_DSN") == "" {
		LOG_DB = DB
		return nil
	}
	logDB, logType, err := chooseDB("LOG_SQL_DSN", true)
	if err != nil {
		return err
	}
	common.SetLogDatabaseType(logType)
	initCol()
	LOG_DB = logDB
	return nil
}

// ReadBootstrapOption exposes read-only option inspection for the production
// bootstrap CLI probe. It never creates rows or tables.
func ReadBootstrapOption(db *gorm.DB, key string) (string, bool, error) {
	return readOptionRow(db, key)
}

// ReadFinanceSchemaVersion reports the applied finance schema version
// (absent when the schema table does not exist yet).
func ReadFinanceSchemaVersion(db *gorm.DB) (int, bool, error) {
	if db == nil {
		return 0, false, errors.New("bootstrap read requires a database handle")
	}
	if !db.Migrator().HasTable(&FinanceSchema{}) {
		return 0, false, nil
	}
	var row FinanceSchema
	err := db.Where(&FinanceSchema{ID: 1}).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return row.Version, true, nil
}

func writeOptionTx(tx *gorm.DB, key, value string) error {
	option := Option{Key: key}
	if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
		return err
	}
	option.Value = value
	return tx.Save(&option).Error
}

func writeOptionIfAbsentTx(tx *gorm.DB, key, value string) error {
	option := Option{Key: key}
	if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
		return err
	}
	if option.Value != "" {
		return nil
	}
	option.Value = value
	return tx.Save(&option).Error
}

// readOptionRow returns (value, true, nil) when the row exists. A missing
// options table (fresh database) is reported as absent, never created.
func readOptionRow(db *gorm.DB, key string) (string, bool, error) {
	if db == nil {
		return "", false, errors.New("bootstrap read requires a database handle")
	}
	if !db.Migrator().HasTable(&Option{}) {
		return "", false, nil
	}
	var option Option
	err := db.Where(&Option{Key: key}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return option.Value, true, nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
