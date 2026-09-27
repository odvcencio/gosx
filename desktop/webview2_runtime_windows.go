//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"syscall"
	"unsafe"
)

var procGetAvailableCoreWebView2BrowserVersionString = modWebView2.NewProc("GetAvailableCoreWebView2BrowserVersionString")

// WebView2RuntimeVersion returns the version in browserExecutableFolder, or
// the installed Evergreen runtime version when the folder is empty.
func WebView2RuntimeVersion(browserExecutableFolder string) (string, error) {
	if err := procCreateCoreWebView2EnvironmentWithOptions.Find(); err != nil {
		return "", webView2LoaderUnavailable(err)
	}
	if err := procGetAvailableCoreWebView2BrowserVersionString.Find(); err != nil {
		return "", webView2LoaderUnavailable(err)
	}

	var folderPtr *uint16
	if browserExecutableFolder != "" {
		var err error
		folderPtr, err = syscall.UTF16PtrFromString(browserExecutableFolder)
		if err != nil {
			return "", fmt.Errorf("%w: browser executable folder: %v", ErrInvalidOptions, err)
		}
	}

	var versionPtr *uint16
	hr, _, _ := procGetAvailableCoreWebView2BrowserVersionString.Call(
		uintptr(unsafe.Pointer(folderPtr)),
		uintptr(unsafe.Pointer(&versionPtr)),
	)
	if failedHRESULT(hr) {
		if versionPtr != nil {
			procCoTaskMemFree.Call(uintptr(unsafe.Pointer(versionPtr)))
		}
		return "", webView2RuntimeUnavailable(hresultError{
			Op:   "GetAvailableCoreWebView2BrowserVersionString",
			Code: hr,
		})
	}
	if versionPtr == nil {
		return "", webView2RuntimeUnavailable(fmt.Errorf("WebView2 returned no version string"))
	}
	version := utf16PtrToString(versionPtr)
	procCoTaskMemFree.Call(uintptr(unsafe.Pointer(versionPtr)))
	if version == "" {
		return "", webView2RuntimeUnavailable(fmt.Errorf("WebView2 returned an empty version string"))
	}
	return version, nil
}
