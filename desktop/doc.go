// Package desktop hosts GoSX applications in native desktop windows.
//
// The initial D1 backend targets Windows through the Win32 API and Microsoft
// Edge WebView2 without cgo. Other operating systems return ErrUnsupported
// until their native webview backends are added.
package desktop
