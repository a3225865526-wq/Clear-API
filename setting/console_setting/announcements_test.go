package console_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setAnnouncementsForTest(t *testing.T, raw string) {
	t.Helper()
	setting := GetConsoleSetting()
	original := setting.Announcements
	setting.Announcements = raw
	t.Cleanup(func() { setting.Announcements = original })
}

func announcementContents(items []map[string]any) []string {
	contents := make([]string, 0, len(items))
	for _, item := range items {
		content, _ := item["content"].(string)
		contents = append(contents, content)
	}
	return contents
}

func TestGetAnnouncementsOnlyReturnsPublishedRecords(t *testing.T) {
	setAnnouncementsForTest(t, `[
		{"content":"draft record","publishDate":"2026-10-01T00:00:00Z","status":"draft"},
		{"content":"withdrawn record","publishDate":"2026-10-02T00:00:00Z","status":"withdrawn"},
		{"content":"published record","publishDate":"2026-10-03T00:00:00Z","status":"published"},
		{"content":"legacy record","publishDate":"2026-09-30T00:00:00Z"}
	]`)

	items := GetAnnouncements()

	// The legacy record predates the lifecycle and must stay visible; the
	// others are ordered newest first.
	assert.Equal(t, []string{"published record", "legacy record"}, announcementContents(items))
}

func TestGetAnnouncementsHidesUnknownStatus(t *testing.T) {
	setAnnouncementsForTest(t, `[
		{"content":"unknown status","publishDate":"2026-10-01T00:00:00Z","status":"archived"},
		{"content":"published record","publishDate":"2026-10-02T00:00:00Z","status":"published"}
	]`)

	items := GetAnnouncements()

	assert.Equal(t, []string{"published record"}, announcementContents(items))
}

func TestValidateConsoleSettingsAnnouncementStatus(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "accepts the three release states",
			payload: `[{"content":"a","publishDate":"2026-10-01T00:00:00Z","status":"draft"},{"content":"b","publishDate":"2026-10-01T00:00:00Z","status":"published"},{"content":"c","publishDate":"2026-10-01T00:00:00Z","status":"withdrawn"}]`,
		},
		{
			name:    "accepts records without a status",
			payload: `[{"content":"a","publishDate":"2026-10-01T00:00:00Z"}]`,
		},
		{
			name:    "rejects an unknown status",
			payload: `[{"content":"a","publishDate":"2026-10-01T00:00:00Z","status":"archived"}]`,
			wantErr: "第1个公告的发布状态值不合法",
		},
		{
			name:    "rejects a non-string status",
			payload: `[{"content":"a","publishDate":"2026-10-01T00:00:00Z","status":1}]`,
			wantErr: "第1个公告的发布状态值不合法",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConsoleSettings(tt.payload, "Announcements")
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}
