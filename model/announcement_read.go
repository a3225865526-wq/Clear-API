package model

import (
	"context"
	"time"

	"gorm.io/gorm/clause"
)

type AnnouncementRead struct {
	UserID int    `gorm:"primaryKey;autoIncrement:false" json:"-"`
	Key    string `gorm:"primaryKey;size:96" json:"key"`
	ReadAt int64  `json:"read_at"`
}

func GetAnnouncementReads(ctx context.Context, userID int, keys []string) ([]AnnouncementRead, error) {
	records := []AnnouncementRead{}
	if len(keys) == 0 {
		return records, nil
	}
	err := DB.WithContext(ctx).Where("user_id = ?", userID).Where(map[string]any{"key": keys}).Find(&records).Error
	return records, err
}

func MarkAnnouncementsRead(ctx context.Context, userID int, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	records := make([]AnnouncementRead, 0, len(keys))
	for _, key := range keys {
		records = append(records, AnnouncementRead{UserID: userID, Key: key, ReadAt: time.Now().Unix()})
	}
	return DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&records).Error
}
