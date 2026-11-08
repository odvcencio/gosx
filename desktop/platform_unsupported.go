//go:build !(windows && (amd64 || arm64)) && !(darwin && !ios && (amd64 || arm64)) && !(linux && (amd64 || arm64))

package desktop

func showPlatformMessage(MessageOptions, uintptr) (MessageResult, error) {
	return "", ErrUnsupported
}

func newPlatformApp(Options) (platformApp, error) {
	return nil, ErrUnsupported
}

func platformAvailable() error {
	return ErrUnsupported
}

// AcquireSingleInstance is unsupported when no Windows desktop backend is available.
func AcquireSingleInstance(string) (*InstanceLock, bool, error) {
	return nil, false, ErrUnsupported
}

// ForwardToFirstInstance is unsupported when no Windows desktop backend is available.
func ForwardToFirstInstance(string, []string, string) error {
	return ErrUnsupported
}

// WebView2RuntimeVersion is unsupported when no Windows WebView2 backend is
// available.
func WebView2RuntimeVersion(string) (string, error) {
	return "", ErrUnsupported
}
