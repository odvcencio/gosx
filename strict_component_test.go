package gosx

import (
	"errors"
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
