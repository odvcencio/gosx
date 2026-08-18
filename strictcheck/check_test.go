package strictcheck

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "gosxstub")
	mustMkdir(t, stub)
	mustWrite(t, filepath.Join(stub, "go.mod"), "module m31labs.dev/gosx\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(stub, "node.go"), `package gosx
type Node struct{}
type AttrValue struct{}
type AttrList []AttrValue
func El(string, ...any) Node { return Node{} }
func Text(string) Node { return Node{} }
func Expr(any) Node { return Node{} }
func RawHTML(string) Node { return Node{} }
func Fragment(...Node) Node { return Node{} }
func Attr(string, any) AttrValue { return AttrValue{} }
func Attrs(...AttrValue) any { return nil }
func Props(values ...AttrValue) AttrList { return values }
func Spread(any) AttrValue { return AttrValue{} }
func If(cond bool, child Node) Node { return Node{} }
func Map[T any](items []T, fn func(T, int) Node) Node { return Node{} }
`)
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.test/app\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => "+filepath.ToSlash(stub)+"\n")
	return dir
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func strictFixture(call string) string {
	return `package main

type LinkProps struct {
	Label string
	HTMLFor string
	URL string
}

component Link(props: *LinkProps) {
	return <a>{props.Label}</a>
}

component Page() {
	return ` + call + `
}
`
}

func TestCheckFileUsesRealGoTypesAndAllowsPackageMainWithoutMain(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	// A real companion with the first conventional overlay name must remain in
	// the build rather than being silently replaced by a projection.
	mustWrite(t, filepath.Join(dir, "zz_gosx_strictcheck_0.go"), "package main\nconst CompanionPresent = true\n")
	mustWrite(t, path, strictFixture(`<Link label="Docs" htmlFor="field" url="/docs" />`))
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

func TestCheckTreeChecksEachPackageClauseInOneDirectory(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "a.gsx"), `package legacy
func Legacy() Node {
	return <p>{data.missing}</p>
}
`)
	mustWrite(t, filepath.Join(dir, "z.gsx"), `package strictapp
type Props struct { Label string }
component Child(props: Props) {
	return <p>{props.Label}</p>
}
component Page() {
	return <Child label={42} />
}
`)
	for _, check := range []struct {
		name string
		fn   func() error
	}{
		{name: "tree", fn: func() error { return CheckTree(context.Background(), dir) }},
		{name: "package", fn: func() error { return CheckPackage(context.Background(), dir) }},
	} {
		t.Run(check.name, func(t *testing.T) {
			err := check.fn()
			if err == nil || !strings.Contains(err.Error(), "cannot use 42") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCheckFileRejectsUnknownAndWrongTypedPropsAtOriginalLine(t *testing.T) {
	for _, test := range []struct {
		name string
		call string
		want string
	}{
		{name: "unknown", call: `<Link mystery="x" />`, want: "unknown field Mystery"},
		{name: "wrong type", call: `<Link label={123} />`, want: "cannot use 123"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, strictFixture(test.call))
			err := CheckFile(context.Background(), path)
			if err == nil {
				t.Fatal("expected strict type error")
			}
			message := err.Error()
			if !strings.Contains(message, "page.gsx:14:") || !strings.Contains(message, test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCheckFileCompanionGoTypesRequireExactInitialism(t *testing.T) {
	for _, test := range []struct {
		name    string
		attr    string
		wantErr bool
	}{
		{name: "exact", attr: `URL="/docs"`},
		{name: "lower camel", attr: `url="/docs"`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := newTestModule(t)
			mustWrite(t, filepath.Join(dir, "props.go"), "package main\ntype LinkProps struct { URL string }\n")
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, `package main
component Link(props: *LinkProps) {
	return <a>link</a>
}
component Page() {
	return <Link `+test.attr+` />
}
`)
			err := CheckFile(context.Background(), path)
			if test.wantErr && (err == nil || !strings.Contains(err.Error(), "unknown field Url")) {
				t.Fatalf("error = %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("CheckFile: %v", err)
			}
		})
	}
}

func TestCheckFileProjectsOutInvalidLegacyDSLButChecksStrictCalls(t *testing.T) {
	for _, test := range []struct {
		name    string
		call    string
		wantErr bool
	}{
		{name: "valid strict", call: `<Card label="ok" />`},
		{name: "invalid strict", call: `<Card label={42} />`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, `package main
import legacy "example.test/undefined/legacy"
type CardProps struct { Label string }
func Legacy() Node {
	return <p>{legacy.Route(data.missing, request.user, unknownHelper())}</p>
}
component Card(props: CardProps) {
	return <p>{props.Label}</p>
}
component Page() {
	return `+test.call+`
}
`)
			err := CheckFile(context.Background(), path)
			if test.wantErr && (err == nil || !strings.Contains(err.Error(), "cannot use 42")) {
				t.Fatalf("error = %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("CheckFile: %v", err)
			}
		})
	}
}

func TestCheckFileIncludesPeerGSXTypesButRejectsCrossFileStrictCalls(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "types.gsx"), `package main
import "time"
type PageProps struct { When time.Time }
func Legacy() Node {
	return <p>{data.missing}</p>
}
`)
	page := filepath.Join(dir, "page.gsx")
	mustWrite(t, page, `package main
component Card(props: PageProps) {
	return <time>peer type</time>
}
component Page() {
	return <Card />
}
`)
	if err := CheckFile(context.Background(), page); err != nil {
		t.Fatalf("peer type check: %v", err)
	}

	mustWrite(t, filepath.Join(dir, "badge.gsx"), `package main
component Badge() {
	return <span />
}
`)
	mustWrite(t, page, `package main
component Page() {
	return <Badge />
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil || (!strings.Contains(err.Error(), "cross-file strict component call") && !strings.Contains(err.Error(), "may call only same-file strict components")) {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckFileResolvesVersionedImportPackageName(t *testing.T) {
	dir := newTestModule(t)
	dependency := filepath.Join(dir, "redisstub")
	mustWrite(t, filepath.Join(dependency, "go.mod"), "module example.test/go-redis/v9\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(dependency, "redis.go"), "package redis\ntype Client struct{}\n")
	goMod := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nrequire example.test/go-redis/v9 v9.0.0\nreplace example.test/go-redis/v9 => "+filepath.ToSlash(dependency)+"\n")...)
	if err := os.WriteFile(goMod, data, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
import "example.test/go-redis/v9"
type Props struct { Client *redis.Client }
component Card(props: Props) {
	return <main>redis</main>
}
component Page() {
	return <Card />
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

func TestCheckFileRetainsConstantsUsedByPropTypes(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
const ItemCount = 3
type Props struct { Values [ItemCount]string }
component Card(props: Props) {
	return <main>constants</main>
}
component Page() {
	return <Card />
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileAcceptsConcatAndCondTogether is the v0.42 acceptance case: a
// strict component that concatenates a string prop and gates a child on a
// bool prop through <If cond> must pass CheckFile end to end (IR lowering,
// Go-compiler projection, and go list).
func TestCheckFileAcceptsConcatAndCondTogether(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type CardProps struct {
	Ready bool
	Tone  string
}
component Card(props: CardProps) {
	return <div class={"tone-" + props.Tone}><If cond={props.Ready}>ready</If><If cond={props.Ready == false}>not ready</If></div>
}
component Page() {
	return <Card ready={true} tone="ok" />
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileRejectsConcatOfNonStringField is the strictcheck half of the
// concat type rule: a concat of an int prop must fail the generated check
// program, and the Go compiler's own diagnostic must still name the field.
func TestCheckFileRejectsConcatOfNonStringField(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type RowProps struct {
	Count int
}
component Row(props: RowProps) {
	return <p>{"Rank " + props.Count}</p>
}
component Page() {
	return <Row count={3} />
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "cannot concatenate props.Count") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileRejectsConcatOfUnknownField proves the Go-compiler backstop
// still owns unknown-field diagnostics for concat operands, exactly as it
// does for a v0.41 direct-field selector.
func TestCheckFileRejectsConcatOfUnknownField(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type RowProps struct {
	Tone string
}
component Row(props: RowProps) {
	return <p>{"tone-" + props.Tonee}</p>
}
component Page() {
	return <Row tone="ok" />
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "Tonee") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileRejectsCondOfNonBoolField is the strictcheck half of section
// 4.4's cond-type rule: cond on an int field must fail with a diagnostic
// naming the field, both at the IR gate and (if it ever got that far) at the
// Go compiler's "cannot use ... as bool" boundary.
func TestCheckFileRejectsCondOfNonBoolField(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type RowProps struct {
	Count int
}
component Row(props: RowProps) {
	return <p><If cond={props.Count}>x</If></p>
}
component Page() {
	return <Row count={3} />
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "cond requires an exact bool props field") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileAcceptsNestedSelector is the strictcheck half of extension
// (b): a two-hop selector through a same-file value struct passes the real
// Go compiler. Row is never called — open question 1 in the design spec
// notes that a strict .gsx caller has no composite-literal spelling to
// construct a struct prop, so a zero-props Page cannot feed Row real data
// (see route's TestStrictNestedSelectorRendersRealStructThroughRouteBoundary
// for the "generated-Go caller feeds it" path this proves compiles). The Go
// compiler still fully type-checks Row's declaration and body as an
// ordinary top-level func, uncalled or not.
func TestCheckFileAcceptsNestedSelector(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type Player struct {
	Name string
}
type RowProps struct {
	Player Player
}
component Row(props: RowProps) {
	return <p>{props.Player.Name}</p>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileRejectsNestedSelectorThroughPointerField proves the pointer
// rejection propagates through the full strictcheck pipeline: gosx.Compile
// runs first (transpile.Transpile re-runs the IR semantic gate), so the
// lowerer's rejection surfaces here exactly as it does through Compile
// directly.
func TestCheckFileRejectsNestedSelectorThroughPointerField(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type Player struct {
	Name string
}
type RowProps struct {
	Player *Player
}
component Row(props: RowProps) {
	return <p>{props.Player.Name}</p>
}
component Page() {
	return <Row />
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "pointer fields cannot preserve Go nil-pointer behavior") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileRejectsNestedSelectorUnknownLeafField covers section 5.4's
// Go-compiler backstop for a nested selector: an unresolvable leaf field
// (Nickname is not declared on Player) is exactly the class of mismatch
// resolveStrictSelectorPath intentionally leaves silent — an unknown field
// anywhere along a path returns early with no lowerer diagnostic, deferring
// to "the package checker supplies the precise unknown/unexported-field
// diagnostic" (the same rule the depth-1 case already followed). This
// proves the Go compiler is still the backstop for nested paths, including
// the companion-.go-file-diverged-from-the-.gsx-schema-copy shape section
// 5.4 describes, of which an unknown nested field is one concrete instance.
func TestCheckFileRejectsNestedSelectorUnknownLeafField(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type Player struct {
	Name string
}
type RowProps struct {
	Player Player
}
component Row(props: RowProps) {
	return <p>{props.Player.Nickname}</p>
}
component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "Nickname") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileAllowsPropsBearingStrictPageRenderEntry proves gosx#248's
// narrowing: a strict Page render entry that declares props now passes
// CheckFile, because renderFilePage binds it from this file's own Load hook
// (see route/filesystem.go and TestRenderFilePageBindsLoadReturnToStrictProps).
// A wrong-shaped or missing Load return still fails, but at render time, not
// here — see the strictSpreadProps-backed tests in route/fileprogram_test.go.
func TestCheckFileAllowsPropsBearingStrictPageRenderEntry(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type PageProps struct { Title string }
component Page(props: PageProps) {
	return <main>{props.Title}</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile error = %v, want a props-bearing Page entry to pass", err)
	}
}

// TestCheckFileRejectsPropsBearingStrictLayoutRenderEntry proves the
// narrowed refusal still applies to a layout: no code path calls a layout's
// own module's Load hook, so a layout's EntryProps is always nil (see
// route/filelayout.go's renderFileLayout), and validateStrictRenderEntries
// still catches this statically instead of deferring to a render-time
// failure that would fire on every request.
func TestCheckFileRejectsPropsBearingStrictLayoutRenderEntry(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "layout.gsx")
	mustWrite(t, path, `package main
type LayoutProps struct { Title string }
component Layout(props: LayoutProps) {
	return <main>{props.Title}</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "layout has no Load hook wired to its own root props") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

func TestCheckFileRejectsStrictServerComponentsOutsideFileRenderer(t *testing.T) {
	for _, tc := range []struct {
		name      string
		companion string
		source    string
	}{
		{
			name:      "companion Go AttrList",
			companion: "package main\nimport gosx \"m31labs.dev/gosx\"\nfunc External(attrs gosx.AttrList) gosx.Node { return gosx.Node{} }\n",
			source: `package main
component Page() {
	return <External label="x" />
}
`,
		},
		{
			name: "imported dotted component",
			source: `package main
import ui "example.test/ui"
component Page() {
	return <ui.Button />
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newTestModule(t)
			if tc.companion != "" {
				mustWrite(t, filepath.Join(dir, "companion.go"), tc.companion)
			}
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, tc.source)
			err := CheckFile(context.Background(), path)
			if err == nil || !strings.Contains(err.Error(), "not renderable") {
				t.Fatalf("CheckFile error = %v", err)
			}
		})
	}
}

func TestCheckTreeAllowsPropsBearingNonRouteComponentsAndIslands(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "gosxstub", "signal", "signal.go"), `package signal
type Signal[T any] struct { value T }
func New[T any](value T) *Signal[T] { return &Signal[T]{value: value} }
func (s *Signal[T]) Get() T { return s.value }
`)
	mustWrite(t, filepath.Join(dir, "player.gsx"), `package main
type PlayerProps struct { Name string }
component Player(props: PlayerProps) {
	return <strong>{props.Name}</strong>
}
`)
	mustWrite(t, filepath.Join(dir, "counter.gsx"), `package main
import "m31labs.dev/gosx/signal"
type CounterProps struct { Initial int }
//gosx:island
func Counter(props CounterProps) Node {
	count := signal.New(props.Initial)
	return <button>{count.Get()}</button>
}
`)
	if err := CheckTree(context.Background(), dir); err != nil {
		t.Fatalf("CheckTree: %v", err)
	}
}

func TestCheckFileRejectsStrictClientDirectiveComponents(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directive string
		want      string
	}{
		{name: "island", directive: "//gosx:island", want: "strict island declarations are not supported"},
		{name: "engine", directive: "//gosx:engine surface", want: "strict engine declarations are not supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, "client.gsx")
			mustWrite(t, path, "package main\n"+tc.directive+"\ncomponent Client() {\nreturn <canvas />\n}\n")
			err := CheckFile(context.Background(), path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CheckFile error = %v", err)
			}
		})
	}
}

func TestCheckTreeSkipsGeneratedHiddenAndNestedGitButChecksUnderscoreRoutes(t *testing.T) {
	invalid := strictFixture(`<Link label={123} />`)
	valid := strictFixture(`<Link label="ok" />`)

	t.Run("skip", func(t *testing.T) {
		dir := newTestModule(t)
		mustWrite(t, filepath.Join(dir, "page.gsx"), valid)
		for _, rel := range []string{
			"build/page.gsx", "vendor/page.gsx", "node_modules/page.gsx", "testdata/page.gsx", ".cache/page.gsx", "nested/page.gsx",
		} {
			mustWrite(t, filepath.Join(dir, rel), invalid)
		}
		mustMkdir(t, filepath.Join(dir, "nested", ".git"))
		if err := CheckTree(context.Background(), dir); err != nil {
			t.Fatalf("CheckTree: %v", err)
		}
	})

	for _, route := range []string{"_slug", "__path"} {
		t.Run(route, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, route, "page.gsx")
			mustWrite(t, path, invalid)
			err := CheckTree(context.Background(), dir)
			if err == nil || !strings.Contains(err.Error(), "page.gsx:14:") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCheckTreeChecksRoutableNestedNamedDirectories(t *testing.T) {
	for _, name := range []string{"build", "dist", "vendor", "testdata"} {
		t.Run(name, func(t *testing.T) {
			dir := newTestModule(t)
			path := filepath.Join(dir, "app", name, "page.gsx")
			mustWrite(t, path, strictFixture(`<Link label={123} />`))
			err := CheckTree(context.Background(), dir)
			if err == nil || !strings.Contains(err.Error(), "page.gsx:14:") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

// TestCheckTreeAccumulatesBuiltinFindingsAcrossTwoDirectoriesWithDifferentPackages
// is the gosx#186 B3 regression using only built-in findings (see
// extension_test.go's TestCheckTreeReportsLintFindingsFromEveryOffendingPackageNotJustTheFirst
// for the ExtraLints version). TestCheckTreeChecksEachPackageClauseInOneDirectory
// above put two package clauses in one directory, but only the second ever
// produced a finding, so it never actually exercised the stop-at-the-first-
// error bug. This uses two directories with two different package names,
// each with its own distinct, independently verifiable finding, and checks
// that CheckTree's error contains both rather than only the first.
func TestCheckTreeAccumulatesBuiltinFindingsAcrossTwoDirectoriesWithDifferentPackages(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "legacypkg", "page.gsx"), `package legacypkg

func Page(data any) Node {
	return <span>{data.picks.length}</span>
}
`)
	mustWrite(t, filepath.Join(dir, "widgets", "page.gsx"), `package widgets

type LinkProps struct {
	Label string
}

component Link(props: *LinkProps) {
	return <a>{props.Label}</a>
}

component Page() {
	return <Link label={42} />
}
`)

	err := CheckTree(context.Background(), dir)
	if err == nil {
		t.Fatal("expected findings from both packages")
	}
	message := err.Error()
	if !strings.Contains(message, "data.picks.length") {
		t.Fatalf("missing the legacypkg finding; message:\n%s", message)
	}
	if !strings.Contains(message, "cannot use 42") {
		t.Fatalf("missing the widgets finding; message:\n%s", message)
	}
}

func TestCheckFileWithOptionsAcceptsBuildEnvironment(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, strictFixture(`<Link label="ok" />`))
	err := CheckFileWithOptions(context.Background(), path, Options{
		Env:     os.Environ(),
		GOWORK:  "off",
		GOFLAGS: "-mod=mod -buildvcs=false",
	})
	if err != nil {
		t.Fatalf("CheckFileWithOptions: %v", err)
	}
}

func TestCommandEnvPreservesWorkspaceUnlessOverridden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("environment key casing differs on Windows")
	}
	got := commandEnv(Options{Env: []string{"PATH=/bin", "GOWORK=/workspace/go.work", "GOFLAGS=-tags=dev"}})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "GOWORK=/workspace/go.work") || !strings.Contains(joined, "GOFLAGS=-tags=dev") {
		t.Fatalf("environment not preserved: %v", got)
	}
	got = commandEnv(Options{Env: got, GOWORK: "off", GOFLAGS: "-mod=readonly"})
	joined = strings.Join(got, "\n")
	if strings.Contains(joined, "GOWORK=/workspace/go.work") || !strings.Contains(joined, "GOWORK=off") || !strings.Contains(joined, "GOFLAGS=-mod=readonly") {
		t.Fatalf("environment not overridden: %v", got)
	}
}

// TestCheckFileAcceptsTierOneSpreadCall covers design spec section 3.2
// (#184 E2) through the full strictcheck pipeline: a strict caller's spread
// source has exactly the callee's declared props type, so
// emitStrictComponent/strictSpreadCallVerbatim emits the call verbatim and
// the Go compiler proves it with zero synthesis.
func TestCheckFileAcceptsTierOneSpreadCall(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}
component TeamMark(props: TeamMarkProps) {
	return <span class={"tone-" + props.Tone}>{props.Abbreviation}</span>
}
component Wrap(props: TeamMarkProps) {
	return <div><TeamMark {...props}></TeamMark></div>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileRejectsTierOneSpreadWrongType proves the lowerer's tier-1
// identity check fails before the Go compiler ever sees a mismatched
// spread — a strict caller spreading a value whose declared type is not
// the callee's props type.
func TestCheckFileRejectsTierOneSpreadWrongType(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
type WrapProps struct {
	Tone string
}
component Wrap(props: WrapProps) {
	return <div><TeamMark {...props}></TeamMark></div>
}
component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "a strict caller spreads a value whose declared type is the callee props type") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileAcceptsStrictEachLoop is the check-program half of #182: a
// same-file strict <Each> over a []T slice of a same-file struct emits
// gosx.Map, and the Go compiler proves every binding-rooted read inside the
// callback, including a concat operand and an <If cond>.
func TestCheckFileAcceptsStrictEachLoop(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type BreakdownRow struct {
	Scored bool
	Label  string
	Points string
}
type RowProps struct {
	Breakdown []BreakdownRow
}
component Row(props: RowProps) {
	return <div>
		<Each of={props.Breakdown} as="row" index="i">
			<div class={"row-" + row.Label} data-scored={row.Scored}><If cond={row.Scored}>{row.Points}</If>{i}</div>
		</Each>
	</div>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileRejectsEachOverScalarSlice proves the lowerer's loopable-type
// table (design spec section 2.3) fails at the IR gate, before the Go
// compiler ever runs, with a message naming the field and its type.
func TestCheckFileRejectsEachOverScalarSlice(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type RowProps struct {
	Names []string
}
component Row(props: RowProps) {
	return <div>
		<Each of={props.Names} as="name">
			<span>{name}</span>
		</Each>
	</div>
}
component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "loop elements must be structs declared in this .gsx file") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileRejectsEachIndexBindingMisusedAsOperand proves an index
// binding's int-only contract fails at the IR gate: an index name used
// where a string field is required (a concat operand here) is a shape the
// syntactic validator's scope rules admit as a selector root candidate but
// the lowerer's type resolution rejects, since an index binding is never a
// struct.
func TestCheckFileRejectsEachIndexBindingMisusedAsOperand(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type BreakdownRow struct {
	Label string
}
type RowProps struct {
	Breakdown []BreakdownRow
}
component Row(props: RowProps) {
	return <div>
		<Each of={props.Breakdown} as="row" index="i">
			<span>{"row-" + i.Label}</span>
		</Each>
	</div>
}
component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "index binding i in a selector") {
		t.Fatalf("CheckFile error = %v", err)
	}
}

// TestCheckFileAcceptsCombinedEachSpreadConcatCondAndNestedSelector proves
// the full file combining E1, E2 tier 1, concat, <If>, and (b)'s nested
// selectors passes — the acceptance bar section 5.4 sets for the combined
// surface.
func TestCheckFileAcceptsCombinedEachSpreadConcatCondAndNestedSelector(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main
type BreakdownRow struct {
	Scored bool
	Label  string
}
type Team struct {
	City string
}
type RowProps struct {
	Breakdown []BreakdownRow
	Team      Team
}
component Row(props: RowProps) {
	return <div>
		<span>{"in-" + props.Team.City}</span>
		<Each of={props.Breakdown} as="row">
			<div class={"tone-" + row.Label}><If cond={row.Scored}>scored</If></div>
		</Each>
	</div>
}
component Wrap(props: RowProps) {
	return <div><Row {...props}></Row></div>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}
