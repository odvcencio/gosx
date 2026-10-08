// Package telemetryerr shares telemetry error identities without import cycles.
package telemetryerr

import "errors"

var (
	ErrAlreadyEnabled      = errors.New("telemetry: already enabled")
	ErrAfterBuild          = errors.New("telemetry: configuration closed after build")
	ErrInvalidOptions      = errors.New("telemetry: invalid options")
	ErrInsecureListener    = errors.New("telemetry: insecure listener")
	ErrUnsupportedPlatform = errors.New("telemetry: unsupported platform")
	ErrClosed              = errors.New("telemetry: closed")
	ErrCapacity            = errors.New("telemetry: capacity exceeded")
	ErrQueueFull           = errors.New("telemetry: queue full")
	ErrFieldBudget         = errors.New("telemetry: field budget exceeded")
	ErrConflict            = errors.New("telemetry: conflicting registration or revision")
	ErrStorageUnavailable  = errors.New("telemetry: storage unavailable")
	ErrNotDurable          = errors.New("telemetry: not durable")
	ErrCorrupt             = errors.New("telemetry: corrupt storage")
	ErrObjectCollision     = errors.New("telemetry: object collision")
)

// ConfigError identifies the invalid field and a fixed error code. Values and
// credentials must never be included in Field or Code.
type ConfigError struct{ Field, Code string }

func (e *ConfigError) Error() string        { return "telemetry: invalid " + e.Field + " (" + e.Code + ")" }
func (e *ConfigError) Is(target error) bool { return target == ErrInvalidOptions }
