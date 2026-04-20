//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// comdlg32 hosts GetOpenFileNameW / GetSaveFileNameW — the legacy but
// reliable file-dialog APIs. IFileOpenDialog (COM) is the modern
// alternative; for the R2.x Windows backend the legacy APIs are a better
// fit since they run on any thread and don't require CoInitialize on the
// calling goroutine.
var (
	modComdlg32        = syscall.NewLazyDLL("comdlg32.dll")
	procGetOpenFileNameW = modComdlg32.NewProc("GetOpenFileNameW")
	procGetSaveFileNameW = modComdlg32.NewProc("GetSaveFileNameW")
	procCommDlgExtendedError = modComdlg32.NewProc("CommDlgExtendedError")

	modShell32           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW    = modShell32.NewProc("ShellExecuteW")

	// Clipboard surface lives in user32 + kernel32.
	procOpenClipboard  = modUser32.NewProc("OpenClipboard")
	procCloseClipboard = modUser32.NewProc("CloseClipboard")
	procEmptyClipboard = modUser32.NewProc("EmptyClipboard")
	procSetClipboardData = modUser32.NewProc("SetClipboardData")
	procGetClipboardData = modUser32.NewProc("GetClipboardData")

	procGlobalAlloc  = modKernel.NewProc("GlobalAlloc")
	procGlobalLock   = modKernel.NewProc("GlobalLock")
	procGlobalUnlock = modKernel.NewProc("GlobalUnlock")
	procGlobalFree   = modKernel.NewProc("GlobalFree")
)

// Win32 flag constants used by file dialogs + clipboard.
const (
	// GetOpenFileNameW Flags.
	ofnPathMustExist    = 0x00000800
	ofnFileMustExist    = 0x00001000
	ofnAllowMultiselect = 0x00000200
	ofnNoChangeDir      = 0x00000008
	ofnExplorer         = 0x00080000
	ofnHideReadonly     = 0x00000004
	ofnOverwritePrompt  = 0x00000002

	// ShellExecute nShowCmd.
	swShellShowNormal = 1

	// GlobalAlloc flags — GMEM_MOVEABLE | GMEM_ZEROINIT.
	gmemMoveable = 0x0002
	gmemZeroinit = 0x0040

	// Clipboard formats.
	cfUnicodeText = 13

	// File-path buffer size — Windows "long path" can reach ~32k UTF-16
	// chars when enabled; 32k is the ceiling for a single path. For
	// multi-select the buffer holds all selected paths concatenated.
	filenameBufSize = 32 * 1024
)

// openFileNameW is the OPENFILENAMEW structure from commdlg.h. Must match
// the size of struct OPENFILENAMEW on the target platform exactly — the
// Windows ABI treats lStructSize as part of the contract.
type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	Flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	FlagsEx           uint32
}

// FileFilter, OpenFileOptions, SaveFileOptions are declared in
// native_types.go so both Windows and unsupported builds compile
// against the same type surface.

// OpenFileDialog runs a modal open-file dialog. Returns the selected
// paths — one entry unless AllowMultiple is set — or nil on cancel.
func (a *windowsApp) OpenFileDialog(opts OpenFileOptions) ([]string, error) {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()

	flags := uint32(ofnPathMustExist | ofnFileMustExist | ofnExplorer |
		ofnHideReadonly | ofnNoChangeDir)
	if opts.AllowMultiple {
		flags |= ofnAllowMultiselect
	}
	buf, of, err := buildOpenFileName(hwnd, opts.Title, opts.Filters,
		opts.InitialDir, opts.InitialFilename, opts.DefaultExt, flags)
	if err != nil {
		return nil, err
	}
	ret, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(of)))
	if ret == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return nil, fmt.Errorf("%w: GetOpenFileNameW: code 0x%x",
				ErrInvalidOptions, code)
		}
		return nil, nil // user cancelled
	}
	return parseOpenFilePaths(buf, opts.AllowMultiple), nil
}

// SaveFileDialog runs a modal save-file dialog.
func (a *windowsApp) SaveFileDialog(opts SaveFileOptions) (string, error) {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()

	flags := uint32(ofnPathMustExist | ofnExplorer | ofnHideReadonly |
		ofnNoChangeDir)
	if opts.OverwritePrompt {
		flags |= ofnOverwritePrompt
	}
	buf, of, err := buildOpenFileName(hwnd, opts.Title, opts.Filters,
		opts.InitialDir, opts.InitialFilename, opts.DefaultExt, flags)
	if err != nil {
		return "", err
	}
	ret, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(of)))
	if ret == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return "", fmt.Errorf("%w: GetSaveFileNameW: code 0x%x",
				ErrInvalidOptions, code)
		}
		return "", nil
	}
	return utf16BufferToString(buf), nil
}

// buildOpenFileName prepares an OPENFILENAMEW struct backed by a caller-
// owned path buffer. The returned buffer must stay alive until the Call
// returns, which is why we return it alongside the struct pointer.
func buildOpenFileName(hwnd uintptr, title string, filters []FileFilter,
	initialDir, initialFilename, defaultExt string, flags uint32) ([]uint16, *openFileNameW, error) {

	buf := make([]uint16, filenameBufSize)
	if initialFilename != "" {
		src, err := syscall.UTF16FromString(initialFilename)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: initial filename: %v",
				ErrInvalidOptions, err)
		}
		copy(buf, src)
	}

	var filterPtr *uint16
	if len(filters) > 0 {
		// Filters are encoded as null-separated pairs: "Name\x00Pattern\x00Name\x00Pattern\x00\x00".
		var b strings.Builder
		for _, f := range filters {
			b.WriteString(f.Name)
			b.WriteByte(0)
			b.WriteString(f.Pattern)
			b.WriteByte(0)
		}
		b.WriteByte(0)
		wide, err := syscall.UTF16FromString(b.String())
		if err != nil {
			return nil, nil, fmt.Errorf("%w: filter: %v", ErrInvalidOptions, err)
		}
		filterPtr = &wide[0]
	}

	titlePtr, _ := optionalUTF16Ptr(title)
	dirPtr, _ := optionalUTF16Ptr(initialDir)
	extPtr, _ := optionalUTF16Ptr(defaultExt)

	of := &openFileNameW{
		lStructSize:     uint32(unsafe.Sizeof(openFileNameW{})),
		hwndOwner:       hwnd,
		lpstrFilter:     filterPtr,
		nFilterIndex:    1,
		lpstrFile:       &buf[0],
		nMaxFile:        uint32(len(buf)),
		lpstrInitialDir: dirPtr,
		lpstrTitle:      titlePtr,
		Flags:           flags,
		lpstrDefExt:     extPtr,
	}
	return buf, of, nil
}

// parseOpenFilePaths decodes the GetOpenFileNameW output buffer. With
// AllowMultiple off, the buffer is a single null-terminated path. With
// AllowMultiple on, the first entry is the directory and each subsequent
// entry is a filename; the list terminates with two consecutive NULs.
func parseOpenFilePaths(buf []uint16, allowMultiple bool) []string {
	if len(buf) == 0 {
		return nil
	}
	segments := splitNullTerminated(buf)
	if len(segments) == 0 {
		return nil
	}
	if !allowMultiple || len(segments) == 1 {
		return []string{segments[0]}
	}
	// First segment is the shared directory; the rest are filenames.
	dir := segments[0]
	out := make([]string, 0, len(segments)-1)
	for _, name := range segments[1:] {
		out = append(out, joinWindowsPath(dir, name))
	}
	return out
}

// splitNullTerminated splits a UTF-16 buffer into strings at NUL boundaries
// until it hits the double-NUL terminator.
func splitNullTerminated(buf []uint16) []string {
	var out []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i == start {
				break
			}
			out = append(out, syscall.UTF16ToString(buf[start:i]))
			start = i + 1
		}
	}
	return out
}

// joinWindowsPath appends name to dir using backslash, matching the
// on-wire contract of GetOpenFileNameW multi-select output.
func joinWindowsPath(dir, name string) string {
	dir = strings.TrimRight(dir, `\/`)
	return dir + `\` + name
}

// utf16BufferToString reads a NUL-terminated UTF-16 string out of a Go-
// owned buffer. Differs from utf16PtrToString in that the input is a slice
// (stack/heap-allocated by the caller) rather than a C-allocated pointer.
func utf16BufferToString(buf []uint16) string {
	for i, v := range buf {
		if v == 0 {
			return syscall.UTF16ToString(buf[:i])
		}
	}
	return syscall.UTF16ToString(buf)
}

// Clipboard ---------------------------------------------------------------

// Clipboard returns the current CF_UNICODETEXT contents, or "" if the
// clipboard holds no text (e.g. only bitmap or file-list data). Returns
// an error if the clipboard is locked by another process.
func (a *windowsApp) Clipboard() (string, error) {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if ret, _, err := procOpenClipboard.Call(hwnd); ret == 0 {
		return "", fmt.Errorf("OpenClipboard: %w", err)
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	locked, _, _ := procGlobalLock.Call(h)
	if locked == 0 {
		return "", fmt.Errorf("%w: GlobalLock on clipboard data returned NULL",
			ErrInvalidOptions)
	}
	defer procGlobalUnlock.Call(h)
	return utf16PtrToString((*uint16)(unsafe.Pointer(locked))), nil
}

// SetClipboard replaces the clipboard with text as CF_UNICODETEXT. The
// Windows API requires a GlobalAlloc'd handle so we allocate a movable
// buffer sized to the UTF-16 encoding plus terminator.
func (a *windowsApp) SetClipboard(text string) error {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()

	if ret, _, err := procOpenClipboard.Call(hwnd); ret == 0 {
		return fmt.Errorf("OpenClipboard: %w", err)
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	wide, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("%w: clipboard text: %v", ErrInvalidOptions, err)
	}
	size := uintptr(len(wide) * 2)
	h, _, callErr := procGlobalAlloc.Call(gmemMoveable|gmemZeroinit, size)
	if h == 0 {
		return fmt.Errorf("GlobalAlloc(%d): %w", size, callErr)
	}
	locked, _, _ := procGlobalLock.Call(h)
	if locked == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("%w: GlobalLock returned NULL", ErrInvalidOptions)
	}
	// Copy UTF-16 bytes into the moveable memory.
	src := unsafe.Slice((*uint16)(unsafe.Pointer(&wide[0])), len(wide))
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(locked)), len(wide))
	copy(dst, src)
	procGlobalUnlock.Call(h)

	if ret, _, _ := procSetClipboardData.Call(cfUnicodeText, h); ret == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("%w: SetClipboardData failed", ErrInvalidOptions)
	}
	// Ownership of h transferred to the clipboard — don't free.
	return nil
}

// OpenURL launches the default handler (typically the system browser) for
// a URL. Uses ShellExecuteW so arbitrary URI schemes work — mailto:, tel:,
// custom protocols registered by other apps.
func (a *windowsApp) OpenURL(url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("%w: url is empty", ErrInvalidOptions)
	}
	verbPtr, _ := syscall.UTF16PtrFromString("open")
	urlPtr, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return fmt.Errorf("%w: url: %v", ErrInvalidOptions, err)
	}
	ret, _, _ := procShellExecuteW.Call(
		0, // hwnd
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(urlPtr)),
		0, // params
		0, // dir
		uintptr(swShellShowNormal),
	)
	// ShellExecute returns > 32 on success. Codes ≤ 32 are documented
	// error values (SE_ERR_*).
	if ret <= 32 {
		return fmt.Errorf("%w: ShellExecuteW returned %d", ErrInvalidOptions, ret)
	}
	return nil
}
