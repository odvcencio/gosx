//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	mfString    = 0x00000000
	mfChecked   = 0x00000008
	mfPopup     = 0x00000010
	mfGrayed    = 0x00000001
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	notifyIconVersion4 = 4
	niifInfo           = 0x00000001

	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	lrDefaultSize  = 0x00000040
	idiApplication = 32512

	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205

	dragQueryFileCount = 0xffffffff
)

var (
	procSetCurrentProcessExplicitAppUserModelID = modShell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	procShellNotifyIconW                        = modShell32.NewProc("Shell_NotifyIconW")
	procDragAcceptFiles                         = modShell32.NewProc("DragAcceptFiles")
	procDragQueryFileW                          = modShell32.NewProc("DragQueryFileW")
	procDragFinish                              = modShell32.NewProc("DragFinish")

	procAppendMenuW     = modUser32.NewProc("AppendMenuW")
	procCreateMenu      = modUser32.NewProc("CreateMenu")
	procCreatePopupMenu = modUser32.NewProc("CreatePopupMenu")
	procDestroyIcon     = modUser32.NewProc("DestroyIcon")
	procDestroyMenu     = modUser32.NewProc("DestroyMenu")
	procDrawMenuBar     = modUser32.NewProc("DrawMenuBar")
	procGetCursorPos    = modUser32.NewProc("GetCursorPos")
	procLoadIconW       = modUser32.NewProc("LoadIconW")
	procLoadImageW      = modUser32.NewProc("LoadImageW")
	procSetMenu         = modUser32.NewProc("SetMenu")
	procTrackPopupMenu  = modUser32.NewProc("TrackPopupMenu")
)

type windowsTray struct {
	options TrayOptions
	hicon   uintptr
	menu    uintptr
	uid     uint32
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type notifyIconDataW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         guid
	HBalloonIcon     uintptr
}

func setCurrentProcessAppUserModelID(appID string) error {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil
	}
	if err := procSetCurrentProcessExplicitAppUserModelID.Find(); err != nil {
		return nil
	}
	ptr, err := syscall.UTF16PtrFromString(appID)
	if err != nil {
		return fmt.Errorf("%w: app user model id: %v", ErrInvalidOptions, err)
	}
	hr, _, _ := procSetCurrentProcessExplicitAppUserModelID.Call(uintptr(unsafe.Pointer(ptr)))
	if failedHRESULT(hr) {
		return hresultError{Op: "SetCurrentProcessExplicitAppUserModelID", Code: hr}
	}
	return nil
}

func applyWindowAccessibility(uintptr, Options) error {
	// WebView2 owns content UIA. Phase 5 exposes metadata/options publicly
	// and leaves deep custom provider work to a later accessibility pass.
	return nil
}

func (a *windowsApp) handleAccessibilityObject(uintptr, uintptr) uintptr {
	return 0
}

func (a *windowsApp) installPendingNativeUI(hwnd uintptr) error {
	a.mu.Lock()
	menuBar := a.pendingMenuBar
	tray := a.pendingTray
	a.mu.Unlock()

	if menuBar != nil {
		if err := a.applyMenuBar(hwnd, *menuBar); err != nil {
			return err
		}
	}
	if tray != nil {
		if err := a.installTray(hwnd, *tray); err != nil {
			return err
		}
	}
	return nil
}

func (a *windowsApp) SetMenuBar(menu Menu) error {
	if _, err := BuildMenuPlan(menu); err != nil {
		return err
	}
	a.mu.Lock()
	a.pendingMenuBar = &menu
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd == 0 {
		return nil
	}
	return a.applyMenuBar(hwnd, menu)
}

func (a *windowsApp) applyMenuBar(hwnd uintptr, menu Menu) error {
	handle, actions, err := a.buildWindowsMenu(menu, true)
	if err != nil {
		return err
	}
	ret, _, callErr := procSetMenu.Call(hwnd, handle)
	if ret == 0 {
		destroyMenu(handle)
		return fmt.Errorf("SetMenu: %w", callErr)
	}
	procDrawMenuBar.Call(hwnd)

	a.mu.Lock()
	old := a.menuBar
	a.menuBar = handle
	a.mergeMenuActionsLocked(actions)
	a.mu.Unlock()
	if old != 0 {
		destroyMenu(old)
	}
	return nil
}

func (a *windowsApp) setWindowContextMenu(hwnd uintptr, menu Menu) error {
	if _, err := BuildMenuPlan(menu); err != nil {
		return err
	}
	handle, actions, err := a.buildWindowsMenu(menu, false)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.contextMenus == nil {
		a.contextMenus = map[uintptr]uintptr{}
	}
	old := a.contextMenus[hwnd]
	a.contextMenus[hwnd] = handle
	a.mergeMenuActionsLocked(actions)
	a.mu.Unlock()
	if old != 0 {
		destroyMenu(old)
	}
	return nil
}

func (a *windowsApp) buildWindowsMenu(menu Menu, menuBar bool) (uintptr, map[uint16]func(), error) {
	a.mu.Lock()
	start := a.nextNativeCommandID
	if start == 0 {
		start = firstNativeCommandID
	}
	plan, err := buildMenuPlan(menu, start)
	if err != nil {
		a.mu.Unlock()
		return 0, nil, err
	}
	a.nextNativeCommandID = plan.NextCommandID
	a.mu.Unlock()

	handle, err := createMenuHandle(plan.Items, menuBar)
	if err != nil {
		return 0, nil, err
	}
	actions := map[uint16]func(){}
	collectMenuActions(plan.Items, menu.Items, actions)
	return handle, actions, nil
}

func createMenuHandle(items []MenuPlanItem, menuBar bool) (uintptr, error) {
	var handle uintptr
	var callErr error
	if menuBar {
		handle, _, callErr = procCreateMenu.Call()
	} else {
		handle, _, callErr = procCreatePopupMenu.Call()
	}
	if handle == 0 {
		return 0, fmt.Errorf("CreateMenu: %w", callErr)
	}
	for _, item := range items {
		flags := uintptr(mfString)
		if item.Separator {
			ret, _, err := procAppendMenuW.Call(handle, mfSeparator, 0, 0)
			if ret == 0 {
				destroyMenu(handle)
				return 0, fmt.Errorf("AppendMenuW(separator): %w", err)
			}
			continue
		}
		if item.Disabled {
			flags |= mfGrayed
		}
		if item.Checked {
			flags |= mfChecked
		}
		label, err := syscall.UTF16PtrFromString(item.Label)
		if err != nil {
			destroyMenu(handle)
			return 0, fmt.Errorf("%w: menu label: %v", ErrInvalidOptions, err)
		}
		itemID := uintptr(item.CommandID)
		if len(item.Items) > 0 {
			child, err := createMenuHandle(item.Items, false)
			if err != nil {
				destroyMenu(handle)
				return 0, err
			}
			flags |= mfPopup
			itemID = child
		}
		ret, _, err := procAppendMenuW.Call(handle, flags, itemID, uintptr(unsafe.Pointer(label)))
		if ret == 0 {
			destroyMenu(handle)
			return 0, fmt.Errorf("AppendMenuW(%q): %w", item.Label, err)
		}
	}
	return handle, nil
}

func collectMenuActions(plan []MenuPlanItem, items []MenuItem, actions map[uint16]func()) {
	for i, planItem := range plan {
		if i >= len(items) {
			return
		}
		item := items[i]
		if len(planItem.Items) > 0 && item.Submenu != nil {
			collectMenuActions(planItem.Items, item.Submenu.Items, actions)
			continue
		}
		if planItem.CommandID != 0 && item.OnClick != nil {
			actions[planItem.CommandID] = item.OnClick
		}
	}
}

func (a *windowsApp) mergeMenuActionsLocked(actions map[uint16]func()) {
	if a.menuActions == nil {
		a.menuActions = map[uint16]func(){}
	}
	for id, action := range actions {
		a.menuActions[id] = action
	}
}

func (a *windowsApp) handleMenuCommand(wparam uintptr) bool {
	id := uint16(wparam & 0xffff)
	a.mu.Lock()
	action := a.menuActions[id]
	a.mu.Unlock()
	if action == nil {
		return false
	}
	action()
	return true
}

func (a *windowsApp) showContextMenu(hwnd uintptr, lparam uintptr) bool {
	a.mu.Lock()
	var menu uintptr
	if a.contextMenus != nil {
		menu = a.contextMenus[hwnd]
	}
	a.mu.Unlock()
	if menu == 0 {
		return false
	}
	pt := point{X: int32(int16(lparam & 0xffff)), Y: int32(int16((lparam >> 16) & 0xffff))}
	if lparam == ^uintptr(0) {
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	}
	procSetForegroundWindow.Call(hwnd)
	procTrackPopupMenu.Call(menu, tpmRightButton, uintptr(uint32(pt.X)), uintptr(uint32(pt.Y)), 0, hwnd, 0)
	return true
}

func (a *windowsApp) SetTray(options TrayOptions) error {
	if _, err := BuildTrayRegistration(a.options.AppID, options); err != nil {
		return err
	}
	a.mu.Lock()
	a.pendingTray = &options
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd == 0 {
		return nil
	}
	return a.installTray(hwnd, options)
}

func (a *windowsApp) installTray(hwnd uintptr, options TrayOptions) error {
	menu, actions, err := a.buildWindowsMenu(options.Menu, false)
	if err != nil {
		return err
	}
	hicon, err := loadTrayIcon(options.Icon)
	if err != nil {
		if menu != 0 {
			destroyMenu(menu)
		}
		return err
	}
	a.mu.Lock()
	old := a.tray
	a.tray = nil
	a.mu.Unlock()
	if old != nil {
		old.close(hwnd)
	}

	tray := &windowsTray{options: options, hicon: hicon, menu: menu, uid: 1}
	data := trayNotifyData(hwnd, tray)
	data.UFlags = nifMessage | nifIcon | nifTip
	data.UCallbackMessage = wmAppTray
	copyUTF16Fixed(data.SzTip[:], options.Tooltip)
	ret, _, callErr := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
	if ret == 0 {
		tray.close(hwnd)
		return fmt.Errorf("Shell_NotifyIconW(NIM_ADD): %w", callErr)
	}
	data.UVersion = notifyIconVersion4
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&data)))

	a.mu.Lock()
	a.tray = tray
	a.mergeMenuActionsLocked(actions)
	a.mu.Unlock()
	return nil
}

func (a *windowsApp) CloseTray() error {
	a.mu.Lock()
	hwnd := a.hwnd
	tray := a.tray
	a.tray = nil
	a.pendingTray = nil
	a.mu.Unlock()
	if tray != nil {
		tray.close(hwnd)
	}
	return nil
}

func (a *windowsApp) Notify(notification Notification) error {
	if _, err := BuildToastPayload(notification); err != nil {
		return err
	}
	// Phase 5 validates the ToastGeneric XML payload but invokes through
	// Shell_NotifyIcon's notification path. A full WinRT
	// ToastNotificationManager bridge can replace this without changing the
	// public Notification API.
	a.mu.Lock()
	hwnd := a.hwnd
	tray := a.tray
	a.mu.Unlock()
	if hwnd == 0 {
		return fmt.Errorf("%w: notification requires a live window", ErrInvalidOptions)
	}
	if tray == nil {
		if err := a.installTray(hwnd, TrayOptions{Tooltip: a.options.Title}); err != nil {
			return err
		}
		a.mu.Lock()
		tray = a.tray
		a.mu.Unlock()
	}
	if tray == nil {
		return fmt.Errorf("%w: tray registration unavailable for notification", ErrInvalidOptions)
	}
	data := trayNotifyData(hwnd, tray)
	data.UFlags = nifInfo
	data.DwInfoFlags = niifInfo
	copyUTF16Fixed(data.SzInfoTitle[:], notification.Title)
	copyUTF16Fixed(data.SzInfo[:], notification.Body)
	ret, _, callErr := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&data)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIconW(NIM_MODIFY): %w", callErr)
	}
	return nil
}

func (a *windowsApp) handleTrayMessage(lparam uintptr) bool {
	a.mu.Lock()
	hwnd := a.hwnd
	tray := a.tray
	a.mu.Unlock()
	if tray == nil {
		return false
	}
	switch uint32(lparam) {
	case wmLButtonUp:
		if tray.options.OnClick != nil {
			tray.options.OnClick(TrayEventClick)
		}
		return true
	case wmRButtonUp:
		if tray.options.OnClick != nil {
			tray.options.OnClick(TrayEventContext)
		}
		if tray.menu != 0 {
			var pt point
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
			procSetForegroundWindow.Call(hwnd)
			procTrackPopupMenu.Call(tray.menu, tpmRightButton, uintptr(uint32(pt.X)), uintptr(uint32(pt.Y)), 0, hwnd, 0)
		}
		return true
	default:
		return false
	}
}

func (t *windowsTray) close(hwnd uintptr) {
	if t == nil {
		return
	}
	if hwnd != 0 {
		data := trayNotifyData(hwnd, t)
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	}
	if t.menu != 0 {
		destroyMenu(t.menu)
		t.menu = 0
	}
	if t.hicon != 0 {
		procDestroyIcon.Call(t.hicon)
		t.hicon = 0
	}
}

func trayNotifyData(hwnd uintptr, tray *windowsTray) notifyIconDataW {
	data := notifyIconDataW{
		CbSize: uint32(unsafe.Sizeof(notifyIconDataW{})),
		HWnd:   hwnd,
		UID:    tray.uid,
		HIcon:  tray.hicon,
	}
	return data
}

func loadTrayIcon(path string) (uintptr, error) {
	if strings.TrimSpace(path) != "" {
		ptr, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return 0, fmt.Errorf("%w: tray icon path: %v", ErrInvalidOptions, err)
		}
		icon, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(ptr)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize)
		if icon != 0 {
			return icon, nil
		}
	}
	icon, _, callErr := procLoadIconW.Call(0, idiApplication)
	if icon == 0 {
		return 0, fmt.Errorf("LoadIconW: %w", callErr)
	}
	return icon, nil
}

func copyUTF16Fixed(dst []uint16, value string) {
	if len(dst) == 0 {
		return
	}
	wide, err := syscall.UTF16FromString(value)
	if err != nil {
		return
	}
	if len(wide) > len(dst) {
		wide = wide[:len(dst)]
		wide[len(wide)-1] = 0
	}
	copy(dst, wide)
}

func destroyMenu(handle uintptr) {
	if handle != 0 {
		procDestroyMenu.Call(handle)
	}
}

func (a *windowsApp) SetFileDropHandler(handler func([]string)) error {
	a.mu.Lock()
	a.options.OnFileDrop = handler
	hwnd := a.hwnd
	a.mu.Unlock()
	if hwnd != 0 {
		enableFileDrop(hwnd, true)
	}
	return nil
}

func enableFileDrop(hwnd uintptr, enabled bool) {
	if hwnd == 0 {
		return
	}
	accept := uintptr(0)
	if enabled {
		accept = 1
	}
	procDragAcceptFiles.Call(hwnd, accept)
}

func (a *windowsApp) handleDropFiles(hdrop uintptr) {
	paths := parseDropFiles(hdrop)
	procDragFinish.Call(hdrop)
	a.mu.Lock()
	handler := a.options.OnFileDrop
	a.mu.Unlock()
	dispatchFileDrop(handler, paths)
}

func parseDropFiles(hdrop uintptr) []string {
	count, _, _ := procDragQueryFileW.Call(hdrop, dragQueryFileCount, 0, 0)
	if count == 0 {
		return nil
	}
	paths := make([]string, 0, int(count))
	for i := uintptr(0); i < count; i++ {
		length, _, _ := procDragQueryFileW.Call(hdrop, i, 0, 0)
		if length == 0 {
			continue
		}
		buf := make([]uint16, length+1)
		procDragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buf[0])), length+1)
		paths = append(paths, syscall.UTF16ToString(buf))
	}
	return paths
}

func (a *windowsApp) disposeNativeUI() {
	a.mu.Lock()
	hwnd := a.hwnd
	tray := a.tray
	a.tray = nil
	menuBar := a.menuBar
	a.menuBar = 0
	contextMenus := a.contextMenus
	a.contextMenus = nil
	a.mu.Unlock()

	if tray != nil {
		tray.close(hwnd)
	}
	if menuBar != 0 {
		destroyMenu(menuBar)
	}
	for _, menu := range contextMenus {
		destroyMenu(menu)
	}
}
