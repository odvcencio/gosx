package route

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// DirConfigureFunc applies request-scoped subtree configuration before a file
// route loads data or renders.
type DirConfigureFunc func(ctx *RouteContext, page FilePage) error

// DirModule wires inherited middleware and request setup to a file-route
// directory.
type DirModule struct {
	Source     string
	Middleware []Middleware
	Configure  DirConfigureFunc
}

// DirModuleOptions configures a directory-scoped route module.
type DirModuleOptions struct {
	Middleware []Middleware
	Configure  DirConfigureFunc
}

// DirModuleFor builds a directory-scoped route module definition.
func DirModuleFor(source string, opts DirModuleOptions) DirModule {
	return DirModule{
		Source:     source,
		Middleware: append([]Middleware(nil), opts.Middleware...),
		Configure:  opts.Configure,
	}
}

// DirModuleHere infers the route directory from the calling file and returns a
// directory-scoped route module.
func DirModuleHere(opts DirModuleOptions) DirModule {
	return DirModuleFor(dirModuleSourceHere(1), opts)
}

// MustRegisterDirModuleHere infers the route directory from the calling file
// and registers the module in the shared registry.
func MustRegisterDirModuleHere(opts DirModuleOptions) {
	MustRegisterDirModule(DirModuleFor(dirModuleSourceHere(1), opts))
}

// DirModuleCaller infers the route directory from a caller higher in the stack.
func DirModuleCaller(skip int, opts DirModuleOptions) DirModule {
	if skip < 0 {
		skip = 0
	}
	return DirModuleFor(dirModuleSourceHere(skip+1), opts)
}

// MustRegisterDirModuleCaller registers a directory module using a caller
// higher in the stack.
func MustRegisterDirModuleCaller(skip int, opts DirModuleOptions) {
	if skip < 0 {
		skip = 0
	}
	MustRegisterDirModule(DirModuleFor(dirModuleSourceHere(skip+1), opts))
}

// DirModuleRegistry stores directory-scoped route modules keyed by source path.
type DirModuleRegistry struct {
	mu      sync.RWMutex
	modules map[string]DirModule
}

// NewDirModuleRegistry creates an empty directory-module registry.
func NewDirModuleRegistry() *DirModuleRegistry {
	return &DirModuleRegistry{modules: make(map[string]DirModule)}
}

var defaultDirModuleRegistry = NewDirModuleRegistry()

// DefaultDirModuleRegistry returns the shared process-wide directory-module registry.
func DefaultDirModuleRegistry() *DirModuleRegistry {
	return defaultDirModuleRegistry
}

// RegisterDirModule adds a directory module to the shared registry.
func RegisterDirModule(module DirModule) error {
	return defaultDirModuleRegistry.Register(module)
}

// MustRegisterDirModule adds a directory module to the shared registry or panics.
func MustRegisterDirModule(module DirModule) {
	if err := RegisterDirModule(module); err != nil {
		panic(err)
	}
}

// Register adds a directory module to the registry.
func (r *DirModuleRegistry) Register(module DirModule) error {
	if r == nil {
		return fmt.Errorf("dir module registry is nil")
	}
	key := normalizeDirModuleSource(module.Source)
	if key == "" {
		return fmt.Errorf("dir module source is required")
	}
	module.Source = key
	module.Middleware = append([]Middleware(nil), module.Middleware...)

	r.mu.Lock()
	r.modules[key] = module
	r.mu.Unlock()
	return nil
}

// Lookup finds a registered directory module by source path.
func (r *DirModuleRegistry) Lookup(source string) (DirModule, bool) {
	if r == nil {
		return DirModule{}, false
	}
	keys := dirModuleLookupKeys(source)
	if len(keys) == 0 {
		return DirModule{}, false
	}
	keySet := moduleLookupKeySet(keys)

	r.mu.RLock()
	for _, key := range keys {
		if module, ok := r.modules[key]; ok {
			r.mu.RUnlock()
			module.Middleware = append([]Middleware(nil), module.Middleware...)
			return module, true
		}
	}

	var match DirModule
	matched := false
	for _, module := range r.modules {
		if !moduleLookupKeysOverlap(dirModuleLookupKeys(module.Source), keySet) {
			continue
		}
		if matched && match.Source != module.Source {
			r.mu.RUnlock()
			return DirModule{}, false
		}
		match = module
		matched = true
	}
	r.mu.RUnlock()
	if !matched {
		return DirModule{}, false
	}
	match.Middleware = append([]Middleware(nil), match.Middleware...)
	return match, true
}

func resolveDirModules(registry *DirModuleRegistry, root string, dir string) []DirModule {
	if registry == nil {
		return nil
	}
	modules := []DirModule{}
	for _, current := range parentFileRouteDirs(dir) {
		module, ok := lookupDirModule(registry, root, current)
		if !ok {
			continue
		}
		modules = append(modules, module)
	}
	return modules
}

func applyDirModules(ctx *RouteContext, page FilePage, modules []DirModule) error {
	for _, module := range modules {
		if module.Configure == nil {
			continue
		}
		if err := module.Configure(ctx, page); err != nil {
			return err
		}
	}
	return nil
}

func collectDirMiddleware(modules []DirModule) []Middleware {
	if len(modules) == 0 {
		return nil
	}
	var middleware []Middleware
	for _, module := range modules {
		middleware = append(middleware, module.Middleware...)
	}
	return middleware
}

func lookupDirModule(registry *DirModuleRegistry, root string, dir string) (DirModule, bool) {
	for _, candidate := range dirModuleCandidates(root, dir) {
		if module, ok := registry.Lookup(candidate); ok {
			return module, true
		}
	}
	return DirModule{}, false
}

func dirModuleCandidates(root string, dir string) []string {
	root = strings.TrimSpace(root)
	dir = normalizeFileRouteDir(dir)

	candidates := []string{}
	if dir != "" {
		candidates = append(candidates, normalizeDirModuleSource(dir))
	}
	if root != "" {
		absRoot, err := filepath.Abs(root)
		if err == nil {
			if dir == "" {
				candidates = append(candidates, normalizeDirModuleSource(absRoot))
			} else {
				candidates = append(candidates, normalizeDirModuleSource(filepath.Join(absRoot, filepath.FromSlash(dir))))
			}
		}
		if base := strings.TrimSpace(filepath.Base(root)); base != "" {
			if dir == "" {
				candidates = append(candidates, normalizeDirModuleSource(base))
			} else {
				candidates = append(candidates, normalizeDirModuleSource(filepath.Join(base, filepath.FromSlash(dir))))
			}
		}
	}
	return appendUniqueStrings(nil, candidates...)
}

func normalizeDirModuleSource(source string) string {
	return normalizeRouteModuleSource(source)
}

func dirModuleSourceHere(skip int) string {
	_, file, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return ""
	}
	return filepath.Dir(file)
}
