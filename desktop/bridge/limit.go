package bridge

// Limit caps inbound traffic from the page. The all-zero value means
// DefaultLimit. Once any field is set, zero values disable that specific
// check so callers can override size without rate limiting, or vice versa.
type Limit struct {
	// MaxBytes is the largest accepted JSON-encoded envelope. Messages
	// larger than this are rejected with CodeTooLarge. 0 disables the
	// cap. Recommended default for a GoSX desktop: 64 KiB, matching the
	// /_gosx/client-events server-side cap.
	MaxBytes int

	// Rate is the steady-state allowed message rate in messages per
	// second. 0 disables rate limiting. Recommended default: 200 msg/s
	// — generous for typed user interaction, tight enough that a
	// runaway page script trips it.
	Rate float64

	// Burst is the token-bucket burst capacity. If Rate > 0 and Burst
	// is 0, withDefaults promotes it to int(Rate).
	Burst int
}

// DefaultLimit is the recommended production limit: 64 KiB per message,
// 200 messages per second, burst of 400.
var DefaultLimit = Limit{
	MaxBytes: 64 * 1024,
	Rate:     200,
	Burst:    400,
}

// withDefaults fills in sensible defaults. The struct zero value maps to
// DefaultLimit; non-zero partial configs preserve explicit zero disables.
func (l Limit) withDefaults() Limit {
	if l == (Limit{}) {
		return DefaultLimit
	}
	if l.Rate > 0 && l.Burst == 0 {
		l.Burst = int(l.Rate)
	}
	return l
}
