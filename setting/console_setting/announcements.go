package console_setting

import (
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

func getPublishTime(item map[string]interface{}) time.Time {
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
func GetAnnouncements() []map[string]interface{} {
	list := getJSONList(GetConsoleSetting().Announcements)
	published := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if announcementStatus(item["status"]) == AnnouncementStatusPublished {
			published = append(published, item)
		}
	}
	sort.SliceStable(published, func(i, j int) bool {
		return getPublishTime(published[i]).After(getPublishTime(published[j]))
	})
	return published
}
