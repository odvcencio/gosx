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
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/island"
	"github.com/odvcencio/gosx/route"
)

func main() {
	// Action registry for server-callable actions
	actions := action.NewRegistry()
	actions.Register("createUser", func(ctx *action.Context) error {
		name := ctx.FormData["name"]
		email := ctx.FormData["email"]
		log.Printf("Creating user: %s <%s>", name, email)
		return nil
	})
	actions.Register("deleteUser", func(ctx *action.Context) error {
		log.Printf("Deleting user from request")
		return nil
	})

	// Island renderer for hydration manifest
	islands := island.NewRenderer("dashboard")
	islands.SetBundle("dashboard", "/gosx/runtime.wasm")
	islands.SetRuntime("/gosx/runtime.wasm", "", 0)

	// Router
	router := route.NewRouter()

	router.SetLayout(func(ctx *route.RouteContext, content gosx.Node) gosx.Node {
		return Layout("Dashboard", islands, content)
	})

	router.Add(
		route.Route{
			Pattern: "/",
			Handler: func(ctx *route.RouteContext) gosx.Node {
				return HomePage()
			},
		},
		route.Route{
			Pattern: "/users",
			Handler: func(ctx *route.RouteContext) gosx.Node {
				q := ctx.Query("q")
				return UsersPage(q)
			},
		},
		route.Route{
			Pattern: "/users/new",
			Handler: func(ctx *route.RouteContext) gosx.Node {
				return NewUserPage()
			},
		},
		route.Route{
			Pattern: "/settings",
			Handler: func(ctx *route.RouteContext) gosx.Node {
				return SettingsPage()
			},
		},
		route.Route{
			Pattern: "/counter",
			Handler: func(ctx *route.RouteContext) gosx.Node {
				countStr := ctx.Query("count")
				count, _ := strconv.Atoi(countStr)
				return CounterPage(count, islands)
			},
		},
	)

	mux := http.NewServeMux()
	mux.Handle("POST /gosx/action/{name}", actions)

	// Resolve paths relative to this source file so it works from any working directory
	_, thisFile, _, _ := runtime.Caller(0)
	exampleDir := filepath.Dir(thisFile)
	moduleDir := filepath.Join(exampleDir, "..", "..")

	// Static CSS
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(exampleDir, "static")))))

	// GoSX client assets
	buildDir := filepath.Join(exampleDir, "build")
	mux.HandleFunc("GET /gosx/runtime.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, filepath.Join(buildDir, "gosx-runtime.wasm"))
	})
	mux.HandleFunc("GET /gosx/wasm_exec.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(buildDir, "wasm_exec.js"))
	})
	mux.HandleFunc("GET /gosx/bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(moduleDir, "client", "js", "bootstrap.js"))
	})
	mux.HandleFunc("GET /gosx/patch.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(moduleDir, "client", "js", "patch.js"))
	})

	mux.Handle("/", router.Build())

	addr := ":3000"
	fmt.Printf("GoSX dashboard at http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// Layout wraps all pages with shared navigation and structure.
func Layout(title string, islands *island.Renderer, content gosx.Node) gosx.Node {
	return gosx.RawHTML(fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<link rel="stylesheet" href="/static/styles.css">
</head>
<body>
`, title) + gosx.RenderHTML(
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "layout")),
			Sidebar(),
			gosx.El("main", gosx.Attrs(gosx.Attr("class", "main")),
				content,
				Footer(),
			),
		),
	) + "\n" + gosx.RenderHTML(islands.PageHead()) + "\n</body>\n</html>")
}

// Sidebar renders the navigation sidebar.
func Sidebar() gosx.Node {
	type navItem struct {
		href  string
		label string
	}
	items := []navItem{
		{"/", "Home"},
		{"/users", "Users"},
		{"/users/new", "New User"},
		{"/counter", "Counter"},
		{"/settings", "Settings"},
	}

	return gosx.El("aside", gosx.Attrs(gosx.Attr("class", "sidebar")),
		gosx.El("h2", gosx.Text("GoSX Dashboard")),
		gosx.El("nav",
			gosx.Map(items, func(item navItem, _ int) gosx.Node {
				return gosx.El("a", gosx.Attrs(gosx.Attr("href", item.href)), gosx.Text(item.label))
			}),
		),
	)
}

// Footer renders the page footer.
func Footer() gosx.Node {
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "footer")),
		gosx.Text(fmt.Sprintf("GoSX v0.1.0 — Server rendered at %s", time.Now().Format("15:04:05"))),
	)
}

// HomePage renders the dashboard home.
func HomePage() gosx.Node {
	return gosx.Fragment(
		gosx.El("h1", gosx.Text("Dashboard")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "grid")),
			StatCard("Users", "1,247"),
			StatCard("Active", "892"),
			StatCard("Revenue", "$48,290"),
			StatCard("Growth", "+12.5%"),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
			gosx.El("h3", gosx.Text("Recent Activity")),
			ActivityTable(),
		),
	)
}

// StatCard renders a statistics card.
func StatCard(label, value string) gosx.Node {
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
		gosx.El("h3", gosx.Text(label)),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "stat")), gosx.Text(value)),
	)
}

// ActivityTable renders recent activity.
func ActivityTable() gosx.Node {
	type activity struct {
		user   string
		action string
		when   string
	}
	activities := []activity{
		{"Alice", "Created account", "2 min ago"},
		{"Bob", "Updated profile", "15 min ago"},
		{"Carol", "Uploaded document", "1 hour ago"},
		{"Dave", "Changed settings", "3 hours ago"},
		{"Eve", "Logged in", "5 hours ago"},
	}

	return gosx.El("table",
		gosx.El("thead",
			gosx.El("tr",
				gosx.El("th", gosx.Text("User")),
				gosx.El("th", gosx.Text("Action")),
				gosx.El("th", gosx.Text("When")),
			),
		),
		gosx.El("tbody",
			gosx.Map(activities, func(a activity, _ int) gosx.Node {
				return gosx.El("tr",
					gosx.El("td", gosx.Text(a.user)),
					gosx.El("td", gosx.Text(a.action)),
					gosx.El("td", gosx.Text(a.when)),
				)
			}),
		),
	)
}

// UsersPage renders the users list with search filtering.
func UsersPage(query string) gosx.Node {
	type user struct {
		name   string
		email  string
		role   string
		active bool
	}
	users := []user{
		{"Alice Johnson", "alice@example.com", "Admin", true},
		{"Bob Smith", "bob@example.com", "Editor", true},
		{"Carol Williams", "carol@example.com", "Viewer", false},
		{"Dave Brown", "dave@example.com", "Editor", true},
		{"Eve Davis", "eve@example.com", "Admin", true},
		{"Frank Miller", "frank@example.com", "Viewer", false},
	}

	// Filter by query
	if query != "" {
		var filtered []user
		q := strings.ToLower(query)
		for _, u := range users {
			if strings.Contains(strings.ToLower(u.name), q) || strings.Contains(strings.ToLower(u.email), q) {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}

	return gosx.Fragment(
		gosx.El("h1", gosx.Text("Users")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "search-bar")),
			gosx.RawHTML(fmt.Sprintf(`<form method="get" action="/users" class="search-form">
				<input type="text" name="q" placeholder="Search users..." value="%s" />
				<button type="submit" class="btn btn-primary">Search</button>
			</form>`, query)),
			gosx.El("a", gosx.Attrs(gosx.Attr("href", "/users/new"), gosx.Attr("class", "btn btn-primary")), gosx.Text("+ New User")),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
			gosx.El("table",
				gosx.El("thead",
					gosx.El("tr",
						gosx.El("th", gosx.Text("Name")),
						gosx.El("th", gosx.Text("Email")),
						gosx.El("th", gosx.Text("Role")),
						gosx.El("th", gosx.Text("Status")),
						gosx.El("th", gosx.Text("Actions")),
					),
				),
				gosx.El("tbody",
					gosx.Map(users, func(u user, _ int) gosx.Node {
						badgeClass := gosx.IfElse(u.active,
							gosx.Text("badge badge-active"),
							gosx.Text("badge badge-inactive"),
						)
						statusText := "Active"
						if !u.active {
							statusText = "Inactive"
						}
						return gosx.El("tr",
							gosx.El("td", gosx.Text(u.name)),
							gosx.El("td", gosx.Text(u.email)),
							gosx.El("td", gosx.Text(u.role)),
							gosx.El("td",
								gosx.El("span", gosx.Attrs(gosx.Attr("class", gosx.RenderHTML(badgeClass))),
									gosx.Text(statusText)),
							),
							gosx.El("td",
								gosx.El("button", gosx.Attrs(gosx.Attr("class", "btn btn-danger btn-sm")), gosx.Text("Delete")),
							),
						)
					}),
				),
			),
			gosx.Show(len(users) == 0,
				gosx.El("p", gosx.Attrs(gosx.Attr("class", "empty-state")),
					gosx.Text("No users found matching your search.")),
			),
		),
	)
}

// NewUserPage renders the user creation form.
func NewUserPage() gosx.Node {
	return gosx.Fragment(
		gosx.El("h1", gosx.Text("New User")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
			gosx.RawHTML(`<form method="post" action="/gosx/action/createUser">
				<div class="form-group">
					<label>Name</label>
					<input type="text" name="name" placeholder="Full name" required />
				</div>
				<div class="form-group">
					<label>Email</label>
					<input type="email" name="email" placeholder="email@example.com" required />
				</div>
				<div class="form-group">
					<label>Role</label>
					<select name="role">
						<option value="viewer">Viewer</option>
						<option value="editor">Editor</option>
						<option value="admin">Admin</option>
					</select>
				</div>
				<button type="submit" class="btn btn-primary">Create User</button>
				<a href="/users" class="btn btn-cancel">Cancel</a>
			</form>`),
		),
	)
}

// CounterPage demonstrates an interactive island.
func CounterPage(count int, islands *island.Renderer) gosx.Node {
	// The counter is an island — server-rendered with hydration metadata
	counterContent := gosx.El("div", gosx.Attrs(gosx.Attr("class", "counter-display")),
		gosx.El("a", gosx.Attrs(gosx.Attr("href", fmt.Sprintf("/counter?count=%d", count-1))), gosx.Text("-")),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "count")), gosx.Expr(count)),
		gosx.El("a", gosx.Attrs(gosx.Attr("href", fmt.Sprintf("/counter?count=%d", count+1))), gosx.Text("+")),
	)

	type counterProps struct {
		Initial int `json:"initial"`
	}

	islandNode := islands.RenderIslandWithEvents(
		"Counter",
		counterProps{Initial: count},
		[]hydrate.EventSlot{
			{SlotID: "dec", EventType: "click", TargetSelector: "a:first-child", HandlerName: "counterDec"},
			{SlotID: "inc", EventType: "click", TargetSelector: "a:last-child", HandlerName: "counterInc"},
		},
		counterContent,
	)

	return gosx.Fragment(
		gosx.El("h1", gosx.Text("Counter (Island Demo)")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
			gosx.El("h3", gosx.Text("Interactive Island")),
			gosx.El("p", gosx.Text("This counter is server-rendered but marked as an island for client hydration.")),
			gosx.El("p", gosx.Text("Without WASM loaded, it falls back to server navigation (links).")),
			gosx.El("br"),
			islandNode,
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
			gosx.El("h3", gosx.Text("How It Works")),
			gosx.El("p", gosx.Text("1. Server renders the counter HTML with the current count")),
			gosx.El("p", gosx.Text("2. Hydration manifest identifies this as an interactive island")),
			gosx.El("p", gosx.Text("3. Client loads only the counter's WASM bundle (not the whole page)")),
			gosx.El("p", gosx.Text("4. Signal[int] drives local reactive updates inside the island")),
		),
	)
}

// SettingsPage renders application settings.
func SettingsPage() gosx.Node {
	return gosx.Fragment(
		gosx.El("h1", gosx.Text("Settings")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
			gosx.El("h3", gosx.Text("Application Settings")),
			gosx.RawHTML(`<form method="post" action="/gosx/action/saveSettings">
				<div class="form-group">
					<label>Site Name</label>
					<input type="text" name="siteName" value="GoSX Dashboard" />
				</div>
				<div class="form-group">
					<label>Theme</label>
					<select name="theme">
						<option value="light">Light</option>
						<option value="dark">Dark</option>
					</select>
				</div>
				<div class="form-group">
					<label>Items per page</label>
					<input type="number" name="pageSize" value="25" min="10" max="100" />
				</div>
				<button type="submit" class="btn btn-primary">Save Settings</button>
			</form>`),
		),
	)
}
