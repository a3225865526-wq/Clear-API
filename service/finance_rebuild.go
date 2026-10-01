package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func financeRebuildDeduplicated(ids []int64) ([]int64, bool) {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			return nil, false
		}
	}
	return sorted, true
}

const (
	financeRebuildMaxSyncEvents = 50
	financeRebuildMaxFacts      = 400
)

type FinanceRebuildInput struct {
	SyncEventIDs     []int64 `json:"sync_event_ids"`
	ExpectedRevision int64   `json:"expected_revision"`
	SnapshotHash     string  `json:"snapshot_hash"`
	Evidence         string  `json:"evidence"`
}

// One canonical fact of the rebuilt ledger. Existing records keep their key,
// time, kind, quota, source, frozen value and evidence; Origin records where
// the fact came from so the snapshot stays reviewable.
type FinanceRebuildFact struct {
	Key      string  `json:"key"`
	At       int64   `json:"at"`
	Kind     string  `json:"kind"`
	Quota    int64   `json:"quota"`
	Source   string  `json:"source,omitempty"`
	ValueCNY *string `json:"value_cny,omitempty"`
	Reverses string  `json:"reverses,omitempty"`
	Evidence string  `json:"evidence"`
	Origin   string  `json:"origin"`
}

type financeRebuildPlan struct {
	facts     []FinanceRebuildFact
	factCount int
	hash      string
	replay    *FinanceRevenueResult
}

type FinanceRebuildPreview struct {
	UserID                 int      `json:"user_id"`
	ExpectedRevision       int64    `json:"expected_revision"`
	SnapshotHash           string   `json:"snapshot_hash"`
	FactCount              int      `json:"fact_count"`
	IncorporatedSyncEvents []int64  `json:"incorporated_sync_events"`
	CurrentRemainingQuota  int64    `json:"current_remaining_quota"`
	CurrentBackingCNY      string   `json:"current_known_backing_cny"`
	CurrentRevenueCNY      string   `json:"current_known_revenue_cny"`
	CurrentUnknownConsumed int64    `json:"current_unknown_consumed_quota"`
	RebuiltRemainingQuota  int64    `json:"rebuilt_remaining_quota"`
	RebuiltBackingCNY      *string  `json:"rebuilt_credit_backing_cny"`
	RebuiltRevenueCNY      *string  `json:"rebuilt_known_consumption_cny"`
	RebuiltUnknownConsumed int64    `json:"rebuilt_unknown_consumed_quota"`
	Status                 string   `json:"status"`
	Warnings               []string `json:"warnings"`
}

// Resolves the parked sync events into canonical facts. Events whose ledger
// state is already correct are skipped; blocked, unresolvable or transient
// ones abort the scope so the rebuild never guesses.
func resolveFinanceRebuildScope(tx *gorm.DB, userID int, eventIDs []int64) ([]FinanceRebuildFact, []int64, []int64, error) {
	facts := make([]FinanceRebuildFact, 0, len(eventIDs))
	incorporated := make([]int64, 0, len(eventIDs))
	skipped := make([]int64, 0, len(eventIDs))
	for _, eventID := range eventIDs {
		var syncEvent model.FinanceSyncEvent
		if err := tx.Where("id = ? AND user_id = ?", eventID, userID).First(&syncEvent).Error; err != nil {
			return nil, nil, nil, model.ErrFinanceInvalid
		}
		var resolution financeSyncResolution
		switch syncEvent.Topic {
		case "offline_redemption":
			resolution = resolveFinanceSyncRedemption(tx, &syncEvent)
		case "consumption":
			resolution = resolveFinanceSyncConsumption(tx, &syncEvent)
		default:
			return nil, nil, nil, model.ErrFinanceInvalid
		}
		switch resolution.outcome.status {
		case financeSyncReady:
			facts = append(facts, FinanceRebuildFact{
				Key: resolution.event.Key, At: resolution.event.At, Kind: resolution.event.Kind,
				Quota: resolution.event.Quota, Source: resolution.event.Source, ValueCNY: resolution.event.ValueCNY,
				Reverses: resolution.event.Reverses,
				Evidence: fmt.Sprintf("rebuild:sync_event:%d; %s", syncEvent.ID, resolution.evidence),
				Origin:   fmt.Sprintf("sync_event:%d", syncEvent.ID),
			})
			incorporated = append(incorporated, syncEvent.ID)
		case model.FinanceSyncDone:
			skipped = append(skipped, syncEvent.ID)
		default:
			return nil, nil, nil, model.ErrFinanceInvalid
		}
	}
	if len(facts) == 0 {
		return nil, nil, nil, model.ErrFinanceInvalid
	}
	return facts, incorporated, skipped, nil
}

// Loads the wallet's existing records as canonical facts and rewrites
// reversal references from record IDs to event keys for the pure replay.
func loadFinanceRebuildRecords(tx *gorm.DB, userID int) ([]FinanceRebuildFact, []model.FinanceRevenueRecord, error) {
	var records []model.FinanceRevenueRecord
	if err := tx.Where("user_id = ?", userID).Order("revision asc").Find(&records).Error; err != nil {
		return nil, nil, err
	}
	keyByID := make(map[int64]string, len(records))
	for _, record := range records {
		keyByID[record.ID] = record.EventKey
	}
	facts := make([]FinanceRebuildFact, 0, len(records))
	for _, record := range records {
		fact := FinanceRebuildFact{
			Key: record.EventKey, At: record.At, Kind: record.Kind, Quota: record.Quota,
			Source: record.Source, ValueCNY: record.ValueCNY, Evidence: record.Evidence,
			Origin: fmt.Sprintf("record:%d", record.ID),
		}
		if record.Reverses != 0 {
			key, ok := keyByID[record.Reverses]
			if !ok {
				return nil, nil, model.ErrFinanceInvalid
			}
			fact.Reverses = key
		}
		facts = append(facts, fact)
	}
	return facts, records, nil
}

// Existing facts keep their historical position (revision order); new facts
// sort after same-second existing facts so the replay stays deterministic.
func buildFinanceRebuildPlan(tx *gorm.DB, userID int, oldRevision int64, existing []FinanceRebuildFact, fresh []FinanceRebuildFact) (*financeRebuildPlan, error) {
	merged := make([]FinanceRebuildFact, 0, len(existing)+len(fresh))
	merged = append(merged, existing...)
	merged = append(merged, fresh...)
	slices.SortStableFunc(merged, func(a, b FinanceRebuildFact) int {
		if a.At != b.At {
			return int(a.At - b.At)
		}
		rankA, rankB := 0, 0
		if strings.HasPrefix(a.Origin, "sync_event:") {
			rankA = 1
		}
		if strings.HasPrefix(b.Origin, "sync_event:") {
			rankB = 1
		}
		return rankA - rankB
	})
	if len(merged) > financeRebuildMaxFacts {
		return nil, model.ErrFinanceInvalid
	}
	events := make([]FinanceRevenueEvent, 0, len(merged))
	for _, fact := range merged {
		events = append(events, FinanceRevenueEvent{Key: fact.Key, At: fact.At, Kind: fact.Kind, Quota: fact.Quota, Source: fact.Source, ValueCNY: fact.ValueCNY, Reverses: fact.Reverses})
	}
	replay, err := PreviewFinanceRevenue(FinanceRevenuePreview{Events: events})
	if err != nil {
		return nil, err
	}
	canonical, err := common.Marshal(struct {
		UserID      int                  `json:"user_id"`
		OldRevision int64                `json:"old_revision"`
		Facts       []FinanceRebuildFact `json:"facts"`
	}{userID, oldRevision, merged})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	return &financeRebuildPlan{facts: merged, factCount: len(merged), hash: hex.EncodeToString(digest[:]), replay: replay}, nil
}

// Preview recomputes the whole wallet from its recorded facts plus the
// selected parked events. Nothing is written; the hash pins the exact fact
// set and pre-rebuild revision for the apply step.
func PreviewFinanceWalletRebuild(db *gorm.DB, userID int, eventIDs []int64) (*FinanceRebuildPreview, error) {
	if err := model.FinanceRevenueReady(db); err != nil {
		return nil, err
	}
	if err := model.FinanceSyncReady(db); err != nil {
		return nil, err
	}
	if err := model.FinanceRebuildsReady(db); err != nil {
		return nil, err
	}
	if userID <= 0 || userID > math.MaxInt32 || len(eventIDs) < 1 || len(eventIDs) > financeRebuildMaxSyncEvents {
		return nil, model.ErrFinanceInvalid
	}
	if _, ok := financeRebuildDeduplicated(eventIDs); !ok {
		return nil, model.ErrFinanceInvalid
	}
	var wallet model.FinanceWallet
	if err := db.First(&wallet, "user_id = ?", userID).Error; err != nil {
		return nil, model.ErrFinanceInvalid
	}
	freshFacts, incorporated, skipped, err := resolveFinanceRebuildScope(db, userID, eventIDs)
	if err != nil {
		return nil, err
	}
	existing, _, err := loadFinanceRebuildRecords(db, userID)
	if err != nil {
		return nil, err
	}
	plan, err := buildFinanceRebuildPlan(db, userID, wallet.Revision, existing, freshFacts)
	if err != nil {
		return nil, err
	}
	result := &FinanceRebuildPreview{
		UserID:                 userID,
		ExpectedRevision:       wallet.Revision,
		SnapshotHash:           plan.hash,
		FactCount:              plan.factCount,
		IncorporatedSyncEvents: incorporated,
		CurrentRemainingQuota:  wallet.RemainingQuota,
		CurrentBackingCNY:      wallet.KnownBackingCNY,
		CurrentRevenueCNY:      wallet.KnownRevenueCNY,
		CurrentUnknownConsumed: wallet.UnknownConsumedQuota,
		RebuiltRemainingQuota:  plan.replay.RemainingQuota,
		RebuiltUnknownConsumed: plan.replay.UnknownConsumedQuota,
		Status:                 "preview",
		Warnings: []string{
			"rebuild_replaces_ledger_rows_and_recreates_metadata",
			"run_during_quiet_period_concurrent_writes_rejected_by_revision",
			"production_execution_requires_migration_and_owner_approval",
			"skipped_already_reflected_events_are_marked_done",
		},
	}
	if plan.replay.BackingValueCNY != nil {
		result.RebuiltBackingCNY = plan.replay.BackingValueCNY
	}
	if plan.replay.ConsumptionCNY != nil {
		result.RebuiltRevenueCNY = plan.replay.ConsumptionCNY
	}
	_ = skipped
	return result, nil
}

// Apply rewrites one wallet's ledger rows inside a single transaction: frozen
// snapshot first, then delete and keyed replay. Expected revision and the
// preview hash must both match; any audit or projection failure rolls the
// whole rebuild back.
func ApplyFinanceWalletRebuild(db *gorm.DB, userID, actorID int, input FinanceRebuildInput, requestID string) (*model.FinanceWallet, error) {
	if err := model.FinanceRevenueReady(db); err != nil {
		return nil, err
	}
	if err := model.FinanceSyncReady(db); err != nil {
		return nil, err
	}
	if err := model.FinanceRebuildsReady(db); err != nil {
		return nil, err
	}
	if !common.IsFinanceOwner(actorID) || userID <= 0 || userID > math.MaxInt32 || input.ExpectedRevision < 0 || input.Evidence == "" || len(input.Evidence) > 500 || len(input.SyncEventIDs) < 1 || len(input.SyncEventIDs) > financeRebuildMaxSyncEvents || input.SnapshotHash == "" || len(input.SnapshotHash) > 64 {
		return nil, model.ErrFinanceInvalid
	}
	if _, ok := financeRebuildDeduplicated(input.SyncEventIDs); !ok {
		return nil, model.ErrFinanceInvalid
	}
	var wallet model.FinanceWallet
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&wallet, "user_id = ?", userID).Error; err != nil {
			return model.ErrFinanceInvalid
		}
		if _, err := model.LockFinanceWallet(tx, userID, input.ExpectedRevision); err != nil {
			return err
		}
		freshFacts, incorporated, skipped, err := resolveFinanceRebuildScope(tx, userID, input.SyncEventIDs)
		if err != nil {
			return err
		}
		existing, records, err := loadFinanceRebuildRecords(tx, userID)
		if err != nil {
			return err
		}
		plan, err := buildFinanceRebuildPlan(tx, userID, input.ExpectedRevision, existing, freshFacts)
		if err != nil {
			return err
		}
		if plan.hash != input.SnapshotHash {
			return model.ErrFinanceConflict
		}
		before, err := common.Marshal(wallet)
		if err != nil {
			return err
		}
		factJSON, err := common.Marshal(plan.facts)
		if err != nil {
			return err
		}
		snapshot := model.FinanceRebuildSnapshot{UserID: userID, FactCount: plan.factCount, Facts: string(factJSON), Before: string(before), Hash: plan.hash, Evidence: input.Evidence, CreatedBy: actorID, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&snapshot).Error; err != nil {
			return err
		}
		recordIDs := make([]int64, 0, len(records))
		for _, record := range records {
			recordIDs = append(recordIDs, record.ID)
		}
		if len(recordIDs) > 0 {
			if err := tx.Where("event_id IN ?", recordIDs).Delete(&model.FinanceRevenueAllocation{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.FinanceRevenueLot{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.FinanceRevenueRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.FinanceWallet{}).Error; err != nil {
			return err
		}
		expected := int64(0)
		for _, fact := range plan.facts {
			event := FinanceRevenueEvent{Key: fact.Key, At: fact.At, Kind: fact.Kind, Quota: fact.Quota, Source: fact.Source, ValueCNY: fact.ValueCNY, Reverses: fact.Reverses}
			if _, err := appendFinanceRevenueAudit(tx, userID, actorID, FinanceRevenueWrite{ExpectedRevision: expected, Event: event, Evidence: fact.Evidence}, requestID, "revenue.rebuild"); err != nil {
				return err
			}
			expected++
		}
		// Keep the revision token monotonic across the rewrite so pre-rebuild
		// writers can never land on the rebuilt state.
		finalRevision := input.ExpectedRevision + int64(plan.factCount)
		if err := tx.Model(&model.FinanceWallet{}).Where("user_id = ?", userID).Update("revision", finalRevision).Error; err != nil {
			return err
		}
		marked := append(append([]int64{}, incorporated...), skipped...)
		if len(marked) > 0 {
			if err := tx.Model(&model.FinanceSyncEvent{}).Where("id IN ?", marked).Updates(map[string]any{"status": model.FinanceSyncDone, "processed_at": time.Now().Unix(), "error_class": ""}).Error; err != nil {
				return err
			}
		}
		var rebuilt model.FinanceWallet
		if err := tx.First(&rebuilt, "user_id = ?", userID).Error; err != nil {
			return err
		}
		if rebuilt.RemainingQuota != plan.replay.RemainingQuota || rebuilt.UnknownConsumedQuota != plan.replay.UnknownConsumedQuota || rebuilt.KnownRevenueCNY != plan.replay.KnownConsumptionCNY || rebuilt.KnownBackingCNY != plan.replay.KnownBackingValueCNY {
			return model.ErrFinanceInvalid
		}
		wallet = rebuilt
		return tx.Create(&model.FinanceAudit{ActorID: actorID, Action: "revenue.rebuild", EntityID: snapshot.ID, PreviousID: input.ExpectedRevision, RequestID: requestID, CreatedAt: time.Now().Unix()}).Error
	})
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}
