package route

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/strictcomponent"
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
	file string
}

// loadSharedDirComponents compiles every .gsx file in dir through the same
// stat-keyed cache (loadCachedGSXProgram) a file-routed page renders
// through, and returns every STRICT component and every island any of them
// declares, keyed by name. An island is included whatever its syntax,
// because a call to it must reach the island renderer and not the inline
// component path; its program is lowered from the target's own ir.Program. Only files whose package clause matches the alphabetically
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
	return loadSharedDirComponentsExcept(dir, "", "", false)
}

// loadSharedDirComponentsExcept is loadSharedDirComponents with options for
// the sibling-file lookup. A file equal to self contributes no components,
// because the caller already resolved its own declarations. A non-empty
// wantPackage fixes the package to that name instead of the alphabetically
// first file's, so a sibling lookup follows the calling file's own package.
// With tolerant set, a file that does not compile is skipped instead of
// failing the whole directory.
func loadSharedDirComponentsExcept(dir, self, wantPackage string, tolerant bool) (map[string]sharedComponentTarget, error) {
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
	targetPackage := wantPackage
	for _, name := range names {
		path := filepath.Join(dir, name)
		prog, err := loadCachedGSXProgram(path)
		if err != nil {
			if tolerant {
				continue
			}
			return nil, fmt.Errorf("compile %s: %w", path, err)
		}
		if targetPackage == "" {
			targetPackage = prog.Package
		} else if prog.Package != targetPackage {
			continue
		}
		if self != "" && filepath.Clean(path) == filepath.Clean(self) {
			continue
		}
		for c := range prog.Components {
			comp := &prog.Components[c]
			if comp.Syntax != ir.ComponentSyntaxStrict && !comp.IsIsland {
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
			targets[comp.Name] = sharedComponentTarget{prog: prog, comp: comp, file: path}
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
	if target.comp.IsIsland {
		r.writeTargetIsland(b, name, target, node, env)
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
	// A named slot gets the same render-time proof the children check above
	// gets, and for the same reason: ir.Lower's validateStrictCalleeSlots
	// proves this at compile time for a same-file call, but it performs no
	// I/O and so never loads a shared target's ir.Component. Without this a
	// slot aimed at a callee that declares no such hole would be dropped in
	// silence.
	for slotName := range node.Slots {
		if !target.comp.AcceptsSlot(slotName) {
			r.err = fmt.Errorf("render shared component %s: %s declares no slot named %q; declare {%s} in its body or remove the slot=%q attribute", node.Tag, name, slotName, strictcomponent.SlotBindingName(slotName), slotName)
			return true
		}
	}
	childrenNode := gosx.RawHTML(r.renderChildren(node.Children, env))
	// Children and slots are both rendered on r — the CALLER's renderer, in
	// the caller's env, against the caller's program — before the callee is
	// entered. A slot child's NodeID indexes the caller's program, so the
	// child renderer built below could not resolve it. See renderCallSlots.
	slotNodes := r.renderCallSlots(node.Slots, env)
	child := newFileProgramRenderer(target.prog, fileRenderOptions{Profile: r.opts.Profile, SourceFile: target.file})
	child.writeLocalComponentWithChildren(b, target.comp, node, env, childrenNode, slotNodes)
	if child.err != nil {
		r.err = child.err
	}
	if child.replaced {
		r.replaced = true
	}
	return true
}

// writeTargetIsland renders a call to an island that another .gsx file
// declares. The call site's attributes evaluate against the caller's env, as
// for a same-file island; the island program is lowered from the target's own
// ir.Program, so the renderer that owns the island's body is the one bound to
// it. Children are not accepted: an island's markup comes from its own body.
func (r *fileProgramRenderer) writeTargetIsland(b *strings.Builder, name string, target sharedComponentTarget, node *ir.Node, env fileRenderEnv) {
	if len(node.Children) > 0 {
		r.err = fmt.Errorf("render island %s: an island renders no children; remove the child content", node.Tag)
		return
	}
	child := newFileProgramRenderer(target.prog, fileRenderOptions{Profile: r.opts.Profile, SourceFile: target.file})
	b.WriteString(child.renderLocalIsland(name, node, env))
	if child.err != nil {
		r.err = child.err
	}
	if child.replaced {
		r.replaced = true
	}
}

// writeSiblingComponent resolves a capitalized tag that the current file does
// not declare against the other .gsx files of the same directory and package,
// the way Go resolves a name declared in another file of one package. Only
// strict components and islands resolve, the same set a shared (./ prefixed)
// import reaches. It reports handled=false when no sibling declares the name,
// so the caller's fallback (an unresolved-tag placeholder) is unchanged.
//
// A sibling that fails to compile is skipped here: the route or build that
// owns that file reports its error, and a page must not start failing because
// an unrelated neighbour is broken.
func (r *fileProgramRenderer) writeSiblingComponent(b *strings.Builder, node *ir.Node, env fileRenderEnv) bool {
	if strings.TrimSpace(r.opts.SourceDir) == "" || strings.Contains(node.Tag, ".") {
		return false
	}
	if !r.siblingsLoaded {
		r.siblingsLoaded = true
		r.siblings = loadSiblingComponents(r.opts.SourceDir, r.opts.SourceFile, r.prog.Package)
	}
	target, ok := r.siblings[node.Tag]
	if !ok {
		return false
	}
	if target.comp.IsIsland {
		r.writeTargetIsland(b, node.Tag, target, node, env)
		return true
	}
	if len(node.Children) > 0 && !target.comp.AcceptsChildren {
		r.err = fmt.Errorf("render component %s: %s renders no children; remove the child content or render {children} in %s's body", node.Tag, node.Tag, node.Tag)
		return true
	}
	for slotName := range node.Slots {
		if !target.comp.AcceptsSlot(slotName) {
			r.err = fmt.Errorf("render component %s: %s declares no slot named %q; declare {%s} in its body or remove the slot=%q attribute", node.Tag, node.Tag, slotName, strictcomponent.SlotBindingName(slotName), slotName)
			return true
		}
	}
	childrenNode := gosx.RawHTML(r.renderChildren(node.Children, env))
	slotNodes := r.renderCallSlots(node.Slots, env)
	child := newFileProgramRenderer(target.prog, fileRenderOptions{Profile: r.opts.Profile, SourceFile: target.file, SourceDir: filepath.Dir(target.file)})
	child.writeLocalComponentWithChildren(b, target.comp, node, env, childrenNode, slotNodes)
	if child.err != nil {
		r.err = child.err
	}
	if child.replaced {
		r.replaced = true
	}
	return true
}

// loadSiblingComponents returns the strict components and islands that the
// other .gsx files in dir declare, keyed by name. Only files in the calling
// file's package count. It skips the calling file and any file that does not
// compile.
func loadSiblingComponents(dir, self, pkg string) map[string]sharedComponentTarget {
	targets, err := loadSharedDirComponentsExcept(dir, self, pkg, true)
	if err != nil {
		return nil
	}
	return targets
}
