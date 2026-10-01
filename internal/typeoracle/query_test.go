package typeoracle

import (
	"path/filepath"
	"testing"
)

// TestSessionPropsFieldTypeFromSiblingGoFile covers fixture (a): a strict
// component's props struct declares a field whose own type is declared
// only in a sibling .go file, not the .gsx file. ir.Lower's same-file rule
// (this package's own doc comment, "Role" section) only rejects a props
// FIELD that the component body actually reads through a nested path — a
// field the body never dereferences passes ir.Lower untouched, and this is
// exactly the case Session exists to serve: strictcheck's own go/ast
// pattern matching (strictcheck/servergo.go) cannot tell you Meta's field
// set at all, because it never parses the sibling .go file as real Go
// types. Session, backed by go/types, can.
func TestSessionPropsFieldTypeFromSiblingGoFile(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), `package main

type CardProps struct {
	Title string
	Meta  Meta
}

component Card(props: CardProps) {
	return <div>{props.Title}</div>
}

component Page() {
	return <Card title="hi" />
}
`)
	mustWrite(t, filepath.Join(dir, "meta.go"), `package main

type Meta struct {
	Label string
	Count int
}
`)
	session := loadTestSession(t, dir)
	if len(session.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", session.Diagnostics)
	}

	structType, ok := session.PropsType("Card")
	if !ok {
		t.Fatal("PropsType(Card) = not ok, want the CardProps struct")
	}
	if structType.NumFields() != 2 {
		t.Fatalf("CardProps has %d fields, want 2", structType.NumFields())
	}

	metaType, ok := session.FieldType("CardProps", "Meta")
	if !ok {
		t.Fatal("FieldType(CardProps, Meta) = not ok")
	}
	if got := metaType.String(); got != "main.Meta" {
		t.Fatalf("FieldType(CardProps, Meta) = %s, want main.Meta", got)
	}

	fields, ok := session.FieldSet("Meta")
	if !ok {
		t.Fatal("FieldSet(Meta) = not ok, want Meta's field set resolved through the sibling .go file")
	}
	want := map[string]string{"Label": "string", "Count": "int"}
	if len(fields) != len(want) {
		t.Fatalf("FieldSet(Meta) = %#v, want %d fields", fields, len(want))
	}
	for _, f := range fields {
		if got, ok := want[f.Name]; !ok || f.Type.String() != got {
			t.Fatalf("field %s has type %s, want %s", f.Name, f.Type, want[f.Name])
		}
	}
}

// TestSessionAliasedImportResolvesFieldType covers fixture (c): a .gsx
// import with an explicit alias, used as a props struct field's type.
// Session must resolve the field's real type (time.Duration) through the
// alias, proving the projection's import rewriting (transpile.go's
// emitImportDeclaration) and this package's sibling go/types check agree
// on what the alias means.
func TestSessionAliasedImportResolvesFieldType(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), `package main

import dur "time"

type EventProps struct {
	Name string
	When dur.Duration
}

component Event(props: EventProps) {
	return <div>{props.Name}</div>
}

component Page() {
	return <Event name="party" />
}
`)
	session := loadTestSession(t, dir)
	if len(session.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", session.Diagnostics)
	}
	whenType, ok := session.FieldType("EventProps", "When")
	if !ok {
		t.Fatal("FieldType(EventProps, When) = not ok")
	}
	if got := whenType.String(); got != "time.Duration" {
		t.Fatalf("FieldType(EventProps, When) = %s, want time.Duration (resolved through the dur alias)", got)
	}
}

// TestSessionAssignableToField exercises the query API's assignability
// answer end to end: a props field's declared type (int) accepts an
// int-typed expression and rejects a string-typed one, using only
// package-scope facts EvalExpr can see (a literal here; see EvalExpr's own
// doc comment for why a local variable is out of scope for this query).
func TestSessionAssignableToField(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), `package main

type CardProps struct {
	Count int
}

component Card(props: CardProps) {
	return <div>hi</div>
}

component Page() {
	return <Card />
}
`)
	session := loadTestSession(t, dir)
	if len(session.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", session.Diagnostics)
	}

	ok, err := session.AssignableToField("42", "CardProps", "Count")
	if err != nil {
		t.Fatalf("AssignableToField(42, CardProps, Count): %v", err)
	}
	if !ok {
		t.Fatal("AssignableToField(42, CardProps, Count) = false, want true")
	}

	ok, err = session.AssignableToField(`"forty-two"`, "CardProps", "Count")
	if err != nil {
		t.Fatalf("AssignableToField(\"forty-two\", CardProps, Count): %v", err)
	}
	if ok {
		t.Fatal("AssignableToField(\"forty-two\", CardProps, Count) = true, want false")
	}

	if _, err := session.AssignableToField("42", "CardProps", "Missing"); err == nil {
		t.Fatal("AssignableToField against an unknown field = nil error, want one naming it")
	}
}
