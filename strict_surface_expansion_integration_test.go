package gosx_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/strictcheck"
)

// teamMarkFixtureSource is the re-audit checkpoint from the design spec
// (GoSX Strict Component Expression Surface Expansion, section 5.7,
// TeamMark row): a component like `func TeamMark(props any)` rewritten as
// strict with `class={"team-mark tone-" + props.Tone}`, plus
// `<If cond={props.Ready}>` / `<If cond={props.Ready == false}>` in the same
// strict body — the exact pair the v0.42 (a)+(c) work packages exist to
// qualify.
func teamMarkFixtureSource(ready bool) string {
	return fmt.Sprintf(`package app

type TeamMarkProps struct {
	Tone  string
	Ready bool
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}><If cond={props.Ready}>ready</If><If cond={props.Ready == false}>pending</If></span>
}

component Page() {
	return <TeamMark tone="home" ready={%v} />
}
`, ready)
}

// TestStrictSurfaceExpansionTeamMarkFixtureCompilesChecksAndRenders is the
// integration fixture the re-audit checkpoint requires: it compiles the
// TeamMark shape, runs it through the real gosx-check pipeline (Go compiler
// backed, via strictcheck.CheckFileWithOptions against this module's real
// gosx package), and renders it, comparing against the generated-Go
// equivalent for both branches of the cond.
func TestStrictSurfaceExpansionTeamMarkFixtureCompilesChecksAndRenders(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(fmt.Sprintf("ready=%v", ready), func(t *testing.T) {
			source := teamMarkFixtureSource(ready)

			// 1. Compiles: the IR lowerer accepts concatenation of a string
			// props field and <If cond> on a bool props field together.
			prog, err := gosx.Compile([]byte(source))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}

			// 2. Checks: the Go-compiler-backed pipeline (strictcheck) accepts
			// the same source in a real module that depends on this gosx.
			root, err := filepath.Abs(".")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			goMod := "module example.test/teammark\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => " + filepath.ToSlash(root) + "\n"
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
			path := filepath.Join(dir, "teammark.gsx")
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := strictcheck.CheckFileWithOptions(context.Background(), path, strictcheck.Options{GOFLAGS: "-mod=mod"}); err != nil {
				t.Fatalf("gosx check (strictcheck.CheckFileWithOptions): %v", err)
			}

			// 3. Renders: the file-route renderer produces exactly what the
			// generated-Go equivalent produces, for this cond branch.
			html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{})
			if err != nil {
				t.Fatalf("RenderProgramComponent: %v", err)
			}
			want := gosx.RenderHTML(gosx.El("span", gosx.Attrs(gosx.Attr("class", "team-mark tone-"+"home")),
				gosx.If(ready, gosx.Fragment(gosx.Text("ready"))),
				gosx.If(ready == false, gosx.Fragment(gosx.Text("pending"))),
			))
			if html != want {
				t.Fatalf("file render = %q, generated-Go render = %q", html, want)
			}
		})
	}
}
