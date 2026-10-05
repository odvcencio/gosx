package ir_test

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

func TestStrictFormStateSchemaAndMapLookups(t *testing.T) {
	for _, expr := range []string{`props.Form.Values["email"]`, `props.Form.FieldErrors["profile.email"]`, `props.Form.Flash[""]`, `"Saved: " + props.Form.Flash["notice"]`} {
		source := `package app
import forms "m31labs.dev/gosx/route"
type PageProps struct { Form forms.FormState }
component Page(props: PageProps) {
	return <form method="post" action={props.Form.ActionURL}><input name="csrf_token" value={props.Form.CSRFToken} /><p>{` + expr + `}</p></form>
}`
		prog, err := gosx.Compile([]byte(source))
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		data, err := json.Marshal(prog)
		if err != nil {
			t.Fatal(err)
		}
		var decoded ir.Program
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if got := decoded.Components[0].PropsFormActions["Form.ActionURL"]; got != "Form.CSRFToken" {
			t.Fatalf("form schema did not round trip: %q", got)
		}
	}
}

func TestStrictMapLookupRejectsUnprovedShapes(t *testing.T) {
	for _, tc := range []struct{ decl, expr string }{
		{`Values []string`, `props.Values["email"]`},
		{`Values map[string]int`, `props.Values["email"]`},
		{`Values map[string]string; Key string`, `props.Values[props.Key]`},
		{`Values map[string]string`, `props.Values[0]`},
		{`Values map[string]string`, `props.Values.email`},
		{`Form *route.FormState`, `props.Form.Values["email"]`},
	} {
		source := `package app
import "m31labs.dev/gosx/route"
type PageProps struct { ` + tc.decl + ` }
component Page(props: PageProps) {
	return <p>{` + tc.expr + `}</p>
}
`
		if _, err := gosx.Compile([]byte(source)); err == nil {
			t.Fatalf("accepted %s", tc.expr)
		}
	}
	if _, err := gosx.Compile([]byte(`package app
type FormState struct { ActionURL string }
type PageProps struct { Form FormState }
component Page(props: PageProps) {
	return <form method="post" action={props.Form.ActionURL}></form>
}
`)); err != nil {
		t.Fatal(err)
	}
}
