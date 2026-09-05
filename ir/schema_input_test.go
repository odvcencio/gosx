package ir_test

import (
	"m31labs.dev/gosx/ir"
	"strings"
	"testing"
)

func TestSchemaSetValidation(t *testing.T) {
	origin := ir.SchemaOrigin{File: "model.go", Line: 4, Column: 2}
	for _, tc := range []struct{ name, typ, want string }{
		{"Title", "string", ""}, {"hidden", "string", "invalid field"}, {"Title", "*Other", "unsupported type"}, {"Title", "pkg.Other", "unsupported type"}, {"Title", "map[string]string", "unsupported type"}, {"Title", "Alias", "unsupported type"}, {"Title", "[]string", "unsupported type"}, {"Title", " string", "invalid type"}, {"Title", "Props", "cycle"},
	} {
		t.Run(tc.name+tc.typ, func(t *testing.T) {
			_, err := ir.NewSchemaSet("app", []ir.StructSchema{{Name: "Props", Origin: origin, Fields: []ir.SchemaField{{Name: tc.name, Type: tc.typ}}}})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
		})
	}
	first := ir.StructSchema{Name: "Props", Origin: ir.SchemaOrigin{File: "a.go", Line: 2}}
	second := ir.StructSchema{Name: "Props", Origin: ir.SchemaOrigin{File: "z.go", Line: 7}}
	_, a := ir.NewSchemaSet("app", []ir.StructSchema{first, second})
	_, b := ir.NewSchemaSet("app", []ir.StructSchema{second, first})
	if a == nil || b == nil || a.Error() != b.Error() || a.Error() != "duplicate schema Props: a.go:2:0 and z.go:7:0" {
		t.Fatal(a, b)
	}
	_, err := ir.NewSchemaSet("app", []ir.StructSchema{{Name: "Props", Origin: origin, Fields: []ir.SchemaField{{Name: "Title", Type: "string"}, {Name: "Title", Type: "bool"}}}})
	if err == nil || !strings.Contains(err.Error(), "duplicate field") {
		t.Fatal(err)
	}
	if _, err := ir.NewSchemaSet("not a package", []ir.StructSchema{first}); err == nil {
		t.Fatal("package accepted")
	}
	first.Name = "modelProps"
	if _, err := ir.NewSchemaSet("app", []ir.StructSchema{first}); err != nil {
		t.Fatal(err)
	}
	first.Name = "string"
	if _, err := ir.NewSchemaSet("app", []ir.StructSchema{first}); err == nil || !strings.Contains(err.Error(), "predeclared type") {
		t.Fatal(err)
	}
}

func TestSchemaSetCopiesAndAliases(t *testing.T) {
	input := []ir.StructSchema{{Name: "Props", Origin: ir.SchemaOrigin{File: "model.go"}, Fields: []ir.SchemaField{{Name: "URL", Type: "string"}, {Name: "Title", Type: "string"}}}}
	schema, err := ir.NewSchemaSet("app", input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Name = "Changed"
	input[0].Fields[0].Type = "bool"
	got := schema.Structs()
	if got[0].Name != "Props" {
		t.Fatal(got)
	}
	aliases := ir.SchemaAliases(got[0])
	if aliases["url"] != "URL" || aliases["title"] != "Title" {
		t.Fatal(aliases)
	}
	got[0].Fields[0].Type = "int"
	got[0].Name = "Changed"
	aliases["url"] = "Wrong"
	again := schema.Structs()
	if again[0].Name != "Props" || again[0].Fields[0].Type != "string" || ir.SchemaAliases(again[0])["url"] != "URL" {
		t.Fatal(again)
	}
	var zero ir.SchemaSet
	if len(zero.Structs()) != 0 || zero.PackageName() != "" {
		t.Fatal(zero)
	}
	var absent *ir.SchemaSet
	if absent.Structs() != nil || absent.PackageName() != "" {
		t.Fatal(absent)
	}
}
