//go:build windows && (amd64 || arm64)

package installerhost

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	messageBoxOK            = 0x00000000
	messageBoxYesNo         = 0x00000004
	messageBoxRetryCancel   = 0x00000005
	messageBoxIconError     = 0x00000010
	messageBoxDefaultNo     = 0x00000100
	messageBoxDefaultYes    = 0x00000000
	messageBoxDefaultRetry  = 0x00000000
	messageBoxSetForeground = 0x00010000
	messageBoxTopmost       = 0x00040000

	messageResultYes    = 6
	messageResultRetry  = 4
	messageResultCancel = 2

	regQueryValue         = 0x0001
	regSetValue           = 0x0002
	regCreateLink         = 0x0004
	regRead               = 0x00020019
	regWrite              = 0x00020006
	regString             = 1
	regDWORD              = 4
	regOptionsNonVolatile = 0

	hkeyCurrentUser  = 0x80000001
	hkeyLocalMachine = 0x80000002

	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	progressWindowStyle = 0x00CC0000
	windowChildVisible  = 0x50000000
	cwUseDefault        = 0x80000000

	processQueryLimitedInformation = 0x1000
	th32csSnapProcess              = 0x00000002
)

var (
	modUser32  = syscall.NewLazyDLL("user32.dll")
	modShell32 = syscall.NewLazyDLL("shell32.dll")
	modOle32   = syscall.NewLazyDLL("ole32.dll")
	modAdvapi  = syscall.NewLazyDLL("advapi32.dll")
	modKernel  = syscall.NewLazyDLL("kernel32.dll")

	procMessageBoxW                = modUser32.NewProc("MessageBoxW")
	procCreateWindowExW            = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow              = modUser32.NewProc("DestroyWindow")
	procShowWindow                 = modUser32.NewProc("ShowWindow")
	procUpdateWindow               = modUser32.NewProc("UpdateWindow")
	procSetWindowTextW             = modUser32.NewProc("SetWindowTextW")
	procPeekMessageW               = modUser32.NewProc("PeekMessageW")
	procTranslateMessage           = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW           = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage            = modUser32.NewProc("PostQuitMessage")
	procCoInitializeEx             = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize             = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance           = modOle32.NewProc("CoCreateInstance")
	procCoTaskMemFree              = modOle32.NewProc("CoTaskMemFree")
	procSHGetKnownFolderPath       = modShell32.NewProc("SHGetKnownFolderPath")
	procRegCreateKeyExW            = modAdvapi.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW              = modAdvapi.NewProc("RegOpenKeyExW")
	procRegSetValueExW             = modAdvapi.NewProc("RegSetValueExW")
	procRegQueryValueExW           = modAdvapi.NewProc("RegQueryValueExW")
	procRegCloseKey                = modAdvapi.NewProc("RegCloseKey")
	procRegDeleteTreeW             = modAdvapi.NewProc("RegDeleteTreeW")
	procCreateToolhelp32Snapshot   = modKernel.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW            = modKernel.NewProc("Process32FirstW")
	procProcess32NextW             = modKernel.NewProc("Process32NextW")
	procOpenProcess                = modKernel.NewProc("OpenProcess")
	procWaitForSingleObject        = modKernel.NewProc("WaitForSingleObject")
	procQueryFullProcessImageNameW = modKernel.NewProc("QueryFullProcessImageNameW")
	procCloseHandle                = modKernel.NewProc("CloseHandle")
	procExpandEnvironmentStringsW  = modKernel.NewProc("ExpandEnvironmentStringsW")
)

type winGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidShellLink  = winGUID{Data1: 0x00021401, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidShellLinkW   = winGUID{Data1: 0x000214F9, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidPersistFile  = winGUID{Data1: 0x0000010B, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	folderIDDesktop = winGUID{Data1: 0xB4BFCC3A, Data2: 0xDB2C, Data3: 0x424C, Data4: [8]byte{0xB0, 0x29, 0x7F, 0xE9, 0x9A, 0x87, 0xC6, 0x41}}
)

type nativeMessage struct {
	HWND    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type processEntry32W struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriorityBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

type installProgress struct {
	hwnd  uintptr
	label uintptr
}

func showMessage(title, message string, flags uintptr) int {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	result, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), flags|messageBoxSetForeground|messageBoxTopmost)
	return int(result)
}

func askYesNo(title, message string, defaultYes bool) bool {
	flags := uintptr(messageBoxYesNo)
	if defaultYes {
		flags |= messageBoxDefaultYes
	} else {
		flags |= messageBoxDefaultNo
	}
	return showMessage(title, message, flags) == messageResultYes
}

func askRetryCancel(title, message string) bool {
	return showMessage(title, message, messageBoxRetryCancel|messageBoxDefaultRetry) == messageResultRetry
}

func newInstallProgress(appName string, silent bool) *installProgress {
	if silent {
		return &installProgress{}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	className, _ := syscall.UTF16PtrFromString("STATIC")
	title, _ := syscall.UTF16PtrFromString("Installing " + appName)
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		progressWindowStyle, uintptr(cwUseDefault), uintptr(cwUseDefault), 420, 130,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		return &installProgress{}
	}
	labelClass, _ := syscall.UTF16PtrFromString("STATIC")
	labelText, _ := syscall.UTF16PtrFromString("Preparing installation...")
	label, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(labelClass)), uintptr(unsafe.Pointer(labelText)),
		windowChildVisible, 20, 24, 370, 32, hwnd, 0, 0, 0,
	)
	procShowWindow.Call(hwnd, 1)
	procUpdateWindow.Call(hwnd)
	return &installProgress{hwnd: hwnd, label: label}
}

func (progress *installProgress) Set(message string) error {
	if progress == nil || progress.hwnd == 0 || progress.label == 0 {
		return nil
	}
	wide, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return err
	}
	procSetWindowTextW.Call(progress.label, uintptr(unsafe.Pointer(wide)))
	procUpdateWindow.Call(progress.hwnd)
	return progress.Pump()
}

func (progress *installProgress) Pump() error {
	if progress == nil || progress.hwnd == 0 {
		return nil
	}
	for {
		var message nativeMessage
		result, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 1)
		if result == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
	return nil
}

func (progress *installProgress) Close() {
	if progress != nil && progress.hwnd != 0 {
		procDestroyWindow.Call(progress.hwnd)
		progress.hwnd = 0
	}
}

func createShellShortcut(linkPath, target, workingDir, icon, description string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if int32(hr) < 0 {
		return fmt.Errorf("CoInitializeEx failed: HRESULT 0x%08x", uint32(hr))
	}
	defer procCoUninitialize.Call()
	var link uintptr
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link)),
	)
	if int32(hr) < 0 || link == 0 {
		return fmt.Errorf("CoCreateInstance(IShellLinkW) failed: HRESULT 0x%08x", uint32(hr))
	}
	defer releaseCOM(link)
	for _, item := range []struct {
		slot  uintptr
		value string
	}{
		{20, target}, {9, workingDir}, {7, description},
	} {
		wide, err := syscall.UTF16PtrFromString(item.value)
		if err != nil {
			return err
		}
		hr = callCOM(link, item.slot, uintptr(unsafe.Pointer(wide)))
		if int32(hr) < 0 {
			return fmt.Errorf("IShellLinkW method %d failed: HRESULT 0x%08x", item.slot, uint32(hr))
		}
	}
	if icon != "" {
		wide, err := syscall.UTF16PtrFromString(icon)
		if err != nil {
			return err
		}
		hr = callCOM(link, 17, uintptr(unsafe.Pointer(wide)), 0)
		if int32(hr) < 0 {
			return fmt.Errorf("IShellLinkW.SetIconLocation failed: HRESULT 0x%08x", uint32(hr))
		}
	}
	var persist uintptr
	hr = callCOM(link, 0, uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&persist)))
	if int32(hr) < 0 || persist == 0 {
		return fmt.Errorf("QueryInterface(IPersistFile) failed: HRESULT 0x%08x", uint32(hr))
	}
	defer releaseCOM(persist)
	widePath, err := syscall.UTF16PtrFromString(linkPath)
	if err != nil {
		return err
	}
	hr = callCOM(persist, 6, uintptr(unsafe.Pointer(widePath)), 1)
	if int32(hr) < 0 {
		return fmt.Errorf("IPersistFile.Save failed: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

func callCOM(object uintptr, slot uintptr, args ...uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(object))
	method := *(*uintptr)(unsafe.Pointer(vtable + slot*unsafe.Sizeof(uintptr(0))))
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, object)
	callArgs = append(callArgs, args...)
	hr, _, _ := syscall.SyscallN(method, callArgs...)
	return hr
}

func releaseCOM(object uintptr) {
	if object != 0 {
		_ = callCOM(object, 2)
	}
}

func shellFolderDesktop() (string, error) {
	var path *uint16
	hr, _, _ := procSHGetKnownFolderPath.Call(
		uintptr(unsafe.Pointer(&folderIDDesktop)), 0, 0, uintptr(unsafe.Pointer(&path)),
	)
	if int32(hr) < 0 || path == nil {
		return "", fmt.Errorf("SHGetKnownFolderPath(Desktop) failed: HRESULT 0x%08x", uint32(hr))
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(path)))
	return syscall.UTF16ToString((*[1 << 15]uint16)(unsafe.Pointer(path))[:]), nil
}

func webView2RuntimeInstalled() bool {
	const clientID = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	for _, hive := range []uintptr{hkeyCurrentUser, hkeyLocalMachine} {
		for _, prefix := range []string{`Software\Microsoft\EdgeUpdate\Clients\`, `Software\WOW6432Node\Microsoft\EdgeUpdate\Clients\`} {
			if hive == hkeyCurrentUser && strings.Contains(prefix, "WOW6432Node") {
				continue
			}
			if registryString(hive, prefix+clientID, "pv") != "" {
				return true
			}
		}
	}
	return false
}

func registryString(hive uintptr, path, name string) string {
	widePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var key uintptr
	rv, _, _ := procRegOpenKeyExW.Call(hive, uintptr(unsafe.Pointer(widePath)), 0, regRead, uintptr(unsafe.Pointer(&key)))
	if rv != 0 {
		return ""
	}
	defer procRegCloseKey.Call(key)
	wideName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var valueType uint32
	var bytes uint32 = 4096
	buffer := make([]uint16, bytes/2)
	rv, _, _ = procRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(wideName)), 0, uintptr(unsafe.Pointer(&valueType)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&bytes)))
	if rv != 0 || valueType != regString {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}

func writeUninstallEntry(record installationRecord) error {
	root := `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + record.UninstallKey
	wideRoot, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return err
	}
	var key uintptr
	var disposition uint32
	rv, _, _ := procRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(wideRoot)), 0, 0,
		regOptionsNonVolatile, regWrite, 0, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&disposition)))
	if rv != 0 {
		return fmt.Errorf("RegCreateKeyExW failed: %w", syscall.Errno(rv))
	}
	defer procRegCloseKey.Call(key)
	icon := record.Config.IconPath(record.InstallRoot) + ",0"
	uninstall := `"` + filepath.Join(record.InstallRoot, "uninstall.exe") + `" --uninstall`
	values := map[string]string{
		"DisplayName": record.Config.Name, "DisplayVersion": record.Config.Version,
		"Publisher": record.Config.Publisher, "InstallLocation": record.InstallRoot,
		"UninstallString": uninstall, "DisplayIcon": icon,
	}
	for name, value := range values {
		if err := setRegistryString(key, name, value); err != nil {
			return err
		}
	}
	size, err := directorySize(record.InstallRoot)
	if err != nil {
		return err
	}
	kb := uint32(size / 1024)
	if err := setRegistryDWORD(key, "EstimatedSize", kb); err != nil {
		return err
	}
	if err := setRegistryDWORD(key, "NoModify", 1); err != nil {
		return err
	}
	return setRegistryDWORD(key, "NoRepair", 1)
}

func setRegistryString(key uintptr, name, value string) error {
	wideName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	wideValue, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	rv, _, _ := procRegSetValueExW.Call(key, uintptr(unsafe.Pointer(wideName)), 0, regString,
		uintptr(unsafe.Pointer(&wideValue[0])), uintptr(len(wideValue)*2))
	if rv != 0 {
		return fmt.Errorf("RegSetValueExW(%s) failed: %w", name, syscall.Errno(rv))
	}
	return nil
}

func setRegistryDWORD(key uintptr, name string, value uint32) error {
	wideName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	rv, _, _ := procRegSetValueExW.Call(key, uintptr(unsafe.Pointer(wideName)), 0, regDWORD,
		uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	if rv != 0 {
		return fmt.Errorf("RegSetValueExW(%s) failed: %w", name, syscall.Errno(rv))
	}
	return nil
}

func deleteUninstallEntry(keyName string) error {
	path := `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + keyName
	widePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	rv, _, _ := procRegDeleteTreeW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(widePath)))
	if rv == 2 {
		return nil
	}
	if rv != 0 {
		return fmt.Errorf("RegDeleteTreeW failed: %w", syscall.Errno(rv))
	}
	return nil
}

func directorySize(root string) (uint64, error) {
	var size uint64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 0 {
			size += uint64(info.Size())
		}
		return nil
	})
	return size, err
}

func runningProcessesForPath(target string) ([]uint32, error) {
	snapshot, _, callErr := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if syscall.Handle(snapshot) == syscall.InvalidHandle {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %w", callErr)
	}
	defer procCloseHandle.Call(snapshot)
	entry := processEntry32W{Size: uint32(unsafe.Sizeof(processEntry32W{}))}
	result, _, callErr := procProcess32FirstW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if result == 0 {
		return nil, fmt.Errorf("Process32FirstW: %w", callErr)
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	target = strings.ToLower(filepath.Clean(target))
	var matches []uint32
	for {
		process, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(entry.ProcessID))
		if process != 0 {
			buffer := make([]uint16, 32768)
			length := uint32(len(buffer))
			ok, _, _ := procQueryFullProcessImageNameW.Call(process, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
			procCloseHandle.Call(process)
			if ok != 0 {
				image := strings.ToLower(filepath.Clean(syscall.UTF16ToString(buffer[:length])))
				if image == target {
					matches = append(matches, entry.ProcessID)
				}
			}
		}
		entry = processEntry32W{Size: uint32(unsafe.Sizeof(processEntry32W{}))}
		result, _, _ = procProcess32NextW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if result == 0 {
			break
		}
	}
	return matches, nil
}

func expandWindowsEnv(value string) (string, error) {
	wide, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return "", err
	}
	needed, _, callErr := procExpandEnvironmentStringsW.Call(uintptr(unsafe.Pointer(wide)), 0, 0)
	if needed == 0 {
		return "", callErr
	}
	buffer := make([]uint16, needed)
	result, _, callErr := procExpandEnvironmentStringsW.Call(uintptr(unsafe.Pointer(wide)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(needed))
	if result == 0 {
		return "", callErr
	}
	return syscall.UTF16ToString(buffer), nil
}
