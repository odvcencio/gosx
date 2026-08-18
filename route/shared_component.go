package route

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// sharedComponentTarget is one shared call's resolved target: the
// ir.Program that owns the component's BODY, and the ir.Component itself.
// writeLocalComponentWithChildren's own doc comment states why both travel
// together — a node ID means nothing outside the Nodes slice that owns it,
// so the renderer that executes comp.Root must be the one bound to prog.
type sharedComponentTarget struct {
	prog *ir.Program
	comp *ir.Component
}

// loadSharedDirComponents compiles every .gsx file in dir through the same
// stat-keyed cache (loadCachedGSXProgram) a file-routed page renders
// through, and returns every STRICT component any of them declares, keyed
// by name. Only files whose package clause matches the alphabetically
// first .gsx file's package are included, mirroring
// transpile.LoadPackageDir's "one file selects the package" rule — the
// check seam (strictcheck) and this render seam must resolve the identical
// shared directory to the identical component set, or a call gosx check
// proved could render differently than it checked.
//
// No aggregate cache sits in front of this scan. It runs one os.ReadDir
// plus one loadCachedGSXProgram call per file in dir — the expensive part
// (parse and lower) is already cached there and skipped when a file is
// unchanged — on every render that reaches a shared call. That keeps a
// shared directory's hot-reload behavior identical to a page's own: add,
// edit, or remove a file under app/ui and the very next render sees it,
// with no second, harder-to-invalidate cache layer sitting in front of the
// first one.
func loadSharedDirComponents(dir string) (map[string]sharedComponentTarget, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read shared directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gsx") {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("shared directory %s has no .gsx files", dir)
	}
	sort.Strings(names)

	targets := make(map[string]sharedComponentTarget)
	targetPackage := ""
	for i, name := range names {
		path := filepath.Join(dir, name)
		prog, err := loadCachedGSXProgram(path)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", path, err)
		}
		if i == 0 {
			targetPackage = prog.Package
		} else if prog.Package != targetPackage {
			continue
		}
		for c := range prog.Components {
			comp := &prog.Components[c]
			if comp.Syntax != ir.ComponentSyntaxStrict {
				continue
			}
			if _, exists := targets[comp.Name]; exists {
				// First declaration wins, in sorted file order — a defensive
				// tie-break for a same-named strict component declared twice
				// in one shared directory. gosx check independently rejects a
				// same-package Go redeclaration (validatePackageDeclCollisions'
				// sibling stage) long before a render ever reaches this.
				continue
			}
			targets[comp.Name] = sharedComponentTarget{prog: prog, comp: comp}
		}
	}
	return targets, nil
}

// writeSharedComponent renders a call through a shared (./ or ../ prefixed)
// import: <alias.Component .../>. It resolves the target directory
// relative to r's own source file (opts.SourceDir) — the one piece of I/O
// ir.Lower cannot perform on the LSP's per-keystroke path (see
// isSharedComponentTag's doc comment in package ir); the file renderer runs
// once per request, not once per keystroke, so the cost is acceptable here.
//
// PR #246 gave every strict component a variadic children parameter, and
// set writeLocalComponentWithChildren's contract: render children on the
// PARENT renderer (r, which owns the call node), then hand the finished
// node to a renderer bound to the CALLEE's own ir.Program. This function is
// that contract's shared-call implementation.
//
// It reports handled=false only for a tag that names no shared import at
// all, so writeComponent's existing dispatch chain (builtins, bound
// components, the unresolved-tag fallback) continues past it unchanged.
// Once a tag IS a shared call, every failure from here on sets r.err and
// reports handled=true — a shared call that fails never falls through to
// defaultRenderedComponent's placeholder markup, the same fail-closed
// contract a same-file strict call already has.
func (r *fileProgramRenderer) writeSharedComponent(b *strings.Builder, node *ir.Node, env fileRenderEnv) bool {
	alias, name, ok := ir.SplitMemberTag(node.Tag)
	if !ok {
		return false
	}
	rawPath := ""
	isShared := false
	for _, imp := range r.prog.Imports {
		if imp.Alias == alias {
			isShared = ir.IsSharedImportPath(imp.Path)
			rawPath = imp.Path
			break
		}
	}
	if !isShared {
		return false
	}
	if strings.TrimSpace(r.opts.SourceDir) == "" {
		r.err = fmt.Errorf("render shared component %s: no source directory available to resolve %q; shared components render only through the file router", node.Tag, rawPath)
		return true
	}
	targetDir := filepath.Clean(filepath.Join(r.opts.SourceDir, rawPath))
	targets, err := loadSharedDirComponents(targetDir)
	if err != nil {
		r.err = fmt.Errorf("render shared component %s: %w", node.Tag, err)
		return true
	}
	target, ok := targets[name]
	if !ok {
		r.err = fmt.Errorf("render shared component %s: %s declares no strict component named %s", node.Tag, rawPath, name)
		return true
	}
	// Fail closed instead of truncating silently, the same guarantee
	// validateStrictCalleeChildren proves for a same-file call at compile
	// time. ir.Lower cannot prove this for a shared call (no I/O — see
	// isSharedComponentTag), and the Go compiler cannot catch it either
	// (every strict component projects a variadic children parameter that
	// accepts zero or more arguments unconditionally — see CHANGELOG
	// "every strict component projects a variadic children parameter"), so
	// this render-time check is where the shared call's own AcceptsChildren
	// finally gets proved, the first point that ever loads the target's
	// real ir.Component.
	if len(node.Children) > 0 && !target.comp.AcceptsChildren {
		r.err = fmt.Errorf("render shared component %s: %s renders no children; remove the child content or render {children} in %s's body", node.Tag, node.Tag, name)
		return true
	}
	childrenNode := gosx.RawHTML(r.renderChildren(node.Children, env))
	child := newFileProgramRenderer(target.prog, fileRenderOptions{Profile: r.opts.Profile})
	child.writeLocalComponentWithChildren(b, target.comp, node, env, childrenNode)
	if child.err != nil {
		r.err = child.err
	}
	if child.replaced {
		r.replaced = true
	}
	return true
}
