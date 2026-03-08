package app

import (
	"time"

	"github.com/doeshing/nekoclaw/internal/usage"
)

func (s *Service) UsageSummary(now time.Time) usage.Summary {
	return usage.SummarizeSessionStore(s.sessions, now)
}
