// Package accountpool implements the site-owner upstream account pool
// management layer. It is a registry and status-sync layer on top of
// executors that own credentials, refresh and scheduling; this package never
// stores, refreshes or schedules credentials itself.
//
// Invariants enforced here:
//   - admin status (owner pause) and run status are separate columns; re-auth,
//     completion and background sync updates never write the admin column, so
//     an owner pause cannot be overwritten by them;
//   - every account row update is guarded by an optimistic version, so a
//     stale writer gets ErrPoolConflict instead of silently winning;
//   - authorization tasks carry no secret fields and executor responses are
//     mapped to fixed codes, never passed through verbatim;
//   - successful authorization lands in run status not_admitted: capability
//     admission (platform + access type + model + adapter version) is a later
//     phase, so no account can join an external channel from here;
//   - authorization is refused until the executor instance is attested as a
//     dedicated, unreachable-by-channels deployment (the fixed CLIProxyAPI
//     hot-reloads fresh credentials into its scheduler with no quarantine);
//   - the new credential is correlated by diffing the executor listing
//     against the task baseline; ambiguous correlation fails closed and
//     never deletes anything.
package accountpool

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	AuthKindInitial = "initial"
	AuthKindReauth  = "reauth"
)

// maxAuthTaskTTL caps the executor-provided session lifetime for a single
// authorization round.
const maxAuthTaskTTL = 30 * time.Minute

// completeAuthPollCap bounds a single CompleteAuth call regardless of the
// task TTL so an HTTP request cannot hang for half an hour.
const completeAuthPollCap = 5 * time.Minute

type CreateInput struct {
	Platform   string `json:"platform"`
	AccessType string `json:"access_type"`
	ExecutorId string `json:"executor_id"`
	Note       string `json:"note"`
}

type StartAuthView struct {
	TaskId          int    `json:"task_id"`
	Method          string `json:"method"`
	AuthURL         string `json:"auth_url,omitempty"`
	VerificationURI string `json:"verification_uri,omitempty"`
	UserCode        string `json:"user_code,omitempty"`
	ExpiresAt       int64  `json:"expires_at"`
	AccountVersion  int    `json:"account_version"`
	// OneTime is true: the fields above are returned exactly once and are
	// not persisted anywhere; after page loss the owner cancels and restarts.
	OneTime bool `json:"one_time"`
}

type CompleteInput struct {
	CallbackURL string `json:"callback_url"`
	Code        string `json:"code"`
	State       string `json:"state"`
}

type AccountWithTasks struct {
	Account model.UpstreamPoolAccount    `json:"account"`
	Tasks   []model.UpstreamPoolAuthTask `json:"tasks"`
}

func db() *gorm.DB { return model.DB }

// --- queries -----------------------------------------------------------------

func ListAccounts(platform, adminStatus, runStatus string) ([]model.UpstreamPoolAccount, error) {
	query := db().Model(&model.UpstreamPoolAccount{})
	if platform != "" {
		query = query.Where("platform = ?", platform)
	}
	if adminStatus != "" {
		query = query.Where("admin_status = ?", adminStatus)
	}
	if runStatus != "" {
		query = query.Where("run_status = ?", runStatus)
	}
	var accounts []model.UpstreamPoolAccount
	if err := query.Order("id asc").Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func GetAccount(id int) (*AccountWithTasks, error) {
	var account model.UpstreamPoolAccount
	if err := db().First(&account, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	var tasks []model.UpstreamPoolAuthTask
	if err := db().Where("account_id = ?", id).Order("id desc").Limit(20).Find(&tasks).Error; err != nil {
		return nil, err
	}
	return &AccountWithTasks{Account: account, Tasks: tasks}, nil
}

// --- account lifecycle -------------------------------------------------------

func CreateAccount(ownerID int, input CreateInput) (*model.UpstreamPoolAccount, error) {
	_, access, ok := Lookup(input.Platform, input.AccessType)
	if !ok {
		return nil, ErrPoolInvalid
	}
	if access.Disabled {
		return nil, ErrPoolInvalid
	}
	executorId := input.ExecutorId
	if executorId == "" {
		executorId = access.ExecutorId
	}
	if executorId != access.ExecutorId {
		return nil, ErrPoolInvalid
	}
	if !executorRegistered(executorId) {
		return nil, ErrPoolInvalid
	}
	note := strings.TrimSpace(input.Note)
	if len([]rune(note)) > 255 {
		note = string([]rune(note)[:255])
	}
	account := &model.UpstreamPoolAccount{
		Platform:    input.Platform,
		AccessType:  input.AccessType,
		Note:        note,
		AdminStatus: model.PoolAdminStatusEnabled,
		RunStatus:   model.PoolRunStatusAwaitingAuth,
		ExecutorId:  executorId,
		Version:     1,
		CreatedAt:   time.Now().Unix(),
		UpdatedAt:   time.Now().Unix(),
	}
	if err := db().Create(account).Error; err != nil {
		return nil, err
	}
	return account, nil
}

func executorRegistered(id string) bool {
	for _, executor := range Executors() {
		if executor.Id == id {
			return true
		}
	}
	return false
}

// StartAuth opens one authorization round. The returned view carries one-time
// fields from the executor (device code / authorization URL) that are never
// persisted; only the task metadata below is stored.
func StartAuth(ownerID, accountID int, kind string) (*StartAuthView, error) {
	var account model.UpstreamPoolAccount
	if err := db().First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	hasCredential := account.CredentialRef != ""
	switch kind {
	case "", AuthKindInitial:
		kind = AuthKindInitial
		if hasCredential {
			return nil, ErrPoolInvalid
		}
	case AuthKindReauth:
		if !hasCredential {
			return nil, ErrPoolInvalid
		}
	default:
		return nil, ErrPoolInvalid
	}

	now := time.Now()
	if err := expireStaleTasks(accountID, now); err != nil {
		return nil, err
	}
	// Reclaim claims whose task is no longer pending (crash/process-exit
	// leftovers); the claim expiry itself covers the rest.
	if err := model.SweepUpstreamPoolClaims(db()); err != nil {
		return nil, err
	}
	var pending int64
	if err := db().Model(&model.UpstreamPoolAuthTask{}).
		Where("account_id = ? AND status = ? AND expires_at > ?", accountID, model.PoolTaskStatusPending, now.Unix()).
		Count(&pending).Error; err != nil {
		return nil, err
	}
	if pending > 0 {
		return nil, ErrPoolConflict
	}

	// Isolation gate: the fixed CLIProxyAPI v8.0.4 hot-reloads every new
	// credential into its scheduler immediately (no quarantine), so
	// authorization is refused until the operator attests a dedicated
	// executor instance that no channel can reach.
	if account.ExecutorId == ExecutorCliproxyAPI && !common.PoolCliproxyDedicated() {
		return nil, ErrPoolIsolationReq
	}
	// One pending authorization per executor is enforced by the atomic
	// executor claim (insert race across processes), not by a count: the
	// completion correlation is a listing diff and is only unambiguous when
	// no other task can create credentials on the same instance.

	executor, err := executorFor(account.ExecutorId)
	if err != nil {
		return nil, err
	}
	baseline, err := executor.ListCredentials(context.Background())
	if err != nil {
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": model.PoolRunStatusExecutorOffline,
			"last_error": executorErrorText(err),
		})
		return nil, err
	}
	result, err := executor.StartAuth(context.Background(), StartAuthRequest{
		Platform: account.Platform, AccessType: account.AccessType, Kind: kind,
	})
	if err != nil {
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": model.PoolRunStatusExecutorOffline,
			"last_error": executorErrorText(err),
		})
		return nil, err
	}

	expiresAt := result.ExpiresAt
	if expiresAt.IsZero() || expiresAt.Sub(now) > maxAuthTaskTTL {
		expiresAt = now.Add(maxAuthTaskTTL)
	}
	task := &model.UpstreamPoolAuthTask{
		AccountId:        account.Id,
		OwnerUserId:      ownerID,
		Platform:         account.Platform,
		AccessType:       account.AccessType,
		Kind:             kind,
		Method:           result.Method,
		ExecutorId:       account.ExecutorId,
		ExecutorStateRef: result.StateRef,
		BaselineRefs:     marshalBaseline(baseline),
		Status:           model.PoolTaskStatusPending,
		PrevRunStatus:    account.RunStatus,
		ExpiresAt:        expiresAt.Unix(),
		CreatedAt:        now.Unix(),
		UpdatedAt:        now.Unix(),
	}
	err = db().Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateUpstreamPoolAccount(tx, account.Id, account.Version, map[string]any{
			"run_status": model.PoolRunStatusAuthorizing,
			"last_error": "",
		}); err != nil {
			return err
		}
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		// Atomic executor claim: loses on any live claim regardless of which
		// process created it. Losing here rolls back the just-created task so
		// no unclaimable pending row lingers.
		released, claimErr := model.AcquireUpstreamPoolClaim(tx, account.ExecutorId, account.Id, task.Id, task.ExpiresAt)
		if claimErr != nil {
			return claimErr
		}
		if released {
			// A taken-over claim implies its old task expired; make that
			// explicit so no stale pending task survives beside the claim.
			if err := tx.Model(&model.UpstreamPoolAuthTask{}).
				Where("executor_id = ? AND status = ? AND expires_at <= ?", account.ExecutorId, model.PoolTaskStatusPending, now.Unix()).
				Updates(map[string]any{"status": model.PoolTaskStatusExpired, "fail_reason": model.PoolTaskFailExpired, "updated_at": now.Unix()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, model.ErrPoolConflict) {
			return nil, ErrPoolConflict
		}
		return nil, err
	}
	deviceURI := ""
	if result.Method == AuthMethodOAuthDevice {
		// The fixed protocol returns the verification URI in "url" for device
		// flows; expose it through both fields so the UI can render either
		// flow without re-reading the executor.
		deviceURI = result.AuthURL
	}
	return &StartAuthView{
		TaskId:          task.Id,
		Method:          result.Method,
		AuthURL:         result.AuthURL,
		VerificationURI: deviceURI,
		UserCode:        result.UserCode,
		ExpiresAt:       task.ExpiresAt,
		AccountVersion:  account.Version + 1,
		OneTime:         true,
	}, nil
}

func expireStaleTasks(accountID int, now time.Time) error {
	return db().Model(&model.UpstreamPoolAuthTask{}).
		Where("account_id = ? AND status = ? AND expires_at <= ?", accountID, model.PoolTaskStatusPending, now.Unix()).
		Updates(map[string]any{"status": model.PoolTaskStatusExpired, "fail_reason": model.PoolTaskFailExpired, "updated_at": now.Unix()}).Error
}

// CompleteAuth settles a pending authorization task. For browser flows the
// pasted callback URL/code is submitted (the executor then exchanges the code
// asynchronously); for device flows no submission exists. Completion is only
// accepted after the executor session reaches its terminal "completed" state.
// The new credential is then correlated by diffing the executor credential
// listing against the task's baseline snapshot — the fixed protocol has no
// state→file field, and ambiguous diffs fail closed without deleting anything.
func CompleteAuth(ownerID, accountID int, input CompleteInput) (*model.UpstreamPoolAccount, error) {
	var account model.UpstreamPoolAccount
	if err := db().First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	now := time.Now()
	var task model.UpstreamPoolAuthTask
	if err := db().Where("account_id = ? AND status = ?", accountID, model.PoolTaskStatusPending).
		Order("id desc").First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPoolTaskConsumed
		}
		return nil, err
	}
	if task.ExpiresAt <= now.Unix() {
		_ = expireStaleTasks(accountID, now)
		return nil, ErrPoolTaskExpired
	}
	if task.Platform != account.Platform {
		failTask(&task, model.PoolTaskFailPlatformMismatch, now)
		return nil, ErrPoolConflict
	}
	method := task.Method
	if method == "" {
		method = AuthMethodOAuthBrowser
	}
	code, state := "", ""
	if method != AuthMethodOAuthDevice {
		var err error
		code, state, err = parseCallback(input)
		if err != nil {
			return nil, err
		}
	}

	executor, err := executorFor(account.ExecutorId)
	if err != nil {
		return nil, err
	}
	deadline := now.Add(completeAuthPollCap)
	if task.ExpiresAt > 0 && time.Unix(task.ExpiresAt, 0).Before(deadline) {
		deadline = time.Unix(task.ExpiresAt, 0)
	}
	if err := executor.CompleteAuth(context.Background(), CompleteAuthRequest{
		Platform: account.Platform, AccessType: account.AccessType, Kind: task.Kind,
		Method: method, StateRef: task.ExecutorStateRef, Code: code, State: state,
		CallbackURL: input.CallbackURL, Deadline: deadline,
	}); err != nil {
		failTask(&task, model.PoolTaskFailExecutorError, now)
		_ = model.ReleaseUpstreamPoolClaim(db(), task.ExecutorId, task.Id)
		restore := task.PrevRunStatus
		if restore == "" {
			restore = model.PoolRunStatusAwaitingAuth
		}
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": restore,
			"last_error": executorErrorText(err),
		})
		return nil, err
	}

	entries, err := executor.ListCredentials(context.Background())
	if err != nil {
		failTask(&task, model.PoolTaskFailCorrelation, now)
		_ = model.ReleaseUpstreamPoolClaim(db(), task.ExecutorId, task.Id)
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": task.PrevRunStatus,
			"last_error": executorErrorText(err),
		})
		return nil, err
	}
	baseline := parseBaseline(task.BaselineRefs)
	credential, ok := correlateCredential(entries, baseline)
	if !ok {
		// Ambiguous or missing correlation: never delete anything (we cannot
		// prove which file the task created), never overwrite the account.
		failTask(&task, model.PoolTaskFailCorrelation, now)
		_ = model.ReleaseUpstreamPoolClaim(db(), task.ExecutorId, task.Id)
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": task.PrevRunStatus,
			"last_error": "correlation_failed",
		})
		return nil, ErrPoolCorrelationFail
	}
	identity := credential.Email
	credentialRef := credential.Name
	if identity == "" || credentialRef == "" {
		failTask(&task, model.PoolTaskFailCorrelation, now)
		return nil, ErrPoolCorrelationFail
	}
	masked := MaskIdentity(identity)
	fingerprint := IdentityFingerprint(identity)

	// Re-auth to a different upstream account must never overwrite the
	// original account. The decision uses the stable fingerprint; masked
	// display values are not identity (two distinct accounts can share one).
	if account.IdentityFingerprint != "" && account.IdentityFingerprint != fingerprint {
		failTask(&task, model.PoolTaskFailIdentityMismatch, now)
		restore := task.PrevRunStatus
		if restore == "" {
			restore = model.PoolRunStatusReauthRequired
		}
		lastError := "identity_mismatch(submitted=" + masked + ")"
		// The freshly created credential is now an orphan. Deleting it is
		// only allowed because correlation proved this task created it and no
		// pool account owns that reference; the original credential is never
		// touched.
		if cleanupErr := cleanupTaskCredential(executor, credentialRef, baseline, account.Id); cleanupErr != nil {
			lastError += ";" + cleanupErrorText(cleanupErr)
		}
		_ = model.ReleaseUpstreamPoolClaim(db(), task.ExecutorId, task.Id)
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": restore,
			"last_error": lastError,
		})
		return nil, ErrPoolIdentityMismatch
	}
	// One credential reference is managed by exactly one pool account. The
	// just-created file may collide with another account's reference (e.g. an
	// identical upstream identity); in that case nothing is deleted — the
	// file may be that other account's credential — and the conflict is
	// surfaced for manual resolution.
	var taken int64
	if err := db().Model(&model.UpstreamPoolAccount{}).
		Where("executor_id = ? AND credential_ref = ? AND id <> ?", account.ExecutorId, credentialRef, account.Id).
		Count(&taken).Error; err != nil {
		return nil, err
	}
	if taken > 0 {
		failTask(&task, model.PoolTaskFailCredentialUsed, now)
		_ = model.ReleaseUpstreamPoolClaim(db(), task.ExecutorId, task.Id)
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": task.PrevRunStatus,
			"last_error": "credential_ref_taken",
		})
		return nil, ErrPoolConflict
	}

	var updated model.UpstreamPoolAccount
	txErr := db().Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateUpstreamPoolAccount(tx, account.Id, account.Version, map[string]any{
			"identity_masked":      masked,
			"identity_fingerprint": fingerprint,
			"credential_ref":       credentialRef,
			"run_status":           model.PoolRunStatusNotAdmitted,
			"last_refresh_at":      now.Unix(),
			"last_error":           "",
		}); err != nil {
			return err
		}
		result := tx.Model(&model.UpstreamPoolAuthTask{}).
			Where("id = ? AND status = ?", task.Id, model.PoolTaskStatusPending).
			Updates(map[string]any{"status": model.PoolTaskStatusCompleted, "updated_at": now.Unix()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPoolTaskConsumed
		}
		if err := tx.Where("executor_id = ? AND task_id = ?", task.ExecutorId, task.Id).Delete(&model.UpstreamPoolAuthClaim{}).Error; err != nil {
			return err
		}
		if err := tx.First(&updated, account.Id).Error; err != nil {
			return err
		}
		return nil
	})
	if txErr != nil {
		// The local commit lost (stale version / concurrent writer / injected
		// failure): the executor now holds an unreferenced credential. Clean
		// it up only when cleanupTaskCredential can prove it is new in the
		// baseline and unowned; never report success.
		if cleanupErr := cleanupTaskCredential(executor, credentialRef, baseline, account.Id); cleanupErr != nil {
			_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
				"last_error": cleanupErrorText(cleanupErr),
			})
		}
		_ = model.ReleaseUpstreamPoolClaim(db(), task.ExecutorId, task.Id)
		failTask(&task, model.PoolTaskFailExecutorError, now)
		switch {
		case errors.Is(txErr, ErrPoolTaskConsumed):
			return nil, ErrPoolTaskConsumed
		case errors.Is(txErr, model.ErrPoolConflict):
			return nil, ErrPoolConflict
		}
		return nil, txErr
	}
	// Defense in depth for isolation: park the credential out of the
	// executor scheduler until admission (phase 3) re-enables it. Failure
	// does not invalidate the authorization but is recorded.
	if pauseErr := executor.PauseCredential(context.Background(), credentialRef); pauseErr != nil {
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, updated.Version, map[string]any{
			"last_error": "post_auth_disable_failed",
		})
		if err := db().First(&updated, account.Id).Error; err != nil {
			return nil, err
		}
	}
	return &updated, nil
}

// correlateCredential diffs the current executor credential listing against
// the task baseline. Only NEW names can prove task ownership: an updated
// timestamp on an existing file proves nothing — background token refresh
// mutates it too — so in-place refreshes never correlate and completion
// fails closed. Exactly one new name is a reliable match; zero or several
// fail closed.
func correlateCredential(entries []CredentialEntry, baseline map[string]int64) (*CredentialEntry, bool) {
	var candidates []*CredentialEntry
	for i := range entries {
		entry := &entries[i]
		if _, exists := baseline[entry.Name]; !exists {
			candidates = append(candidates, entry)
		}
	}
	if len(candidates) != 1 {
		return nil, false
	}
	return candidates[0], true
}

var errCleanupRefused = errors.New("orphan credential cleanup refused: ownership not provable")

func cleanupErrorText(err error) string {
	if errors.Is(err, errCleanupRefused) {
		return "cleanup_refused"
	}
	return "orphan_cleanup_failed"
}

// cleanupTaskCredential deletes an executor credential ONLY when it can be
// proven to have been created by the current task: its reference must be new
// relative to the task baseline (never a baseline entry, never the account's
// pre-existing credential) and must not be referenced by any pool account
// row. Anything unprovable is kept and reported for manual handling.
func cleanupTaskCredential(executor ExecutorClient, credentialRef string, baseline map[string]int64, accountID int) error {
	if credentialRef == "" {
		return errCleanupRefused
	}
	if _, existed := baseline[credentialRef]; existed {
		return errCleanupRefused
	}
	owned, err := credentialRefOwned(credentialRef, accountID)
	if err != nil {
		return err
	}
	if owned {
		return errCleanupRefused
	}
	return executor.DeleteCredential(context.Background(), credentialRef)
}

func marshalBaseline(entries []CredentialEntry) string {
	baseline := map[string]int64{}
	for _, entry := range entries {
		baseline[entry.Name] = entry.UpdatedAt
	}
	encoded, err := common.Marshal(baseline)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func parseBaseline(raw string) map[string]int64 {
	baseline := map[string]int64{}
	if raw == "" {
		return baseline
	}
	_ = common.Unmarshal([]byte(raw), &baseline)
	return baseline
}

// credentialRefOwned reports whether any other pool account row already
// manages the given executor credential reference.
func credentialRefOwned(credentialRef string, accountID int) (bool, error) {
	var count int64
	err := db().Model(&model.UpstreamPoolAccount{}).
		Where("credential_ref = ? AND id <> ?", credentialRef, accountID).
		Count(&count).Error
	return count > 0, err
}

// parseCallback extracts code/state locally from a pasted callback URL. The
// URL is never fetched; any address in it is irrelevant beyond parsing.
func parseCallback(input CompleteInput) (code, state string, err error) {
	if input.CallbackURL != "" {
		parsed, parseErr := url.Parse(input.CallbackURL)
		if parseErr != nil || parsed.Scheme == "" {
			return "", "", ErrPoolInvalid
		}
		query := parsed.Query()
		code, state = query.Get("code"), query.Get("state")
		// Claude-style pastes may carry "code#state": the fragment holds the
		// state either as a query string or as a raw token.
		if state == "" {
			if fragment := parsed.Fragment; fragment != "" {
				if values, fragmentErr := url.ParseQuery(fragment); fragmentErr == nil && values.Get("state") != "" {
					state = values.Get("state")
				} else {
					state = fragment
				}
			}
		}
	} else {
		code, state = input.Code, input.State
	}
	if code == "" {
		return "", "", ErrPoolInvalid
	}
	return code, state, nil
}

func failTask(task *model.UpstreamPoolAuthTask, reason string, now time.Time) {
	db().Model(&model.UpstreamPoolAuthTask{}).
		Where("id = ? AND status = ?", task.Id, model.PoolTaskStatusPending).
		Updates(map[string]any{"status": model.PoolTaskStatusFailed, "fail_reason": reason, "updated_at": now.Unix()})
}

// executorErrorText maps executor failures to fixed, secret-free texts.
func executorErrorText(err error) string {
	switch {
	case errors.Is(err, ErrPoolExecutorOffline):
		return "executor_offline"
	case errors.Is(err, ErrPoolUnsupported):
		return "executor_unsupported"
	case errors.Is(err, ErrPoolCorrelationFail):
		return "correlation_failed"
	default:
		return "executor_error"
	}
}

// CancelAuth cancels a pending authorization task. The executor session state
// is checked first: a session that already completed has produced a
// credential, which is cleaned up only when the baseline diff correlates it
// unambiguously and no pool account owns it.
func CancelAuth(ownerID, accountID, taskID int) error {
	var task model.UpstreamPoolAuthTask
	if err := db().Where("id = ? AND account_id = ?", taskID, accountID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPoolNotFound
		}
		return err
	}
	if task.Status != model.PoolTaskStatusPending {
		return ErrPoolTaskConsumed
	}
	now := time.Now()
	if executor, err := executorFor(task.ExecutorId); err == nil {
		switch state, statusErr := executor.AuthStatus(context.Background(), task.ExecutorStateRef); {
		case statusErr == nil && state == AuthStateCompleted:
			// The authorization finished server-side while the task sat
			// pending: the new credential is an orphan candidate, cleaned up
			// only when provably created by this task and unowned — and only
			// while the task is still pending, so a cancel racing a winning
			// completion never deletes a credential the completion just
			// claimed.
			var fresh model.UpstreamPoolAuthTask
			if err := db().First(&fresh, task.Id).Error; err == nil && fresh.Status == model.PoolTaskStatusPending {
				if entries, listErr := executor.ListCredentials(context.Background()); listErr == nil {
					if credential, ok := correlateCredential(entries, parseBaseline(task.BaselineRefs)); ok {
						_ = cleanupTaskCredential(executor, credential.Name, parseBaseline(task.BaselineRefs), accountID)
					}
				}
			}
		case statusErr == nil && state == AuthStateWaiting:
			_ = executor.CancelAuth(context.Background(), task.ExecutorStateRef)
		}
	}
	result := db().Model(&task).Where("id = ? AND status = ?", task.Id, model.PoolTaskStatusPending).
		Updates(map[string]any{"status": model.PoolTaskStatusCancelled, "updated_at": now.Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// A concurrent completion settled the task first; the caller must not
		// believe the cancel happened.
		return ErrPoolTaskConsumed
	}
	_ = model.ReleaseUpstreamPoolClaim(db(), task.ExecutorId, task.Id)
	var account model.UpstreamPoolAccount
	if err := db().First(&account, accountID).Error; err == nil && account.RunStatus == model.PoolRunStatusAuthorizing {
		restore := task.PrevRunStatus
		if restore == "" {
			restore = model.PoolRunStatusAwaitingAuth
		}
		_ = model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, map[string]any{
			"run_status": restore,
		})
	}
	return nil
}

// PauseAccount parks the account: the executor credential is disabled first
// (remote pause succeeds before any local write), then the admin column is
// flipped under the optimistic version. Remote failure leaves the local row
// untouched — a pause is never faked. A local version conflict after a
// successful remote pause is resolved by re-reading and applying once: the
// admin column has exactly one authorized writer (user ID 1), so this only
// collapses a double-click race; if even that fails the conflict is returned
// and status sync will surface the residual difference.
func PauseAccount(ownerID, accountID, expectedVersion int) (*model.UpstreamPoolAccount, error) {
	return setAdminStatus(accountID, expectedVersion, model.PoolAdminStatusPaused)
}

// ResumeAccount lifts a pause. It is gated on admission: in phase two no
// account is admitted, so resume returns an explicit error instead of
// silently re-enabling a non-admitted credential.
func ResumeAccount(ownerID, accountID, expectedVersion int) (*model.UpstreamPoolAccount, error) {
	var account model.UpstreamPoolAccount
	if err := db().First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	if !admitted(&account) {
		return nil, ErrPoolNotAdmitted
	}
	return setAdminStatus(accountID, expectedVersion, model.PoolAdminStatusEnabled)
}

// admitted reports whether the account passed capability admission. Phase two
// has no admission path, so only the reserved "available" run status counts.
func admitted(account *model.UpstreamPoolAccount) bool {
	return account.RunStatus == model.PoolRunStatusAvailable
}

func setAdminStatus(accountID, expectedVersion int, status string) (*model.UpstreamPoolAccount, error) {
	var account model.UpstreamPoolAccount
	if err := db().First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	pause := status == model.PoolAdminStatusPaused
	if pause && account.CredentialRef != "" {
		executor, err := executorFor(account.ExecutorId)
		if err != nil {
			return nil, err
		}
		var remoteErr error
		if pause {
			remoteErr = executor.PauseCredential(context.Background(), account.CredentialRef)
		} else {
			remoteErr = executor.ResumeCredential(context.Background(), account.CredentialRef)
		}
		if remoteErr != nil {
			return nil, remoteErr
		}
	}
	if err := model.UpdateUpstreamPoolAccount(db(), accountID, expectedVersion, map[string]any{
		"admin_status": status,
	}); err != nil {
		if !errors.Is(err, model.ErrPoolConflict) {
			return nil, err
		}
		// Version conflict after a successful remote flip: re-read and apply
		// once with the fresh version (single authorized writer of the admin
		// column; see PauseAccount). Any residual failure is reported.
		if err := db().First(&account, accountID).Error; err != nil {
			return nil, err
		}
		if err := model.UpdateUpstreamPoolAccount(db(), accountID, account.Version, map[string]any{
			"admin_status": status,
		}); err != nil {
			return nil, err
		}
	}
	if err := db().First(&account, accountID).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// DeleteAccount removes the executor credential first; only after the
// executor confirms is the local record deleted, so no orphaned credential
// stays schedulable behind a deleted UI row.
func DeleteAccount(ownerID, accountID int) error {
	var account model.UpstreamPoolAccount
	if err := db().First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPoolNotFound
		}
		return err
	}
	if account.CredentialRef != "" {
		executor, err := executorFor(account.ExecutorId)
		if err != nil {
			return err
		}
		if err := executor.DeleteCredential(context.Background(), account.CredentialRef); err != nil {
			return err
		}
	}
	return db().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("account_id = ?", accountID).Delete(&model.UpstreamPoolAuthTask{}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", accountID).Delete(&model.UpstreamPoolAuthClaim{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.UpstreamPoolAccount{}, accountID).Error
	})
}

// SyncAccount refreshes run status from the executor. It never writes the
// admin status column, so a pause survives any background refresh.
func SyncAccount(ownerID, accountID int) (*model.UpstreamPoolAccount, error) {
	var account model.UpstreamPoolAccount
	if err := db().First(&account, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	executor, err := executorFor(account.ExecutorId)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	columns := map[string]any{"last_refresh_at": now.Unix()}
	if err := executor.Health(context.Background()); err != nil {
		columns["run_status"] = model.PoolRunStatusExecutorOffline
		columns["last_error"] = executorErrorText(err)
		if err := model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, columns); err != nil {
			return nil, err
		}
	} else if account.CredentialRef == "" {
		if account.RunStatus != model.PoolRunStatusAuthorizing {
			columns["run_status"] = model.PoolRunStatusAwaitingAuth
		}
		if err := model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, columns); err != nil {
			return nil, err
		}
	} else {
		status, err := executor.CredentialStatus(context.Background(), account.CredentialRef)
		if err != nil {
			columns["last_error"] = executorErrorText(err)
			if err := model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, columns); err != nil {
				return nil, err
			}
		} else {
			columns["last_error"] = ""
			switch status.State {
			case CredentialStateActive, CredentialStateDisabled:
				// Active or parked-by-admission credentials are both valid;
				// admission (phase 3) is still pending.
				columns["run_status"] = model.PoolRunStatusNotAdmitted
			case CredentialStateInvalid:
				columns["run_status"] = model.PoolRunStatusReauthRequired
			case CredentialStateCooldown:
				columns["run_status"] = model.PoolRunStatusCooldown
			}
			if err := model.UpdateUpstreamPoolAccount(db(), account.Id, account.Version, columns); err != nil {
				return nil, err
			}
		}
	}
	if err := db().First(&account, accountID).Error; err != nil {
		return nil, err
	}
	return &account, nil
}
