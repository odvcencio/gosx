// Package island provides the island rendering and manifest generation system.
//
// Islands are component subtrees that are server-rendered and then hydrated
// on the client. This package handles:
// - Marking components as islands
// - Generating hydration manifests
// - Rendering island wrapper HTML with anchor IDs
// - Serializing props for client delivery
package island

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/buildmanifest"
	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/island/program"
)

// Renderer handles island-aware rendering of GoSX component trees.
type Renderer struct {
	manifest      *hydrate.Manifest
	counter       int
	bundleID      string
	programDir    string // directory where island programs are stored
	programFormat string // "json" or "bin"
	programAssets map[string]programAsset
	wasmExecPath  string
	patchPath     string
	bootstrapPath string
}

type programAsset struct {
	path   string
	format string
	hash   string
}

// NewRenderer creates an island renderer.
func NewRenderer(bundleID string) *Renderer {
	return &Renderer{
		manifest:      hydrate.NewManifest(),
		bundleID:      bundleID,
		programFormat: "json", // default to dev mode
		programAssets: make(map[string]programAsset),
		wasmExecPath:  "/gosx/wasm_exec.js",
		patchPath:     "/gosx/patch.js",
		bootstrapPath: "/gosx/bootstrap.js",
	}
}

// SetProgramDir sets the directory where island programs are stored.
func (r *Renderer) SetProgramDir(dir string) {
	r.programDir = dir
}

// SetProgramFormat sets the program format ("json" or "bin").
func (r *Renderer) SetProgramFormat(format string) {
	r.programFormat = format
}

// SetProgramAsset registers an exact program asset for a component.
// Use this when build output is content-hashed and can't be inferred from name.
func (r *Renderer) SetProgramAsset(componentName, path, format, hash string) {
	if format == "" {
		format = r.programFormat
	}
	r.programAssets[componentName] = programAsset{
		path:   path,
		format: format,
		hash:   hash,
	}
}

// SetRuntime registers the shared WASM runtime in the manifest.
func (r *Renderer) SetRuntime(path string, hash string, size int64) {
	r.manifest.Runtime = hydrate.RuntimeRef{
		Path: path,
		Hash: hash,
		Size: size,
	}
}

// SetBundle registers a WASM bundle in the manifest.
func (r *Renderer) SetBundle(id string, path string) {
	r.manifest.Bundles[id] = hydrate.BundleRef{Path: path}
}

// SetClientAssetPaths overrides the default runtime script URLs used in PageHead.
func (r *Renderer) SetClientAssetPaths(wasmExecPath, patchPath, bootstrapPath string) {
	if wasmExecPath != "" {
		r.wasmExecPath = wasmExecPath
	}
	if patchPath != "" {
		r.patchPath = patchPath
	}
	if bootstrapPath != "" {
		r.bootstrapPath = bootstrapPath
	}
}

// ApplyBuildManifest wires hashed runtime and island asset URLs into the renderer.
// assetBaseURL should be the public URL prefix that serves dist/assets.
func (r *Renderer) ApplyBuildManifest(manifest *buildmanifest.Manifest, assetBaseURL string) error {
	if manifest == nil {
		return fmt.Errorf("build manifest is nil")
	}

	runtime := manifest.RuntimeURLs(assetBaseURL)
	if runtime.WASM != "" {
		r.SetRuntime(runtime.WASM, manifest.Runtime.WASM.Hash, manifest.Runtime.WASM.Size)
		r.SetBundle(r.bundleID, runtime.WASM)
	}
	r.SetClientAssetPaths(runtime.WASMExec, runtime.Patch, runtime.Bootstrap)

	for _, asset := range manifest.Islands {
		r.SetProgramAsset(asset.Name, manifest.IslandURL(assetBaseURL, asset), asset.Format, asset.Hash)
	}

	return nil
}

// LoadBuildManifest reads a build manifest from disk and applies its asset URLs.
func (r *Renderer) LoadBuildManifest(path, assetBaseURL string) error {
	manifest, err := buildmanifest.Load(path)
	if err != nil {
		return err
	}
	return r.ApplyBuildManifest(manifest, assetBaseURL)
}

// RenderIsland wraps a component in an island container and registers it in the manifest.
func (r *Renderer) RenderIsland(componentName string, props any, content gosx.Node) gosx.Node {
	id, err := r.manifest.AddIsland(componentName, r.bundleID, props)
	if err != nil {
		return gosx.El("div",
			gosx.Attrs(gosx.Attr("class", "gosx-error")),
			gosx.Text(fmt.Sprintf("island error: %v", err)),
		)
	}

	// Set program ref fields on the new entry
	lastIdx := len(r.manifest.Islands) - 1
	r.applyProgramRef(&r.manifest.Islands[lastIdx], componentName)

	r.counter++

	// Wrap content in an island root div
	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("data-gosx-island", componentName),
		),
		content,
	)
}

// RenderIslandWithEvents wraps a component with event bindings.
func (r *Renderer) RenderIslandWithEvents(componentName string, props any, events []hydrate.EventSlot, content gosx.Node) gosx.Node {
	id, err := r.manifest.AddIsland(componentName, r.bundleID, props)
	if err != nil {
		return gosx.El("div", gosx.Text(fmt.Sprintf("island error: %v", err)))
	}

	// Add events and program ref to the last island entry
	lastIdx := len(r.manifest.Islands) - 1
	r.manifest.Islands[lastIdx].Events = events
	r.applyProgramRef(&r.manifest.Islands[lastIdx], componentName)

	r.counter++

	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("data-gosx-island", componentName),
		),
		content,
	)
}

// Manifest returns the generated hydration manifest.
func (r *Renderer) Manifest() *hydrate.Manifest {
	return r.manifest
}

// ManifestJSON returns the manifest as a JSON string.
func (r *Renderer) ManifestJSON() (string, error) {
	data, err := r.manifest.Marshal()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ManifestScript returns an HTML script tag containing the manifest.
func (r *Renderer) ManifestScript() gosx.Node {
	data, err := r.manifest.Marshal()
	if err != nil {
		return gosx.Text("")
	}
	return gosx.RawHTML(fmt.Sprintf(
		`<script id="gosx-manifest" type="application/json">%s</script>`,
		html.EscapeString(string(data)),
	))
}

// BootstrapScript returns the script tags needed for island hydration.
func (r *Renderer) BootstrapScript() gosx.Node {
	if !r.needsClientBootstrap() {
		return gosx.Text("")
	}

	var b strings.Builder
	if (len(r.manifest.Islands) > 0 || len(r.manifest.Hubs) > 0 || r.hasWASMEngines()) && r.wasmExecPath != "" {
		b.WriteString(fmt.Sprintf(`<script data-gosx-script="wasm-exec" src="%s"></script>`, html.EscapeString(r.wasmExecPath)))
		b.WriteByte('\n')
	}
	if len(r.manifest.Islands) > 0 && r.patchPath != "" {
		b.WriteString(fmt.Sprintf(`<script data-gosx-script="patch" src="%s"></script>`, html.EscapeString(r.patchPath)))
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf(`<script data-gosx-script="bootstrap" src="%s"></script>`, html.EscapeString(r.bootstrapPath)))
	return gosx.RawHTML(b.String())
}

// RenderEngine registers an engine instance in the hydration manifest and,
// for surface engines, emits a mount element the client runtime can attach to.
// The optional JS runtime metadata is escape-hatch only; built-in engines can
// rely on factories registered directly on the shared bootstrap runtime.
func (r *Renderer) RenderEngine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	if cfg.Name == "" {
		return renderEngineError(fmt.Errorf("name required"))
	}
	if cfg.Kind == "" {
		return renderEngineError(fmt.Errorf("kind required"))
	}
	if err := engine.ValidateCapabilities(cfg.Capabilities); err != nil {
		return renderEngineError(err)
	}

	props, err := engineProps(cfg.Props)
	if err != nil {
		return renderEngineError(err)
	}

	mountID := cfg.MountID
	if cfg.Kind == engine.KindSurface && mountID == "" {
		mountID = fmt.Sprintf("gosx-engine-mount-%d", len(r.manifest.Engines))
	}

	id, err := r.manifest.AddEngineWithRuntime(
		cfg.Name,
		string(cfg.Kind),
		cfg.WASMPath,
		mountID,
		cfg.JSPath,
		cfg.JSExport,
		string(cfg.Runtime),
		props,
		engineCapabilities(cfg.Capabilities),
	)
	if err != nil {
		return renderEngineError(err)
	}

	if cfg.Kind == engine.KindWorker {
		return gosx.Text("")
	}

	attrs := []any{
		gosx.Attr("id", mountID),
		gosx.Attr("data-gosx-engine", cfg.Name),
		gosx.Attr("data-gosx-engine-id", id),
		gosx.Attr("data-gosx-engine-kind", string(cfg.Kind)),
	}
	for name, value := range cfg.MountAttrs {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		switch name {
		case "id", "data-gosx-engine", "data-gosx-engine-id", "data-gosx-engine-kind", "data-gosx-engine-capabilities":
			continue
		}
		attrs = append(attrs, gosx.Attr(name, value))
	}
	if len(cfg.Capabilities) > 0 {
		attrs = append(attrs, gosx.Attr("data-gosx-engine-capabilities", strings.Join(engineCapabilities(cfg.Capabilities), " ")))
	}

	args := []any{gosx.Attrs(attrs...)}
	if !fallback.IsZero() {
		args = append(args, fallback)
	}

	return gosx.El("div", args...)
}

// BindHub registers a realtime hub connection that can update shared island
// signals on the client runtime.
func (r *Renderer) BindHub(name, path string, bindings []hydrate.HubBinding) string {
	return r.manifest.AddHub(name, path, bindings)
}

// RenderIslandFromProgram renders an island entirely from its compiled IslandProgram.
// No manual event wiring needed — events are extracted from the program's node tree.
// Server HTML is generated with data-gosx-on-* attributes plus the legacy
// click shorthand data-gosx-handler for compatibility.
//
// This is the fully automatic path: write .gsx → compile → call this → done.
func (r *Renderer) RenderIslandFromProgram(prog *program.Program, props any) gosx.Node {
	// Extract event slots from the program's node tree
	events := extractEventSlots(prog)

	// Generate server-rendered HTML from the program's evaluated initial tree.
	content := renderProgramHTML(prog, props)

	// Register in manifest
	id, err := r.manifest.AddIsland(prog.Name, r.bundleID, props)
	if err != nil {
		return gosx.El("div", gosx.Text(fmt.Sprintf("island error: %v", err)))
	}

	lastIdx := len(r.manifest.Islands) - 1
	r.manifest.Islands[lastIdx].Events = events
	r.applyProgramRef(&r.manifest.Islands[lastIdx], prog.Name)

	r.counter++

	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("data-gosx-island", prog.Name),
		),
		content,
	)
}

// extractEventSlots walks the program's node tree and extracts event bindings.
// Each slot gets a stable path-derived ID and selector relative to the island root.
func extractEventSlots(prog *program.Program) []hydrate.EventSlot {
	if len(prog.Nodes) == 0 {
		return nil
	}

	var slots []hydrate.EventSlot

	var walk func(nodeID program.NodeID, path string)
	walk = func(nodeID program.NodeID, path string) {
		if int(nodeID) >= len(prog.Nodes) {
			return
		}

		node := prog.Nodes[nodeID]
		for _, attr := range node.Attrs {
			if attr.Kind == program.AttrEvent {
				eventType := eventNameToType(attr.Name)
				slots = append(slots, hydrate.EventSlot{
					SlotID:         fmt.Sprintf("%s:%s:%s", path, eventType, attr.Event),
					EventType:      eventType,
					TargetSelector: fmt.Sprintf(`[data-gosx-path="%s"]`, path),
					HandlerName:    attr.Event,
				})
			}
		}

		for idx, child := range node.Children {
			walk(child, childProgramPath(path, idx))
		}
	}

	walk(prog.Root, "0")
	return slots
}

// eventNameToType converts JSX event names to DOM event types.
func eventNameToType(name string) string {
	switch name {
	case "onClick":
		return "click"
	case "onInput":
		return "input"
	case "onChange":
		return "change"
	case "onSubmit":
		return "submit"
	case "onKeyDown":
		return "keydown"
	case "onKeyUp":
		return "keyup"
	case "onFocus":
		return "focus"
	case "onBlur":
		return "blur"
	default:
		// Strip "on" prefix and lowercase
		if len(name) > 2 && name[:2] == "on" {
			return strings.ToLower(name[2:3]) + name[3:]
		}
		return name
	}
}

func renderProgramHTMLWithProps(prog *program.Program, propsJSON json.RawMessage) gosx.Node {
	resolved := vm.ResolveInitialTree(prog, string(propsJSON))
	html := RenderResolvedHTML(prog, resolved)
	return gosx.RawHTML(html)
}

// renderProgramHTML renders an IslandProgram's node tree to server HTML.
// Event attributes are rendered as data-gosx-on-* for delegated dispatch.
func renderProgramHTML(prog *program.Program, props any) gosx.Node {
	if len(prog.Nodes) == 0 {
		return gosx.Text("")
	}
	propsJSON, err := SerializeProps(props)
	if err != nil {
		return gosx.Text("")
	}
	return renderProgramHTMLWithProps(prog, propsJSON)
}

// RenderResolvedHTML renders a program tree from an already-resolved VM tree.
// This is used by the island test harness to assert post-dispatch DOM output.
func RenderResolvedHTML(prog *program.Program, resolved *vm.ResolvedTree) string {
	if prog == nil || len(prog.Nodes) == 0 {
		return ""
	}
	return renderProgramNode(prog, resolved, prog.Root, "0")
}

func renderProgramNode(prog *program.Program, resolved *vm.ResolvedTree, nodeID program.NodeID, path string) string {
	if int(nodeID) >= len(prog.Nodes) {
		return ""
	}
	node := prog.Nodes[nodeID]

	switch node.Kind {
	case program.NodeText:
		return html.EscapeString(node.Text)
	case program.NodeExpr:
		if resolved == nil || int(nodeID) >= len(resolved.Nodes) {
			return ""
		}
		return html.EscapeString(resolved.Nodes[nodeID].Text)
	case program.NodeFragment:
		var b strings.Builder
		for idx, child := range node.Children {
			b.WriteString(renderProgramNode(prog, resolved, child, childProgramPath(path, idx)))
		}
		return b.String()
	case program.NodeElement:
		var b strings.Builder
		safeTag := html.EscapeString(node.Tag)
		b.WriteString("<")
		b.WriteString(safeTag)

		var resolvedAttrs map[string]string
		if resolved != nil && int(nodeID) < len(resolved.Nodes) {
			resolvedAttrs = make(map[string]string, len(resolved.Nodes[nodeID].Attrs))
			for _, attr := range resolved.Nodes[nodeID].Attrs {
				resolvedAttrs[attr.Name] = attr.Value
			}
		}

		// Render attributes
		hasEventBinding := false
		for _, attr := range node.Attrs {
			safeName := html.EscapeString(attr.Name)
			safeEvent := html.EscapeString(attr.Event)
			switch attr.Kind {
			case program.AttrStatic:
				b.WriteString(fmt.Sprintf(` %s="%s"`, safeName, html.EscapeString(attr.Value)))
			case program.AttrBool:
				b.WriteString(" " + safeName)
			case program.AttrEvent:
				eventType := html.EscapeString(eventNameToType(attr.Name))
				b.WriteString(fmt.Sprintf(` data-gosx-on-%s="%s"`, eventType, safeEvent))
				if eventType == "click" {
					b.WriteString(fmt.Sprintf(` data-gosx-handler="%s"`, safeEvent))
				}
				hasEventBinding = true
			case program.AttrExpr:
				if resolvedAttrs == nil {
					continue
				}
				if value, ok := resolvedAttrs[attr.Name]; ok {
					b.WriteString(fmt.Sprintf(` %s="%s"`, safeName, html.EscapeString(value)))
				}
			}
		}
		if hasEventBinding {
			b.WriteString(fmt.Sprintf(` data-gosx-path="%s"`, html.EscapeString(path)))
		}

		b.WriteString(">")

		// Render children
		for idx, child := range node.Children {
			b.WriteString(renderProgramNode(prog, resolved, child, childProgramPath(path, idx)))
		}

		b.WriteString(fmt.Sprintf("</%s>", safeTag))
		return b.String()
	default:
		return ""
	}
}

func (r *Renderer) applyProgramRef(entry *hydrate.IslandEntry, componentName string) {
	if asset, ok := r.programAssets[componentName]; ok {
		entry.ProgramRef = asset.path
		entry.ProgramFormat = asset.format
		entry.ProgramHash = asset.hash
		return
	}

	if r.programDir == "" {
		return
	}

	entry.ProgramFormat = r.programFormat
	entry.ProgramRef = r.programDir + "/" + componentName + programFileExt(r.programFormat)
}

func programFileExt(format string) string {
	if format == "bin" {
		return ".gxi"
	}
	return ".json"
}

func childProgramPath(parent string, idx int) string {
	return fmt.Sprintf("%s/%d", parent, idx)
}

// PreloadHints returns <link rel="preload"> tags for the HTML <head>.
// These tell the browser to start downloading WASM and island programs
// during HTML parsing, BEFORE the scripts execute — eliminating the
// serial dependency of HTML → JS → fetch WASM.
func (r *Renderer) PreloadHints() gosx.Node {
	if !r.needsClientBootstrap() {
		return gosx.Text("")
	}

	var b strings.Builder

	// Preload the WASM runtime — this is the biggest asset and biggest win.
	// "as=fetch" with crossorigin lets the browser start the download immediately.
	if (len(r.manifest.Islands) > 0 || len(r.manifest.Hubs) > 0) && r.manifest.Runtime.Path != "" {
		b.WriteString(fmt.Sprintf(`<link rel="preload" href="%s" as="fetch" type="application/wasm" crossorigin>`, r.manifest.Runtime.Path))
		b.WriteByte('\n')
	}

	// Prefetch island programs — downloaded during WASM compile.
	for _, island := range r.manifest.Islands {
		if island.ProgramRef != "" {
			b.WriteString(fmt.Sprintf(`<link rel="prefetch" href="%s">`, island.ProgramRef))
			b.WriteByte('\n')
		}
	}

	for _, entry := range r.manifest.Engines {
		if entry.ProgramRef != "" {
			if strings.HasSuffix(entry.ProgramRef, ".wasm") {
				b.WriteString(fmt.Sprintf(`<link rel="preload" href="%s" as="fetch" type="application/wasm" crossorigin>`, entry.ProgramRef))
			} else {
				b.WriteString(fmt.Sprintf(`<link rel="prefetch" href="%s">`, entry.ProgramRef))
			}
			b.WriteByte('\n')
		}
		if entry.JSRef != "" {
			b.WriteString(fmt.Sprintf(`<link rel="prefetch" href="%s" as="script">`, entry.JSRef))
			b.WriteByte('\n')
		}
	}

	return gosx.RawHTML(b.String())
}

// PageHead returns all head elements needed for islands on this page.
// Includes preload hints (for <head>) and scripts (for end of <body>).
func (r *Renderer) PageHead() gosx.Node {
	if !r.needsClientBootstrap() {
		return gosx.Text("")
	}
	return gosx.Fragment(
		r.ManifestScript(),
		r.BootstrapScript(),
	)
}

// Checksum computes a content hash for cache invalidation.
func Checksum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

// SerializeProps converts props to JSON, validating serializability.
func SerializeProps(props any) (json.RawMessage, error) {
	data, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("props not serializable: %w", err)
	}
	return data, nil
}

func (r *Renderer) needsClientBootstrap() bool {
	return len(r.manifest.Islands) > 0 || len(r.manifest.Engines) > 0 || len(r.manifest.Hubs) > 0
}

func (r *Renderer) hasWASMEngines() bool {
	for _, entry := range r.manifest.Engines {
		if entry.ProgramRef != "" {
			return true
		}
	}
	return false
}

func engineProps(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var props any
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil, fmt.Errorf("invalid engine props JSON: %w", err)
	}
	return props, nil
}

func engineCapabilities(caps []engine.Capability) []string {
	if len(caps) == 0 {
		return nil
	}

	out := make([]string, len(caps))
	for i := range caps {
		out[i] = string(caps[i])
	}
	return out
}

func renderEngineError(err error) gosx.Node {
	return gosx.El("div",
		gosx.Attrs(gosx.Attr("class", "gosx-error")),
		gosx.Text(fmt.Sprintf("engine error: %v", err)),
	)
}
