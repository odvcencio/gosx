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

	// ShowWindow nCmdShow values.
	swHide           = 0
	swShowNormal     = 1
	swShowMinimized  = 2
	swShowMaximized  = 3
	swShowNoActivate = 4
	swShow           = 5
	swMinimize       = 6
	swShowDefault    = 10
	swRestore        = 9

	wmClose          = 0x0010
	wmDestroy        = 0x0002
	wmCommand        = 0x0111
	wmContextMenu    = 0x007B
	wmCopyData       = 0x004A
	wmDropFiles      = 0x0233
	wmGetObject      = 0x003D
	wmSize           = 0x0005
	wmGetMinMaxInfo  = 0x0024
	wmKeyDown        = 0x0100
	wmPowerBroadcast = 0x0218
	wmAppTray        = 0x8001

	vkF12 = 0x7B

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000

	// GetWindowLongPtrW / SetWindowLongPtrW indices. Windows passes the
	// index as an int that can be negative (GWL_STYLE == -16). We encode
	// the two's-complement bit pattern so uintptr conversions don't
	// overflow at compile time.
	gwlStyle = uintptr(0xFFFFFFFFFFFFFFF0)

	// SetWindowPos uFlags.
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	swpNoCopyBits   = 0x0100
	swpShowWindow   = 0x0040

	// MonitorFromWindow dwFlags.
	monitorDefaultToNearest = 0x00000002

	// GetSystemMetrics indices.
	smCxScreen = 0
	smCyScreen = 1

	// Per-monitor DPI-aware v2 context. Matches DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2.
	dpiAwarenessContextPerMonitorV2 = uintptr(0xFFFFFFFC) // ~3 as a uintptr (cast back to HANDLE)
)

var (
	modOle32  = syscall.NewLazyDLL("ole32.dll")
	modUser32 = syscall.NewLazyDLL("user32.dll")
	modKernel = syscall.NewLazyDLL("kernel32.dll")

	procCoInitializeEx = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize = modOle32.NewProc("CoUninitialize")

	procGetModuleHandleW = modKernel.NewProc("GetModuleHandleW")

	procRegisterClassExW    = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW     = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW      = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow       = modUser32.NewProc("DestroyWindow")
	procDispatchMessageW    = modUser32.NewProc("DispatchMessageW")
	procGetClientRect       = modUser32.NewProc("GetClientRect")
	procGetMessageW         = modUser32.NewProc("GetMessageW")
	procLoadCursorW         = modUser32.NewProc("LoadCursorW")
	procPostMessageW        = modUser32.NewProc("PostMessageW")
	procPostQuitMessage     = modUser32.NewProc("PostQuitMessage")
	procShowWindow          = modUser32.NewProc("ShowWindow")
	procTranslateMessage    = modUser32.NewProc("TranslateMessage")
	procUpdateWindow        = modUser32.NewProc("UpdateWindow")
	procSetWindowTextW      = modUser32.NewProc("SetWindowTextW")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procSetPropW            = modUser32.NewProc("SetPropW")
	procRemovePropW         = modUser32.NewProc("RemovePropW")
	procGetPropW            = modUser32.NewProc("GetPropW")
	procEnumWindows         = modUser32.NewProc("EnumWindows")
	procSendMessageTimeoutW = modUser32.NewProc("SendMessageTimeoutW")

	// SetProcessDpiAwarenessContext is Win10 1703+. NewLazyProc resolves
	// at first .Call(), so older systems silently fall back when the Find()
	// probe fails in setDPIAwareness.
	procSetProcessDpiAwarenessContext = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = modUser32.NewProc("SetProcessDPIAware")

	procSetWindowLongPtrW = modUser32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowPos      = modUser32.NewProc("SetWindowPos")
	procGetWindowRect     = modUser32.NewProc("GetWindowRect")
	procMonitorFromWindow = modUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW   = modUser32.NewProc("GetMonitorInfoW")
	procGetSystemMetrics  = modUser32.NewProc("GetSystemMetrics")
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
	if err := setAppWindowProperty(hwnd, app.options.AppID); err != nil {
		destroyWindow(hwnd)
		return 0, err
	}
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

// showWindowState drives the window into a specific show state — minimized,
// maximized, restored, or hidden. Passing swRestore brings a minimized or
// maximized window back to its previous bounds; swMinimize hides it in the
// taskbar; swShowMaximized snaps it to monitor extents.
func showWindowState(hwnd uintptr, state int) {
	if hwnd == 0 {
		return
	}
	procShowWindow.Call(hwnd, uintptr(state))
}

// focusWindow brings hwnd to the foreground. No-op when the OS refuses
// activation due to foreground-lock rules — the caller has no recourse
// except via AllowSetForegroundWindow from a partner process.
func focusWindow(hwnd uintptr) error {
	if hwnd == 0 {
		return fmt.Errorf("%w: focus requires a live window", ErrInvalidOptions)
	}
	procSetForegroundWindow.Call(hwnd)
	return nil
}

// setWindowTitle rewrites the window caption via SetWindowTextW. Short,
// blocking call — fires a WM_SETTEXT message that the default window
// procedure handles.
func setWindowTitle(hwnd uintptr, title string) error {
	if hwnd == 0 {
		return fmt.Errorf("%w: title change requires a live window", ErrInvalidOptions)
	}
	ptr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return fmt.Errorf("%w: title: %v", ErrInvalidOptions, err)
	}
	ret, _, callErr := procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
	if ret == 0 {
		return fmt.Errorf("SetWindowTextW: %w", callErr)
	}
	return nil
}

// setDPIAwareness opts the process into the requested DPI semantics before
// the first HWND is created. Per-monitor-v2 falls back to system-DPI aware
// when older Windows builds do not expose the v2 context API.
func setDPIAwareness(mode DPIAwareness) {
	switch mode {
	case DPIAwarenessUnaware:
		return
	case DPIAwarenessSystem:
		procSetProcessDPIAware.Call()
	default:
		if err := procSetProcessDpiAwarenessContext.Find(); err == nil {
			procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorV2)
			return
		}
		procSetProcessDPIAware.Call()
	}
}

// monitorInfo is the Win32 MONITORINFO used to query the current monitor's
// working rect so fullscreen can cover it precisely.
type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

// mINMAXINFO is the payload of a WM_GETMINMAXINFO message. We intercept
// the message and write caller-configured min/max tracking sizes in place.
type mINMAXINFO struct {
	ptReserved     point
	ptMaxSize      point
	ptMaxPosition  point
	ptMinTrackSize point
	ptMaxTrackSize point
}

// currentMonitorWorkArea returns the work rect (excluding taskbar) of the
// monitor hosting hwnd. Used to size the window during fullscreen.
func currentMonitorWorkArea(hwnd uintptr) (rect, bool) {
	hmonitor, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if hmonitor == 0 {
		return rect{}, false
	}
	var mi monitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	ret, _, _ := procGetMonitorInfoW.Call(hmonitor, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return rect{}, false
	}
	return mi.rcMonitor, true
}

// applyFullscreen switches hwnd between borderless-fullscreen and the
// saved pre-fullscreen style+bounds. `enabled` drives the toggle.
//
// The saved style/bounds live on the caller-owned fullscreenState struct
// so multiple toggle calls don't accumulate corrupted state.
func applyFullscreen(hwnd uintptr, state *fullscreenState, enabled bool) error {
	if hwnd == 0 {
		return fmt.Errorf("%w: fullscreen requires a live window", ErrInvalidOptions)
	}
	if enabled {
		if state.active {
			return nil
		}
		style, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle))
		var r rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		state.savedStyle = uint32(style)
		state.savedBounds = r
		state.active = true

		procSetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle), uintptr(wsPopup))

		area, ok := currentMonitorWorkArea(hwnd)
		if !ok {
			// Fallback to the primary screen via GetSystemMetrics.
			w, _, _ := procGetSystemMetrics.Call(smCxScreen)
			h, _, _ := procGetSystemMetrics.Call(smCyScreen)
			area = rect{Left: 0, Top: 0, Right: int32(w), Bottom: int32(h)}
		}
		procSetWindowPos.Call(hwnd, 0,
			uintptr(area.Left), uintptr(area.Top),
			uintptr(area.Right-area.Left), uintptr(area.Bottom-area.Top),
			uintptr(swpNoZOrder|swpFrameChanged|swpShowWindow))
		return nil
	}

	if !state.active {
		return nil
	}
	procSetWindowLongPtrW.Call(hwnd, uintptr(gwlStyle), uintptr(state.savedStyle))
	r := state.savedBounds
	procSetWindowPos.Call(hwnd, 0,
		uintptr(r.Left), uintptr(r.Top),
		uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top),
		uintptr(swpNoZOrder|swpFrameChanged|swpShowWindow))
	state.active = false
	return nil
}

// fullscreenState remembers pre-fullscreen chrome + placement so exiting
// fullscreen restores the user's window instead of producing a maximized-
// looking popup.
type fullscreenState struct {
	active      bool
	savedStyle  uint32
	savedBounds rect
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
	case wmCommand:
		if app != nil && app.handleMenuCommand(wparam) {
			return 0
		}
	case wmContextMenu:
		if app != nil && app.showContextMenu(hwnd, lparam) {
			return 0
		}
	case wmCopyData:
		if app != nil && app.handleCopyData(lparam) {
			return 1
		}
	case wmDropFiles:
		if app != nil {
			app.handleDropFiles(wparam)
			return 0
		}
	case wmAppTray:
		if app != nil && app.handleTrayMessage(lparam) {
			return 0
		}
	case wmKeyDown:
		if app != nil && wparam == vkF12 {
			app.openDevToolsFromShortcut()
			return 0
		}
	case wmGetObject:
		if app != nil {
			if ret := app.handleAccessibilityObject(wparam, lparam); ret != 0 {
				return ret
			}
		}
	case wmSize:
		if app != nil {
			_ = app.resizeWebView()
		}
	case wmGetMinMaxInfo:
		// Let the caller clamp the resize range. The MINMAXINFO struct
		// is at lparam; we mutate its MinTrackSize / MaxTrackSize fields
		// with whatever the app has configured via SetMinSize / SetMaxSize.
		if app != nil && lparam != 0 {
			info := (*mINMAXINFO)(unsafe.Pointer(lparam))
			app.applyMinMaxTo(info)
		}
	case wmPowerBroadcast:
		if app != nil {
			switch uint32(wparam) {
			case pbtAPMSuspend:
				app.onSuspend()
			case pbtAPMResumeAutomatic, pbtAPMResumeSuspend:
				app.onResume()
			}
			return 1
		}
	case wmClose:
		if hwnd != 0 {
			destroyWindow(hwnd)
			return 0
		}
	case wmDestroy:
		if app != nil {
			removeAppWindowProperty(hwnd, app.options.AppID)
			app.disposeNativeUI()
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
