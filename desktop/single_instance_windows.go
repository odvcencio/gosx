//go:build windows && (amd64 || arm64)

package desktop

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	copyDataInstanceLaunch = uintptr(0x47535831) // GSX1
	errorAlreadyExists     = syscall.Errno(183)

	smtoAbortIfHung = 0x0002
)

var (
	procCreateMutexW = modKernel.NewProc("CreateMutexW")
	procCloseHandle  = modKernel.NewProc("CloseHandle")
)

type singleInstanceLock struct {
	handle uintptr
}

type copyDataStruct struct {
	dwData uintptr
	cbData uint32
	lpData uintptr
}

func acquireSingleInstanceLock(appID string) (*singleInstanceLock, bool, error) {
	name, err := syscall.UTF16PtrFromString(singleInstanceMutexName(appID))
	if err != nil {
		return nil, false, fmt.Errorf("%w: mutex name: %v", ErrInvalidOptions, err)
	}
	handle, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, false, fmt.Errorf("CreateMutexW: %w", callErr)
	}
	owned := !errors.Is(callErr, errorAlreadyExists)
	return &singleInstanceLock{handle: handle}, owned, nil
}

func (l *singleInstanceLock) Close() error {
	if l == nil || l.handle == 0 {
		return nil
	}
	ret, _, callErr := procCloseHandle.Call(l.handle)
	l.handle = 0
	if ret == 0 {
		return fmt.Errorf("CloseHandle: %w", callErr)
	}
	return nil
}

func singleInstanceMutexName(appID string) string {
	return `Global\gosx-` + appID
}

func forwardCurrentLaunch(appID string) error {
	wd, _ := os.Getwd()
	payload, err := BuildInstanceMessage(appID, os.Args[1:], wd)
	if err != nil {
		return err
	}
	hwnd, err := waitForAppWindow(appID, 2*time.Second)
	if err != nil {
		return err
	}
	return sendCopyData(hwnd, payload)
}

func waitForAppWindow(appID string, timeout time.Duration) (uintptr, error) {
	deadline := time.Now().Add(timeout)
	for {
		if hwnd := findAppWindow(appID); hwnd != 0 {
			return hwnd, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("%w: existing instance window not found for %s",
				ErrInvalidOptions, appID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func sendCopyData(hwnd uintptr, payload []byte) error {
	if hwnd == 0 {
		return fmt.Errorf("%w: missing destination window", ErrInvalidOptions)
	}
	if len(payload) == 0 || len(payload) > MaxInstanceMessageBytes {
		return fmt.Errorf("%w: invalid instance message size", ErrInvalidOptions)
	}
	cds := copyDataStruct{
		dwData: copyDataInstanceLaunch,
		cbData: uint32(len(payload)),
		lpData: uintptr(unsafe.Pointer(&payload[0])),
	}
	var result uintptr
	ret, _, callErr := procSendMessageTimeoutW.Call(
		hwnd,
		uintptr(wmCopyData),
		0,
		uintptr(unsafe.Pointer(&cds)),
		smtoAbortIfHung,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
	if ret == 0 {
		return fmt.Errorf("SendMessageTimeoutW: %w", callErr)
	}
	if result == 0 {
		return fmt.Errorf("%w: existing instance rejected launch payload",
			ErrInvalidOptions)
	}
	return nil
}

func (a *windowsApp) handleCopyData(lparam uintptr) bool {
	if lparam == 0 {
		return false
	}
	cds := (*copyDataStruct)(unsafe.Pointer(lparam))
	if cds.dwData != copyDataInstanceLaunch || cds.lpData == 0 ||
		cds.cbData == 0 || cds.cbData > MaxInstanceMessageBytes {
		return false
	}
	payload := unsafe.Slice((*byte)(unsafe.Pointer(cds.lpData)), int(cds.cbData))
	msg, err := ParseInstanceMessage(payload)
	if err != nil || msg.AppID != a.options.AppID {
		return false
	}

	a.mu.Lock()
	cb := a.options.OnSecondInstance
	a.mu.Unlock()
	if cb != nil {
		cb(msg)
	}
	_ = a.Focus()
	return true
}

func setAppWindowProperty(hwnd uintptr, appID string) error {
	name, err := appWindowPropertyNamePtr(appID)
	if err != nil {
		return err
	}
	ret, _, callErr := procSetPropW.Call(hwnd, uintptr(unsafe.Pointer(name)), 1)
	if ret == 0 {
		return fmt.Errorf("SetPropW: %w", callErr)
	}
	return nil
}

func removeAppWindowProperty(hwnd uintptr, appID string) {
	name, err := appWindowPropertyNamePtr(appID)
	if err != nil {
		return
	}
	procRemovePropW.Call(hwnd, uintptr(unsafe.Pointer(name)))
}

func findAppWindow(appID string) uintptr {
	name, err := appWindowPropertyNamePtr(appID)
	if err != nil {
		return 0
	}
	state := enumWindowState{propName: name}
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		s := (*enumWindowState)(unsafe.Pointer(lparam))
		ret, _, _ := procGetPropW.Call(hwnd, uintptr(unsafe.Pointer(s.propName)))
		if ret != 0 {
			s.hwnd = hwnd
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, uintptr(unsafe.Pointer(&state)))
	return state.hwnd
}

type enumWindowState struct {
	propName *uint16
	hwnd     uintptr
}

func appWindowPropertyNamePtr(appID string) (*uint16, error) {
	if err := validateAppID(appID); err != nil {
		return nil, err
	}
	return syscall.UTF16PtrFromString("GoSX.AppID." + appID)
}
