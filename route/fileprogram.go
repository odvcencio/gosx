package route

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/ir"
	islandprogram "github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/textlayout"
)

type fileProgramRenderer struct {
	prog           *ir.Program
	components     map[string]*ir.Component
	componentIndex map[string]int
	islandPrograms map[string]*islandprogram.Program
	opts           fileRenderOptions
	replaced       bool
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
	componentIndex := make(map[string]int, len(prog.Components))
	for i := range prog.Components {
		components[prog.Components[i].Name] = &prog.Components[i]
		componentIndex[prog.Components[i].Name] = i
	}
	return &fileProgramRenderer{
		prog:           prog,
		components:     components,
		componentIndex: componentIndex,
		islandPrograms: make(map[string]*islandprogram.Program),
		opts:           opts,
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
	formContract := fileAutoManagedFormContract(node.Attrs, env, strings.EqualFold(node.Tag, "form"))
	b.WriteByte('<')
	b.WriteString(tag)
	attrs := node.Attrs
	if formContract.Managed {
		attrs = managedFormAttrs(node.Attrs)
	}
	r.renderAttrs(&b, attrs, env)
	r.writeManagedFormContract(&b, node.Attrs, env, formContract)
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

	if comp, ok := r.components[node.Tag]; ok {
		switch {
		case comp.IsIsland:
			return r.renderLocalIsland(node.Tag, node, env)
		case !comp.IsEngine:
			return r.renderLocalComponent(comp, node, env)
		}
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
	case "Form":
		return true, r.renderManagedForm(node, env, managedFormOptions{})
	case "ActionForm":
		return true, r.renderManagedForm(node, env, managedFormOptions{
			defaultMethod: strings.ToLower(http.MethodPost),
			defaultAction: fileRenderActionPath(env, stringValue(attrValue(node.Attrs, env, "actionName"))),
		})
	case "Image":
		return true, r.renderImage(node, env)
	case "Motion":
		return true, r.renderMotion(node, env)
	case "Video":
		return true, r.renderVideo(node, env)
	case "TextBlock":
		return true, r.renderTextBlock(node, env)
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
	contract := fileManagedLinkContractForAttrs(node.Attrs, env)
	r.renderLinkAttrs(&b, node.Attrs, env)
	r.writeManagedLinkContract(&b, node.Attrs, env, contract)
	b.WriteByte('>')
	b.WriteString(r.renderChildren(node.Children, env))
	b.WriteString("</a>")
	return b.String()
}

func (r *fileProgramRenderer) renderLinkAttrs(b *strings.Builder, attrs []ir.Attr, env fileRenderEnv) {
	for _, attr := range attrs {
		if linkReservedAttr(attr.Name) {
			continue
		}
		switch attr.Kind {
		case ir.AttrStatic:
			fmt.Fprintf(b, ` %s="%s"`, html.EscapeString(normalizeFileAttrName(attr.Name)), html.EscapeString(attr.Value))
		case ir.AttrExpr:
			renderFileEvaluatedAttr(b, html.EscapeString(normalizeFileAttrName(attr.Name)), evalFileExpr(attr.Expr, env))
		case ir.AttrBool:
			fmt.Fprintf(b, " %s", html.EscapeString(normalizeFileAttrName(attr.Name)))
		case ir.AttrSpread:
			for _, entry := range sortedSpreadProps(evalFileExpr(attr.Expr, env)) {
				key := entry.Key
				value := entry.Value
				normalized := normalizeFileAttrName(key)
				if normalized == "" || linkReservedAttr(normalized) {
					continue
				}
				renderFileEvaluatedAttr(b, html.EscapeString(normalized), value)
			}
		}
	}
}

func normalizedLinkPrefetchValue(attrs []ir.Attr, env fileRenderEnv) (string, bool) {
	return server.NormalizeNavigationLinkPrefetch(stringValue(attrValue(attrs, env, server.NavigationLinkPrefetchAttr, "prefetch")))
}

func linkReservedAttr(name string) bool {
	switch normalizeFileAttrName(strings.TrimSpace(name)) {
	case "prefetch", server.NavigationLinkPrefetchAttr, "current", server.NavigationLinkCurrentAttr, server.NavigationLinkCurrentPolicyAttr:
		return true
	default:
		return false
	}
}

type fileManagedLinkContract struct {
	Current          string
	CurrentPolicy    string
	Prefetch         string
	PrefetchProvided bool
}

type fileManagedLinkPresence struct {
	Navigation       bool
	LinkState        bool
	PrefetchState    bool
	Enhancement      bool
	EnhancementLayer bool
	Fallback         bool
	AriaCurrent      bool
}

func fileCurrentRequestPath(env fileRenderEnv) string {
	if pageValue, ok := env.values["page"].(map[string]any); ok {
		if current := strings.TrimSpace(stringValue(pageValue["path"])); current != "" {
			return current
		}
	}
	if requestValue, ok := env.values["request"].(map[string]any); ok {
		if current := strings.TrimSpace(stringValue(requestValue["path"])); current != "" {
			return current
		}
	}
	return "/"
}

func fileManagedLinkContractForAttrs(attrs []ir.Attr, env fileRenderEnv) fileManagedLinkContract {
	currentPolicy := normalizedLinkCurrentPolicy(attrs, env)
	prefetch, prefetchProvided := normalizedLinkPrefetchValue(attrs, env)
	return fileManagedLinkContract{
		Current:          server.ResolveNavigationLinkCurrent(stringValue(attrValue(attrs, env, "href")), fileCurrentRequestPath(env), currentPolicy),
		CurrentPolicy:    currentPolicy,
		Prefetch:         prefetch,
		PrefetchProvided: prefetchProvided,
	}
}

func fileManagedLinkPresenceForAttrs(attrs []ir.Attr, env fileRenderEnv) fileManagedLinkPresence {
	return fileManagedLinkPresence{
		Navigation:       attrValue(attrs, env, server.NavigationLinkAttr) != nil,
		LinkState:        attrValue(attrs, env, server.NavigationLinkStateAttr) != nil,
		PrefetchState:    attrValue(attrs, env, server.NavigationLinkPrefetchStateAttr) != nil,
		Enhancement:      attrValue(attrs, env, server.NavigationEnhanceAttr) != nil,
		EnhancementLayer: attrValue(attrs, env, server.NavigationEnhanceLayerAttr) != nil,
		Fallback:         attrValue(attrs, env, server.NavigationFallbackAttr) != nil,
		AriaCurrent:      attrValue(attrs, env, "aria-current", "ariaCurrent") != nil,
	}
}

func (r *fileProgramRenderer) writeManagedLinkContract(b *strings.Builder, attrs []ir.Attr, env fileRenderEnv, contract fileManagedLinkContract) {
	presence := fileManagedLinkPresenceForAttrs(attrs, env)
	r.writeManagedLinkBaseAttrs(b, presence)
	r.writeManagedLinkCurrentAttrs(b, contract)
	r.writeManagedLinkPrefetchAttrs(b, presence, contract)
	r.writeManagedLinkA11yAttrs(b, presence, contract)
}

func (r *fileProgramRenderer) writeManagedLinkBaseAttrs(b *strings.Builder, presence fileManagedLinkPresence) {
	if !presence.Navigation {
		b.WriteString(" " + server.NavigationLinkAttr)
	}
	if !presence.LinkState {
		fmt.Fprintf(b, ` %s="idle"`, server.NavigationLinkStateAttr)
	}
	if !presence.Enhancement {
		fmt.Fprintf(b, ` %s="navigation"`, server.NavigationEnhanceAttr)
	}
	if !presence.EnhancementLayer {
		fmt.Fprintf(b, ` %s="bootstrap"`, server.NavigationEnhanceLayerAttr)
	}
	if !presence.Fallback {
		fmt.Fprintf(b, ` %s="native-link"`, server.NavigationFallbackAttr)
	}
}

func (r *fileProgramRenderer) writeManagedLinkCurrentAttrs(b *strings.Builder, contract fileManagedLinkContract) {
	fmt.Fprintf(b, ` %s="%s"`, server.NavigationLinkCurrentPolicyAttr, html.EscapeString(contract.CurrentPolicy))
	fmt.Fprintf(b, ` %s="%s"`, server.NavigationLinkCurrentAttr, html.EscapeString(contract.Current))
}

func (r *fileProgramRenderer) writeManagedLinkPrefetchAttrs(b *strings.Builder, presence fileManagedLinkPresence, contract fileManagedLinkContract) {
	if !presence.PrefetchState {
		fmt.Fprintf(b, ` %s="idle"`, server.NavigationLinkPrefetchStateAttr)
	}
	if contract.PrefetchProvided {
		fmt.Fprintf(b, ` %s="%s"`, server.NavigationLinkPrefetchAttr, html.EscapeString(contract.Prefetch))
	}
}

func (r *fileProgramRenderer) writeManagedLinkA11yAttrs(b *strings.Builder, presence fileManagedLinkPresence, contract fileManagedLinkContract) {
	if contract.Current == "page" && !presence.AriaCurrent {
		fmt.Fprintf(b, ` aria-current="page" %s="true"`, server.NavigationLinkManagedCurrentAttr)
	}
}

func normalizedLinkCurrentPolicy(attrs []ir.Attr, env fileRenderEnv) string {
	return server.NormalizeNavigationLinkCurrentPolicy(stringValue(attrValue(
		attrs,
		env,
		server.NavigationLinkCurrentPolicyAttr,
		server.NavigationLinkCurrentAttr,
		"current",
	)))
}

type managedFormOptions struct {
	defaultMethod string
	defaultAction string
}

type fileManagedFormContract struct {
	Managed bool
	Mode    string
}

type fileManagedFormPresence struct {
	Form             bool
	State            bool
	Enhancement      bool
	EnhancementLayer bool
	Fallback         bool
}

func (r *fileProgramRenderer) renderManagedForm(node *ir.Node, env fileRenderEnv, opts managedFormOptions) string {
	var b strings.Builder
	contract := fileBuiltinManagedFormContract(node.Attrs, env, opts.defaultMethod)
	b.WriteString("<form")
	if method := strings.TrimSpace(opts.defaultMethod); method != "" && attrValue(node.Attrs, env, "method") == nil {
		fmt.Fprintf(&b, ` method="%s"`, html.EscapeString(method))
	}
	if action := strings.TrimSpace(opts.defaultAction); action != "" && attrValue(node.Attrs, env, "action") == nil {
		fmt.Fprintf(&b, ` action="%s"`, html.EscapeString(action))
	}
	r.renderAttrs(&b, managedFormAttrs(node.Attrs), env)
	r.writeManagedFormContract(&b, node.Attrs, env, contract)
	b.WriteByte('>')
	b.WriteString(r.renderChildren(node.Children, env))
	b.WriteString("</form>")
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

func (r *fileProgramRenderer) renderMotion(node *ir.Node, env fileRenderEnv) string {
	props := server.MotionProps{
		Tag:                  firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "as", "tag")), "div"),
		Preset:               server.MotionPreset(stringValue(attrValue(node.Attrs, env, "preset"))),
		Trigger:              server.MotionTrigger(stringValue(attrValue(node.Attrs, env, "trigger"))),
		Duration:             int(numericValue(attrValue(node.Attrs, env, "duration", "durationMs", "duration_ms"))),
		Delay:                int(numericValue(attrValue(node.Attrs, env, "delay", "delayMs", "delay_ms"))),
		Easing:               stringValue(attrValue(node.Attrs, env, "easing")),
		Distance:             numericValue(attrValue(node.Attrs, env, "distance")),
		RespectReducedMotion: boolPointerValue(firstNonEmptyValue(attrValue(node.Attrs, env, "respectReducedMotion"), attrValue(node.Attrs, env, "respect_reduced_motion"))),
	}
	if env.enableBootstrap != nil {
		env.enableBootstrap()
	}
	extra := fileExtraNodeAttrs(node.Attrs, env, fileAttrNameSet(
		"as", "tag",
		"preset",
		"trigger",
		"duration", "durationMs", "duration_ms",
		"delay", "delayMs", "delay_ms",
		"easing",
		"distance",
		"respectReducedMotion", "respect_reduced_motion",
	))
	args := make([]any, 0, 2)
	if len(extra) > 0 {
		args = append(args, gosx.Attrs(extra...))
	}
	childrenHTML := r.renderChildren(node.Children, env)
	if childrenHTML != "" {
		args = append(args, gosx.RawHTML(childrenHTML))
	}
	return gosx.RenderHTML(server.Motion(props, args...))
}

func (r *fileProgramRenderer) renderVideo(node *ir.Node, env fileRenderEnv) string {
	props := server.VideoProps{
		EngineName:    stringValue(attrValue(node.Attrs, env, "engineName", "name", "component")),
		Src:           stringValue(attrValue(node.Attrs, env, "src")),
		Sources:       videoSourceListValue(attrValue(node.Attrs, env, "sources")),
		Poster:        stringValue(attrValue(node.Attrs, env, "poster")),
		Preload:       stringValue(attrValue(node.Attrs, env, "preload")),
		CrossOrigin:   firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "crossOrigin")), stringValue(attrValue(node.Attrs, env, "crossorigin"))),
		AutoPlay:      truthy(attrValue(node.Attrs, env, "autoPlay", "autoplay")),
		Controls:      truthy(attrValue(node.Attrs, env, "controls")),
		Loop:          truthy(attrValue(node.Attrs, env, "loop")),
		Muted:         truthy(attrValue(node.Attrs, env, "muted")),
		PlaysInline:   truthy(attrValue(node.Attrs, env, "playsInline", "playsinline")),
		Width:         int(numericValue(attrValue(node.Attrs, env, "width"))),
		Height:        int(numericValue(attrValue(node.Attrs, env, "height"))),
		Volume:        numericValue(attrValue(node.Attrs, env, "volume")),
		Rate:          numericValue(attrValue(node.Attrs, env, "rate")),
		Sync:          stringValue(attrValue(node.Attrs, env, "sync")),
		SyncMode:      firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "syncMode")), stringValue(attrValue(node.Attrs, env, "sync_mode"))),
		SyncStrategy:  firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "syncStrategy")), stringValue(attrValue(node.Attrs, env, "sync_strategy"))),
		HLS:           mapStringAnyValue(attrValue(node.Attrs, env, "hls")),
		HLSConfig:     mapStringAnyValue(attrValue(node.Attrs, env, "hlsConfig", "hls_config")),
		SubtitleBase:  firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "subtitleBase")), stringValue(attrValue(node.Attrs, env, "subtitle_base"))),
		SubtitleTrack: firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "subtitleTrack")), stringValue(attrValue(node.Attrs, env, "subtitle_track"))),
		SubtitleTracks: videoTrackListValue(firstNonEmptyValue(
			attrValue(node.Attrs, env, "subtitleTracks"),
			attrValue(node.Attrs, env, "subtitle_tracks"),
			attrValue(node.Attrs, env, "tracks"),
		)),
	}
	extra := fileExtraNodeAttrs(node.Attrs, env, fileAttrNameSet(
		"engineName", "name", "component",
		"src", "sources",
		"poster", "preload",
		"crossOrigin", "crossorigin",
		"autoPlay", "autoplay",
		"controls",
		"loop",
		"muted",
		"playsInline", "playsinline",
		"width", "height",
		"volume", "rate",
		"sync", "syncMode", "sync_mode", "syncStrategy", "sync_strategy",
		"hls", "hlsConfig", "hls_config",
		"subtitleBase", "subtitle_base",
		"subtitleTrack", "subtitle_track",
		"subtitleTracks", "subtitle_tracks",
		"tracks",
	))
	args := make([]any, 0, 2)
	if len(extra) > 0 {
		args = append(args, gosx.Attrs(extra...))
	}
	childrenHTML := r.renderChildren(node.Children, env)
	if childrenHTML != "" {
		args = append(args, gosx.RawHTML(childrenHTML))
	}
	fallback := server.Video(props, args...)
	return gosx.RenderHTML(env.engine(server.VideoEngineConfig(props), fallback))
}

func (r *fileProgramRenderer) renderTextBlock(node *ir.Node, env fileRenderEnv) string {
	props := server.TextBlockProps{
		Mode:          server.TextBlockMode(stringValue(attrValue(node.Attrs, env, "mode"))),
		Tag:           firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "as", "tag")), "div"),
		Text:          stringValue(attrValue(node.Attrs, env, "text")),
		Font:          stringValue(attrValue(node.Attrs, env, "font")),
		Lang:          firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "lang", "locale")), ""),
		Direction:     firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "dir", "direction")), ""),
		Align:         stringValue(attrValue(node.Attrs, env, "align", "textAlign", "text-align")),
		WhiteSpace:    textlayout.WhiteSpace(stringValue(attrValue(node.Attrs, env, "whiteSpace", "whitespace"))),
		LineHeight:    numericValue(attrValue(node.Attrs, env, "lineHeight")),
		MaxWidth:      numericValue(attrValue(node.Attrs, env, "maxWidth")),
		MaxLines:      int(numericValue(attrValue(node.Attrs, env, "maxLines"))),
		Overflow:      textlayout.OverflowMode(stringValue(attrValue(node.Attrs, env, "overflow"))),
		HeightHint:    numericValue(attrValue(node.Attrs, env, "heightHint")),
		LineCountHint: int(numericValue(attrValue(node.Attrs, env, "lineCountHint"))),
		Static:        truthy(attrValue(node.Attrs, env, "static")),
		Source:        firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "source")), r.textContentChildren(node.Children, env)),
	}
	if env.enableBootstrap != nil && server.TextBlockRequiresBootstrap(props) {
		env.enableBootstrap()
	}
	childrenHTML := r.renderChildren(node.Children, env)
	if strings.TrimSpace(childrenHTML) == "" && props.Text != "" {
		childrenHTML = ""
	}
	extra := fileExtraNodeAttrs(node.Attrs, env, fileAttrNameSet(
		"mode",
		"as", "tag",
		"text",
		"font",
		"lang", "locale",
		"dir", "direction",
		"align", "textAlign", "text-align",
		"whiteSpace", "whitespace",
		"lineHeight",
		"maxWidth",
		"maxLines",
		"overflow",
		"heightHint",
		"lineCountHint",
		"source",
		"static",
	))
	args := make([]any, 0, 2)
	if len(extra) > 0 {
		args = append(args, gosx.Attrs(extra...))
	}
	if childrenHTML != "" {
		args = append(args, gosx.RawHTML(childrenHTML))
	}
	return gosx.RenderHTML(server.TextBlock(props, args...))
}

func (r *fileProgramRenderer) renderStylesheet(node *ir.Node, env fileRenderEnv) string {
	href, opts := stylesheetContractForAttrs(node.Attrs, env)
	extra := fileExtraNodeAttrs(node.Attrs, env, fileAttrNameSet("href", "src", "rel", "layer", "owner", "source"))
	args := []any{}
	if len(extra) > 0 {
		args = append(args, gosx.Attrs(extra...))
	}
	return gosx.RenderHTML(server.DocumentStylesheet(href, opts, args...))
}

type fileEngineDefaults struct {
	Name         string
	WASMPath     string
	Capabilities []engine.Capability
	Runtime      engine.Runtime
	MountAttrs   map[string]any
}

type fileEngineTransport struct {
	Name         any
	WASMPath     any
	MountID      any
	Capabilities any
	Runtime      any
}

func (r *fileProgramRenderer) renderSurface(node *ir.Node, env fileRenderEnv) string {
	return r.renderEngineComponent(node, env, engine.KindSurface, fileEngineDefaults{})
}

func (r *fileProgramRenderer) renderWorker(node *ir.Node, env fileRenderEnv) string {
	return r.renderEngineComponent(node, env, engine.KindWorker, fileEngineDefaults{})
}

func (r *fileProgramRenderer) renderScene3D(node *ir.Node, env fileRenderEnv) string {
	return r.renderEngineComponent(node, env, engine.KindSurface, fileEngineDefaults{
		Name: "GoSXScene3D",
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
	props, transport := splitEngineTransportProps(props)
	name := engineComponentIdentity(node.Attrs, env, defaults, transport)
	mountID := firstNonEmptyString(
		stringValue(attrValue(node.Attrs, env, "mountId", "id")),
		stringValue(transport.MountID),
	)
	if kind == engine.KindSurface {
		mountAttrs = withDefaultMountAttrs(mountAttrs, defaults.MountAttrs)
	}

	cfg := engineComponentConfigValue(node.Attrs, env, kind, defaults, transport, name, mountID, props, mountAttrs)
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

func engineComponentIdentity(attrs []ir.Attr, env fileRenderEnv, defaults fileEngineDefaults, transport fileEngineTransport) string {
	return firstNonEmptyString(
		stringValue(attrValue(attrs, env, "name", "component")),
		stringValue(transport.Name),
		defaults.Name,
	)
}

func withDefaultMountAttrs(mountAttrs map[string]any, defaults map[string]any) map[string]any {
	if len(defaults) == 0 {
		return mountAttrs
	}
	if mountAttrs == nil {
		mountAttrs = map[string]any{}
	}
	for _, entry := range sortedStringAnyMap(defaults) {
		if _, exists := mountAttrs[entry.Key]; exists {
			continue
		}
		mountAttrs[entry.Key] = entry.Value
	}
	return mountAttrs
}

func engineComponentConfigValue(attrs []ir.Attr, env fileRenderEnv, kind engine.Kind, defaults fileEngineDefaults, transport fileEngineTransport, name, mountID string, props, mountAttrs map[string]any) engine.Config {
	return engine.Config{
		Name:         name,
		Kind:         kind,
		WASMPath:     firstNonEmptyString(stringValue(attrValue(attrs, env, "wasmPath", "wasm", "programRef", "program")), stringValue(transport.WASMPath), defaults.WASMPath),
		MountID:      mountID,
		MountAttrs:   mountAttrs,
		Props:        marshalEngineProps(props),
		Capabilities: engineCapabilitiesValue(firstNonEmptyValue(attrValue(attrs, env, "capabilities"), transport.Capabilities), defaults.Capabilities),
		Runtime:      engine.Runtime(firstNonEmptyString(stringValue(attrValue(attrs, env, "runtime")), stringValue(transport.Runtime), string(defaults.Runtime))),
	}
}

func splitEngineTransportProps(props map[string]any) (map[string]any, fileEngineTransport) {
	if len(props) == 0 {
		return props, fileEngineTransport{}
	}

	clean := cloneSpreadProps(props)
	transport := fileEngineTransport{
		Name:         extractEngineTransportValue(clean, "name", "component"),
		WASMPath:     extractEngineTransportValue(clean, "wasmPath", "wasm", "programRef", "program"),
		MountID:      extractEngineTransportValue(clean, "mountId", "id"),
		Capabilities: extractEngineTransportValue(clean, "capabilities"),
		Runtime:      extractEngineTransportValue(clean, "runtime"),
	}
	if len(clean) == 0 {
		clean = nil
	}
	return clean, transport
}

func extractEngineTransportValue(props map[string]any, names ...string) any {
	if len(props) == 0 {
		return nil
	}
	for _, name := range names {
		if value, ok := lookupTemplatePropValue(props, name); ok {
			for _, candidate := range names {
				deleteTemplatePropValue(props, candidate)
			}
			return value
		}
	}
	return nil
}

func deleteTemplatePropValue(props map[string]any, name string) {
	if len(props) == 0 {
		return
	}
	for _, candidate := range []string{name, exportedPropAlias(name), unexportedPropAlias(name), strings.ToLower(name)} {
		if candidate == "" {
			continue
		}
		delete(props, candidate)
	}
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

func (r *fileProgramRenderer) renderLocalIsland(name string, node *ir.Node, env fileRenderEnv) string {
	if env.renderIsland == nil {
		return defaultRenderedComponent(node.Tag, r.componentAttrMap(node.Attrs, env), r.renderChildren(node.Children, env))
	}

	prog, err := r.islandProgram(name)
	if err != nil {
		return gosx.RenderHTML(gosx.El("div",
			gosx.Attrs(gosx.Attr("class", "gosx-error")),
			gosx.Text(fmt.Sprintf("island error: %v", err)),
		))
	}

	props := r.componentAttrMap(node.Attrs, env)
	return gosx.RenderHTML(env.island(prog, props))
}

func (r *fileProgramRenderer) islandProgram(name string) (*islandprogram.Program, error) {
	if prog, ok := r.islandPrograms[name]; ok {
		return prog, nil
	}

	idx, ok := r.componentIndex[name]
	if !ok {
		return nil, fmt.Errorf("component %q not found", name)
	}

	prog, err := ir.LowerIsland(r.prog, idx)
	if err != nil {
		return nil, err
	}
	r.islandPrograms[name] = prog
	return prog, nil
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
	for _, entry := range sortedSpreadProps(value) {
		normalized := normalizeFileAttrName(entry.Key)
		if normalized == "" {
			continue
		}
		renderFileEvaluatedAttr(b, html.EscapeString(normalized), entry.Value)
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

func plainTextFileEvaluatedExpr(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case gosx.Node:
		return gosx.PlainText(v)
	case *gosx.Node:
		if v == nil {
			return ""
		}
		return gosx.PlainText(*v)
	case []gosx.Node:
		var b strings.Builder
		for _, node := range v {
			b.WriteString(gosx.PlainText(node))
		}
		return b.String()
	case []string:
		return strings.Join(v, "")
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
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

func managedFormAttrs(attrs []ir.Attr) []ir.Attr {
	out := make([]ir.Attr, 0, len(attrs))
	for _, attr := range attrs {
		switch strings.TrimSpace(attr.Name) {
		case "actionName", server.NavigationFormModeAttr:
			continue
		}
		out = append(out, attr)
	}
	return out
}

func mergeComponentProps(props map[string]any, value any) {
	for key, item := range spreadProps(value) {
		setComponentProp(props, key, item)
	}
}

func fileRenderActionPath(env fileRenderEnv, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || env.funcs == nil {
		return ""
	}
	actionPath, ok := env.funcs["actionPath"].(func(string) string)
	if !ok {
		return ""
	}
	return actionPath(name)
}

func fileFormEnhancementMode(attrs []ir.Attr, env fileRenderEnv) string {
	return managedFormMode(attrs, env, "")
}

func fileAutoFormEnhancementMode(attrs []ir.Attr, env fileRenderEnv) string {
	mode := managedFormMode(attrs, env, "")
	if mode != http.MethodGet {
		return ""
	}
	return mode
}

func fileBuiltinManagedFormContract(attrs []ir.Attr, env fileRenderEnv, defaultMethod string) fileManagedFormContract {
	return fileManagedFormContract{
		Managed: true,
		Mode:    managedFormMode(attrs, env, defaultMethod),
	}
}

func fileAutoManagedFormContract(attrs []ir.Attr, env fileRenderEnv, isForm bool) fileManagedFormContract {
	if !isForm {
		return fileManagedFormContract{}
	}
	mode := fileAutoFormEnhancementMode(attrs, env)
	if mode == "" {
		return fileManagedFormContract{}
	}
	return fileManagedFormContract{
		Managed: true,
		Mode:    mode,
	}
}

func fileManagedFormPresenceForAttrs(attrs []ir.Attr, env fileRenderEnv) fileManagedFormPresence {
	return fileManagedFormPresence{
		Form:             attrValue(attrs, env, server.NavigationFormAttr) != nil,
		State:            attrValue(attrs, env, server.NavigationFormStateAttr) != nil,
		Enhancement:      attrValue(attrs, env, server.NavigationEnhanceAttr) != nil,
		EnhancementLayer: attrValue(attrs, env, server.NavigationEnhanceLayerAttr) != nil,
		Fallback:         attrValue(attrs, env, server.NavigationFallbackAttr) != nil,
	}
}

func (r *fileProgramRenderer) writeManagedFormContract(b *strings.Builder, attrs []ir.Attr, env fileRenderEnv, contract fileManagedFormContract) {
	if !contract.Managed {
		return
	}
	presence := fileManagedFormPresenceForAttrs(attrs, env)
	if !presence.Form {
		b.WriteString(" " + server.NavigationFormAttr)
	}
	if contract.Mode != "" {
		fmt.Fprintf(b, ` %s="%s"`, server.NavigationFormModeAttr, html.EscapeString(contract.Mode))
	}
	if !presence.State {
		fmt.Fprintf(b, ` %s="idle"`, server.NavigationFormStateAttr)
	}
	if !presence.Enhancement {
		fmt.Fprintf(b, ` %s="form"`, server.NavigationEnhanceAttr)
	}
	if !presence.EnhancementLayer {
		fmt.Fprintf(b, ` %s="bootstrap"`, server.NavigationEnhanceLayerAttr)
	}
	if !presence.Fallback {
		fmt.Fprintf(b, ` %s="native-form"`, server.NavigationFallbackAttr)
	}
}

func managedFormMode(attrs []ir.Attr, env fileRenderEnv, defaultMethod string) string {
	return server.NormalizeNavigationFormMode(
		stringValue(attrValue(attrs, env, "method")),
		stringValue(attrValue(attrs, env, "action")),
		stringValue(attrValue(attrs, env, "target")),
		defaultMethod,
	)
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
			for _, entry := range sortedSpreadProps(evalFileExpr(attr.Expr, env)) {
				if _, ok := consumed[entry.Key]; ok {
					continue
				}
				if rendered, ok := fileNodeAttr(normalizeFileAttrName(entry.Key), entry.Value); ok {
					out = append(out, rendered)
				}
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
			for _, entry := range sortedSpreadProps(evalFileExpr(attr.Expr, env)) {
				key := entry.Key
				value := entry.Value
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
	for _, entry := range sortedSpreadProps(value) {
		setComponentProp(dst, entry.Key, entry.Value)
	}
}

func stylesheetContractForAttrs(attrs []ir.Attr, env fileRenderEnv) (string, server.StylesheetOptions) {
	href := stringValue(attrValue(attrs, env, "href", "src"))
	layer := server.CSSLayer(firstNonEmptyString(stringValue(attrValue(attrs, env, "layer")), string(server.CSSLayerPage)))
	return href, server.StylesheetOptions{
		Layer:  layer,
		Owner:  firstNonEmptyString(stringValue(attrValue(attrs, env, "owner")), server.FileStylesheetOwner(layer)),
		Source: stringValue(attrValue(attrs, env, "source")),
	}
}

func fileAttrNameSet(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		normalized := normalizeFileAttrName(name)
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func fileExtraNodeAttrs(attrs []ir.Attr, env fileRenderEnv, consumed map[string]struct{}) []any {
	out := []any{}
	for _, attr := range attrs {
		out = appendFileExtraNodeAttr(out, attr, env, consumed)
	}
	return out
}

func appendFileExtraNodeAttr(out []any, attr ir.Attr, env fileRenderEnv, consumed map[string]struct{}) []any {
	if attr.Kind == ir.AttrSpread {
		for _, entry := range sortedSpreadProps(evalFileExpr(attr.Expr, env)) {
			normalized := normalizeFileAttrName(entry.Key)
			if normalized == "" || fileAttrConsumed(consumed, normalized) {
				continue
			}
			if rendered, ok := fileNodeAttr(normalized, entry.Value); ok {
				out = append(out, rendered)
			}
		}
		return out
	}

	normalized := normalizeFileAttrName(attr.Name)
	if normalized == "" || fileAttrConsumed(consumed, normalized) {
		return out
	}

	switch attr.Kind {
	case ir.AttrStatic:
		out = append(out, gosx.Attr(normalized, attr.Value))
	case ir.AttrExpr:
		if rendered, ok := fileNodeAttr(normalized, evalFileExpr(attr.Expr, env)); ok {
			out = append(out, rendered)
		}
	case ir.AttrBool:
		out = append(out, gosx.BoolAttr(normalized))
	}
	return out
}

func fileAttrConsumed(consumed map[string]struct{}, name string) bool {
	if len(consumed) == 0 {
		return false
	}
	_, ok := consumed[name]
	return ok
}

func fileNodeAttr(name string, value any) (any, bool) {
	switch v := value.(type) {
	case nil:
		return nil, false
	case bool:
		if !v {
			return nil, false
		}
		return gosx.BoolAttr(name), true
	default:
		return gosx.Attr(name, value), true
	}
}

type fileStringAnyEntry struct {
	Key   string
	Value any
}

func sortedSpreadProps(value any) []fileStringAnyEntry {
	return sortedStringAnyMap(spreadProps(value))
}

func sortedStringAnyMap(values map[string]any) []fileStringAnyEntry {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]fileStringAnyEntry, 0, len(keys))
	for _, key := range keys {
		out = append(out, fileStringAnyEntry{Key: key, Value: values[key]})
	}
	return out
}

func isEngineReservedAttr(name string) bool {
	switch strings.TrimSpace(name) {
	case "name", "component", "kind", "wasmPath", "wasm", "programRef", "program", "mountId", "capabilities", "runtime", "props", "id":
		return true
	default:
		return false
	}
}

func (r *fileProgramRenderer) renderTextBlockExtraAttrs(b *strings.Builder, attrs []ir.Attr, env fileRenderEnv) {
	for _, attr := range attrs {
		if isTextBlockReservedAttr(attr.Name) || attr.Kind == ir.AttrSpread {
			continue
		}
		switch attr.Kind {
		case ir.AttrStatic:
			fmt.Fprintf(b, ` %s="%s"`, html.EscapeString(attr.Name), html.EscapeString(attr.Value))
		case ir.AttrExpr:
			value := evalFileExpr(attr.Expr, env)
			renderFileEvaluatedAttr(b, html.EscapeString(attr.Name), value)
		case ir.AttrBool:
			fmt.Fprintf(b, " %s", html.EscapeString(attr.Name))
		}
	}
}

func isTextBlockReservedAttr(name string) bool {
	switch strings.TrimSpace(name) {
	case "mode", "as", "tag", "text", "font", "lang", "locale", "dir", "direction", "align", "textAlign", "text-align", "whiteSpace", "whitespace", "lineHeight", "maxWidth", "maxLines", "overflow", "heightHint", "lineCountHint", "source", "static":
		return true
	default:
		return false
	}
}

func (r *fileProgramRenderer) textContentChildren(children []ir.NodeID, env fileRenderEnv) string {
	var b strings.Builder
	for _, child := range children {
		b.WriteString(r.textContentNode(child, env))
	}
	return b.String()
}

func (r *fileProgramRenderer) textContentNode(nodeID ir.NodeID, env fileRenderEnv) string {
	node := r.prog.NodeAt(nodeID)
	switch node.Kind {
	case ir.NodeText:
		return node.Text
	case ir.NodeExpr:
		return plainTextFileEvaluatedExpr(evalFileExpr(node.Text, env))
	case ir.NodeFragment, ir.NodeElement:
		return r.textContentChildren(node.Children, env)
	case ir.NodeComponent:
		comp, ok := r.components[node.Tag]
		if !ok || comp.IsIsland || comp.IsEngine {
			return ""
		}
		childrenHTML := r.renderChildren(node.Children, env)
		childrenNode := gosx.RawHTML(childrenHTML)
		props := componentProps(node.Attrs, env, childrenNode)
		scope := env.withValue("props", props)
		scope = scope.withValue("children", childrenNode)
		return r.textContentNode(comp.Root, scope)
	default:
		return ""
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

func firstNonEmptyValue(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
		}
		return value
	}
	return nil
}

func boolPointerValue(value any) *bool {
	if value == nil {
		return nil
	}
	result := truthy(value)
	return &result
}

func marshalEngineProps(props map[string]any) json.RawMessage {
	if len(props) == 0 {
		return nil
	}
	normalized := canonicalizeEnginePropsMap(props)
	if len(normalized) == 0 {
		return nil
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil
	}
	return data
}

func canonicalizeEnginePropsMap(props map[string]any) map[string]any {
	if len(props) == 0 {
		return nil
	}

	groups := map[string]map[string]any{}
	for key, value := range props {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		canonical := canonicalEnginePropKey(key)
		if groups[canonical] == nil {
			groups[canonical] = map[string]any{}
		}
		groups[canonical][key] = canonicalizeEnginePropValue(value)
	}

	if len(groups) == 0 {
		return nil
	}

	out := make(map[string]any, len(groups))
	for canonical, candidates := range groups {
		if value, ok := candidates[canonical]; ok {
			out[canonical] = value
			continue
		}
		if exported := exportedPropAlias(canonical); exported != "" {
			if value, ok := candidates[exported]; ok {
				out[canonical] = value
				continue
			}
		}
		if bestKey, ok := firstSortedMapKey(candidates); ok {
			out[canonical] = candidates[bestKey]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalizeEnginePropValue(value any) any {
	if value == nil {
		return nil
	}

	if typed := mapStringAnyValue(value); len(typed) > 0 {
		return canonicalizeEnginePropsMap(typed)
	}

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
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return value
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			if !iter.Value().IsValid() || !iter.Value().CanInterface() {
				continue
			}
			out[iter.Key().String()] = canonicalizeEnginePropValue(iter.Value().Interface())
		}
		return canonicalizeEnginePropsMap(out)
	case reflect.Array, reflect.Slice:
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			if !rv.Index(i).IsValid() || !rv.Index(i).CanInterface() {
				out = append(out, nil)
				continue
			}
			out = append(out, canonicalizeEnginePropValue(rv.Index(i).Interface()))
		}
		return out
	default:
		return value
	}
}

func canonicalEnginePropKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if alias := unexportedPropAlias(name); alias != "" {
		return alias
	}
	return name
}

func firstSortedMapKey(values map[string]any) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys[0], true
}

func mapStringAnyValue(value any) map[string]any {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func videoSourceListValue(value any) []server.VideoSource {
	return decodeVideoListValue[server.VideoSource](value)
}

func videoTrackListValue(value any) []server.VideoTrack {
	return decodeVideoListValue[server.VideoTrack](value)
}

func decodeVideoListValue[T any](value any) []T {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var list []T
	if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
		return list
	}
	var single T
	if err := json.Unmarshal(data, &single); err == nil {
		return []T{single}
	}
	return nil
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
	if value == nil {
		return out
	}
	if provider, ok := value.(interface{ GoSXSpreadProps() map[string]any }); ok {
		return cloneSpreadProps(provider.GoSXSpreadProps())
	}

	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return out
		}
		if provider, ok := rv.Interface().(interface{ GoSXSpreadProps() map[string]any }); ok {
			return cloneSpreadProps(provider.GoSXSpreadProps())
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

func cloneSpreadProps(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
