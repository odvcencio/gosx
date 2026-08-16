package route

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"html"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
	gosxcss "m31labs.dev/gosx/css"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/internal/htmlattr"
	"m31labs.dev/gosx/ir"
	islandprogram "m31labs.dev/gosx/island/program"
	gosxscene "m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/capability"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/textlayout"
)

type fileProgramRenderer struct {
	prog           *ir.Program
	components     map[string]*ir.Component
	componentIndex map[string]int
	islandPrograms map[string]*islandprogram.Program
	opts           fileRenderOptions
	replaced       bool
	err            error
}

func renderFileProgramHTML(prog *ir.Program, component string, opts fileRenderOptions) (string, bool, error) {
	// gosx#185: a render profile's validation pass runs before anything is
	// written, over the whole compiled program, not just the component
	// being rendered. A non-empty diagnostic list aborts the render here —
	// fail closed, no output written at all — rather than after the
	// component below has already produced partial HTML.
	if opts.Profile != nil && opts.Profile.Validate != nil {
		if diags := opts.Profile.Validate(prog); len(diags) > 0 {
			return "", false, &RenderProfileError{Diagnostics: diags}
		}
	}
	renderer := newFileProgramRenderer(prog, opts)
	comp, ok := renderer.components[component]
	if !ok {
		return "", false, fmt.Errorf("component %q not found", component)
	}
	if comp.Syntax == ir.ComponentSyntaxStrict && strings.TrimSpace(comp.PropsType) != "" {
		return "", false, fmt.Errorf("strict render entry %s accepts props %s, but the file renderer has no root props binding; use a zero-props Page/Layout entry", comp.Name, comp.PropsType)
	}
	// One builder carries the whole document. The renderer used to allocate a
	// fresh strings.Builder per element and let the parent copy the child's
	// string, which cost O(nodes x depth) byte traffic — a depth-100 page
	// allocated 1,085,241 B to emit 5,556 bytes.
	var b strings.Builder
	b.Grow(fileProgramRenderSizeHint(prog))
	renderer.writeNode(&b, comp.Root, opts.EvalEnv)
	if renderer.err != nil {
		return "", renderer.replaced, renderer.err
	}
	return b.String(), renderer.replaced, nil
}

// fileProgramRenderSizeHint estimates the output size from the node count so the
// top-level builder starts near its final capacity. A strings.Builder that grows
// from zero doubles its buffer, so it allocates about log2(N) times and copies
// about 2N bytes. The estimate stays static: it needs no per-program state, so
// it cannot retain a stale ir.Program after a hot reload.
func fileProgramRenderSizeHint(prog *ir.Program) int {
	const (
		bytesPerNode = 32
		minHint      = 256
		maxHint      = 64 << 10
	)
	size := len(prog.Nodes) * bytesPerNode
	if size < minHint {
		return minHint
	}
	if size > maxHint {
		return maxHint
	}
	return size
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

// writeNode appends one IR node's HTML to b.
//
// Every function on the render spine takes the shared builder instead of
// returning a string. Leaf builtins that must hand their children to a helper
// (Motion, Video, TextBlock, bound components) still call renderChildren, which
// allocates its own builder.
func (r *fileProgramRenderer) writeNode(b *strings.Builder, nodeID ir.NodeID, env fileRenderEnv) {
	node := r.prog.NodeAt(nodeID)
	if node == nil {
		return
	}
	switch node.Kind {
	case ir.NodeElement:
		r.writeElement(b, node, env)
	case ir.NodeComponent:
		r.writeComponent(b, node, env)
	case ir.NodeText:
		b.WriteString(html.EscapeString(node.Text))
	case ir.NodeExpr:
		b.WriteString(renderFileEvaluatedExpr(evalFileExpr(node.Text, env)))
	case ir.NodeFragment:
		r.writeChildren(b, node.Children, env)
	case ir.NodeRawHTML:
		b.WriteString(node.Text)
	}
}

func (r *fileProgramRenderer) writeElement(b *strings.Builder, node *ir.Node, env fileRenderEnv) {
	tag := html.EscapeString(node.Tag)
	isForm := strings.EqualFold(node.Tag, "form")
	formContract := fileAutoManagedFormContract(node.Attrs, env, isForm)
	// gosx#179: a <form data-gosx-managed> shorthand attribute expands the
	// same way here as it does through node.go's RenderHTML (Go Node API
	// path) and the island package's resolved-tree renderer — see
	// fileManagedFormShorthandTruthy and gosx.ManagedFormShorthandTruthy,
	// the shared truthy rule all three render paths call. Unlike the
	// method-based auto-detection above, the shorthand does not set a
	// mode: the HTML method attribute, if present, stays authoritative
	// for the navigation runtime.
	shorthandManaged := isForm && fileManagedFormShorthandTruthy(node.Attrs, env)
	if shorthandManaged {
		formContract.Managed = true
	}
	b.WriteByte('<')
	b.WriteString(tag)
	attrs := node.Attrs
	// excludeSpreadKey filters the shorthand key back out of a
	// {...extra}-supplied spread map, matching stripManagedFormShorthandAttr's
	// removal of a directly-written shorthand attribute above (gosx#179 F4).
	// Only set once the shorthand has actually expanded — an opted-out or
	// absent shorthand must stay visible in the output exactly as authored.
	excludeSpreadKey := ""
	if shorthandManaged {
		attrs = stripManagedFormShorthandAttr(attrs)
		excludeSpreadKey = gosx.ManagedFormShorthandAttr
	}
	if formContract.Managed {
		attrs = managedFormAttrs(attrs, formContract.Mode)
	}
	// gosx#185: a render profile's AttrWriter, when set, sees this same
	// fully-expanded attrs slice — after shorthand and managed-form
	// expansion, before renderAttrs' own escaping — and can rewrite, veto,
	// or append entries. No profile, or a profile with no AttrWriter, takes
	// the original renderAttrs path unchanged.
	if writer := r.profileAttrWriter(); writer != nil {
		r.renderAttrsWithProfile(b, node.Tag, attrs, env, excludeSpreadKey, writer)
	} else {
		r.renderAttrs(b, attrs, env, excludeSpreadKey)
	}
	r.writeManagedFormContract(b, node.Attrs, env, formContract)
	// Match node.go's renderNodeHTML: only self-close a void element that has
	// no children. The old branch dropped children silently.
	if ir.VoidElements[node.Tag] && len(node.Children) == 0 {
		b.WriteString(" />")
		return
	}
	b.WriteByte('>')
	r.writeChildren(b, node.Children, env)
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')
}

func (r *fileProgramRenderer) writeComponent(b *strings.Builder, node *ir.Node, env fileRenderEnv) {
	// Strict calls are type-checked as calls to the same-file declaration. Keep
	// that declaration authoritative even when its name collides with a layout
	// replacement or one of the legacy renderer builtins; otherwise generated Go
	// and file rendering would execute different components.
	if comp, ok := r.components[node.Tag]; ok && comp.Syntax == ir.ComponentSyntaxStrict {
		r.writeLocalComponent(b, comp, node, env)
		return
	}

	if replacement, ok := r.opts.ComponentReplacements[node.Tag]; ok {
		r.replaced = true
		if replacement != "" {
			b.WriteString(replacement)
			return
		}
		r.writeChildren(b, node.Children, env)
		return
	}

	if r.writeBuiltinComponent(b, node, env) {
		return
	}

	if comp, ok := r.components[node.Tag]; ok {
		switch {
		case comp.IsIsland:
			b.WriteString(r.renderLocalIsland(node.Tag, node, env))
			return
		case !comp.IsEngine:
			r.writeLocalComponent(b, comp, node, env)
			return
		}
	}

	if handled, out := r.renderBoundComponent(node, env); handled {
		b.WriteString(out)
		return
	}

	b.WriteString(defaultRenderedComponent(node.Tag, r.componentAttrMap(node.Attrs, env), r.renderChildren(node.Children, env)))
}

func (r *fileProgramRenderer) writeBuiltinComponent(b *strings.Builder, node *ir.Node, env fileRenderEnv) bool {
	switch node.Tag {
	case "If", "Show", "When":
		r.writeConditional(b, node, env)
	case "Each", "For":
		r.writeEach(b, node, env)
	case "Link":
		r.writeLink(b, node, env)
	case "Form":
		r.writeManagedForm(b, node, env, managedFormOptions{})
	case "ActionForm":
		r.writeManagedForm(b, node, env, managedFormOptions{
			defaultMethod: strings.ToLower(http.MethodPost),
			defaultAction: fileRenderActionPath(env, stringValue(attrValue(node.Attrs, env, "actionName"))),
		})
	case "Image":
		b.WriteString(r.renderImage(node, env))
	case "Motion":
		b.WriteString(r.renderMotion(node, env))
	case "Video":
		b.WriteString(r.renderVideo(node, env))
	case "TextBlock":
		b.WriteString(r.renderTextBlock(node, env))
	case "Stylesheet":
		b.WriteString(r.renderStylesheet(node, env))
	case "Surface":
		b.WriteString(r.renderSurface(node, env))
	case "Worker":
		b.WriteString(r.renderWorker(node, env))
	case "Scene3D":
		b.WriteString(r.renderScene3D(node, env))
	default:
		return false
	}
	return true
}

func (r *fileProgramRenderer) writeConditional(b *strings.Builder, node *ir.Node, env fileRenderEnv) {
	condition := attrValue(node.Attrs, env, "when", "if", "cond", "test")
	if truthy(condition) {
		r.writeChildren(b, node.Children, env)
		return
	}
	fallback := attrValue(node.Attrs, env, "fallback", "else")
	b.WriteString(renderFileEvaluatedExpr(fallback))
}

func (r *fileProgramRenderer) writeEach(b *strings.Builder, node *ir.Node, env fileRenderEnv) {
	collection := attrValue(node.Attrs, env, "of", "each", "items")
	if collection == nil {
		return
	}

	itemName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "as", "item")))
	if itemName == "" {
		itemName = "item"
	}
	indexName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "index")))

	items := fileEachEntries(collection)
	if len(items) == 0 {
		fallback := attrValue(node.Attrs, env, "fallback", "empty")
		b.WriteString(renderFileEvaluatedExpr(fallback))
		return
	}

	// Build the key binding name once instead of once per item, and take every
	// per-item binding from one arena.
	keyName := itemName + "Key"
	arena := make(fileScopeArena, 0, len(items)*fileEachBindingsPerItem(items, indexName))
	for _, entry := range items {
		scope := arena.push(env.scope, itemName, entry.Value)
		if indexName != "" {
			scope = arena.push(scope, indexName, entry.Index)
		}
		if entry.Key != nil {
			scope = arena.push(scope, keyName, entry.Key)
		}
		r.writeChildren(b, node.Children, env.withScope(scope))
	}
}

// fileEachBindingsPerItem sizes the loop scope arena. It is a capacity hint, so
// an inexact answer stays correct.
func fileEachBindingsPerItem(items []fileEachEntry, indexName string) int {
	count := 1
	if indexName != "" {
		count++
	}
	if len(items) > 0 && items[0].Key != nil {
		count++
	}
	return count
}

func (r *fileProgramRenderer) writeLink(b *strings.Builder, node *ir.Node, env fileRenderEnv) {
	b.WriteString("<a")
	contract := fileManagedLinkContractForAttrs(node.Attrs, env)
	r.renderLinkAttrs(b, node.Attrs, env)
	r.writeManagedLinkContract(b, node.Attrs, env, contract)
	b.WriteByte('>')
	r.writeChildren(b, node.Children, env)
	b.WriteString("</a>")
}

func (r *fileProgramRenderer) renderLinkAttrs(b *strings.Builder, attrs []ir.Attr, env fileRenderEnv) {
	for _, attr := range attrs {
		if linkReservedAttr(attr.Name) {
			continue
		}
		switch attr.Kind {
		case ir.AttrStatic:
			writeFileAttrPair(b, html.EscapeString(normalizeFileAttrName(attr.Name)), html.EscapeString(attr.Value))
		case ir.AttrExpr:
			renderFileEvaluatedAttr(b, normalizeFileAttrName(attr.Name), evalFileExpr(attr.Expr, env))
		case ir.AttrBool:
			writeFileAttrName(b, html.EscapeString(normalizeFileAttrName(attr.Name)))
		case ir.AttrSpread:
			for _, entry := range sortedSpreadProps(evalFileExpr(attr.Expr, env)) {
				key := entry.Key
				value := entry.Value
				normalized := normalizeFileAttrName(key)
				if normalized == "" || linkReservedAttr(normalized) {
					continue
				}
				renderFileEvaluatedAttr(b, normalized, value)
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
	pageBinding, _ := env.lookupValue("page")
	if pageValue, ok := pageBinding.(map[string]any); ok {
		if current := strings.TrimSpace(stringValue(pageValue["path"])); current != "" {
			return current
		}
	}
	requestBinding, _ := env.lookupValue("request")
	if requestValue, ok := requestBinding.(map[string]any); ok {
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

func (r *fileProgramRenderer) writeManagedForm(b *strings.Builder, node *ir.Node, env fileRenderEnv, opts managedFormOptions) {
	contract := fileBuiltinManagedFormContract(node.Attrs, env, opts.defaultMethod)
	b.WriteString("<form")
	if method := strings.TrimSpace(opts.defaultMethod); method != "" && attrValue(node.Attrs, env, "method") == nil {
		fmt.Fprintf(b, ` method="%s"`, html.EscapeString(method))
	}
	if action := strings.TrimSpace(opts.defaultAction); action != "" && attrValue(node.Attrs, env, "action") == nil {
		fmt.Fprintf(b, ` action="%s"`, html.EscapeString(action))
	}
	// The <Form>/<ActionForm> builtins are always managed, so an author-
	// written data-gosx-managed shorthand alongside them is always noise —
	// strip it (both a directly-written and a {...extra}-supplied copy)
	// instead of rendering it beside the full contract it did nothing to
	// produce (gosx#179 F9).
	attrs := stripManagedFormShorthandAttr(node.Attrs)
	r.renderAttrs(b, managedFormAttrs(attrs, contract.Mode), env, gosx.ManagedFormShorthandAttr)
	r.writeManagedFormContract(b, node.Attrs, env, contract)
	b.WriteByte('>')
	r.writeChildren(b, node.Children, env)
	b.WriteString("</form>")
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
		SyncTuning:    videoSyncTuningValue(firstNonEmptyValue(attrValue(node.Attrs, env, "syncTuning"), attrValue(node.Attrs, env, "sync_tuning"))),
		HLS:           mapStringAnyValue(attrValue(node.Attrs, env, "hls")),
		HLSConfig:     mapStringAnyValue(attrValue(node.Attrs, env, "hlsConfig", "hls_config")),
		AudioTrack:    firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "audioTrack")), stringValue(attrValue(node.Attrs, env, "audio_track"))),
		AudioTracks:   videoAudioTrackListValue(firstNonEmptyValue(attrValue(node.Attrs, env, "audioTracks"), attrValue(node.Attrs, env, "audio_tracks"))),
		AudioSource:   videoAudioSourceOptionsValue(firstNonEmptyValue(attrValue(node.Attrs, env, "audioSource"), attrValue(node.Attrs, env, "audio_source"))),
		SubtitleBase:  firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "subtitleBase")), stringValue(attrValue(node.Attrs, env, "subtitle_base"))),
		SubtitleTrack: firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "subtitleTrack")), stringValue(attrValue(node.Attrs, env, "subtitle_track"))),
		SubtitleTracks: videoTrackListValue(firstNonEmptyValue(
			attrValue(node.Attrs, env, "subtitleTracks"),
			attrValue(node.Attrs, env, "subtitle_tracks"),
			attrValue(node.Attrs, env, "tracks"),
		)),
		Subtitles:    videoSubtitleOptionsValue(firstNonEmptyValue(attrValue(node.Attrs, env, "subtitles"), attrValue(node.Attrs, env, "subtitleOptions"), attrValue(node.Attrs, env, "subtitle_options"))),
		Fullscreen:   videoFullscreenOptionsValue(firstNonEmptyValue(attrValue(node.Attrs, env, "fullscreen"), attrValue(node.Attrs, env, "fullscreenOptions"), attrValue(node.Attrs, env, "fullscreen_options"))),
		Telemetry:    videoTelemetryOptionsValue(firstNonEmptyValue(attrValue(node.Attrs, env, "telemetry"), attrValue(node.Attrs, env, "videoTelemetry"), attrValue(node.Attrs, env, "video_telemetry"))),
		PersistPrefs: truthy(attrValue(node.Attrs, env, "persistPrefs", "persist_prefs")),
		PersistKey:   firstNonEmptyString(stringValue(attrValue(node.Attrs, env, "persistKey")), stringValue(attrValue(node.Attrs, env, "persist_key"))),
		LockInput:    truthy(attrValue(node.Attrs, env, "lockInput", "lock_input")),
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
		"syncTuning", "sync_tuning", "SyncTuning",
		"hls", "hlsConfig", "hls_config",
		"audioTrack", "audio_track",
		"audioTracks", "audio_tracks",
		"audioSource", "audio_source", "AudioSource",
		"subtitleBase", "subtitle_base",
		"subtitleTrack", "subtitle_track",
		"subtitleTracks", "subtitle_tracks",
		"tracks",
		"subtitles", "subtitleOptions", "subtitle_options", "Subtitles",
		"fullscreen", "fullscreenOptions", "fullscreen_options", "Fullscreen",
		"telemetry", "videoTelemetry", "video_telemetry", "Telemetry",
		"persistPrefs", "persist_prefs", "PersistPrefs",
		"persistKey", "persist_key", "PersistKey",
		"lockInput", "lock_input", "LockInput",
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
	Name                 string
	WASMPath             string
	Capabilities         []engine.Capability
	RequiredCapabilities []engine.Capability
	Runtime              engine.Runtime
	MountAttrs           map[string]any
}

type fileEngineTransport struct {
	Name                 any
	WASMPath             any
	MountID              any
	Capabilities         any
	RequiredCapabilities any
	Runtime              any
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
			engine.CapWebGPU,
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
		cfg.Props = normalizeScene3DScalarProps(cfg.Props)
		cfg.Props = r.applyScene3DComposableChildren(cfg.Props, node, env)
		cfg.Props = defaultScene3DProps(cfg.Props, cfg.WASMPath)
		cfg.Props = r.applyScene3DStyles(cfg.Props, node, env)
		cfg.RequiredCapabilities = scene3DRequiredCapabilities(cfg.Props, cfg.RequiredCapabilities)
		if err := validateScene3DCompilerCapabilities(cfg.Props, mergedEngineCapabilities(cfg.Capabilities, cfg.RequiredCapabilities)); err != nil {
			return r.renderError(err)
		}
	}
	return gosx.RenderHTML(env.engine(cfg, fallback))
}

func normalizeScene3DScalarProps(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	props := map[string]any{}
	if err := json.Unmarshal(raw, &props); err != nil {
		return raw
	}
	if _, exists := props["controlTarget"]; !exists {
		_, hasX := props["controlTargetX"]
		_, hasY := props["controlTargetY"]
		_, hasZ := props["controlTargetZ"]
		if hasX || hasY || hasZ {
			props["controlTarget"] = map[string]any{
				"x": numericValue(props["controlTargetX"]),
				"y": numericValue(props["controlTargetY"]),
				"z": numericValue(props["controlTargetZ"]),
			}
		}
	}
	delete(props, "controlTargetX")
	delete(props, "controlTargetY")
	delete(props, "controlTargetZ")
	encoded, err := json.Marshal(props)
	if err != nil {
		return raw
	}
	return encoded
}

func (r *fileProgramRenderer) renderError(err error) string {
	if err != nil && r.err == nil {
		r.err = err
	}
	return ""
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
		childrenHTML := strings.TrimSpace(r.renderEngineFallbackChildren(node, env, name))
		if childrenHTML != "" {
			fallback = gosx.RawHTML(childrenHTML)
		}
	}
	return cfg, fallback
}

func (r *fileProgramRenderer) renderEngineFallbackChildren(node *ir.Node, env fileRenderEnv, engineName string) string {
	if engineName != "GoSXScene3D" {
		return r.renderChildren(node.Children, env)
	}
	return r.renderScene3DFallbackChildren(node.Children, env)
}

// renderScene3DFallbackChildren renders the DOM fallback for a Scene3D subtree.
// It skips composable scene tags, which lower into the engine props instead.
// The caller trims the result and wraps it in gosx.RawHTML, so this walk keeps
// its own builder.
func (r *fileProgramRenderer) renderScene3DFallbackChildren(children []ir.NodeID, env fileRenderEnv) string {
	var b strings.Builder
	r.writeScene3DFallbackChildren(&b, children, env)
	return b.String()
}

func (r *fileProgramRenderer) writeScene3DFallbackChildren(b *strings.Builder, children []ir.NodeID, env fileRenderEnv) {
	for _, childID := range children {
		child := r.prog.NodeAt(childID)
		if child == nil {
			continue
		}
		if child.Kind == ir.NodeComponent {
			switch child.Tag {
			case "Each", "For":
				r.writeScene3DFallbackEach(b, child, env)
				continue
			case "If", "Show", "When":
				r.writeScene3DFallbackConditional(b, child, env)
				continue
			}
			if isScene3DComposableTag(child.Tag) {
				continue
			}
		}
		r.writeNode(b, childID, env)
	}
}

func (r *fileProgramRenderer) writeScene3DFallbackConditional(b *strings.Builder, node *ir.Node, env fileRenderEnv) {
	condition := attrValue(node.Attrs, env, "when", "if", "cond", "test")
	if truthy(condition) {
		r.writeScene3DFallbackChildren(b, node.Children, env)
		return
	}
	fallback := attrValue(node.Attrs, env, "fallback", "else")
	b.WriteString(renderFileEvaluatedExpr(fallback))
}

func (r *fileProgramRenderer) writeScene3DFallbackEach(b *strings.Builder, node *ir.Node, env fileRenderEnv) {
	collection := attrValue(node.Attrs, env, "of", "each", "items")
	if collection == nil {
		return
	}

	itemName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "as", "item")))
	if itemName == "" {
		itemName = "item"
	}
	indexName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "index")))

	items := fileEachEntries(collection)
	if len(items) == 0 {
		fallback := attrValue(node.Attrs, env, "fallback", "empty")
		b.WriteString(renderFileEvaluatedExpr(fallback))
		return
	}

	keyName := itemName + "Key"
	arena := make(fileScopeArena, 0, len(items)*fileEachBindingsPerItem(items, indexName))
	for _, entry := range items {
		scope := arena.push(env.scope, itemName, entry.Value)
		if indexName != "" {
			scope = arena.push(scope, indexName, entry.Index)
		}
		if entry.Key != nil {
			scope = arena.push(scope, keyName, entry.Key)
		}
		r.writeScene3DFallbackChildren(b, node.Children, env.withScope(scope))
	}
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
		RequiredCapabilities: engineCapabilitiesValue(firstNonEmptyValue(
			attrValue(attrs, env, "requiredCapabilities", "requireCapabilities", "requires"),
			transport.RequiredCapabilities,
		), defaults.RequiredCapabilities),
		Runtime: engine.Runtime(firstNonEmptyString(stringValue(attrValue(attrs, env, "runtime")), stringValue(transport.Runtime), string(defaults.Runtime))),
	}
}

func splitEngineTransportProps(props map[string]any) (map[string]any, fileEngineTransport) {
	if len(props) == 0 {
		return props, fileEngineTransport{}
	}

	clean := cloneSpreadProps(props)
	transport := fileEngineTransport{
		Name:                 extractEngineTransportValue(clean, "name", "component"),
		WASMPath:             extractEngineTransportValue(clean, "wasmPath", "wasm", "programRef", "program"),
		MountID:              extractEngineTransportValue(clean, "mountId", "id"),
		Capabilities:         extractEngineTransportValue(clean, "capabilities"),
		RequiredCapabilities: extractEngineTransportValue(clean, "requiredCapabilities", "requireCapabilities", "requires"),
		Runtime:              extractEngineTransportValue(clean, "runtime"),
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

func (r *fileProgramRenderer) writeLocalComponent(b *strings.Builder, comp *ir.Component, node *ir.Node, env fileRenderEnv) {
	// Children render into their own string because the component body may
	// reference them through the `children` binding.
	childrenNode := gosx.RawHTML(r.renderChildren(node.Children, env))
	props, err := localComponentProps(comp, node.Attrs, env, childrenNode)
	if err != nil {
		r.err = fmt.Errorf("render strict component %s: %w", comp.Name, err)
		return
	}
	scope := env.withValue("props", props)
	scope = scope.withValue("children", childrenNode)
	r.writeNode(b, comp.Root, scope)
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
	if comp := r.components[name]; comp != nil && comp.Syntax == ir.ComponentSyntaxStrict {
		converted, err := localComponentProps(comp, node.Attrs, env, gosx.Node{})
		if err != nil {
			r.err = fmt.Errorf("render strict island %s: %w", name, err)
			return ""
		}
		props = converted
	}
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

func (r *fileProgramRenderer) writeChildren(b *strings.Builder, children []ir.NodeID, env fileRenderEnv) {
	for _, child := range children {
		r.writeNode(b, child, env)
	}
}

// renderChildren renders children into their own string. Use writeChildren on
// the render spine; use this only when a helper needs the child HTML as a value.
func (r *fileProgramRenderer) renderChildren(children []ir.NodeID, env fileRenderEnv) string {
	if len(children) == 0 {
		return ""
	}
	var b strings.Builder
	r.writeChildren(&b, children, env)
	return b.String()
}

// renderAttrs writes attrs as HTML attribute text. excludeSpreadKey, when
// non-empty, drops a matching key out of any {...spread} attribute's
// evaluated map before it renders (gosx#179 F4) — used to keep an already-
// expanded data-gosx-managed shorthand from surviving into the output when
// it was only supplied through a spread instead of written directly.
func (r *fileProgramRenderer) renderAttrs(b *strings.Builder, attrs []ir.Attr, env fileRenderEnv, excludeSpreadKey string) {
	for _, attr := range attrs {
		renderFileAttr(b, attr, env, excludeSpreadKey)
	}
}

// profileAttrWriter returns the active render profile's AttrWriter, or nil
// when there is no profile or the profile leaves AttrWriter unset. writeElement
// uses a nil return to fall through to the original, profile-unaware
// renderAttrs path — the byte-identical-with-nil-profile guarantee holds for
// an empty *RenderProfile{} too, not only for a nil *RenderProfile.
func (r *fileProgramRenderer) profileAttrWriter() AttrWriter {
	if r.opts.Profile == nil {
		return nil
	}
	return r.opts.Profile.AttrWriter
}

// renderAttrsWithProfile resolves attrs the same way renderAttrs's
// per-attribute helpers do — {expr} evaluation, {...spread} expansion and
// flattening, and the excludeSpreadKey filter all run first — then hands the
// fully resolved, unescaped list to writer before emitting. renderResolvedAttrs
// is the only place a RenderAttr produced by writer reaches output, and it
// escapes every Name and Value unconditionally, so writer cannot inject
// unescaped HTML through a returned value.
func (r *fileProgramRenderer) renderAttrsWithProfile(b *strings.Builder, tag string, attrs []ir.Attr, env fileRenderEnv, excludeSpreadKey string, writer AttrWriter) {
	resolved := resolveFileAttrs(attrs, env, excludeSpreadKey)
	renderResolvedAttrs(b, writer(tag, resolved))
}

// resolveFileAttrs evaluates and flattens attrs into RenderAttr values,
// mirroring renderFileAttr/renderFileSpreadAttrs' coercion rules exactly but
// building a slice instead of writing bytes, so a RenderProfile's AttrWriter
// can inspect and rewrite the list before anything is escaped or emitted.
func resolveFileAttrs(attrs []ir.Attr, env fileRenderEnv, excludeSpreadKey string) []RenderAttr {
	out := make([]RenderAttr, 0, len(attrs))
	for _, attr := range attrs {
		switch attr.Kind {
		case ir.AttrStatic:
			out = append(out, RenderAttr{Name: attr.Name, Value: attr.Value})
		case ir.AttrExpr:
			out = appendResolvedAttr(out, attr.Name, evalFileExpr(attr.Expr, env))
		case ir.AttrBool:
			out = append(out, RenderAttr{Name: attr.Name, Boolean: true})
		case ir.AttrSpread:
			for _, entry := range sortedSpreadProps(evalFileExpr(attr.Expr, env)) {
				normalized := normalizeFileAttrName(entry.Key)
				if normalized == "" || normalized == excludeSpreadKey {
					continue
				}
				out = appendResolvedAttr(out, normalized, entry.Value)
			}
		}
	}
	return out
}

// appendResolvedAttr coerces one evaluated attribute value into a RenderAttr
// using the same rules renderFileEvaluatedAttr applies when writing directly:
// nil drops the attribute, a bool becomes a presence-only Boolean attribute
// when the name uses HTML boolean semantics (htmlattr.IsBoolean) and
// "true"/"false" text otherwise, and every other value takes the same
// scalar/Stringer/fmt.Sprint fallback chain the non-profile path uses.
func appendResolvedAttr(out []RenderAttr, name string, value any) []RenderAttr {
	switch v := value.(type) {
	case nil:
		return out
	case bool:
		if htmlattr.IsBoolean(name) {
			if v {
				return append(out, RenderAttr{Name: name, Boolean: true})
			}
			return out
		}
		return append(out, RenderAttr{Name: name, Value: strconv.FormatBool(v)})
	case fmt.Stringer:
		return append(out, RenderAttr{Name: name, Value: v.String()})
	default:
		if text, ok := fileScalarText(value); ok {
			return append(out, RenderAttr{Name: name, Value: text})
		}
		return append(out, RenderAttr{Name: name, Value: fmt.Sprint(v)})
	}
}

// renderResolvedAttrs emits attrs as HTML attribute text, escaping every
// Name and Value unconditionally. See RenderAttr and RenderProfile.AttrWriter
// for the escape-after-the-hook guarantee this function completes.
func renderResolvedAttrs(b *strings.Builder, attrs []RenderAttr) {
	for _, attr := range attrs {
		name := html.EscapeString(attr.Name)
		if attr.Boolean {
			writeFileAttrName(b, name)
			continue
		}
		writeFileAttrPair(b, name, html.EscapeString(attr.Value))
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

// writeFileAttrPair appends ` name="value"` without fmt.
//
// WHY: fmt.Fprintf boxes both arguments into an []any and runs the printer, so
// it cost two to three allocations per attribute. After the shared-builder
// change, renderFileAttr held 48.5% of the remaining allocated objects on a
// depth-100 page. Direct writes cost none.
func writeFileAttrPair(b *strings.Builder, name, value string) {
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString(`="`)
	b.WriteString(value)
	b.WriteByte('"')
}

func writeFileAttrName(b *strings.Builder, name string) {
	b.WriteByte(' ')
	b.WriteString(name)
}

func renderFileAttr(b *strings.Builder, attr ir.Attr, env fileRenderEnv, excludeSpreadKey string) {
	name := html.EscapeString(attr.Name)
	switch attr.Kind {
	case ir.AttrStatic:
		writeFileAttrPair(b, name, html.EscapeString(attr.Value))
	case ir.AttrExpr:
		renderFileEvaluatedAttr(b, attr.Name, evalFileExpr(attr.Expr, env))
	case ir.AttrBool:
		writeFileAttrName(b, name)
	case ir.AttrSpread:
		renderFileSpreadAttrs(b, evalFileExpr(attr.Expr, env), excludeSpreadKey)
	}
}

func renderFileSpreadAttrs(b *strings.Builder, value any, excludeKey string) {
	for _, entry := range sortedSpreadProps(value) {
		normalized := normalizeFileAttrName(entry.Key)
		if normalized == "" || normalized == excludeKey {
			continue
		}
		renderFileEvaluatedAttr(b, normalized, entry.Value)
	}
}

// fileScalarText formats a scalar without fmt. It reports false for values that
// still need the reflect-based fmt path.
//
// WHY: fmt.Sprint boxes the value and runs the printer, which cost two
// allocations per hole. The float64 case matters most — evalFileExpr returns
// float64 for every arithmetic result, and fmt.Sprint held 11.1% of the
// remaining allocated objects on a 100-item page. `%v` on a float64 equals
// strconv.FormatFloat(v, 'g', -1, 64), including +Inf, -Inf and NaN.
func fileScalarText(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return "", false
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
	}
	if text, ok := fileScalarText(value); ok {
		return html.EscapeString(text)
	}
	return html.EscapeString(fmt.Sprint(value))
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
	safeName := html.EscapeString(name)
	switch v := value.(type) {
	case nil:
		return
	case bool:
		if htmlattr.IsBoolean(name) {
			if v {
				writeFileAttrName(b, safeName)
			}
			return
		}
		writeFileAttrPair(b, safeName, strconv.FormatBool(v))
	case fmt.Stringer:
		writeFileAttrPair(b, safeName, html.EscapeString(v.String()))
	default:
		if text, ok := fileScalarText(value); ok {
			writeFileAttrPair(b, safeName, html.EscapeString(text))
			return
		}
		writeFileAttrPair(b, safeName, html.EscapeString(fmt.Sprint(v)))
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

func localComponentProps(comp *ir.Component, attrs []ir.Attr, env fileRenderEnv, children gosx.Node) (map[string]any, error) {
	if comp == nil || comp.Syntax != ir.ComponentSyntaxStrict || len(comp.PropsFields) == 0 {
		return componentProps(attrs, env, children), nil
	}
	props := make(map[string]any, len(attrs)+4)
	for _, attr := range attrs {
		fieldType, rendered := comp.PropsFields[attr.Name]
		var value any
		if rendered {
			converted, err := strictComponentAttrValue(comp, attr, env, fieldType)
			if err != nil {
				return nil, fmt.Errorf("prop %s (%s): %w", attr.Name, fieldType, err)
			}
			value = converted
		} else {
			value = attrValue([]ir.Attr{attr}, env, attr.Name)
		}
		setComponentProp(props, attr.Name, value)
	}
	if !children.IsZero() {
		setComponentProp(props, "children", children)
		setComponentProp(props, "Children", children)
	}
	return props, nil
}

func strictComponentAttrValue(comp *ir.Component, attr ir.Attr, env fileRenderEnv, fieldType string) (any, error) {
	if !strictScalarFieldType(fieldType) {
		// A rendered field whose declared type is not one of the renderer's
		// exact scalar builtins is a nested-selector root (ir.Component.
		// PropsPaths): a same-file struct, provable at every strict-call
		// boundary but not renderable directly (ir/lower.go's
		// validateStrictRenderedProps only ever admits a struct-typed root
		// when at least one nested path under it resolves to a scalar leaf).
		return strictComponentStructAttrValue(comp, attr, env, fieldType)
	}
	var value any
	switch attr.Kind {
	case ir.AttrStatic:
		value = attr.Value
	case ir.AttrBool:
		value = true
	case ir.AttrExpr:
		if literal, ok := strictGoConstant(attr.Expr); ok {
			return convertStrictConstant(literal, fieldType)
		}
		value = evalFileExpr(attr.Expr, env)
	case ir.AttrSpread:
		return nil, fmt.Errorf("spread attributes are not supported")
	}
	return requireStrictScalarType(value, fieldType)
}

// strictScalarFieldType reports whether fieldType is one of the renderer's
// exact scalar builtins. Duplicated from ir.strictRendererScalarType (an
// unexported ir-package function route cannot import) rather than exported,
// since it is a small, stable, closed set and this is its only other
// reference point.
func strictScalarFieldType(fieldType string) bool {
	switch fieldType {
	case "string", "bool",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64":
		return true
	default:
		return false
	}
}

// strictComponentStructAttrValue handles a rendered prop whose declared type
// is a same-file struct. A struct value can only arrive through an AttrExpr
// — there is no static or boolean spelling for one — so every other
// attribute kind fails closed with the same shape of error a type mismatch
// would produce.
func strictComponentStructAttrValue(comp *ir.Component, attr ir.Attr, env fileRenderEnv, fieldType string) (any, error) {
	if attr.Kind != ir.AttrExpr {
		return nil, fmt.Errorf("value has no struct spelling, want exact struct %s", fieldType)
	}
	value := evalFileExpr(attr.Expr, env)
	return requireStrictStructValue(value, fieldType, strictStructPropsPaths(comp, attr.Name))
}

// strictStructPropsPaths filters comp.PropsPaths — keyed by the full
// dot-joined path including its root, e.g. "Player.Name" — down to the
// sub-paths under one root field, with the root segment removed (e.g.
// "Name"), for requireStrictStructValue to walk.
func strictStructPropsPaths(comp *ir.Component, root string) map[string]string {
	if comp == nil || len(comp.PropsPaths) == 0 {
		return nil
	}
	prefix := root + "."
	var out map[string]string
	for path, leafType := range comp.PropsPaths {
		if sub, ok := strings.CutPrefix(path, prefix); ok {
			if out == nil {
				out = make(map[string]string, len(comp.PropsPaths))
			}
			out[sub] = leafType
		}
	}
	return out
}

// requireStrictStructValue is requireStrictScalarType's counterpart for a
// nested-selector root: value must be exactly the same-file declared struct
// type typeName (not a pointer, not an anonymous struct, not a map), and
// every read path this component resolves under that root (paths, keyed by
// the dot-joined sub-path with the root segment removed) must resolve by
// FieldByName to a value of its declared leaf type. A mismatch anywhere
// fails closed; the caller (strictComponentAttrValue, through
// localComponentProps) wraps the error with the attribute name and field
// type, matching the scalar boundary's existing error shape.
func requireStrictStructValue(value any, typeName string, paths map[string]string) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("value is nil")
	}
	rv := reflect.ValueOf(value)
	rt := rv.Type()
	if rt.Kind() != reflect.Struct || rt.PkgPath() == "" || rt.Name() != typeName {
		return nil, fmt.Errorf("runtime value has type %s, want exact struct %s", rt, typeName)
	}
	for subPath, leafType := range paths {
		fv := rv
		for _, segment := range strings.Split(subPath, ".") {
			if fv.Kind() != reflect.Struct {
				return nil, fmt.Errorf("path %s.%s: value has type %s, want struct", typeName, subPath, fv.Type())
			}
			field := fv.FieldByName(segment)
			if !field.IsValid() || !field.CanInterface() {
				return nil, fmt.Errorf("path %s.%s: field %s not found", typeName, subPath, segment)
			}
			fv = field
		}
		if _, err := requireStrictScalarType(fv.Interface(), leafType); err != nil {
			return nil, fmt.Errorf("path %s.%s: %w", typeName, subPath, err)
		}
	}
	return value, nil
}

func strictGoConstant(source string) (constant.Value, bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false
	}
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	switch node := expr.(type) {
	case *ast.BasicLit:
		value := constant.MakeFromLiteral(node.Value, node.Kind, 0)
		return value, value.Kind() != constant.Unknown
	case *ast.Ident:
		switch node.Name {
		case "true":
			return constant.MakeBool(true), true
		case "false":
			return constant.MakeBool(false), true
		}
	}
	return nil, false
}

func convertStrictConstant(value constant.Value, fieldType string) (any, error) {
	switch fieldType {
	case "string":
		if value.Kind() != constant.String {
			return nil, fmt.Errorf("constant is %s, not string", value.Kind())
		}
		return constant.StringVal(value), nil
	case "bool":
		if value.Kind() != constant.Bool {
			return nil, fmt.Errorf("constant is %s, not bool", value.Kind())
		}
		return constant.BoolVal(value), nil
	case "float32":
		if value.Kind() != constant.Int && value.Kind() != constant.Float {
			return nil, fmt.Errorf("constant is %s, not numeric", value.Kind())
		}
		converted, _ := constant.Float32Val(value)
		if math.IsInf(float64(converted), 0) {
			return nil, fmt.Errorf("constant overflows float32")
		}
		return converted, nil
	case "float64":
		if value.Kind() != constant.Int && value.Kind() != constant.Float {
			return nil, fmt.Errorf("constant is %s, not numeric", value.Kind())
		}
		converted, _ := constant.Float64Val(value)
		if math.IsInf(converted, 0) {
			return nil, fmt.Errorf("constant overflows float64")
		}
		return converted, nil
	}

	integer := constant.ToInt(value)
	if integer.Kind() == constant.Unknown {
		return nil, fmt.Errorf("constant is not an exact integer")
	}
	switch fieldType {
	case "int":
		value, ok := constant.Int64Val(integer)
		if !ok || (strconv.IntSize == 32 && (value < math.MinInt32 || value > math.MaxInt32)) {
			return nil, fmt.Errorf("constant overflows int")
		}
		return int(value), nil
	case "int8":
		value, ok := constant.Int64Val(integer)
		if !ok || value < math.MinInt8 || value > math.MaxInt8 {
			return nil, fmt.Errorf("constant overflows int8")
		}
		return int8(value), nil
	case "int16":
		value, ok := constant.Int64Val(integer)
		if !ok || value < math.MinInt16 || value > math.MaxInt16 {
			return nil, fmt.Errorf("constant overflows int16")
		}
		return int16(value), nil
	case "int32", "rune":
		value, ok := constant.Int64Val(integer)
		if !ok || value < math.MinInt32 || value > math.MaxInt32 {
			return nil, fmt.Errorf("constant overflows %s", fieldType)
		}
		return int32(value), nil
	case "int64":
		value, ok := constant.Int64Val(integer)
		if !ok {
			return nil, fmt.Errorf("constant overflows int64")
		}
		return value, nil
	case "uint":
		value, ok := constant.Uint64Val(integer)
		if !ok || (strconv.IntSize == 32 && value > math.MaxUint32) {
			return nil, fmt.Errorf("constant overflows uint")
		}
		return uint(value), nil
	case "uint8", "byte":
		value, ok := constant.Uint64Val(integer)
		if !ok || value > math.MaxUint8 {
			return nil, fmt.Errorf("constant overflows %s", fieldType)
		}
		return uint8(value), nil
	case "uint16":
		value, ok := constant.Uint64Val(integer)
		if !ok || value > math.MaxUint16 {
			return nil, fmt.Errorf("constant overflows uint16")
		}
		return uint16(value), nil
	case "uint32":
		value, ok := constant.Uint64Val(integer)
		if !ok || value > math.MaxUint32 {
			return nil, fmt.Errorf("constant overflows uint32")
		}
		return uint32(value), nil
	case "uint64":
		value, ok := constant.Uint64Val(integer)
		if !ok {
			return nil, fmt.Errorf("constant overflows uint64")
		}
		return value, nil
	case "uintptr":
		value, ok := constant.Uint64Val(integer)
		if !ok || (strconv.IntSize == 32 && value > math.MaxUint32) {
			return nil, fmt.Errorf("constant overflows uintptr")
		}
		return uintptr(value), nil
	default:
		return nil, fmt.Errorf("type %s is not renderer-safe", fieldType)
	}
}

func requireStrictScalarType(value any, fieldType string) (any, error) {
	want := fieldType
	switch want {
	case "byte":
		want = "uint8"
	case "rune":
		want = "int32"
	}
	if value == nil {
		return nil, fmt.Errorf("value is nil")
	}
	actual := reflect.TypeOf(value)
	if actual.PkgPath() != "" || actual.Name() != want {
		return nil, fmt.Errorf("runtime value has type %s, want exact %s", actual, fieldType)
	}
	return value, nil
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

// managedFormAttrs drops the attributes the managed-form contract writes
// itself so they are not duplicated: actionName (an ActionForm prop, never
// real HTML) always, and server.NavigationFormModeAttr only when the
// contract computed its own mode (contractMode != ""), because that mode
// is about to be written by writeManagedFormContract. When contractMode is
// "" — the common case for the data-gosx-managed shorthand, which never
// computes a mode of its own (gosx#179 F1) — an author-written
// data-gosx-form-mode is left in attrs untouched, so it survives into the
// output instead of silently disappearing.
func managedFormAttrs(attrs []ir.Attr, contractMode string) []ir.Attr {
	out := make([]ir.Attr, 0, len(attrs))
	for _, attr := range attrs {
		switch strings.TrimSpace(attr.Name) {
		case "actionName":
			continue
		case server.NavigationFormModeAttr:
			if contractMode != "" {
				continue
			}
		}
		out = append(out, attr)
	}
	return out
}

// fileManagedFormShorthandTruthy reports whether attrs carries a truthy
// gosx.ManagedFormShorthandAttr (data-gosx-managed). attrValue already
// resolves a static value, a dynamic {expr} attribute expression, an
// AttrBool presence attribute, and a spread attribute to the same "value
// present or not" shape, so this covers every way the shorthand can appear
// in a .gsx template. attrValue returns nil when the attribute is absent,
// or when a dynamic expression evaluated to nil; gosx.ManagedFormShorthandTruthy
// treats a nil value as "not present" and returns false — the same rule
// node.go and the island renderer apply, so this function delegates the
// whole judgment (including the nil case) to the one shared definition
// instead of special-casing nil here too.
func fileManagedFormShorthandTruthy(attrs []ir.Attr, env fileRenderEnv) bool {
	return gosx.ManagedFormShorthandTruthy(false, attrValue(attrs, env, gosx.ManagedFormShorthandAttr))
}

// stripManagedFormShorthandAttr removes the data-gosx-managed attribute
// from attrs. Called only once the shorthand has expanded, matching
// node.go's rule that the shorthand attribute itself does not survive into
// the rendered output.
func stripManagedFormShorthandAttr(attrs []ir.Attr) []ir.Attr {
	out := make([]ir.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Name == gosx.ManagedFormShorthandAttr {
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
		if htmlattr.IsBoolean(name) {
			if !v {
				return nil, false
			}
			return gosx.BoolAttr(name), true
		}
		return gosx.Attr(name, v), true
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
	case "name", "component", "kind", "wasmPath", "wasm", "programRef", "program", "mountId", "capabilities", "requiredCapabilities", "requireCapabilities", "requires", "runtime", "props", "id":
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
			renderFileEvaluatedAttr(b, attr.Name, value)
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

// hoistEngineSceneShaderLib deduplicates repeated shader source in a scene that
// was assembled from JSX children rather than from a typed SceneIR.
//
// It runs after canonicalizeEnginePropsMap for two reasons: that pass deep-
// copies every nested map and slice, so the scene map here is owned by this
// call and safe to mutate, and it rewrites prop keys, which would otherwise
// rename the "shaderLib" key this pass adds.
//
// A json.RawMessage scene is left alone. Those bytes come from
// SceneIR.marshalWire, which has already run the typed hoisting pass, so
// decoding them here would cost a full parse of the largest value in the
// manifest to discover there is nothing left to hoist.
func hoistEngineSceneShaderLib(normalized map[string]any) {
	sceneMap, ok := normalized["scene"].(map[string]any)
	if !ok {
		return
	}
	gosxscene.ApplyShaderLib(sceneMap)
}

func marshalEngineProps(props map[string]any) json.RawMessage {
	if len(props) == 0 {
		return nil
	}
	normalized := canonicalizeEnginePropsMap(props)
	if len(normalized) == 0 {
		return nil
	}
	hoistEngineSceneShaderLib(normalized)
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

	// Pre-marshaled JSON bytes pass through unchanged. The scene package's
	// spreadPropsFast() wraps the Scene3D IR in json.RawMessage to skip the
	// legacy map-tree build — if we let the default reflect.Slice branch
	// below iterate over those bytes, each byte would get canonicalized into
	// a separate interface{} and the whole optimization would evaporate
	// (and produce corrupt JSON downstream). json.Marshal handles
	// json.RawMessage natively so passing the value straight through is
	// both correct and fast.
	if _, ok := value.(json.RawMessage); ok {
		return value
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

func videoAudioTrackListValue(value any) []server.VideoAudioTrack {
	return decodeVideoListValue[server.VideoAudioTrack](value)
}

func videoTrackListValue(value any) []server.VideoTrack {
	return decodeVideoListValue[server.VideoTrack](value)
}

func videoSyncTuningValue(value any) *server.SyncTuning {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case server.SyncTuning:
		return &typed
	case *server.SyncTuning:
		return typed
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var tuning server.SyncTuning
	if err := json.Unmarshal(data, &tuning); err != nil {
		return nil
	}
	if tuning == (server.SyncTuning{}) {
		return nil
	}
	return &tuning
}

func videoSubtitleOptionsValue(value any) *server.SubtitleOptions {
	return decodeVideoStructPointerValue[server.SubtitleOptions](value)
}

func videoAudioSourceOptionsValue(value any) *server.AudioSourceOptions {
	return decodeVideoStructPointerValue[server.AudioSourceOptions](value)
}

func videoFullscreenOptionsValue(value any) *server.FullscreenOptions {
	return decodeVideoStructPointerValue[server.FullscreenOptions](value)
}

func videoTelemetryOptionsValue(value any) *server.VideoTelemetryOptions {
	return decodeVideoStructPointerValue[server.VideoTelemetryOptions](value)
}

func decodeVideoStructPointerValue[T comparable](value any) *T {
	if value == nil {
		return nil
	}
	if typed, ok := value.(T); ok {
		if typed == *new(T) {
			return nil
		}
		return &typed
	}
	if typed, ok := value.(*T); ok {
		return typed
	}
	caseString, ok := value.(string)
	if ok && strings.TrimSpace(caseString) == "" {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	if out == *new(T) {
		return nil
	}
	return &out
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

func (r *fileProgramRenderer) applyScene3DStyles(raw json.RawMessage, node *ir.Node, env fileRenderEnv) json.RawMessage {
	if len(r.opts.Scene3DStyles.Rules) == 0 {
		return raw
	}
	props := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &props)
	}
	if props == nil {
		props = map[string]any{}
	}

	rootAttrs := map[string]any{}
	if node != nil {
		rootAttrs = r.componentAttrMap(node.Attrs, env)
	}
	sceneMap := mapStringAnyValue(props["scene"])
	sceneChanged := false
	propsChanged := false

	for _, rule := range r.opts.Scene3DStyles.Rules {
		if scene3DStyleSelectorMatches(rule.Selector, "Scene3D", rootAttrs) {
			for _, declaration := range rule.Declarations {
				if applyScene3DRootDeclaration(props, &sceneMap, &sceneChanged, declaration) {
					propsChanged = true
				}
			}
		}
	}

	if sceneMap != nil {
		for _, target := range scene3DStyleTargets(sceneMap) {
			records := scene3DRecordList(sceneMap[target.key])
			if len(records) == 0 {
				continue
			}
			recordsChanged := false
			for index := range records {
				for _, rule := range r.opts.Scene3DStyles.Rules {
					if !scene3DStyleSelectorMatchesAny(rule.Selector, target.tags(records[index]), records[index]) {
						continue
					}
					for _, declaration := range rule.Declarations {
						if applyScene3DRecordDeclaration(records[index], declaration) {
							recordsChanged = true
						}
					}
				}
			}
			if recordsChanged {
				sceneMap[target.key] = records
				sceneChanged = true
			}
		}
	}

	if sceneChanged && sceneMap != nil {
		props["scene"] = sceneMap
		propsChanged = true
	}
	if !propsChanged {
		return raw
	}
	return marshalEngineProps(props)
}

type scene3DStyleTarget struct {
	key  string
	tags func(map[string]any) []string
}

func scene3DStyleTargets(sceneMap map[string]any) []scene3DStyleTarget {
	return []scene3DStyleTarget{
		{key: "objects", tags: func(map[string]any) []string { return []string{"Mesh"} }},
		{key: "models", tags: func(map[string]any) []string { return []string{"Model"} }},
		{key: "points", tags: func(map[string]any) []string { return []string{"Points"} }},
		{key: "instancedMeshes", tags: func(map[string]any) []string { return []string{"InstancedMesh"} }},
		{key: "computeParticles", tags: func(map[string]any) []string { return []string{"ComputeParticles"} }},
		{key: "waterSystems", tags: func(map[string]any) []string { return []string{"WaterSystem"} }},
		{key: "lights", tags: scene3DLightStyleTags},
		{key: "materials", tags: func(map[string]any) []string { return []string{"Material"} }},
		{key: "postEffects", tags: scene3DPostEffectStyleTags},
	}
}

func scene3DLightStyleTags(record map[string]any) []string {
	switch strings.ToLower(strings.TrimSpace(scene3DStyleAttrString(record["kind"]))) {
	case "directional":
		return []string{"Light", "DirectionalLight"}
	case "point":
		return []string{"Light", "PointLight"}
	case "ambient":
		return []string{"Light", "AmbientLight"}
	case "spot":
		return []string{"Light", "SpotLight"}
	case "hemisphere":
		return []string{"Light", "HemisphereLight"}
	default:
		return []string{"Light"}
	}
}

func scene3DPostEffectStyleTags(record map[string]any) []string {
	switch strings.ToLower(strings.TrimSpace(scene3DStyleAttrString(record["kind"]))) {
	case "bloom":
		return []string{"PostFX", "Bloom"}
	case "vignette":
		return []string{"PostFX", "Vignette"}
	case "color-grade", "colorgrading", "color-grading":
		return []string{"PostFX", "ColorGrading"}
	default:
		return []string{"PostFX"}
	}
}

func applyScene3DRootDeclaration(props map[string]any, sceneMapRef *map[string]any, sceneChanged *bool, declaration gosxcss.Scene3DDeclaration) bool {
	switch declaration.Name {
	case "background", "scene-background":
		props["background"] = scene3DCSSStringValue(declaration.Value)
		return true
	case "auto-rotate":
		return scene3DSetBool(props, "autoRotate", declaration.Value)
	case "camera-x":
		return scene3DSetNestedNumber(props, "camera", "x", declaration.Value)
	case "camera-y":
		return scene3DSetNestedNumber(props, "camera", "y", declaration.Value)
	case "camera-z":
		return scene3DSetNestedNumber(props, "camera", "z", declaration.Value)
	case "camera-fov":
		return scene3DSetNestedNumber(props, "camera", "fov", declaration.Value)
	case "environment-ambient-color":
		return scene3DSetSceneNestedString(sceneMapRef, sceneChanged, "environment", "ambientColor", declaration.Value)
	case "environment-ambient-intensity":
		return scene3DSetSceneNestedNumber(sceneMapRef, sceneChanged, "environment", "ambientIntensity", declaration.Value)
	case "environment-fog-color":
		return scene3DSetSceneNestedString(sceneMapRef, sceneChanged, "environment", "fogColor", declaration.Value)
	case "environment-fog-density":
		return scene3DSetSceneNestedNumber(sceneMapRef, sceneChanged, "environment", "fogDensity", declaration.Value)
	case "environment-sky-color":
		return scene3DSetSceneNestedString(sceneMapRef, sceneChanged, "environment", "skyColor", declaration.Value)
	case "environment-ground-color":
		return scene3DSetSceneNestedString(sceneMapRef, sceneChanged, "environment", "groundColor", declaration.Value)
	case "postfx-bloom-intensity":
		return scene3DSetPostEffectNumber(sceneMapRef, sceneChanged, "bloom", "intensity", declaration.Value)
	case "postfx-bloom-threshold":
		return scene3DSetPostEffectNumber(sceneMapRef, sceneChanged, "bloom", "threshold", declaration.Value)
	case "postfx-vignette-intensity":
		return scene3DSetPostEffectNumber(sceneMapRef, sceneChanged, "vignette", "intensity", declaration.Value)
	case "scene-filter":
		return scene3DSetSceneFilter(sceneMapRef, sceneChanged, declaration.Value)
	default:
		return false
	}
}

func applyScene3DRecordDeclaration(record map[string]any, declaration gosxcss.Scene3DDeclaration) bool {
	switch declaration.Name {
	case "color", "material-color", "point-color", "light-color":
		record["color"] = scene3DCSSStringValue(declaration.Value)
		return true
	case "material-kind":
		record["materialKind"] = scene3DCSSStringValue(declaration.Value)
		return true
	case "blend-mode":
		record["blendMode"] = scene3DCSSStringValue(declaration.Value)
		return true
	case "render-pass":
		record["renderPass"] = scene3DCSSStringValue(declaration.Value)
		return true
	case "x":
		return scene3DSetNumber(record, "x", declaration.Value)
	case "y":
		return scene3DSetNumber(record, "y", declaration.Value)
	case "z":
		return scene3DSetNumber(record, "z", declaration.Value)
	case "width":
		return scene3DSetNumber(record, "width", declaration.Value)
	case "height":
		return scene3DSetNumber(record, "height", declaration.Value)
	case "depth":
		return scene3DSetNumber(record, "depth", declaration.Value)
	case "size", "point-size":
		return scene3DSetNumber(record, "size", declaration.Value)
	case "radius":
		return scene3DSetNumber(record, "radius", declaration.Value)
	case "segments":
		return scene3DSetNumber(record, "segments", declaration.Value)
	case "opacity", "material-opacity":
		return scene3DSetNumber(record, "opacity", declaration.Value)
	case "roughness", "material-roughness":
		return scene3DSetNumber(record, "roughness", declaration.Value)
	case "metalness", "material-metalness":
		return scene3DSetNumber(record, "metalness", declaration.Value)
	case "emissive", "material-emissive":
		return scene3DSetNumber(record, "emissive", declaration.Value)
	case "light-intensity":
		return scene3DSetNumber(record, "intensity", declaration.Value)
	case "rotation-x", "rotate-x":
		return scene3DSetNumber(record, "rotationX", declaration.Value)
	case "rotation-y", "rotate-y":
		return scene3DSetNumber(record, "rotationY", declaration.Value)
	case "rotation-z", "rotate-z":
		return scene3DSetNumber(record, "rotationZ", declaration.Value)
	case "spin-x":
		return scene3DSetNumber(record, "spinX", declaration.Value)
	case "spin-y":
		return scene3DSetNumber(record, "spinY", declaration.Value)
	case "spin-z":
		return scene3DSetNumber(record, "spinZ", declaration.Value)
	case "line-width":
		return scene3DSetNumber(record, "lineWidth", declaration.Value)
	case "cast-shadow":
		return scene3DSetBool(record, "castShadow", declaration.Value)
	case "receive-shadow":
		return scene3DSetBool(record, "receiveShadow", declaration.Value)
	case "depth-write":
		return scene3DSetBool(record, "depthWrite", declaration.Value)
	case "wireframe":
		return scene3DSetBool(record, "wireframe", declaration.Value)
	case "pickable":
		return scene3DSetBool(record, "pickable", declaration.Value)
	case "attenuation", "size-attenuation":
		return scene3DSetBool(record, "attenuation", declaration.Value)
	default:
		return false
	}
}

func scene3DSetNestedNumber(parent map[string]any, key, childKey, value string) bool {
	child := mapStringAnyValue(parent[key])
	if child == nil {
		child = map[string]any{}
	}
	if !scene3DSetNumber(child, childKey, value) {
		return false
	}
	parent[key] = child
	return true
}

func scene3DSetSceneNestedString(sceneMapRef *map[string]any, changed *bool, key, childKey, value string) bool {
	sceneMap := scene3DEnsureMap(sceneMapRef)
	child := mapStringAnyValue(sceneMap[key])
	if child == nil {
		child = map[string]any{}
	}
	child[childKey] = scene3DCSSStringValue(value)
	sceneMap[key] = child
	*changed = true
	return true
}

func scene3DSetSceneNestedNumber(sceneMapRef *map[string]any, changed *bool, key, childKey, value string) bool {
	sceneMap := scene3DEnsureMap(sceneMapRef)
	child := mapStringAnyValue(sceneMap[key])
	if child == nil {
		child = map[string]any{}
	}
	if !scene3DSetNumber(child, childKey, value) {
		return false
	}
	sceneMap[key] = child
	*changed = true
	return true
}

func scene3DSetPostEffectNumber(sceneMapRef *map[string]any, changed *bool, kind, key, value string) bool {
	number, ok := scene3DCSSNumber(value)
	if !ok {
		return false
	}
	sceneMap := scene3DEnsureMap(sceneMapRef)
	effects := scene3DRecordList(sceneMap["postEffects"])
	for index := range effects {
		if strings.EqualFold(scene3DStyleAttrString(effects[index]["kind"]), kind) {
			effects[index][key] = number
			sceneMap["postEffects"] = effects
			*changed = true
			return true
		}
	}
	effects = append(effects, map[string]any{
		"kind": kind,
		key:    number,
	})
	sceneMap["postEffects"] = effects
	*changed = true
	return true
}

func scene3DSetSceneFilter(sceneMapRef *map[string]any, changed *bool, value string) bool {
	effects := scene3DParseSceneFilter(value)
	if len(effects) == 0 {
		return false
	}
	sceneMap := scene3DEnsureMap(sceneMapRef)
	sceneMap["postEffects"] = effects
	*changed = true
	return true
}

func scene3DParseSceneFilter(value string) []map[string]any {
	text := strings.TrimSpace(value)
	if text == "" || strings.EqualFold(text, "none") {
		return nil
	}
	effects := []map[string]any{}
	for {
		open := strings.IndexByte(text, '(')
		if open < 0 {
			break
		}
		kind := scene3DSceneFilterKind(strings.TrimSpace(text[:open]))
		rest := text[open+1:]
		close := strings.IndexByte(rest, ')')
		if close < 0 {
			break
		}
		if kind != "" {
			effect := map[string]any{"kind": kind}
			body := strings.NewReplacer(",", " ", ":", " ", "=", " ").Replace(rest[:close])
			parts := strings.Fields(body)
			for i := 0; i+1 < len(parts); i += 2 {
				key := scene3DSceneFilterKey(parts[i])
				if key == "" {
					continue
				}
				if number, ok := scene3DCSSNumber(parts[i+1]); ok {
					effect[key] = number
				}
			}
			effects = append(effects, effect)
		}
		text = strings.TrimSpace(rest[close+1:])
	}
	return effects
}

func scene3DSceneFilterKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "bloom", "vignette":
		return strings.ToLower(strings.TrimSpace(kind))
	case "color-grade", "color-grading", "colorgrade":
		return "color-grade"
	default:
		return ""
	}
}

func scene3DSceneFilterKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "threshold", "intensity", "radius", "scale", "saturation", "contrast", "exposure":
		return strings.ToLower(strings.TrimSpace(key))
	default:
		return ""
	}
}

func scene3DEnsureMap(ref *map[string]any) map[string]any {
	if *ref == nil {
		*ref = map[string]any{}
	}
	return *ref
}

func scene3DSetString(record map[string]any, key, value string) bool {
	record[key] = scene3DCSSStringValue(value)
	return true
}

func scene3DSetNumber(record map[string]any, key, value string) bool {
	if scene3DCSSVarExpression(value) {
		record[key] = strings.TrimSpace(value)
		return true
	}
	number, ok := scene3DCSSNumber(value)
	if !ok {
		return false
	}
	record[key] = number
	return true
}

func scene3DSetBool(record map[string]any, key, value string) bool {
	boolean, ok := scene3DCSSBool(value)
	if !ok {
		return false
	}
	record[key] = boolean
	return true
}

func scene3DCSSStringValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func scene3DCSSNumber(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "px")
	if scene3DCSSVarExpression(value) {
		return 0, false
	}
	var number float64
	if _, err := fmt.Sscanf(value, "%f", &number); err != nil {
		return 0, false
	}
	return number, true
}

func scene3DCSSVarExpression(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "var(") && strings.Contains(value, "--") && strings.HasSuffix(value, ")")
}

func scene3DCSSBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func scene3DStyleSelectorMatchesAny(selector string, tags []string, attrs map[string]any) bool {
	for _, tag := range tags {
		if scene3DStyleSelectorMatches(selector, tag, attrs) {
			return true
		}
	}
	return false
}

func scene3DStyleSelectorMatches(selector, tag string, attrs map[string]any) bool {
	for _, part := range splitScene3DSelectorList(selector) {
		if scene3DStyleSimpleSelectorMatches(part, tag, attrs) {
			return true
		}
	}
	return false
}

func splitScene3DSelectorList(selector string) []string {
	parts := []string{}
	start := 0
	for pos := 0; pos <= len(selector); pos++ {
		if pos < len(selector) && selector[pos] != ',' {
			continue
		}
		if part := strings.TrimSpace(selector[start:pos]); part != "" {
			parts = append(parts, part)
		}
		start = pos + 1
	}
	return parts
}

func scene3DStyleSimpleSelectorMatches(selector, tag string, attrs map[string]any) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" || strings.ContainsAny(selector, " >+~[:") {
		return false
	}
	pos := 0
	for pos < len(selector) && selector[pos] != '.' && selector[pos] != '#' {
		pos++
	}
	typeName := selector[:pos]
	if typeName != "" && typeName != "*" && !strings.EqualFold(typeName, tag) {
		return false
	}
	if typeName == "" && (pos >= len(selector) || selector[pos] != '.' && selector[pos] != '#') {
		return false
	}
	for pos < len(selector) {
		prefix := selector[pos]
		if prefix != '.' && prefix != '#' {
			return false
		}
		pos++
		start := pos
		for pos < len(selector) && selector[pos] != '.' && selector[pos] != '#' {
			pos++
		}
		value := strings.TrimSpace(selector[start:pos])
		if value == "" {
			return false
		}
		if prefix == '#' {
			if scene3DStyleAttrString(attrs["id"]) != value {
				return false
			}
			continue
		}
		if !scene3DStyleHasClass(attrs, value) {
			return false
		}
	}
	return true
}

func scene3DStyleHasClass(attrs map[string]any, className string) bool {
	for _, source := range []any{attrs["class"], attrs["className"]} {
		for _, class := range strings.Fields(scene3DStyleAttrString(source)) {
			if class == className {
				return true
			}
		}
	}
	return false
}

func scene3DStyleAttrString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		return strings.TrimSpace(strings.Join(v, " "))
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := scene3DStyleAttrString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
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
				},
				{
					"kind":  "cube",
					"size":  1.1,
					"x":     1.7,
					"y":     -0.8,
					"z":     1.4,
					"color": "#ffd48f",
				},
			},
		}
	}
	return marshalEngineProps(props)
}

func scene3DRequiredCapabilities(raw json.RawMessage, existing []engine.Capability) []engine.Capability {
	props := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &props)
	}
	requireWebGL := false
	if value, ok := lookupTemplatePropValue(props, "requireWebGL"); ok {
		requireWebGL = truthy(value)
	}
	if !requireWebGL {
		return existing
	}

	out := append([]engine.Capability(nil), existing...)
	seen := map[engine.Capability]struct{}{}
	for _, capability := range out {
		seen[capability] = struct{}{}
	}
	for _, capability := range []engine.Capability{engine.CapCanvas, engine.CapWebGL} {
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	return out
}

func mergedEngineCapabilities(primary, secondary []engine.Capability) []engine.Capability {
	if len(primary) == 0 && len(secondary) == 0 {
		return nil
	}
	out := make([]engine.Capability, 0, len(primary)+len(secondary))
	seen := map[engine.Capability]struct{}{}
	appendCapability := func(capability engine.Capability) {
		normalized := engine.Capability(strings.ToLower(strings.TrimSpace(string(capability))))
		if normalized == "" {
			return
		}
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	for _, capability := range primary {
		appendCapability(capability)
	}
	for _, capability := range secondary {
		appendCapability(capability)
	}
	return out
}

func validateScene3DCompilerCapabilities(raw json.RawMessage, capabilities []engine.Capability) error {
	if len(raw) == 0 {
		return nil
	}
	props := map[string]any{}
	if err := json.Unmarshal(raw, &props); err != nil {
		return fmt.Errorf("decode Scene3D props for capability validation: %w", err)
	}

	nodes := scene3DCapabilityNodes(props)
	if sceneMap := mapStringAnyValue(props["scene"]); sceneMap != nil {
		nodes = append(nodes, scene3DCapabilityNodes(sceneMap)...)
	}
	if len(nodes) == 0 {
		return nil
	}

	caps := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		caps = append(caps, string(capability))
	}
	if err := gosxscene.ValidateCapabilities(gosxscene.IR{
		Version: gosxscene.IRVersion,
		Nodes:   nodes,
	}, caps); err != nil {
		return fmt.Errorf("Scene3D capability validation failed: %w", err)
	}
	return nil
}

func scene3DCapabilityNodes(values map[string]any) []gosxscene.IRNode {
	if len(values) == 0 {
		return nil
	}
	nodes := []gosxscene.IRNode{}
	for _, record := range scene3DRecordList(values["computeParticles"]) {
		nodes = append(nodes, gosxscene.IRNode{
			Kind:         "compute-particles",
			ID:           strings.TrimSpace(fmt.Sprint(record["id"])),
			Capabilities: []string{"compute"},
			Compute:      &gosxscene.IRComputeNode{},
		})
	}
	for _, record := range scene3DRecordList(values["waterSystems"]) {
		nodes = append(nodes, gosxscene.IRNode{
			Kind:         "water-system",
			ID:           strings.TrimSpace(fmt.Sprint(record["id"])),
			Capabilities: []string{"webgpu"},
			Compute:      &gosxscene.IRComputeNode{},
		})
	}
	for _, record := range scene3DRecordList(values["nodes"]) {
		kind := strings.TrimSpace(fmt.Sprint(record["kind"]))
		if kind == "compute-particles" {
			nodes = append(nodes, gosxscene.IRNode{
				Kind:         kind,
				ID:           strings.TrimSpace(fmt.Sprint(record["id"])),
				Capabilities: []string{"compute"},
				Compute:      &gosxscene.IRComputeNode{},
			})
		}
	}
	return nodes
}

func (r *fileProgramRenderer) applyScene3DComposableChildren(raw json.RawMessage, node *ir.Node, env fileRenderEnv) json.RawMessage {
	if node == nil || len(node.Children) == 0 {
		return raw
	}
	childProps := r.lowerScene3DComposableChildren(node.Children, env)
	if len(childProps) == 0 {
		return raw
	}

	props := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &props)
	}
	if props == nil {
		props = map[string]any{}
	}
	if camera, ok := childProps["camera"]; ok {
		if mapped := mapStringAnyValue(camera); mapped != nil {
			mergeStringAnyMapValue(props, "camera", mapped)
		} else {
			props["camera"] = camera
		}
	}
	if sceneValue, ok := childProps["scene"].(map[string]any); ok {
		sceneMap := mapStringAnyValue(props["scene"])
		if sceneMap == nil {
			sceneMap = map[string]any{}
		}
		mergeScene3DSceneMap(sceneMap, sceneValue)
		annotateScene3DComposableBackendCaps(sceneMap)
		props["scene"] = sceneMap
	}
	return marshalEngineProps(props)
}

func annotateScene3DComposableBackendCaps(sceneMap map[string]any) {
	if len(sceneMap) == 0 {
		return
	}
	if _, ok := sceneMap["backendCaps"]; ok {
		return
	}
	features := scene3DComposableBackendFeatures(sceneMap)
	if len(features) == 0 {
		return
	}
	sceneMap["backendCaps"] = capability.Verdict(features, nil, capability.DefaultPolicy())
}

func scene3DComposableBackendFeatures(sceneMap map[string]any) []capability.Feature {
	seen := map[capability.Feature]bool{}
	for _, water := range scene3DRecordList(sceneMap["waterSystems"]) {
		if scene3DWaterSystemUsesObjectTexturePass(water) {
			seen[capability.FeatureWaterObjectTexturePass] = true
		}
		if scene3DWaterSystemUsesObjectMeshShadowPass(water) {
			seen[capability.FeatureWaterObjectMeshShadowPass] = true
		}
		seen[capability.FeatureWaterSim] = true
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]capability.Feature, 0, len(seen))
	for feature := range seen {
		out = append(out, feature)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func scene3DWaterSystemUsesObjectTexturePass(water map[string]any) bool {
	if numericValue(water["objectTextureResolution"]) > 0 || scene3DNonEmptyString(water["objectTextureResolutionMode"]) || numericValue(water["objectShadowResolution"]) > 0 {
		return true
	}
	if scene3DWaterObjectKindUsesTexturePass(water["objectKind"]) || scene3DWaterObjectKindUsesTexturePass(water["activeObject"]) || scene3DWaterObjectKindUsesTexturePass(water["interactionObject"]) {
		return true
	}
	return len(scene3DRecordList(water["objectDisplacementSpheres"])) > 0
}

func scene3DWaterSystemUsesObjectMeshShadowPass(water map[string]any) bool {
	if scene3DNonEmptyString(water["objectMeshShadowVertexWGSL"]) ||
		scene3DNonEmptyString(water["objectMeshShadowFragmentWGSL"]) ||
		scene3DNonEmptyString(water["objectMeshShadowVertexWGSLRef"]) ||
		scene3DNonEmptyString(water["objectMeshShadowFragmentWGSLRef"]) {
		return true
	}
	return scene3DWaterObjectKindUsesMeshProjectedPass(water["objectKind"]) ||
		scene3DWaterObjectKindUsesMeshProjectedPass(water["activeObject"]) ||
		scene3DWaterObjectKindUsesMeshProjectedPass(water["interactionObject"])
}

func scene3DWaterObjectKindUsesTexturePass(value any) bool {
	return scene3DWaterObjectKindUsesMeshProjectedPass(value)
}

func scene3DWaterObjectKindUsesMeshProjectedPass(value any) bool {
	text := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	if text == "" || text == "none" || text == "off" || text == "disabled" || text == "no_object" || text == "<nil>" {
		return false
	}
	return strings.Contains(text, "compound") ||
		strings.Contains(text, "mesh") ||
		strings.Contains(text, "torus") ||
		strings.Contains(text, "duck") ||
		strings.Contains(text, "gltf") ||
		strings.Contains(text, "glb")
}

func scene3DNonEmptyString(value any) bool {
	text := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	return text != "" && text != "none" && text != "off" && text != "disabled" && text != "<nil>"
}

func (r *fileProgramRenderer) lowerScene3DComposableChildren(children []ir.NodeID, env fileRenderEnv) map[string]any {
	sceneMap := map[string]any{}
	out := map[string]any{}
	r.lowerScene3DChildList(children, env, sceneMap, out)
	if len(sceneMap) > 0 {
		out["scene"] = sceneMap
	}
	return out
}

func (r *fileProgramRenderer) lowerScene3DChildList(children []ir.NodeID, env fileRenderEnv, sceneMap, out map[string]any) {
	for _, childID := range children {
		child := r.prog.NodeAt(childID)
		if child == nil {
			continue
		}
		// Handle control-flow builtins by evaluating and recursing
		// into their children so <Each>/<If> work inside <Scene3D>.
		if child.Kind == ir.NodeComponent {
			switch child.Tag {
			case "Each", "For":
				r.lowerScene3DEach(child, env, sceneMap, out)
				continue
			case "If", "Show", "When":
				condition := attrValue(child.Attrs, env, "when", "if", "cond", "test")
				if truthy(condition) {
					r.lowerScene3DChildList(child.Children, env, sceneMap, out)
				}
				continue
			}
			if !isScene3DComposableTag(child.Tag) {
				continue
			}
			r.lowerScene3DComposableNode(child, env, sceneMap, out)
		}
	}
}

func (r *fileProgramRenderer) lowerScene3DEach(node *ir.Node, env fileRenderEnv, sceneMap, out map[string]any) {
	collection := attrValue(node.Attrs, env, "of", "each", "items")
	if collection == nil {
		return
	}
	itemName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "as", "item")))
	if itemName == "" {
		itemName = "item"
	}
	indexName := strings.TrimSpace(stringValue(attrValue(node.Attrs, env, "index")))
	for _, entry := range fileEachEntries(collection) {
		scope := env.withValue(itemName, entry.Value)
		if indexName != "" {
			scope = scope.withValue(indexName, entry.Index)
		}
		if entry.Key != nil {
			scope = scope.withValue(itemName+"Key", entry.Key)
		}
		r.lowerScene3DChildList(node.Children, scope, sceneMap, out)
	}
}

func (r *fileProgramRenderer) lowerScene3DComposableNode(child *ir.Node, env fileRenderEnv, sceneMap, out map[string]any) {
	attrs := r.componentAttrMap(child.Attrs, env)
	switch child.Tag {
	case "Mesh":
		appendScene3DSceneRecord(sceneMap, "objects", attrs)
	case "Decal":
		appendScene3DSceneRecord(sceneMap, "objects", scene3DDecalAttrs(attrs))
	case "Model":
		appendScene3DSceneRecord(sceneMap, "models", attrs)
	case "Points":
		appendScene3DSceneRecord(sceneMap, "points", attrs)
	case "InstancedMesh":
		appendScene3DSceneRecord(sceneMap, "instancedMeshes", attrs)
	case "ComputeParticles":
		appendScene3DSceneRecord(sceneMap, "computeParticles", attrs)
	case "WaterSystem":
		appendScene3DSceneRecord(sceneMap, "waterSystems", attrs)
	case "Html", "HTML":
		attrs = cloneStringAnyMap(attrs)
		if _, ok := attrs["html"]; !ok {
			if _, ok := attrs["markup"]; !ok {
				if _, ok := attrs["content"]; !ok {
					if childrenHTML := strings.TrimSpace(r.renderChildren(child.Children, env)); childrenHTML != "" {
						attrs["html"] = childrenHTML
					}
				}
			}
		}
		appendScene3DSceneRecord(sceneMap, "html", attrs)
	case "DirectionalLight", "PointLight", "AmbientLight", "SpotLight", "HemisphereLight", "RectAreaLight", "LightProbe":
		attrs = cloneStringAnyMap(attrs)
		if _, ok := attrs["kind"]; !ok {
			attrs["kind"] = scene3DLightKind(child.Tag)
		}
		appendScene3DSceneRecord(sceneMap, "lights", attrs)
	case "Environment":
		mergeStringAnyMapValue(sceneMap, "environment", attrs)
	case "Camera":
		out["camera"] = attrs
	case "Material":
		appendScene3DSceneRecord(sceneMap, "materials", attrs)
	case "LineBasicMaterial":
		appendScene3DSceneRecord(sceneMap, "materials", scene3DMaterialAttrs(attrs, "line-basic"))
	case "LineDashedMaterial":
		appendScene3DSceneRecord(sceneMap, "materials", scene3DMaterialAttrs(attrs, "line-dashed"))
	case "CustomMaterial":
		appendScene3DSceneRecord(sceneMap, "materials", scene3DMaterialAttrs(attrs, "custom"))
	case "AxesHelper":
		lowerScene3DAxesHelper(sceneMap, attrs)
	case "GridHelper":
		lowerScene3DGridHelper(sceneMap, attrs)
	case "BoxHelper":
		lowerScene3DBoxHelper(sceneMap, attrs)
	case "BoundingBoxHelper":
		lowerScene3DBoundingBoxHelper(sceneMap, attrs)
	case "SkeletonHelper":
		lowerScene3DSkeletonHelper(sceneMap, attrs)
	case "TransformControls":
		lowerScene3DTransformControls(sceneMap, attrs)
	case "PostFX.SSAO":
		appendScene3DSceneRecord(sceneMap, "postEffects", withDefaultKind(attrs, "ssao"))
	case "PostFX.DOF":
		appendScene3DSceneRecord(sceneMap, "postEffects", withDefaultKind(attrs, "dof"))
	case "PostFX.Bloom":
		appendScene3DSceneRecord(sceneMap, "postEffects", withDefaultKind(attrs, "bloom"))
	case "PostFX.Custom":
		appendScene3DSceneRecord(sceneMap, "postEffects", withDefaultKind(attrs, "customPost"))
	case "PostFX.Vignette":
		appendScene3DSceneRecord(sceneMap, "postEffects", withDefaultKind(attrs, "vignette"))
	case "PostFX.ColorGrading":
		appendScene3DSceneRecord(sceneMap, "postEffects", withDefaultKind(attrs, "color-grade"))
	case "PostFX.Tonemap":
		appendScene3DSceneRecord(sceneMap, "postEffects", withDefaultKind(attrs, "tonemap"))
	}
}

func isScene3DComposableTag(tag string) bool {
	switch tag {
	case "Mesh", "Decal", "Model", "Points", "InstancedMesh", "ComputeParticles", "WaterSystem",
		"Html", "HTML",
		"DirectionalLight", "PointLight", "AmbientLight", "SpotLight", "HemisphereLight", "RectAreaLight", "LightProbe",
		"Environment", "Camera", "Material", "LineBasicMaterial", "LineDashedMaterial", "CustomMaterial",
		"AxesHelper", "GridHelper", "BoxHelper", "BoundingBoxHelper", "SkeletonHelper", "TransformControls",
		"PostFX.SSAO", "PostFX.DOF", "PostFX.Bloom", "PostFX.Custom", "PostFX.Vignette", "PostFX.ColorGrading", "PostFX.Tonemap":
		return true
	default:
		return false
	}
}

func scene3DDecalAttrs(attrs map[string]any) map[string]any {
	out := cloneStringAnyMap(attrs)
	out["kind"] = "plane"
	if _, ok := out["texture"]; !ok {
		if src, ok := out["src"]; ok {
			out["texture"] = src
		}
	}
	if _, ok := out["materialKind"]; !ok {
		out["materialKind"] = "flat"
	}
	if _, ok := out["color"]; !ok {
		out["color"] = "#ffffff"
	}
	if _, ok := out["opacity"]; !ok {
		out["opacity"] = 1
	}
	if _, ok := out["blendMode"]; !ok {
		out["blendMode"] = "alpha"
	}
	if _, ok := out["renderPass"]; !ok {
		out["renderPass"] = "alpha"
	}
	if _, ok := out["depthWrite"]; !ok {
		out["depthWrite"] = false
	}
	return out
}

func scene3DMaterialAttrs(attrs map[string]any, kind string) map[string]any {
	out := withDefaultKind(attrs, kind)
	if kind == "line-basic" || kind == "line-dashed" {
		if _, ok := out["lineWidth"]; !ok {
			if width, ok := out["width"]; ok {
				out["lineWidth"] = width
			}
		}
	}
	if kind == "line-dashed" {
		if _, ok := out["lineDash"]; !ok {
			out["lineDash"] = true
		}
	}
	if kind == "custom" {
		if _, ok := out["customVertex"]; !ok {
			if value, ok := out["vertexGLSL"]; ok {
				out["customVertex"] = value
			} else if value, ok := out["vertex"]; ok {
				out["customVertex"] = value
			}
		}
		if _, ok := out["customFragment"]; !ok {
			if value, ok := out["fragmentGLSL"]; ok {
				out["customFragment"] = value
			} else if value, ok := out["fragment"]; ok {
				out["customFragment"] = value
			}
		}
		if _, ok := out["customUniforms"]; !ok {
			if value, ok := out["uniforms"]; ok {
				out["customUniforms"] = value
			}
		}
	}
	return out
}

func lowerScene3DAxesHelper(sceneMap map[string]any, attrs map[string]any) {
	size := scene3DNumber(attrs, 1, "size")
	width := scene3DNumber(attrs, 1.5, "lineWidth", "widthPx", "width")
	base := scene3DHelperID(attrs, "scene-axes-helper")
	appendScene3DSceneRecord(sceneMap, "objects", scene3DLineObject(attrs, base+"-x", "#ef4444", width,
		[]map[string]any{scene3DPoint(0, 0, 0), scene3DPoint(size, 0, 0)}, [][2]int{{0, 1}}))
	appendScene3DSceneRecord(sceneMap, "objects", scene3DLineObject(attrs, base+"-y", "#22c55e", width,
		[]map[string]any{scene3DPoint(0, 0, 0), scene3DPoint(0, size, 0)}, [][2]int{{0, 1}}))
	appendScene3DSceneRecord(sceneMap, "objects", scene3DLineObject(attrs, base+"-z", "#3b82f6", width,
		[]map[string]any{scene3DPoint(0, 0, 0), scene3DPoint(0, 0, size)}, [][2]int{{0, 1}}))
}

func lowerScene3DGridHelper(sceneMap map[string]any, attrs map[string]any) {
	size := scene3DNumber(attrs, 10, "size")
	divisions := int(scene3DNumber(attrs, 10, "divisions"))
	if divisions <= 0 {
		divisions = 10
	}
	half := size / 2
	step := size / float64(divisions)
	points := make([]map[string]any, 0, (divisions+1)*4)
	segments := make([][2]int, 0, (divisions+1)*2)
	for i := 0; i <= divisions; i++ {
		offset := -half + float64(i)*step
		base := len(points)
		points = append(points,
			scene3DPoint(-half, 0, offset),
			scene3DPoint(half, 0, offset),
			scene3DPoint(offset, 0, -half),
			scene3DPoint(offset, 0, half),
		)
		segments = append(segments, [2]int{base, base + 1}, [2]int{base + 2, base + 3})
	}
	width := scene3DNumber(attrs, 1, "lineWidth", "widthPx", "width")
	color := scene3DString(attrs, "#9ca3af", "color")
	record := scene3DLineObject(attrs, scene3DHelperID(attrs, "scene-grid-helper"), color, width, points, segments)
	if _, ok := record["opacity"]; !ok {
		record["opacity"] = 0.72
	}
	if _, ok := record["blendMode"]; !ok {
		record["blendMode"] = "alpha"
	}
	appendScene3DSceneRecord(sceneMap, "objects", record)
}

func lowerScene3DBoxHelper(sceneMap map[string]any, attrs map[string]any) {
	width := scene3DNumber(attrs, 1, "width")
	height := scene3DNumber(attrs, width, "height")
	depth := scene3DNumber(attrs, width, "depth")
	lineWidth := scene3DNumber(attrs, 0, "lineWidth", "widthPx")
	points, segments := scene3DBoxLineGeometry(width, height, depth)
	appendScene3DSceneRecord(sceneMap, "objects", scene3DLineObject(attrs, scene3DHelperID(attrs, "scene-box-helper"),
		scene3DString(attrs, "#f59e0b", "color"), lineWidth, points, segments))
}

func lowerScene3DBoundingBoxHelper(sceneMap map[string]any, attrs map[string]any) {
	min := scene3DVectorFromAttrs(attrs, "min", scene3DPointValue(attrs["min"]))
	max := scene3DVectorFromAttrs(attrs, "max", scene3DPointValue(attrs["max"]))
	width := math.Abs(max[0] - min[0])
	height := math.Abs(max[1] - min[1])
	depth := math.Abs(max[2] - min[2])
	points, segments := scene3DBoxLineGeometry(width, height, depth)
	record := scene3DLineObject(attrs, scene3DHelperID(attrs, "scene-bounds-helper"),
		scene3DString(attrs, "#f59e0b", "color"), scene3DNumber(attrs, 0, "lineWidth", "widthPx"), points, segments)
	record["x"] = (min[0] + max[0]) / 2
	record["y"] = (min[1] + max[1]) / 2
	record["z"] = (min[2] + max[2]) / 2
	appendScene3DSceneRecord(sceneMap, "objects", record)
}

func lowerScene3DSkeletonHelper(sceneMap map[string]any, attrs map[string]any) {
	points := scene3DPointListValue(firstNonEmptyValue(attrs["joints"], attrs["points"]))
	segments := scene3DSegmentListValue(firstNonEmptyValue(attrs["bones"], attrs["segments"]))
	if len(points) == 0 || len(segments) == 0 {
		return
	}
	appendScene3DSceneRecord(sceneMap, "objects", scene3DLineObject(attrs, scene3DHelperID(attrs, "scene-skeleton-helper"),
		scene3DString(attrs, "#e879f9", "color"), scene3DNumber(attrs, 1.25, "lineWidth", "widthPx", "width"), points, segments))
}

func lowerScene3DTransformControls(sceneMap map[string]any, attrs map[string]any) {
	positioned := cloneStringAnyMap(attrs)
	if target := strings.TrimSpace(stringValue(attrs["target"])); target != "" {
		if targetAttrs := scene3DFindObject(sceneMap, target); targetAttrs != nil {
			for _, key := range []string{"x", "y", "z"} {
				if _, ok := positioned[key]; !ok {
					if value, exists := targetAttrs[key]; exists && value != nil {
						positioned[key] = value
					}
				}
			}
		}
	}
	lowerScene3DAxesHelper(sceneMap, positioned)
	if strings.EqualFold(strings.TrimSpace(stringValue(attrs["mode"])), "rotate") {
		size := scene3DNumber(attrs, 1.25, "size")
		lineWidth := scene3DNumber(attrs, 2, "lineWidth", "widthPx", "width")
		points, segments := scene3DRingLineGeometry(size, 48)
		appendScene3DSceneRecord(sceneMap, "objects", scene3DLineObject(positioned, scene3DHelperID(attrs, "scene-transform-controls")+"-ring",
			"#facc15", lineWidth, points, segments))
	}
}

func scene3DLineObject(attrs map[string]any, id, color string, width float64, points []map[string]any, segments [][2]int) map[string]any {
	record := scene3DTransformAttrs(attrs)
	record["id"] = id
	record["kind"] = "lines"
	record["materialKind"] = "line-basic"
	record["color"] = color
	record["points"] = points
	record["segments"] = segments
	if width > 0 {
		record["lineWidth"] = width
	}
	return record
}

func scene3DTransformAttrs(attrs map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{
		"x", "y", "z",
		"rotationX", "rotationY", "rotationZ",
		"scaleX", "scaleY", "scaleZ",
		"spinX", "spinY", "spinZ",
		"pickable", "selected", "outlineColor", "outlineWidth",
	} {
		if value, ok := attrs[key]; ok {
			out[key] = value
		}
	}
	return out
}

func scene3DHelperID(attrs map[string]any, fallback string) string {
	if id := strings.TrimSpace(stringValue(attrs["id"])); id != "" {
		return id
	}
	return fallback
}

func scene3DNumber(attrs map[string]any, fallback float64, names ...string) float64 {
	for _, name := range names {
		if value, ok := attrs[name]; ok {
			return numericValue(value)
		}
	}
	return fallback
}

func scene3DString(attrs map[string]any, fallback string, names ...string) string {
	for _, name := range names {
		if value, ok := attrs[name]; ok {
			if text := strings.TrimSpace(stringValue(value)); text != "" {
				return text
			}
		}
	}
	return fallback
}

func scene3DPoint(x, y, z float64) map[string]any {
	return map[string]any{"x": x, "y": y, "z": z}
}

func scene3DBoxLineGeometry(width, height, depth float64) ([]map[string]any, [][2]int) {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	if depth <= 0 {
		depth = 1
	}
	x := width / 2
	y := height / 2
	z := depth / 2
	return []map[string]any{
			scene3DPoint(-x, -y, -z), scene3DPoint(x, -y, -z), scene3DPoint(x, y, -z), scene3DPoint(-x, y, -z),
			scene3DPoint(-x, -y, z), scene3DPoint(x, -y, z), scene3DPoint(x, y, z), scene3DPoint(-x, y, z),
		}, [][2]int{
			{0, 1}, {1, 2}, {2, 3}, {3, 0},
			{4, 5}, {5, 6}, {6, 7}, {7, 4},
			{0, 4}, {1, 5}, {2, 6}, {3, 7},
		}
}

func scene3DRingLineGeometry(radius float64, count int) ([]map[string]any, [][2]int) {
	if radius <= 0 {
		radius = 1
	}
	if count < 8 {
		count = 32
	}
	points := make([]map[string]any, 0, count)
	segments := make([][2]int, 0, count)
	for i := 0; i < count; i++ {
		angle := (float64(i) / float64(count)) * math.Pi * 2
		points = append(points, scene3DPoint(math.Cos(angle)*radius, math.Sin(angle)*radius, 0))
		segments = append(segments, [2]int{i, (i + 1) % count})
	}
	return points, segments
}

func scene3DPointValue(value any) [3]float64 {
	switch current := value.(type) {
	case gosxscene.Vector3:
		return [3]float64{current.X, current.Y, current.Z}
	case map[string]any:
		return [3]float64{numericValue(firstNonEmptyValue(current["x"], current["X"])), numericValue(firstNonEmptyValue(current["y"], current["Y"])), numericValue(firstNonEmptyValue(current["z"], current["Z"]))}
	}
	mapped := mapStringAnyValue(value)
	if mapped != nil {
		return [3]float64{numericValue(firstNonEmptyValue(mapped["x"], mapped["X"])), numericValue(firstNonEmptyValue(mapped["y"], mapped["Y"])), numericValue(firstNonEmptyValue(mapped["z"], mapped["Z"]))}
	}
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return [3]float64{}
		}
		rv = rv.Elem()
	}
	if rv.IsValid() && rv.Kind() == reflect.Struct {
		return [3]float64{
			numericValue(fieldValueByName(rv, "X")),
			numericValue(fieldValueByName(rv, "Y")),
			numericValue(fieldValueByName(rv, "Z")),
		}
	}
	return [3]float64{}
}

func scene3DVectorFromAttrs(attrs map[string]any, prefix string, fallback [3]float64) [3]float64 {
	out := fallback
	for index, key := range []string{"X", "Y", "Z"} {
		if value, ok := attrs[prefix+key]; ok {
			out[index] = numericValue(value)
		}
		if value, ok := attrs[prefix+strings.ToLower(key)]; ok {
			out[index] = numericValue(value)
		}
	}
	return out
}

func scene3DPointListValue(value any) []map[string]any {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil
	}
	if rv.Len() > 0 {
		first := scene3DIndirectReflectValue(rv.Index(0))
		if first.IsValid() && isNumericKind(first.Kind()) && rv.Len()%3 == 0 {
			out := make([]map[string]any, 0, rv.Len()/3)
			for i := 0; i+2 < rv.Len(); i += 3 {
				out = append(out, scene3DPoint(numericValue(rv.Index(i).Interface()), numericValue(rv.Index(i+1).Interface()), numericValue(rv.Index(i+2).Interface())))
			}
			return out
		}
	}
	out := make([]map[string]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		point := scene3DPointValue(rv.Index(i).Interface())
		out = append(out, scene3DPoint(point[0], point[1], point[2]))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scene3DSegmentListValue(value any) [][2]int {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil
	}
	out := make([][2]int, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := scene3DIndirectReflectValue(rv.Index(i))
		if !item.IsValid() || (item.Kind() != reflect.Slice && item.Kind() != reflect.Array) || item.Len() < 2 {
			continue
		}
		out = append(out, [2]int{int(numericValue(item.Index(0).Interface())), int(numericValue(item.Index(1).Interface()))})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isNumericKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func scene3DIndirectReflectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func fieldValueByName(value reflect.Value, name string) any {
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return nil
	}
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	return field.Interface()
}

func scene3DFindObject(sceneMap map[string]any, id string) map[string]any {
	for _, record := range scene3DRecordList(sceneMap["objects"]) {
		if strings.TrimSpace(stringValue(record["id"])) == id {
			return record
		}
	}
	return nil
}

func scene3DLightKind(tag string) string {
	switch tag {
	case "DirectionalLight":
		return "directional"
	case "PointLight":
		return "point"
	case "AmbientLight":
		return "ambient"
	case "SpotLight":
		return "spot"
	case "HemisphereLight":
		return "hemisphere"
	case "RectAreaLight":
		return "rect-area"
	case "LightProbe":
		return "light-probe"
	default:
		return ""
	}
}

func appendScene3DSceneRecord(sceneMap map[string]any, key string, record map[string]any) {
	if sceneMap == nil || record == nil {
		return
	}
	current, _ := sceneMap[key].([]map[string]any)
	if current == nil {
		if values, ok := sceneMap[key].([]any); ok {
			current = make([]map[string]any, 0, len(values)+1)
			for _, value := range values {
				if mapped := mapStringAnyValue(value); mapped != nil {
					current = append(current, mapped)
				}
			}
		}
	}
	sceneMap[key] = append(current, cloneStringAnyMap(record))
}

func mergeScene3DSceneMap(dst, src map[string]any) {
	for key, value := range src {
		switch key {
		case "objects", "models", "points", "instancedMeshes", "computeParticles", "waterSystems", "lights", "materials", "postEffects":
			for _, item := range scene3DRecordList(value) {
				appendScene3DSceneRecord(dst, key, item)
			}
		case "environment":
			if mapped := mapStringAnyValue(value); mapped != nil {
				mergeStringAnyMapValue(dst, key, mapped)
			}
		default:
			dst[key] = value
		}
	}
}

func scene3DRecordList(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if mapped := mapStringAnyValue(item); mapped != nil {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func mergeStringAnyMapValue(target map[string]any, key string, values map[string]any) {
	current := mapStringAnyValue(target[key])
	if current == nil {
		current = map[string]any{}
	}
	for itemKey, itemValue := range values {
		current[itemKey] = itemValue
	}
	target[key] = current
}

func cloneStringAnyMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func withDefaultKind(values map[string]any, kind string) map[string]any {
	out := cloneStringAnyMap(values)
	if _, ok := out["kind"]; !ok {
		out["kind"] = kind
	}
	return out
}

func spreadValue(value any, name string) (any, bool) {
	if props := spreadProps(value); len(props) > 0 {
		if item, ok := lookupTemplatePropValue(props, name); ok {
			return item, true
		}
	}
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
