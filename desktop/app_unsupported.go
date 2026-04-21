//go:build !windows || (windows && !amd64 && !arm64)

package desktop

import (
	"fmt"
	"net/http"
)

type unsupportedApp struct{}

func newPlatformApp(Options) (platformApp, error) {
	return nil, ErrUnsupported
}

func platformAvailable() error {
	return ErrUnsupported
}

func (unsupportedApp) Run() error {
	return ErrUnsupported
}

func (unsupportedApp) Close() error {
	return nil
}

func (unsupportedApp) Navigate(url string) error {
	if url == "" {
		return fmt.Errorf("%w: empty url", ErrInvalidOptions)
	}
	return ErrUnsupported
}

func (unsupportedApp) SetHTML(string) error {
	return ErrUnsupported
}

func (unsupportedApp) PostMessage(string) error {
	return ErrUnsupported
}

func (unsupportedApp) ExecuteScript(string) error {
	return ErrUnsupported
}

func (unsupportedApp) OpenDevTools() error {
	return ErrUnsupported
}

func (unsupportedApp) PrependBootstrapScript(string) error {
	return ErrUnsupported
}

func (unsupportedApp) Minimize() error { return ErrUnsupported }
func (unsupportedApp) Maximize() error { return ErrUnsupported }
func (unsupportedApp) Restore() error  { return ErrUnsupported }
func (unsupportedApp) Focus() error    { return ErrUnsupported }
func (unsupportedApp) SetTitle(string) error {
	return ErrUnsupported
}

func (unsupportedApp) Serve(string, http.Handler) error {
	return ErrUnsupported
}

func (unsupportedApp) OpenFileDialog(OpenFileOptions) ([]string, error) {
	return nil, ErrUnsupported
}

func (unsupportedApp) SaveFileDialog(SaveFileOptions) (string, error) {
	return "", ErrUnsupported
}

func (unsupportedApp) Clipboard() (string, error) {
	return "", ErrUnsupported
}

func (unsupportedApp) SetClipboard(string) error {
	return ErrUnsupported
}

func (unsupportedApp) OpenURL(string) error {
	return ErrUnsupported
}

func (unsupportedApp) SetFullscreen(bool) error  { return ErrUnsupported }
func (unsupportedApp) SetMinSize(int, int) error { return ErrUnsupported }
func (unsupportedApp) SetMaxSize(int, int) error { return ErrUnsupported }

func (unsupportedApp) NewWindow(WindowOptions) (*Window, error) {
	return nil, ErrUnsupported
}

func (unsupportedApp) RegisterProtocol(string) error {
	return ErrUnsupported
}

func (unsupportedApp) RegisterFileType(string, string, string) error {
	return ErrUnsupported
}

func (unsupportedApp) SetMenuBar(Menu) error {
	return ErrUnsupported
}

func (unsupportedApp) SetTray(TrayOptions) error {
	return ErrUnsupported
}

func (unsupportedApp) CloseTray() error {
	return ErrUnsupported
}

func (unsupportedApp) Notify(Notification) error {
	return ErrUnsupported
}

func (unsupportedApp) SetFileDropHandler(func([]string)) error {
	return ErrUnsupported
}
