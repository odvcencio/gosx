package desktop

// This file carries the native-API option structs so callers on non-
// Windows builds can still reference OpenFileOptions / SaveFileOptions
// / FileFilter when constructing API surfaces. The real dialog
// implementations live in native_windows.go; the stubs in
// app_unsupported.go return ErrUnsupported when invoked.

// FileFilter is one entry in a dialog's type filter. Patterns are
// semicolon-separated and follow the Win32 convention:
//
//	FileFilter{Name: "Images", Pattern: "*.png;*.jpg;*.jpeg"}
//
// Non-Windows backends will translate to their platform equivalent when
// those backends land — e.g. NSOpenPanel allowedContentTypes on macOS.
type FileFilter struct {
	Name    string
	Pattern string
}

// OpenFileOptions configures an Open-file dialog. Every field is optional.
// AllowMultiple enables multi-select on backends that support it.
type OpenFileOptions struct {
	Title           string
	Filters         []FileFilter
	InitialDir      string
	InitialFilename string
	DefaultExt      string
	AllowMultiple   bool
}

// SaveFileOptions configures a Save-file dialog. OverwritePrompt, when
// true, asks the user to confirm before replacing an existing file.
type SaveFileOptions struct {
	Title           string
	Filters         []FileFilter
	InitialDir      string
	InitialFilename string
	DefaultExt      string
	OverwritePrompt bool
}
