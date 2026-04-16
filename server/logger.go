package server

import (
	"log/slog"
	"os"
	"sync/atomic"
)

var defaultLogger atomic.Pointer[slog.Logger]

func init() {
	defaultLogger.Store(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

// Logger returns the package-level structured logger. The zero value is a
// JSON handler writing to stderr at info level. Replace via SetLogger.
func Logger() *slog.Logger {
	return defaultLogger.Load()
}

// SetLogger replaces the package-level logger. A nil value restores the default.
func SetLogger(l *slog.Logger) {
	if l == nil {
		l = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	defaultLogger.Store(l)
}
