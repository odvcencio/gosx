//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"syscall"
	"unsafe"
)

func showPlatformMessage(options MessageOptions, owner uintptr) (MessageResult, error) {
	text, err := syscall.UTF16PtrFromString(options.Text)
	if err != nil {
		return "", fmt.Errorf("encode message text: %w", err)
	}
	title, err := syscall.UTF16PtrFromString(options.Title)
	if err != nil {
		return "", fmt.Errorf("encode message title: %w", err)
	}
	result, _, callErr := procMessageBoxW.Call(
		owner,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		uintptr(messageBoxFlags(options.Kind, options.Buttons)),
	)
	if result == 0 {
		return "", fmt.Errorf("MessageBoxW failed: %v", callErr)
	}
	return messageResultFromID(int(result)), nil
}
