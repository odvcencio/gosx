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
	CurrentProgressAgeMs *int64  `json:"current_progress_age_ms,omitempty"`
	ProgressTimeoutMs    *int64  `json:"progress_timeout_ms,omitempty"`
	ErrorClass           string  `json:"error_class,omitempty"`
}

const scheduledStatusLimit = 64

// ScheduledStatus returns at most 64 task statuses and whether the snapshot is
// complete. It removes progress and error text and never creates a scheduler.
func (a *App) ScheduledStatus(limit int) ([]scheduled.TaskStatus, bool) {
	if a == nil || a.scheduler == nil {
		return nil, true
	}
	if limit <= 0 || limit > scheduledStatusLimit {
		limit = scheduledStatusLimit
	}
	return sanitizedScheduledStatus(a.scheduler, limit)
}

func sanitizedScheduledStatus(s *scheduled.Scheduler, limit int) ([]scheduled.TaskStatus, bool) {
	if s == nil {
		return nil, true
	}
	statuses, complete := s.StatusLimit(limit)
	for i := range statuses {
		statuses[i].CurrentProgress = ""
		if statuses[i].RecentError != "" {
			statuses[i].RecentError = "task_failed"
		}
	}
	return statuses, complete
}

// ScheduledStatusHandler returns bounded, redacted task status as a JSON array.
// Mount this handler behind authentication; it does not authorize requests.
func ScheduledStatusHandler(s *scheduled.Scheduler) http.Handler {
	return scheduledStatusHandler(func() ([]scheduled.TaskStatus, bool) {
		return sanitizedScheduledStatus(s, scheduledStatusLimit)
	})
}

// ScheduledStatusHandler returns the App's bounded, redacted task status without
// creating a scheduler. Internal aliases can mount the same handler behind
// their admin authentication.
func (a *App) ScheduledStatusHandler() http.Handler {
	return scheduledStatusHandler(func() ([]scheduled.TaskStatus, bool) {
		return a.ScheduledStatus(scheduledStatusLimit)
	})
}

func scheduledStatusHandler(snapshot func() ([]scheduled.TaskStatus, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses, complete := snapshot()
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
			if st.RecentError != "" {
				item.ErrorClass = "task_failed"
			}
			items = append(items, item)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if complete {
			w.Header().Set("X-GoSX-Snapshot-Complete", "true")
		} else {
			w.Header().Set("X-GoSX-Snapshot-Complete", "false")
		}
		_ = json.NewEncoder(w).Encode(items)
	})
}
