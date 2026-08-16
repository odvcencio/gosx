package gosx_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/strictcheck"
)

// checkFixtureInRealModule runs source through strictcheck.CheckFileWithOptions
// inside a throwaway module that replaces m31labs.dev/gosx with this
// checkout — the same real-module, Go-compiler-backed harness
// strict_surface_expansion_integration_test.go uses, reused here for the
// #182/#184 gridiron acceptance table (design spec section 6).
func checkFixtureInRealModule(t *testing.T, moduleName, fileName, source string) {
	t.Helper()
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := "module example.test/" + moduleName + "\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => " + filepath.ToSlash(root) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile("go.sum")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), goSum, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := strictcheck.CheckFileWithOptions(context.Background(), path, strictcheck.Options{GOFLAGS: "-mod=mod"}); err != nil {
		t.Fatalf("gosx check (strictcheck.CheckFileWithOptions): %v", err)
	}
}

// rosterRowFixtureSource is the design spec's section 6 post-extension
// RosterRow source (app/team/page.gsx), needing E1 (the Breakdown <Each>
// body loop) and E2 (the legacy call site's {...player} spread), trimmed
// to the fields the loop and its If siblings actually read. Loader structs
// (BreakdownRow, RosterCard) stand in for internal/league types, per
// section 5.7's fixture convention.
const rosterRowFixtureSource = `package app

type BreakdownRow struct {
	Scored bool
	Label  string
	Calc   string
	Points string
}

type RosterRowProps struct {
	Name           string
	HasBreakdown   bool
	Breakdown      []BreakdownRow
	BreakdownTotal string
}

component RosterRow(props: RosterRowProps) {
	return <div class="roster-row">
		<strong>{props.Name}</strong>
		<If cond={props.HasBreakdown}>
			<div class="stat-tip__rows">
				<Each of={props.Breakdown} as="row">
					<div class="stat-tip__row" data-scored={row.Scored}>
						<span>{row.Label}</span>
						<span class="mono">{row.Calc}</span>
						<b class="mono">{row.Points}</b>
					</div>
				</Each>
				<div class="stat-tip__total">
					<span>League scoring</span>
					<b class="mono">{props.BreakdownTotal}</b>
				</div>
			</div>
		</If>
		<If cond={props.HasBreakdown == false}>
			<p class="stat-tip__empty">No projection detail for this position.</p>
		</If>
	</div>
}

type RosterCard struct {
	Name           string
	HasBreakdown   bool
	Breakdown      []BreakdownRow
	BreakdownTotal string
}

func Page() Node {
	roster := []RosterCard{
		{Name: "Ada Lovelace", HasBreakdown: true, BreakdownTotal: "12.4", Breakdown: []BreakdownRow{
			{Scored: true, Label: "Pass Yds", Calc: "250/25", Points: "10.0"},
			{Scored: false, Label: "INT", Calc: "0", Points: "0"},
		}},
	}
	return <div>
		<Each of={roster} as="player">
			<RosterRow {...player}></RosterRow>
		</Each>
	</div>
}
`

// draftTeamFixtureSource is the design spec's section 6 post-extension
// DraftTeam source (app/draft/page.gsx): body qualifies since v0.42.0
// (concat class, If pairs); needs only E2 at the call site.
const draftTeamFixtureSource = `package app

type DraftTeamProps struct {
	OnClock      bool
	Tone         string
	Abbreviation string
	Ready        bool
	Autopick     bool
}

component DraftTeam(props: DraftTeamProps) {
	return <div class="draft-team" data-on-clock={props.OnClock}>
		<span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
		<If cond={props.Ready}><b class="ready-state is-ready">Ready</b></If>
		<If cond={props.Ready == false}><b class="ready-state">Not ready</b></If>
		<If cond={props.Autopick}><b class="autopick-badge mono">AUTO</b></If>
	</div>
}

type DraftSeat struct {
	OnClock      bool
	Tone         string
	Abbreviation string
	Ready        bool
	Autopick     bool
}

func Page() Node {
	teams := []DraftSeat{
		{OnClock: true, Tone: "red", Abbreviation: "NE", Ready: true, Autopick: false},
	}
	return <div>
		<Each of={teams} as="team">
			<DraftTeam {...team}></DraftTeam>
		</Each>
	</div>
}
`

// teamMarkFixtureCallSitesSource is the design spec's section 6
// post-extension TeamMark source (app/page.gsx): a concat-only strict
// body, called with {...props.away}/{...props.home} from a legacy
// MiniMatchup and {...props} from a legacy StandingRow — the two spread
// shapes the gridiron TeamMark call sites need.
const teamMarkFixtureCallSitesSource = `package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone} aria-hidden="true">{props.Abbreviation}</span>
}

type MatchupSide struct {
	Tone         string
	Abbreviation string
	Name         string
}

type MiniMatchupProps struct {
	Away MatchupSide
	Home MatchupSide
}

func MiniMatchup(props MiniMatchupProps) Node {
	return <div class="mini-matchup">
		<TeamMark {...props.Away}></TeamMark>
		<TeamMark {...props.Home}></TeamMark>
	</div>
}

func StandingRow(props TeamMarkProps) Node {
	return <div class="standing-row"><TeamMark {...props}></TeamMark></div>
}

func Page() Node {
	return <div>
		<MiniMatchup away={MatchupSide{Tone: "red", Abbreviation: "NE", Name: "Patriots"}} home={MatchupSide{Tone: "blue", Abbreviation: "BUF", Name: "Bills"}}></MiniMatchup>
		<StandingRow tone="red" abbreviation="NE"></StandingRow>
	</div>
}
`

// TestGridironAcceptanceRosterRowCompilesChecksAndRenders is the E1+E2
// acceptance fixture for design spec section 6's RosterRow row: it
// compiles the post-extension source (proving PropsSlices for the
// Breakdown loop) and checks it through the real Go-compiler-backed
// strictcheck pipeline. It does not additionally render the legacy Page
// entry: like the pre-existing BoardRow fixture (open question 1's
// reasoning), Page's local roster slice is a Go composite literal, which
// the file-interpreter render path does not execute — that is a full-
// transpile/compiled-Go concern strictcheck's real `go list -export`
// already covers, and render parity for the loop-plus-spread shape itself
// is proved directly against a hand-built ir.Program in
// route/strict_component_test.go's TestStrictEachRendersSliceParityWithGeneratedGo
// and TestStrictSpreadParityWithExplicitAttrCall.
func TestGridironAcceptanceRosterRowCompilesChecksAndRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(rosterRowFixtureSource))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	rosterRow := prog.Components[0]
	if rosterRow.Name != "RosterRow" {
		t.Fatalf("unexpected component order: %#v", prog.Components)
	}
	if rosterRow.PropsSlices["Breakdown"].Elem != "BreakdownRow" {
		t.Fatalf("PropsSlices[Breakdown].Elem = %q, want BreakdownRow", rosterRow.PropsSlices["Breakdown"].Elem)
	}

	checkFixtureInRealModule(t, "rosterrow", "rosterrow.gsx", rosterRowFixtureSource)
}

// TestGridironAcceptanceDraftTeamCompilesAndChecks is the E2 acceptance
// fixture for design spec section 6's DraftTeam row.
func TestGridironAcceptanceDraftTeamCompilesAndChecks(t *testing.T) {
	if _, err := gosx.Compile([]byte(draftTeamFixtureSource)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	checkFixtureInRealModule(t, "draftteam", "draftteam.gsx", draftTeamFixtureSource)
}

// TestGridironAcceptanceTeamMarkCallSitesCompileAndCheck is the E2
// acceptance fixture for design spec section 6's TeamMark row: both call
// shapes gridiron needs (a spread over a nested field, and a spread over a
// legacy body's own whole typed props parameter) compile and check clean.
func TestGridironAcceptanceTeamMarkCallSitesCompileAndCheck(t *testing.T) {
	if _, err := gosx.Compile([]byte(teamMarkFixtureCallSitesSource)); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	checkFixtureInRealModule(t, "teammarkcallsites", "teammark.gsx", teamMarkFixtureCallSitesSource)
}

// TestStrictcheckRejectsPromotedEachBindingHopZeroField is gosx#182/#184
// M-2's real-Go-compiler-backed proof, alongside the lowerer-level
// TestCompileStrictEachRejectsPromotedBindingHopZeroField: before the fix,
// the projected check program compiled clean here — the promoted field
// resolves fine in real Go, same package — which is exactly why the old
// "the package checker backstops it" comment was false for this root.
// strictcheck must reject it at the strict-syntax stage (the same
// diagnostic gosx.Compile produces), never reach the Go compiler at all.
func TestStrictcheckRejectsPromotedEachBindingHopZeroField(t *testing.T) {
	source := `package app
type Base struct { Label string }
type Row struct { Base; Points string }
type RowProps struct { Rows []Row }
component C(props: RowProps) {
	return <section><Each of={props.Rows} as="row"><span>{row.Label}</span></Each></section>
}
`
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := "module example.test/promotedhopzero\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => " + filepath.ToSlash(root) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile("go.sum")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), goSum, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "promotedhopzero.gsx")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	err = strictcheck.CheckFileWithOptions(context.Background(), path, strictcheck.Options{GOFLAGS: "-mod=mod"})
	if err == nil {
		t.Fatal("strictcheck unexpectedly accepted a promoted field read at an <Each> binding's hop 0")
	}
	if want := "declares no visible field Label"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}
