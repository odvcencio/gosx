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

// routeTestBreakdownRow backs the E1 (#182) renderer-boundary tests below,
// standing in for a same-file .gsx-declared element struct the same way
// routeTestPlayer does for a nested-selector struct root.
type routeTestBreakdownRow struct {
	Scored bool
	Label  string
	Points string
}

// strictEachRowProgram builds the hand-written ir.Program every E1
// renderer-boundary test below shares: a strict Row component with a
// <Each of={props.Breakdown} as="row" index="i"> body, called from a
// zero-props legacy Page that hands the Breakdown slice through
// ProgramRenderEnv.Values — the same "generated-Go/typed caller" stand-in
// TestStrictNestedSelectorRendersRealStructThroughRouteBoundary uses, since
// the strict surface itself has no slice-literal spelling to construct one
// (open question 1's reasoning, generalized to a slice source).
func strictEachRowProgram() *ir.Program {
	prog := &ir.Program{}
	labelExprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "row.Label"})
	pointsExprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "row.Points"})
	indexExprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "i"})
	rowDivID := prog.AddNode(ir.Node{
		Kind: ir.NodeElement,
		Tag:  "div",
		Attrs: []ir.Attr{
			{Name: "data-scored", Kind: ir.AttrExpr, Expr: "row.Scored"},
		},
		Children: []ir.NodeID{labelExprID, pointsExprID, indexExprID},
	})
	eachID := prog.AddNode(ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Each",
		Attrs: []ir.Attr{
			{Name: "of", Kind: ir.AttrExpr, Expr: "props.Breakdown"},
			{Name: "as", Kind: ir.AttrStatic, Value: "row"},
			{Name: "index", Kind: ir.AttrStatic, Value: "i"},
		},
		Children: []ir.NodeID{rowDivID},
	})
	rowRoot := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "section", Children: []ir.NodeID{eachID}})
	prog.Components = append(prog.Components, ir.Component{
		Name:      "Row",
		PropsName: "props",
		PropsType: "RowProps",
		PropsFields: map[string]string{
			"Breakdown": "[]routeTestBreakdownRow",
		},
		PropsSlices: map[string]ir.SlicePropSchema{
			"Breakdown": {
				Elem: "routeTestBreakdownRow",
				Reads: map[string]string{
					"Scored": "bool",
					"Label":  "string",
					"Points": "string",
				},
			},
		},
		Syntax: ir.ComponentSyntaxStrict,
		Root:   rowRoot,
	})

	rowCall := prog.AddNode(ir.Node{
		Kind:  ir.NodeComponent,
		Tag:   "Row",
		Attrs: []ir.Attr{{Name: "Breakdown", Kind: ir.AttrExpr, Expr: "breakdownVar"}},
	})
	prog.Components = append(prog.Components, ir.Component{
		Name:   "Page",
		Syntax: ir.ComponentSyntaxLegacy,
		Root:   rowCall,
	})
	return prog
}

// TestStrictEachRendersSliceParityWithGeneratedGo is E1's render-parity
// proof: a three-element slice renders byte-identically to the
// hand-computed gosx.Map equivalent, including escaping, bool attr
// rendering (data-scored), and index text.
func TestStrictEachRendersSliceParityWithGeneratedGo(t *testing.T) {
	rows := []routeTestBreakdownRow{
		{Scored: true, Label: "Pass Yds", Points: "12.4"},
		{Scored: false, Label: "Rush <TD>", Points: "0"},
		{Scored: true, Label: "Rec", Points: "6"},
	}
	html, err := RenderProgramComponent(strictEachRowProgram(), "Page", ProgramRenderEnv{
		Values: map[string]any{"breakdownVar": rows},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	var want strings.Builder
	want.WriteString("<section>")
	for i, row := range rows {
		want.WriteString(gosx.RenderHTML(gosx.El("div", gosx.Attrs(gosx.Attr("data-scored", row.Scored)),
			gosx.Expr(row.Label), gosx.Expr(row.Points), gosx.Expr(i),
		)))
	}
	want.WriteString("</section>")
	if html != want.String() {
		t.Fatalf("file render = %q, generated-Go render = %q", html, want.String())
	}
}

// TestStrictEachEmptyAndNilSliceRenderEmptyString covers section 2.5: the
// strict form admits no fallback/empty attribute, so an empty or nil slice
// renders zero iterations on both the file renderer and the generated
// gosx.Map twin.
func TestStrictEachEmptyAndNilSliceRenderEmptyString(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []routeTestBreakdownRow
	}{
		{"empty", []routeTestBreakdownRow{}},
		{"nil", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html, err := RenderProgramComponent(strictEachRowProgram(), "Page", ProgramRenderEnv{
				Values: map[string]any{"breakdownVar": tc.rows},
			})
			if err != nil {
				t.Fatalf("RenderProgramComponent: %v", err)
			}
			if html != "<section></section>" {
				t.Fatalf("html = %q, want %q", html, "<section></section>")
			}
		})
	}
}

// TestStrictEachScopeIsolatesNestedBindings proves nested strict <Each>
// loops keep their bindings separate — the render-time scope chain
// (newest-first) matches the lexical shadow ban's guarantee, so the two
// cannot disagree (design spec section 2.2). It mirrors
// TestGSXEachScopeIsolatesItemBindings (route/gsxperf_test.go) for the
// strict surface, and doubles as the implicit-key-binding-absent proof: the
// strict Each path never binds an outer.rowKey-style name, so this
// component reads only its own declared bindings, unlike the legacy Each
// path this file's gsxperf_test.go exercises.
func TestStrictEachScopeIsolatesNestedBindings(t *testing.T) {
	prog := &ir.Program{}
	innerLabelID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "inner.Label"})
	innerSpanID := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "b", Children: []ir.NodeID{innerLabelID}})
	innerEachID := prog.AddNode(ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Each",
		Attrs: []ir.Attr{
			{Name: "of", Kind: ir.AttrExpr, Expr: "row.Stats"},
			{Name: "as", Kind: ir.AttrStatic, Value: "inner"},
		},
		Children: []ir.NodeID{innerSpanID},
	})
	outerLabelID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "row.Label"})
	outerDivID := prog.AddNode(ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "div",
		Children: []ir.NodeID{outerLabelID, innerEachID},
	})
	outerEachID := prog.AddNode(ir.Node{
		Kind: ir.NodeComponent,
		Tag:  "Each",
		Attrs: []ir.Attr{
			{Name: "of", Kind: ir.AttrExpr, Expr: "props.Rows"},
			{Name: "as", Kind: ir.AttrStatic, Value: "row"},
		},
		Children: []ir.NodeID{outerDivID},
	})
	rootID := prog.AddNode(ir.Node{Kind: ir.NodeElement, Tag: "section", Children: []ir.NodeID{outerEachID}})
	prog.Components = append(prog.Components, ir.Component{
		Name:      "Nested",
		PropsName: "props",
		PropsType: "NestedProps",
		PropsFields: map[string]string{
			"Rows": "[]routeTestOuterRow",
		},
		PropsSlices: map[string]ir.SlicePropSchema{
			"Rows": {Elem: "routeTestOuterRow", Reads: map[string]string{"Label": "string"}},
		},
		Syntax: ir.ComponentSyntaxStrict,
		Root:   rootID,
	})
	call := prog.AddNode(ir.Node{Kind: ir.NodeComponent, Tag: "Nested", Attrs: []ir.Attr{{Name: "Rows", Kind: ir.AttrExpr, Expr: "rowsVar"}}})
	prog.Components = append(prog.Components, ir.Component{Name: "Page", Syntax: ir.ComponentSyntaxLegacy, Root: call})

	rows := []routeTestOuterRow{
		{Label: "outer-1", Stats: []routeTestInnerStat{{Label: "a"}, {Label: "b"}}},
		{Label: "outer-2", Stats: []routeTestInnerStat{{Label: "c"}}},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"rowsVar": rows},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := "<section><div>outer-1<b>a</b><b>b</b></div><div>outer-2<b>c</b></div></section>"
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

type routeTestOuterRow struct {
	Label string
	Stats []routeTestInnerStat
}

type routeTestInnerStat struct {
	Label string
}

// TestRequireStrictSliceValueDirectRejections exercises
// requireStrictSliceValue directly, mirroring
// TestRequireStrictStructValueRejectsBoundaryMismatches's pattern for the
// slice boundary: []map[string]any (the wrong Kind of element), a wrong
// element type name, a wrong leaf type, and pointer elements each fail
// closed with a message naming what the renderer expected.
func TestRequireStrictSliceValueDirectRejections(t *testing.T) {
	schema := ir.SlicePropSchema{
		Elem:  "routeTestBreakdownRow",
		Reads: map[string]string{"Label": "string"},
	}
	type otherRow struct{ Label string }

	for _, tc := range []struct {
		name  string
		value any
	}{
		{"nil value", nil},
		{"not a slice", routeTestBreakdownRow{Label: "x"}},
		{"slice of maps", []map[string]any{{"Label": "x"}}},
		{"wrong element type name", []otherRow{{Label: "x"}}},
		{"pointer elements", []*routeTestBreakdownRow{{Label: "x"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := requireStrictSliceValue(tc.value, schema); err == nil {
				t.Fatalf("requireStrictSliceValue(%#v) unexpectedly accepted", tc.value)
			}
		})
	}

	t.Run("wrong leaf field type", func(t *testing.T) {
		type wrongLeafRow struct{ Label int }
		wrongSchema := ir.SlicePropSchema{Elem: "wrongLeafRow", Reads: map[string]string{"Label": "string"}}
		if _, err := requireStrictSliceValue([]wrongLeafRow{{Label: 1}}, wrongSchema); err == nil {
			t.Fatal("requireStrictSliceValue unexpectedly accepted a mismatched leaf field type")
		}
	})

	t.Run("typed nil slice passes and iterates zero times", func(t *testing.T) {
		var rows []routeTestBreakdownRow
		got, err := requireStrictSliceValue(rows, schema)
		if err != nil {
			t.Fatalf("requireStrictSliceValue(nil slice): %v", err)
		}
		if got == nil {
			t.Fatal("requireStrictSliceValue returned nil for a typed nil slice")
		}
	})

	// "promoted element field" extends gosx#183's M2 fix (reject a
	// promoted field by StructField.Index length before it can cross the
	// boundary) to E1's own element walk: reflect.Type.FieldByName also
	// resolves embedded-field promotion at the type level, so a bare
	// found check alone would let a slice element whose read only
	// resolves through embedding cross this boundary even though the
	// lowerer's walkStrictHops already refuses to compile a loop-binding
	// read through one (gosx#182/#184's generalized B1 fix).
	t.Run("promoted element field required by the renderer", func(t *testing.T) {
		type promotedElemInner struct{ City string }
		type promotedElemRow struct {
			promotedElemInner
			Age int
		}
		promotedSchema := ir.SlicePropSchema{Elem: "promotedElemRow", Reads: map[string]string{"City": "string"}}
		value := []promotedElemRow{{promotedElemInner: promotedElemInner{City: "Springfield"}, Age: 7}}
		_, err := requireStrictSliceValue(value, promotedSchema)
		if err == nil {
			t.Fatal("requireStrictSliceValue unexpectedly accepted a promoted element field")
		}
		want := "field City is a promoted (embedded) field"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	})
}

// --- E2 (#184): spread props at strict call sites -------------------------

type routeTestMatchupTeam struct {
	Tone         string
	Abbreviation string
}

// strictSpreadTeamMarkProgram builds the shared TeamMark component both the
// spread-parity test and the explicit-attr comparison call through, so the
// two render paths exercise the identical strict body.
func strictSpreadTeamMarkProgram(callAttrs []ir.Attr) *ir.Program {
	prog := &ir.Program{}
	toneExprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Tone"})
	abbrExprID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "props.Abbreviation"})
	rootID := prog.AddNode(ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "span",
		Attrs:    []ir.Attr{{Name: "class", Kind: ir.AttrExpr, Expr: `"tone-" + props.Tone`}},
		Children: []ir.NodeID{abbrExprID, toneExprID},
	})
	prog.Components = append(prog.Components, ir.Component{
		Name:      "TeamMark",
		PropsName: "props",
		PropsType: "TeamMarkProps",
		PropsFields: map[string]string{
			"Tone":         "string",
			"Abbreviation": "string",
		},
		Syntax: ir.ComponentSyntaxStrict,
		Root:   rootID,
	})
	call := prog.AddNode(ir.Node{Kind: ir.NodeComponent, Tag: "TeamMark", Attrs: callAttrs})
	prog.Components = append(prog.Components, ir.Component{Name: "Page", Syntax: ir.ComponentSyntaxLegacy, Root: call})
	return prog
}

// TestStrictSpreadParityWithExplicitAttrCall proves a legacy caller
// spreading a covering struct renders identically to the explicit-attr
// call with the same values — the twin-equivalence E2's whole design rests
// on (design spec section 3.1).
func TestStrictSpreadParityWithExplicitAttrCall(t *testing.T) {
	team := routeTestMatchupTeam{Tone: "red", Abbreviation: "NE"}

	spreadHTML, err := RenderProgramComponent(
		strictSpreadTeamMarkProgram([]ir.Attr{{Kind: ir.AttrSpread, Expr: "teamVar"}}),
		"Page", ProgramRenderEnv{Values: map[string]any{"teamVar": team}},
	)
	if err != nil {
		t.Fatalf("RenderProgramComponent (spread): %v", err)
	}

	explicitHTML, err := RenderProgramComponent(
		strictSpreadTeamMarkProgram([]ir.Attr{
			{Name: "Tone", Kind: ir.AttrExpr, Expr: "teamVar.Tone"},
			{Name: "Abbreviation", Kind: ir.AttrExpr, Expr: "teamVar.Abbreviation"},
		}),
		"Page", ProgramRenderEnv{Values: map[string]any{"teamVar": team}},
	)
	if err != nil {
		t.Fatalf("RenderProgramComponent (explicit): %v", err)
	}

	if spreadHTML != explicitHTML {
		t.Fatalf("spread render = %q, explicit-attr render = %q", spreadHTML, explicitHTML)
	}
	if want := `<span class="tone-red">NEred</span>`; spreadHTML != want {
		t.Fatalf("html = %q, want %q", spreadHTML, want)
	}
}

// TestStrictSpreadProps exercises strictSpreadProps directly for every
// design spec section 4.5 rejection shape: a nil source, a map source
// (rejected unconditionally, never zero-filled), a struct missing a
// rendered field, and a rendered field whose runtime type does not match
// its declared scalar type.
func TestStrictSpreadProps(t *testing.T) {
	comp := &ir.Component{
		Name: "TeamMark",
		PropsFields: map[string]string{
			"Tone":         "string",
			"Abbreviation": "string",
		},
	}

	t.Run("nil source", func(t *testing.T) {
		if _, err := strictSpreadProps(comp, nil); err == nil {
			t.Fatal("strictSpreadProps(nil) unexpectedly accepted")
		}
	})

	t.Run("map source rejected unconditionally", func(t *testing.T) {
		_, err := strictSpreadProps(comp, map[string]any{"Tone": "red", "Abbreviation": "NE"})
		if err == nil {
			t.Fatal("strictSpreadProps(map) unexpectedly accepted")
		}
		if !strings.Contains(err.Error(), "maps cannot prove field coverage") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing field", func(t *testing.T) {
		type partialTeam struct{ Tone string }
		_, err := strictSpreadProps(comp, partialTeam{Tone: "red"})
		if err == nil || !strings.Contains(err.Error(), "no field Abbreviation") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("wrong field type", func(t *testing.T) {
		type wrongTypeTeam struct {
			Tone         string
			Abbreviation int
		}
		_, err := strictSpreadProps(comp, wrongTypeTeam{Tone: "red", Abbreviation: 1})
		if err == nil || !strings.Contains(err.Error(), "want exact string") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("accepts a covering struct and proves values through setComponentProp aliases", func(t *testing.T) {
		props, err := strictSpreadProps(comp, routeTestMatchupTeam{Tone: "red", Abbreviation: "NE"})
		if err != nil {
			t.Fatalf("strictSpreadProps: %v", err)
		}
		if props["Tone"] != "red" || props["tone"] != "red" {
			t.Fatalf("props = %#v, want Tone/tone aliases set", props)
		}
	})

	// TestStrictSpreadProps/promoted field on the spread source fails
	// closed instead of zero-filling extends gosx#183's M2 fix (reject a
	// promoted field before Value.FieldByName can panic or silently
	// resolve one) to the E2 tier-2 spread boundary: a legacy caller's
	// spread source has no declared type at compile time, so
	// walkStrictHops' B1 rejection at lowering time never sees it — this
	// render-time boundary is the only place a source whose Tone field is
	// only reachable through embedding promotion is ever caught. Before
	// the fix, Value.FieldByName resolved the promotion (a non-nil,
	// non-pointer embed cannot panic the way M2's nil-embedded-pointer
	// case does) and let the promoted value cross the boundary un-proven,
	// the same silent-acceptance gap requireStrictStructValue's own M2 fix
	// closed for a nested-selector struct root — never a Go zero value
	// (strictSpreadProps' contract is to fail closed on any missing or
	// unprovable field, never to synthesize one the way the generated-Go
	// twin does for an explicit call's omitted attribute).
	t.Run("promoted field on the spread source fails closed instead of zero-filling", func(t *testing.T) {
		type toneHolder struct{ Tone string }
		type promotedSpreadSource struct {
			toneHolder
			Abbreviation string
		}
		source := promotedSpreadSource{toneHolder: toneHolder{Tone: "red"}, Abbreviation: "NE"}
		_, err := strictSpreadProps(comp, source)
		if err == nil {
			t.Fatal("strictSpreadProps unexpectedly accepted a promoted field on the spread source")
		}
		want := "field Tone is a promoted (embedded) field"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	})
}
