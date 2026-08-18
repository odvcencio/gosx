package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckFileRejectsLegacyPropsSpreadIntoStrict proves gosx#229's rule
// reaches the surface the two production instances went through. Both
// shipped because `gosx check` passed; this is the run that now stops them.
// The rule lives in the IR validation pass, so the same diagnostic reaches
// the check CLI, the build gate, and the LSP without any per-surface work.
func TestCheckFileRejectsLegacyPropsSpreadIntoStrict(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}
component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
}
func ScoreTeam(props any) Node {
	return <div class="score-team"><TeamMark {...props}></TeamMark></div>
}
component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil {
		t.Fatal("CheckFile unexpectedly accepted a strict spread from a legacy body's own props")
	}
	for _, want := range []string{
		"legacy component ScoreTeam cannot spread props into strict component TeamMark",
		"the strict spread boundary proves field coverage on struct values only",
		"declare ScoreTeam as a strict component",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want to contain %q", err, want)
		}
	}
}

// TestCheckFileAcceptsLegacyNestedFieldSpreadIntoStrict is the same run for
// the safe shape beside it: a legacy body spreading a struct-typed field of
// its props keeps checking clean.
func TestCheckFileAcceptsLegacyNestedFieldSpreadIntoStrict(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}
component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
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
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}
