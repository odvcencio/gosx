package route

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
)

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
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", file, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}

	return buildFileLayout(abs, layoutFilePage("", abs), resolveLayoutModule(DefaultFileModuleRegistry(), "", abs), opts), nil
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
}

func renderFileNode(path string, opts fileRenderOptions) (gosx.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return gosx.Node{}, fmt.Errorf("read %s: %w", path, err)
	}

	switch filepath.Ext(path) {
	case ".html":
		rendered, used := replaceHTMLPlaceholders(string(data), opts.ComponentReplacements, opts.HTMLPlaceholders)
		if opts.RequireReplacement && !used {
			return gosx.Node{}, fmt.Errorf("layout %s is missing a slot placeholder", path)
		}
		return gosx.RawHTML(rendered), nil
	case ".gsx":
		return renderGSXFile(path, data, opts)
	default:
		return gosx.Node{}, fmt.Errorf("unsupported page extension: %s", path)
	}
}

func renderGSXFile(path string, data []byte, opts fileRenderOptions) (gosx.Node, error) {
	prog, err := gosx.Compile(data)
	if err != nil {
		return gosx.Node{}, fmt.Errorf("compile %s: %w", path, err)
	}

	component, err := preferredGSXRenderComponent(path, prog)
	if err != nil {
		return gosx.Node{}, err
	}

	htmlOut, replaced, err := renderFileProgramHTML(prog, component, opts)
	if err != nil {
		return gosx.Node{}, fmt.Errorf("render %s: %w", path, err)
	}
	if opts.RequireReplacement && !replaced {
		return gosx.Node{}, fmt.Errorf("layout %s is missing a <Slot /> or <Outlet /> component", path)
	}
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

func defaultRenderedComponent(tag string, attrs map[string]any, childrenHTML string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div data-gosx-component="%s"`, html.EscapeString(tag))
	for name, value := range attrs {
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
