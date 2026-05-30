package server

import (
	"encoding/json"
	"net/http"

	"m31labs.dev/gosx/scheduled"
)

// scheduledStatusItem is the JSON shape for a single task in the
// /_gosx/scheduled response.
type scheduledStatusItem struct {
	Name                 string  `json:"name"`
	Schedule             string  `json:"schedule,omitempty"`
	LastRunAt            *string `json:"last_run_at,omitempty"`
	LastSuccessAt        *string `json:"last_success_at,omitempty"`
	NextDueAt            *string `json:"next_due_at,omitempty"`
	CurrentAttempt       int     `json:"current_attempt,omitempty"`
	CurrentProgress      *string `json:"current_progress"`
	CurrentProgressAgeMs *int64  `json:"current_progress_age_ms,omitempty"`
	ProgressTimeoutMs    *int64  `json:"progress_timeout_ms,omitempty"`
	RecentError          *string `json:"recent_error,omitempty"`
}

// ScheduledStatusHandler returns an http.Handler that serves the current
// status of all registered tasks as a JSON array at /_gosx/scheduled.
func ScheduledStatusHandler(s *scheduled.Scheduler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := s.Status()
		items := make([]scheduledStatusItem, 0, len(statuses))
		for _, st := range statuses {
			item := scheduledStatusItem{
				Name:                 st.Name,
				Schedule:             st.Schedule,
				CurrentAttempt:       st.CurrentAttempt,
				CurrentProgressAgeMs: st.CurrentProgressAgeMs,
				ProgressTimeoutMs:    st.ProgressTimeoutMs,
			}
			if !st.LastRunAt.IsZero() {
				s := st.LastRunAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
				item.LastRunAt = &s
			}
			if !st.LastSuccessAt.IsZero() {
				s := st.LastSuccessAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
				item.LastSuccessAt = &s
			}
			if !st.NextDueAt.IsZero() {
				s := st.NextDueAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
				item.NextDueAt = &s
			}
			if st.CurrentProgress != "" {
				cp := st.CurrentProgress
				item.CurrentProgress = &cp
			}
			if st.RecentError != "" {
				re := st.RecentError
				item.RecentError = &re
			}
			items = append(items, item)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(items)
	})
}
