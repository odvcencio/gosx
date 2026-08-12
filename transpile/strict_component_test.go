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
