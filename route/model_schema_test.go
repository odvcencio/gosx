package route

import (
	"m31labs.dev/gosx"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type ModelRow struct{ Title string }
type modelPage struct{ Rows []ModelRow }

func TestFileModelPublicHTTPAndDirectories(t *testing.T) {
	for _, tag := range []string{"p", "strong"} {
		t.Run(tag, func(t *testing.T) {
			root := t.TempDir()
			writeRouteFile(t, root, "page.gsx", "package app\ncomponent Page(props: Props) {\n return <main><Each of={props.Rows} as=\"row\"><"+tag+">{row.Title}</"+tag+"></Each></main>\n}\n")
			writeRouteFile(t, root, "model.go", "package app\ntype Props struct {Rows []ModelRow}\ntype ModelRow struct {Title string}\n")
			modules := NewFileModuleRegistry()
			if err := modules.Register(FileModuleFor("page.gsx", FileModuleOptions{Load: func(*RouteContext, FilePage) (any, error) {
				return modelPage{Rows: []ModelRow{{"<one>"}, {"two"}}}, nil
			}})); err != nil {
				t.Fatal(err)
			}
			router := NewRouter()
			router.SetLayout(func(_ *RouteContext, n gosx.Node) gosx.Node { return n })
			if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
				t.Fatal(err)
			}
			handler := router.Build()
			var wg sync.WaitGroup
			for i := 0; i < 6; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
					want := "<main><" + tag + ">&lt;one&gt;</" + tag + "><" + tag + ">two</" + tag + "></main>"
					if w.Code != 200 || w.Body.String() != want {
						t.Errorf("%d %s", w.Code, w.Body.String())
					}
				}()
			}
			wg.Wait()
		})
	}
}

func TestFileModelSnapshotRefresh(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "page.gsx")
	model := filepath.Join(root, "model.go")
	writeRouteFile(t, root, "page.gsx", "package app\ncomponent Page(props: Props) {\n return <p>{props.Title}</p>\n}\n")
	good := "package app\ntype Props struct {Title string}\n"
	writeRouteFile(t, root, "model.go", good)
	render := func(want string) {
		t.Helper()
		p, err := loadCachedGSXProgram(page)
		if want != "" {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("wanted %q got %v", want, err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		html, err := RenderProgramComponent(p, "Page", ProgramRenderEnv{Props: struct{ Title string }{"<hello>"}})
		if err != nil || html != "<p>&lt;hello&gt;</p>" {
			t.Fatalf("%v %s", err, html)
		}
	}
	render("")
	stat, err := os.Stat(model)
	if err != nil {
		t.Fatal(err)
	}
	writeRouteFile(t, root, "model.go", strings.Replace(good, "Title", "Other", 1))
	if err := os.Chtimes(model, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	render("Title")
	writeRouteFile(t, root, "model.go", good)
	render("")
	writeRouteFile(t, root, "duplicate.go", good)
	render("duplicate model")
	if err := os.Remove(filepath.Join(root, "duplicate.go")); err != nil {
		t.Fatal(err)
	}
	render("")
	if err := os.Remove(model); err != nil {
		t.Fatal(err)
	}
	render("struct schema")
	writeRouteFile(t, root, "model.go", good)
	render("")
	// Pure byte compilation remains unaware of owning model files.
	if _, err := gosx.Compile([]byte("package app\ncomponent Page(props: Props) {\n return <p>{props.Title}</p>\n}")); err == nil {
		t.Fatal("pure compile discovered model")
	}
}

func TestFileModelsIdenticalGSXIndependentDirectories(t *testing.T) {
	source := "package app\ncomponent Page(props: Props) {\n return <p>{props.Title}</p>\n}\n"
	paths := []string{filepath.Join(t.TempDir(), "page.gsx"), filepath.Join(t.TempDir(), "page.gsx")}
	for i, typ := range []string{"string", "int"} {
		root := filepath.Dir(paths[i])
		writeRouteFile(t, root, "page.gsx", source)
		writeRouteFile(t, root, "model.go", "package app\ntype Props struct{Title "+typ+"}\n")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			index := i % 2
			program, err := loadCachedGSXProgram(paths[index])
			if err != nil {
				t.Error(err)
				return
			}
			typ := "string"
			var props any = struct{ Title string }{"hello"}
			want := "<p>hello</p>"
			if index == 1 {
				typ = "int"
				props = struct{ Title int }{42}
				want = "<p>42</p>"
			}
			if program.Components[0].PropsFields["Title"] != typ {
				t.Errorf("directory schema leaked: %#v", program.Components[0].PropsFields)
			}
			html, err := RenderProgramComponent(program, "Page", ProgramRenderEnv{Props: props})
			if err != nil || html != want {
				t.Errorf("%v %s", err, html)
			}
		}(i)
	}
	wg.Wait()
}

func TestFileSourceOwnedModelDuplicate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	writeRouteFile(t, root, "page.gsx", "package app\ntype Props struct{Title string}\ncomponent Page(props: Props) {\n return <p>{props.Title}</p>\n}\n")
	if _, err := loadCachedGSXProgram(path); err != nil {
		t.Fatal(err)
	}
	writeRouteFile(t, root, "model.go", "package app\ntype Props struct{Title string}\n")
	if _, err := loadCachedGSXProgram(path); err == nil || !strings.Contains(err.Error(), "duplicate model Props") {
		t.Fatalf("duplicate accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "model.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCachedGSXProgram(path); err != nil {
		t.Fatal(err)
	}
}
