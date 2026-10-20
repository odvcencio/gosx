package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestStrictFormStateCSRFContract(t *testing.T) {
	for _, tc := range []struct {
		name, method, action, contents string
		missing, warning               bool
	}{
		{"missing", "post", "props.Form.ActionURL", `<button>Save</button>`, true, false},
		{"missing with field messages", "post", "props.Form.ActionURL", `<p>{props.Form.FieldErrors["email"]}</p><p>{props.Form.Message}</p>`, true, false},
		{"nested missing", "patch", "props.Nested.Form.ActionURL", `<button>Save</button>`, true, false},
		{"token", "post", "props.Form.ActionURL", `<div><input name="csrf_token" value={props.Form.CSRFToken} /></div>`, false, false},
		{"get", "get", "props.Form.ActionURL", `<button>Save</button>`, false, false},
		{"native props URL", "post", "props.NativeURL", `<button>Save</button>`, false, false},
		{"application FormState", "post", "props.Fake.ActionURL", `<button>Save</button>`, false, false},
		{"dynamic descendant", "post", "props.Form.ActionURL", `{children}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newTestModule(t)
			mustWrite(t, filepath.Join(dir, "gosxstub", "route", "formstate.go"), `package route
type FormState struct {
	ActionURL string
	CSRFToken string
	Values map[string]string
	FieldErrors map[string]string
	Flash map[string]string
	Message string
	OK bool
	Status int
}
`)
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, `package app
import forms "m31labs.dev/gosx/route"
type FormState struct { ActionURL string }
type Nested struct { Form forms.FormState }
type PageProps struct { Form forms.FormState; Nested Nested; NativeURL string; Fake FormState }
component Page(props: PageProps) {
	return <form method="`+tc.method+`" action={`+tc.action+`}>`+tc.contents+`</form>
}
`)
			var warnings []ir.Diagnostic
			err := CheckFileWithOptions(context.Background(), path, Options{Warnings: &warnings})
			if tc.missing {
				if err == nil || !strings.Contains(err.Error(), tc.action) || !strings.Contains(err.Error(), "csrf_token") || !strings.Contains(err.Error(), strings.TrimSuffix(tc.action, "ActionURL")+"CSRFToken") {
					t.Fatalf("missing token diagnostic: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			foundWarning := false
			for _, warning := range warnings {
				foundWarning = foundWarning || strings.Contains(warning.Message, "csrf_token")
			}
			if foundWarning != tc.warning {
				t.Fatalf("CSRF warning = %v, want %v: %v", foundWarning, tc.warning, warnings)
			}
		})
	}
}
