package server

import (
	"context"
	"strings"
)

// ReadyCheck validates whether the app is ready to serve traffic.
type ReadyCheck interface {
	CheckReady(context.Context) error
}

// ReadyCheckFunc adapts a function into a readiness check.
type ReadyCheckFunc func(context.Context) error

// CheckReady runs the readiness check.
func (fn ReadyCheckFunc) CheckReady(ctx context.Context) error {
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

type namedReadyCheck struct {
	name  string
	check ReadyCheck
}

// ReadinessCheckResult reports the result of one named readiness check.
type ReadinessCheckResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ReadinessReport is the JSON shape served by `/readyz`.
type ReadinessReport struct {
	OK     bool                   `json:"ok"`
	Checks []ReadinessCheckResult `json:"checks,omitempty"`
}

func normalizeReadyCheckName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unnamed"
	}
	return name
}
