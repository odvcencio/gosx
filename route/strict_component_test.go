package route

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

const strictInitialismSource = `package app
type LinkProps struct {
	HTMLFor string
	URL string
}
component AnchorLabel(props: LinkProps) {
	return <a for={props.HTMLFor} href={props.URL}>{props.HTMLFor}</a>
}
component Page() {
	return <AnchorLabel htmlFor="field" url="/docs" />
}
`

func TestRenderProgramComponentNormalizesStrictInitialismSchema(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictInitialismSource))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	for _, want := range []string{`for="field"`, `href="/docs"`, `>field</a>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %q", want, html)
		}
	}
}

func TestRenderProgramComponentPrefersStrictLocalOverBuiltin(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type LinkProps struct { Label string }
component Link(props: LinkProps) {
	return <strong>{props.Label}</strong>
}
component Page() {
	return <Link label="local" />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if html != "<strong>local</strong>" {
		t.Fatalf("strict Link rendered as builtin: %q", html)
	}
}

func TestRenderProgramComponentPrefersStrictLocalOverReplacement(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type SlotProps struct { Label string }
component Slot(props: SlotProps) {
	return <em>{props.Label}</em>
}
component Page() {
	return <Slot label="local" />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, replaced, err := renderFileProgramHTML(prog, "Page", fileRenderOptions{
		ComponentReplacements: map[string]string{"Slot": "replacement"},
	})
	if err != nil {
		t.Fatalf("renderFileProgramHTML: %v", err)
	}
	if replaced || html != "<em>local</em>" {
		t.Fatalf("strict Slot replacement won: html=%q replaced=%v", html, replaced)
	}
}

func TestDefaultFileRendererNormalizesStrictInitialismSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, []byte(strictInitialismSource), 0o600); err != nil {
		t.Fatal(err)
	}
	node, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `for="field"`) || !strings.Contains(html, `href="/docs"`) {
		t.Fatalf("initialism props were lost: %q", html)
	}
}

func TestDefaultFileRendererRejectsCrossFileStrictComponentWithoutShellingOut(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "badge.gsx"), []byte(`package app
component Badge() {
	return <span>badge</span>
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, []byte(`package app
component Page() {
	return <Badge />
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err == nil || !strings.Contains(err.Error(), "may call only same-file strict components") {
		t.Fatalf("DefaultFileRenderer error = %v", err)
	}
}

func TestRenderProgramComponentLeavesLegacyCalleeAttrNamesUnchanged(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
func Legacy(props Props) Node {
	return <p>{props.HtmlFor}</p>
}
func Page() Node {
	return <Legacy htmlFor="legacy" />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	page := prog.Components[1]
	call := prog.NodeAt(page.Root)
	if call == nil || len(call.Attrs) != 1 || call.Attrs[0].Name != "htmlFor" {
		t.Fatalf("legacy callee attr changed: %#v", call)
	}
}

func TestRenderProgramComponentCannotReceiveDivergentStrictExpression(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
type Props struct { A int; B int }
component Page(props: Props) {
	return <main>{props.A / props.B}</main>
}
`))
	if err == nil || !strings.Contains(err.Error(), `binary operator "/" is not supported`) {
		t.Fatalf("Compile error = %v", err)
	}
}

func TestRenderProgramComponentStrictSafeLiteralParity(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Page() {
	return <main>{"text"}{42}{1.5}{true}{false}</main>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if html != "<main>text421.5truefalse</main>" {
		t.Fatalf("html = %q", html)
	}
}

func TestStrictServerExpressionRejectsNilInTextAndAttrs(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "text", body: `<main>{nil}</main>`},
		{name: "attribute", body: `<main data-value={nil}>text</main>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gosx.Compile([]byte("package app\ncomponent Page() {\nreturn " + tc.body + "\n}\n"))
			if err == nil || !strings.Contains(err.Error(), "nil is not supported") {
				t.Fatalf("Compile error = %v", err)
			}
		})
	}
}

func TestStrictLocalCallRequiresEveryRenderedProp(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
type BadgeProps struct {
	Count int
	Enabled bool
	Unused string
}
component Badge(props: BadgeProps) {
	return <p>{(props).Count}:{props.Enabled}</p>
}
component Page() {
	return <Badge />
}
`))
	if err == nil {
		t.Fatal("Compile unexpectedly accepted omitted rendered props")
	}
	for _, want := range []string{"requires prop Count", "requires prop Enabled"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile error %q does not contain %q", err, want)
		}
	}
}

func TestRenderProgramComponentRejectsStrictRootProps(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type PageProps struct { Title string }
component Page(props: PageProps) {
	return <main>{props.Title}</main>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err == nil || !strings.Contains(err.Error(), "file renderer has no root props binding") {
		t.Fatalf("RenderProgramComponent error = %v", err)
	}
}

func TestDefaultFileRendererRejectsStrictPreferredRouteProps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, []byte(`package app
type PageProps struct { Title string }
component Page(props: PageProps) {
	return <main>{props.Title}</main>
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err == nil || !strings.Contains(err.Error(), "file renderer has no root props binding") {
		t.Fatalf("DefaultFileRenderer error = %v", err)
	}
}

func TestStrictLocalCallExplicitZeroValuesMatchGeneratedGo(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type BadgeProps struct {
	Count int
	Enabled bool
}
component Badge(props: BadgeProps) {
	return <p>{props.Count}:{props.Enabled}</p>
}
component Page() {
	return <Badge count={0} enabled={false} />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if html != "<p>0:false</p>" {
		t.Fatalf("html = %q", html)
	}
}

// TestStrictConcatAndCondParityWithGeneratedGo is the v0.42 render-parity
// test: it renders through renderFileProgramHTML and compares against the
// generated-Go equivalent (a hand-written gosx.El/gosx.If tree matching
// exactly what transpile.Transpile emits for this source), for both the true
// and false branches of the cond. Single-line JSX avoids a pre-existing,
// unrelated whitespace-handling difference between the transpile path (which
// drops whitespace-only text children) and the IR/file-render path (which
// renders them as a single space) — see the investigation note in this
// change's report; that gap predates this change and is not part of it.
func TestStrictConcatAndCondParityWithGeneratedGo(t *testing.T) {
	const source = `package app
type CardProps struct {
	Ready bool
	Tone  string
}
component Card(props: CardProps) {
	return <div class={"tone-" + props.Tone}><If cond={props.Ready}>ready</If><If cond={props.Ready == false}>not ready</If></div>
}
component Page() {
	return <Card ready={%s} tone="ok" />
}
`
	for _, ready := range []bool{true, false} {
		t.Run(strconv.FormatBool(ready), func(t *testing.T) {
			prog, err := gosx.Compile([]byte(fmt.Sprintf(source, strconv.FormatBool(ready))))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
			if err != nil {
				t.Fatalf("RenderProgramComponent: %v", err)
			}
			want := gosx.RenderHTML(gosx.El("div", gosx.Attrs(gosx.Attr("class", "tone-"+"ok")),
				gosx.If(ready, gosx.Fragment(gosx.Text("ready"))),
				gosx.If(ready == false, gosx.Fragment(gosx.Text("not ready"))),
			))
			if html != want {
				t.Fatalf("file render = %q, generated-Go render = %q", html, want)
			}
		})
	}
}

// TestStrictConcatEscapesHTMLInJoinedValue proves the concatenated result
// runs through the same HTML-escaping path as any other attribute value —
// concatenation happens before escaping, not after.
func TestStrictConcatEscapesHTMLInJoinedValue(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type CardProps struct { Tone string }
component Card(props: CardProps) {
	return <div class={"tone-" + props.Tone}>x</div>
}
component Page() {
	return <Card tone={"\"<script>\""} />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := gosx.RenderHTML(gosx.El("div", gosx.Attrs(gosx.Attr("class", "tone-"+`"<script>"`)), gosx.Text("x")))
	if html != want {
		t.Fatalf("file render = %q, generated-Go render = %q", html, want)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected the joined value to be HTML-escaped, got %q", html)
	}
}

// TestRequireStrictScalarTypeRejectsConcatBoundaryMismatch exercises the
// renderer boundary directly (requireStrictScalarType), mirroring
// TestRenderProgramComponentCannotReceiveDivergentStrictExpression: a
// same-file strict callee whose declared field is string, called with a
// non-string value, must fail closed at the render boundary rather than
// silently stringify.
func TestRequireStrictScalarTypeRejectsConcatBoundaryMismatch(t *testing.T) {
	if _, err := requireStrictScalarType(42, "string"); err == nil {
		t.Fatal("requireStrictScalarType(42, \"string\") unexpectedly accepted a non-string value")
	}
}

// TestIfConditionalParityMatchesGoSXIf checks writeConditional (the existing
// "If"/"Show"/"When" builtin) against gosx.If directly for both branches,
// confirming the v0.42 <If cond> extension needed zero renderer changes: the
// strict validator only narrows which shapes reach the pre-existing builtin.
func TestIfConditionalParityMatchesGoSXIf(t *testing.T) {
	const source = `package app
type Props struct { Ready bool }
component Card(props: Props) {
	return <main><If cond={props.Ready}>yes</If></main>
}
component Page() {
	return <Card ready={%s} />
}
`
	for _, ready := range []bool{true, false} {
		t.Run(strconv.FormatBool(ready), func(t *testing.T) {
			prog, err := gosx.Compile([]byte(fmt.Sprintf(source, strconv.FormatBool(ready))))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
			if err != nil {
				t.Fatalf("RenderProgramComponent: %v", err)
			}
			want := gosx.RenderHTML(gosx.El("main", nil, gosx.If(ready, gosx.Fragment(gosx.Text("yes")))))
			if got != want {
				t.Fatalf("ready=%v: file render = %q, gosx.If render = %q", ready, got, want)
			}
		})
	}
}

func TestStrictLocalCallAppliesGeneratedGoNumericConversions(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type NumericProps struct {
	N uint64
	F float32
}
component Numeric(props: NumericProps) {
	return <p>{props.N}:{props.F}</p>
}
component Page() {
	return <Numeric n={1e19} f={1.23456789} />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := gosx.RenderHTML(gosx.El("p",
		gosx.Expr(uint64(10000000000000000000)),
		gosx.Text(":"),
		gosx.Expr(float32(1.23456789)),
	))
	if html != want {
		t.Fatalf("file render = %q, generated-Go render = %q", html, want)
	}
}

// routeTestPlayer and routeTestTeam back the nested-selector renderer
// boundary tests below. They stand in for a same-file .gsx-declared struct:
// a real Go type, declared in this package, so reflect.Type.Name() and
// PkgPath() behave exactly as they would for a struct the strict lowerer
// resolved from a .gsx file's own schema.
type routeTestPlayer struct {
	Name string
	Team routeTestTeam
}

type routeTestTeam struct {
	City string
}

// routeTestTeamEmbed and routeTestPlayerEmbeddedPtr back the M2
// (requireStrictStructValue panic) tests below: a value-embedded and a
// pointer-embedded struct, both promoting City. A .gsx-compiled program can
// no longer produce PropsPaths naming a promoted field at all (B1 rejects
// it at compile time), so these stand in for a generated-Go caller or
// hand-built ir.Program whose runtime struct shape was never validated by
// the lowerer — exactly the "foreign type passing the nominal gate" case
// requireStrictStructValue's own boundary must still fail closed against.
type routeTestTeamEmbed struct {
	routeTestTeam
	Age int
}

type routeTestPlayerEmbeddedPtr struct {
	*routeTestTeam
	Age int
}

// TestRequireStrictStructValueAcceptsMatchingStruct is the struct-boundary
// accept case, mirroring TestRequireStrictScalarTypeRejectsConcatBoundaryMismatch's
// direct-call pattern for the new function.
func TestRequireStrictStructValueAcceptsMatchingStruct(t *testing.T) {
	value := routeTestPlayer{Name: "Ada", Team: routeTestTeam{City: "Springfield"}}
	got, err := requireStrictStructValue(value, "routeTestPlayer", map[string]string{
		"Name":      "string",
		"Team.City": "string",
	})
	if err != nil {
		t.Fatalf("requireStrictStructValue: %v", err)
	}
	if got != value {
		t.Fatalf("requireStrictStructValue returned %#v, want %#v", got, value)
	}
}

// TestRequireStrictStructValueRejectsBoundaryMismatches exercises the
// renderer boundary directly for every §2.b rejection shape reachable at
// the struct boundary: a wrong declared type name, a pointer masquerading
// as the value struct, a map masquerading as the struct, and a struct whose
// runtime leaf field does not match its declared scalar type.
func TestRequireStrictStructValueRejectsBoundaryMismatches(t *testing.T) {
	type otherStruct struct{ Name string }

	for _, tc := range []struct {
		name  string
		value any
		paths map[string]string
	}{
		{
			name:  "nil value",
			value: nil,
			paths: nil,
		},
		{
			name:  "wrong declared type",
			value: otherStruct{Name: "Ada"},
			paths: nil,
		},
		{
			name:  "pointer to the struct is not the struct",
			value: &routeTestPlayer{Name: "Ada"},
			paths: nil,
		},
		{
			name:  "map masquerading as the struct",
			value: map[string]any{"Name": "Ada"},
			paths: nil,
		},
		{
			name:  "leaf field type mismatch",
			value: struct{ Name int }{Name: 1},
			paths: map[string]string{"Name": "string"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := requireStrictStructValue(tc.value, "routeTestPlayer", tc.paths); err == nil {
				t.Fatalf("requireStrictStructValue(%#v) unexpectedly accepted", tc.value)
			}
		})
	}
}

// TestRequireStrictStructValueRejectsMissingNestedField covers a leaf path
// that does not exist on the runtime value at all — the "companion Go
// struct diverged from the .gsx schema copy" scenario section 5.4 assigns to
// strictcheck's Go-compiler-backed half; this proves the renderer boundary
// also fails closed for the same shape instead of panicking or rendering
// empty.
func TestRequireStrictStructValueRejectsMissingNestedField(t *testing.T) {
	value := routeTestPlayer{Name: "Ada"}
	if _, err := requireStrictStructValue(value, "routeTestPlayer", map[string]string{"Missing": "string"}); err == nil {
		t.Fatal("requireStrictStructValue unexpectedly accepted a path with no matching field")
	}
}

// TestRequireStrictStructValueRejectsPathThroughScalarField covers a
// registered sub-path that tries to select through a leaf that already
// bottomed out at a scalar (a slice-through-a-string shape mirroring
// section 2.b's "selector paths cross same-file struct fields only" rule,
// exercised here at the runtime boundary rather than at compile time).
func TestRequireStrictStructValueRejectsPathThroughScalarField(t *testing.T) {
	value := routeTestPlayer{Name: "Ada"}
	if _, err := requireStrictStructValue(value, "routeTestPlayer", map[string]string{"Name.Extra": "string"}); err == nil {
		t.Fatal("requireStrictStructValue unexpectedly accepted a path through a scalar field")
	}
}

// TestRequireStrictStructValueRejectsPromotedFieldWithoutPanicking is M2's
// core reproduction: value.FieldByName used to walk promotion and panic on
// this exact shape when the promoting field was a nil pointer (see the next
// test); here the embedding is a plain value, non-nil, so the pre-fix code
// did not panic — it silently accepted the promoted field and returned its
// value. The fix rejects a promoted field (StructField.Index length > 1) at
// the type level before ever touching the value, matching the lowerer's own
// rule that a promoted field cannot cross this boundary.
func TestRequireStrictStructValueRejectsPromotedFieldWithoutPanicking(t *testing.T) {
	value := routeTestTeamEmbed{routeTestTeam: routeTestTeam{City: "Springfield"}, Age: 7}
	_, err := requireStrictStructValue(value, "routeTestTeamEmbed", map[string]string{"City": "string"})
	if err == nil {
		t.Fatal("requireStrictStructValue unexpectedly accepted a promoted field")
	}
	want := "field City is a promoted (embedded) field"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
}

// TestRequireStrictStructValueRejectsNilEmbeddedPointerWithoutPanicking is
// M2's nil-pointer reproduction: reflect.Value.FieldByName indirects
// through every embedded pointer on the way to a promoted field and panics
// ("reflect: indirection through nil pointer to embedded struct") when one
// is nil. The Team pointer here is nil (zero value), so a pre-fix call
// would panic instead of returning an error; requireStrictStructValue must
// fail closed with an ordinary error, and RenderProgramComponent (the
// public API sitting above it) must never let that panic through.
func TestRequireStrictStructValueRejectsNilEmbeddedPointerWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("requireStrictStructValue panicked: %v", r)
		}
	}()
	value := routeTestPlayerEmbeddedPtr{Age: 3}
	_, err := requireStrictStructValue(value, "routeTestPlayerEmbeddedPtr", map[string]string{"City": "string"})
	if err == nil {
		t.Fatal("requireStrictStructValue unexpectedly accepted a field through a nil embedded pointer")
	}
	want := "field City is a promoted (embedded) field"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
}

// TestRenderProgramComponentDoesNotPanicOnNilEmbeddedPointerPromotedField is
// the end-to-end proof that M2's fix reaches the public API: a hand-built
// ir.Program feeding a nil-embedded-pointer struct through the render
// boundary (the same reflect.FieldByName panic vector, one layer up) must
// return an error from RenderProgramComponent, never panic out of it.
func TestRenderProgramComponentDoesNotPanicOnNilEmbeddedPointerPromotedField(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RenderProgramComponent panicked: %v", r)
		}
	}()
	prog := &ir.Program{}
	exprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Player.City"})
	rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "p", Children: []ir.NodeID{exprID}})
	prog.Components = append(prog.Components, ir.Component{
		Name:        "Row",
		PropsName:   "props",
		PropsType:   "RowProps",
		PropsFields: map[string]string{"Player": "routeTestPlayerEmbeddedPtr"},
		PropsPaths:  map[string]string{"Player.City": "string"},
		Syntax:      ir.ComponentSyntaxStrict,
		Root:        rowRoot,
	})
	rowCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Row",
		Attrs: []ir.Attr{{Name: "Player", Kind: ir.AttrExpr, Expr: "playerVar"}},
	})
	prog.Components = append(prog.Components, ir.Component{
		Name:   "Page",
		Syntax: ir.ComponentSyntaxLegacy,
		Root:   rowCall,
	})

	_, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"playerVar": routeTestPlayerEmbeddedPtr{Age: 3}},
	})
	if err == nil {
		t.Fatal("RenderProgramComponent unexpectedly accepted a nil-embedded-pointer promoted field")
	}
}

// TestStrictNestedSelectorRendersRealStructThroughRouteBoundary is the
// nested-selector render-parity proof. Open question 1 in the design spec
// notes that a strict .gsx caller cannot construct a struct value to
// forward — there is no composite-literal spelling in the strict surface —
// so no compiled .gsx source can feed a real struct through the file-route
// boundary today; "generated-Go callers feed them" is the documented path.
// This test exercises that same boundary code (writeLocalComponent ->
// localComponentProps -> strictComponentAttrValue -> requireStrictStructValue
// -> the callee body's props.Player.Name read through the pre-existing,
// unchanged selectValue chain in fileeval.go) with a hand-built ir.Program
// standing in for a generated-Go/typed caller, since ir.Program is plain
// data and gosx.Compile is not the only legitimate way to produce one.
func TestStrictNestedSelectorRendersRealStructThroughRouteBoundary(t *testing.T) {
	prog := &ir.Program{}

	// Row's body: <p>{props.Player.Name}</p>
	exprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Player.Name"})
	rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "p", Children: []ir.NodeID{exprID}})
	prog.Components = append(prog.Components, ir.Component{
		Name:        "Row",
		PropsName:   "props",
		PropsType:   "RowProps",
		PropsFields: map[string]string{"Player": "routeTestPlayer"},
		PropsPaths:  map[string]string{"Player.Name": "string"},
		Syntax:      ir.ComponentSyntaxStrict,
		Root:        rowRoot,
	})

	// Page's body: <Row player={playerVar} />. Page itself carries no
	// props (a zero-props Legacy root satisfies renderFileProgramHTML's
	// root-props gate the same way a real zero-props Page component would);
	// playerVar resolves through env.Values, standing in for a value a
	// generated-Go/typed caller would pass directly.
	rowCall := prog.AddNode(ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Row",
		Attrs: []ir.Attr{
			{Name: "Player", Kind: ir.AttrExpr, Expr: "playerVar"},
		},
	})
	prog.Components = append(prog.Components, ir.Component{
		Name:   "Page",
		Syntax: ir.ComponentSyntaxLegacy,
		Root:   rowCall,
	})

	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"playerVar": routeTestPlayer{Name: "Ada"}},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if html != "<p>Ada</p>" {
		t.Fatalf("html = %q, want %q", html, "<p>Ada</p>")
	}
}

// TestStrictNestedSelectorRejectsWrongTypedStructAtRouteBoundary is the
// reject half of the same end-to-end wiring: Row still declares its player
// prop as exactly routeTestPlayer, so a caller handing it any other struct
// — even one with a same-named, same-typed Name field, the classic "looks
// right by field but is not the declared type" mismatch — fails closed at
// the render boundary instead of rendering whatever the mismatched value
// happens to expose.
func TestStrictNestedSelectorRejectsWrongTypedStructAtRouteBoundary(t *testing.T) {
	type otherPlayer struct{ Name string }

	prog := &ir.Program{}
	exprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Player.Name"})
	rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "p", Children: []ir.NodeID{exprID}})
	prog.Components = append(prog.Components, ir.Component{
		Name:        "Row",
		PropsName:   "props",
		PropsType:   "RowProps",
		PropsFields: map[string]string{"Player": "routeTestPlayer"},
		PropsPaths:  map[string]string{"Player.Name": "string"},
		Syntax:      ir.ComponentSyntaxStrict,
		Root:        rowRoot,
	})
	rowCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Row",
		Attrs: []ir.Attr{{Name: "Player", Kind: ir.AttrExpr, Expr: "playerVar"}},
	})
	prog.Components = append(prog.Components, ir.Component{
		Name:   "Page",
		Syntax: ir.ComponentSyntaxLegacy,
		Root:   rowCall,
	})

	_, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"playerVar": otherPlayer{Name: "Ada"}},
	})
	if err == nil {
		t.Fatal("RenderProgramComponent unexpectedly accepted a struct of the wrong declared type")
	}
	if !strings.Contains(err.Error(), "render strict component Row") {
		t.Fatalf("error = %v, want it to name the failing strict component", err)
	}
}

// TestLocalComponentPropsResolvesAliasBeforePropsFieldsLookup is m5's
// reproduction: comp.PropsFields is keyed by the exact Go field spelling
// ("TeamName"), but a generated-Go caller or hand-built ir.Program can
// supply the lowerCamelInitialism alias ("teamName") directly, without ever
// running through the lowerer's own normalizeStrictComponentAttrs rewrite
// (that rewrite only fires for a call the lowerer itself compiled). Before
// the fix, an exact-key lookup on the alias name missed comp.PropsFields,
// "rendered" came back false, and the value skipped
// strictComponentAttrValue's whole type-checked boundary (conversion,
// requireStrictScalarType) while still landing in the runtime props map —
// a caller supplying the wrong type under an alias name sailed through
// un-rejected. The reject sub-test is the actual regression proof: a
// mistyped value under the alias must fail exactly as it would under the
// canonical spelling.
func TestLocalComponentPropsResolvesAliasBeforePropsFieldsLookup(t *testing.T) {
	newProg := func(attrExpr string) *ir.Program {
		prog := &ir.Program{}
		exprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.TeamName"})
		rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "p", Children: []ir.NodeID{exprID}})
		prog.Components = append(prog.Components, ir.Component{
			Name:        "Row",
			PropsName:   "props",
			PropsType:   "RowProps",
			PropsFields: map[string]string{"TeamName": "string"},
			Syntax:      ir.ComponentSyntaxStrict,
			Root:        rowRoot,
		})
		rowCall := prog.AddNode(ir.Node{
			Kind:  ir.NodeComponent,
			Tag:   "Row",
			Attrs: []ir.Attr{{Name: "teamName", Kind: ir.AttrExpr, Expr: attrExpr}},
		})
		prog.Components = append(prog.Components, ir.Component{
			Name:   "Page",
			Syntax: ir.ComponentSyntaxLegacy,
			Root:   rowCall,
		})
		return prog
	}

	t.Run("accepts a correctly typed value under the alias", func(t *testing.T) {
		html, err := RenderProgramComponent(newProg(`"Meteors"`), "Page", ProgramRenderEnv{})
		if err != nil {
			t.Fatalf("RenderProgramComponent: %v", err)
		}
		if html != "<p>Meteors</p>" {
			t.Fatalf("html = %q, want %q", html, "<p>Meteors</p>")
		}
	})

	t.Run("rejects a mistyped value under the alias instead of skipping the boundary", func(t *testing.T) {
		_, err := RenderProgramComponent(newProg("42"), "Page", ProgramRenderEnv{})
		if err == nil {
			t.Fatal("RenderProgramComponent unexpectedly accepted an int literal for an alias-named string prop")
		}
		want := "prop teamName (string)"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	})
}
