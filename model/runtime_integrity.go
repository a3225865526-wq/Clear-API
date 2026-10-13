package model

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrRuntimeConfigMismatch = errors.New("RUNTIME_CONFIG_MISMATCH")
	ErrApprovalViewInvalid   = errors.New("APPROVAL_VIEW_INVALID")
	ErrProbeUnavailable      = errors.New("PROBE_UNAVAILABLE")
	ErrProductViewInvalid    = errors.New("PRODUCT_VIEW_INVALID")
)

// RuntimeIntegrity contains only public configuration facts and opaque
// identities. ConfigGeneration is a content fingerprint, not an update counter.
// ReferralEnabled is the reward gate; referral business reads it directly from DB.
type RuntimeIntegrity struct {
	SchemaVersion         string    `json:"schema_version"`
	ReportVersion         string    `json:"report_version"`
	Status                string    `json:"status"`
	InstanceID            string    `json:"instance_id"`
	ContainerID           string    `json:"container_id"`
	StartedAt             string    `json:"started_at"`
	BuildIdentity         string    `json:"build_identity"`
	ConfigGeneration      string    `json:"config_generation"`
	ConfigDigest          string    `json:"config_digest"`
	OnlinePaymentEnabled  bool      `json:"online_payment_enabled"`
	StoreEnabled          bool      `json:"store_enabled"`
	BillingMode           string    `json:"billing_mode"`
	ReferralEnabled       bool      `json:"referral_enabled"`
	PoolEnabled           bool      `json:"pool_enabled"`
	ApprovalSchema        int       `json:"approval_schema"`
	CatalogVersion        int64     `json:"catalog_version"`
	ApprovalViewDigest    string    `json:"approval_view_digest"`
	ProductViewDigest     string    `json:"product_view_digest"`
	EffectiveProductCount int       `json:"effective_product_count"`
	GeneratedAt           time.Time `json:"generated_at"`
}

// RuntimeIntegrityObservation is internal evidence. Only the completed report
// is serialized by the API; ViewStateDigest fences the separately reused user
// projections against catalog/configuration changes during collection.
type RuntimeIntegrityObservation struct {
	Report          RuntimeIntegrity
	ViewStateDigest string
}

// Docker's host supplies this file via a read-only mount using inspect metadata.
// It is not an environment override. Kernel mount/cgroup metadata must identify
// the same container; unavailable or ambiguous identity always fails closed.
const runtimeIdentityPath = "/run/clearapi/runtime-identity.json"

var runtimeContainerIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var runtimeBuildDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var runtimeContainerPattern = regexp.MustCompile(`(?:^|/)(?:docker-)?([a-f0-9]{64})(?:/|\.scope|$)`)

func rootOwnedRuntimeMetadata(info os.FileInfo) bool {
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return false
	}
	uid, gid, links := value.FieldByName("Uid"), value.FieldByName("Gid"), value.FieldByName("Nlink")
	return uid.IsValid() && uid.CanUint() && uid.Uint() == 0 && gid.IsValid() && gid.CanUint() && gid.Uint() == 0 && links.IsValid() && links.CanUint() && (info.IsDir() || links.Uint() == 1)
}

func readRuntimeContainerIdentity() (string, string, error) {
	if runtime.GOOS != "linux" {
		return "", "", ErrProbeUnavailable
	}
	ids := make(map[string]bool)
	for _, path := range []string{"/proc/self/cgroup", "/proc/self/mountinfo"} {
		f, err := os.Open(path)
		if err != nil {
			return "", "", ErrProbeUnavailable
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if path == "/proc/self/mountinfo" {
				fields := strings.Fields(line)
				if len(fields) < 5 || fields[4] != "/etc/hostname" {
					continue
				}
				line = fields[3]
			}
			if match := runtimeContainerPattern.FindStringSubmatch(line); len(match) == 2 {
				ids[match[1]] = true
			}
		}
		err = scanner.Err()
		_ = f.Close()
		if err != nil {
			return "", "", ErrProbeUnavailable
		}
	}
	if len(ids) != 1 {
		return "", "", ErrProbeUnavailable
	}
	for _, path := range []string{"/run", "/run/clearapi"} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || !rootOwnedRuntimeMetadata(info) || info.Mode().Perm()&0o022 != 0 {
			return "", "", ErrProbeUnavailable
		}
	}
	before, err := os.Lstat(runtimeIdentityPath)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0o400 || !rootOwnedRuntimeMetadata(before) {
		return "", "", ErrProbeUnavailable
	}
	f, err := os.Open(runtimeIdentityPath)
	if err != nil {
		return "", "", ErrProbeUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(before, info) {
		return "", "", ErrProbeUnavailable
	}
	var identity struct {
		ContainerID string `json:"container_id"`
		StartedAt   string `json:"started_at"`
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(raw) > 4096 || common.Unmarshal(raw, &identity) != nil || !ids[identity.ContainerID] {
		return "", "", ErrProbeUnavailable
	}
	started, err := time.Parse(time.RFC3339Nano, identity.StartedAt)
	if err != nil || !started.After(time.Unix(0, 0)) || started.After(time.Now()) {
		return "", "", ErrProbeUnavailable
	}
	return identity.ContainerID, identity.StartedAt, nil
}

var runtimeInstanceID = uuid.NewString()
var runtimeBuildIdentity = sync.OnceValues(func() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", ErrProbeUnavailable
	}
	// On Linux, resolve the running inode even if its pathname was replaced.
	if runtime.GOOS == "linux" {
		path = "/proc/self/exe"
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ErrProbeUnavailable
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", ErrProbeUnavailable
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
})

// RuntimeIdentityProvider is an explicit dependency, never selected by a
// request, environment variable or global override. Production API wiring uses
// only dockerRuntimeIdentityProvider; deterministic implementations live in tests.
type RuntimeIdentity struct {
	InstanceID    string
	ContainerID   string
	StartedAt     string
	BuildIdentity string
}

type RuntimeIdentityProvider interface {
	RuntimeIdentity(context.Context) (RuntimeIdentity, error)
}

type dockerRuntimeIdentityProvider struct{}

func (dockerRuntimeIdentityProvider) RuntimeIdentity(ctx context.Context) (RuntimeIdentity, error) {
	if ctx.Err() != nil {
		return RuntimeIdentity{}, ErrProbeUnavailable
	}
	containerID, startedAt, err := readRuntimeContainerIdentity()
	if err != nil {
		return RuntimeIdentity{}, err
	}
	build, err := runtimeBuildIdentity()
	if err != nil {
		return RuntimeIdentity{}, err
	}
	return RuntimeIdentity{InstanceID: runtimeInstanceID, ContainerID: containerID, StartedAt: startedAt, BuildIdentity: build}, nil
}

// ObserveRuntimeIntegrity never reloads options, initializes schemas, repairs data,
// or starts workers. The option read lock holds the effective fields stable
// against the existing updateOptionMap path throughout the observation.
func ObserveRuntimeIntegrity(ctx context.Context) (RuntimeIntegrityObservation, error) {
	return ObserveRuntimeIntegrityWithProvider(ctx, nil)
}

// ObserveRuntimeIntegrityWithProvider permits explicit dependency injection.
// Nil always means the production provider. A provider error or invalid identity
// is BLOCKED; it never falls back to another provider or database-only evidence.
func ObserveRuntimeIntegrityWithProvider(ctx context.Context, provider RuntimeIdentityProvider) (RuntimeIntegrityObservation, error) {
	var result RuntimeIntegrity
	if DB == nil || ctx.Err() != nil {
		return RuntimeIntegrityObservation{}, ErrProbeUnavailable
	}
	if provider == nil {
		provider = dockerRuntimeIdentityProvider{}
	}
	identity, err := provider.RuntimeIdentity(ctx)
	if err != nil {
		return RuntimeIntegrityObservation{}, ErrProbeUnavailable
	}
	instance, err := uuid.Parse(identity.InstanceID)
	started, timeErr := time.Parse(time.RFC3339Nano, identity.StartedAt)
	if err != nil || instance.Version() != 4 || instance.String() != identity.InstanceID ||
		!runtimeContainerIDPattern.MatchString(identity.ContainerID) ||
		!runtimeBuildDigestPattern.MatchString(identity.BuildIdentity) ||
		timeErr != nil || !started.After(time.Unix(0, 0)) || started.After(time.Now()) {
		return RuntimeIntegrityObservation{}, ErrProbeUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	result = RuntimeIntegrity{
		SchemaVersion: "1", ReportVersion: "1", ContainerID: identity.ContainerID, StartedAt: identity.StartedAt,
		InstanceID: identity.InstanceID, BuildIdentity: identity.BuildIdentity,
		OnlinePaymentEnabled: !operation_setting.DisableOnlinePayments,
		StoreEnabled:         operation_setting.StoreRechargeEnabled,
		BillingMode:          operation_setting.ReservationHardeningMode(),
		PoolEnabled:          common.OptionMap[PoolMasterOptionKey] == "true",
	}
	expected := map[string]string{
		"DisableOnlinePayments":       strconv.FormatBool(!result.OnlinePaymentEnabled),
		"StoreRechargeEnabled":        strconv.FormatBool(result.StoreEnabled),
		"billing_reserve_policy.mode": result.BillingMode,
		PoolMasterOptionKey:           strconv.FormatBool(result.PoolEnabled),
	}
	for key, value := range expected {
		raw, ok := common.OptionMap[key]
		if !ok {
			return RuntimeIntegrityObservation{}, ErrProbeUnavailable
		}
		if raw != value {
			return RuntimeIntegrityObservation{}, ErrRuntimeConfigMismatch
		}
	}
	var viewStateDigest string
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var options []Option
		if err := tx.Where(commonKeyCol+" IN ?", []string{"DisableOnlinePayments", "StoreRechargeEnabled", "billing_reserve_policy.mode", PoolMasterOptionKey, ReferralOptionKey}).Find(&options).Error; err != nil {
			return ErrProbeUnavailable
		}
		stored := make(map[string]string, len(options))
		for _, option := range options {
			stored[option.Key] = option.Value
		}
		for key, value := range expected {
			raw, exists := stored[key]
			// An absent pool row is its existing, fail-closed default.
			if key == PoolMasterOptionKey && !exists {
				raw, exists = "false", true
			}
			if !exists {
				return ErrProbeUnavailable
			}
			if raw != value {
				return ErrRuntimeConfigMismatch
			}
		}
		if _, exists := stored[ReferralOptionKey]; !exists {
			return ErrProbeUnavailable
		}
		referral, err := readReferralConfig(tx, false)
		if err != nil {
			return ErrProbeUnavailable
		}
		result.ReferralEnabled = referral.RewardsEnabled
		catalog, err := getEffectiveCreditProducts(tx)
		if err != nil {
			return ErrApprovalViewInvalid
		}
		result.ApprovalSchema, result.CatalogVersion = catalog.ApprovalSchema, catalog.Version
		// Hash the validated view and compatibility set without returning their
		// contents. The shared validator checks publications even when store is off.
		withdrawals := []CreditProduct{}
		for _, p := range catalog.ApprovalProducts {
			if p.LegacyWithdrawn {
				withdrawals = append(withdrawals, CreditProduct{ID: p.ID, LegacyStoreID: p.LegacyStoreID, LegacyWithdrawn: true})
			}
		}
		raw, err := common.Marshal(struct {
			Unified           bool            `json:"unified"`
			Products          []CreditProduct `json:"products"`
			LegacyProducts    []*StoreProduct `json:"legacy_products"`
			LegacyCatalog     []CreditProduct `json:"legacy_catalog"`
			LegacyWithdrawals []CreditProduct `json:"legacy_withdrawals"`
		}{catalog.Unified, catalog.Products, catalog.LegacyProducts, catalog.LegacyCatalog, withdrawals})
		if err != nil {
			return ErrProbeUnavailable
		}
		viewStateDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
		// Only already validated publication claims enter the approval digest.
		// Query selected event/batch metadata, never redemption values or keys.
		type approvalEvidence struct {
			ProductID   string              `json:"product_id"`
			Revision    int64               `json:"revision"`
			TermsDigest string              `json:"terms_digest"`
			Event       CreditApprovalEvent `json:"event"`
			Batch       RedemptionBatch     `json:"batch"`
		}
		evidence := []approvalEvidence{}
		configured := map[string]bool{}
		for _, p := range catalog.ApprovalProducts {
			if !p.Published {
				continue
			}
			terms, err := CreditProductDigest(p)
			if err != nil {
				return ErrApprovalViewInvalid
			}
			var event CreditApprovalEvent
			if err := tx.First(&event, p.ApprovalID).Error; err != nil {
				return ErrApprovalViewInvalid
			}
			var batch RedemptionBatch
			if err := tx.Select("id", "product_id", "product_revision", "delivery_binding", "approval_id", "evidence_only", "status", "expired_time", "count", "quota", "face_value_cents", "source", "type").First(&batch, p.EvidenceBatchID).Error; err != nil {
				return ErrApprovalViewInvalid
			}
			evidence = append(evidence, approvalEvidence{p.ID, p.Revision, terms, event, batch})
			configured["product/"+p.ID] = true
		}
		for _, legacy := range catalog.LegacyProducts {
			if legacy.Enabled {
				configured[fmt.Sprintf("legacy/%d", legacy.Id)] = true
			}
		}
		for _, legacy := range catalog.LegacyCatalog {
			if legacy.Published {
				configured["product/"+legacy.ID] = true
			}
		}
		result.EffectiveProductCount = len(configured)
		raw, err = common.Marshal(evidence)
		if err != nil {
			return ErrProbeUnavailable
		}
		result.ApprovalViewDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
		generation, err := common.Marshal(struct {
			Options  map[string]string `json:"options"`
			Referral ReferralConfig    `json:"referral"`
		}{expected, referral})
		if err != nil {
			return ErrProbeUnavailable
		}
		result.ConfigGeneration = fmt.Sprintf("sha256:%x", sha256.Sum256(generation))
		configuration, err := common.Marshal(struct {
			Online   bool   `json:"online_payment_enabled"`
			Store    bool   `json:"store_enabled"`
			Mode     string `json:"billing_mode"`
			Referral bool   `json:"referral_enabled"`
			Pool     bool   `json:"pool_enabled"`
		}{result.OnlinePaymentEnabled, result.StoreEnabled, result.BillingMode, result.ReferralEnabled, result.PoolEnabled})
		if err != nil {
			return ErrProbeUnavailable
		}
		result.ConfigDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(configuration))
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrRuntimeConfigMismatch) || errors.Is(err, ErrApprovalViewInvalid) || errors.Is(err, ErrProbeUnavailable) {
			return RuntimeIntegrityObservation{}, err
		}
		return RuntimeIntegrityObservation{}, ErrProbeUnavailable
	}
	return RuntimeIntegrityObservation{Report: result, ViewStateDigest: viewStateDigest}, nil
}
