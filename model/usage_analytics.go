package model

import (
	"context"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// UsageAnalytics is a read projection, never a migrated table. Amounts are
// customer charges, not supplier costs. Missing observations stay unknown.
type UsageAnalytics struct {
	ModelName    string `json:"model_name"`
	CreatedAt    int64  `json:"created_at"`
	Count        int64  `json:"count"`
	Quota        int64  `json:"quota"`
	TokenUsed    int64  `json:"token_used"`
	UsageQuality string `json:"usage_quality"`
}

// Read only retained consume logs; do not add quota_data or reservations to
// these facts. Repeated reads cannot backfill, mutate or double-count bills.
func GetUsageAnalytics(ctx context.Context, mainDB, db *gorm.DB, start, end int64, username string, userID int) ([]UsageAnalytics, error) {
	result := []UsageAnalytics{}
	groups := map[struct {
		model, quality string
		hour           int64
	}]*UsageAnalytics{}
	query := db.WithContext(ctx).Model(&Log{}).Select("id, model_name, created_at, quota, prompt_tokens, completion_tokens, other").Where("type = ? AND created_at >= ? AND created_at <= ?", LogTypeConsume, start, end)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if username != "" {
		// Canonical outbox logs may have no denormalized username.
		var user User
		if err := mainDB.Unscoped().Select("id").Where("username = ?", username).First(&user).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return result, nil
			}
			return nil, err
		}
		query = query.Where("user_id = ?", user.Id)
	}
	rows, err := query.Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var log Log
		if err := db.ScanRows(rows, &log); err != nil {
			return nil, err
		}
		var metadata struct {
			Admin struct {
				Observation struct {
					Quality string `json:"quality"`
				} `json:"token_observation"`
			} `json:"admin_info"`
		}
		quality := "unknown"
		if common.UnmarshalJsonStr(log.Other, &metadata) == nil {
			switch metadata.Admin.Observation.Quality {
			case "upstream_complete":
				quality = "confirmed"
			case "upstream_partial":
				quality = "partial"
			case "local_estimate":
				quality = "estimated"
			}
		}
		key := struct {
			model, quality string
			hour           int64
		}{log.ModelName, quality, log.CreatedAt - log.CreatedAt%3600}
		row := groups[key]
		if row == nil {
			row = &UsageAnalytics{ModelName: key.model, CreatedAt: key.hour, UsageQuality: quality}
			groups[key] = row
		}
		row.Count++
		row.Quota += int64(log.Quota)
		row.TokenUsed += int64(log.PromptTokens) + int64(log.CompletionTokens)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, row := range groups {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt != result[j].CreatedAt {
			return result[i].CreatedAt < result[j].CreatedAt
		}
		if result[i].ModelName != result[j].ModelName {
			return result[i].ModelName < result[j].ModelName
		}
		return result[i].UsageQuality < result[j].UsageQuality
	})
	return result, nil
}
