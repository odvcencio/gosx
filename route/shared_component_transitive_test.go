package route

import (
	"fmt"
	"m31labs.dev/gosx"
	ip "m31labs.dev/gosx/island/program"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedTransitiveCycles(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		t.Run(fmt.Sprint(indirect), func(t *testing.T) {
			root := t.TempDir()
			writeRouteFile(t, root, "page.gsx", "package app\nimport ui \"./ui\"\ncomponent Page() {\nreturn <ui.Card/>\n}\n")
			target := "./"
			name := "Card"
			if indirect {
				target = "./other"
				name = "Other"
			}
			writeRouteFile(t, root, "ui/card.gsx", fmt.Sprintf("package ui\nimport x %q\ncomponent Card() {\nreturn <x.%s/>\n}\n", target, name))
			if indirect {
				writeRouteFile(t, root, "ui/other/other.gsx", "package other\nimport x \"../\"\ncomponent Other() {\nreturn <x.Card/>\n}\n")
			}
			var prior string
			for i := 0; i < 2; i++ {
				node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx")})
				if err == nil || !strings.Contains(err.Error(), "shared component cycle") {
					t.Fatalf("cycle: %v", err)
				}
				if !node.IsZero() {
					t.Fatal("partial output")
				}
				if i > 0 && prior != err.Error() {
					t.Fatal("unstable cycle diagnostic")
				}
				prior = err.Error()
			}
		})
	}
}

func TestSharedTransitiveRejectsInteractiveClosure(t *testing.T) {
	for _, directive := range []string{"//gosx:island", "//gosx:engine worker"} {
		for _, transitive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", directive, transitive), func(t *testing.T) {
				root := t.TempDir()
				writeRouteFile(t, root, "page.gsx", "package app\nimport ui \"./ui\"\ncomponent Page() {\nreturn <ui.Card><Caller/></ui.Card>\n}\n//gosx:island\ncomponent Caller() {\nreturn <button>caller</button>\n}\n")
				leaf := directive + "\ncomponent Live() {\nreturn <button>live</button>\n}\n"
				if transitive {
					writeRouteFile(t, root, "ui/card.gsx", "package ui\nimport p \"./primitives\"\ncomponent Card() {\nreturn <section>{children}<p.Live/></section>\n}\n")
					writeRouteFile(t, root, "ui/primitives/live.gsx", "package primitives\n"+leaf)
				} else {
					writeRouteFile(t, root, "ui/card.gsx", "package ui\ncomponent Card() {\nreturn <section>{children}<Live/></section>\n}\n"+leaf)
				}
				registrations := 0
				node, err := renderGSXFile(filepath.Join(root, "page.gsx"), fileRenderOptions{EvalEnv: fileRenderEnv{renderIsland: func(*ip.Program, any) gosx.Node { registrations++; return gosx.Text("registered") }}}, "")
				want := "strict pure-view components only"
				if strings.Contains(directive, "engine") {
					want = "strict engine declarations are not yet supported"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("closure: %v", err)
				}
				if !node.IsZero() || registrations != 0 {
					t.Fatalf("output/registrations leaked: %v %d", node, registrations)
				}
			})
		}
	}
}

func TestSharedTransitiveDependencyRefreshAndRoots(t *testing.T) {
	for _, value := range []string{"one", "two"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			writeRouteFile(t, root, "page.gsx", "package app\nimport ui \"./ui\"\ncomponent Page() {\nreturn <ui.Card/>\n}\n")
			writeRouteFile(t, root, "ui/card.gsx", "package ui\nimport p \"./primitives\"\ncomponent Card() {\nreturn <p.Label/>\n}\n")
			render := func() (string, error) {
				node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx")})
				return gosx.RenderHTML(node), err
			}
			writeRouteFile(t, root, "ui/primitives/neighbor.gsx", "package primitive\ncomponent Neighbor() {\nreturn <i/>\n}\n")
			if _, err := render(); err == nil {
				t.Fatal("missing label accepted")
			}
			for _, text := range []string{value, value + " edited"} {
				writeRouteFile(t, root, "ui/primitives/label.gsx", "package primitive\ncomponent Label() {\nreturn <b>"+text+"</b>\n}\n")
				if got, err := render(); err != nil || got != "<b>"+text+"</b>" {
					t.Fatalf("%q %v", got, err)
				}
			}
			if err := os.Remove(filepath.Join(root, "ui/primitives/label.gsx")); err != nil {
				t.Fatal(err)
			}
			if _, err := render(); err == nil {
				t.Fatal("deleted label accepted")
			}
		})
	}
}

func TestSharedTransitiveLegacyEngineClosure(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", "package app\nimport ui \"./ui\"\ncomponent Page() {\nreturn <ui.Card/>\n}\n")
	writeRouteFile(t, root, "ui/card.gsx", `package ui
type Props struct { Title string }
component Card() {
 return <Live Title="x"/>
}
//gosx:engine worker
func Live(props Props) Node {
 return <button>{props.Title}</button>
}
`)
	node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx")})
	if err == nil || !strings.Contains(err.Error(), "strict pure-view components only") || !node.IsZero() {
		t.Fatalf("legacy engine closure: %v", err)
	}
}

func TestSharedTransitiveEngineBuiltinPrecedence(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprint(strict), func(t *testing.T) {
			root := t.TempDir()
			writeRouteFile(t, root, "page.gsx", "package app\nimport ui \"./ui\"\ncomponent Page() {\nreturn <ui.Card/>\n}\n")
			declaration := "func Scene3D(props Props) Node"
			if strict {
				declaration = "component Scene3D(props: Props)"
			}
			writeRouteFile(t, root, "ui/card.gsx", "package ui\ntype Props struct { Title string }\ncomponent Card() {\nreturn <Scene3D Title=\"safe\"/>\n}\n"+declaration+" {\nreturn <b>{props.Title}</b>\n}\n")
			node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx")})
			if strict {
				if err != nil || gosx.RenderHTML(node) != "<b>safe</b>" {
					t.Fatalf("strict precedence: %v %s", err, gosx.RenderHTML(node))
				}
			} else if err == nil || !strings.Contains(err.Error(), "engine builtin Scene3D") {
				t.Fatalf("legacy precedence: %v", err)
			}
		})
	}
}

func TestSharedTransitiveDistinctCallerRoots(t *testing.T) {
	root := t.TempDir()
	page := "package app\nimport ui \"./ui\"\ncomponent Page() {\nreturn <ui.Card/>\n}\n"
	card := "package ui\nimport p \"./primitives\"\ncomponent Card() {\nreturn <section><p.Label/></section>\n}\n"
	for _, side := range []string{"home", "away"} {
		writeRouteFile(t, root, side+"/page.gsx", page)
		writeRouteFile(t, root, side+"/ui/card.gsx", card)
		writeRouteFile(t, root, side+"/ui/primitives/label.gsx", "package primitive\ncomponent Label() {\nreturn <b>"+side+"</b>\n}\n")
	}
	for _, side := range []string{"home", "away", "home", "away"} {
		node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, side, "page.gsx"), Pattern: "/" + side})
		if err != nil {
			t.Fatal(err)
		}
		if got := gosx.RenderHTML(node); got != "<section><b>"+side+"</b></section>" {
			t.Fatalf("%s: %q", side, got)
		}
	}
}

func TestSharedTransitivePureViews(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package app
import ui "./ui"
component Page() {
 return <main><ui.Card Title="first"><b>caller</b></ui.Card><ui.Card Title="second"><i>next</i></ui.Card></main>
}
`)
	writeRouteFile(t, root, "ui/card.gsx", `package ui
import p "./primitives"
type Props struct { Title string }
component Card(props: Props) {
 return <section><p.Label Title={props.Title}>{children}</p.Label></section>
}
`)
	writeRouteFile(t, root, "ui/primitives/label.gsx", `package primitive
type Props struct { Title string }
component Label(props: Props) {
 return <label>{props.Title}{children}</label>
}
`)
	node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx"), Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := gosx.RenderHTML(node); got != `<main><section><label>first<b>caller</b></label></section><section><label>second<i>next</i></label></section></main>` {
		t.Fatal(got)
	}
}
