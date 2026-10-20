//go:build !(js && wasm)

package browser

func (DesktopBridge) ServicesAvailable() bool    { return false }
func (DesktopBridge) DiagnosticsAvailable() bool { return false }
func (DesktopBridge) PrivacyAvailable() bool     { return false }
func (DesktopBridge) WindowAvailable() bool      { return false }
func desktopUnavailable(done func(error)) {
	if done != nil {
		done(ErrDesktopUnavailable)
	}
}
func (DesktopBridge) SetFullscreen(_ bool, done func(error)) { desktopUnavailable(done) }
func (DesktopBridge) PreviewDiagnostics(done func(DiagnosticsPlan, error)) {
	if done != nil {
		done(DiagnosticsPlan{}, ErrDesktopUnavailable)
	}
}
func (DesktopBridge) ExportDiagnostics(_ string, done func(error)) { desktopUnavailable(done) }
func (DesktopBridge) BrowserErrorReports(done func(bool, error)) {
	if done != nil {
		done(false, ErrDesktopUnavailable)
	}
}
func (DesktopBridge) CheckForUpdates(done func(bool, error)) {
	if done != nil {
		done(false, ErrDesktopUnavailable)
	}
}
func (DesktopBridge) SetBrowserErrorReports(_ bool, done func(error)) { desktopUnavailable(done) }
func (DesktopBridge) SetCheckForUpdates(_ bool, done func(error))     { desktopUnavailable(done) }
func (DesktopBridge) DeleteMyData(done func(error))                   { desktopUnavailable(done) }
