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
		`func Card(props CardProps) gosx.Node`,
		`gosx.Attr("class", "card")`,
		`gosx.Attr("for", "field")`,
		`func Page() gosx.Node`,
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
			if !strings.Contains(out, "func Page() "+tc.result) || !strings.Contains(out, tc.el) {
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
	if !strings.Contains(out, `import gosxgen1 "m31labs.dev/gosx"`) || !strings.Contains(out, `func Page() gosxgen1.Node`) {
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
