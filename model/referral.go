package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ReferralOptionKey = "clearapi.referral"

// The option row is the cross-process gate. Every payout and gate update locks
// the same row; a disable therefore cannot race a committed payout.
type ReferralConfig struct {
	InvitesEnabled bool   `json:"invites_enabled"`
	RewardsEnabled bool   `json:"rewards_enabled"`
	Mode           string `json:"mode"`
	BasisPoints    int64  `json:"basis_points"`
	FixedQuota     int64  `json:"fixed_quota"`
	PerRewardCap   int64  `json:"per_reward_cap"`
	LifetimeCap    int64  `json:"lifetime_cap"`
	Version        int64  `json:"version"`
	Epoch          int64  `json:"epoch"`
}

func DefaultReferralConfig() ReferralConfig {
	return ReferralConfig{InvitesEnabled: true, Mode: "percentage", BasisPoints: 1000,
		FixedQuota: 500000, PerRewardCap: 50000000, LifetimeCap: 500000000, Version: 1, Epoch: 1}
}

func (cfg ReferralConfig) Validate() error {
	if cfg.Mode != "percentage" && cfg.Mode != "fixed" {
		return errors.New("invalid reward mode")
	}
	if cfg.BasisPoints < 0 || cfg.BasisPoints > 10000 {
		return errors.New("percentage must be between 0 and 100 with at most two decimals")
	}
	if cfg.FixedQuota < 0 || cfg.PerRewardCap <= 0 || cfg.LifetimeCap <= 0 {
		return errors.New("invalid reward limits")
	}
	for _, q := range []int64{cfg.FixedQuota, cfg.PerRewardCap, cfg.LifetimeCap} {
		if q > int64(common.MaxWalletQuota) {
			return errors.New("reward limit exceeds wallet ceiling")
		}
	}
	return nil
}

func InitReferralConfig() error {
	raw, err := common.Marshal(DefaultReferralConfig())
	if err != nil {
		return err
	}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: ReferralOptionKey, Value: string(raw)}).Error
}

func readReferralConfig(tx *gorm.DB, lock bool) (ReferralConfig, error) {
	if !tx.Migrator().HasTable(&Option{}) {
		return DefaultReferralConfig(), nil
	}
	var row Option
	query := tx
	if lock {
		query = lockForUpdate(tx)
	}
	err := query.Where(commonKeyCol+" = ?", ReferralOptionKey).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultReferralConfig(), nil
	}
	if err != nil {
		return ReferralConfig{}, err
	}
	var cfg ReferralConfig
	if err = common.UnmarshalJsonStr(row.Value, &cfg); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func GetReferralConfig() (ReferralConfig, error) { return readReferralConfig(DB, false) }

func validateRegistrationInviter(tx *gorm.DB, user *User, inviterID int) error {
	cfg, err := readReferralConfig(tx, false)
	if err != nil {
		return err
	}
	user.InviterId = 0 // Never trust a caller-supplied relation independently.
	if !cfg.InvitesEnabled || inviterID == 0 {
		return nil
	}
	if user.Id > 0 && user.Id == inviterID {
		return errors.New("self invitation is forbidden")
	}
	var inviter User
	if err = tx.Select("id").Where("id = ? AND status = ?", inviterID, common.UserStatusEnabled).First(&inviter).Error; err != nil {
		return errors.New("invalid inviter")
	}
	user.InviterId = inviter.Id
	return nil
}

func EnsureReferralCode(id int) (string, error) {
	var user User
	if err := DB.Select("id", "aff_code").First(&user, id).Error; err != nil {
		return "", err
	}
	if user.AffCode != "" {
		return user.AffCode, nil
	}
	// Unique index remains the final guard; collisions retry with a new random code.
	for range 3 {
		code := common.GetRandomString(16)
		result := DB.Model(&User{}).Where("id = ? AND (aff_code IS NULL OR aff_code = ?)", id, "").Update("aff_code", code)
		if result.Error != nil {
			continue
		}
		if err := DB.Select("aff_code").First(&user, id).Error; err != nil {
			return "", err
		}
		if user.AffCode != "" {
			return user.AffCode, nil
		}
	}
	return "", errors.New("unable to allocate invitation code")
}

// Optimistic version validation prevents stale settings forms from undoing an
// emergency disable. A disable increments Epoch; re-enable never resumes old work.
func SaveReferralConfig(cfg ReferralConfig, actor int) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		old, err := readReferralConfig(tx, true)
		if err != nil {
			return err
		}
		if cfg.Version != old.Version {
			return errors.New("settings changed; reload before saving")
		}
		cfg.Version = old.Version + 1
		cfg.Epoch = old.Epoch
		if old.RewardsEnabled && !cfg.RewardsEnabled {
			cfg.Epoch++
		}
		raw, err := common.Marshal(cfg)
		if err != nil {
			return err
		}
		result := tx.Model(&Option{}).Where(commonKeyCol+" = ?", ReferralOptionKey).Update("value", string(raw))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("referral gate is unavailable")
		}
		if !cfg.RewardsEnabled {
			if err = tx.Model(&ReferralReward{}).Where("status = ?", "pending").Update("status", "paused").Error; err != nil {
				return err
			}
		}
		return tx.Create(&ReferralAudit{ActorID: actor, Action: "settings", Details: string(raw), CreatedTime: common.GetTimestamp()}).Error
	})
}

type ReferralReward struct {
	ID           int    `json:"id"`
	RedemptionID int    `json:"redemption_id" gorm:"uniqueIndex;not null"`
	InviterID    int    `json:"-" gorm:"index"`
	InviteeID    int    `json:"-"`
	SourceQuota  int64  `json:"source_quota" gorm:"type:bigint"`
	RewardQuota  int64  `json:"reward_quota" gorm:"type:bigint"`
	RuleSnapshot string `json:"rule_snapshot" gorm:"type:text"`
	GateEpoch    int64  `json:"-" gorm:"type:bigint"`
	Status       string `json:"status" gorm:"type:varchar(24);index"`
	CreatedTime  int64  `json:"created_time" gorm:"type:bigint"`
	PaidTime     int64  `json:"paid_time" gorm:"type:bigint"`
	Attempts     int    `json:"attempts"`
	LastError    string `json:"last_error" gorm:"type:varchar(128)"`
}

// An independent main-database ledger, atomic with quota and reward state even
// when the ordinary log database is configured on a separate server.
type ReferralLedger struct {
	ID          int
	RewardID    int   `gorm:"uniqueIndex;not null"`
	UserID      int   `gorm:"index"`
	Quota       int64 `gorm:"type:bigint"`
	CreatedTime int64 `gorm:"type:bigint"`
}

type ReferralAudit struct {
	ID          int    `json:"id"`
	ActorID     int    `json:"actor_id"`
	Action      string `json:"action" gorm:"type:varchar(32)"`
	Details     string `json:"details" gorm:"type:text"`
	CreatedTime int64  `json:"created_time" gorm:"type:bigint"`
}

func CalculateReferralReward(quota int64, cfg ReferralConfig) (int64, error) {
	if err := cfg.Validate(); err != nil {
		return 0, err
	}
	if quota <= 0 {
		return 0, nil
	}
	amount := cfg.FixedQuota
	if cfg.Mode == "percentage" {
		n := new(big.Int).Mul(big.NewInt(quota), big.NewInt(cfg.BasisPoints))
		n.Div(n, big.NewInt(10000)) // floor to integer Quota; no floating point.
		if !n.IsInt64() {
			return 0, errors.New("reward overflow")
		}
		amount = n.Int64()
	}
	if amount > cfg.PerRewardCap {
		return 0, nil
	} // reject, never silently clip.
	return amount, nil
}

// Store eligibility uses frozen issuance facts, not the current product revision.
// Evidence samples and orphaned/mismatched codes can never earn sale rewards.
func storeCodeAllowsReferral(tx *gorm.DB, code *Redemption) (bool, error) {
	if code.BatchId <= 0 || code.FaceValueCents <= 0 || code.Quota <= 0 || code.Type != RedemptionTypeBalance {
		return false, nil
	}
	var count int64
	err := tx.Model(&RedemptionBatch{}).Where("id = ? AND source = ? AND type = ? AND evidence_only = ? AND face_value_cents = ? AND quota = ?",
		code.BatchId, RedemptionSourceStore, RedemptionTypeBalance, false, code.FaceValueCents, code.Quota).Count(&count).Error
	return count == 1, err
}

// Called only inside a successful redemption transaction. No history scan.
func queueReferralReward(tx *gorm.DB, code *Redemption, userID int, cfg ReferralConfig) error {
	if !cfg.RewardsEnabled || code.ReferralKind != "paid" || code.Quota <= 0 {
		return nil
	}
	switch code.Source {
	case RedemptionSourceStore:
		eligible, err := storeCodeAllowsReferral(tx, code)
		if err != nil || !eligible {
			return err
		}
	case RedemptionSourceLegacy, RedemptionSourceAdmin:
	default:
		return nil
	}
	var invitee, inviter User
	if err := tx.Select("id", "inviter_id").First(&invitee, userID).Error; err != nil {
		return err
	}
	if invitee.InviterId <= 0 || invitee.InviterId == userID {
		return nil
	}
	err := tx.Select("id").Where("id = ? AND status = ?", invitee.InviterId, common.UserStatusEnabled).First(&inviter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	q, err := CalculateReferralReward(int64(code.Quota), cfg)
	if err != nil {
		return err
	}
	if q == 0 {
		return nil
	}
	raw, err := common.Marshal(cfg)
	if err != nil {
		return err
	}
	return tx.Create(&ReferralReward{RedemptionID: code.Id, InviterID: inviter.Id, InviteeID: userID,
		SourceQuota: int64(code.Quota), RewardQuota: q, RuleSnapshot: string(raw), GateEpoch: cfg.Epoch,
		Status: "pending", CreatedTime: common.GetTimestamp()}).Error
}

func PayReferralReward(id int) error {
	var creditedUser, creditedQuota int
	err := DB.Transaction(func(tx *gorm.DB) error {
		cfg, err := readReferralConfig(tx, true)
		if err != nil {
			return err
		}
		var reward ReferralReward
		if err = lockForUpdate(tx).First(&reward, id).Error; err != nil {
			return err
		}
		if reward.Status != "pending" {
			return nil
		}
		if !cfg.RewardsEnabled || reward.GateEpoch != cfg.Epoch {
			return tx.Model(&reward).Update("status", "paused").Error
		}
		var snapshot ReferralConfig
		if err = common.UnmarshalJsonStr(reward.RuleSnapshot, &snapshot); err != nil {
			return err
		}
		if err = snapshot.Validate(); err != nil {
			return err
		}
		cap := min(snapshot.LifetimeCap, cfg.LifetimeCap)
		q := reward.RewardQuota
		if q <= 0 || q > min(snapshot.PerRewardCap, cfg.PerRewardCap) || q > cap {
			return errors.New("reward requires limit review")
		}
		// CAS is also required on SQLite (FOR UPDATE is unavailable there).
		result := tx.Model(&ReferralReward{}).Where("id = ? AND status = ?", id, "pending").Update("status", "paying")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		var user User
		if err = lockForUpdate(tx).Select("id", "status", "referral_paid_quota").First(&user, reward.InviterID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return errors.New("inviter unavailable")
		}
		result = tx.Model(&User{}).Where("id = ? AND status = ? AND referral_paid_quota <= ?", user.Id, common.UserStatusEnabled, cap-q).
			Update("referral_paid_quota", gorm.Expr("referral_paid_quota + ?", q))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("lifetime reward limit reached")
		}
		if err = creditTopUpQuota(tx, user.Id, int(q), nil); err != nil {
			return err
		}
		if err = tx.Create(&ReferralLedger{RewardID: id, UserID: user.Id, Quota: q, CreatedTime: common.GetTimestamp()}).Error; err != nil {
			return err
		}
		if err = tx.Model(&reward).Updates(map[string]any{"status": "paid", "paid_time": common.GetTimestamp(), "last_error": ""}).Error; err != nil {
			return err
		}
		creditedUser, creditedQuota = user.Id, int(q)
		return nil
	})
	if err != nil {
		// Safe error category only, never persist driver messages or card keys.
		DB.Model(&ReferralReward{}).Where("id = ? AND status = ?", id, "pending").Updates(map[string]any{"attempts": gorm.Expr("attempts + 1"), "last_error": "payout_failed"})
		return err
	}
	if creditedQuota > 0 {
		syncCreditUserQuotaCache(creditedUser, creditedQuota, "referral reward")
	}
	return nil
}

// Explicit, audited human reconciliation; only paused rows may be resumed.
func ResumeReferralReward(id, actor int, reason string) error {
	if len(reason) < 5 || len(reason) > 256 {
		return errors.New("review reason must be 5-256 characters")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		cfg, err := readReferralConfig(tx, true)
		if err != nil {
			return err
		}
		if !cfg.RewardsEnabled {
			return errors.New("rewards are disabled")
		}
		r := tx.Model(&ReferralReward{}).Where("id = ? AND status = ?", id, "paused").Updates(map[string]any{"status": "pending", "gate_epoch": cfg.Epoch})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return errors.New("reward is not paused")
		}
		return tx.Create(&ReferralAudit{ActorID: actor, Action: "resume", Details: fmt.Sprintf("reward=%d reason=%s", id, reason), CreatedTime: common.GetTimestamp()}).Error
	})
}

func RunReferralRewards() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		var ids []int
		if err := DB.Model(&ReferralReward{}).Where("status = ?", "pending").Order("attempts ASC, id ASC").Limit(100).Pluck("id", &ids).Error; err == nil {
			for _, id := range ids {
				if err := PayReferralReward(id); err != nil {
					common.SysError(fmt.Sprintf("referral payout %d failed; retained for retry", id))
				}
			}
		}
		<-ticker.C
	}
}

func SetRedemptionReferralKind(ids []int, kind, reason string, actor int) error {
	if len(ids) == 0 || len(ids) > 100 || len(reason) < 5 || len(reason) > 256 {
		return errors.New("invalid qualification request")
	}
	if kind != "paid" && kind != "free" && kind != "test" && kind != "compensation" {
		return errors.New("invalid qualification kind")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		// Serialize against redemption before changing eligibility.
		if _, err := readReferralConfig(tx, true); err != nil {
			return err
		}
		var codes []Redemption
		if err := lockForUpdate(tx).Where("id IN ?", ids).Find(&codes).Error; err != nil {
			return err
		}
		seen := map[int]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		if len(codes) != len(seen) {
			return errors.New("code not found")
		}
		for _, code := range codes {
			if code.Status == common.RedemptionCodeStatusUsed || code.Type != RedemptionTypeBalance {
				return errors.New("only unused balance codes can be qualified")
			}
			switch code.Source {
			case RedemptionSourceStore:
				if code.Status != common.RedemptionCodeStatusEnabled || (code.ExpiredTime != 0 && code.ExpiredTime < common.GetTimestamp()) {
					return errors.New("only active unused store codes can be qualified")
				}
				if kind == "paid" {
					eligible, err := storeCodeAllowsReferral(tx, &code)
					if err != nil {
						return err
					}
					if !eligible {
						return errors.New("store evidence samples and mismatched codes cannot earn rewards")
					}
				}
			case RedemptionSourceLegacy, RedemptionSourceAdmin:
			default:
				return errors.New("unsupported code source for reward qualification")
			}
		}
		if err := tx.Model(&Redemption{}).Where("id IN ?", ids).Update("referral_kind", kind).Error; err != nil {
			return err
		}
		raw, err := common.Marshal(map[string]any{"ids": ids, "kind": kind, "reason": reason})
		if err != nil {
			return err
		}
		return tx.Create(&ReferralAudit{ActorID: actor, Action: "qualification", Details: string(raw), CreatedTime: common.GetTimestamp()}).Error
	})
}

var ErrReferralStockChanged = errors.New("Inventory changed. Check again before confirming.")

type ReferralStockBatch struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	ProductID       string `json:"product_id"`
	ProductRevision int64  `json:"product_revision"`
	Count           int    `json:"count"`
}

type ReferralStockPreview struct {
	Count   int                  `json:"count"`
	Batches []ReferralStockBatch `json:"batches"`
	Digest  string               `json:"digest"`
}

type referralStockCandidate struct {
	ID              int
	BatchID         int
	Name            string
	ProductID       string
	ProductRevision int64
	ApprovalID      int
	DeliveryBinding string
	FaceValueCents  int64
	Quota           int
}

// Only unclassified formal inventory is eligible. Explicit exclusions and
// historical redemptions are never reclassified by the bulk operation.
func referralStockCandidates(tx *gorm.DB, locking bool) ([]referralStockCandidate, error) {
	if locking {
		tx = lockForUpdate(tx)
	}
	rows := make([]referralStockCandidate, 0)
	now := common.GetTimestamp()
	err := tx.Model(&Redemption{}).
		Select("redemptions.id, redemptions.batch_id, redemption_batches.name, redemption_batches.product_id, redemption_batches.product_revision, redemption_batches.approval_id, redemption_batches.delivery_binding, redemptions.face_value_cents, redemptions.quota").
		Joins("JOIN redemption_batches ON redemption_batches.id = redemptions.batch_id").
		Joins("JOIN credit_approval_events ON credit_approval_events.id = redemption_batches.approval_id AND credit_approval_events.product_id = redemption_batches.product_id AND credit_approval_events.revision = redemption_batches.product_revision").
		Where("((credit_approval_events.decision = ? AND credit_approval_events.policy = ?) OR (credit_approval_events.decision = ? AND credit_approval_events.policy = ?))", "approve", CreditApprovalPolicy, "auto_validate", CreditAutomaticValidationPolicy).
		Where("redemptions.source = ? AND redemptions.type = ? AND redemptions.status = ?", RedemptionSourceStore, RedemptionTypeBalance, common.RedemptionCodeStatusEnabled).
		Where("(redemptions.referral_kind IS NULL OR redemptions.referral_kind = ?)", "").
		Where("(redemptions.expired_time = 0 OR redemptions.expired_time >= ?)", now).
		Where("redemption_batches.source = ? AND redemption_batches.type = ? AND redemption_batches.status = ? AND redemption_batches.evidence_only = ?", RedemptionSourceStore, RedemptionTypeBalance, RedemptionBatchStatusEnabled, false).
		Where("redemption_batches.product_id <> ? AND redemption_batches.product_revision > 0 AND redemption_batches.approval_id > 0", "").
		Where("(redemption_batches.expired_time = 0 OR redemption_batches.expired_time >= ?)", now).
		Where("redemptions.face_value_cents > 0 AND redemptions.quota > 0 AND redemptions.face_value_cents = redemption_batches.face_value_cents AND redemptions.quota = redemption_batches.quota").
		Order("redemptions.id").Find(&rows).Error
	return rows, err
}

func referralStockPreview(rows []referralStockCandidate) (*ReferralStockPreview, error) {
	raw, err := common.Marshal(rows)
	if err != nil {
		return nil, err
	}
	preview := &ReferralStockPreview{Count: len(rows), Batches: make([]ReferralStockBatch, 0), Digest: fmt.Sprintf("%x", sha256.Sum256(raw))}
	indexes := make(map[int]int)
	for _, row := range rows {
		i, exists := indexes[row.BatchID]
		if !exists {
			i = len(preview.Batches)
			indexes[row.BatchID] = i
			preview.Batches = append(preview.Batches, ReferralStockBatch{ID: row.BatchID, Name: row.Name, ProductID: row.ProductID, ProductRevision: row.ProductRevision})
		}
		preview.Batches[i].Count++
	}
	return preview, nil
}

func GetReferralStockPreview() (*ReferralStockPreview, error) {
	rows, err := referralStockCandidates(DB, false)
	if err != nil {
		return nil, err
	}
	return referralStockPreview(rows)
}

func QualifyReferralStock(digest string, actor int) (int, error) {
	if len(digest) != 64 || actor <= 0 {
		return 0, errors.New("invalid inventory confirmation")
	}
	updated := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := readReferralConfig(tx, true); err != nil {
			return err
		}
		rows, err := referralStockCandidates(tx, false)
		if err != nil || len(rows) == 0 {
			return err // A repeated successful request is a no-op.
		}
		preview, err := referralStockPreview(rows)
		if err != nil {
			return err
		}
		if preview.Digest != digest {
			return ErrReferralStockChanged
		}
		batchIDs := make([]int, 0, len(preview.Batches))
		for _, batch := range preview.Batches {
			batchIDs = append(batchIDs, batch.ID)
		}
		slices.Sort(batchIDs)
		lockedKinds := make(map[int]string)
		// Match batch management's Batch -> Redemption lock order. The shared
		// referral gate also serializes this operation against redemption/payout.
		for _, id := range batchIDs {
			var batch RedemptionBatch
			if err := lockForUpdate(tx).First(&batch, id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrReferralStockChanged
				}
				return err
			}
			var codes []Redemption
			if err := lockForUpdate(tx).Select("id", "referral_kind").Where("batch_id = ?", id).Order("id").Find(&codes).Error; err != nil {
				return err
			}
			for _, code := range codes {
				lockedKinds[code.Id] = code.ReferralKind
			}
		}
		// A locking read sees current rows on MySQL REPEATABLE READ as well.
		reviewedRows := rows
		rows, err = referralStockCandidates(tx, true)
		if err != nil {
			return err
		}
		current, err := referralStockPreview(rows)
		if err != nil {
			return err
		}
		if current.Count == 0 {
			for _, row := range reviewedRows {
				if lockedKinds[row.ID] != "paid" {
					return ErrReferralStockChanged
				}
			}
			return nil
		}
		if current.Digest != digest {
			return ErrReferralStockChanged
		}
		ids := make([]int, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		// Bound bind parameters for every supported database, while committing
		// the entire confirmation and its audit in one transaction.
		for start := 0; start < len(ids); start += 500 {
			part := ids[start:min(start+500, len(ids))]
			result := tx.Model(&Redemption{}).Where("id IN ?", part).
				Where("status = ? AND (referral_kind IS NULL OR referral_kind = ?)", common.RedemptionCodeStatusEnabled, "").Update("referral_kind", "paid")
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != int64(len(part)) {
				return ErrReferralStockChanged
			}
		}
		raw, err := common.Marshal(map[string]any{"ids": ids, "kind": "paid", "reason": "operator confirmed paid-sale purpose of reviewed formal inventory", "digest": digest, "batches": current.Batches})
		if err != nil {
			return err
		}
		if err := tx.Create(&ReferralAudit{ActorID: actor, Action: "qualification", Details: string(raw), CreatedTime: common.GetTimestamp()}).Error; err != nil {
			return err
		}
		updated = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return updated, nil
}
