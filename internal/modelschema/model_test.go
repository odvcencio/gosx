package modelschema_test

import (
	"context"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/modelschema"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredModelClosure(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package /* comment */ app\ntype Props struct { Meta MetaProps }\ncomponent Page(props: Props) {\n return <p>{props.Meta.Title}</p>\n}\n")
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	refs := modelschema.SourceRefs(tree.RootNode(), lang, source)
	path := filepath.Join(dir, "model.go")
	model := "package app\ntype MetaProps struct {Title string; private *int; cache map[string]any}\ntype Unrelated[T any] struct { Value T }\n"
	if err := os.WriteFile(path, []byte(model), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := modelschema.Resolve(context.Background(), dir, refs, modelschema.Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if refs.Package != "app" || len(snapshot.Schemas().Structs()) != 1 {
		t.Fatalf("refs %#v schema %#v", refs, snapshot.Schemas().Structs())
	}
	if _, err := gosx.CompileWithOptions(source, gosx.CompileOptions{Schemas: snapshot.Schemas()}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source string
		fail         bool
	}{
		{"model_linux_helpers.go", "package app\ntype Other struct{ X *int }", false},
		{"unrelated.go", "//go:build custom\n\npackage app\ntype Other struct {X *int}", false},
		{"alternate.go", "//go:build custom\n\npackage app\ntype MetaProps struct {Title string}", true},
		{"malformed.go", "//go:build (\n\npackage app\ntype MetaProps struct {Title string}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name)
			if err := os.WriteFile(p, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(p)
			_, err := modelschema.Resolve(context.Background(), dir, refs, modelschema.Selection{})
			if (err != nil) != tc.fail {
				t.Fatalf("err %v", err)
			}
			if tc.fail && !strings.Contains(err.Error(), "conditional") {
				t.Fatal(err)
			}
		})
	}
}

func TestModelGOOSSelection(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{"go.mod": "module example.test/models\n\ngo 1.26\n", "model_linux.go": "package app\ntype Props struct{Title string}\n", "model_windows.go": "package app\ntype Props struct{Title int}\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	refs := modelschema.Refs{Package: "app", Roots: []string{"Props"}, Declared: map[string]bool{}}
	previous := ""
	for _, tc := range []struct{ os, typ string }{{"linux", "string"}, {"windows", "int"}} {
		snapshot, err := modelschema.Resolve(context.Background(), dir, refs, modelschema.Selection{GoTool: true, Env: append(os.Environ(), "GOOS="+tc.os, "CGO_ENABLED=0")})
		if err != nil {
			t.Fatal(err)
		}
		if got := snapshot.Schemas().Structs()[0].Fields[0].Type; got != tc.typ {
			t.Fatalf("%s: %s", tc.os, got)
		}
		if snapshot.Digest() == previous {
			t.Fatal("selection digest leaked")
		}
		previous = snapshot.Digest()
	}
}

func TestCapturedDirectoryStability(t *testing.T) {
	for _, ext := range []string{".go", ".gsx"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			name := "model" + ext
			path := filepath.Join(dir, name)
			original := []byte("original")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			captured := map[string][]byte{name: original}
			if err := modelschema.VerifyDirectory(dir, ext, captured); err != nil {
				t.Fatal(err)
			}
			for _, change := range []string{"edit", "add", "delete"} {
				t.Run(change, func(t *testing.T) {
					switch change {
					case "edit":
						if err := os.WriteFile(path, []byte("modified"), 0600); err != nil {
							t.Fatal(err)
						}
					case "add":
						if err := os.WriteFile(filepath.Join(dir, "new"+ext), original, 0600); err != nil {
							t.Fatal(err)
						}
					case "delete":
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
					if err := modelschema.VerifyDirectory(dir, ext, captured); err == nil || !strings.Contains(err.Error(), "changed during capture") {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, original, 0600); err != nil {
						t.Fatal(err)
					}
					if change == "add" {
						if err := os.Remove(filepath.Join(dir, "new"+ext)); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func TestSourceOwnedModelCollisionAndIgnoredExtension(t *testing.T) {
	for _, tc := range []struct{ name, file, source, want string }{
		{"duplicate", "model.go", "package app\ntype Props struct{Title string}\n", "duplicate model Props"},
		{"conditional duplicate", "model.go", "//go:build custom\n\npackage app\ntype Props struct{Title string}\n", "conditional"},
		{"uppercase ignored", "model.GO", "this is not Go source", ""},
		{"unreachable duplicate", "model.go", "package app\ntype Other struct{Value *int}\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			refs := modelschema.Refs{Package: "app", Roots: []string{"Props"}, Declared: map[string]bool{"Props": true, "Other": true}}
			_, err := modelschema.Resolve(context.Background(), dir, refs, modelschema.Selection{})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q got %v", tc.want, err)
			}
		})
	}
}

func TestSourceOwnedModelSelectedCollision(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{"go.mod": "module example.test/models\n\ngo 1.26\n", "marker.go": "package app\n", "model.go": "//go:build custom\n\npackage app\ntype Props struct{Title string}\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	refs := modelschema.Refs{Package: "app", Roots: []string{"Props"}, Declared: map[string]bool{"Props": true}}
	for _, tags := range []string{"", " -tags=custom", ""} {
		_, err := modelschema.Resolve(context.Background(), dir, refs, modelschema.Selection{GoTool: true, Env: append(os.Environ(), "GOFLAGS=-mod=readonly"+tags)})
		if tags == "" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "duplicate model Props") {
			t.Fatal(err)
		}
	}
}

func TestSourceOwnedNestedModelCollision(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package app\ntype Props struct{Meta MetaProps}\ntype MetaProps struct{Title string}\ncomponent Page(props: Props) {\n return <p>{props.Meta.Title}</p>\n}\n")
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.go"), []byte("package app\ntype MetaProps struct{Title string}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = modelschema.Resolve(context.Background(), dir, modelschema.SourceRefs(tree.RootNode(), lang, source), modelschema.Selection{})
	if err == nil || !strings.Contains(err.Error(), "duplicate model MetaProps") {
		t.Fatal(err)
	}
}
