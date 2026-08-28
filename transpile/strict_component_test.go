package transpile

import (
	"strings"
	"testing"
)

func TestTranspileStrictComponentToTypedGo(t *testing.T) {
	source := []byte(`package app

type CardProps struct { Label string }

component Card(props: CardProps) {
	return <label className="card" htmlFor="field">{props.Label}</label>
}

component Page() {
	return <Card Label="Ready" />
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	for _, want := range []string{
		`import gosx "m31labs.dev/gosx"`,
		`func Card(props CardProps, children ...gosx.Node) gosx.Node`,
		`gosx.Attr("class", "card")`,
		`gosx.Attr("for", "field")`,
		`func Page(children ...gosx.Node) gosx.Node`,
		`Card(CardProps{Label: "Ready"})`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTranspileStrictComponentAppliesFailClosedIRValidation(t *testing.T) {
	_, err := Transpile([]byte(`package app
component Page(props: Props) {
	value := props.Value
	return <main>{value}</main>
}
`), Options{SourceFile: "page.gsx"})
	if err == nil || !strings.Contains(err.Error(), "IR renderer cannot execute") {
		t.Fatalf("error = %v", err)
	}
}

func TestTranspileStrictComponentRespectsExistingGoSXImportStyle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		imp    string
		result string
		el     string
	}{
		{"alias", `gx "m31labs.dev/gosx"`, "gx.Node", `gx.El("main"`},
		{"dot", `. "m31labs.dev/gosx"`, "Node", `El("main"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\nimport " + tc.imp + "\ncomponent Page() {\nreturn <main>ok</main>\n}\n")
			out, err := Transpile(source, Options{SourceFile: "page.gsx"})
			if err != nil {
				t.Fatalf("Transpile: %v", err)
			}
			if !strings.Contains(out, "func Page(children ..."+tc.result+") "+tc.result) || !strings.Contains(out, tc.el) {
				t.Fatalf("import style not respected:\n%s", out)
			}
		})
	}
}

func TestTranspileStrictComponentInjectsCollisionSafeGoSXAlias(t *testing.T) {
	source := []byte(`package app
import gosx "example.test/unrelated"
component Page() {
	return <main>ok</main>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if !strings.Contains(out, `import gosxgen1 "m31labs.dev/gosx"`) || !strings.Contains(out, `func Page(children ...gosxgen1.Node) gosxgen1.Node`) {
		t.Fatalf("collision-safe alias missing:\n%s", out)
	}
}

func TestTranspileStrictComponentMapsFieldsPointersAndInitialisms(t *testing.T) {
	source := []byte(`package app
type LinkProps struct {
	Label string
	HTMLFor string
	URL string
	hidden string
}
component Link(props: *LinkProps) {
	return <a>{props.Label}</a>
}
component Page() {
	return <Link label="Docs" htmlFor="field" url="/docs" />
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	want := `Link(&LinkProps{Label: "Docs", HTMLFor: "field", URL: "/docs"})`
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

func TestTranspileNoPropsStrictComponentAttrsFailClosed(t *testing.T) {
	source := []byte(`package app
component Badge() {
	return <span>badge</span>
}
component Page() {
	return <Badge bogus="x" />
}
`)
	_, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err == nil || !strings.Contains(err.Error(), "does not accept props") {
		t.Fatalf("error = %v", err)
	}
}

// TestTranspileStrictConcatAndCondEmitVerbatimGo covers the v0.42 extensions:
// a concat hole appears verbatim inside gosx.Attr/gosx.Expr, and a strict
// <If cond={...}> emits gosx.If(cond, gosx.Fragment(children...)).
func TestTranspileStrictConcatAndCondEmitVerbatimGo(t *testing.T) {
	source := []byte(`package app

type CardProps struct {
	Ready bool
	Tone  string
}

component Card(props: CardProps) {
	return <div class={"tone-" + props.Tone}>{"Rank " + props.Tone}<If cond={props.Ready}>ready</If><If cond={props.Ready == false}>not ready</If></div>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	for _, want := range []string{
		`gosx.Attr("class", "tone-" + props.Tone)`,
		`gosx.Expr("Rank " + props.Tone)`,
		`gosx.If(props.Ready, gosx.Fragment(gosx.Text("ready")))`,
		`gosx.If(props.Ready == false, gosx.Fragment(gosx.Text("not ready")))`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTranspileStrictConditionalHonorsGoSXAliasInjection mirrors
// TestTranspileStrictComponentRespectsExistingGoSXImportStyle and
// TestTranspileStrictComponentInjectsCollisionSafeGoSXAlias for the new <If>
// emission: it must reference whatever alias (or dot import) the rest of the
// file's gosx emission uses, exactly like every other gosxRef call.
func TestTranspileStrictConditionalHonorsGoSXAliasInjection(t *testing.T) {
	for _, tc := range []struct {
		name string
		imp  string
		want string
	}{
		{"alias", `gx "m31labs.dev/gosx"`, `gx.If(props.Ready, gx.Fragment(gx.Text("ready")))`},
		{"dot", `. "m31labs.dev/gosx"`, `If(props.Ready, Fragment(Text("ready")))`},
		{"collision", `gosx "example.test/unrelated"`, `gosxgen1.If(props.Ready, gosxgen1.Fragment(gosxgen1.Text("ready")))`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\nimport " + tc.imp + "\ntype Props struct { Ready bool }\ncomponent Page(props: Props) {\nreturn <main><If cond={props.Ready}>ready</If></main>\n}\n")
			out, err := Transpile(source, Options{SourceFile: "page.gsx"})
			if err != nil {
				t.Fatalf("Transpile: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("missing %q in:\n%s", tc.want, out)
			}
		})
	}
}

// TestTranspileStrictConditionalSelfClosingHasEmptyFragment covers the
// zero-children shape: <If cond={...} /> still emits a (empty) gosx.Fragment
// so If's single-child signature holds.
func TestTranspileStrictConditionalSelfClosingHasEmptyFragment(t *testing.T) {
	source := []byte(`package app
type Props struct { Ready bool }
component Page(props: Props) {
	return <main><If cond={props.Ready} /></main>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `gosx.If(props.Ready, gosx.Fragment())`; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

// TestTranspileStrictConditionalShadowedByLocalComponentStaysAComponentCall
// covers the shadow rule: a same-file strict component named If must keep
// its ordinary typed-composite-literal emission instead of the gosx.If
// builtin projection.
func TestTranspileStrictConditionalShadowedByLocalComponentStaysAComponentCall(t *testing.T) {
	source := []byte(`package app
type IfProps struct { Label string }
component If(props: IfProps) {
	return <em>{props.Label}</em>
}
component Page() {
	return <If label="shadowed" />
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if !strings.Contains(out, `If(IfProps{Label: "shadowed"})`) {
		t.Fatalf("shadowed If lost its component-call emission:\n%s", out)
	}
	if strings.Contains(out, "gosx.If(") {
		t.Fatalf("shadowed If incorrectly emitted the builtin:\n%s", out)
	}
}

// TestTranspileLegacyIfKeepsExistingEmission covers the compatibility note in
// spec section 6: legacy (non-strict) bodies never reach the new strict <If>
// emission path (t.strict stays 0), so their existing If(...) component-call
// projection is unchanged.
func TestTranspileLegacyIfKeepsExistingEmission(t *testing.T) {
	source := []byte(`package app
func If(cond bool, child Node) Node {
	return child
}
func Page() Node {
	return <main><If cond={true}>legacy</If></main>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if strings.Contains(out, "gosx.If(") {
		t.Fatalf("legacy body incorrectly used the strict builtin emission:\n%s", out)
	}
}

// TestTranspileStrictNestedSelectorEmitsVerbatimGo covers section 2.b's
// "no transpiler change" claim: a nested-selector text hole, a nested
// operand inside a concat chain, and a nested <If cond> selector all appear
// verbatim in the projected Go, exactly like a direct field read.
func TestTranspileStrictNestedSelectorEmitsVerbatimGo(t *testing.T) {
	source := []byte(`package app

type Team struct {
	City string
}

type Player struct {
	Name  string
	Ready bool
	Team  Team
}

type RowProps struct {
	Player Player
}

component Row(props: RowProps) {
	return <p class={"player-" + props.Player.Name}><If cond={props.Player.Ready}>{props.Player.Team.City}</If></p>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	for _, want := range []string{
		`func Row(props RowProps, children ...gosx.Node) gosx.Node`,
		`gosx.Attr("class", "player-" + props.Player.Name)`,
		`gosx.If(props.Player.Ready, gosx.Fragment(gosx.Expr(props.Player.Team.City)))`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTranspileStrictNestedSelectorForwardsStructPropVerbatim covers the
// generated-Go-caller path open question 1 describes: a strict component
// declared with a struct-typed field forwards it as a plain composite
// literal field, and the Go compiler — not the transpiler — proves the
// field exists with the declared type.
func TestTranspileStrictNestedSelectorForwardsStructPropVerbatim(t *testing.T) {
	source := []byte(`package app

type Player struct {
	Name string
}

type RowProps struct {
	Player Player
}

component Row(props: RowProps) {
	return <p>{props.Player.Name}</p>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if !strings.Contains(out, `func Row(props RowProps, children ...gosx.Node) gosx.Node`) {
		t.Fatalf("missing typed func signature:\n%s", out)
	}
	if !strings.Contains(out, `gosx.Expr(props.Player.Name)`) {
		t.Fatalf("missing verbatim nested read:\n%s", out)
	}
}

func TestTranspileStrictExplicitZeroPropsMatchFileRendererContract(t *testing.T) {
	source := []byte(`package app
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
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `Badge(BadgeProps{Count: 0, Enabled: false})`; !strings.Contains(out, want) {
		t.Fatalf("generated Go is missing %q:\n%s", want, out)
	}
}

// --- E1 (#182): strict <Each> ----------------------------------------------

// TestTranspileStrictEachEmitsGoSXMap covers design spec section 2.8: a
// strict <Each of={...} as="row"> projects onto gosx.Map, with the row
// element type named from the same-file struct schema and every
// binding-rooted read emitted verbatim so the Go compiler proves it inside
// the callback scope.
func TestTranspileStrictEachEmitsGoSXMap(t *testing.T) {
	source := []byte(`package app
type BreakdownRow struct {
	Scored bool
	Label  string
}
type RowProps struct {
	Breakdown []BreakdownRow
}
component Row(props: RowProps) {
	return <div>
		<Each of={props.Breakdown} as="row">
			<div data-scored={row.Scored}>{row.Label}</div>
		</Each>
	</div>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	for _, want := range []string{
		`gosx.Map(props.Breakdown, func(row BreakdownRow, _ int) gosx.Node { return gosx.Fragment(`,
		`gosx.Attr("data-scored", row.Scored)`,
		`gosx.Expr(row.Label)`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTranspileStrictEachWithIndexNamesTheCallbackParam covers the index
// binding: with index="i", the callback's second parameter is named i
// instead of the placeholder _, so a bare {i} read compiles as an ordinary
// int reference (compare the <If> emission tests above's alias-injection
// pattern).
func TestTranspileStrictEachWithIndexNamesTheCallbackParam(t *testing.T) {
	source := []byte(`package app
type BreakdownRow struct {
	Label string
}
type RowProps struct {
	Breakdown []BreakdownRow
}
component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row" index="i">{row.Label}{i}</Each></div>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `func(row BreakdownRow, i int) gosx.Node`; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	if want := `gosx.Expr(i)`; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

// TestTranspileStrictEachHonorsGoSXAliasInjection mirrors
// TestTranspileStrictConditionalHonorsGoSXAliasInjection for the Map
// emission: gosx.Map must reference whatever alias (or dot import) the
// rest of the file's gosx emission uses.
func TestTranspileStrictEachHonorsGoSXAliasInjection(t *testing.T) {
	source := []byte(`package app
import gx "m31labs.dev/gosx"
type Row struct {
	Label string
}
type Props struct {
	Rows []Row
}
component Page(props: Props) {
	return <div><Each of={props.Rows} as="row">{row.Label}</Each></div>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `gx.Map(props.Rows, func(row Row, _ int) gx.Node { return gx.Fragment(`; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

// TestTranspileStrictEachShadowedByLocalComponentStaysAComponentCall mirrors
// TestTranspileStrictConditionalShadowedByLocalComponentStaysAComponentCall:
// a same-file strict component named Each keeps its ordinary typed-call
// emission instead of the gosx.Map builtin projection.
func TestTranspileStrictEachShadowedByLocalComponentStaysAComponentCall(t *testing.T) {
	source := []byte(`package app
type EachProps struct {
	Label string
}
component Each(props: EachProps) {
	return <em>{props.Label}</em>
}
component Page() {
	return <Each label="shadowed" />
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `Each(EachProps{Label: "shadowed"})`; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	if strings.Contains(out, "gosx.Map") {
		t.Fatalf("shadowed Each must not emit gosx.Map:\n%s", out)
	}
}

// --- E2 (#184): spread props at strict call sites -------------------------

// TestTranspileStrictTierOneSpreadEmitsVerbatimCall covers design spec
// section 3.2's check-program encoding: a proven tier-1 spread emits the
// call verbatim — Callee(<source>) — instead of a composite literal, so
// the Go compiler proves type identity with zero synthesis.
func TestTranspileStrictTierOneSpreadEmitsVerbatimCall(t *testing.T) {
	source := []byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
component Wrap(props: TeamMarkProps) {
	return <div><TeamMark {...props}></TeamMark></div>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `TeamMark(props)`; !strings.Contains(out, want) {
		t.Fatalf("missing verbatim spread call %q in:\n%s", want, out)
	}
	if strings.Contains(out, "TeamMarkProps{") {
		t.Fatalf("tier-1 spread must not synthesize a composite literal:\n%s", out)
	}
}

// TestTranspileStrictTierOneSpreadForwardsNestedFieldVerbatim covers a
// props field selector source (not bare props): the spread source's exact
// text is still emitted verbatim as the call argument.
func TestTranspileStrictTierOneSpreadForwardsNestedFieldVerbatim(t *testing.T) {
	source := []byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
type MatchupProps struct {
	Away TeamMarkProps
}
component Matchup(props: MatchupProps) {
	return <div><TeamMark {...props.Away}></TeamMark></div>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `TeamMark(props.Away)`; !strings.Contains(out, want) {
		t.Fatalf("missing verbatim spread call %q in:\n%s", want, out)
	}
}

// TestTranspileStrictLegacyTierTwoSpreadStillFailsFullTranspile covers
// non-goal 3.5/open question 5: a legacy body's spread into a strict
// callee is proved only at the file-renderer boundary, so full transpile
// keeps failing, now with the updated message naming the supported path.
func TestTranspileStrictLegacyTierTwoSpreadStillFailsFullTranspile(t *testing.T) {
	source := []byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
func Page() Node {
	team := map[string]any{"Tone": "red"}
	return <div><TeamMark {...team}></TeamMark></div>
}
`)
	_, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err == nil || !strings.Contains(err.Error(), "proven by the file renderer boundary, not by gosx transpile") {
		t.Fatalf("error = %v", err)
	}
}
