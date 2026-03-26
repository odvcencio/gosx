package route

import (
	"encoding/json"
	"fmt"
	"html"
	"reflect"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/server"
)

type fileProgramRenderer struct {
	prog       *ir.Program
	components map[string]*ir.Component
	opts       fileRenderOptions
	replaced   bool
}

func renderFileProgramHTML(prog *ir.Program, component string, opts fileRenderOptions) (string, bool, error) {
	renderer := newFileProgramRenderer(prog, opts)
	comp, ok := renderer.components[component]
	if !ok {
		return "", false, fmt.Errorf("component %q not found", component)
	}
	html := renderer.renderNode(comp.Root, opts.EvalEnv)
	return html, renderer.replaced, nil
}

func newFileProgramRenderer(prog *ir.Program, opts fileRenderOptions) *fileProgramRenderer {
	components := make(map[string]*ir.Component, len(prog.Components))
	for i := range prog.Components {
		components[prog.Components[i].Name] = &prog.Components[i]
	}
	return &fileProgramRenderer{
		prog:       prog,
		components: components,
		opts:       opts,
	}
}

func (r *fileProgramRenderer) renderNode(nodeID ir.NodeID, env fileRenderEnv) string {
	node := r.prog.NodeAt(nodeID)
	switch node.Kind {
	case ir.NodeElement:
		return r.renderElement(node, env)
	case ir.NodeComponent:
		return r.renderComponent(node, env)
	case ir.NodeText:
		return html.EscapeString(node.Text)
	case ir.NodeExpr:
		return renderFileEvaluatedExpr(evalFileExpr(node.Text, env))
	case ir.NodeFragment:
		return r.renderChildren(node.Children, env)
	case ir.NodeRawHTML:
		return node.Text
	default:
		return ""
	}
}

func (r *fileProgramRenderer) renderElement(node *ir.Node, env fileRenderEnv) string {
	var b strings.Builder
	tag := html.EscapeString(node.Tag)
	b.WriteByte('<')
	b.WriteString(tag)
	r.renderAttrs(&b, node.Attrs, env)
	if ir.VoidElements[node.Tag] {
		b.WriteString(" />")
		return b.String()
	}
	b.WriteByte('>')
	b.WriteString(r.renderChildren(node.Children, env))
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')
	return b.String()
}

func (r *fileProgramRenderer) renderComponent(node *ir.Node, env fileRenderEnv) string {
	if replacement, ok := r.opts.ComponentReplacements[node.Tag]; ok {
		r.replaced = true
		if replacement != "" {
			return replacement
		}
		return r.renderChildren(node.Children, env)
	}

	if handled, out := r.renderBuiltinComponent(node, env); handled {
		return out
	}

	if comp, ok := r.components[node.Tag]; ok && !comp.IsIsland && !comp.IsEngine {
		return r.renderLocalComponent(comp, node, env)
	}

	if handled, out := r.renderBoundComponent(node, env); handled {
		return out
	}

	return defaultRenderedComponent(node.Tag, r.componentAttrMap(node.Attrs, env), r.renderChildren(node.Children, env))
}

func (r *fileProgramRenderer) renderBuiltinComponent(node *ir.Node, env fileRenderEnv) (bool, string) {
	switch node.Tag {
	case "If", "Show", "When":
		return true, r.renderConditional(node, env)
	case "Each", "For":
		return true, r.renderEach(node, env)
	case "Link":
		return true, r.renderLink(node, env)
	case "Image":
		return true, r.renderImage(node, env)
	case "Stylesheet":
		return true, r.renderStylesheet(node, env)
	case "Surface":
		return true, r.renderSurface(node, env)
	case "Worker":
		return true, r.renderWorker(node, env)
	case "Scene3D":
		return true, r.renderScene3D(node, env)
	default:
		return false, ""
	}
}

func (r *fileProgramRenderer) renderConditional(node *ir.Node, env fileRenderEnv) string {
	condition := attrValue(node.Attrs, env, "when", "if", "test")
	if truthy(condition) {
		return r.renderChildren(node.Children, env)
	}
	fallback := attrValue(node.Attrs, env, "fallback", "else")
	return renderFileEvaluatedExpr(fallback)
}

func (r *fileProgramRenderer) renderEach(node *ir.Node, env fileRenderEnv) string {
	collection := attrValue(node.Attrs, env, "of", "each", "items")
	if collection == nil {
		return ""
	}

	itemName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "as", "item")))
	if itemName == "" {
		itemName = "item"
	}
	indexName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "index")))

	items := fileEachEntries(collection)
	if len(items) == 0 {
		fallback := attrValue(node.Attrs, env, "fallback", "empty")
		return renderFileEvaluatedExpr(fallback)
	}

	var b strings.Builder
	for _, entry := range items {
		scope := env.withValue(itemName, entry.Value)
		if indexName != "" {
			scope = scope.withValue(indexName, entry.Index)
		}
		if entry.Key != nil {
			scope = scope.withValue(itemName+"Key", entry.Key)
		}
		b.WriteString(r.renderChildren(node.Children, scope))
	}
	return b.String()
}

func (r *fileProgramRenderer) renderLink(node *ir.Node, env fileRenderEnv) string {
	var b strings.Builder
	b.WriteString("<a")
	hasNavAttr := attrValue(node.Attrs, env, "data-gosx-link") != nil
	r.renderAttrs(&b, node.Attrs, env)
	if !hasNavAttr {
		b.WriteString(" data-gosx-link")
	}
	b.WriteByte('>')
	b.WriteString(r.renderChildren(node.Children, env))
	b.WriteString("</a>")
	return b.String()
}

func (r *fileProgramRenderer) renderImage(node *ir.Node, env fileRenderEnv) string {
	props := server.ImageProps{
		Src:           stringValue(attrValue(node.Attrs, env, "src")),
		Alt:           stringValue(attrValue(node.Attrs, env, "alt")),
		Width:         int(numericValue(attrValue(node.Attrs, env, "width"))),
		Height:        int(numericValue(attrValue(node.Attrs, env, "height"))),
		Widths:        intSliceValue(attrValue(node.Attrs, env, "widths")),
		Sizes:         stringValue(attrValue(node.Attrs, env, "sizes")),
		Loading:       stringValue(attrValue(node.Attrs, env, "loading")),
		Decoding:      stringValue(attrValue(node.Attrs, env, "decoding")),
		FetchPriority: stringValue(attrValue(node.Attrs, env, "fetchpriority", "fetchPriority")),
		Quality:       int(numericValue(attrValue(node.Attrs, env, "quality"))),
		Format:        stringValue(attrValue(node.Attrs, env, "format")),
	}

	extra := imageExtraAttrs(node.Attrs, env)
	args := make([]any, 0, len(extra))
	if len(extra) > 0 {
		args = append(args, gosx.Attrs(extra...))
	}
	return gosx.RenderHTML(server.Image(props, args...))
}

func (r *fileProgramRenderer) renderStylesheet(node *ir.Node, env fileRenderEnv) string {
	href := stringValue(attrValue(node.Attrs, env, "href", "src"))
	extra := []any{}
	for _, attr := range node.Attrs {
		if attr.Kind == ir.AttrSpread {
			continue
		}
		if attr.Name == "href" || attr.Name == "src" || attr.Name == "rel" {
			continue
		}
		switch attr.Kind {
		case ir.AttrStatic:
			extra = append(extra, gosx.Attr(attr.Name, attr.Value))
		case ir.AttrExpr:
			value := evalFileExpr(attr.Expr, env)
			if value == nil {
				continue
			}
			extra = append(extra, gosx.Attr(attr.Name, fmt.Sprint(value)))
		case ir.AttrBool:
			extra = append(extra, gosx.BoolAttr(attr.Name))
		}
	}
	return gosx.RenderHTML(server.Stylesheet(href, extra...))
}

type fileEngineDefaults struct {
	Name         string
	JSExport     string
	JSPath       string
	WASMPath     string
	Capabilities []engine.Capability
	Runtime      engine.Runtime
	MountAttrs   map[string]any
}

func (r *fileProgramRenderer) renderSurface(node *ir.Node, env fileRenderEnv) string {
	return r.renderEngineComponent(node, env, engine.KindSurface, fileEngineDefaults{})
}

func (r *fileProgramRenderer) renderWorker(node *ir.Node, env fileRenderEnv) string {
	return r.renderEngineComponent(node, env, engine.KindWorker, fileEngineDefaults{})
}

func (r *fileProgramRenderer) renderScene3D(node *ir.Node, env fileRenderEnv) string {
	return r.renderEngineComponent(node, env, engine.KindSurface, fileEngineDefaults{
		Name:     "GoSXScene3D",
		JSExport: "GoSXScene3D",
		Capabilities: []engine.Capability{
			engine.CapCanvas,
			engine.CapWebGL,
			engine.CapAnimation,
		},
		MountAttrs: map[string]any{
			"data-gosx-scene3d": true,
		},
	})
}

func (r *fileProgramRenderer) renderEngineComponent(node *ir.Node, env fileRenderEnv, kind engine.Kind, defaults fileEngineDefaults) string {
	cfg, fallback := r.engineComponentConfig(node, env, kind, defaults)
	if kind == engine.KindSurface && cfg.Name == "GoSXScene3D" {
		cfg.Props = defaultScene3DProps(cfg.Props, cfg.WASMPath)
	}
	return gosx.RenderHTML(env.engine(cfg, fallback))
}

func (r *fileProgramRenderer) engineComponentConfig(node *ir.Node, env fileRenderEnv, kind engine.Kind, defaults fileEngineDefaults) (engine.Config, gosx.Node) {
	props, mountAttrs := engineComponentProps(node.Attrs, env, kind == engine.KindSurface)
	name := firstNonEmptyString(
		stringValue(attrValue(node.Attrs, env, "name", "component")),
		defaults.Name,
	)
	jsExport := firstNonEmptyString(
		stringValue(attrValue(node.Attrs, env, "jsExport", "export", "factory")),
		defaults.JSExport,
		name,
	)
	if name == "" {
		name = jsExport
	}

	mountID := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "mountId", "id")))
	if kind == engine.KindSurface {
		for key, value := range defaults.MountAttrs {
			if _, exists := mountAttrs[key]; exists {
				continue
			}
			mountAttrs[key] = value
		}
	}

	cfg := engine.Config{
		Name:         name,
		Kind:         kind,
		WASMPath:     firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "wasmPath", "wasm", "programRef", "program")), defaults.WASMPath),
		JSPath:       firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "jsPath", "js", "script")), defaults.JSPath),
		JSExport:     jsExport,
		MountID:      mountID,
		MountAttrs:   mountAttrs,
		Props:        marshalEngineProps(props),
		Capabilities: engineCapabilitiesValue(attrValue(node.Attrs, env, "capabilities"), defaults.Capabilities),
		Runtime:      engine.Runtime(firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "runtime")), string(defaults.Runtime))),
	}
	if cfg.Runtime == engine.RuntimeNone && kind == engine.KindSurface && cfg.Name == "GoSXScene3D" && cfg.WASMPath != "" {
		cfg.Runtime = engine.RuntimeShared
	}

	var fallback gosx.Node
	if kind == engine.KindSurface {
		childrenHTML := strings.TrimSpace(r.renderChildren(node.Children, env))
		if childrenHTML != "" {
			fallback = gosx.RawHTML(childrenHTML)
		}
	}
	return cfg, fallback
}

func (r *fileProgramRenderer) renderBoundComponent(node *ir.Node, env fileRenderEnv) (bool, string) {
	component, ok := env.component(node.Tag)
	if !ok {
		return false, ""
	}

	childrenHTML := r.renderChildren(node.Children, env)
	childrenNode := gosx.RawHTML(childrenHTML)
	props := componentProps(node.Attrs, env, childrenNode)
	candidates := [][]any{
		componentCallArgs(node.Attrs, env),
		{props},
	}
	if single, ok := singleComponentPropValue(props); ok {
		candidates = append(candidates, []any{single})
	}
	if !childrenNode.IsZero() {
		explicitArgs := componentCallArgs(node.Attrs, env)
		candidates = append(candidates,
			append(append([]any(nil), explicitArgs...), childrenNode),
			[]any{props, childrenNode},
		)
		if single, ok := singleComponentPropValue(props); ok {
			candidates = append(candidates, []any{single, childrenNode})
		}
	}

	if rendered, ok := renderBoundComponentValue(component, candidates); ok {
		return true, rendered
	}
	return true, defaultRenderedComponent(node.Tag, r.componentAttrMap(node.Attrs, env), childrenHTML)
}

func (r *fileProgramRenderer) renderLocalComponent(comp *ir.Component, node *ir.Node, env fileRenderEnv) string {
	childrenHTML := r.renderChildren(node.Children, env)
	childrenNode := gosx.RawHTML(childrenHTML)
	props := componentProps(node.Attrs, env, childrenNode)
	scope := env.withValue("props", props)
	scope = scope.withValue("children", childrenNode)
	return r.renderNode(comp.Root, scope)
}

func (r *fileProgramRenderer) renderChildren(children []ir.NodeID, env fileRenderEnv) string {
	var b strings.Builder
	for _, child := range children {
		b.WriteString(r.renderNode(child, env))
	}
	return b.String()
}

func (r *fileProgramRenderer) renderAttrs(b *strings.Builder, attrs []ir.Attr, env fileRenderEnv) {
	for _, attr := range attrs {
		renderFileAttr(b, attr, env)
	}
}

func (r *fileProgramRenderer) componentAttrMap(attrs []ir.Attr, env fileRenderEnv) map[string]any {
	values := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		switch attr.Kind {
		case ir.AttrStatic:
			values[attr.Name] = attr.Value
		case ir.AttrExpr:
			values[attr.Name] = evalFileExpr(attr.Expr, env)
		case ir.AttrBool:
			values[attr.Name] = true
		case ir.AttrSpread:
			mergeComponentProps(values, evalFileExpr(attr.Expr, env))
		}
	}
	return values
}

func renderFileAttr(b *strings.Builder, attr ir.Attr, env fileRenderEnv) {
	name := html.EscapeString(attr.Name)
	switch attr.Kind {
	case ir.AttrStatic:
		fmt.Fprintf(b, ` %s="%s"`, name, html.EscapeString(attr.Value))
	case ir.AttrExpr:
		renderFileEvaluatedAttr(b, name, evalFileExpr(attr.Expr, env))
	case ir.AttrBool:
		fmt.Fprintf(b, " %s", name)
	case ir.AttrSpread:
		renderFileSpreadAttrs(b, evalFileExpr(attr.Expr, env))
	}
}

func renderFileSpreadAttrs(b *strings.Builder, value any) {
	for key, value := range spreadProps(value) {
		normalized := normalizeFileAttrName(key)
		if normalized == "" {
			continue
		}
		renderFileEvaluatedAttr(b, html.EscapeString(normalized), value)
	}
}

func renderFileEvaluatedExpr(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case gosx.Node:
		return gosx.RenderHTML(v)
	case *gosx.Node:
		if v == nil {
			return ""
		}
		return gosx.RenderHTML(*v)
	case []gosx.Node:
		var b strings.Builder
		for _, node := range v {
			b.WriteString(gosx.RenderHTML(node))
		}
		return b.String()
	case []string:
		return html.EscapeString(strings.Join(v, ""))
	case fmt.Stringer:
		return html.EscapeString(v.String())
	default:
		return html.EscapeString(fmt.Sprint(v))
	}
}

func renderFileEvaluatedAttr(b *strings.Builder, name string, value any) {
	switch v := value.(type) {
	case nil:
		return
	case bool:
		if v {
			fmt.Fprintf(b, " %s", name)
		}
	case fmt.Stringer:
		fmt.Fprintf(b, ` %s="%s"`, name, html.EscapeString(v.String()))
	default:
		fmt.Fprintf(b, ` %s="%s"`, name, html.EscapeString(fmt.Sprint(v)))
	}
}

func attrValue(attrs []ir.Attr, env fileRenderEnv, names ...string) any {
	for _, name := range names {
		for _, attr := range attrs {
			if attr.Name != name {
				continue
			}
			switch attr.Kind {
			case ir.AttrStatic:
				return attr.Value
			case ir.AttrExpr:
				return evalFileExpr(attr.Expr, env)
			case ir.AttrBool:
				return true
			}
		}
		for _, attr := range attrs {
			if attr.Kind != ir.AttrSpread {
				continue
			}
			if value, ok := spreadValue(evalFileExpr(attr.Expr, env), name); ok {
				return value
			}
		}
	}
	return nil
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func intSliceValue(value any) []int {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]int, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, int(numericValue(rv.Index(i).Interface())))
	}
	return out
}

func componentProps(attrs []ir.Attr, env fileRenderEnv, children gosx.Node) map[string]any {
	props := make(map[string]any, len(attrs)+4)
	for _, attr := range attrs {
		if attr.Kind == ir.AttrSpread {
			mergeComponentProps(props, evalFileExpr(attr.Expr, env))
			continue
		}
		value := attrValue([]ir.Attr{attr}, env, attr.Name)
		setComponentProp(props, attr.Name, value)
	}
	if !children.IsZero() {
		setComponentProp(props, "children", children)
		setComponentProp(props, "Children", children)
	}
	return props
}

func componentCallArgs(attrs []ir.Attr, env fileRenderEnv) []any {
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		switch attr.Kind {
		case ir.AttrStatic:
			args = append(args, attr.Value)
		case ir.AttrExpr:
			args = append(args, evalFileExpr(attr.Expr, env))
		case ir.AttrBool:
			args = append(args, true)
		}
	}
	return args
}

func singleComponentPropValue(props map[string]any) (any, bool) {
	canonical := make(map[string]any)
	for key, value := range props {
		if key == "children" || key == "Children" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		if _, exists := canonical[name]; exists {
			continue
		}
		canonical[name] = value
	}
	if len(canonical) != 1 {
		return nil, false
	}
	for _, value := range canonical {
		return value, true
	}
	return nil, false
}

func renderBoundComponentValue(component any, candidates [][]any) (string, bool) {
	switch component.(type) {
	case gosx.Node, *gosx.Node, []gosx.Node, string, fmt.Stringer:
		return renderFileEvaluatedExpr(component), true
	}

	rv := reflect.ValueOf(component)
	if !rv.IsValid() || rv.Kind() != reflect.Func {
		return "", false
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := fmt.Sprintf("%#v", candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if value, ok := tryCallValue(component, candidate); ok {
			return renderFileEvaluatedExpr(value), true
		}
	}
	return "", false
}

func setComponentProp(props map[string]any, name string, value any) {
	if props == nil || strings.TrimSpace(name) == "" {
		return
	}
	props[name] = value
	if alt := exportedPropAlias(name); alt != "" {
		props[alt] = value
	}
	if alt := unexportedPropAlias(name); alt != "" {
		props[alt] = value
	}
}

func normalizeFileAttrName(name string) string {
	name = strings.TrimSpace(name)
	switch name {
	case "":
		return ""
	case "className":
		return "class"
	default:
		return name
	}
}

func mergeComponentProps(props map[string]any, value any) {
	for key, item := range spreadProps(value) {
		setComponentProp(props, key, item)
	}
}

func exportedPropAlias(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func unexportedPropAlias(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToLower(name[:1]) + name[1:]
}

type fileEachEntry struct {
	Index int
	Key   any
	Value any
}

func fileEachEntries(value any) []fileEachEntry {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}

	switch rv.Kind() {
	case reflect.Array, reflect.Slice:
		out := make([]fileEachEntry, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			item := rv.Index(i)
			if item.IsValid() && item.CanInterface() {
				out = append(out, fileEachEntry{Index: i, Key: i, Value: item.Interface()})
			}
		}
		return out
	case reflect.Map:
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
		})
		out := make([]fileEachEntry, 0, len(keys))
		for i, key := range keys {
			item := rv.MapIndex(key)
			if !item.IsValid() || !item.CanInterface() {
				continue
			}
			entry := fileEachEntry{Index: i, Value: item.Interface()}
			if key.CanInterface() {
				entry.Key = key.Interface()
			}
			out = append(out, entry)
		}
		return out
	default:
		return nil
	}
}

func imageExtraAttrs(attrs []ir.Attr, env fileRenderEnv) []any {
	consumed := map[string]struct{}{
		"src":           {},
		"alt":           {},
		"width":         {},
		"height":        {},
		"widths":        {},
		"sizes":         {},
		"loading":       {},
		"decoding":      {},
		"fetchpriority": {},
		"fetchPriority": {},
		"quality":       {},
		"format":        {},
	}
	out := []any{}
	for _, attr := range attrs {
		if _, ok := consumed[attr.Name]; ok {
			continue
		}
		switch attr.Kind {
		case ir.AttrStatic:
			out = append(out, gosx.Attr(attr.Name, attr.Value))
		case ir.AttrExpr:
			out = append(out, gosx.Attr(attr.Name, evalFileExpr(attr.Expr, env)))
		case ir.AttrBool:
			out = append(out, gosx.BoolAttr(attr.Name))
		case ir.AttrSpread:
			for key, value := range spreadProps(evalFileExpr(attr.Expr, env)) {
				if _, ok := consumed[key]; ok {
					continue
				}
				out = append(out, gosx.Attr(key, value))
			}
		}
	}
	return out
}

func engineComponentProps(attrs []ir.Attr, env fileRenderEnv, surface bool) (map[string]any, map[string]any) {
	props := map[string]any{}
	mountAttrs := map[string]any{}
	propsAttr := attrValue(attrs, env, "props")
	mergeEngineProps(props, propsAttr)

	for _, attr := range attrs {
		if attr.Kind == ir.AttrSpread {
			for key, value := range spreadProps(evalFileExpr(attr.Expr, env)) {
				normalized := normalizeSurfaceMountAttr(key)
				if surface && normalized != "" {
					mountAttrs[normalized] = value
					continue
				}
				if isEngineReservedAttr(key) {
					continue
				}
				setComponentProp(props, key, value)
			}
			continue
		}

		if isEngineReservedAttr(attr.Name) {
			continue
		}

		value := attrValue([]ir.Attr{attr}, env, attr.Name)
		if surface {
			if normalized := normalizeSurfaceMountAttr(attr.Name); normalized != "" {
				mountAttrs[normalized] = value
				continue
			}
		}
		setComponentProp(props, attr.Name, value)
	}

	if len(props) == 0 {
		props = nil
	}
	if len(mountAttrs) == 0 {
		mountAttrs = nil
	}
	return props, mountAttrs
}

func mergeEngineProps(dst map[string]any, value any) {
	for key, item := range spreadProps(value) {
		setComponentProp(dst, key, item)
	}
}

func isEngineReservedAttr(name string) bool {
	switch strings.TrimSpace(name) {
	case "name", "component", "kind", "wasmPath", "wasm", "programRef", "program", "jsPath", "js", "script", "jsExport", "export", "factory", "mountId", "capabilities", "runtime", "props", "id":
		return true
	default:
		return false
	}
}

func normalizeSurfaceMountAttr(name string) string {
	name = strings.TrimSpace(name)
	switch name {
	case "className":
		return "class"
	case "class", "style", "role", "title":
		return name
	}
	if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-") {
		return name
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func marshalEngineProps(props map[string]any) json.RawMessage {
	if len(props) == 0 {
		return nil
	}
	data, err := json.Marshal(props)
	if err != nil {
		return nil
	}
	return data
}

func engineCapabilitiesValue(value any, fallback []engine.Capability) []engine.Capability {
	if value == nil {
		if len(fallback) == 0 {
			return nil
		}
		return append([]engine.Capability(nil), fallback...)
	}

	normalized := []engine.Capability{}
	appendCapability := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		normalized = append(normalized, engine.Capability(raw))
	}

	switch v := value.(type) {
	case string:
		for _, part := range strings.Fields(strings.NewReplacer(",", " ", "|", " ").Replace(v)) {
			appendCapability(part)
		}
	case []string:
		for _, item := range v {
			appendCapability(item)
		}
	case []engine.Capability:
		if len(v) == 0 {
			return nil
		}
		return append([]engine.Capability(nil), v...)
	default:
		rv := reflect.ValueOf(value)
		for rv.IsValid() && rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			rv = rv.Elem()
		}
		if rv.IsValid() && (rv.Kind() == reflect.Array || rv.Kind() == reflect.Slice) {
			for i := 0; i < rv.Len(); i++ {
				appendCapability(fmt.Sprint(rv.Index(i).Interface()))
			}
		}
	}

	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func defaultScene3DProps(raw json.RawMessage, programRef string) json.RawMessage {
	props := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &props)
	}
	if props == nil {
		props = map[string]any{}
	}
	if _, ok := lookupTemplatePropValue(props, "width"); !ok {
		props["width"] = 720
	}
	if _, ok := lookupTemplatePropValue(props, "height"); !ok {
		props["height"] = 420
	}
	if _, ok := lookupTemplatePropValue(props, "background"); !ok {
		props["background"] = "#08151f"
	}
	if _, ok := lookupTemplatePropValue(props, "autoRotate"); !ok {
		props["autoRotate"] = true
	}
	if _, ok := lookupTemplatePropValue(props, "camera"); !ok {
		props["camera"] = map[string]any{
			"z":   6,
			"fov": 75,
		}
	}
	if _, ok := lookupTemplatePropValue(props, "scene"); !ok && strings.TrimSpace(programRef) == "" {
		props["scene"] = map[string]any{
			"objects": []map[string]any{
				{
					"kind":  "cube",
					"size":  1.8,
					"x":     -1.2,
					"y":     0.2,
					"z":     0,
					"color": "#8de1ff",
					"spinX": 0.42,
					"spinY": 0.74,
					"spinZ": 0.18,
				},
				{
					"kind":  "cube",
					"size":  1.1,
					"x":     1.7,
					"y":     -0.8,
					"z":     1.4,
					"color": "#ffd48f",
					"spinX": -0.22,
					"spinY": 0.46,
					"spinZ": 0.12,
				},
			},
		}
	}
	return marshalEngineProps(props)
}

func spreadValue(value any, name string) (any, bool) {
	for _, candidate := range []string{name, exportedPropAlias(name), unexportedPropAlias(name)} {
		if candidate == "" {
			continue
		}
		if item, ok := mapLookup(value, candidate); ok {
			return item, true
		}
		if item := selectValue(value, candidate); item != nil {
			return item, true
		}
	}
	return nil, false
}

func spreadProps(value any) map[string]any {
	out := map[string]any{}

	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return out
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return out
	}

	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return out
		}
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			val := iter.Value()
			if key.IsValid() && key.CanInterface() && val.IsValid() && val.CanInterface() {
				out[fmt.Sprint(key.Interface())] = val.Interface()
			}
		}
	case reflect.Struct:
		for i := 0; i < rv.NumField(); i++ {
			field := rv.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			valueField := rv.Field(i)
			if !valueField.IsValid() || !valueField.CanInterface() {
				continue
			}
			out[field.Name] = valueField.Interface()
		}
	}
	return out
}
