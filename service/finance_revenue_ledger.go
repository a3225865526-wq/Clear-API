package service

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type FinanceRevenueWrite struct {
	ExpectedRevision int64               `json:"expected_revision"`
	Event            FinanceRevenueEvent `json:"event"`
	Evidence         string              `json:"evidence"`
}

// Manual confirmed facts remain a partial ledger until business-event capture
// and balance reconciliation are implemented. No User.Quota write occurs here.
func AppendFinanceRevenue(db *gorm.DB, userID, actorID int, input FinanceRevenueWrite, requestID string) (*model.FinanceRevenueRecord, error) {
	if strings.HasPrefix(input.Event.Key, "offline-redemption-") || strings.HasPrefix(input.Event.Key, "request-charge-") {
		return nil, model.ErrFinanceInvalid
	}
	return appendFinanceRevenueAudit(db, userID, actorID, input, requestID, "revenue.append")
}

func appendFinanceRevenue(db *gorm.DB, userID, actorID int, input FinanceRevenueWrite, requestID string) (*model.FinanceRevenueRecord, error) {
	return appendFinanceRevenueAudit(db, userID, actorID, input, requestID, "revenue.append")
}

func appendFinanceRevenueAudit(db *gorm.DB, userID, actorID int, input FinanceRevenueWrite, requestID string, auditAction string) (*model.FinanceRevenueRecord, error) {
	if err := model.FinanceRevenueReady(db); err != nil {
		return nil, err
	}
	event := input.Event
	if userID <= 0 || userID > math.MaxInt32 || actorID <= 0 || input.ExpectedRevision < 0 || input.ExpectedRevision == math.MaxInt64 || input.Evidence == "" || len(input.Evidence) > 500 || !financeKeyPattern.MatchString(event.Key) || event.At <= 0 || event.Quota > common.MaxWalletQuota || event.Quota < -common.MaxWalletQuota {
		return nil, model.ErrFinanceInvalid
	}
	switch event.Kind {
	case "credit":
		if _, err := PreviewFinanceRevenue(FinanceRevenuePreview{Events: []FinanceRevenueEvent{event}}); err != nil {
			return nil, err
		}
	case "consume", "reverse":
		if event.Source != "" || event.ValueCNY != nil || event.Quota <= 0 || (event.Kind == "consume" && event.Reverses != "") || (event.Kind == "reverse" && !financeKeyPattern.MatchString(event.Reverses)) {
			return nil, model.ErrFinanceInvalid
		}
	case "adjust":
		if event.Reverses != "" || event.Quota == 0 {
			return nil, model.ErrFinanceInvalid
		}
		if event.Quota > 0 {
			if _, err := PreviewFinanceRevenue(FinanceRevenuePreview{Events: []FinanceRevenueEvent{event}}); err != nil {
				return nil, err
			}
		} else if event.Source != "" || event.ValueCNY != nil {
			return nil, model.ErrFinanceInvalid
		}
	default:
		return nil, model.ErrFinanceInvalid
	}
	record := &model.FinanceRevenueRecord{UserID: userID, EventKey: event.Key, Revision: input.ExpectedRevision + 1, At: event.At, Kind: event.Kind, Quota: event.Quota, Source: event.Source, ValueCNY: event.ValueCNY, Evidence: input.Evidence, CreatedAt: time.Now().Unix(), CreatedBy: actorID}
	err := db.Transaction(func(tx *gorm.DB) error {
		var users int64
		if err := tx.Model(&model.User{}).Where("id = ?", userID).Count(&users).Error; err != nil {
			return err
		}
		if users != 1 {
			return model.ErrFinanceInvalid
		}
		wallet, err := model.LockFinanceWallet(tx, userID, input.ExpectedRevision)
		if err != nil {
			return err
		}
		if event.At < wallet.LastAt {
			return model.ErrFinanceInvalid
		}
		var duplicate int64
		if err := tx.Model(&model.FinanceRevenueRecord{}).Where("user_id = ? AND event_key = ?", userID, event.Key).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate != 0 {
			return model.ErrFinanceConflict
		}
		cash, err := decimal.NewFromString(wallet.KnownBackingCNY)
		if err != nil {
			return model.ErrFinanceInvalid
		}
		revenue, err := decimal.NewFromString(wallet.KnownRevenueCNY)
		if err != nil {
			return model.ErrFinanceInvalid
		}
		var original model.FinanceRevenueRecord
		if event.Kind == "reverse" {
			if err := tx.Where("user_id = ? AND event_key = ? AND kind = ?", userID, event.Reverses, "consume").First(&original).Error; err != nil {
				return model.ErrFinanceInvalid
			}
			record.Reverses = original.ID
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		switch event.Kind {
		case "credit":
			if event.Quota > common.MaxWalletQuota-wallet.RemainingQuota {
				return model.ErrFinanceInvalid
			}
			lot := model.FinanceRevenueLot{UserID: userID, EventID: record.ID, LotKey: event.Key, Source: event.Source, TotalQuota: event.Quota, ValueCNY: event.ValueCNY, RecognizedCNY: "0"}
			if err := tx.Create(&lot).Error; err != nil {
				return err
			}
			wallet.RemainingQuota += event.Quota
			if event.ValueCNY == nil {
				wallet.UnknownCreditCount++
			} else {
				amount, _ := financeDecimal(*event.ValueCNY, false)
				cash = cash.Add(amount)
			}
		case "consume":
			if event.Quota > wallet.RemainingQuota || event.Quota > math.MaxInt64-wallet.UnknownConsumedQuota {
				return model.ErrFinanceInvalid
			}
			left := event.Quota
			var lastID int64
			for left > 0 {
				var lots []model.FinanceRevenueLot
				if err := tx.Where("user_id = ? AND id > ? AND used_quota < total_quota", userID, lastID).Order("id asc").Limit(100).Find(&lots).Error; err != nil {
					return err
				}
				if len(lots) == 0 {
					return model.ErrFinanceInvalid
				}
				for _, lot := range lots {
					lastID = lot.ID
					take := min(left, lot.TotalQuota-lot.UsedQuota)
					part := model.FinanceRevenueAllocation{EventID: record.ID, LotID: lot.ID, Quota: take}
					if lot.ValueCNY == nil {
						wallet.UnknownConsumedQuota += take
					} else {
						total, err := decimal.NewFromString(*lot.ValueCNY)
						if err != nil {
							return model.ErrFinanceInvalid
						}
						recognized, err := decimal.NewFromString(lot.RecognizedCNY)
						if err != nil {
							return model.ErrFinanceInvalid
						}
						amount := decimal.Max(decimal.Zero, total.Mul(decimal.NewFromInt(lot.UsedQuota+take)).DivRound(decimal.NewFromInt(lot.TotalQuota), 12).Sub(recognized))
						value := amount.String()
						part.ValueCNY = &value
						lot.RecognizedCNY = recognized.Add(amount).String()
						revenue = revenue.Add(amount)
					}
					lot.UsedQuota += take
					if err := tx.Model(&lot).Updates(map[string]any{"used_quota": lot.UsedQuota, "recognized_cny": lot.RecognizedCNY}).Error; err != nil {
						return err
					}
					if err := tx.Create(&part).Error; err != nil {
						return err
					}
					left -= take
					if left == 0 {
						break
					}
				}
			}
			wallet.RemainingQuota -= event.Quota
		case "reverse":
			if event.Quota > common.MaxWalletQuota-wallet.RemainingQuota {
				return model.ErrFinanceInvalid
			}
			// Quota and value already restored by earlier partial reversals of
			// the same consumption; this reversal may only take the remainder.
			type reversedPart struct {
				quota int64
				value decimal.Decimal
			}
			reversed := map[int64]*reversedPart{}
			var reversalIDs []int64
			if err := tx.Model(&model.FinanceRevenueRecord{}).Where("user_id = ? AND reverses = ?", userID, original.ID).Pluck("id", &reversalIDs).Error; err != nil {
				return err
			}
			if len(reversalIDs) > 0 {
				var lastID int64
				for {
					var parts []model.FinanceRevenueAllocation
					if err := tx.Where("event_id IN ? AND id > ?", reversalIDs, lastID).Order("id asc").Limit(100).Find(&parts).Error; err != nil {
						return err
					}
					if len(parts) == 0 {
						break
					}
					for _, part := range parts {
						lastID = part.ID
						state := reversed[part.LotID]
						if state == nil {
							state = &reversedPart{}
							reversed[part.LotID] = state
						}
						state.quota -= part.Quota
						if part.ValueCNY != nil {
							value, err := decimal.NewFromString(*part.ValueCNY)
							if err != nil {
								return model.ErrFinanceInvalid
							}
							state.value = state.value.Sub(value)
						}
					}
				}
			}
			// Scan in bounded batches; an original event may span many lots.
			var lastID, restored int64
			for {
				var parts []model.FinanceRevenueAllocation
				if err := tx.Where("event_id = ? AND id > ?", original.ID, lastID).Order("id asc").Limit(100).Find(&parts).Error; err != nil {
					return err
				}
				if len(parts) == 0 {
					break
				}
				for _, part := range parts {
					lastID = part.ID
					if part.Quota <= 0 {
						return model.ErrFinanceInvalid
					}
					var reversedQuota int64
					var reversedValue decimal.Decimal
					if state := reversed[part.LotID]; state != nil {
						reversedQuota, reversedValue = state.quota, state.value
					}
					take := min(event.Quota-restored, part.Quota-reversedQuota)
					if take <= 0 {
						continue
					}
					var lot model.FinanceRevenueLot
					if err := tx.Where("id = ? AND user_id = ?", part.LotID, userID).First(&lot).Error; err != nil || take > lot.UsedQuota {
						return model.ErrFinanceInvalid
					}
					restored += take
					negative := model.FinanceRevenueAllocation{EventID: record.ID, LotID: part.LotID, Quota: -take}
					if part.ValueCNY == nil {
						wallet.UnknownConsumedQuota -= take
					} else {
						total, err := decimal.NewFromString(*part.ValueCNY)
						if err != nil {
							return model.ErrFinanceInvalid
						}
						partQuota := decimal.NewFromInt(part.Quota)
						priorCumulative := total.Mul(decimal.NewFromInt(reversedQuota)).DivRound(partQuota, 12)
						if !reversedValue.Equal(priorCumulative) {
							return model.ErrFinanceInvalid
						}
						amount := total.Mul(decimal.NewFromInt(reversedQuota+take)).DivRound(partQuota, 12).Sub(priorCumulative)
						recognized, err := decimal.NewFromString(lot.RecognizedCNY)
						if err != nil {
							return model.ErrFinanceInvalid
						}
						revenue = revenue.Sub(amount)
						lot.RecognizedCNY = recognized.Sub(amount).String()
						value := amount.Neg().String()
						negative.ValueCNY = &value
					}
					lot.UsedQuota -= take
					if err := tx.Model(&lot).Updates(map[string]any{"used_quota": lot.UsedQuota, "recognized_cny": lot.RecognizedCNY}).Error; err != nil {
						return err
					}
					if err := tx.Create(&negative).Error; err != nil {
						return err
					}
				}
			}
			if restored != event.Quota || wallet.UnknownConsumedQuota < 0 {
				return model.ErrFinanceInvalid
			}
			wallet.RemainingQuota += restored
		case "adjust":
			if event.Quota > 0 {
				if event.Quota > common.MaxWalletQuota-wallet.RemainingQuota {
					return model.ErrFinanceInvalid
				}
				lot := model.FinanceRevenueLot{UserID: userID, EventID: record.ID, LotKey: event.Key, Source: event.Source, TotalQuota: event.Quota, ValueCNY: event.ValueCNY, RecognizedCNY: "0"}
				if err := tx.Create(&lot).Error; err != nil {
					return err
				}
				wallet.RemainingQuota += event.Quota
				if event.ValueCNY == nil {
					wallet.UnknownCreditCount++
				} else {
					amount, _ := financeDecimal(*event.ValueCNY, false)
					cash = cash.Add(amount)
				}
			} else {
				forfeit := -event.Quota
				if forfeit > wallet.RemainingQuota {
					return model.ErrFinanceInvalid
				}
				left := forfeit
				var lastID int64
				for left > 0 {
					var lots []model.FinanceRevenueLot
					if err := tx.Where("user_id = ? AND id > ? AND used_quota < total_quota", userID, lastID).Order("id asc").Limit(100).Find(&lots).Error; err != nil {
						return err
					}
					if len(lots) == 0 {
						return model.ErrFinanceInvalid
					}
					for _, lot := range lots {
						lastID = lot.ID
						take := min(left, lot.TotalQuota-lot.UsedQuota)
						lot.UsedQuota += take
						if err := tx.Model(&lot).Update("used_quota", lot.UsedQuota).Error; err != nil {
							return err
						}
						// Forfeit parts carry no value: nothing was consumed
						// and no revenue or unknown counter participates.
						if err := tx.Create(&model.FinanceRevenueAllocation{EventID: record.ID, LotID: lot.ID, Quota: take}).Error; err != nil {
							return err
						}
						left -= take
						if left == 0 {
							break
						}
					}
				}
				wallet.RemainingQuota -= forfeit
			}
		}
		wallet.Revision, wallet.LastAt = record.Revision, event.At
		wallet.KnownBackingCNY, wallet.KnownRevenueCNY = cash.String(), revenue.String()
		updated := tx.Model(&model.FinanceWallet{}).Where("user_id = ? AND revision = ?", userID, input.ExpectedRevision).Updates(map[string]any{
			"revision": wallet.Revision, "last_at": wallet.LastAt, "remaining_quota": wallet.RemainingQuota,
			"known_backing_cny": wallet.KnownBackingCNY, "known_revenue_cny": wallet.KnownRevenueCNY,
			"unknown_credit_count": wallet.UnknownCreditCount, "unknown_consumed_quota": wallet.UnknownConsumedQuota,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return model.ErrFinanceConflict
		}
		return tx.Create(&model.FinanceAudit{ActorID: actorID, Action: auditAction, EntityID: record.ID, PreviousID: record.Reverses, RequestID: requestID, CreatedAt: record.CreatedAt}).Error
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}

type FinanceWalletSummary struct {
	Wallet         *model.FinanceWallet `json:"wallet"`
	BackingCNY     *string              `json:"recorded_credit_backing_cny"`
	ConsumptionCNY *string              `json:"recorded_consumption_cny"`
	Status         string               `json:"status"`
	Coverage       string               `json:"coverage"`
	Warnings       []string             `json:"warnings"`
}

func SummarizeFinanceWallet(db *gorm.DB, userID int) (*FinanceWalletSummary, error) {
	if err := model.FinanceRevenueReady(db); err != nil {
		return nil, err
	}
	result := &FinanceWalletSummary{Status: "partial", Coverage: "manual_facts_only", Warnings: []string{"business_events_not_connected", "cash_receipts_not_connected", "balance_reconciliation_not_completed", "cash_refunds_separate_cash_account", "subscription_revenue_not_supported"}}
	if model.FinanceReceiptsReady(db) == nil {
		result.Coverage = "partial_confirmed_facts"
		result.Warnings = []string{"business_events_not_fully_connected", "cash_receipts_separate_not_net_reconciled", "balance_reconciliation_not_completed", "cash_refunds_separate_cash_account", "subscription_revenue_not_supported"}
	}
	var wallet model.FinanceWallet
	if err := db.First(&wallet, "user_id = ?", userID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		result.Warnings = append(result.Warnings, "no_recorded_sources")
		return result, nil
	} else if err != nil {
		return nil, err
	}
	result.Wallet = &wallet
	if wallet.UnknownCreditCount == 0 {
		value := wallet.KnownBackingCNY
		result.BackingCNY = &value
	}
	if wallet.UnknownConsumedQuota == 0 {
		value := wallet.KnownRevenueCNY
		result.ConsumptionCNY = &value
	}
	return result, nil
}
