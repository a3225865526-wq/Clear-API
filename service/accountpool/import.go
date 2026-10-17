package accountpool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	ImportMaxFiles        = 10
	ImportMaxFileBytes    = 256 * 1024
	ImportMaxTotalBytes   = 1024 * 1024
	ImportFormatCodexV1   = "codex-oauth-top-level-v1"
	ImportStatusImported  = "imported"
	ImportStatusDuplicate = "duplicate"
	ImportStatusRejected  = "rejected"
	ImportStatusFailed    = "failed"
)

// ImportError is a stable, non-secret failure code returned to the operator.
type ImportError struct{ Code string }

func (e *ImportError) Error() string  { return e.Code }
func importFailure(code string) error { return &ImportError{Code: code} }

// CodexCredential is the validated top-level Codex OAuth shape. Raw token
// material stays in Raw and is never included in a response or log.
type CodexCredential struct {
	AccountID    string
	Email        string
	PlanType     string
	Raw          []byte
	Digest       string
	IdentityKey  string
	IdentityMask string
	Format       string
}

type ImportResult struct {
	FileName     string `json:"file_name"`
	Status       string `json:"status"`
	Code         string `json:"code,omitempty"`
	AccountID    int    `json:"account_id,omitempty"`
	IdentityMask string `json:"identity_masked,omitempty"`
	Format       string `json:"format,omitempty"`
	RunStatus    string `json:"run_status,omitempty"`
	Admission    string `json:"admission,omitempty"`
	Capability   string `json:"capability,omitempty"`
	Retryable    bool   `json:"retryable"`
}

// ValidateCodexCredential accepts only the documented top-level Codex OAuth
// file. Duplicate keys, trailing data, nested collections, API keys and
// missing token pairs are rejected before any executor mutation.
func ValidateCodexCredential(raw []byte) (CodexCredential, error) {
	result := CodexCredential{}
	if len(raw) == 0 || len(raw) > ImportMaxFileBytes {
		return result, importFailure("FILE_TOO_LARGE")
	}
	if err := validateUniqueJSON(raw); err != nil {
		return result, importFailure("JSON_INVALID")
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return result, importFailure("JSON_INVALID")
	}
	if kind, _ := fields["type"].(string); kind != "codex" {
		return result, importFailure("PROVIDER_UNSUPPORTED")
	}
	for _, key := range []string{"accounts", "auths", "credentials", "api_key", "apiKey"} {
		if _, exists := fields[key]; exists {
			return result, importFailure("CREDENTIAL_SHAPE_UNSUPPORTED")
		}
	}
	for _, key := range []string{"access_token", "refresh_token"} {
		value, ok := fields[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return result, importFailure("CREDENTIAL_MISSING_TOKEN")
		}
	}
	accountID, _ := fields["account_id"].(string)
	chatGPTAccountID, _ := fields["chatgpt_account_id"].(string)
	accountID = strings.TrimSpace(accountID)
	chatGPTAccountID = strings.TrimSpace(chatGPTAccountID)
	if accountID == "" && chatGPTAccountID == "" {
		return result, importFailure("CREDENTIAL_MISSING_IDENTITY")
	}
	if accountID != "" && chatGPTAccountID != "" && accountID != chatGPTAccountID {
		return result, importFailure("CREDENTIAL_IDENTITY_MISMATCH")
	}
	if accountID == "" {
		accountID = chatGPTAccountID
	}
	email, _ := fields["email"].(string)
	email = strings.TrimSpace(email)
	if email == "" {
		return result, importFailure("CREDENTIAL_MISSING_EMAIL")
	}
	planType, _ := fields["plan_type"].(string)
	if planType == "" {
		planType, _ = fields["chatgpt_plan_type"].(string)
	}
	sum := sha256.Sum256(raw)
	result.AccountID = accountID
	result.Email = email
	result.PlanType = strings.TrimSpace(planType)
	result.Raw = append([]byte(nil), raw...)
	result.Digest = hex.EncodeToString(sum[:])
	result.IdentityKey = "codex:" + accountID
	result.IdentityMask = MaskIdentity(email)
	result.Format = ImportFormatCodexV1
	return result, nil
}

func validateUniqueJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := walkUniqueJSON(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func walkUniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("too deep")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid key")
			}
			if _, duplicate := seen[name]; duplicate {
				return errors.New("duplicate key")
			}
			seen[name] = struct{}{}
			if err := walkUniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkUniqueJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid delimiter")
	}
	_, err = decoder.Token()
	return err
}

// ImportCodexCredentials imports a bounded batch. Each item is independently
// settled. An executor/database failure triggers compensation so no
// unregistered credential is left behind.
func ImportCodexCredentials(ctx context.Context, ownerID int, files []ImportFile) []ImportResult {
	results := make([]ImportResult, 0, len(files))
	seen := map[string]struct{}{}
	for _, file := range files {
		result := ImportResult{FileName: file.Name, Status: ImportStatusRejected}
		credential, err := ValidateCodexCredential(file.Raw)
		if err != nil {
			result.Code = importCode(err)
			results = append(results, result)
			continue
		}
		result.Format = credential.Format
		result.IdentityMask = credential.IdentityMask
		if _, duplicate := seen[credential.IdentityKey]; duplicate {
			result.Status, result.Code = ImportStatusDuplicate, "DUPLICATE_IN_BATCH"
			results = append(results, result)
			continue
		}
		seen[credential.IdentityKey] = struct{}{}
		settled, dbErr := importOne(ctx, ownerID, credential)
		if dbErr != nil {
			result.Status, result.Code = ImportStatusFailed, importCode(dbErr)
			settled = result
		} else {
			settled.FileName = file.Name
		}
		results = append(results, settled)
	}
	return results
}

type ImportFile struct {
	Name string
	Raw  []byte
}

func importOne(ctx context.Context, ownerID int, credential CodexCredential) (ImportResult, error) {
	result := ImportResult{Status: ImportStatusFailed, IdentityMask: credential.IdentityMask,
		Format: credential.Format, Retryable: true}
	executor, err := executorFor(ExecutorCliproxyAPI)
	if err != nil {
		result.Code = "EXECUTOR_UNAVAILABLE"
		return result, err
	}
	uploader, ok := executor.(interface {
		ImportCredential(context.Context, CredentialImportRequest) (*CredentialImportResult, error)
		DeleteImportedCredential(context.Context, string) error
	})
	if !ok {
		result.Code = "EXECUTOR_IMPORT_UNSUPPORTED"
		return result, ErrPoolUnsupported
	}
	account, existing, err := reserveImportAccount(ownerID, credential)
	if err != nil {
		result.Code = importCode(err)
		if errors.Is(err, ErrPoolConflict) {
			result.Status = ImportStatusDuplicate
			result.Retryable = false
		}
		return result, err
	}
	if existing != nil {
		result.AccountID = existing.Id
		result.RunStatus = existing.RunStatus
		result.Status = ImportStatusDuplicate
		result.Code = "DUPLICATE_ACCOUNT"
		return result, nil
	}

	imported, err := uploader.ImportCredential(ctx, CredentialImportRequest{
		FileName: "codex-" + credential.Digest + ".json",
		Raw:      credential.Raw,
	})
	if err != nil {
		_ = deleteUnclaimedImportAccount(account.Id)
		result.Code = importCode(err)
		return result, err
	}
	if imported == nil || imported.CredentialRef == "" || imported.CredentialDigest == "" || imported.Provider != "codex" {
		_ = deleteUnclaimedImportAccount(account.Id)
		if imported != nil {
			_ = uploader.DeleteImportedCredential(context.Background(), imported.CredentialRef)
		}
		result.Code = "EXECUTOR_IMPORT_INVALID"
		return result, ErrPoolExecutorFailed
	}
	ref := imported.CredentialRef
	// The executor may derive a digest over normalized fields rather than the
	// raw file hash. Bind the pool row to the executor's canonical digest, and
	// never claim a model as admitted from the raw bytes alone.
	canonicalDigest := imported.CredentialDigest
	txErr := db().Transaction(func(tx *gorm.DB) error {
		return model.UpdateUpstreamPoolAccount(tx, account.Id, account.Version, map[string]any{
			"identity_masked":      credential.IdentityMask,
			"identity_fingerprint": IdentityFingerprint(credential.IdentityKey),
			"credential_ref":       ref,
			"run_status":           model.PoolRunStatusNotAdmitted,
			"last_refresh_at":      time.Now().Unix(),
			"last_error":           "",
		})
	})
	if txErr != nil {
		_ = uploader.DeleteImportedCredential(context.Background(), ref)
		_ = deleteUnclaimedImportAccount(account.Id)
		result.Code = importCode(txErr)
		return result, txErr
	}

	snapshot, snapErr := executor.(interface {
		PoolModelSnapshot(context.Context) (PoolExecutorModelSnapshot, error)
	}).PoolModelSnapshot(ctx)
	admission, capability := importAdmission(canonicalDigest, ref, snapshot, snapErr)
	result.Status = ImportStatusImported
	result.Code = ""
	result.AccountID = account.Id
	result.RunStatus = model.PoolRunStatusNotAdmitted
	result.Admission = admission
	result.Capability = capability
	result.Retryable = false
	return result, nil
}

func reserveImportAccount(ownerID int, credential CodexCredential) (*model.UpstreamPoolAccount, *model.UpstreamPoolAccount, error) {
	_ = ownerID
	fingerprint := IdentityFingerprint(credential.IdentityKey)
	var existing model.UpstreamPoolAccount
	err := db().Where("platform = ? AND executor_id = ? AND identity_fingerprint = ?",
		"codex", ExecutorCliproxyAPI, fingerprint).First(&existing).Error
	if err == nil {
		return nil, &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	account := &model.UpstreamPoolAccount{
		Platform: "codex", AccessType: AuthMethodOAuthBrowser,
		IdentityMasked: credential.IdentityMask, IdentityFingerprint: fingerprint,
		Note: "web JSON import", AdminStatus: model.PoolAdminStatusEnabled,
		RunStatus: model.PoolRunStatusAuthorizing, ExecutorId: ExecutorCliproxyAPI,
		Version: 1, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	if err := db().Create(account).Error; err != nil {
		return nil, nil, err
	}
	return account, nil, nil
}

func deleteUnclaimedImportAccount(id int) error {
	return db().Where("id = ? AND credential_ref = ''", id).Delete(&model.UpstreamPoolAccount{}).Error
}

func importAdmission(credentialDigest, authID string, snapshot PoolExecutorModelSnapshot, snapErr error) (string, string) {
	if snapErr != nil || authID == "" || credentialDigest == "" {
		return "unverified", "unverified"
	}
	trusted := false
	for _, identity := range snapshot.Identities {
		if identity.AuthID == authID && identity.Provider == "codex" &&
			identity.CredentialDigest == credentialDigest && identity.State == "trusted" {
			trusted = true
			break
		}
	}
	if !trusted {
		return "pending_trust", "unverified"
	}
	verified := 0
	for _, capability := range snapshot.Capabilities {
		if capability.AuthID == authID && capability.Provider == "codex" &&
			capability.CredentialDigest == credentialDigest && capability.State == "verified" &&
			capability.Model != "" {
			verified++
		}
	}
	if verified == 0 {
		return "trusted_no_model_grant", "unverified"
	}
	return "trusted", "verified"
}
func importCode(err error) string {
	var typed *ImportError
	if errors.As(err, &typed) {
		return typed.Code
	}
	switch {
	case errors.Is(err, ErrPoolConflict):
		return "DUPLICATE_ACCOUNT"
	case errors.Is(err, ErrPoolExecutorOffline):
		return "EXECUTOR_OFFLINE"
	case errors.Is(err, ErrPoolExecutorFailed):
		return "EXECUTOR_REJECTED"
	case errors.Is(err, ErrPoolUnsupported):
		return "EXECUTOR_IMPORT_UNSUPPORTED"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "IMPORT_CANCELLED"
	default:
		return "IMPORT_FAILED"
	}
}
