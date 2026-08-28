package gosx_test

import (
	"errors"
	"strings"
	"testing"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
)

// --- gosx#229: a strict spread from a non-strict component's own props ----

// legacyPropsSpreadDiagnostic compiles source, requires exactly one lowering
// diagnostic, and returns it. Every gosx#229 rejection below asserts on the
// message AND on the span, because the whole point of the rule is that the
// author gets a source position for a failure that used to surface only as a
// render-time error with no position at all.
func legacyPropsSpreadDiagnostic(t *testing.T, source string) ir.Diagnostic {
	t.Helper()
	_, err := gosx.Compile([]byte(source))
	if err == nil {
		t.Fatal("Compile unexpectedly accepted a strict spread from a legacy body's own props")
	}
	var diagErr *ir.DiagnosticsError
	if !errors.As(err, &diagErr) {
		t.Fatalf("error %v is not an *ir.DiagnosticsError", err)
	}
	if len(diagErr.Diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %v", len(diagErr.Diagnostics), err)
	}
	return diagErr.Diagnostics[0]
}

// TestCompileRejectsUntypedLegacyPropsSpreadIntoStrict is gosx#229's
// headline shape, verbatim from the issue: a `props any` component
// bare-spreading into a strict callee. It shipped to production twice,
// passing gosx check both times, and failed on every execution.
func TestCompileRejectsUntypedLegacyPropsSpreadIntoStrict(t *testing.T) {
	diag := legacyPropsSpreadDiagnostic(t, `package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
}

func ScoreTeam(props any) Node {
	return <div class="score-team">
		<TeamMark {...props}></TeamMark>
	</div>
}
`)
	// gosx#240 narrowed the rule to the case it is unavoidable for, so the
	// message says "untyped legacy component" where it used to say "legacy
	// component": a typed legacy component is retrofitted now, not rejected.
	want := "untyped legacy component ScoreTeam cannot spread props into strict component TeamMark; an untyped legacy render frame binds props to map[string]any, and the strict spread boundary proves field coverage on struct values only"
	if diag.Message != want {
		t.Fatalf("message = %q, want %q", diag.Message, want)
	}
	if diag.Hint == "" || !strings.Contains(diag.Hint, "declare ScoreTeam as a strict component") {
		t.Fatalf("hint = %q, want the strict-component remedy", diag.Hint)
	}
	// The spread site, not the component declaration: line 14 is the
	// <TeamMark ...> call, column 3 its opening angle bracket.
	if diag.Span.StartLine != 14 || diag.Span.StartCol != 3 {
		t.Fatalf("span = %d:%d, want 14:3", diag.Span.StartLine, diag.Span.StartCol)
	}
}

// --- gosx#240: the typed legacy retrofit -----------------------------------

// TestTypedLegacyPropsSpreadIntoStrictRenders is gosx#240's headline shape,
// and the exact composition gosx#229 rejected under the name
// TestCompileRejectsTypedLegacyPropsSpreadIntoStrict. A legacy component
// that declares a props STRUCT declared in the same file now carries the
// schema a strict component carries, so its whole-props forward is proved at
// the declaration and renders. The test asserts HTML rather than a clean
// compile: the rejection it replaces existed because the composition
// compiled and then failed at every render.
func TestTypedLegacyPropsSpreadIntoStrictRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}

type ScoreTeamProps struct {
	Tone         string
	Abbreviation string
}

func ScoreTeam(props ScoreTeamProps) Node {
	return <div class="score-team"><TeamMark {...props}></TeamMark></div>
}

func Page() Node {
	return <div><ScoreTeam {...data.away}></ScoreTeam></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if comp := mustFindComponent(t, prog, "ScoreTeam"); !comp.PropsTyped {
		t.Fatal("ScoreTeam.PropsTyped = false, want true")
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{"away": strictSpreadTestSide{Tone: "red", Abbreviation: "NE"}},
	})
	want := `<div class="score-team"><span class="tone-red">NE</span></div>`
	if !strings.Contains(html, want) {
		t.Fatalf("rendered HTML %q does not contain %q", html, want)
	}
}

// TestTypedLegacyPropsSpreadRendersFromEveryCallShape is the property that
// made this retrofit acceptable where preserving a raw value beside the
// flattened map was not (gosx#229's adjacent question): the proof is made at
// the DECLARATION, so it does not turn on how any caller invokes the
// component. The same ScoreTeam body renders whether its caller spreads one
// struct into it or names its attributes one by one, including when the
// caller omits an attribute and Go would supply the zero value.
func TestTypedLegacyPropsSpreadRendersFromEveryCallShape(t *testing.T) {
	const prelude = `package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}

type ScoreTeamProps struct {
	Tone         string
	Abbreviation string
}

func ScoreTeam(props ScoreTeamProps) Node {
	return <div class="score-team"><TeamMark {...props}></TeamMark></div>
}
`
	for _, tc := range []struct {
		name string
		page string
		want string
	}{
		{
			name: "named attributes",
			page: `<div><ScoreTeam tone="blue" abbreviation="BUF"></ScoreTeam></div>`,
			want: `<div class="score-team"><span class="tone-blue">BUF</span></div>`,
		},
		{
			name: "one omitted attribute takes the Go zero value",
			page: `<div><ScoreTeam tone="blue"></ScoreTeam></div>`,
			want: `<div class="score-team"><span class="tone-blue"></span></div>`,
		},
		{
			name: "single struct spread",
			page: `<div><ScoreTeam {...data.away}></ScoreTeam></div>`,
			want: `<div class="score-team"><span class="tone-red">NE</span></div>`,
		},
		{
			name: "single map spread",
			page: `<div><ScoreTeam {...data.raw}></ScoreTeam></div>`,
			want: `<div class="score-team"><span class="tone-gold">GB</span></div>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := gosx.Compile([]byte(prelude + "\nfunc Page() Node {\n\treturn " + tc.page + "\n}\n"))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			html := mustRenderPage(t, prog, map[string]any{
				"data": map[string]any{
					"away": strictSpreadTestSide{Tone: "red", Abbreviation: "NE"},
					"raw":  map[string]any{"Tone": "gold", "Abbreviation": "GB"},
				},
			})
			if !strings.Contains(html, tc.want) {
				t.Fatalf("rendered HTML %q does not contain %q", html, tc.want)
			}
		})
	}
}

// TestTypedLegacyComponentNestsInsideStrictComponent covers the other
// direction of the same boundary: a strict body may now call a typed legacy
// component, by one spread or by named attributes. Both shapes were an
// outright cross-style error before gosx#240.
func TestTypedLegacyComponentNestsInsideStrictComponent(t *testing.T) {
	const prelude = `package app

type SideProps struct {
	Tone         string
	Abbreviation string
}

func ScoreTeam(props SideProps) Node {
	return <b class={"tone-" + props.Tone}>{props.Abbreviation}</b>
}
`
	for _, tc := range []struct {
		name string
		call string
	}{
		{name: "spread", call: `<ScoreTeam {...props}></ScoreTeam>`},
		{name: "named attributes", call: `<ScoreTeam tone={props.Tone} abbreviation={props.Abbreviation}></ScoreTeam>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := gosx.Compile([]byte(prelude + `
component Shell(props: SideProps) {
	return <div class="shell">` + tc.call + `</div>
}

func Page() Node {
	return <div><Shell {...data.away}></Shell></div>
}
`))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			html := mustRenderPage(t, prog, map[string]any{
				"data": map[string]any{"away": strictSpreadTestSide{Tone: "red", Abbreviation: "NE"}},
			})
			want := `<div class="shell"><b class="tone-red">NE</b></div>`
			if !strings.Contains(html, want) {
				t.Fatalf("rendered HTML %q does not contain %q", html, want)
			}
		})
	}
}

// TestTypedLegacyAndStrictNestBothWaysInOneTree renders one page whose tree
// alternates the two spellings — strict, then typed legacy, then strict —
// so the retrofit is proved as composition rather than as two separate one
// hop cases.
func TestTypedLegacyAndStrictNestBothWaysInOneTree(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type SideProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: SideProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}

func ScoreTeam(props SideProps) Node {
	return <b class="score-team"><TeamMark {...props}></TeamMark></b>
}

component Shell(props: SideProps) {
	return <div class="shell"><ScoreTeam {...props}></ScoreTeam></div>
}

func Page() Node {
	return <div><Shell {...data.away}></Shell></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{"away": strictSpreadTestSide{Tone: "red", Abbreviation: "NE"}},
	})
	want := `<div class="shell"><b class="score-team"><span class="tone-red">NE</span></b></div>`
	if !strings.Contains(html, want) {
		t.Fatalf("rendered HTML %q does not contain %q", html, want)
	}
}

// TestTypedLegacyPropsSpreadInsideEachRenders is the shape both production
// instances of gosx#229 actually had — the strict call sits inside a loop —
// spelled with a declared props struct. It rejected at compile time before
// gosx#240 and renders now.
func TestTypedLegacyPropsSpreadInsideEachRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

type MatchupCardProps struct {
	Tone string
	Rows []int
}

func MatchupCard(props MatchupCardProps) Node {
	return <div class="card">
		<Each of={props.Rows} as="row">
			<TeamMark {...props}></TeamMark>
		</Each>
	</div>
}

func Page() Node {
	return <div><MatchupCard tone="red" rows={data.rows}></MatchupCard></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{"rows": []int{1, 2}},
	})
	if got := strings.Count(html, "<span>red</span>"); got != 2 {
		t.Fatalf("rendered HTML %q has %d marks, want 2", html, got)
	}
}

// TestCompileRejectsTypedLegacyForwardMissingField proves the retrofit is a
// proof, not a waiver. A whole-props forward reads the CALLER's own fields,
// so the caller's struct must declare every field the callee renders. The
// diagnostic names the field, its type, and the read that needs it.
func TestCompileRejectsTypedLegacyForwardMissingField(t *testing.T) {
	diag := legacyPropsSpreadDiagnostic(t, `package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}{props.Abbreviation}</span>
}

type ScoreTeamProps struct {
	Tone string
}

func ScoreTeam(props ScoreTeamProps) Node {
	return <div><TeamMark {...props}></TeamMark></div>
}
`)
	want := "typed legacy component ScoreTeam cannot spread props into strict component TeamMark: ScoreTeamProps does not declare field Abbreviation (string), which TeamMark renders as props.Abbreviation"
	if diag.Message != want {
		t.Fatalf("message = %q, want %q", diag.Message, want)
	}
	if !strings.Contains(diag.Hint, "add Abbreviation string to ScoreTeamProps") {
		t.Fatalf("hint = %q, want the add-the-field remedy", diag.Hint)
	}
}

// TestCompileRejectsTypedLegacyForwardTypeMismatch is the same proof for a
// field that is declared but declared differently. Both names resolve in one
// file, so an identical spelling is an identical type and the check costs an
// author nothing that is not already true.
func TestCompileRejectsTypedLegacyForwardTypeMismatch(t *testing.T) {
	diag := legacyPropsSpreadDiagnostic(t, `package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

type ScoreTeamProps struct {
	Tone int
}

func ScoreTeam(props ScoreTeamProps) Node {
	return <div><TeamMark {...props}></TeamMark></div>
}
`)
	want := "typed legacy component ScoreTeam cannot spread props into strict component TeamMark: field Tone is int on ScoreTeamProps and string on TeamMarkProps; a whole-props forward needs the declared types to match"
	if diag.Message != want {
		t.Fatalf("message = %q, want %q", diag.Message, want)
	}
}

// TestCompileRejectsPropsSpreadFromAnotherFilesPropsType pins the retrofit's
// boundary. "Typed" means a struct declared in THIS .gsx file, the same
// same-file schema rule a strict component answers to: a props type declared
// in a sibling .go file has no schema the lowerer can prove against, so it
// stays an untyped legacy component and keeps the gosx#229 rejection.
func TestCompileRejectsPropsSpreadFromAnotherFilesPropsType(t *testing.T) {
	diag := legacyPropsSpreadDiagnostic(t, `package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func ScoreTeam(props ExternalProps) Node {
	return <div><TeamMark {...props}></TeamMark></div>
}
`)
	if !strings.HasPrefix(diag.Message, "untyped legacy component ScoreTeam cannot spread props into strict component TeamMark") {
		t.Fatalf("message = %q", diag.Message)
	}
}

// TestTypedLegacyComponentKeepsItsMapBinding is gosx#240's compatibility
// guard, and the reason the retrofit changes what a typed legacy component
// may COMPOSE with without changing how its body observes its props. A
// legacy body reads props through the flattened map the file renderer
// builds from the call site, and real programs depend on three properties
// of that map that a struct binding would have taken away:
//
//  1. children arrive under props.Children, which no props struct declares
//     (cmd/gosx/templates/docs/app/page.gsx does exactly this);
//  2. an attribute the declared struct does not name still arrives;
//  3. a spread source may be a map, not only a struct.
//
// All three keep working for a typed legacy component. The retrofit adds a
// schema; it does not replace the binding.
func TestTypedLegacyComponentKeepsItsMapBinding(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type CardProps struct {
	Title string
}

func Card(props CardProps) Node {
	return <section class="card"><h3>{props.Title}</h3><em>{props.Kicker}</em>{props.Children}</section>
}

func Spread(props CardProps) Node {
	return <p>{props.Title}|{props.Extra}</p>
}

func Page() Node {
	return <div>
		<Card title="T" kicker="K"><b>body</b></Card>
		<Spread {...data.card}></Spread>
	</div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if comp := mustFindComponent(t, prog, "Card"); !comp.PropsTyped {
		t.Fatal("Card.PropsTyped = false, want true")
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{"card": map[string]any{"Title": "T2", "Extra": "E"}},
	})
	for _, want := range []string{
		`<section class="card"><h3>T</h3><em>K</em><b>body</b></section>`,
		`<p>T2|E</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q does not contain %q", html, want)
		}
	}
}

// mustFindComponent returns the named component of prog, or fails.
func mustFindComponent(t *testing.T, prog *ir.Program, name string) ir.Component {
	t.Helper()
	for _, comp := range prog.Components {
		if comp.Name == name {
			return comp
		}
	}
	t.Fatalf("component %s not found in %#v", name, prog.Components)
	return ir.Component{}
}

// TestCompileRejectsSelfClosingLegacyPropsSpreadIntoStrict pins the rule to
// the call, not to the element spelling: a self-closing call site reaches
// the same validation and must report the same diagnostic.
func TestCompileRejectsSelfClosingLegacyPropsSpreadIntoStrict(t *testing.T) {
	diag := legacyPropsSpreadDiagnostic(t, `package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func ScoreTeam(props any) Node {
	return <div><TeamMark {...props} /></div>
}
`)
	if !strings.Contains(diag.Message, "cannot spread props into strict component TeamMark") {
		t.Fatalf("message = %q", diag.Message)
	}
}

// TestCompileRejectsLegacyPropsSpreadInsideEach covers the shape both
// production instances actually had: the strict call sits inside a loop, so
// it renders only once real data exists. The diagnostic must still fire at
// compile time.
func TestCompileRejectsLegacyPropsSpreadInsideEach(t *testing.T) {
	diag := legacyPropsSpreadDiagnostic(t, `package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func MatchupCard(props any) Node {
	return <div class="card">
		<Each of={props.Rows} as="row">
			<TeamMark {...props}></TeamMark>
		</Each>
	</div>
}
`)
	if !strings.Contains(diag.Message, "legacy component MatchupCard cannot spread props into strict component TeamMark") {
		t.Fatalf("message = %q", diag.Message)
	}
}

// TestCompileRejectsLegacyPropsSpreadForEveryStrictCalleeOccurrence proves
// the rule reports each offending call site rather than the first: two rows
// spreading into two strict marks are two separate defects to fix.
func TestCompileRejectsLegacyPropsSpreadForEveryStrictCalleeOccurrence(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

component OtherMark(props: TeamMarkProps) {
	return <b>{props.Tone}</b>
}

func ScoreTeam(props any) Node {
	return <div>
		<TeamMark {...props}></TeamMark>
		<OtherMark {...props}></OtherMark>
	</div>
}
`))
	var diagErr *ir.DiagnosticsError
	if !errors.As(err, &diagErr) {
		t.Fatalf("error %v is not an *ir.DiagnosticsError", err)
	}
	if len(diagErr.Diagnostics) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %v", len(diagErr.Diagnostics), err)
	}
}

// --- gosx#229: the shapes that must keep compiling -------------------------

// TestCompileAcceptsLegacyNestedFieldSpreadIntoStrict is the primary
// false-positive guard, and the shape the downstream audit documented as
// safe: a legacy body spreads a struct-typed FIELD of its props. The field
// value survives the shallow flatten with its own type intact, so the strict
// boundary proves it. This test renders the composition as well as compiling
// it, so a future tightening of the rule cannot pass by compiling alone.
func TestCompileAcceptsLegacyNestedFieldSpreadIntoStrict(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}

type MatchupSide struct {
	Tone         string
	Abbreviation string
}

type MiniMatchupProps struct {
	Away MatchupSide
}

func MiniMatchup(props MiniMatchupProps) Node {
	return <div class="mini"><TeamMark {...props.Away}></TeamMark></div>
}

func Page() Node {
	return <div><MiniMatchup away={data.away}></MiniMatchup></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{"away": strictSpreadTestSide{Tone: "red", Abbreviation: "NE"}},
	})
	if want := `<span class="tone-red">NE</span>`; !strings.Contains(html, want) {
		t.Fatalf("rendered HTML %q does not contain %q", html, want)
	}
}

// TestCompileAcceptsLegacyToLegacyPropsSpread guards the second documented
// safe shape: no strict boundary exists on a legacy-to-legacy spread, so the
// rule has nothing to say about it.
func TestCompileAcceptsLegacyToLegacyPropsSpread(t *testing.T) {
	if _, err := gosx.Compile([]byte(`package app

func Inner(props any) Node {
	return <span>inner</span>
}

func Outer(props any) Node {
	return <div><Inner {...props}></Inner></div>
}
`)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// TestCompileAcceptsPageDataSpreadIntoStrict guards the third documented
// safe shape, and the proven production pattern: a page entry with no props
// of its own spreads a loader value into a strict component.
func TestCompileAcceptsPageDataSpreadIntoStrict(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}

func Page() Node {
	return <div><TeamMark {...data.away}></TeamMark></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{"away": strictSpreadTestSide{Tone: "blue", Abbreviation: "BUF"}},
	})
	if want := `<span class="tone-blue">BUF</span>`; !strings.Contains(html, want) {
		t.Fatalf("rendered HTML %q does not contain %q", html, want)
	}
}

// TestCompileAcceptsStrictToStrictNamedAttributes guards the fourth
// documented safe shape: a strict caller passing named attributes never
// reaches the tier-2 rule at all.
func TestCompileAcceptsStrictToStrictNamedAttributes(t *testing.T) {
	if _, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}

component Row(props: TeamMarkProps) {
	return <div><TeamMark tone={props.Tone} abbreviation={props.Abbreviation}></TeamMark></div>
}
`)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// TestCompileAcceptsStrictBarePropsForward guards the tier-1 spread the
// gosx#229 diagnostic recommends as the fix: a STRICT caller forwarding its
// own props keeps working, because a strict frame carries the raw source
// value the boundary can prove.
func TestCompileAcceptsStrictBarePropsForward(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}

component StandingRow(props: TeamMarkProps) {
	return <div class="standing-row"><TeamMark {...props}></TeamMark></div>
}

func Page() Node {
	return <div><StandingRow {...data.away}></StandingRow></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{"away": strictSpreadTestSide{Tone: "red", Abbreviation: "NE"}},
	})
	if want := `<div class="standing-row"><span class="tone-red">NE</span></div>`; !strings.Contains(html, want) {
		t.Fatalf("rendered HTML %q does not contain %q", html, want)
	}
}

// TestCompileAcceptsLegacyLocalVariableSpreadIntoStrict guards a legacy body
// spreading anything that is not its own props identifier. The rule proves
// one source and says nothing about the rest.
func TestCompileAcceptsLegacyLocalVariableSpreadIntoStrict(t *testing.T) {
	if _, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func ScoreTeam(props any) Node {
	team := map[string]any{"Tone": "red"}
	return <div><TeamMark {...team}></TeamMark></div>
}
`)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// TestCompileAcceptsPropsSpreadFromAPropslessLegacyComponent pins the rule's
// declared scope. A legacy component that takes no parameter has no props
// binding for this rule to recognize, so a stray {...props} there is a
// different defect (an unbound identifier) with a different cause, and this
// rule stays silent about it.
func TestCompileAcceptsPropsSpreadFromAPropslessLegacyComponent(t *testing.T) {
	if _, err := gosx.Compile([]byte(`package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

func Page() Node {
	return <div><TeamMark {...props}></TeamMark></div>
}
`)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// --- gosx#230(b): nested struct fields prove structurally ------------------

// strictSpreadTestSide and strictSpreadTestRow stand in for the loader types
// a sibling page.server.go builds. Their names deliberately differ from the
// .gsx schema's own type names in every fixture below: proving that a
// converter type no longer has to be renamed, nor its nested model
// flattened, is the whole point of gosx#230's relaxation.
type strictSpreadTestSide struct {
	Tone         string
	Abbreviation string
}

type strictSpreadTestRow struct {
	Team strictSpreadTestSide
	Tone string
}

// TestSpreadProvesNestedStructFieldStructurally is gosx#230 ask 2: the .gsx
// declares its nested struct locally (rule 1 requires that), the sibling .go
// converter builds its own type, and the spread now proves the nested value
// by the fields the renderer reads rather than by the declared type's name.
// Before this change the render failed with "want exact struct Team", which
// is what forced downstream authors to flatten nested models into scalars.
func TestSpreadProvesNestedStructFieldStructurally(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type Team struct {
	Abbreviation string
}

type MarkProps struct {
	Team Team
	Tone string
}

component Mark(props: MarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Team.Abbreviation}</span>
}

func Page() Node {
	return <div><Mark {...data.row}></Mark></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html := mustRenderPage(t, prog, map[string]any{
		"data": map[string]any{
			"row": strictSpreadTestRow{
				Team: strictSpreadTestSide{Tone: "unused", Abbreviation: "NE"},
				Tone: "red",
			},
		},
	})
	if want := `<span class="tone-red">NE</span>`; !strings.Contains(html, want) {
		t.Fatalf("rendered HTML %q does not contain %q", html, want)
	}
}

// TestSpreadRejectsNestedStructMissingAReadField is the fail-closed half of
// the relaxation: structural means proved by the fields the renderer reads,
// not unproved. A nested value missing one of those fields still fails.
func TestSpreadRejectsNestedStructMissingAReadField(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type Team struct {
	Abbreviation string
}

type MarkProps struct {
	Team Team
	Tone string
}

component Mark(props: MarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Team.Abbreviation}</span>
}

func Page() Node {
	return <div><Mark {...data.row}></Mark></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	type wrongSide struct{ City string }
	type wrongRow struct {
		Team wrongSide
		Tone string
	}
	_, renderErr := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": map[string]any{"row": wrongRow{Team: wrongSide{City: "Foxborough"}, Tone: "red"}},
		},
	})
	if renderErr == nil {
		t.Fatal("render unexpectedly accepted a nested struct missing a read field")
	}
	if want := "field Abbreviation not found"; !strings.Contains(renderErr.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", renderErr, want)
	}
}

// TestSpreadRejectsNestedMapMasqueradingAsAStruct keeps the one shape the
// relaxation never admits: a map cannot prove field coverage, because a
// missing key is indistinguishable from a typed zero value in the
// generated-Go twin.
func TestSpreadRejectsNestedMapMasqueradingAsAStruct(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app

type Team struct {
	Abbreviation string
}

type MarkProps struct {
	Team Team
	Tone string
}

component Mark(props: MarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Team.Abbreviation}</span>
}

func Page() Node {
	return <div><Mark {...data.row}></Mark></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	type mapRow struct {
		Team map[string]any
		Tone string
	}
	_, renderErr := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": map[string]any{"row": mapRow{Team: map[string]any{"Abbreviation": "NE"}, Tone: "red"}},
		},
	})
	if renderErr == nil {
		t.Fatal("render unexpectedly accepted a nested map as a struct")
	}
	if want := "want a struct carrying the fields Team declares"; !strings.Contains(renderErr.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", renderErr, want)
	}
}

func mustRenderPage(t *testing.T, prog *ir.Program, values map[string]any) string {
	t.Helper()
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{Values: values})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	return html
}
