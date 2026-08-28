package route

import (
	"crypto/sha256"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"m31labs.dev/gosx"
	gosxcss "m31labs.dev/gosx/css"
	"m31labs.dev/gosx/ir"
)

// gsxCompileCache caches compiled .gsx IR programs.
//
// WHY two keys: the old cache keyed only on the content hash, so every request
// paid os.ReadFile plus fmt.Sprintf("%x", sha256.Sum256(data)) — a full file
// hash and a 64-byte hex allocation — just to look the program up. The `files`
// map keys on path plus modification time plus size, which one os.Stat
// supplies. The hash map stays as the fallback for callers that already hold
// the bytes and for the rare case where os.Stat fails but the read succeeds.
//
// Hot reload still works: an edit changes the modification time, the key misses
// and the template recompiles. The trade-off matches the Go build cache — an
// edit that keeps both the byte length and the modification time serves the
// stale program. Filesystems with one-second time resolution can hit that; ext4
// and APFS report nanoseconds and cannot.
var gsxCompileCache struct {
	mu    sync.RWMutex
	progs map[string]*ir.Program
	files map[string]gsxFileProgram
}

// gsxFileProgram is one stat-keyed cache entry. It stores the compile error too
// so a broken template does not recompile on every request.
type gsxFileProgram struct {
	modTimeNano int64
	size        int64
	prog        *ir.Program
	err         error
}

func init() {
	gsxCompileCache.progs = make(map[string]*ir.Program)
	gsxCompileCache.files = make(map[string]gsxFileProgram)
}

// loadCachedGSXProgram returns the compiled program for a .gsx file, reading
// and compiling it only when the file changed since the last request.
func loadCachedGSXProgram(path string) (*ir.Program, error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		// Stat failed. Read the bytes and fall back to the content hash.
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		prog, err := compileCachedGSX(data)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", path, err)
		}
		return prog, nil
	}

	modTimeNano := info.ModTime().UnixNano()
	size := info.Size()

	gsxCompileCache.mu.RLock()
	entry, ok := gsxCompileCache.files[path]
	gsxCompileCache.mu.RUnlock()
	if ok && entry.modTimeNano == modTimeNano && entry.size == size {
		return entry.prog, entry.err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	prog, compileErr := compileCachedGSX(data)
	if compileErr != nil {
		compileErr = fmt.Errorf("compile %s: %w", path, compileErr)
	}

	gsxCompileCache.mu.Lock()
	gsxCompileCache.files[path] = gsxFileProgram{
		modTimeNano: modTimeNano,
		size:        size,
		prog:        prog,
		err:         compileErr,
	}
	gsxCompileCache.mu.Unlock()

	return prog, compileErr
}

// htmlSourceCache caches the raw text of an .html layout or page.
//
// WHY: an .html file substitutes placeholders in its raw bytes, so it never
// reaches the compiled-program cache and every request re-read it from disk. The
// key is the path plus the modification time plus the size, which one os.Stat
// supplies, so an edit still reaches the next render.
var htmlSourceCache sync.Map

type htmlSourceEntry struct {
	modTimeNano int64
	size        int64
	source      string
}

func loadCachedHTMLSource(path string) (string, error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		return string(data), nil
	}

	modTimeNano := info.ModTime().UnixNano()
	size := info.Size()
	if cached, ok := htmlSourceCache.Load(path); ok {
		entry, _ := cached.(htmlSourceEntry)
		if entry.modTimeNano == modTimeNano && entry.size == size {
			return entry.source, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	source := string(data)
	htmlSourceCache.Store(path, htmlSourceEntry{
		modTimeNano: modTimeNano,
		size:        size,
		source:      source,
	})
	return source, nil
}

func compileCachedGSX(data []byte) (*ir.Program, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	gsxCompileCache.mu.RLock()
	if prog, ok := gsxCompileCache.progs[hash]; ok {
		gsxCompileCache.mu.RUnlock()
		return prog, nil
	}
	gsxCompileCache.mu.RUnlock()

	prog, err := gosx.Compile(data)
	if err != nil {
		return nil, err
	}
	// Lower every {expr} hole now. After this the render path never calls
	// go/parser for this program.
	prewarmFileProgramExprs(prog)

	gsxCompileCache.mu.Lock()
	gsxCompileCache.progs[hash] = prog
	gsxCompileCache.mu.Unlock()

	return prog, nil
}

var defaultLayoutSlotComponents = []string{"Slot", "Outlet"}
var defaultLayoutHTMLPlaceholders = []string{"{{slot}}", "{{outlet}}", "<!-- gosx:slot -->", "<!--gosx:slot-->"}

// FileLayoutOptions configures how a file-backed layout injects the page body.
type FileLayoutOptions struct {
	SlotComponents   []string
	HTMLPlaceholders []string
}

// FileLayout loads a .gsx or .html layout file and returns a LayoutFunc that
// injects page content into <Slot /> / <Outlet /> markers or HTML placeholders.
func FileLayout(file string) (LayoutFunc, error) {
	return FileLayoutWithOptions(file, FileLayoutOptions{})
}

// FileLayoutWithOptions loads a file-backed layout with custom slot markers.
func FileLayoutWithOptions(file string, opts FileLayoutOptions) (LayoutFunc, error) {
	return FileLayoutWithOptionsAndRegistry(file, DefaultFileModuleRegistry(), opts)
}

// FileLayoutWithRegistry loads a file-backed layout using an explicit file
// module registry instead of the shared default registry.
func FileLayoutWithRegistry(file string, registry *FileModuleRegistry) (LayoutFunc, error) {
	return FileLayoutWithOptionsAndRegistry(file, registry, FileLayoutOptions{})
}

// FileLayoutWithOptionsAndRegistry loads a file-backed layout with custom slot
// markers and an explicit file module registry.
func FileLayoutWithOptionsAndRegistry(file string, registry *FileModuleRegistry, opts FileLayoutOptions) (LayoutFunc, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", file, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if registry == nil {
		registry = DefaultFileModuleRegistry()
	}
	return buildFileLayout(abs, layoutFilePage("", abs), resolveLayoutModule(registry, "", abs), opts), nil
}

func buildFileLayout(file string, page FilePage, module FileModule, opts FileLayoutOptions) LayoutFunc {
	return func(ctx *RouteContext, content gosx.Node) gosx.Node {
		node, err := renderFileLayout(file, ctx, content, page, module, opts)
		if err != nil {
			ctx.SetStatus(http.StatusInternalServerError)
			return defaultFileRouteError(err)
		}
		return node
	}
}

func renderFileLayout(file string, ctx *RouteContext, content gosx.Node, page FilePage, module FileModule, opts FileLayoutOptions) (gosx.Node, error) {
	slotHTML := ""
	if !content.IsZero() {
		slotHTML = gosx.RenderHTML(content)
	}
	return renderFileNode(file, fileRenderOptions{
		ComponentReplacements: slotComponentReplacements(slotHTML, opts),
		HTMLPlaceholders:      htmlSlotPlaceholders(opts),
		EvalEnv:               filePageRenderEnv(ctx, page, module),
		RequireReplacement:    true,
	})
}

func resolveLayoutModule(registry *FileModuleRegistry, root, file string) FileModule {
	if registry == nil {
		return FileModule{}
	}
	page := layoutFilePage(root, file)
	module, _ := resolveFileModule(registry, root, page)
	return module
}

func layoutFilePage(root, file string) FilePage {
	file = filepath.Clean(file)
	source := file
	if root != "" {
		root = filepath.Clean(root)
		if rel, err := filepath.Rel(root, file); err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			source = rel
		}
	}
	source = filepath.ToSlash(source)
	dir := filepath.ToSlash(filepath.Dir(source))
	if dir == "." {
		dir = ""
	}
	return FilePage{
		FilePath: file,
		Source:   source,
		Dir:      dir,
	}
}

type fileRenderOptions struct {
	ComponentReplacements map[string]string
	HTMLPlaceholders      []string
	EvalEnv               fileRenderEnv
	RequireReplacement    bool
	Scene3DStyles         gosxcss.Scene3DStylesheet
	// Profile installs an EXPERIMENTAL render-profile hook (gosx#185). A nil
	// Profile reproduces today's rendering exactly, byte for byte. See
	// RenderProfile. Only RenderProgramComponent sets this field, from
	// ProgramRenderEnv.Profile: renderFileNode, the entry point a
	// file-routed page or layout renders through, has no Profile field of
	// its own to set it from, so a file-routed page or layout cannot
	// install a render profile today (gosx#185 m6).
	Profile *RenderProfile
	// EntryProps supplies the typed props value for a strict component
	// rendered as the render entry (gosx#226). Only RenderProgramComponent
	// sets this field, from ProgramRenderEnv.Props: renderFileNode, the
	// entry point a file-routed page or layout renders through, has no
	// Props field of its own to set it from, so a file-routed page or
	// layout still cannot render with typed root props — a file-routed
	// entry's data comes from ctx.Data and a DataLoader, not a Go-typed
	// caller. See renderFileProgramHTML's strict-entry branch.
	EntryProps any
	// SourceDir is the absolute directory of the .gsx file being rendered.
	// A shared (./ or ../ prefixed) import resolves relative to it — see
	// writeSharedComponent. ir.Program carries no reliable directory of its
	// own at render time (Program.Dir is empty unless the build pipeline
	// sets it, and the compiled-program cache keys on content hash alone,
	// so two files with byte-identical content could share one cached
	// Program and one wrong Dir — see loadCachedGSXProgram), so this field
	// is how renderGSXFile threads the one directory it actually knows.
	// Empty for a program rendered through RenderProgramComponent, which
	// has no file path of its own to set it from: a shared call inside such
	// a program fails clearly at render time rather than resolving against
	// the wrong directory.
	SourceDir string
	// EntryChildren supplies the children node for a strict component
	// rendered as the render entry (gosx#226, gosx#246). Only
	// RenderProgramComponent sets this field, built from its own
	// children ...gosx.Node parameter with gosx.Fragment: renderFileNode
	// has no children parameter of its own to build it from, so a
	// file-routed page or layout still cannot accept Go-supplied
	// children — same limitation as EntryProps, for the same reason.
	//
	// The zero Node (EntryChildren.IsZero()) means "no children
	// supplied", not "an empty children node": renderFileProgramHTML
	// only binds the "children" scope value when this is non-zero, so a
	// caller that never sets it — every existing caller before this
	// field existed — reproduces the prior unresolved-identifier
	// behavior byte for byte, not a bound empty Fragment. See
	// renderFileProgramHTML's strict-entry branch.
	EntryChildren gosx.Node
	// EntrySlots supplies named-slot values for a strict component rendered
	// as the render entry (gosx#249). Only RenderProgramComponent sets this
	// field, from ProgramRenderEnv.Slots. Keyed by slot name ("Title", not
	// "slotTitle" — see strictcomponent.SlotBindingName for the reserved
	// identifier a name binds to).
	//
	// A nil or empty map means "no slots supplied", the same "take no new
	// branch" contract EntryChildren's zero-Node sentinel keeps: every
	// caller that predates this field reproduces prior behavior exactly.
	// A key naming a slot comp's body does not declare (ir.Component.
	// AcceptsSlot) fails closed with a descriptive error instead of
	// silently rendering nothing — see renderFileProgramHTML's strict-entry
	// branch, and validateStrictCalleeChildren's arity-rule precedent for
	// why an unrecognized supply is an error rather than a silent no-op. A
	// slot comp's body declares but this map does not supply stays
	// unresolved, rendering empty, exactly the way an unsupplied
	// {children} does today.
	EntrySlots map[string]gosx.Node
}

func renderFileNode(path string, opts fileRenderOptions) (gosx.Node, error) {
	css := sidecarCSSFile(path)
	scopeID := fileCSSScopeID(css.path)
	opts.Scene3DStyles = opts.Scene3DStyles.Merge(fileScene3DStyles(css))

	switch filepath.Ext(path) {
	case ".html":
		// HTML layouts substitute placeholders in the raw bytes. The source
		// text now comes from a stat-keyed cache, so a request re-reads the file
		// only after an edit.
		source, err := loadCachedHTMLSource(path)
		if err != nil {
			return gosx.Node{}, err
		}
		rendered, used := replaceHTMLPlaceholders(source, opts.ComponentReplacements, opts.HTMLPlaceholders)
		if opts.RequireReplacement && !used {
			return gosx.Node{}, fmt.Errorf("layout %s is missing a slot placeholder", path)
		}
		rendered = scopeHTMLFragmentRoots(rendered, scopeID)
		return gosx.RawHTML(rendered), nil
	case ".gsx":
		return renderGSXFile(path, opts, scopeID)
	default:
		return gosx.Node{}, fmt.Errorf("unsupported page extension: %s", path)
	}
}

func renderGSXFile(path string, opts fileRenderOptions, scopeID string) (gosx.Node, error) {
	prog, err := loadCachedGSXProgram(path)
	if err != nil {
		return gosx.Node{}, err
	}

	component, err := preferredGSXRenderComponent(path, prog)
	if err != nil {
		return gosx.Node{}, err
	}

	// opts.SourceDir resolves a shared (./ or ../ prefixed) import; see
	// writeSharedComponent and fileRenderOptions.SourceDir's doc comment.
	// path is absolute here (every caller of renderFileNode already resolves
	// one — FileLayoutWithOptionsAndRegistry, and the file router's own page
	// resolution), so filepath.Dir needs no further Abs call.
	opts.SourceDir = filepath.Dir(path)

	htmlOut, replaced, err := renderFileProgramHTML(prog, component, opts)
	if err != nil {
		return gosx.Node{}, fmt.Errorf("render %s: %w", path, err)
	}
	if opts.RequireReplacement && !replaced {
		return gosx.Node{}, fmt.Errorf("layout %s is missing a <Slot /> or <Outlet /> component", path)
	}
	htmlOut = scopeHTMLFragmentRoots(htmlOut, scopeID)
	return gosx.RawHTML(htmlOut), nil
}

func preferredGSXRenderComponent(path string, prog *ir.Program) (string, error) {
	if len(prog.Components) == 0 {
		return "", fmt.Errorf("no components found in %s", path)
	}

	preferred := []string{"Page"}
	if strings.EqualFold(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "layout") {
		preferred = []string{"Layout", "Page"}
	}

	for _, name := range preferred {
		if hasComponent(prog, name) {
			return name, nil
		}
	}
	return prog.Components[0].Name, nil
}

func slotComponentReplacements(slotHTML string, opts FileLayoutOptions) map[string]string {
	names := opts.SlotComponents
	if len(names) == 0 {
		names = defaultLayoutSlotComponents
	}
	replacements := make(map[string]string, len(names))
	for _, name := range names {
		replacements[name] = slotHTML
	}
	return replacements
}

func htmlSlotPlaceholders(opts FileLayoutOptions) []string {
	if len(opts.HTMLPlaceholders) > 0 {
		return append([]string(nil), opts.HTMLPlaceholders...)
	}
	return append([]string(nil), defaultLayoutHTMLPlaceholders...)
}

func replaceHTMLPlaceholders(input string, replacements map[string]string, placeholders []string) (string, bool) {
	output := input
	replacement := replacements["Slot"]
	if replacement == "" {
		replacement = replacements["Outlet"]
	}

	used := false
	for _, placeholder := range placeholders {
		if strings.Contains(output, placeholder) {
			output = strings.ReplaceAll(output, placeholder, replacement)
			used = true
		}
	}
	return output, used
}

func scopeHTMLFragmentRoots(fragment, scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if fragment == "" || scopeID == "" {
		return fragment
	}
	attrName := "data-gosx-s"
	attrValue := html.EscapeString(scopeID)

	var out strings.Builder
	depth := 0
	for i := 0; i < len(fragment); {
		if fragment[i] != '<' {
			out.WriteByte(fragment[i])
			i++
			continue
		}
		switch {
		case strings.HasPrefix(fragment[i:], "<!--"):
			end := strings.Index(fragment[i+4:], "-->")
			if end < 0 {
				out.WriteString(fragment[i:])
				return out.String()
			}
			end += i + 7
			out.WriteString(fragment[i:end])
			i = end
			continue
		case strings.HasPrefix(fragment[i:], "</"):
			end := findHTMLTagEnd(fragment, i+2)
			if depth > 0 {
				depth--
			}
			out.WriteString(fragment[i:end])
			i = end
			continue
		case strings.HasPrefix(fragment[i:], "<!") || strings.HasPrefix(fragment[i:], "<?"):
			end := findHTMLTagEnd(fragment, i+1)
			out.WriteString(fragment[i:end])
			i = end
			continue
		}

		end := findHTMLTagEnd(fragment, i+1)
		tagText := fragment[i:end]
		name := htmlTagName(tagText)
		if name == "" {
			out.WriteString(tagText)
			i = end
			continue
		}

		if depth == 0 && !strings.Contains(tagText, attrName+"=") {
			tagText = injectHTMLTagAttr(tagText, attrName, attrValue)
		}
		out.WriteString(tagText)

		if !htmlTagSelfClosing(tagText) && !ir.VoidElements[strings.ToLower(name)] {
			depth++
		}
		i = end
	}
	return out.String()
}

func findHTMLTagEnd(fragment string, start int) int {
	quote := byte(0)
	for i := start; i < len(fragment); i++ {
		ch := fragment[i]
		if quote != 0 {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '>' {
			return i + 1
		}
	}
	return len(fragment)
}

func htmlTagName(tag string) string {
	if len(tag) < 3 || tag[0] != '<' || tag[1] == '/' || tag[1] == '!' || tag[1] == '?' {
		return ""
	}
	start := 1
	for start < len(tag) && (tag[start] == ' ' || tag[start] == '\t' || tag[start] == '\n' || tag[start] == '\r') {
		start++
	}
	end := start
	for end < len(tag) {
		ch := tag[end]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '/' || ch == '>' {
			break
		}
		end++
	}
	if start == end {
		return ""
	}
	return tag[start:end]
}

func htmlTagSelfClosing(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	return strings.HasSuffix(trimmed, "/>")
}

func injectHTMLTagAttr(tag, name, value string) string {
	insertAt := len(tag) - 1
	if insertAt > 0 && tag[insertAt-1] == '/' {
		insertAt--
	}
	var out strings.Builder
	out.Grow(len(tag) + len(name) + len(value) + 4)
	out.WriteString(tag[:insertAt])
	fmt.Fprintf(&out, ` %s="%s"`, name, value)
	out.WriteString(tag[insertAt:])
	return out.String()
}

// defaultRenderedComponent emits the fallback markup for an unresolved
// component reference: a <div data-gosx-component="Tag"> carrying every
// attribute the reference supplied, so client-side hydration can find and
// mount the real component.
//
// attrs iterates in sorted name order (gosx#188): Go's map iteration order
// is randomized per run, so two renders of the same input previously
// differed only in attribute order — byte-identity goldens, HTTP ETags, and
// caches all churn on content that has not actually changed. Sorting names
// before emission makes the output deterministic across runs and processes.
func defaultRenderedComponent(tag string, attrs map[string]any, childrenHTML string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div data-gosx-component="%s"`, html.EscapeString(tag))
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := attrs[name]
		safeName := html.EscapeString(name)
		switch v := value.(type) {
		case bool:
			if v {
				fmt.Fprintf(&b, " %s", safeName)
			}
		case string:
			fmt.Fprintf(&b, ` %s="%s"`, safeName, html.EscapeString(v))
		default:
			fmt.Fprintf(&b, ` %s="%s"`, safeName, html.EscapeString(fmt.Sprint(v)))
		}
	}
	b.WriteByte('>')
	b.WriteString(childrenHTML)
	b.WriteString("</div>")
	return b.String()
}
