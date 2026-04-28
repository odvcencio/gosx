// Package desktop hosts GoSX applications in native desktop windows.
//
// The initial D1 backend targets Windows through the Win32 API and Microsoft
// Edge WebView2 without cgo. macOS and Linux currently return ErrUnsupported;
// darwin/amd64 and darwin/arm64 are cross-compiled in CI so the unsupported
// path stays buildable while the native macOS backend is developed.
package desktop
