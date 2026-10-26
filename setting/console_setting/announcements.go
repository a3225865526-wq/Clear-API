package console_setting

import (
	"crypto/sha256"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"sort"
	"time"
)

// Announcement release states. Records created before the lifecycle existed
// carry no `status` field; they stay visible, so announcementStatus reports
// them as published instead of requiring a one-off data migration.
const (
	AnnouncementStatusDraft     = "draft"
	AnnouncementStatusPublished = "published"
	AnnouncementStatusWithdrawn = "withdrawn"
)

var announcementStatuses = map[string]bool{
	AnnouncementStatusDraft:     true,
	AnnouncementStatusPublished: true,
	AnnouncementStatusWithdrawn: true,
}

// announcementStatus maps a stored or submitted status onto a release state.
// A missing status means a record predating the lifecycle, which is published;
// an unrecognized value is kept out of public view by reporting it as a draft.
func announcementStatus(value any) string {
	status, _ := value.(string)
	switch {
	case status == AnnouncementStatusDraft, status == AnnouncementStatusWithdrawn:
		return status
	case status == AnnouncementStatusPublished, status == "":
		return AnnouncementStatusPublished
	default:
		return AnnouncementStatusDraft
	}
}

func getPublishTime(item map[string]any) time.Time {
	if v, ok := item["publishDate"]; ok {
		if s, ok2 := v.(string); ok2 {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// GetAnnouncements returns the announcements end users may read, newest first.
// Drafts and withdrawn announcements stay in storage for the administrator but
// are never served from here.
func GetAnnouncements() []map[string]any {
	list := getJSONList(GetConsoleSetting().Announcements)
	published := make([]map[string]any, 0, len(list))
	for index, item := range list {
		if item["id"] == nil {
			item["id"] = index + 1
		}
		if announcementStatus(item["status"]) == AnnouncementStatusPublished {
			item["display"] = AnnouncementDisplay(item["display"])
			item["read_key"] = AnnouncementReadKey(item)
			published = append(published, item)
		}
	}
	sort.SliceStable(published, func(i, j int) bool {
		return getPublishTime(published[i]).After(getPublishTime(published[j]))
	})
	return published
}

// Legacy announcements keep their original timeline placement.
func AnnouncementDisplay(value any) string {
	switch value {
	case "notice":
		return "notice"
	case "both":
		return "both"
	default:
		return "timeline"
	}
}

func AnnouncementReadKey(item map[string]any) string {
	// Placement and release state do not change the identity of the content read.
	extra, _ := item["extra"].(string)
	value, _ := common.Marshal([]any{item["id"], item["publishDate"], item["content"], extra, item["type"]})
	return fmt.Sprintf("announcement:%x", sha256.Sum256(value))
}

func NoticeReadKey(content string) string {
	return fmt.Sprintf("notice:%x", sha256.Sum256([]byte(content)))
}

func GetAnnouncementsFor(display string) []map[string]any {
	result := []map[string]any{}
	for _, item := range GetAnnouncements() {
		if item["display"] == display || item["display"] == "both" {
			result = append(result, item)
		}
	}
	return result
}
