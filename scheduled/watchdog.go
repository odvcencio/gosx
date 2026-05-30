package scheduled

import "time"

// watchdogVerdict is the result of evaluating watchdog thresholds.
type watchdogVerdict int

const (
	// verdictNone means neither threshold has been exceeded.
	verdictNone watchdogVerdict = iota
	// verdictStall means the progress timeout has been exceeded (within the hard deadline).
	verdictStall
	// verdictTimeout means the hard deadline has been exceeded.
	verdictTimeout
)

// evaluate checks whether the hard timeout or progress stall threshold has been
// exceeded. Hard timeout takes precedence over stall.
//
//   - timeout <= 0 disables the hard deadline check.
//   - progressTimeout <= 0 disables the stall check.
func evaluate(now, startedAt, lastProgressAt time.Time, timeout, progressTimeout time.Duration) watchdogVerdict {
	if timeout > 0 && now.Sub(startedAt) >= timeout {
		return verdictTimeout
	}
	if progressTimeout > 0 && now.Sub(lastProgressAt) >= progressTimeout {
		return verdictStall
	}
	return verdictNone
}
