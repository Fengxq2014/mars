package sequence

import (
	"testing"
	"time"
)

func TestTime(t *testing.T) {
	now := time.Now()
	duration, _ := time.ParseDuration("5m4s")
	t.Log(duration.Minutes())

	daysInMonth := now.AddDate(0, 1, -now.Day()).Day()
	lastDayOfYear := time.Date(now.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
	t.Log(daysInMonth)
	t.Log(lastDayOfYear.YearDay())
}
