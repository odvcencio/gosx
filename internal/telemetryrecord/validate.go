package telemetryrecord

import (
	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/internal/telemetryfields"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

func invalid() error { return telemetryerr.ErrInvalidOptions }
func id(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func name(s string) bool {
	if len(s) == 0 || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range []byte(s[1:]) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func optionalName(s string) bool { return s == "" || name(s) }
func text(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}
func timestamp(t time.Time) bool { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }
func nonnegative(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
func observations(start, update, end time.Time, elapsed float64) bool {
	return timestamp(start) && timestamp(update) && (end.IsZero() || timestamp(end) && !end.Before(start)) && nonnegative(elapsed)
}
func codec(c Codec) bool { return name(c.Name) && c.Version != 0 }
func identity(v *Identity) bool {
	return v == nil || v.App != "" && text(v.App, 128) && text(v.Version, 128) && text(v.Revision, 128)
}
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func client(v Client) bool {
	return oneOf(v.Platform, "windows", "macos", "ios", "android", "linux", "chromeos", "other") && oneOf(v.Browser, "chrome", "safari", "firefox", "edge", "opera", "samsung", "webview", "other") && oneOf(v.Device, "desktop", "phone", "tablet", "bot", "unknown")
}
func fields(f telemetryfields.Fields, total *int) error {
	b, err := telemetryfields.AppendFields(nil, f)
	if err != nil {
		return err
	}
	// The aggregate field cap applies across every participant/event projection.
	*total += len(b)
	if *total > 4096 {
		return telemetryerr.ErrFieldBudget
	}
	return nil
}
func validateVisit(v *Visit) error {
	if !id(v.ID) || !observations(v.StartedAt, v.UpdatedAt, v.EndedAt, v.ElapsedMS) || !nonnegative(v.ActiveMS) || v.ActiveMS > v.ElapsedMS+5000 || !optionalName(v.EndReason) || !text(v.InitialRoute, 128) || !client(v.Client) || (v.VisitorHash != "" && !id(v.VisitorHash)) || !identity(v.Identity) {
		return invalid()
	}
	if v.Vitals != nil {
		for _, p := range []*float64{v.Vitals.LCPMS, v.Vitals.INPMS, v.Vitals.TTFBMS} {
			if p != nil && (!nonnegative(*p) || *p > 60000) {
				return invalid()
			}
		}
		if v.Vitals.CLS != nil && !nonnegative(*v.Vitals.CLS) {
			return invalid()
		}
	}
	total := 0
	return fields(v.Fields, &total)
}
func validateHub(v *HubSession) error {
	if !id(v.ID) || !name(v.Hub) || (v.VisitID != "" && !id(v.VisitID)) || !observations(v.StartedAt, v.UpdatedAt, v.EndedAt, v.ElapsedMS) || !optionalName(v.Reason) || !identity(v.Identity) {
		return invalid()
	}
	return nil
}
func validateActivity(v *Activity) error {
	if !id(v.ID) || (v.ParentID != "" && !id(v.ParentID)) || !name(v.Kind) || len(v.Dimensions) > 2 || !observations(v.StartedAt, v.UpdatedAt, v.EndedAt, v.ElapsedMS) || !optionalName(v.Outcome) || !optionalName(v.Reason) || !codec(v.Codec) || len(v.Participants) > 33 || !identity(v.Identity) {
		return invalid()
	}
	for _, d := range v.Dimensions {
		if !name(d.Name) || d.Value == "" || !text(d.Value, 64) {
			return invalid()
		}
	}
	total := 0
	if err := fields(v.Fields, &total); err != nil {
		return err
	}
	var seats [33]bool
	seenIDs := make(map[string]bool, len(v.Participants))
	for _, p := range v.Participants {
		if !id(p.ID) || p.Seat < -1 || p.Seat > 31 || !name(p.Role) || !nonnegative(p.SeatPresenceMS) || len(p.Reasons) > 36 || len(p.Sessions) > 16 || !codec(p.Codec) || (p.Client != nil && !client(*p.Client)) || seats[p.Seat+1] || seenIDs[p.ID] {
			return invalid()
		}
		seats[p.Seat+1] = true
		seenIDs[p.ID] = true
		var reasons [36]string
		for i, r := range p.Reasons {
			if !name(r.Reason) {
				return invalid()
			}
			for _, old := range reasons[:i] {
				if old == r.Reason {
					return invalid()
				}
			}
			reasons[i] = r.Reason
		}
		var links [16]string
		for i, s := range p.Sessions {
			if !id(s.ID) || (s.VisitID != "" && !id(s.VisitID)) {
				return invalid()
			}
			for _, old := range links[:i] {
				if old == s.ID {
					return invalid()
				}
			}
			links[i] = s.ID
		}
		if err := fields(p.Fields, &total); err != nil {
			return err
		}
	}
	if h := v.TickHealth; h != nil {
		if !h.Available {
			v.TickHealth = nil
		} else if !nonnegative(h.P50MS) || !nonnegative(h.P99MS) || !nonnegative(h.MaxMS) || !nonnegative(h.BudgetMS) || h.P50MS > h.P99MS || h.Overruns > h.Samples || h.OverflowSamples > h.Samples {
			return invalid()
		}
	}
	return nil
}
func validateEvent(v *ActivityEvent) error {
	if !id(v.ActivityID) || v.Seq == 0 || !name(v.Name) || !timestamp(v.ObservedAt) || !codec(v.Codec) {
		return invalid()
	}
	total := 0
	return fields(v.Fields, &total)
}
func validateHealth(v *ClientHealthSummary) error {
	if !timestamp(v.BucketAt) || !text(v.Route, 128) || !name(v.Code) || !oneOf(v.RenderTier, "full", "lite", "ssr", "unknown") || !oneOf(v.Viewport, "small", "medium", "large", "unknown") || v.Count == 0 {
		return invalid()
	}
	return nil
}
