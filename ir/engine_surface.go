package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SurfaceProgram is the lowered description of an engine surface component.
// It is consumed by the build pipeline (Chunk C) to generate a per-surface
// WASM module and by the runtime renderer (Chunk B) to emit the SSR placeholder.
type SurfaceProgram struct {
	// Name is the component function name, e.g. "Graph".
	Name string

	// Package is the source package import path, e.g.
	// "m31labs.dev/gosx/examples/mygraph".
	// Populated from Program.PackagePath; may be empty when PackagePath is not set.
	Package string

	// PropsTypeName is the Go type name for the component's props parameter,
	// e.g. "GraphProps". Empty when the component has no typed props or when
	// the type name cannot be determined at the IR layer.
	PropsTypeName string

	// Capabilities lists the browser APIs declared via //gosx:capabilities,
	// e.g. ["canvas", "pointer", "wheel"].
	Capabilities []string

	// MountAttrs holds the static <canvas> attributes that are forwarded to the
	// SSR placeholder element. The recognised set is: width, height, class, id,
	// style, tabindex, aria-label. Unknown static attrs are silently forwarded.
	MountAttrs map[string]string

	// Handlers lists the event handler bindings extracted from the canvas element.
	Handlers []SurfaceHandlerBind

	// SourceFiles contains the content of every non-test .go file in the
	// component's package directory, sorted by path for deterministic hashing.
	// Empty when Program.Dir is not set.
	SourceFiles []SurfaceSourceFile

	// SourceFingerprint is the lowercase hex SHA-256 of the combined source
	// file contents concatenated with the JSON-encoded capability set and
	// handler list. The build cache uses this to detect staleness.
	SourceFingerprint string
}

// SurfaceHandlerBind pairs a canonical DOM event name with the Go function
// that handles it.
type SurfaceHandlerBind struct {
	// EventName is the canonical lowercase event name, e.g. "click", "mount",
	// "wheel", "pointerdown".
	EventName string

	// FunctionName is the top-level Go function name in Package that handles
	// the event, e.g. "onSelect", "mount".
	FunctionName string
}

// SurfaceSourceFile holds the content of a single Go source file within the
// component's package directory.
type SurfaceSourceFile struct {
	// Path is the file path relative to the package directory.
	Path string

	// Content is the raw file bytes.
	Content []byte
}

// LowerEngineSurface produces a *SurfaceProgram for the component at compIdx.
// The caller should verify Component.EngineSurface before calling; the function
// returns an error if the component is not a valid engine surface.
func LowerEngineSurface(prog *Program, compIdx int) (*SurfaceProgram, error) {
	if compIdx < 0 || compIdx >= len(prog.Components) {
		return nil, fmt.Errorf("component index %d out of range (program has %d components)", compIdx, len(prog.Components))
	}
	comp := prog.Components[compIdx]
	if !comp.EngineSurface {
		return nil, fmt.Errorf("component %q is not an engine surface (EngineSurface is false)", comp.Name)
	}

	sp := &SurfaceProgram{
		Name:          comp.Name,
		Package:       prog.PackagePath,
		PropsTypeName: comp.PropsType,
		Capabilities:  comp.EngineCapabilities,
	}

	// Build MountAttrs from the root canvas element's static attributes.
	sp.MountAttrs = buildMountAttrs(prog, comp.Root)

	// Translate SurfaceHandlerRef list into SurfaceHandlerBind list with
	// canonical (lowercase, no "on" prefix) event names.
	sp.Handlers = buildHandlerBinds(comp.SurfaceHandlers)

	// Read source files from the package directory when Dir is available.
	if prog.Dir != "" {
		files, err := readPackageSourceFiles(prog.Dir)
		if err != nil {
			return nil, fmt.Errorf("read package source files for %q: %w", comp.Name, err)
		}
		sp.SourceFiles = files
	}

	// Compute the source fingerprint.
	sp.SourceFingerprint = computeFingerprint(sp.SourceFiles, sp.Capabilities, sp.Handlers)

	return sp, nil
}

// buildMountAttrs collects static attributes from the root canvas node.
// All static attrs are forwarded; dynamic attrs are silently ignored.
func buildMountAttrs(prog *Program, rootID NodeID) map[string]string {
	if int(rootID) >= len(prog.Nodes) {
		return nil
	}
	root := &prog.Nodes[rootID]
	if len(root.Attrs) == 0 {
		return nil
	}

	attrs := make(map[string]string, len(root.Attrs))
	for _, a := range root.Attrs {
		if a.Kind != AttrStatic {
			continue
		}
		// Skip on* handler attributes — those are not canvas DOM attrs.
		if strings.HasPrefix(a.Name, "on") && len(a.Name) > 2 && a.Name[2] >= 'A' && a.Name[2] <= 'Z' {
			continue
		}
		attrs[a.Name] = a.Value
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// canvasEventNameToCanonical maps the JSX camelCase on* attribute name to the
// canonical DOM event name used by SurfaceHandlerBind.EventName.
var canvasEventNameToCanonical = map[string]string{
	"onMount":         "mount",
	"onClick":         "click",
	"onDblClick":      "dblclick",
	"onPointerDown":   "pointerdown",
	"onPointerMove":   "pointermove",
	"onPointerUp":     "pointerup",
	"onPointerCancel": "pointercancel",
	"onWheel":         "wheel",
	"onKeyDown":       "keydown",
	"onKeyUp":         "keyup",
	"onResize":        "resize",
	"onDispose":       "dispose",
}

// buildHandlerBinds translates SurfaceHandlerRef entries into SurfaceHandlerBind
// entries using canonical event names.
func buildHandlerBinds(refs []SurfaceHandlerRef) []SurfaceHandlerBind {
	if len(refs) == 0 {
		return nil
	}
	binds := make([]SurfaceHandlerBind, 0, len(refs))
	for _, ref := range refs {
		canonical, ok := canvasEventNameToCanonical[ref.EventName]
		if !ok {
			// Fallback: strip the "on" prefix and lowercase the rest.
			canonical = lowerOnPrefix(ref.EventName)
		}
		binds = append(binds, SurfaceHandlerBind{
			EventName:    canonical,
			FunctionName: ref.FunctionName,
		})
	}
	return binds
}

// lowerOnPrefix strips the "on" prefix from an event name and lowercases the
// first character of the remainder, e.g. "onClick" → "click".
func lowerOnPrefix(name string) string {
	if !strings.HasPrefix(name, "on") || len(name) <= 2 {
		return strings.ToLower(name)
	}
	rest := name[2:]
	if len(rest) == 0 {
		return ""
	}
	return strings.ToLower(rest[:1]) + rest[1:]
}

// readPackageSourceFiles reads every *.go file in dir that is not a _test.go
// file. Files are returned sorted by relative path.
func readPackageSourceFiles(dir string) ([]SurfaceSourceFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []SurfaceSourceFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		files = append(files, SurfaceSourceFile{
			Path:    name,
			Content: content,
		})
	}

	// Sort by path for deterministic hashing.
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

// computeFingerprint computes the SHA-256 fingerprint over the concatenation of:
//   - all source file contents (in sorted order)
//   - JSON-encoded capability set
//   - JSON-encoded handler list (EventName + FunctionName pairs)
func computeFingerprint(files []SurfaceSourceFile, caps []string, handlers []SurfaceHandlerBind) string {
	h := sha256.New()

	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0}) // null separator
		h.Write(f.Content)
		h.Write([]byte{0})
	}

	capJSON, _ := json.Marshal(caps)
	h.Write(capJSON)
	h.Write([]byte{0})

	type handlerJSON struct {
		EventName    string `json:"event"`
		FunctionName string `json:"fn"`
	}
	hb := make([]handlerJSON, len(handlers))
	for i, bind := range handlers {
		hb[i] = handlerJSON{EventName: bind.EventName, FunctionName: bind.FunctionName}
	}
	handlersJSON, _ := json.Marshal(hb)
	h.Write(handlersJSON)

	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}
