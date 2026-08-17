package gosx

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// TestCompileStrictServerRejectsNestedSelectorThroughPointerField flips
// polarity from the v0.41 blanket nested-selector rejection: a pointer
// intermediate stays rejected under extension (b), but the diagnostic now
// comes from the lowerer's path resolution (section 4.3), naming the
// component and the exact pointer type, because the syntax itself
// (props.Nested.Value) is now a valid shape — see
// TestCompileStrictServerAcceptsValueStructNestedSelectors below for the
// case this test used to conflate with pointer rejection.
func TestCompileStrictServerRejectsNestedSelectorThroughPointerField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Nested struct { Value string }
type Props struct { Nested *Nested }
component Page(props: Props) {
	return <main>{props.Nested.Value}</main>
}
`))
	want := "strict component Page cannot select through props.Nested of pointer type *Nested; pointer fields cannot preserve Go nil-pointer behavior in the file renderer"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerAcceptsValueStructNestedSelectors is the accept
// half of section 2.b: a two- and three-hop selector chain through same-file
// value (non-pointer) structs resolves to a scalar leaf and compiles.
func TestCompileStrictServerAcceptsValueStructNestedSelectors(t *testing.T) {
	source := []byte(`package app
type Team struct { City string }
type Player struct { Name string; Team Team }
type Props struct { Player Player }
component Row(props: Props) {
	return <p>{props.Player.Name}: {props.Player.Team.City}</p>
}
`)
	prog, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	row := prog.Components[0]
	if row.PropsFields["Player"] != "Player" {
		t.Fatalf("PropsFields[Player] = %q, want %q", row.PropsFields["Player"], "Player")
	}
	if row.PropsPaths["Player.Name"] != "string" {
		t.Fatalf("PropsPaths[Player.Name] = %q, want %q", row.PropsPaths["Player.Name"], "string")
	}
	if row.PropsPaths["Player.Team.City"] != "string" {
		t.Fatalf("PropsPaths[Player.Team.City] = %q, want %q", row.PropsPaths["Player.Team.City"], "string")
	}
}

// TestCompileStrictServerRejectsSelectorThroughScalarField covers section
// 4.3's first message: a selector tries to select a further field through
// an intermediate that already resolved to a renderer scalar (nothing to
// select through).
func TestCompileStrictServerRejectsSelectorThroughScalarField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Props struct { Player string }
component BoardRow(props: Props) {
	return <main>{props.Player.Name}</main>
}
`))
	want := `strict component BoardRow cannot select "Name" through props.Player of type string; selector paths cross same-file struct fields only`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerRejectsSelectorThroughUndeclaredStruct covers
// section 4.3's third message: an intermediate field's type is a plausible
// struct reference, but this .gsx file does not declare that struct.
func TestCompileStrictServerRejectsSelectorThroughUndeclaredStruct(t *testing.T) {
	_, err := Compile([]byte(`package app
type Player struct { Team Team }
type Props struct { Player Player }
component BoardRow(props: Props) {
	return <main>{props.Player.Team.City}</main>
}
`))
	want := "strict component BoardRow cannot resolve props.Player.Team.City: struct Team is not declared in this .gsx file; declare the renderer-visible struct beside the component"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerRejectsSelectorTooDeep covers section 4.3's fourth
// message: the lowerer's three-hop resolution cap. The syntax itself is
// valid (see TestValidateServerExpressionAcceptsNestedSelectors in
// internal/strictcomponent); the cap is a lowerer concern with full
// component context, not a schema-blind shape concern.
func TestCompileStrictServerRejectsSelectorTooDeep(t *testing.T) {
	_, err := Compile([]byte(`package app
type C struct { D string }
type B struct { C C }
type A struct { B B }
type Props struct { A A }
component BoardRow(props: Props) {
	return <main>{props.A.B.C.D}</main>
}
`))
	want := "strict component BoardRow selector props.A.B.C.D is too deep; the strict renderer resolves at most three fields"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerRejectsNestedSelectorNonScalarLeaf covers a leaf
// that resolves cleanly through the schema but is not itself a renderer
// scalar (a struct-typed leaf within the three-hop cap) — the same
// "renderer-visible props fields" message the depth-1 case already uses,
// generalized to a full path instead of a bare field name.
func TestCompileStrictServerRejectsNestedSelectorNonScalarLeaf(t *testing.T) {
	_, err := Compile([]byte(`package app
type Team struct { City string }
type Player struct { Team Team }
type Props struct { Player Player }
component BoardRow(props: Props) {
	return <main>{props.Player.Team}</main>
}
`))
	want := "strict component BoardRow cannot render props.Player.Team of type Team; renderer-visible props fields must use exact string, bool, integer, or floating-point builtins"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerRejectsNestedSelectorEmbeddedField covers section
// 2.b's embedded-field non-goal: collectStructSchemas only records
// field_identifier children, so a promoted field through an embedded struct
// is invisible to the same-file schema. Player also carries its own named
// sibling field (Age) so structTypes registers Player at all — with a
// host struct that has no named field of its own, Player never enters
// structTypes and the call fails on the separate "struct is not declared"
// gate instead of the promoted-field gate this test means to cover (this
// was B1: the promoted-field gate silently deferred at hop i>0 and let the
// call compile).
func TestCompileStrictServerRejectsNestedSelectorEmbeddedField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Team struct { City string }
type Player struct { Team; Age int }
type Props struct { Player Player }
component BoardRow(props: Props) {
	return <main>{props.Player.City}</main>
}
`))
	want := "strict component BoardRow cannot resolve props.Player.City: struct Player declares no visible field City; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerRejectsNestedSelectorUnexportedLeaf covers the
// other B1 shape: an unexported leaf field on a same-file declared struct
// (not promoted, just lowercase) hits the identical hop-i>0 unknown-field
// gate, since collectStructSchemas never records an unexported field at
// all.
func TestCompileStrictServerRejectsNestedSelectorUnexportedLeaf(t *testing.T) {
	_, err := Compile([]byte(`package app
type Team struct { city string; Zone string }
type Props struct { Team Team }
component BoardRow(props: Props) {
	return <main>{props.Team.city}</main>
}
`))
	want := "strict component BoardRow cannot resolve props.Team.city: struct Team declares no visible field city; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerAcceptsConcatOfNestedStringField covers the
// interaction note: a concat operand accepts a nested selector path, not
// just a direct field, in the same change that admits nested reads
// generally.
func TestCompileStrictServerAcceptsConcatOfNestedStringField(t *testing.T) {
	source := []byte(`package app
type Player struct { Name string }
type Props struct { Player Player }
component BoardRow(props: Props) {
	return <span class={"player-" + props.Player.Name}>{props.Player.Name}</span>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// TestCompileStrictServerRejectsConcatOfNestedNonStringField mirrors
// TestCompileStrictServerRejectsConcatOfNonStringField for a nested operand:
// the leaf must be exactly string, named by its full path.
func TestCompileStrictServerRejectsConcatOfNestedNonStringField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Player struct { Number int }
type Props struct { Player Player }
component BoardRow(props: Props) {
	return <span>{"No. " + props.Player.Number}</span>
}
`))
	want := `strict component BoardRow cannot concatenate props.Player.Number of type int; "+" operands must be exact string props fields`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerAcceptsConcatOfNestedFieldPointerRootStillFailsClosed
// proves the pointer rule applies uniformly to a concat operand, not just a
// bare nested read — the interaction note's "widened selector rule applies
// consistently wherever selectors are admitted" requirement.
func TestCompileStrictServerAcceptsConcatOfNestedFieldPointerRootStillFailsClosed(t *testing.T) {
	_, err := Compile([]byte(`package app
type Player struct { Name string }
type Props struct { Player *Player }
component BoardRow(props: Props) {
	return <span>{"player-" + props.Player.Name}</span>
}
`))
	want := "strict component BoardRow cannot select through props.Player of pointer type *Player; pointer fields cannot preserve Go nil-pointer behavior in the file renderer"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerAcceptsBoolCondOnNestedSelector covers the
// interaction note for <If cond>: a nested selector is accepted in cond
// position the same release that admits it as a bare read, both the direct
// and the == false spelling.
func TestCompileStrictServerAcceptsBoolCondOnNestedSelector(t *testing.T) {
	for _, cond := range []string{"props.Player.Ready", "props.Player.Ready == false"} {
		t.Run(cond, func(t *testing.T) {
			source := []byte("package app\ntype Player struct { Ready bool }\ntype Props struct { Player Player }\ncomponent BoardRow(props: Props) {\nreturn <main><If cond={" + cond + "}>content</If></main>\n}\n")
			if _, err := Compile(source); err != nil {
				t.Fatalf("Compile: %v", err)
			}
		})
	}
}

// TestCompileStrictServerRejectsNonBoolNestedCond mirrors
// TestCompileStrictServerRejectsNonBoolCond for a nested selector.
func TestCompileStrictServerRejectsNonBoolNestedCond(t *testing.T) {
	_, err := Compile([]byte(`package app
type Player struct { Label string }
type Props struct { Player Player }
component SeatRow(props: Props) {
	return <main><If cond={props.Player.Label}>content</If></main>
}
`))
	want := "strict component SeatRow cannot use props.Player.Label of type string in <If cond>; cond requires an exact bool props field"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictLocalCallRequiresRootOfNestedSelectorOnce covers the
// interaction note's explicit-supply rule: a callee that reads two different
// nested paths under the same root requires that root supplied exactly
// once, not once per nested path.
func TestCompileStrictLocalCallRequiresRootOfNestedSelectorOnce(t *testing.T) {
	_, err := Compile([]byte(`package app
type Player struct { Name string; Number int }
type Props struct { Player Player }
component BoardRow(props: Props) {
	return <p>{props.Player.Name}: {props.Player.Number}</p>
}
component Page() {
	return <BoardRow />
}
`))
	if err == nil {
		t.Fatal("Compile unexpectedly accepted a call that omitted the nested selector's root prop")
	}
	msg := err.Error()
	if !strings.Contains(msg, "requires prop Player") {
		t.Fatalf("error = %v, want it to require prop Player", err)
	}
	if strings.Count(msg, "requires prop Player") != 1 {
		t.Fatalf("error = %v, want exactly one \"requires prop Player\" diagnostic for two nested reads under one root", err)
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

// TestCompileStrictServerRejectsConcatChainsMissingALiteral covers the one
// v0.41 rejection whose message changed under the v0.42 concatenation
// extension: `props.A + props.B` used to fail the blanket "binary operator...
// not supported" message; it still fails closed, but now with the
// concat-specific diagnostic that names the missing literal operand instead.
func TestCompileStrictServerRejectsConcatChainsMissingALiteral(t *testing.T) {
	_, err := Compile([]byte(`package app
component Page(props: Props) {
	return <main>{props.A + props.B}</main>
}
`))
	if err == nil || !strings.Contains(err.Error(), "strict concatenation requires at least one string literal operand") {
		t.Fatalf("error = %v", err)
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
		{name: "spread", call: `<Badge {...props} />`, message: "does not accept props"},
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

// TestCompileStrictServerAcceptsConcatOfExactStringFields covers the v0.42
// concatenation extension (spec section 2.a): a `+` chain whose operands are
// string literals and props field selectors of declared type string.
func TestCompileStrictServerAcceptsConcatOfExactStringFields(t *testing.T) {
	for _, expression := range []string{
		`"tone-" + props.Tone`,
		`props.Tone + "-tone"`,
		`"Remove " + props.Name + " from roster"`,
		`("tone-") + (props.Tone)`,
	} {
		t.Run(expression, func(t *testing.T) {
			source := []byte("package app\ntype Props struct { Tone string; Name string }\ncomponent Page(props: Props) {\nreturn <main class={" + expression + "}>{" + expression + "}</main>\n}\n")
			if _, err := Compile(source); err != nil {
				t.Fatalf("Compile: %v", err)
			}
		})
	}
}

// TestCompileStrictServerRejectsConcatOfNonStringField covers section 4.2:
// a field that passes the general renderer-scalar-type gate (it is a valid
// exact builtin) but is not exactly string still fails concatenation, named
// specifically.
func TestCompileStrictServerRejectsConcatOfNonStringField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Props struct { Count int }
component RosterRow(props: Props) {
	return <main>{"Rank " + props.Count}</main>
}
`))
	want := `strict component RosterRow cannot concatenate props.Count of type int; "+" operands must be exact string props fields`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerConcatOfNamedStringTypeFailsClosedOnExistingGate
// documents a spec divergence: section 4.2's second example message
// ("named string types are outside the strict renderer contract") is not
// separately reachable. Any field whose declared type is not one of the
// known renderer-scalar builtins — including a named string type — already
// fails validateStrictRenderedProps's pre-existing renderer-scalar-type gate
// before the concat-specific pass would run. The component still fails
// closed; it just does so with the existing, more general diagnostic
// instead of a second, concat-specific one.
//
// This test also doubles as the regression check for a fail-open bug this
// change fixes in collectStrictPropReads (see the doc comment there):
// props.Tone here is read only inside the class attribute's `{...}` value,
// and before the fix, attribute-position expressions were invisible to
// read-tracking (the grammar hands them back as one opaque scanner token
// with no nested CST), so validateStrictRenderedProps never saw this field
// at all and this exact source compiled with no error.
func TestCompileStrictServerConcatOfNamedStringTypeFailsClosedOnExistingGate(t *testing.T) {
	_, err := Compile([]byte(`package app
type Tone string
type Props struct { Tone Tone }
component TeamMark(props: Props) {
	return <span class={"tone-" + props.Tone}>mark</span>
}
`))
	if err == nil || !strings.Contains(err.Error(), "renderer-visible props fields must use exact string, bool, integer, or floating-point builtins") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "cannot concatenate") {
		t.Fatalf("did not expect a duplicate concat-specific diagnostic: %v", err)
	}
}

// TestCompileStrictServerTracksAttributePositionPropsReads is a direct
// regression test for the collectStrictPropReads fix: a props field read
// only inside attribute-position expressions (a concat operand and an <If
// cond> selector) must still be subject to the explicit-required-prop rule
// and the renderer-scalar-type gate, exactly as a text-child read would be.
func TestCompileStrictServerTracksAttributePositionPropsReads(t *testing.T) {
	const source = `package app
type CardProps struct {
	Ready bool
	Tone  string
}
component Card(props: CardProps) {
	return <div class={"tone-" + props.Tone}><If cond={props.Ready}>ready</If></div>
}
component Page() {
	return <Card />
}
`
	_, err := Compile([]byte(source))
	if err == nil {
		t.Fatal("Compile unexpectedly accepted a call that omitted attribute-only-read props")
	}
	for _, want := range []string{"requires prop Ready", "requires prop Tone"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile error %q does not contain %q", err, want)
		}
	}
}

// TestCompileStrictServerRejectsConcatMissingAPropsFieldOperand covers the
// "literal-only chain" rejection from section 2.a's non-goals.
func TestCompileStrictServerRejectsConcatMissingAPropsFieldOperand(t *testing.T) {
	_, err := Compile([]byte(`package app
component Page() {
	return <main>{"a" + "b"}</main>
}
`))
	want := "strict concatenation requires at least one props field operand; fold literal-only chains by hand"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v", err)
	}
}

// TestCompileStrictServerAcceptsBoolCond covers the v0.42 <If cond> extension
// (spec section 2.c): a bare bool selector and its `== false` negation.
func TestCompileStrictServerAcceptsBoolCond(t *testing.T) {
	for _, cond := range []string{"props.Ready", "props.Ready == false", "(props.Ready) == (false)"} {
		t.Run(cond, func(t *testing.T) {
			source := []byte("package app\ntype Props struct { Ready bool }\ncomponent Page(props: Props) {\nreturn <main><If cond={" + cond + "}>content</If></main>\n}\n")
			if _, err := Compile(source); err != nil {
				t.Fatalf("Compile: %v", err)
			}
		})
	}
}

// TestCompileStrictServerRejectsNonBoolCond covers section 4.4's cond-type
// diagnostic: a props field of an exact renderer-scalar type that is not bool.
func TestCompileStrictServerRejectsNonBoolCond(t *testing.T) {
	_, err := Compile([]byte(`package app
type Props struct { Label string }
component SeatRow(props: Props) {
	return <main><If cond={props.Label}>content</If></main>
}
`))
	want := "strict component SeatRow cannot use props.Label of type string in <If cond>; cond requires an exact bool props field"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerRejectsIfShapeViolations covers section 4.4's shape
// diagnostics: a second attribute and a missing cond attribute (the
// `when=` legacy spelling does not satisfy the cond-only shape).
func TestCompileStrictServerRejectsIfShapeViolations(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "second-attribute",
			body: `<If cond={props.Ready} fallback="no">content</If>`,
			want: `strict <If> does not accept attribute "fallback"; cond is the only supported attribute`,
		},
		{
			name: "when-spelling",
			body: `<If when={props.Ready}>content</If>`,
			want: "strict <If> requires exactly one cond attribute",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\ntype Props struct { Ready bool }\ncomponent Page(props: Props) {\nreturn <main>" + tc.body + "</main>\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want to contain %q", err, tc.want)
			}
		})
	}
}

// TestCompileStrictServerIfSpreadReportsOneDiagnostic covers the N1 review
// fix: a spread attribute on strict <If> is always missing cond, but the
// validator must not also report the separate cond-count violation — a
// spread attribute report is enough on its own.
func TestCompileStrictServerIfSpreadReportsOneDiagnostic(t *testing.T) {
	_, err := Compile([]byte(`package app
type Props struct { Extra bool }
component Page(props: Props) {
	return <main><If {...props.Extra}>content</If></main>
}
`))
	if err == nil {
		t.Fatal("expected validation diagnostics")
	}
	var diagErr *ir.DiagnosticsError
	if !errors.As(err, &diagErr) {
		t.Fatalf("expected diagnostics error, got %T: %v", err, err)
	}
	if len(diagErr.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(diagErr.Diagnostics), diagErr.Diagnostics)
	}
	if !strings.Contains(diagErr.Diagnostics[0].Message, "strict <If> does not accept spread attributes") {
		t.Fatalf("unexpected diagnostic %#v", diagErr.Diagnostics[0])
	}
}

// TestCompileStrictServerRejectsIfCondComparisonShapes covers section 4.4's
// cond-expression-shape diagnostics from the validator.
func TestCompileStrictServerRejectsIfCondComparisonShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		cond string
		want string
	}{
		{"eq-true", "props.Ready == true", `comparison "== true" is not supported in strict cond; write the field bare`},
		{"neq-false", "props.Ready != false", `strict cond must be a bool props field or a bool props field compared with "== false"; got "props.Ready != false"`},
		{"negation", "!props.Ready", `strict cond must be a bool props field or a bool props field compared with "== false"; got "!props.Ready"`},
		{"two-selectors", "props.A == props.B", `strict cond must be a bool props field or a bool props field compared with "== false"; got "props.A == props.B"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\ntype Props struct { Ready bool; A bool; B bool }\ncomponent Page(props: Props) {\nreturn <main><If cond={" + tc.cond + "}>content</If></main>\n}\n")
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want to contain %q", err, tc.want)
			}
		})
	}
}

// TestCompileStrictServerIfShadowedBySameFileComponentStaysAComponentCall
// covers the shadow regression from section 5.2: a same-file strict
// component named If wins over the builtin, and the ordinary strict-call
// rules (explicit rendered props, no positional children) still apply to it.
func TestCompileStrictServerIfShadowedBySameFileComponentStaysAComponentCall(t *testing.T) {
	prog, err := Compile([]byte(`package app
type IfProps struct { Label string }
component If(props: IfProps) {
	return <em>{props.Label}</em>
}
component Page() {
	return <If label="shadowed" />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	page := prog.Components[1]
	call := prog.NodeAt(page.Root)
	if call == nil || call.Tag != "If" || len(call.Attrs) != 1 || call.Attrs[0].Name != "Label" {
		t.Fatalf("shadowed If call not lowered as a component call: %#v", call)
	}

	// The ordinary strict-call rules still apply: omitting the rendered prop
	// still fails closed instead of silently falling through to the builtin.
	_, err = Compile([]byte(`package app
type IfProps struct { Label string }
component If(props: IfProps) {
	return <em>{props.Label}</em>
}
component Page() {
	return <If />
}
`))
	if err == nil || !strings.Contains(err.Error(), "requires prop Label") {
		t.Fatalf("error = %v", err)
	}
}

// --- E2 (#184): spread props at strict call sites -------------------------

// TestCompileStrictTierOneSpreadIntoShadowedEachAccepts covers gosx#182/
// #184 minor m-6: a same-file strict component literally named "Each"
// (the section 2.1 shadow carve-out) is a valid E2 tier-1 spread callee
// like any other. Before the fix, collectStrictElementReads excluded tag
// "Each" from the spread-forward read class unconditionally, so the
// struct-typed spread source fell through to the scalar-read walk and
// validateStrictRenderedProps rejected it as a non-scalar prop — even
// though validateStrictToStrictSpreadCall, a separate pass, proves this
// exact call valid. This must now compile clean.
func TestCompileStrictTierOneSpreadIntoShadowedEachAccepts(t *testing.T) {
	_, err := Compile([]byte(`package app
type EachProps struct {
	Label string
}
component Each(props: EachProps) {
	return <em>{props.Label}</em>
}
component Page(props: EachProps) {
	return <div><Each {...props}></Each></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// TestCompileStrictTierOneSpreadAcceptsExactTypeSources covers design spec
// section 3.2: bare props (the whole props value) and a props field
// selector whose declared type is exactly the callee's props type.
func TestCompileStrictTierOneSpreadAcceptsExactTypeSources(t *testing.T) {
	t.Run("bare props", func(t *testing.T) {
		_, err := Compile([]byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
component Matchup(props: TeamMarkProps) {
	return <div><TeamMark {...props}></TeamMark></div>
}
`))
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
	})
	t.Run("props field", func(t *testing.T) {
		_, err := Compile([]byte(`package app
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
`))
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
	})
}

// TestCompileStrictTierOneSpreadForwardReferencedCalleeCompiles covers
// gosx#182/#184 M-3: collectStrictSchemas used to be one pass, populating
// l.strictNames in file order while ALSO collecting each component's own
// reads in that same pass — so isSpreadForwardTag (which needs
// l.strictNames complete to classify a spread's read) saw a callee
// declared LATER in the file as unknown, and a caller's {...props.Away}
// spread into it fell through to the generic scalar-read walk, which
// rejected the struct-typed props.Away as not a renderer scalar. Whether
// this compiled depended only on which order Mark and Outer were declared
// in, not on any real type-safety difference between the two orders. Both
// orders must now compile, and produce byte-identical PropsFields and
// PropsPaths for both components regardless of which pass order collected
// them.
func TestCompileStrictTierOneSpreadForwardReferencedCalleeCompiles(t *testing.T) {
	calleeFirst := `package app
type MarkProps struct {
	Tone string
}
component Mark(props: MarkProps) {
	return <span>{props.Tone}</span>
}
type OuterProps struct {
	Away MarkProps
}
component Outer(props: OuterProps) {
	return <div><Mark {...props.Away}></Mark></div>
}
`
	calleeLast := `package app
type OuterProps struct {
	Away MarkProps
}
component Outer(props: OuterProps) {
	return <div><Mark {...props.Away}></Mark></div>
}
type MarkProps struct {
	Tone string
}
component Mark(props: MarkProps) {
	return <span>{props.Tone}</span>
}
`
	progFirst, err := Compile([]byte(calleeFirst))
	if err != nil {
		t.Fatalf("Compile (callee declared first): %v", err)
	}
	progLast, err := Compile([]byte(calleeLast))
	if err != nil {
		t.Fatalf("Compile (callee declared last, forward reference): %v", err)
	}

	fieldsByName := func(prog *ir.Program) map[string]ir.Component {
		out := make(map[string]ir.Component, len(prog.Components))
		for _, c := range prog.Components {
			out[c.Name] = c
		}
		return out
	}
	first := fieldsByName(progFirst)
	last := fieldsByName(progLast)
	for _, name := range []string{"Outer", "Mark"} {
		fc, lc := first[name], last[name]
		if len(fc.PropsFields) == 0 {
			t.Fatalf("%s: PropsFields empty in the callee-first order", name)
		}
		if fmtMap(fc.PropsFields) != fmtMap(lc.PropsFields) {
			t.Fatalf("%s: PropsFields differ by declaration order: first=%#v last=%#v", name, fc.PropsFields, lc.PropsFields)
		}
		if fmtMap(fc.PropsPaths) != fmtMap(lc.PropsPaths) {
			t.Fatalf("%s: PropsPaths differ by declaration order: first=%#v last=%#v", name, fc.PropsPaths, lc.PropsPaths)
		}
	}
}

// fmtMap gives two maps a deterministic, comparable string form for an
// order-independence assertion — map equality via reflect.DeepEqual would
// work too, but this reads directly in a failure message.
func fmtMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s;", k, m[k])
	}
	return b.String()
}

func TestCompileStrictTierOneSpreadRejectsWrongType(t *testing.T) {
	_, err := Compile([]byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
type MatchupTeam struct {
	Tone string
}
type MatchupProps struct {
	Away MatchupTeam
}
component Matchup(props: MatchupProps) {
	return <div><TeamMark {...props.Away}></TeamMark></div>
}
`))
	want := "strict spread source props.Away has type MatchupTeam, want exact TeamMarkProps; a strict caller spreads a value whose declared type is the callee props type"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictTierOneSpreadRejectsNonSelectorSource(t *testing.T) {
	_, err := Compile([]byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
component Matchup(props: TeamMarkProps) {
	return <div><TeamMark {...team()}></TeamMark></div>
}
`))
	want := `strict spread source "team()" is not renderable; spread sources are props or a props field selector`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictTierOneSpreadRejectsSpreadPlusNamedAttr(t *testing.T) {
	_, err := Compile([]byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
component Matchup(props: TeamMarkProps) {
	return <div><TeamMark {...props} tone="x"></TeamMark></div>
}
`))
	want := "strict component call TeamMark accepts at most one spread attribute and no other attributes"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictTierOneSpreadRejectsTwoSpreads(t *testing.T) {
	_, err := Compile([]byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
component Matchup(props: TeamMarkProps) {
	return <div><TeamMark {...props} {...props}></TeamMark></div>
}
`))
	want := "strict component call TeamMark accepts at most one spread attribute and no other attributes"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictTierTwoSpreadAcceptsLegacySingleSpread covers section
// 3.3: a legacy body's single spread into a same-file strict component
// compiles — the shape is all the lowerer can prove; the renderer boundary
// proves the value.
func TestCompileStrictTierTwoSpreadAcceptsLegacySingleSpread(t *testing.T) {
	_, err := Compile([]byte(`package app
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
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// TestCompileStrictTierTwoSpreadRejectsNamedAttrs covers section 4.4's
// updated cross-style message: a legacy body calling a strict component
// with named attributes stays banned, now naming the supported spread
// spelling instead of the flat v0.39 message.
func TestCompileStrictTierTwoSpreadRejectsNamedAttrs(t *testing.T) {
	_, err := Compile([]byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
func Page() Node {
	return <div><TeamMark tone="red"></TeamMark></div>
}
`))
	want := "legacy component cannot call strict component TeamMark with named attributes; pass one {...source} spread and the renderer will prove it at the boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictTierTwoSpreadRejectsSpreadPlusAttr covers the shape rule
// for tier 2 too: a spread plus a named attribute stays rejected by the
// same message as the bare named-attrs case.
func TestCompileStrictTierTwoSpreadRejectsSpreadPlusAttr(t *testing.T) {
	_, err := Compile([]byte(`package app
type TeamMarkProps struct {
	Tone string
}
component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
func Page() Node {
	team := map[string]any{"Tone": "red"}
	return <div><TeamMark {...team} tone="blue"></TeamMark></div>
}
`))
	want := "legacy component cannot call strict component TeamMark with named attributes; pass one {...source} spread and the renderer will prove it at the boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileRejectsCrossStyleComponentCallsPolarityRegression re-runs
// TestCompileRejectsCrossStyleComponentCalls' strict-to-legacy direction
// (unaffected by E2, which only narrows the legacy-to-strict direction) and
// the legacy-to-strict, no-attribute direction (which keeps its exact
// v0.39 message).
func TestCompileRejectsCrossStyleComponentCallsPolarityRegression(t *testing.T) {
	_, err := Compile([]byte(`package app
component Badge() {
	return <span>badge</span>
}
func Page() Node {
	return <Badge />
}
`))
	if err == nil || !strings.Contains(err.Error(), "calls must stay within one style") {
		t.Fatalf("error = %v", err)
	}
	_, err = Compile([]byte(`package app
func Badge(attrs AttrList) Node {
	return <span>badge</span>
}
component Page() {
	return <Badge bogus="x">child</Badge>
}
`))
	if err == nil || !strings.Contains(err.Error(), "calls must stay within one style") {
		t.Fatalf("error = %v", err)
	}
}

// --- E1 (#182): strict <Each> ----------------------------------------------

const breakdownRowFixturePrelude = `package app
type BreakdownRow struct {
	Scored bool
	Label  string
	Points string
}
type RowProps struct {
	Breakdown []BreakdownRow
}
`

func TestCompileStrictEachAcceptsSliceOfSameFileStruct(t *testing.T) {
	prog, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div>
		<Each of={props.Breakdown} as="row">
			<div data-scored={row.Scored}><span>{row.Label}</span><b>{row.Points}</b></div>
		</Each>
	</div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	row := prog.Components[0]
	schema, ok := row.PropsSlices["Breakdown"]
	if !ok {
		t.Fatalf("PropsSlices missing Breakdown: %#v", row.PropsSlices)
	}
	if schema.Elem != "BreakdownRow" {
		t.Fatalf("Elem = %q, want BreakdownRow", schema.Elem)
	}
	for _, field := range []string{"Scored", "Label", "Points"} {
		if _, ok := schema.Reads[field]; !ok {
			t.Fatalf("Reads missing %q: %#v", field, schema.Reads)
		}
	}
	if row.PropsFields["Breakdown"] != "[]BreakdownRow" {
		t.Fatalf("PropsFields[Breakdown] = %q, want []BreakdownRow", row.PropsFields["Breakdown"])
	}
}

func TestCompileStrictEachRejectsLoopableTypeTable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields string
		of     string
		want   string
	}{
		{
			name:   "map",
			fields: "Breakdown map[string]BreakdownRow",
			of:     "props.Breakdown",
			want:   "<Each> sources must be []T slices of structs declared in this .gsx file",
		},
		{
			name:   "pointer elements",
			fields: "Breakdown []*BreakdownRow",
			of:     "props.Breakdown",
			want:   "pointer elements cannot preserve Go nil-pointer behavior in the file renderer",
		},
		{
			name:   "scalar elements",
			fields: "Names []string",
			of:     "props.Names",
			want:   "loop elements must be structs declared in this .gsx file",
		},
		{
			name:   "array",
			fields: "Breakdown [3]BreakdownRow",
			of:     "props.Breakdown",
			want:   "<Each> sources must be []T slices of structs declared in this .gsx file",
		},
		{
			name:   "named slice type",
			fields: "Breakdown Rows",
			of:     "props.Breakdown",
			want:   "<Each> sources must be []T slices of structs declared in this .gsx file",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(`package app
type BreakdownRow struct {
	Label string
}
type Rows []BreakdownRow
type RowProps struct {
	` + tc.fields + `
}
component Row(props: RowProps) {
	return <div><Each of={` + tc.of + `} as="row"><span>{row.Label}</span></Each></div>
}
`)
			_, err := Compile(source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want to contain %q", err, tc.want)
			}
		})
	}
}

func TestCompileStrictEachRejectsMissingAs(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown}><span>x</span></Each></div>
}
`))
	want := "strict <Each> requires a static as attribute naming the loop binding"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachRejectsDuplicateOf, TestCompileStrictEachRejectsDuplicateAs,
// and TestCompileStrictEachRejectsDuplicateIndex cover gosx#182/#184 B-1:
// strictEachShape used to count of, as, and index last-wins, with no
// duplicate check — validateStrictConditionalCall already counted cond,
// but <Each> lost that rule. route/fileprogram.go's attrValue binds the
// FIRST matching attribute, while the transpiled Go call binds whichever
// one the AST holds last, so `<Each of={props.Rows} as="row" as="alt">`
// compiled clean and bound "alt" in generated Go but "row" in the file
// renderer — a silent two-surface divergence with no compile error at
// all. These prove the fix fails closed at compile time.
func TestCompileStrictEachRejectsDuplicateOf(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} of={props.Breakdown} as="row"><span>{row.Label}</span></Each></div>
}
`))
	want := "strict <Each> requires exactly one of attribute with an expression value"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachRejectsDuplicateAs reproduces the reviewer's exact
// divergence shape verbatim: `<Each of={props.Rows} as="row" as="alt">`.
// Before the fix this compiled; the file renderer bound "row" (attrValue's
// first match) while the transpiled Go bound "alt" (the AST's last as),
// so the SAME program rendered two different values depending on which
// renderer executed it, with no diagnostic anywhere. It must now fail
// closed at Compile, closing that divergence at the source.
func TestCompileStrictEachRejectsDuplicateAs(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row" as="alt"><span>{alt.Label}</span></Each></div>
}
`))
	want := "strict <Each> requires exactly one as attribute naming the loop binding"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachRejectsDuplicateIndex(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row" index="i" index="j"><span>{j}</span><b>{row.Label}</b></Each></div>
}
`))
	want := "strict <Each> requires exactly one index attribute naming the index binding"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachRejectsBindingNamedAfterImportAlias covers gosx#182/
// #184 nit n-1: a binding named after an explicit same-file import alias
// already failed closed in the strictcheck projection (the generated Go
// shadows the package alias inside the loop body), just with a confusing
// Go-compiler error far from the .gsx source instead of a clear lowerer
// diagnostic at the binding itself.
func TestCompileStrictEachRejectsBindingNamedAfterImportAlias(t *testing.T) {
	_, err := Compile([]byte(`package app

import strs "strings"

type BreakdownRow struct {
	Label string
}
type RowProps struct {
	Breakdown []BreakdownRow
}
component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="strs"><span>{strs.Label}</span></Each></div>
}
`))
	want := `strict <Each> binding "strs" shadows this file's "strs" import; choose another name`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachRejectsFallbackAttr(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row" fallback="none"><span>{row.Label}</span></Each></div>
}
`))
	want := `strict <Each> does not accept attribute "fallback"; of, as, and index are the only supported attributes`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachRejectsUppercaseBinding(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="Row"><span>x</span></Each></div>
}
`))
	want := `strict <Each> binding "Row" must start with a lowercase letter and be a valid Go identifier`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachRejectsReservedPropsBinding(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="props"><span>x</span></Each></div>
}
`))
	want := `strict <Each> binding "props" is reserved; choose another name`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachRejectsShadowedBinding(t *testing.T) {
	_, err := Compile([]byte(`package app
type Stat struct {
	Label string
}
type BreakdownRow struct {
	Stats []Stat
	Label string
}
type RowProps struct {
	Breakdown []BreakdownRow
}
component Row(props: RowProps) {
	return <div>
		<Each of={props.Breakdown} as="row">
			<Each of={row.Stats} as="row"><span>{row.Label}</span></Each>
		</Each>
	</div>
}
`))
	want := `strict <Each> binding "row" is already bound by an enclosing <Each>; loop bindings cannot shadow`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachRejectsCrossFileElement covers section 2.3's
// cross-package/sibling-.go element row: this .gsx file's schema is blind
// to any type it does not itself declare, including a scalar-looking
// builtin-shaped name it never defines.
func TestCompileStrictEachRejectsCrossFileElement(t *testing.T) {
	_, err := Compile([]byte(`package app
type RowProps struct {
	Breakdown []external.Row
}
component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span>x</span></Each></div>
}
`))
	if err == nil {
		t.Fatal("Compile unexpectedly accepted a cross-package element type")
	}
}

// TestCompileStrictEachShadowedByComponentStaysAComponentCall mirrors
// TestCompileStrictServerIfShadowedBySameFileComponentStaysAComponentCall:
// a same-file strict component literally named Each wins over the
// builtin, and ordinary strict-call rules apply to it.
func TestCompileStrictEachShadowedByComponentStaysAComponentCall(t *testing.T) {
	prog, err := Compile([]byte(`package app
type EachProps struct {
	Label string
}
component Each(props: EachProps) {
	return <em>{props.Label}</em>
}
component Page() {
	return <Each label="shadowed" />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	page := prog.Components[1]
	call := prog.NodeAt(page.Root)
	if call == nil || call.Tag != "Each" || len(call.Attrs) != 1 || call.Attrs[0].Name != "Label" {
		t.Fatalf("shadowed Each call not lowered as a component call: %#v", call)
	}
}

func TestCompileStrictEachRejectsSliceFieldRenderedAsScalar(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div>{props.Breakdown}</div>
}
`))
	want := "strict component Row cannot render props.Breakdown of type []BreakdownRow here; slice props render only through <Each of>"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachAcceptsFieldBothLoopedAndNeverRenderedScalar proves
// the same field looped (and never separately rendered as a bare scalar)
// compiles cleanly.
func TestCompileStrictEachAcceptsFieldBothLoopedAndNeverRenderedScalar(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span>{row.Label}</span></Each></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func TestCompileStrictEachBindingSelectorAcceptsScalarLeafRejectsStructLeaf(t *testing.T) {
	source := []byte(`package app
type Stat struct {
	Label string
}
type BreakdownRow struct {
	Stat Stat
}
type RowProps struct {
	Breakdown []BreakdownRow
}
component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span>{row.Stat}</span></Each></div>
}
`)
	_, err := Compile(source)
	want := "strict component Row cannot render row.Stat of type Stat; loop selectors must reach an exact scalar field"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachRejectsBindingSelectorEmbeddedField extends gosx#183's
// B1 fix (a promoted field at selector hop i>0 fails closed) to an <Each>
// binding root: row.Stat.City crosses a promoted field the same way
// props.Player.City did in TestCompileStrictServerRejectsNestedSelectorEmbeddedField.
// TeamRef carries its own named sibling field (Note) so structTypes
// registers TeamRef at all — with no named field of its own, TeamRef never
// enters structTypes and the call fails on the separate "struct is not
// declared" gate instead of the promoted-field gate this test means to
// cover.
func TestCompileStrictEachRejectsBindingSelectorEmbeddedField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Team struct { City string }
type TeamRef struct { Team; Note string }
type BreakdownRow struct { Stat TeamRef }
type RowProps struct { Breakdown []BreakdownRow }
component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span>{row.Stat.City}</span></Each></div>
}
`))
	want := "strict component Row cannot resolve row.Stat.City: struct TeamRef declares no visible field City; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachRejectsBindingSelectorUnexportedLeaf covers the other
// B1 shape for a binding root: an unexported leaf field on a same-file
// declared struct (not promoted, just lowercase) hits the identical
// hop-i>0 unknown-field gate, since collectStructSchemas never records an
// unexported field at all.
func TestCompileStrictEachRejectsBindingSelectorUnexportedLeaf(t *testing.T) {
	_, err := Compile([]byte(`package app
type Team struct { city string; Zone string }
type BreakdownRow struct { Stat Team }
type RowProps struct { Breakdown []BreakdownRow }
component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span>{row.Stat.city}</span></Each></div>
}
`))
	want := "strict component Row cannot resolve row.Stat.city: struct Team declares no visible field city; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachRejectsPromotedBindingHopZeroField and
// TestCompileStrictEachRejectsUnexportedBindingHopZeroField cover gosx#182/
// #184 M-2: a HOP-0 read on an <Each> BINDING root (the item's own
// field, not a nested one) used to return the silent strictHopUnknownField
// kind, on the claim that the package checker backstops it. That claim was
// false here — the projection compiles in the SAME package as the .gsx
// file's declarations, so Go resolves a promoted or unexported field on
// the binding's element struct exactly as it resolves a declared one.
// Before the fix this compiled clean, strictcheck accepted it, PropsSlices
// omitted the field from Reads, and the boundary never proved it, so the
// file renderer and the transpiled Go diverged on row.Label silently. Both
// must now fail closed at compile time with the B1-style message.
func TestCompileStrictEachRejectsPromotedBindingHopZeroField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Base struct { Label string }
type Row struct { Base; Points string }
type RowProps struct { Rows []Row }
component C(props: RowProps) {
	return <section><Each of={props.Rows} as="row"><span>{row.Label}</span></Each></section>
}
`))
	want := "strict component C cannot resolve row.Label: struct Row declares no visible field Label; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachRejectsUnexportedBindingHopZeroField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Row struct { label string; Points string }
type RowProps struct { Rows []Row }
component C(props: RowProps) {
	return <section><Each of={props.Rows} as="row"><span>{row.label}</span></Each></section>
}
`))
	want := "strict component C cannot resolve row.label: struct Row declares no visible field label; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerRejectsPromotedPropsHopZeroField and
// TestCompileStrictServerRejectsUnexportedPropsHopZeroField cover gosx#195:
// a HOP-0 read on the PROPS root itself (a direct field of the props
// struct, not a nested one) used to return the silent strictHopUnknownField
// kind, deferring to the package checker. That deferral was false here too
// — the generated check program compiles in the SAME package as the .gsx
// file's declarations, so Go resolves a promoted or unexported props field
// exactly as it resolves a declared one. Before the fix this compiled
// clean, strictcheck accepted it, the boundary schema omitted the field,
// and the file renderer and the transpiled Go diverged on props.Label
// silently — the props-root mirror of gosx#182/#184's M-2 finding for an
// <Each> binding root. Both must now fail closed at compile time with the
// same B1-style message TestCompileStrictEachRejectsPromotedBindingHopZeroField
// and TestCompileStrictEachRejectsUnexportedBindingHopZeroField prove for a
// binding root.
func TestCompileStrictServerRejectsPromotedPropsHopZeroField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Base struct { Label string }
type Props struct { Base; Points string }
component C(props: Props) {
	return <section><span>{props.Label}</span></section>
}
`))
	want := "strict component C cannot resolve props.Label: struct Props declares no visible field Label; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictServerRejectsUnexportedPropsHopZeroField(t *testing.T) {
	_, err := Compile([]byte(`package app
type Props struct { label string; Points string }
component C(props: Props) {
	return <section><span>{props.label}</span></section>
}
`))
	want := "strict component C cannot resolve props.label: struct Props declares no visible field label; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictServerAcceptsDirectScalarPropsHopZeroField is the accept
// half of gosx#195's fix: Points is declared directly on Props (not
// promoted through Base, not unexported), so a hop-0 read of it must keep
// compiling clean — the fix rejects only a promoted or unexported hop-0
// field, never a legitimately declared one.
func TestCompileStrictServerAcceptsDirectScalarPropsHopZeroField(t *testing.T) {
	source := []byte(`package app
type Base struct { Label string }
type Props struct { Base; Points string }
component C(props: Props) {
	return <section><span>{props.Points}</span></section>
}
`)
	if _, err := Compile(source); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// TestCompileStrictServerRejectsMixedValidAndPromotedPropsReads closes the
// loop gosx#195's review probe opened: a read set that mixes one
// legitimate direct scalar field with one promoted hop-0 field must still
// fail closed on the promoted field, proving the fix fires per-path across
// a component's whole read set, not only when the promoted field is the
// sole read.
func TestCompileStrictServerRejectsMixedValidAndPromotedPropsReads(t *testing.T) {
	_, err := Compile([]byte(`package app
type Base struct { Label string }
type Props struct { Base; Points string }
component C(props: Props) {
	return <section><span>{props.Points}</span><b>{props.Label}</b></section>
}
`))
	want := "strict component C cannot resolve props.Label: struct Props declares no visible field Label; promoted, unexported, and unknown fields cannot cross the file renderer boundary"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachBindingConcatAndCondBothPolarities(t *testing.T) {
	// Accept: exact string/bool leaves.
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span class={"tone-" + row.Label}><If cond={row.Scored}>scored</If></span></Each></div>
}
`))
	if err != nil {
		t.Fatalf("Compile (accept): %v", err)
	}

	// Reject: concat of a non-string binding field.
	_, err = Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span>{"scored-" + row.Scored}</span></Each></div>
}
`))
	want := `strict component Row cannot concatenate row.Scored of type bool; "+" operands must be exact string fields`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}

	// Reject: cond over a non-bool binding field.
	_, err = Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><If cond={row.Label}>x</If></Each></div>
}
`))
	want = "strict component Row cannot use row.Label of type string in <If cond>; cond requires an exact bool field"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

func TestCompileStrictEachRejectsIndexBindingInSelector(t *testing.T) {
	_, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row" index="i"><span>{i.Label}</span></Each></div>
}
`))
	want := "strict component Row cannot use index binding i in a selector; the index is an int value"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCompileStrictEachSerializationRoundTrip covers the PropsSlices
// round-trip half of section 5.2's list (paired with the dedicated absent-
// field decode test in ir/propspaths_roundtrip_test.go-style coverage,
// added under ir/).
func TestCompileStrictEachSerializationRoundTrip(t *testing.T) {
	prog, err := Compile([]byte(breakdownRowFixturePrelude + `component Row(props: RowProps) {
	return <div><Each of={props.Breakdown} as="row"><span>{row.Label}</span></Each></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	data, err := json.Marshal(prog.Components[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded ir.Component
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.PropsSlices["Breakdown"].Elem != "BreakdownRow" {
		t.Fatalf("PropsSlices did not round-trip: %#v", decoded.PropsSlices)
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
