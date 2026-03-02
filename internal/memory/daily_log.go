package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const dailyLogDateFormat = "2006-01-02"

// DailyLogPath returns the file path for a daily log on the given date.
func DailyLogPath(memoryDir string, date time.Time) string {
	return filepath.Join(memoryDir, date.Format(dailyLogDateFormat)+".md")
}

// AppendDailyLog appends content to today's daily log file, creating it
// if it doesn't exist. A timestamp header is prepended to each entry.
func AppendDailyLog(memoryDir string, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	path := DailyLogPath(memoryDir, time.Now())
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open daily log: %w", err)
	}
	defer f.Close()

	entry := fmt.Sprintf("\n## %s\n\n%s\n", time.Now().Format("15:04:05"), content)
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("write daily log: %w", err)
	}
	return nil
}

// LoadRecentLogs reads today's and yesterday's daily logs, returning
// their combined content. Missing files are silently skipped.
func LoadRecentLogs(memoryDir string, days int) (string, error) {
	if days <= 0 {
		days = 2
	}

	var parts []string
	now := time.Now()
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		path := DailyLogPath(memoryDir, date)
		content, err := os.ReadFile(path)
		if err != nil {
			continue // file doesn't exist yet
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed != "" {
			header := fmt.Sprintf("# Daily Log: %s", date.Format(dailyLogDateFormat))
			parts = append(parts, header+"\n\n"+trimmed)
		}
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}
