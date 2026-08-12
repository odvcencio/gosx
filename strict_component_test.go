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
		source := []byte("package app\ncomponent Page(props: " + propsType + ") {\nreturn <main>{props.Value}</main>\n}\n")
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
		{"helper-call", `return <p>{strings.ToUpper(props.Label)}</p>`, "call target must be rooted in props"},
		{"free-helper", `return <p>{format(props.Label)}</p>`, "call target must be rooted in props"},
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
component Page(props: Props) {
	return <main title={props.Title} data-first={props.Items[0]}>{props.Formatter(props.Title) + "!"}</main>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile: %v", err)
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

func TestCompileStrictIslandAllowsOnlyRecognizedDeclarations(t *testing.T) {
	valid := []byte(`package app
import "m31labs.dev/gosx/signal"

//gosx:island
component Counter(props: Props) {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <button onClick={increment}>{count.Get()}</button>
}
`)
	prog, err := Compile(valid)
	if err != nil {
		t.Fatalf("Compile valid island: %v", err)
	}
	if !prog.Components[0].IsIsland || prog.Components[0].Scope == nil {
		t.Fatalf("island metadata missing: %#v", prog.Components[0])
	}

	invalid := []byte(`package app
//gosx:island
component Counter(props: Props) {
	label := props.Label
	return <button>{label}</button>
}
`)
	_, err = Compile(invalid)
	if err == nil || !strings.Contains(err.Error(), "only signal/computed/handler short declarations") {
		t.Fatalf("error = %v", err)
	}
}
