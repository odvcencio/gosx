//go:build !windows || (windows && !amd64 && !arm64)

package desktop

// GPUAdapters is unsupported when no Windows desktop backend is compiled.
func GPUAdapters() ([]GPUAdapter, error) {
	return nil, ErrUnsupported
}
