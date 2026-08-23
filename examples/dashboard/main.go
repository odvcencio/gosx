// Example dashboard demonstrates a realistic GoSX application.
//
// Features:
// - Multiple routes with shared layout
// - Server-rendered pages
// - Interactive islands (counter, filters)
// - Forms with server actions
// - Tables with data loading
// - Hydration manifest generation
//
// Run: go run ./examples/dashboard
// Visit: http://localhost:3000
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	_ "m31labs.dev/gosx/examples/dashboard/modules"
	"m31labs.dev/gosx/highlight"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

func main() {
	// Build the Counter island program.
	// This uses the reference CounterProgram which has real signals, handlers,
	// and expression opcodes the VM can execute. The .gsx compilation pipeline
	// is proven by test/gsx_pipeline_test.go; here we need a functional counter.
	counterProgram := program.CounterProgram()
	counterProgramJSON, err := program.EncodeJSON(counterProgram)
	if err != nil {
		log.Fatalf("encode island program: %v", err)
	}
	log.Printf("Island program: %s (%d nodes, %d exprs, %d bytes JSON)",
		counterProgram.Name, len(counterProgram.Nodes), len(counterProgram.Exprs), len(counterProgramJSON))

	tabsProg := program.TabsProgram()
	tabsJSON, _ := program.EncodeJSON(tabsProg)
	log.Printf("Island: %s (%d nodes, %d bytes)", tabsProg.Name, len(tabsProg.Nodes), len(tabsJSON))

	toggleProg := program.ToggleProgram()
	toggleJSON, _ := program.EncodeJSON(toggleProg)
	log.Printf("Island: %s (%d nodes, %d bytes)", toggleProg.Name, len(toggleProg.Nodes), len(toggleJSON))

	todoProg := program.TodoProgram()
	todoJSON, _ := program.EncodeJSON(todoProg)
	log.Printf("Island: %s (%d nodes, %d bytes)", todoProg.Name, len(todoProg.Nodes), len(todoJSON))

	formProg := program.FormProgram()
	formJSON, _ := program.EncodeJSON(formProg)
	log.Printf("Island: %s (%d nodes, %d bytes)", formProg.Name, len(formProg.Nodes), len(formJSON))

	derivedProg := program.DerivedProgram()
	derivedJSON, _ := program.EncodeJSON(derivedProg)
	log.Printf("Island: %s (%d nodes, %d bytes)", derivedProg.Name, len(derivedProg.Nodes), len(derivedJSON))

	editorProg := program.EditorProgram()
	editorJSON, _ := program.EncodeJSON(editorProg)
	log.Printf("Island: %s (%d nodes, %d bytes)", editorProg.Name, len(editorProg.Nodes), len(editorJSON))

	listProg := program.ListProgram()
	listJSON, _ := program.EncodeJSON(listProg)
	log.Printf("Island: %s (%d nodes, %d bytes)", listProg.Name, len(listProg.Nodes), len(listJSON))

	_, thisFilePath, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFilePath)

	// Router
	router := route.NewRouter()

	// newIslands creates a fresh island renderer per request to avoid manifest accumulation
	newIslands := func() *island.Renderer {
		r := island.NewRenderer("dashboard")
		r.SetBundle("dashboard", "/gosx/runtime.wasm")
		r.SetRuntime("/gosx/runtime.wasm", "", 0)
		r.SetProgramDir("/gosx/islands")
		r.SetProgramFormat("json")
		return r
	}

	// The default layout wraps only the document shell (doctype, head, css).
	// The sidebar/main/footer chrome lives in app/layout.gsx and applies
	// automatically to every page AddDir discovers below — /counter and
	// /kitchen-sink keep their own Layout override (see below) because they
	// inject island preload hints and page-head scripts this shell has no
	// hook for; see the comment on app/layout.gsx's Layout entry.
	router.SetLayout(func(ctx *route.RouteContext, content gosx.Node) gosx.Node {
		return gosx.RawHTML(fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<link rel="stylesheet" href="/static/styles.css">
</head>
<body>
`, ctx.Title("Dashboard")) + gosx.RenderHTML(content) + "\n\n</body>\n</html>")
	})

	if err := router.AddDir(filepath.Join(baseDir, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}
	if err := registerManagedActions(router); err != nil {
		log.Fatal(err)
	}

	router.Add(
		route.Route{
			Pattern: "/counter",
			Layout: func(ctx *route.RouteContext, content gosx.Node) gosx.Node {
				isl := newIslands()
				countStr := ctx.Query("count")
				count, _ := strconv.Atoi(countStr)
				return chromeLayout("Dashboard", isl, CounterPage(count, isl))
			},
			Handler: func(ctx *route.RouteContext) gosx.Node {
				return gosx.Text("") // content built in layout
			},
		},
		route.Route{
			Pattern: "/kitchen-sink",
			Layout: func(ctx *route.RouteContext, content gosx.Node) gosx.Node {
				isl := newIslands()
				return chromeLayout("Dashboard", isl, KitchenSinkPage(isl))
			},
			Handler: func(ctx *route.RouteContext) gosx.Node {
				return gosx.Text("")
			},
		},
	)

	mux := http.NewServeMux()

	// Resolve paths relative to this source file so it works from any working directory
	exampleDir := baseDir
	moduleDir := filepath.Join(exampleDir, "..", "..")

	// Static CSS
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(exampleDir, "static")))))

	// noCacheFile serves assets that change frequently (JS, island programs during dev).
	noCacheFile := func(contentType, path string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			http.ServeFile(w, r, path)
		}
	}

	// GoSX client assets
	buildDir := filepath.Join(exampleDir, "build")

	// During development: no-cache on WASM to pick up rebuilds.
	// In production, use content-hash URLs with immutable caching.
	mux.HandleFunc("GET /gosx/runtime.wasm", noCacheFile("application/wasm", filepath.Join(buildDir, "gosx-runtime.wasm")))
	mux.HandleFunc("GET /gosx/wasm_exec.js", noCacheFile("", filepath.Join(buildDir, "wasm_exec.js")))
	mux.HandleFunc("GET /gosx/bootstrap.js", noCacheFile("", filepath.Join(moduleDir, "client", "js", "bootstrap.js")))
	mux.HandleFunc("GET /gosx/patch.js", noCacheFile("", filepath.Join(moduleDir, "client", "js", "patch.js")))
	// Serve compiled island programs — all no-cache for reliable iteration
	noCacheJSON := func(data []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			w.Write(data)
		}
	}
	mux.HandleFunc("GET /gosx/islands/Counter.json", noCacheJSON(counterProgramJSON))
	mux.HandleFunc("GET /gosx/islands/Tabs.json", noCacheJSON(tabsJSON))
	mux.HandleFunc("GET /gosx/islands/Toggle.json", noCacheJSON(toggleJSON))
	mux.HandleFunc("GET /gosx/islands/Todo.json", noCacheJSON(todoJSON))
	mux.HandleFunc("GET /gosx/islands/Form.json", noCacheJSON(formJSON))
	mux.HandleFunc("GET /gosx/islands/Derived.json", noCacheJSON(derivedJSON))
	mux.HandleFunc("GET /gosx/islands/Editor.json", noCacheJSON(editorJSON))
	mux.HandleFunc("GET /gosx/islands/List.json", noCacheJSON(listJSON))

	rootHandler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	manager, err := session.New("gosx-dashboard-session-secret", session.Options{CookieName: "gosx_dashboard", AllowInsecure: true})
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", manager.Middleware(manager.Protect(rootHandler)))

	addr := ":3000"
	fmt.Printf("GoSX dashboard at http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func registerManagedActions(router *route.Router) error {
	if router == nil {
		return errors.New("managed action router is nil")
	}
	installers := []func(*route.Router) error{
		registerSettingsAction,
		registerCreateUserAction,
	}
	for _, install := range installers {
		if err := install(router); err != nil {
			return err
		}
	}
	return nil
}

func registerSettingsAction(router *route.Router) error {
	return router.RegisterManagedPOST("saveSettings", action.Config{}, func(ctx *action.Context) (action.Result, error) {
		siteName := strings.TrimSpace(ctx.Form.Value("siteName"))
		theme := strings.TrimSpace(ctx.Form.Value("theme"))
		pageSize, err := strconv.Atoi(strings.TrimSpace(ctx.Form.Value("pageSize")))
		fieldErrors := map[string]string{}
		if siteName == "" {
			fieldErrors["siteName"] = "Site name is required."
		}
		if theme != "light" && theme != "dark" {
			fieldErrors["theme"] = "Choose light or dark."
		}
		if err != nil || pageSize < 10 || pageSize > 100 {
			fieldErrors["pageSize"] = "Choose a value from 10 to 100."
		}
		if len(fieldErrors) > 0 {
			return action.Result{}, action.Validation("Check the highlighted settings.", fieldErrors)
		}
		return action.Result{OK: true, Message: "Settings saved.", Redirect: "/settings"}, nil
	})
}

func registerCreateUserAction(router *route.Router) error {
	return router.RegisterManagedPOST("createUser", action.Config{}, func(ctx *action.Context) (action.Result, error) {
		name := strings.TrimSpace(ctx.Form.Value("name"))
		email := strings.TrimSpace(ctx.Form.Value("email"))
		role := strings.TrimSpace(ctx.Form.Value("role"))
		fieldErrors := map[string]string{}
		if name == "" {
			fieldErrors["name"] = "Name is required."
		}
		if !strings.Contains(email, "@") {
			fieldErrors["email"] = "Enter a valid email address."
		}
		switch role {
		case "viewer", "editor", "admin":
		default:
			fieldErrors["role"] = "Choose a valid role."
		}
		if len(fieldErrors) > 0 {
			return action.Result{}, action.Validation("Check the highlighted user fields.", fieldErrors)
		}
		return action.Result{OK: true, Message: "User created.", Redirect: "/users"}, nil
	})
}

// chromeLayout wraps content with layout.gsx's Layout component: the
// shared navigation, document shell, and island hydration hooks every
// /counter and /kitchen-sink response needs (gosx#249). Sidebar nests
// inside Layout's own body with no data of its own to supply — see
// layout.gsx. Footer needs one per-request value (chromeFooter's message,
// built with time.Now()), which a strict .gsx expression cannot compute,
// so it renders through chromeFooter here and folds into the same
// Fragment as content, matching this file's pre-conversion nesting of
// content and the footer together inside the layout's main element.
//
// islands, when non-nil, has already registered every island content
// contains by the time this function runs: content is a fully built
// gosx.Node BEFORE chromeLayout is called (CounterPage/KitchenSinkPage run
// as an argument to this call, and Go evaluates a function's arguments
// before the call), so PreloadHints and PageHead already reflect it here —
// the same "content built first, then the layout wraps it" order
// route.LayoutFunc's own contract guarantees for a file-routed layout
// (route/filesystem.go's applyLayoutFuncs), not a special case this
// function has to arrange for itself.
func chromeLayout(title string, islands *island.Renderer, content gosx.Node) gosx.Node {
	preloadHints := gosx.Text("")
	pageHead := gosx.Text("")
	if islands != nil {
		preloadHints = islands.PreloadHints()
		pageHead = islands.PageHead()
	}
	html, err := route.RenderProgramComponent(layoutProgram, "Layout", route.ProgramRenderEnv{
		Slots: map[string]gosx.Node{
			"Title":        gosx.Text(title),
			"PreloadHints": preloadHints,
			"PageHead":     pageHead,
		},
	}, gosx.Fragment(content, chromeFooter()))
	if err != nil {
		log.Fatalf("render layout.gsx Layout: %v", err)
	}
	// <!DOCTYPE html> has no .gsx element spelling — a doctype is not a tag
	// — so it is prepended here, in Go, the one part of the pre-conversion
	// document shell layout.gsx's Layout component does not itself express.
	return gosx.RawHTML("<!DOCTYPE html>\n" + html)
}

// chromeFooter renders layout.gsx's Footer component with the one
// per-request value it needs: a version-and-timestamp line built with
// time.Now(), which a strict .gsx expression cannot call (calls are
// categorically unsupported in a strict server expression — see
// strictcomponent.validate's CallExpr case). Message is a proved prop, not
// a slot: it is scalar data, not markup, and a slot's contract carries
// only "one opaque gosx.Node" the same way children's does.
func chromeFooter() gosx.Node {
	message := fmt.Sprintf("GoSX v%s — Server rendered at %s", gosx.Version, time.Now().Format("15:04:05"))
	node, err := route.RenderProgramComponentNode(layoutProgram, "Footer", route.ProgramRenderEnv{
		Props: struct{ Message string }{Message: message},
	})
	if err != nil {
		log.Fatalf("render layout.gsx Footer: %v", err)
	}
	return node
}

// CounterPage demonstrates an interactive island compiled from counter.gsx.
//
// The server-rendered HTML uses buttons with data-gosx-handler attributes that
// match the handler names in the compiled IslandProgram. The event delegation
// system picks these up and dispatches to the WASM VM.
//
// For browsers without WASM/JS, a <noscript> fallback provides link-based navigation.
func CounterPage(count int, islands *island.Renderer) gosx.Node {
	// Server-render the counter matching the .gsx island structure:
	//   <div class="counter">
	//     <button data-gosx-handler="decrement">-</button>
	//     <span class="count">{count}</span>
	//     <button data-gosx-handler="increment">+</button>
	//   </div>
	counterContent := gosx.El("div", gosx.Attrs(gosx.Attr("class", "counter")),
		gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "decrement")), gosx.Text("-")),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "count")), gosx.Expr(count)),
		gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "increment")), gosx.Text("+")),
	)

	type counterProps struct {
		Initial int `json:"initial"`
	}

	islandNode := islands.RenderIslandWithEvents(
		"Counter",
		counterProps{Initial: count},
		[]hydrate.EventSlot{
			{SlotID: "dec", EventType: "click", HandlerName: "decrement"},
			{SlotID: "inc", EventType: "click", HandlerName: "increment"},
		},
		counterContent,
	)

	// Fallback for no-JS: link-based counter
	noscriptFallback := gosx.RawHTML(fmt.Sprintf(`<noscript><div class="counter-display"><a href="/counter?count=%d">-</a> <span>%d</span> <a href="/counter?count=%d">+</a></div></noscript>`, count-1, count, count+1))

	return gosx.Fragment(
		chrome("CounterIntro"),
		chromeCard(chrome("CounterCardHeader"), islandNode, noscriptFallback),
		chrome("CounterHowItWorksCard"),
	)
}

// KitchenSinkPage renders all island patterns on a single page.
func KitchenSinkPage(islands *island.Renderer) gosx.Node {
	// === COUNTER ===
	counterIsland := islands.RenderIslandFromProgram(program.CounterProgram(), map[string]int{"initial": 0})

	// === TABS (with dynamic CSS class toggling) ===
	tabsIsland := islands.RenderIslandFromProgram(program.TabsProgram(), nil)

	// === TOGGLE (click + keyboard handler) ===
	toggleIsland := islands.RenderIslandFromProgram(program.ToggleProgram(), nil)

	// === TODO ===
	todoContent := gosx.El("div", gosx.Attrs(gosx.Attr("class", "todo")),
		gosx.El("h3", gosx.Text("Todo List")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "todo-input")),
			gosx.El("span", gosx.Text("")),
			gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "addItem")), gosx.Text("Add")),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "todo-items")),
			gosx.El("p", gosx.Text("")),
		),
		gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "clearAll")), gosx.Text("Clear All")),
	)
	todoIsland := islands.RenderIslandWithEvents("Todo",
		nil,
		[]hydrate.EventSlot{
			{SlotID: "add", EventType: "click", HandlerName: "addItem"},
			{SlotID: "clr", EventType: "click", HandlerName: "clearAll"},
		},
		todoContent,
	)

	// === FORM (two-way input binding via OpEventGet) ===
	formContent := gosx.El("div", gosx.Attrs(gosx.Attr("class", "form-demo")),
		gosx.El("h3", gosx.Text("Form Validation")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "form-field")),
			gosx.El("label", gosx.Text("Name")),
			gosx.RawHTML(`<input type="text" placeholder="Enter name..." data-gosx-on-input="updateName" />`),
			gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "fillName")), gosx.Text("Fill Name")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "field-value")), gosx.Text("")),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "form-status")),
			gosx.El("p", gosx.Text("Please fill in name")),
		),
		gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "validateForm")), gosx.Text("Validate")),
	)
	formIsland := islands.RenderIslandWithEvents("Form",
		nil,
		[]hydrate.EventSlot{
			{SlotID: "fi", EventType: "input", HandlerName: "updateName"},
			{SlotID: "fn", EventType: "click", HandlerName: "fillName"},
			{SlotID: "fv", EventType: "click", HandlerName: "validateForm"},
		},
		formContent,
	)

	// === DERIVED / PRICE CALCULATOR ===
	derivedIsland := islands.RenderIslandFromProgram(program.DerivedProgram(), nil)

	// === LIST (dynamic list rendering) ===
	listContent := gosx.El("div", gosx.Attrs(gosx.Attr("class", "list-demo")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "list-input")),
			gosx.RawHTML(`<input type="text" placeholder="Add item..." data-gosx-on-input="addItem" />`),
			gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "addItem")), gosx.Text("Add")),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "list-display")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "item-count")), gosx.Text("0 items")),
			gosx.El("pre", gosx.Attrs(gosx.Attr("class", "item-list")), gosx.Text("")),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "list-actions")),
			gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "removeLastItem")), gosx.Text("Remove Last")),
			gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "clearItems")), gosx.Text("Clear All")),
		),
	)
	listIsland := islands.RenderIslandWithEvents("List",
		nil,
		[]hydrate.EventSlot{
			{SlotID: "lai", EventType: "input", HandlerName: "addItem"},
			{SlotID: "la", EventType: "click", HandlerName: "addItem"},
			{SlotID: "lr", EventType: "click", HandlerName: "removeLastItem"},
			{SlotID: "lc", EventType: "click", HandlerName: "clearItems"},
		},
		listContent,
	)

	// === CODE EDITOR ===
	// The editor uses an overlay pattern: a transparent textarea for input
	// (native cursor, selection, undo) with a highlighted <pre> layer behind it.
	// __gosx_highlight provides live syntax coloring when that optional export is available.
	sampleCode := `package main

import "fmt"

func main() {
	// GoSX code editor with syntax highlighting
	name := "world"
	fmt.Println("Hello, " + name + "!")
}`
	editorContent := gosx.El("div", gosx.Attrs(gosx.Attr("class", "editor")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "editor-toolbar")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "editor-title")), gosx.Text("editor.go")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "editor-lang")), gosx.Text("Go")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "char-count")),
				gosx.El("span", gosx.Text(fmt.Sprintf("%d", len(sampleCode)))),
				gosx.Text(" chars"),
			),
			gosx.El("button", gosx.Attrs(gosx.Attr("data-gosx-handler", "clear"), gosx.Attr("class", "editor-btn")), gosx.Text("Clear")),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "editor-body")),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "line-numbers"), gosx.Attr("id", "editor-lines")),
				gosx.RawHTML(lineNumbersHTML(strings.Count(sampleCode, "\n")+1)),
			),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "editor-area")),
				gosx.El("pre", gosx.Attrs(gosx.Attr("class", "code-highlight"), gosx.Attr("id", "editor-highlight")),
					gosx.RawHTML(serverHighlight(sampleCode)),
				),
				gosx.El("textarea", gosx.Attrs(
					gosx.Attr("class", "editor-textarea"),
					gosx.Attr("spellcheck", "false"),
					gosx.Attr("autocomplete", "off"),
					gosx.Attr("autocorrect", "off"),
					gosx.Attr("autocapitalize", "off"),
				), gosx.Text(sampleCode)),
			),
		),
	)
	editorIsland := islands.RenderIslandWithEvents("Editor",
		nil,
		[]hydrate.EventSlot{
			{SlotID: "clr", EventType: "click", HandlerName: "clear"},
		},
		editorContent,
	)

	// Inline script for editor-specific behavior:
	// - Syncs textarea scroll with highlight layer
	// - Uses __gosx_highlight for live syntax highlighting when available
	// - Updates line numbers
	editorScript := gosx.RawHTML(`<script>
(function() {
  function setupEditor() {
    var ta = document.querySelector('.editor-textarea');
    var hl = document.getElementById('editor-highlight');
    var ln = document.getElementById('editor-lines');
    if (!ta || !hl) return;

    function update() {
      var code = ta.value;
      // Syntax highlight when the optional runtime highlighter is present.
      if (typeof window.__gosx_highlight === 'function') {
        hl.innerHTML = window.__gosx_highlight(code, 'go') + '\n';
      } else {
        hl.textContent = code + '\n';
      }
      // Update line numbers
      var lines = (code.match(/\n/g) || []).length + 1;
      var nums = [];
      for (var i = 1; i <= lines; i++) nums.push(i);
      if (ln) ln.textContent = nums.join('\n');
      // Update char count
      var cc = document.querySelector('.char-count span');
      if (cc) cc.textContent = code.length;
    }

    ta.addEventListener('input', update);

    // Clear button
    var clearBtn = document.querySelector('.editor-btn');
    if (clearBtn) {
      clearBtn.addEventListener('click', function(e) {
        e.stopPropagation();
        ta.value = '';
        update();
        ta.focus();
      });
    }

    ta.addEventListener('scroll', function() {
      hl.scrollTop = ta.scrollTop;
      hl.scrollLeft = ta.scrollLeft;
      if (ln) ln.scrollTop = ta.scrollTop;
    });

    // Initial highlight after WASM loads
    if (window.__gosx && window.__gosx.ready) {
      update();
    } else {
      document.addEventListener('gosx:ready', update);
      // Also try after a delay in case the event already fired
      setTimeout(update, 3000);
    }
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', setupEditor);
  } else {
    setupEditor();
  }
})();
</script>`)

	return gosx.Fragment(
		chrome("KitchenSinkIntro"),
		chromeCard(chrome("KSCounterHeader"), counterIsland),
		chromeCard(chrome("KSTabsHeader"), tabsIsland),
		chromeCard(chrome("KSToggleHeader"), toggleIsland),
		chromeCard(chrome("KSTodoHeader"), todoIsland),
		chromeCard(chrome("KSFormHeader"), formIsland),
		chromeCard(chrome("KSPriceHeader"), derivedIsland),
		chromeCard(chrome("KSListHeader"), listIsland),
		chromeCard(chrome("KSEditorHeader"), editorIsland, editorScript),
	)
}

// serverHighlight produces syntax-highlighted HTML on the server.
// This is the initial render — the client updates it live via __gosx_highlight.
func serverHighlight(source string) string {
	return highlight.HTML(highlight.LangGo, source)
}

func lineNumbersHTML(count int) string {
	return highlight.LineNumbers(count)
}
