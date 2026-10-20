package strictcheck

import (
	"fmt"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/imagepipe"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/transpile"
)

// validateImageContract is strictcheck's check-time <Image> contract
// (gosx#201). It walks every file's compiled *ir.Program directly, the same
// legacy-and-strict-alike shape validateStrictRenderEntries already uses,
// so it runs whether or not the package has any strict syntax at all --
// runBuiltinChecks calls it beside validateStrictRenderEntries, before the
// packageHasStrict early return, for exactly that reason: the real
// consumer surface (gridiron-2000) compiles as legacy syntax, so a check
// placed after that return would never run for it.
//
// Four rules, each independently testable, mirroring the render-time
// contract server.Image and route.renderImage already enforce (gosx#199,
// gosx#201) so a check-time failure and a render-time failure never
// disagree about what "valid" means:
//
//  1. alt is required and must not be empty.
//  2. A local (root-relative) static src must name a real, readable image
//     file under the project's public/ directory -- read with
//     imagepipe.Probe, not just os.Stat, so a path that exists but is not
//     a decodable image (or is not an image at all) still fails. SVG is
//     exempt from the decode check: gosx never optimizes SVG (see
//     shouldOptimizeImageSource in server/image.go), so existence alone is
//     the whole contract for one.
//  3. An external (http/https, or protocol-relative "//") or dynamic
//     (non-literal, or unresolvable through a spread) src requires an
//     explicit width and height -- a local src does not: the renderer
//     probes the file and injects its intrinsic (or ladder-derived)
//     dimensions automatically (gosx#201's whole point).
//  4. format, if a static literal, must name a format the render-time
//     optimizer can actually produce -- reusing
//     server.ValidateProducibleImageFormat's exact allowlist and message,
//     so the two checks can never drift apart.
//
// An island-nested <Image> is rejected earlier still, inside ir.Validate
// (which gosx.Compile always runs -- see compile.go), before
// transpile.LoadPackage can even hand this function a *ir.Program to walk;
// see ir/validate.go's unsupportedIslandComponentDiagnostic.
func validateImageContract(files []transpile.PackageFile, root string) error {
	publicDir := publicImageDirFor(root)
	var diags []ir.Diagnostic
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		for _, comp := range file.Program.Components {
			for _, id := range collectImageContractNodeIDs(file.Program, comp.Root) {
				node := &file.Program.Nodes[id]
				if node.Kind != ir.NodeComponent || node.Tag != "Image" {
					continue
				}
				diags = append(diags, imageNodeDiagnostics(node, publicDir, file.Path)...)
			}
		}
	}
	return ir.NewDiagnosticsError("image", diags)
}

// publicImageDirFor resolves root's public/ directory, or "" when root is
// unknown or has no public/ directory. Either way, the local-file-exists
// rule below degrades to a no-op for the whole run rather than reporting a
// false positive it cannot actually prove -- see findProjectRoot's doc
// comment for why a caller-supplied root might legitimately have no
// public/ under it.
func publicImageDirFor(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	dir := filepath.Join(root, "public")
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// collectImageContractNodeIDs walks a component's node tree (Program.Nodes,
// indexed by ir.NodeID) from root, depth-first, mirroring ir.Validate's own
// unexported collectComponentNodeIDs -- strictcheck cannot reach that one
// directly, so this is its own copy of the same, small, well-understood
// shape.
func collectImageContractNodeIDs(prog *ir.Program, root ir.NodeID) []ir.NodeID {
	var ids []ir.NodeID
	var walk func(id ir.NodeID)
	walk = func(id ir.NodeID) {
		if int(id) >= len(prog.Nodes) {
			return
		}
		ids = append(ids, id)
		for _, child := range prog.Nodes[id].Children {
			walk(child)
		}
	}
	walk(root)
	return ids
}

// imageNodeDiagnostics runs every rule against one <Image> node and returns
// every finding -- a node can fail more than one rule at once (for example
// a bad format on a src that is also missing alt), and every failure is
// reported in one check run rather than only the first (gosx#186 B3's same
// "accumulate, do not stop at the first" posture, applied within one node
// instead of across files).
func imageNodeDiagnostics(node *ir.Node, publicDir, filePath string) []ir.Diagnostic {
	span := node.Span
	span.File = filePath

	hasSpread := imageNodeHasSpreadAttr(node.Attrs)
	class, src := classifyImageSrc(node)

	var diags []ir.Diagnostic
	if diag, ok := imageAltDiagnostic(node, span, hasSpread); ok {
		diags = append(diags, diag)
	}
	if diag, ok := imageLocalFileDiagnostic(span, publicDir, class, src); ok {
		diags = append(diags, diag)
	}
	if diag, ok := imageDimensionsDiagnostic(node, span, class, src, hasSpread); ok {
		diags = append(diags, diag)
	}
	if diag, ok := imageFormatDiagnostic(node, span); ok {
		diags = append(diags, diag)
	}
	return diags
}

func findImageAttr(attrs []ir.Attr, name string) (ir.Attr, bool) {
	for _, attr := range attrs {
		if attr.Name == name {
			return attr, true
		}
	}
	return ir.Attr{}, false
}

func imageNodeHasSpreadAttr(attrs []ir.Attr) bool {
	for _, attr := range attrs {
		if attr.Kind == ir.AttrSpread {
			return true
		}
	}
	return false
}

// imageAttrExplicit reports whether attrs names name with a non-empty
// value: present and non-blank for a static literal, present at all for
// any other kind (an expression's or bool attribute's value is not known
// until render time, so presence is all a check-time rule can ask of it).
func imageAttrExplicit(attrs []ir.Attr, name string) bool {
	attr, found := findImageAttr(attrs, name)
	if !found {
		return false
	}
	if attr.Kind == ir.AttrStatic {
		return strings.TrimSpace(attr.Value) != ""
	}
	return true
}

// Rule 1: alt is required and must not be empty. A spread attribute on the
// node (`{...props}`) might supply alt at render time in a way this check
// cannot see, so a missing (never an explicitly empty) alt is not flagged
// when one is present -- see route/filesystem_test.go's
// TestDefaultFileRendererSupportsImageBuiltin for the real, already-tested
// shape (`{...data.image}` alone supplying src/width/height/widths) this
// exception exists to keep passing.
func imageAltDiagnostic(node *ir.Node, span ir.Span, hasSpread bool) (ir.Diagnostic, bool) {
	attr, found := findImageAttr(node.Attrs, "alt")
	switch {
	case found && attr.Kind == ir.AttrStatic && strings.TrimSpace(attr.Value) == "":
		// An explicitly empty alt is unambiguous even alongside a spread.
	case found:
		return ir.Diagnostic{}, false
	case hasSpread:
		return ir.Diagnostic{}, false
	}
	return ir.Diagnostic{
		Span:    span,
		Message: `gosx: Image requires a non-empty "alt" attribute`,
		Hint:    `pass a descriptive alt, for example alt="Team photo"`,
	}, true
}

// imageSrcClass classifies a <Image> src attribute for rules 2 and 3.
type imageSrcClass int

const (
	// imageSrcUnresolved covers a data: URI and any src shape this checker
	// does not recognize as local or external -- neither rule 2 nor rule 3
	// applies to it.
	imageSrcUnresolved imageSrcClass = iota
	imageSrcLocal
	imageSrcExternal
	// imageSrcDynamic covers a non-literal src (an expression, or a spread
	// that might supply one) and a missing src attribute entirely -- this
	// checker cannot resolve either to a real file, so it is treated like
	// an external src for rule 3's dimensions requirement.
	imageSrcDynamic
)

func classifyImageSrc(node *ir.Node) (imageSrcClass, string) {
	attr, found := findImageAttr(node.Attrs, "src")
	if !found {
		return imageSrcDynamic, ""
	}
	if attr.Kind != ir.AttrStatic {
		return imageSrcDynamic, ""
	}
	value := strings.TrimSpace(attr.Value)
	switch {
	case value == "":
		return imageSrcDynamic, ""
	case strings.HasPrefix(value, "data:"):
		return imageSrcUnresolved, value
	case looksExternalImageSrc(value):
		return imageSrcExternal, value
	case looksLocalImageSrc(value):
		return imageSrcLocal, value
	default:
		return imageSrcDynamic, value
	}
}

func looksExternalImageSrc(value string) bool {
	if strings.HasPrefix(value, "//") {
		return true
	}
	parsed, err := neturl.Parse(value)
	return err == nil && (parsed.Scheme != "" || parsed.Host != "")
}

func looksLocalImageSrc(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return false
	}
	parsed, err := neturl.Parse(value)
	return err == nil && parsed.Scheme == "" && parsed.Host == ""
}

// Rule 2: a local static src must name a real, readable image file under
// public/.
func imageLocalFileDiagnostic(span ir.Span, publicDir string, class imageSrcClass, src string) (ir.Diagnostic, bool) {
	if class != imageSrcLocal || publicDir == "" {
		return ir.Diagnostic{}, false
	}
	if imageLocalFileReadable(publicDir, src) {
		return ir.Diagnostic{}, false
	}
	return ir.Diagnostic{
		Span:    span,
		Message: fmt.Sprintf("gosx: Image src %q does not name a readable image file under public/", src),
		Hint:    "local <Image> sources must exist under the project's public/ directory as an SVG, JPEG, PNG, GIF, or WebP file",
	}, true
}

// imageLocalFileReadable joins src onto publicDir the same way
// server/image.go's own resolveImagePath cleans a request path (leading
// slash, no ".." escape), then confirms it names a real file. SVG is
// checked for existence only -- gosx never decodes or optimizes SVG (see
// shouldOptimizeImageSource), so imagepipe.Probe (which cannot decode one
// either) would otherwise reject a perfectly valid SVG source. Every other
// extension is probed for real: existence alone is not enough when the
// render-time renderer (and, at build time, imagepipe itself) must also
// decode the file to resize it or read its intrinsic dimensions.
func imageLocalFileReadable(publicDir, src string) bool {
	parsed, err := neturl.Parse(src)
	if err != nil {
		return false
	}
	clean := path.Clean("/" + parsed.Path)
	if clean == "/" {
		return false
	}
	full := filepath.Join(publicDir, filepath.FromSlash(strings.TrimPrefix(clean, "/")))

	if strings.EqualFold(filepath.Ext(full), ".svg") {
		info, err := os.Stat(full)
		return err == nil && info.Mode().IsRegular()
	}
	_, _, err = imagepipe.Probe(full)
	return err == nil
}

// Rule 3: an external or dynamic src requires an explicit width and
// height. A spread attribute on the node might supply either at render
// time in a way this check cannot see, so neither is required (by name)
// when one is present -- the same exception, and for the same reason, as
// imageAltDiagnostic's.
func imageDimensionsDiagnostic(node *ir.Node, span ir.Span, class imageSrcClass, src string, hasSpread bool) (ir.Diagnostic, bool) {
	if class != imageSrcExternal && class != imageSrcDynamic {
		return ir.Diagnostic{}, false
	}
	if hasSpread {
		return ir.Diagnostic{}, false
	}
	if imageAttrExplicit(node.Attrs, "width") && imageAttrExplicit(node.Attrs, "height") {
		return ir.Diagnostic{}, false
	}
	if class == imageSrcExternal {
		return ir.Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("gosx: Image src %q is external and requires explicit width and height", src),
			Hint:    "local sources infer their intrinsic size automatically; an external source cannot, so set width and height explicitly to avoid layout shift",
		}, true
	}
	return ir.Diagnostic{
		Span:    span,
		Message: "gosx: Image src is dynamic and requires explicit width and height",
		Hint:    "a src computed at render time cannot be probed at check time, so set width and height explicitly to avoid layout shift",
	}, true
}

// Rule 4: format, if a static literal, must be a producible output format.
// A dynamic format ({expr}) is exempt at check time the same way an
// external/dynamic src's rule stays scoped to what a literal proves -- an
// unproducible dynamic format value still fails closed at render time
// (server.Image / route.renderImage both call
// server.ValidateProducibleImageFormat before building any URL).
func imageFormatDiagnostic(node *ir.Node, span ir.Span) (ir.Diagnostic, bool) {
	attr, found := findImageAttr(node.Attrs, "format")
	if !found || attr.Kind != ir.AttrStatic {
		return ir.Diagnostic{}, false
	}
	if err := server.ValidateProducibleImageFormat(attr.Value); err != nil {
		return ir.Diagnostic{Span: span, Message: err.Error()}, true
	}
	return ir.Diagnostic{}, false
}
