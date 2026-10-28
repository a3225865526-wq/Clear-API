package billingexpr

import (
	_ "embed"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// The announced holiday periods include days off in lieu, but not makeup
// workdays. Weekday/weekend pricing remains an explicit expression condition.
// Both the Go engine and browser simulator read this exact versioned asset.
//
//go:embed chinese_holidays.json
var chineseHolidayJSON []byte

var chineseHolidayZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type chineseHolidayYear struct {
	Dates []string `json:"dates"`
}

var loadChineseHolidays = sync.OnceValues(func() (map[string]chineseHolidayYear, error) {
	var calendar struct {
		Years map[string]chineseHolidayYear `json:"years"`
	}
	err := common.Unmarshal(chineseHolidayJSON, &calendar)
	return calendar.Years, err
})

func chineseHoliday(now time.Time) (int, error) {
	calendar, err := loadChineseHolidays()
	if err != nil {
		return 0, fmt.Errorf("Chinese holiday calendar: %w", err)
	}
	date := now.In(chineseHolidayZone)
	year := strconv.Itoa(date.Year())
	days, ok := calendar[year]
	if !ok {
		return 0, fmt.Errorf("Chinese holiday calendar unavailable for %s", year)
	}
	if slices.Contains(days.Dates, date.Format("2006-01-02")) {
		return 1, nil
	}
	return 0, nil
}
