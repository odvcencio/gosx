//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

const (
	miniDumpNormal = 0x00000000

	mbIconError = 0x00000010
	mbYesNo     = 0x00000004
	idYes       = 6
)

var (
	modDbghelp                      = syscall.NewLazyDLL("dbghelp.dll")
	procMiniDumpWriteDump           = modDbghelp.NewProc("MiniDumpWriteDump")
	procGetCurrentProcess           = modKernel.NewProc("GetCurrentProcess")
	procGetCurrentProcessID         = modKernel.NewProc("GetCurrentProcessId")
	procSetUnhandledExceptionFilter = modKernel.NewProc("SetUnhandledExceptionFilter")
	procMessageBoxW                 = modUser32.NewProc("MessageBoxW")

	sehCrashMu       sync.Mutex
	sehCrashOptions  CrashReporterOptions
	sehCrashCallback = syscall.NewCallback(sehUnhandledExceptionFilter)
)

func writePlatformCrashDump(path, reason string, stack []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	process, _, _ := procGetCurrentProcess.Call()
	pid, _, _ := procGetCurrentProcessID.Call()
	ret, _, callErr := procMiniDumpWriteDump.Call(
		process,
		pid,
		f.Fd(),
		miniDumpNormal,
		0,
		0,
		0,
	)
	if ret == 0 {
		_ = os.WriteFile(path+".txt", crashStackReport(reason, stack), 0644)
		return fmt.Errorf("MiniDumpWriteDump: %w", callErr)
	}
	return nil
}

func promptCrashUpload(report CrashReport) bool {
	text, _ := syscall.UTF16PtrFromString("GoSX captured a crash dump.\n\nUpload it now?")
	title, _ := syscall.UTF16PtrFromString("GoSX crash report")
	ret, _, _ := procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		mbIconError|mbYesNo,
	)
	return ret == idYes
}

func installSEHCrashReporter(options CrashReporterOptions) error {
	sehCrashMu.Lock()
	sehCrashOptions = options
	sehCrashMu.Unlock()
	procSetUnhandledExceptionFilter.Call(sehCrashCallback)
	return nil
}

func sehUnhandledExceptionFilter(exceptionInfo uintptr) uintptr {
	sehCrashMu.Lock()
	options := sehCrashOptions
	sehCrashMu.Unlock()
	_, _ = CaptureCrash(fmt.Sprintf("unhandled SEH exception 0x%x", exceptionInfo), options)
	return 0
}
