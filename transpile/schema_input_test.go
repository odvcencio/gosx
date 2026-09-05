package transpile_test

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
	"strings"
	"sync"
	"testing"
)

func TestExplicitSchemaInput(t *testing.T) {
	schema, err := ir.NewSchemaSet("app", []ir.StructSchema{{Name: "Props", Origin: ir.SchemaOrigin{File: "model.go", Line: 3}, Fields: []ir.SchemaField{{Name: "Title", Type: "string"}}}})
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("package app\ncomponent Page(props: Props) {\n return <h1>{props.Title}</h1>\n}\n")
	if _, err := gosx.Compile(source); err == nil {
		t.Fatal("byte API unexpectedly loaded a model")
	}
	if _, err := gosx.CompileWithOptions(source, gosx.CompileOptions{Schemas: schema}); err != nil {
		t.Fatal(err)
	}
	generated, err := transpile.Transpile(source, transpile.Options{Schemas: schema})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(generated, "type Props") {
		t.Fatal("model declaration duplicated", generated)
	}
}

func TestExplicitSchemaCollisionsAndLegacyBoundary(t *testing.T) {
	schema, err := ir.NewSchemaSet("app", []ir.StructSchema{{Name: "Props", Origin: ir.SchemaOrigin{File: "model.go", Line: 3}, Fields: []ir.SchemaField{{Name: "Title", Type: "string"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range []string{"import Props \"example.test/model\"", "type Props struct {Title string}", "type Props string", "type Props = string", "var Props = 1", "const Props = 1", "func Props() {}", "component Props() {\n return <p>other</p>\n}"} {
		source := []byte("package app\n" + decl + "\ncomponent Page() {\n return <p>ok</p>\n}\n")
		if _, err := gosx.CompileWithOptions(source, gosx.CompileOptions{Schemas: schema}); err == nil || !strings.Contains(err.Error(), "source declaration Props conflicts with schema at model.go:3:0") {
			t.Fatal(err)
		}
	}
	source := []byte("package other\ncomponent Page() {\n return <p>ok</p>\n}\n")
	if _, err := gosx.CompileWithOptions(source, gosx.CompileOptions{Schemas: schema}); err == nil || !strings.Contains(err.Error(), "does not match source package") {
		t.Fatal(err)
	}
	legacy := []byte("package app\ncomponent Page() {\n return <Legacy Title=\"x\"/>\n}\nfunc Legacy(props Props) Node {\n return <p>{props.Title}</p>\n}\n")
	if _, err := gosx.CompileWithOptions(legacy, gosx.CompileOptions{Schemas: schema}); err == nil || !strings.Contains(err.Error(), "untyped legacy") {
		t.Fatal(err)
	}
}

func TestExplicitSchemaConcurrentCompile(t *testing.T) {
	schema, err := ir.NewSchemaSet("app", []ir.StructSchema{{Name: "Props", Origin: ir.SchemaOrigin{File: "model.go"}, Fields: []ir.SchemaField{{Name: "Title", Type: "string"}}}})
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("package app\ncomponent Page(props: Props) {\n return <p>{props.Title}</p>\n}\n")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := schema.Structs()
			copy[0].Fields[0].Type = "int"
			if _, err := transpile.Transpile(source, transpile.Options{Schemas: schema}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, fieldType := range []string{"string", "bool", "int"} {
		wg.Add(1)
		go func(fieldType string) {
			defer wg.Done()
			independent, err := ir.NewSchemaSet("app", []ir.StructSchema{{Name: "Props", Origin: ir.SchemaOrigin{File: fieldType + ".go"}, Fields: []ir.SchemaField{{Name: "Title", Type: fieldType}}}})
			if err != nil {
				t.Error(err)
				return
			}
			for i := 0; i < 4; i++ {
				program, err := gosx.CompileWithOptions(source, gosx.CompileOptions{Schemas: independent})
				if err != nil {
					t.Error(err)
					return
				}
				if program.Components[0].PropsFields["Title"] != fieldType {
					t.Errorf("snapshot %s leaked: %#v", fieldType, program.Components[0].PropsFields)
				}
				if _, err := transpile.Transpile(source, transpile.Options{Schemas: independent}); err != nil {
					t.Error(err)
				}
			}
		}(fieldType)
	}
	wg.Wait()
}
