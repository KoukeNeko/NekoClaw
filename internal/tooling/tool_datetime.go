package tooling

import (
	"fmt"
	"time"
)

func (e *RuntimeExecutor) runDatetime() (string, error) {
	now := time.Now()
	zone, offset := now.Zone()
	offsetHours := offset / 3600
	offsetMin := (offset % 3600) / 60

	return fmt.Sprintf(
		"date: %s\ntime: %s\ntimezone: %s (UTC%+03d:%02d)\nunix: %d\nweekday: %s",
		now.Format("2006-01-02"),
		now.Format("15:04:05"),
		zone, offsetHours, offsetMin,
		now.Unix(),
		now.Weekday().String(),
	), nil
}
