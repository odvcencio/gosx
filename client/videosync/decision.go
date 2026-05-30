package videosync

// none builds a no-op Decision. ActualRate is 1.0 (nominal playback speed).
// reason is debug-only and is never passed across the hot path in production.
func none(reason string) Decision {
	return Decision{
		Kind:       ActionNone,
		ActualRate: 1.0,
		Reason:     reason,
	}
}
