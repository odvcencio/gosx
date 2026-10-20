//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"fmt"
	"path/filepath"
	"strings"
)

// runFilePanel is replaceable by native tests so configuration and cancellation
// are checked without showing a dialog or requiring keyboard automation.
var runFilePanel = func(panel uintptr) uintptr { return send0(panel, objcSel("runModal")) }

func fileDialogExtensions(filters []FileFilter, defaultExt string) ([]string, bool, error) {
	var extensions []string
	seen := map[string]bool{}
	allowOther := len(filters) == 0
	for _, filter := range filters {
		for _, pattern := range strings.Split(filter.Pattern, ";") {
			pattern = strings.TrimSpace(pattern)
			if pattern == "*" || pattern == "*.*" {
				allowOther = true
				continue
			}
			ext := strings.TrimPrefix(strings.TrimPrefix(pattern, "*"), ".")
			if ext == "" || strings.ContainsAny(ext, "*?/\\\x00") {
				return nil, false, fmt.Errorf("%w: unsupported file filter %q", ErrInvalidOptions, pattern)
			}
			if !seen[ext] {
				seen[ext] = true
				extensions = append(extensions, ext)
			}
		}
	}
	if ext := strings.TrimPrefix(strings.TrimSpace(defaultExt), "."); ext != "" && !seen[ext] {
		if strings.ContainsAny(ext, "*?/\\\x00") {
			return nil, false, fmt.Errorf("%w: invalid default extension", ErrInvalidOptions)
		}
		extensions = append(extensions, ext)
	}
	return extensions, allowOther, nil
}

func nsArray(objects []uintptr) uintptr {
	array := send0(objcClass("NSMutableArray"), objcSel("array"))
	for _, obj := range objects {
		if obj != 0 {
			send1(array, objcSel("addObject:"), obj)
		}
	}
	return array
}

func configureFilePanel(panel uintptr, title, directory, filename string, extensions []string, allowOther bool) error {
	for _, value := range []string{title, directory, filename} {
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%w: file dialog option contains NUL", ErrInvalidOptions)
		}
	}
	if panel == 0 {
		return fmt.Errorf("%w: native file panel is unavailable", ErrUnsupported)
	}
	if title != "" {
		send1(panel, objcSel("setTitle:"), nsString(title))
	}
	if directory != "" {
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return err
		}
		u := send1(objcClass("NSURL"), objcSel("fileURLWithPath:"), nsString(absolute))
		send1(panel, objcSel("setDirectoryURL:"), u)
	}
	if filename != "" {
		send1(panel, objcSel("setNameFieldStringValue:"), nsString(filename))
	}
	if len(extensions) != 0 {
		objects := make([]uintptr, 0, len(extensions))
		for _, ext := range extensions {
			objects = append(objects, nsString(ext))
		}
		// This API remains available on macOS 12, our oldest supported OS.
		send1(panel, objcSel("setAllowedFileTypes:"), nsArray(objects))
	}
	send1(panel, objcSel("setAllowsOtherFileTypes:"), boolArg(allowOther))
	send1(panel, objcSel("setCanCreateDirectories:"), 1)
	return nil
}

func (a *darwinApp) OpenFileDialog(opts OpenFileOptions) ([]string, error) {
	extensions, allowOther, err := fileDialogExtensions(opts.Filters, opts.DefaultExt)
	if err != nil {
		return nil, err
	}
	var paths []string
	err = a.onUIThread(func() error {
		if pthreadMainNP() == 0 {
			return errNeedsMainThread("OpenFileDialog")
		}
		var panelErr error
		withPool(func() {
			panel := send0(objcClass("NSOpenPanel"), objcSel("openPanel"))
			if panelErr = configureFilePanel(panel, opts.Title, opts.InitialDir, opts.InitialFilename, extensions, allowOther); panelErr != nil {
				return
			}
			send1(panel, objcSel("setCanChooseFiles:"), 1)
			send1(panel, objcSel("setCanChooseDirectories:"), 0)
			send1(panel, objcSel("setAllowsMultipleSelection:"), boolArg(opts.AllowMultiple))
			if runFilePanel(panel) == 1 { // NSModalResponseOK
				paths = documentPaths(send0(panel, objcSel("URLs")), true)
			}
		})
		return panelErr
	})
	return paths, err
}

// NSSavePanel always confirms replacing an existing file, even when the caller
// leaves OverwritePrompt false. There is no safe public API to bypass it.
func (a *darwinApp) SaveFileDialog(opts SaveFileOptions) (string, error) {
	extensions, allowOther, err := fileDialogExtensions(opts.Filters, opts.DefaultExt)
	if err != nil {
		return "", err
	}
	var path string
	err = a.onUIThread(func() error {
		if pthreadMainNP() == 0 {
			return errNeedsMainThread("SaveFileDialog")
		}
		var panelErr error
		withPool(func() {
			panel := send0(objcClass("NSSavePanel"), objcSel("savePanel"))
			if panelErr = configureFilePanel(panel, opts.Title, opts.InitialDir, opts.InitialFilename, extensions, allowOther); panelErr != nil {
				return
			}
			if runFilePanel(panel) == 1 {
				paths := documentPaths(nsArray([]uintptr{send0(panel, objcSel("URL"))}), true)
				if len(paths) != 0 {
					path = paths[0]
				}
			}
		})
		return panelErr
	})
	return path, err
}

// documentPaths handles Finder's NSString paths and NSURL documents, as well as
// panel selections. Remote URLs are never treated as local document paths.
func documentPaths(array uintptr, urls bool) []string {
	var paths []string
	for i, count := uintptr(0), send0(array, objcSel("count")); i < count; i++ {
		value := send1(array, objcSel("objectAtIndex:"), i)
		if urls {
			if !sendBool0(value, objcSel("isFileURL")) {
				continue
			}
			value = send0(value, objcSel("path"))
		}
		if path, ok := cfStringToString(value); ok && filepath.IsAbs(path) {
			paths = append(paths, filepath.Clean(path))
		}
	}
	return paths
}

func (a *darwinApp) openDocuments(array uintptr, urls bool) {
	paths := documentPaths(array, urls)
	a.mu.Lock()
	callback, app, window, closing := a.options.OnOpenFiles, a.nsapp, a.window, a.closing
	a.mu.Unlock()
	accepted := len(paths) != 0 && callback != nil && !closing
	if accepted {
		callback(paths)
		if window != 0 {
			send1(window, objcSel("makeKeyAndOrderFront:"), 0)
			send1(app, objcSel("activateIgnoringOtherApps:"), 1)
		}
	}
	if !urls && app != 0 {
		reply := uintptr(2) // NSApplicationDelegateReplyFailure
		if accepted {
			reply = 0 // NSApplicationDelegateReplySuccess
		}
		send1(app, objcSel("replyToOpenOrPrint:"), reply)
	}
}
