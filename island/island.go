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
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/hydrate"
)

// Renderer handles island-aware rendering of GoSX component trees.
type Renderer struct {
	manifest      *hydrate.Manifest
	counter       int
	bundleID      string
	programDir    string // directory where island programs are stored
	programFormat string // "json" or "bin"
}

// NewRenderer creates an island renderer.
func NewRenderer(bundleID string) *Renderer {
	return &Renderer{
		manifest:      hydrate.NewManifest(),
		bundleID:      bundleID,
		programFormat: "json", // default to dev mode
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
	ext := ".json"
	if r.programFormat == "bin" {
		ext = ".bin"
	}
	r.manifest.Islands[lastIdx].ProgramRef = r.programDir + "/" + componentName + ext
	r.manifest.Islands[lastIdx].ProgramFormat = r.programFormat

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

	// Add events to the last island entry
	lastIdx := len(r.manifest.Islands) - 1
	r.manifest.Islands[lastIdx].Events = events

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
		string(data),
	))
}

// BootstrapScript returns the script tags needed for island hydration.
func (r *Renderer) BootstrapScript() gosx.Node {
	if len(r.manifest.Islands) == 0 {
		return gosx.Text("") // No islands, no client runtime needed
	}

	var b strings.Builder
	b.WriteString(`<script src="/gosx/wasm_exec.js"></script>`)
	b.WriteByte('\n')
	b.WriteString(`<script src="/gosx/patch.js"></script>`)
	b.WriteByte('\n')
	b.WriteString(`<script src="/gosx/bootstrap.js"></script>`)
	return gosx.RawHTML(b.String())
}

// PageHead returns all head elements needed for islands on this page.
func (r *Renderer) PageHead() gosx.Node {
	if len(r.manifest.Islands) == 0 {
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
