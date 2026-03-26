package route

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/server"
)

// FileLoadFunc loads request-scoped data for a file-routed page.
type FileLoadFunc func(ctx *RouteContext, page FilePage) (any, error)

// FileMetadataFunc derives metadata for a file-routed page after Load runs.
type FileMetadataFunc func(ctx *RouteContext, page FilePage, data any) (server.Metadata, error)

// FileRenderDataFunc overrides the default page-file renderer.
type FileRenderDataFunc func(ctx *RouteContext, page FilePage, data any) (gosx.Node, error)

// FileActions maps action names to handlers for a file-routed page.
type FileActions map[string]action.Handler

// FileModule wires server-side hooks to a file-routed page source file.
type FileModule struct {
	Source   string
	Load     FileLoadFunc
	Metadata FileMetadataFunc
	Render   FileRenderDataFunc
	Actions  FileActions
}

// FileModuleOptions configures a file-routed server module.
type FileModuleOptions struct {
	Load     FileLoadFunc
	Metadata FileMetadataFunc
	Render   FileRenderDataFunc
	Actions  FileActions
}

// FileModuleFor builds a file-routed server module definition.
func FileModuleFor(source string, opts FileModuleOptions) FileModule {
	return FileModule{
		Source:   source,
		Load:     opts.Load,
		Metadata: opts.Metadata,
		Render:   opts.Render,
		Actions:  cloneFileActions(opts.Actions),
	}
}

// FileModuleHere infers the sibling page source path from the calling
// `*.server.go` file so callers do not need to repeat `"page.gsx"` strings.
func FileModuleHere(opts FileModuleOptions) FileModule {
	return FileModuleFor(fileModuleSourceHere(1), opts)
}

// MustRegisterFileModuleHere infers the sibling page source path from the
// calling file and registers the module in the shared registry.
func MustRegisterFileModuleHere(opts FileModuleOptions) {
	MustRegisterFileModule(FileModuleFor(fileModuleSourceHere(1), opts))
}

// FileModuleCaller infers the sibling page source path from a caller higher in
// the stack. Use this when wrapping file-module registration in helper
// functions so the outer `page.server.go` remains the registered source.
func FileModuleCaller(skip int, opts FileModuleOptions) FileModule {
	if skip < 0 {
		skip = 0
	}
	return FileModuleFor(fileModuleSourceHere(skip+1), opts)
}

// MustRegisterFileModuleCaller registers a file module using a caller higher in
// the stack. `skip=0` means the immediate caller, `skip=1` skips one wrapper,
// and so on.
func MustRegisterFileModuleCaller(skip int, opts FileModuleOptions) {
	if skip < 0 {
		skip = 0
	}
	MustRegisterFileModule(FileModuleFor(fileModuleSourceHere(skip+1), opts))
}

// FileModuleRegistry stores file-route server modules keyed by source path.
type FileModuleRegistry struct {
	mu      sync.RWMutex
	modules map[string]FileModule
}

// NewFileModuleRegistry creates an empty file-route module registry.
func NewFileModuleRegistry() *FileModuleRegistry {
	return &FileModuleRegistry{modules: make(map[string]FileModule)}
}

var defaultFileModuleRegistry = NewFileModuleRegistry()

// DefaultFileModuleRegistry returns the shared process-wide module registry.
func DefaultFileModuleRegistry() *FileModuleRegistry {
	return defaultFileModuleRegistry
}

// RegisterFileModule adds a file-route module to the shared registry.
func RegisterFileModule(module FileModule) error {
	return defaultFileModuleRegistry.Register(module)
}

// MustRegisterFileModule adds a file-route module to the shared registry or panics.
func MustRegisterFileModule(module FileModule) {
	if err := RegisterFileModule(module); err != nil {
		panic(err)
	}
}

// Register adds a file-route module to the registry.
func (r *FileModuleRegistry) Register(module FileModule) error {
	if r == nil {
		return fmt.Errorf("file module registry is nil")
	}
	key := normalizeFileModuleSource(module.Source)
	if key == "" {
		return fmt.Errorf("file module source is required")
	}
	module.Source = key
	module.Actions = cloneFileActions(module.Actions)

	r.mu.Lock()
	r.modules[key] = module
	r.mu.Unlock()
	return nil
}

// Lookup finds a registered file-route module by source path.
func (r *FileModuleRegistry) Lookup(source string) (FileModule, bool) {
	if r == nil {
		return FileModule{}, false
	}
	key := normalizeFileModuleSource(source)
	if key == "" {
		return FileModule{}, false
	}

	r.mu.RLock()
	module, ok := r.modules[key]
	r.mu.RUnlock()
	return module, ok
}

func fileModuleCandidates(root string, page FilePage) []string {
	candidates := []string{page.Source, page.FilePath}
	if base := strings.TrimSpace(filepath.Base(root)); base != "" {
		candidates = append(candidates, filepath.ToSlash(filepath.Join(base, page.Source)))
	}
	return candidates
}

func resolveFileModule(registry *FileModuleRegistry, root string, page FilePage) (FileModule, bool) {
	for _, candidate := range fileModuleCandidates(root, page) {
		if module, ok := registry.Lookup(candidate); ok {
			return module, true
		}
	}
	return FileModule{}, false
}

func normalizeFileModuleSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	source = filepath.ToSlash(filepath.Clean(source))
	source = strings.TrimPrefix(source, "./")
	return source
}

func cloneFileActions(src FileActions) FileActions {
	if len(src) == 0 {
		return nil
	}
	dst := make(FileActions, len(src))
	for name, handler := range src {
		dst[name] = handler
	}
	return dst
}

func fileModuleSourceHere(skip int) string {
	_, file, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return ""
	}
	return fileModuleSourceFromFile(file)
}

func fileModuleSourceFromFile(file string) string {
	caller := filepath.Clean(file)
	if strings.HasSuffix(caller, ".server.go") {
		base := strings.TrimSuffix(caller, ".server.go")
		for _, ext := range []string{".gsx", ".html"} {
			candidate := base + ext
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		return base + ".gsx"
	}
	return caller
}
