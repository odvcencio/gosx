//go:build !windows || (windows && !amd64 && !arm64)

package desktop

import "fmt"

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
