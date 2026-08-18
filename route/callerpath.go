package route

import (
	"path/filepath"
	"runtime/debug"
	"strings"

	"m31labs.dev/gosx/server"
)

// resolveCallerFilePath turns a runtime.Caller file value into a path this
// process can actually open. An ordinary build already gives one:
// runtime.Caller returns the absolute path the compiler saw on disk. A
// `-trimpath` build — `gosx build`'s default, and the shape of most
// container images — does not: it replaces that absolute path with the
// calling module's own declared path plus the file's path inside that
// module (for example "gridiron-2000/app/wire/page.gsx" instead of
// "/app/app/wire/page.gsx"), a string with no filesystem meaning on the
// machine that runs the binary (gosx#239).
//
// When file is already absolute, this returns it unchanged, so every
// existing dev-mode and `go test` behavior stays exactly as it was — neither
// passes -trimpath, so runtime.Caller already returns a real path in both.
// Otherwise it strips the running binary's own module path — read from
// runtime/debug.BuildInfo, which -trimpath does not touch — off the front of
// file, and resolves what remains against server.ResolveAppRoot(""): the
// same app-root discovery (the GOSX_APP_ROOT environment variable, the
// running executable's own directory, then the working directory) a
// generated main.go already uses to mount file routes, so this does not add
// a second, independent way for root discovery to go wrong. When neither
// step applies — no build info, or file does not start with the module
// path — file is returned unchanged, the same best-effort result callers got
// before this existed.
func resolveCallerFilePath(file string) string {
	if file == "" || filepath.IsAbs(file) {
		return file
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Path == "" {
		return file
	}
	prefix := info.Main.Path + "/"
	if !strings.HasPrefix(file, prefix) {
		return file
	}
	rel := strings.TrimPrefix(file, prefix)
	root := strings.TrimSpace(server.ResolveAppRoot(""))
	if root == "" {
		return file
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}
