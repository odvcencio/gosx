package gosx

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestCompileSupportsLegacyAndStrictComponentStyles(t *testing.T) {
	source := []byte(`package app

type CardProps struct { Title string }

func Legacy() Node {
	return <p>legacy</p>
}

component Card(props: *CardProps) {
	return <article className="card"><label htmlFor="title">{props.Title}</label></article>
}
`)
	prog, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(prog.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(prog.Components))
	}
	legacy, strict := prog.Components[0], prog.Components[1]
	if legacy.Syntax != ir.ComponentSyntaxLegacy || strict.Syntax != ir.ComponentSyntaxStrict {
		t.Fatalf("unexpected syntax metadata: legacy=%v strict=%v", legacy.Syntax, strict.Syntax)
	}
	if strict.PropsName != "props" || strict.PropsType != "*CardProps" {
		t.Fatalf("strict props = %q %q", strict.PropsName, strict.PropsType)
	}
	root := prog.NodeAt(strict.Root)
	if root == nil || len(root.Attrs) != 1 || root.Attrs[0].Name != "class" {
		t.Fatalf("strict className alias was not normalized: %#v", root)
	}
	label := prog.NodeAt(root.Children[0])
	if label == nil || len(label.Attrs) != 1 || label.Attrs[0].Name != "for" {
		t.Fatalf("strict htmlFor alias was not normalized: %#v", label)
	}
}

func TestCompileLegacyAttributeNamesRemainUnchanged(t *testing.T) {
	prog, err := Compile([]byte(`package app
func Legacy() Node {
	return <label className="legacy" htmlFor="field">Legacy</label>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	root := prog.NodeAt(prog.Components[0].Root)
	if root == nil || len(root.Attrs) != 2 || root.Attrs[0].Name != "className" || root.Attrs[1].Name != "htmlFor" {
		t.Fatalf("legacy aliases changed: %#v", root)
	}
}

func TestCompileLegacyCodeMayStillUseComponentAsIdentifier(t *testing.T) {
	_, err := Compile([]byte(`package app
func Legacy() Node {
	component := "legacy"
	return <p>{component}</p>
}
`))
	if err != nil {
		t.Fatalf("legacy identifier became reserved: %v", err)
	}
}

func TestCompileStrictComponentAcceptsGoTypeGrammar(t *testing.T) {
	for _, propsType := range []string{
		"Props",
		"*Props",
		"pkg.Props",
		"pkg.Props[string]",
		"map[string][]pkg.Props[int]",
	} {
		source := []byte("package app\ncomponent Page(props: " + propsType + ") {\nreturn <main>typed</main>\n}\n")
		prog, err := Compile(source)
		if err != nil {
			t.Fatalf("type %q: %v", propsType, err)
		}
		if got := prog.Components[0].PropsType; got != propsType {
			t.Fatalf("type %q lowered as %q", propsType, got)
		}
	}
}

func TestCompileStrictServerBodyFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"local", `label := props.Label
	return <p>{label}</p>`, "statement the IR renderer cannot execute"},
		{"control-flow", `if false {
		return <p>wrong</p>
	}
	return <p>{props.Label}</p>`, "statement the IR renderer cannot execute"},
		{"helper-call", `return <p>{strings.ToUpper(props.Label)}</p>`, "calls are not supported"},
		{"free-helper", `return <p>{format(props.Label)}</p>`, "calls are not supported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\ncomponent Page(props: Props) {\n" + tc.body + "\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCompileStrictServerAllowsRendererSupportedPropsExpressions(t *testing.T) {
	source := []byte(`package app
type Props struct { Title string; Subtitle string }
component Page(props: Props) {
	return <main title={(props).Title}>{(props.Subtitle)}</main>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func TestCompileStrictServerRejectsNestedPropsSelectors(t *testing.T) {
	_, err := Compile([]byte(`package app
type Nested struct { Value string }
type Props struct { Nested *Nested }
component Page(props: Props) {
	return <main>{props.Nested.Value}</main>
}
`))
	if err == nil || !strings.Contains(err.Error(), "one field directly on props") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileStrictServerRestrictsRenderedPropFieldTypes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fieldType string
	}{
		{name: "named duration", fieldType: "Duration"},
		{name: "pointer", fieldType: "*int"},
		{name: "slice", fieldType: "[]string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\ntype Duration int64\ntype Props struct { Value " + tc.fieldType + " }\ncomponent Page(props: Props) {\nreturn <main>{props.Value}</main>\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), "renderer-visible props fields") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCompileStrictServerAcceptsExactScalarPropFieldTypes(t *testing.T) {
	for _, fieldType := range []string{"string", "bool", "int", "int64", "uint32", "float32", "float64"} {
		source := []byte("package app\ntype Props struct { Value " + fieldType + " }\ncomponent Page(props: Props) {\nreturn <main>{props.Value}</main>\n}\n")
		if _, err := Compile(source); err != nil {
			t.Fatalf("type %s: %v", fieldType, err)
		}
	}
}

func TestCompileStrictServerRejectsUnresolvedComponentTags(t *testing.T) {
	for _, tag := range []string{"External", "ui.Button"} {
		t.Run(tag, func(t *testing.T) {
			source := []byte("package app\ncomponent Page() {\nreturn <" + tag + " />\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), "not renderable") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCompileStrictServerRejectsHTMLElementSpread(t *testing.T) {
	_, err := Compile([]byte(`package app
type Props struct { Attrs map[string]any }
component Page(props: Props) {
	return <div {...props.Attrs}>text</div>
}
`))
	if err == nil || !strings.Contains(err.Error(), "spread attributes are not supported on strict server HTML elements") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileStrictServerRejectsTypeDivergentExpressions(t *testing.T) {
	for _, expression := range []string{
		"props.A / props.B",
		"props.OK && props.SideEffect()",
		"props.OK || props.SideEffect()",
		"props.A + props.B",
		"-props.A",
		"!props.OK",
		"props.Items[0]",
		"props.Formatter(props.Title)",
		"props.Normalize()",
	} {
		t.Run(expression, func(t *testing.T) {
			source := []byte("package app\ncomponent Page(props: Props) {\nreturn <main>{" + expression + "}</main>\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), "not supported by the strict server renderer") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCompileStrictServerRejectsLiteralSpellingsWithoutRuntimeParity(t *testing.T) {
	for _, literal := range []string{"'x'", "0xff", "0b10", "0o10", "01", "1_000", "1i", "0x1p2", "1_0.5", "nil"} {
		t.Run(literal, func(t *testing.T) {
			source := []byte("package app\ncomponent Page() {\nreturn <main>{" + literal + "}</main>\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), "not supported") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCompileStrictServerAcceptsParitySafeLiterals(t *testing.T) {
	for _, literal := range []string{`"text"`, "0", "42", "1.5", ".5", "1e3", "true", "false"} {
		source := []byte("package app\ncomponent Page() {\nreturn <main>{" + literal + "}</main>\n}\n")
		if _, err := Compile(source); err != nil {
			t.Fatalf("literal %s: %v", literal, err)
		}
	}
}

func TestCompileStrictLocalCallNormalizesSameFileInitialismFields(t *testing.T) {
	prog, err := Compile([]byte(`package app
type LinkProps struct {
	HTMLFor string
	URL string
}
component Link(props: LinkProps) {
	return <a href={props.URL}>{props.HTMLFor}</a>
}
component Page() {
	return <Link htmlFor="field" url="/docs" />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	page := prog.Components[1]
	call := prog.NodeAt(page.Root)
	if call == nil || len(call.Attrs) != 2 || call.Attrs[0].Name != "HTMLFor" || call.Attrs[1].Name != "URL" {
		t.Fatalf("initialism attrs not normalized in IR: %#v", call)
	}
}

func TestCompileStrictAmbiguousFieldAliasFailsClosed(t *testing.T) {
	_, err := Compile([]byte(`package app
type Props struct { URL string; Url string }
component Child(props: Props) {
	return <p>{props.URL}</p>
}
component Page() {
	return <Child url="/docs" />
}
`))
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileStrictCalleeRejectsDynamicCallShape(t *testing.T) {
	tests := []struct {
		name    string
		call    string
		message string
	}{
		{name: "spread", call: `<Badge {...props} />`, message: "spread attributes are not supported"},
		{name: "attribute", call: `<Badge bogus="x" />`, message: "does not accept props"},
		{name: "children", call: `<Badge>child</Badge>`, message: "does not accept positional children"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := `package app
component Badge() {
	return <span>badge</span>
}
component Page(props: Props) {
	return ` + test.call + `
}
`
			_, err := Compile([]byte(source))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestCompileStrictCalleeAcceptsEmptyCall(t *testing.T) {
	_, err := Compile([]byte(`package app
component Badge() {
	return <span>badge</span>
}
component Page() {
	return <Badge />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func TestCompileRejectsCrossStyleComponentCalls(t *testing.T) {
	for _, source := range []string{`package app
func Badge(attrs AttrList) Node {
	return <span>badge</span>
}
component Page() {
	return <Badge bogus="x">child</Badge>
}
	`, `package app
component Badge() {
	return <span>badge</span>
}
func Page() Node {
	return <Badge />
}
`} {
		_, err := Compile([]byte(source))
		if err == nil || !strings.Contains(err.Error(), "calls must stay within one style") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestCompileRejectsDuplicateComponentNamesAcrossStyles(t *testing.T) {
	for _, declarations := range []string{
		`component Badge() {
	return <strong>strict</strong>
}
func Badge() Node {
	return <span>legacy</span>
}`,
		`func Badge() Node {
	return <span>legacy</span>
}
component Badge() {
	return <strong>strict</strong>
}`,
		`component Badge() {
	return <strong>one</strong>
}
component Badge() {
	return <strong>two</strong>
}`,
	} {
		_, err := Compile([]byte("package app\n" + declarations + "\n"))
		if err == nil || !strings.Contains(err.Error(), `duplicate component name "Badge"`) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestCompileStrictPropsParameterMustBeNamedProps(t *testing.T) {
	_, err := Compile([]byte(`package app
component Page(input: Props) {
	return <main>{input.Title}</main>
}
`))
	if err == nil || !strings.Contains(err.Error(), "must be named props") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsStrictClientDirectiveComponents(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directive string
		root      string
		want      string
	}{
		{name: "island", directive: "//gosx:island", root: `<button>count</button>`, want: "strict island declarations are not supported"},
		{name: "engine", directive: "//gosx:engine surface", root: `<canvas />`, want: "strict engine declarations are not supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\n" + tc.directive + "\ncomponent Client() {\nreturn " + tc.root + "\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
