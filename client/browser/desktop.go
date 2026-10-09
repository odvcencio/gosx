package browser

import (
	"context"
	"errors"
)

var ErrDesktopUnavailable = errors.New("browser: desktop bridge unavailable")

// DiagnosticsFile is one file in a user-reviewable diagnostic export plan.
type DiagnosticsFile struct {
	Name string
	Size float64
}

// DiagnosticsPlan is the opaque plan ID and file list supplied by the desktop
// diagnostics service. The ID binds export to the preview the user reviewed.
type DiagnosticsPlan struct {
	ID            string
	Files         []DiagnosticsFile
	TotalSizeText string
}

// DesktopBridge exposes the supported desktop services as typed operations.
// Bridge identity and capability checks occur at each operation. Methods
// initiate calls synchronously, then deliver promise results asynchronously.
type DesktopBridge struct{ ctx context.Context }

func Desktop() DesktopBridge { return DesktopBridge{ctx: context.Background()} }

// WithContext bounds pending service waits. Cancellation releases Go promise
// handlers; the desktop process may still finish the already-started action.
func (d DesktopBridge) WithContext(ctx context.Context) DesktopBridge { d.ctx = ctx; return d }
func (d DesktopBridge) context() context.Context {
	if d.ctx == nil {
		return context.Background()
	}
	return d.ctx
}
