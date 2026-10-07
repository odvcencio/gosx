// Package telemetryerr shares telemetry error identities without import cycles.
package telemetryerr

import "errors"

var (
	ErrAfterBuild     = errors.New("telemetry: configuration closed after build")
	ErrInvalidOptions = errors.New("telemetry: invalid options")
)

// ConfigError identifies the invalid field and a fixed error code. Values and
// credentials must never be included in Field or Code.
type ConfigError struct{ Field, Code string }

func (e *ConfigError) Error() string        { return "telemetry: invalid " + e.Field + " (" + e.Code + ")" }
func (e *ConfigError) Is(target error) bool { return target == ErrInvalidOptions }
