package route

import (
	"strings"
	"testing"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// --- gosx#240: the typed legacy render boundary -----------------------------

// routeTypedLegacySide stands in for the loader struct a sibling page.server.go
// builds. Its name differs from the .gsx schema's on purpose, the same way
// strictSpreadTestSide's does: a spread source is proved by the fields the
// renderer reads, not by its type's name (gosx#230).
type routeTypedLegacySide struct {
	Tone         string
	Abbreviation string
}

// typedLegacyMarkComponent is the strict callee every fixture below forwards
// into: it renders two scalar props, so it needs both of them proved.
func typedLegacyMarkComponent(prog *ir.Program) ir.Component {
	tone := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Tone"})
	abbr := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Abbreviation"})
	root := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "span", Children: []ir.NodeID{tone, abbr}})
	return ir.Component{
		Name:        "Mark",
		PropsName:   "props",
		PropsType:   "MarkProps",
		PropsFields: map[string]string{"Tone": "string", "Abbreviation": "string"},
		Syntax:      ir.ComponentSyntaxStrict,
		Root:        root,
	}
}

// TestTypedLegacyFrameProvesItsOwnMapAtTheStrictBoundary is gosx#240's
// unconditional-correctness proof at the render boundary. The typed legacy
// Row frame is entered by NAMED attributes, so it holds no raw source at
// all, yet its bare {...props} forward still renders: the frame's map is a
// faithful reading of a value of its declared type, so the boundary proves
// it key by key instead of rejecting it on the struct-kind check.
func TestTypedLegacyFrameProvesItsOwnMapAtTheStrictBoundary(t *testing.T) {
	prog := &ir.Program{}
	prog.Components = append(prog.Components, typedLegacyMarkComponent(prog))

	markCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Mark",
		Attrs: []ir.Attr{{Kind: ir.AttrSpread, Expr: "props"}},
	})
	rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "div", Children: []ir.NodeID{markCall}})
	prog.Components = append(prog.Components, ir.Component{
		Name:       "Row",
		PropsName:  "props",
		PropsType:  "RowProps",
		PropsTyped: true,
		Syntax:     ir.ComponentSyntaxLegacy,
		Root:       rowRoot,
	})

	rowCall := prog.AddNode(ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Row",
		Attrs: []ir.Attr{
			{Name: "tone", Kind: ir.AttrStatic, Value: "red"},
			{Name: "abbreviation", Kind: ir.AttrStatic, Value: "NE"},
		},
	})
	prog.Components = append(prog.Components, ir.Component{Name: "Page", Syntax: ir.ComponentSyntaxLegacy, Root: rowCall})

	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if want := "<div><span>redNE</span></div>"; html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestTypedLegacyFrameZeroFillsAnOmittedScalar covers the case the map path
// exists for. Row's caller omits abbreviation, so generated Go would supply
// the empty string; the boundary supplies the same empty string rather than
// observing a missing key, which is the divergence the struct-kind check
// guards against everywhere else.
func TestTypedLegacyFrameZeroFillsAnOmittedScalar(t *testing.T) {
	prog := &ir.Program{}
	prog.Components = append(prog.Components, typedLegacyMarkComponent(prog))

	markCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Mark",
		Attrs: []ir.Attr{{Kind: ir.AttrSpread, Expr: "props"}},
	})
	rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "div", Children: []ir.NodeID{markCall}})
	prog.Components = append(prog.Components, ir.Component{
		Name:       "Row",
		PropsName:  "props",
		PropsType:  "RowProps",
		PropsTyped: true,
		Syntax:     ir.ComponentSyntaxLegacy,
		Root:       rowRoot,
	})

	rowCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Row",
		Attrs: []ir.Attr{{Name: "tone", Kind: ir.AttrStatic, Value: "red"}},
	})
	prog.Components = append(prog.Components, ir.Component{Name: "Page", Syntax: ir.ComponentSyntaxLegacy, Root: rowCall})

	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if want := "<div><span>red</span></div>"; html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestTypedLegacyFrameFailsClosedOnAnOmittedStructField documents the one
// residual gap and proves it fails closed. A same-file struct has no zero
// value this boundary can build from a type NAME, so an omitted one is an
// error that names the field and the remedy, never a silently empty render.
func TestTypedLegacyFrameFailsClosedOnAnOmittedStructField(t *testing.T) {
	prog := &ir.Program{}
	leaf := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Team.Abbreviation"})
	markRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "span", Children: []ir.NodeID{leaf}})
	prog.Components = append(prog.Components, ir.Component{
		Name:        "Mark",
		PropsName:   "props",
		PropsType:   "MarkProps",
		PropsFields: map[string]string{"Team": "Team"},
		PropsPaths:  map[string]string{"Team.Abbreviation": "string"},
		Syntax:      ir.ComponentSyntaxStrict,
		Root:        markRoot,
	})

	markCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Mark",
		Attrs: []ir.Attr{{Kind: ir.AttrSpread, Expr: "props"}},
	})
	rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "div", Children: []ir.NodeID{markCall}})
	prog.Components = append(prog.Components, ir.Component{
		Name:       "Row",
		PropsName:  "props",
		PropsType:  "RowProps",
		PropsTyped: true,
		Syntax:     ir.ComponentSyntaxLegacy,
		Root:       rowRoot,
	})

	rowCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Row",
		Attrs: []ir.Attr{{Name: "tone", Kind: ir.AttrStatic, Value: "red"}},
	})
	prog.Components = append(prog.Components, ir.Component{Name: "Page", Syntax: ir.ComponentSyntaxLegacy, Root: rowCall})

	_, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err == nil {
		t.Fatal("RenderProgramComponent unexpectedly rendered an omitted struct field")
	}
	for _, want := range []string{
		"prop Team (Team)",
		"only a builtin scalar has a zero value this boundary can synthesize",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestStrictFrameForwardingIntoTypedLegacyFailsClosedWithoutARawSource
// covers the new strict-caller edge. A strict frame's own props map holds
// only the fields that frame renders, so forwarding the map itself would
// hand the typed legacy callee a value that is silently short of fields.
// The boundary refuses instead, and names the call shape that works.
func TestStrictFrameForwardingIntoTypedLegacyFailsClosedWithoutARawSource(t *testing.T) {
	prog := &ir.Program{}

	scoreExpr := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Abbreviation"})
	scoreRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "b", Children: []ir.NodeID{scoreExpr}})
	prog.Components = append(prog.Components, ir.Component{
		Name:       "Score",
		PropsName:  "props",
		PropsType:  "SideProps",
		PropsTyped: true,
		Syntax:     ir.ComponentSyntaxLegacy,
		Root:       scoreRoot,
	})

	scoreCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Score",
		Attrs: []ir.Attr{{Kind: ir.AttrSpread, Expr: "props"}},
	})
	shellRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "div", Children: []ir.NodeID{scoreCall}})
	prog.Components = append(prog.Components, ir.Component{
		Name:        "Shell",
		PropsName:   "props",
		PropsType:   "SideProps",
		PropsFields: map[string]string{"Tone": "string"},
		Syntax:      ir.ComponentSyntaxStrict,
		Root:        shellRoot,
	})

	shellCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Shell",
		Attrs: []ir.Attr{{Name: "Tone", Kind: ir.AttrStatic, Value: "red"}},
	})
	prog.Components = append(prog.Components, ir.Component{Name: "Page", Syntax: ir.ComponentSyntaxLegacy, Root: shellCall})

	_, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err == nil {
		t.Fatal("RenderProgramComponent unexpectedly forwarded a strict frame's reduced map")
	}
	want := "the enclosing strict frame's props came from named attributes"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
}

// TestStrictFrameForwardsItsRawSourceIntoTypedLegacy is the same edge with
// the call shape that works: Shell is entered by one spread, so its raw
// source is available and the typed legacy callee observes every field of
// it, not just the ones Shell renders.
func TestStrictFrameForwardsItsRawSourceIntoTypedLegacy(t *testing.T) {
	prog := &ir.Program{}

	scoreExpr := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Abbreviation"})
	scoreRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "b", Children: []ir.NodeID{scoreExpr}})
	prog.Components = append(prog.Components, ir.Component{
		Name:       "Score",
		PropsName:  "props",
		PropsType:  "SideProps",
		PropsTyped: true,
		Syntax:     ir.ComponentSyntaxLegacy,
		Root:       scoreRoot,
	})

	scoreCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Score",
		Attrs: []ir.Attr{{Kind: ir.AttrSpread, Expr: "props"}},
	})
	shellRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "div", Children: []ir.NodeID{scoreCall}})
	prog.Components = append(prog.Components, ir.Component{
		Name:        "Shell",
		PropsName:   "props",
		PropsType:   "SideProps",
		PropsFields: map[string]string{"Tone": "string"},
		Syntax:      ir.ComponentSyntaxStrict,
		Root:        shellRoot,
	})

	shellCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Shell",
		Attrs: []ir.Attr{{Kind: ir.AttrSpread, Expr: "side"}},
	})
	prog.Components = append(prog.Components, ir.Component{Name: "Page", Syntax: ir.ComponentSyntaxLegacy, Root: shellCall})

	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"side": routeTypedLegacySide{Tone: "red", Abbreviation: "NE"}},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if want := "<div><b>NE</b></div>"; html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestTypedLegacyComponentPropsKeepsTheMapBinding is the compatibility
// guard at the unit the retrofit could most easily have broken. A typed
// legacy callee still receives the flattened map componentProps has always
// built, extra keys and children included, so no legacy body observes its
// props differently than it did in v0.48.
func TestTypedLegacyComponentPropsKeepsTheMapBinding(t *testing.T) {
	comp := &ir.Component{
		Name:        "Card",
		PropsName:   "props",
		PropsType:   "CardProps",
		PropsTyped:  true,
		PropsFields: map[string]string{"Title": "string"},
		Syntax:      ir.ComponentSyntaxLegacy,
	}
	attrs := []ir.Attr{
		{Name: "title", Kind: ir.AttrStatic, Value: "T"},
		{Name: "kicker", Kind: ir.AttrStatic, Value: "K"},
	}
	props, rawSource, err := localComponentProps(comp, attrs, fileRenderEnv{}, gosx.RawHTML("<b>body</b>"))
	if err != nil {
		t.Fatalf("localComponentProps: %v", err)
	}
	if rawSource != nil {
		t.Fatalf("rawSource = %#v, want nil for a named-attribute call", rawSource)
	}
	for key, want := range map[string]string{"Title": "T", "title": "T", "Kicker": "K", "kicker": "K"} {
		if got, _ := props[key].(string); got != want {
			t.Fatalf("props[%q] = %#v, want %q", key, props[key], want)
		}
	}
	if _, ok := props["Children"]; !ok {
		t.Fatalf("props has no Children key: %#v", props)
	}
}

// TestTypedLegacyComponentPropsCarriesOnlyAStructRawSource pins
// structSpreadSource. A legacy call site may spread a map, and no strict
// boundary can prove one, so carrying it forward would only turn a
// provable frame map into an unprovable raw value.
func TestTypedLegacyComponentPropsCarriesOnlyAStructRawSource(t *testing.T) {
	comp := &ir.Component{
		Name:       "Card",
		PropsName:  "props",
		PropsType:  "CardProps",
		PropsTyped: true,
		Syntax:     ir.ComponentSyntaxLegacy,
	}
	env := fileRenderEnv{values: map[string]any{
		"side": routeTypedLegacySide{Tone: "red", Abbreviation: "NE"},
		"raw":  map[string]any{"Tone": "red"},
	}}
	for _, tc := range []struct {
		name       string
		expr       string
		wantSource bool
	}{
		{name: "struct", expr: "side", wantSource: true},
		{name: "map", expr: "raw", wantSource: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props, rawSource, err := localComponentProps(comp, []ir.Attr{{Kind: ir.AttrSpread, Expr: tc.expr}}, env, gosx.Node{})
			if err != nil {
				t.Fatalf("localComponentProps: %v", err)
			}
			if got := rawSource != nil; got != tc.wantSource {
				t.Fatalf("rawSource != nil = %v, want %v (%#v)", got, tc.wantSource, rawSource)
			}
			if got, _ := props["Tone"].(string); got != "red" {
				t.Fatalf("props[Tone] = %#v, want red", props["Tone"])
			}
		})
	}
}

// TestStrictScalarZeroValueCoversTheRendererBuiltins holds the two lists to
// one shape: every type strictScalarFieldType admits must have a zero value
// the frame path can synthesize, or an omitted field of that type would
// fail closed for no reason.
func TestStrictScalarZeroValueCoversTheRendererBuiltins(t *testing.T) {
	for _, fieldType := range []string{
		"string", "bool",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64",
	} {
		zero, ok := strictScalarZeroValue(fieldType)
		if !ok {
			t.Fatalf("strictScalarZeroValue(%q) reported no zero value", fieldType)
		}
		if _, err := requireStrictScalarType(zero, fieldType); err != nil {
			t.Fatalf("requireStrictScalarType(zero of %s): %v", fieldType, err)
		}
	}
	if _, ok := strictScalarZeroValue("Team"); ok {
		t.Fatal("strictScalarZeroValue accepted a same-file struct name")
	}
	if _, ok := strictScalarZeroValue("[]Row"); ok {
		t.Fatal("strictScalarZeroValue accepted a slice type")
	}
}
