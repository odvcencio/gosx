//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const (
	coinitApartmentThreaded = 0x2

	cwUseDefault = 0x80000000
	idcArrow     = 32512

	colorWindow = 5

	swShowDefault = 10

	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmSize    = 0x0005

	wsOverlappedWindow = 0x00CF0000
)

var (
	modOle32  = syscall.NewLazyDLL("ole32.dll")
	modUser32 = syscall.NewLazyDLL("user32.dll")
	modKernel = syscall.NewLazyDLL("kernel32.dll")

	procCoInitializeEx = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize = modOle32.NewProc("CoUninitialize")

	procGetModuleHandleW = modKernel.NewProc("GetModuleHandleW")

	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow    = modUser32.NewProc("DestroyWindow")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procGetClientRect    = modUser32.NewProc("GetClientRect")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procLoadCursorW      = modUser32.NewProc("LoadCursorW")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procUpdateWindow     = modUser32.NewProc("UpdateWindow")
)

type point struct {
	X int32
	Y int32
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type msg struct {
	HWND    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

var (
	windowClassName = syscall.StringToUTF16Ptr("GoSXDesktopWindow")
	windowProc      = syscall.NewCallback(desktopWndProc)
	registerOnce    sync.Once
	registerErr     error

	windowMu   sync.Mutex
	windowApps = map[uintptr]*windowsApp{}
)

func coInitializeApartment() error {
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if failedHRESULT(hr) {
		return hresultError{Op: "CoInitializeEx", Code: hr}
	}
	return nil
}

func coUninitialize() {
	procCoUninitialize.Call()
}

func createDesktopWindow(title string, width, height int, app *windowsApp) (uintptr, error) {
	if err := registerWindowClass(); err != nil {
		return 0, err
	}

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, fmt.Errorf("%w: title: %v", ErrInvalidOptions, err)
	}
	instance, err := moduleHandle()
	if err != nil {
		return 0, err
	}

	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(windowClassName)),
		uintptr(unsafe.Pointer(titlePtr)),
		wsOverlappedWindow,
		cwUseDefault,
		cwUseDefault,
		uintptr(width),
		uintptr(height),
		0,
		0,
		instance,
		0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowExW: %w", callErr)
	}

	windowMu.Lock()
	windowApps[hwnd] = app
	windowMu.Unlock()
	return hwnd, nil
}

func registerWindowClass() error {
	registerOnce.Do(func() {
		instance, err := moduleHandle()
		if err != nil {
			registerErr = err
			return
		}
		cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		class := wndClassEx{
			CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
			LpfnWndProc:   windowProc,
			HInstance:     instance,
			HCursor:       cursor,
			HbrBackground: colorWindow + 1,
			LpszClassName: windowClassName,
		}
		atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
		if atom == 0 {
			registerErr = fmt.Errorf("RegisterClassExW: %w", callErr)
		}
	})
	return registerErr
}

func moduleHandle() (uintptr, error) {
	instance, _, callErr := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return 0, fmt.Errorf("GetModuleHandleW: %w", callErr)
	}
	return instance, nil
}

func showWindow(hwnd uintptr) {
	procShowWindow.Call(hwnd, swShowDefault)
	procUpdateWindow.Call(hwnd)
}

func destroyWindow(hwnd uintptr) {
	procDestroyWindow.Call(hwnd)
}

func postMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) {
	procPostMessageW.Call(hwnd, uintptr(message), wparam, lparam)
}

func runMessageLoop() error {
	var m msg
	for {
		ret, _, callErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(ret) {
		case -1:
			return fmt.Errorf("GetMessageW: %w", callErr)
		case 0:
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func clientRect(hwnd uintptr) (rect, error) {
	var r rect
	ok, _, callErr := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return rect{}, fmt.Errorf("GetClientRect: %w", callErr)
	}
	return r, nil
}

func desktopWndProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	windowMu.Lock()
	app := windowApps[hwnd]
	windowMu.Unlock()

	switch message {
	case wmSize:
		if app != nil {
			_ = app.resizeWebView()
		}
	case wmClose:
		if hwnd != 0 {
			destroyWindow(hwnd)
			return 0
		}
	case wmDestroy:
		if app != nil {
			app.releaseWebView()
		}
		windowMu.Lock()
		delete(windowApps, hwnd)
		windowMu.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return ret
}
