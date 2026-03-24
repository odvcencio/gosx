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
	islands.SetBundle("dashboard", "/gosx/assets/dashboard.wasm")

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
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; color: #1a1a2e; background: #f8f9fa; }
.layout { display: flex; min-height: 100vh; }
.sidebar { width: 240px; background: #1a1a2e; color: #e0e0e0; padding: 1.5rem 0; }
.sidebar h2 { padding: 0 1.5rem; margin-bottom: 1.5rem; color: #fff; font-size: 1.1rem; }
.sidebar nav a { display: block; padding: 0.6rem 1.5rem; color: #b0b0c0; text-decoration: none; font-size: 0.9rem; }
.sidebar nav a:hover { background: rgba(255,255,255,0.08); color: #fff; }
.main { flex: 1; padding: 2rem; }
.main h1 { margin-bottom: 1.5rem; font-size: 1.5rem; }
.card { background: #fff; border-radius: 8px; padding: 1.5rem; margin-bottom: 1rem; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }
.card h3 { margin-bottom: 0.75rem; font-size: 1rem; color: #555; }
.stat { font-size: 2rem; font-weight: 700; color: #1a1a2e; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem; margin-bottom: 1.5rem; }
table { width: 100%%; border-collapse: collapse; }
th, td { padding: 0.75rem 1rem; text-align: left; border-bottom: 1px solid #eee; }
th { font-weight: 600; color: #555; font-size: 0.85rem; text-transform: uppercase; }
tr:hover { background: #f8f9fa; }
.btn { display: inline-block; padding: 0.5rem 1rem; border-radius: 6px; border: none; cursor: pointer; font-size: 0.9rem; text-decoration: none; }
.btn-primary { background: #4361ee; color: #fff; }
.btn-primary:hover { background: #3651d4; }
.btn-danger { background: #e63946; color: #fff; }
.btn-sm { padding: 0.3rem 0.6rem; font-size: 0.8rem; }
input, select { padding: 0.5rem; border: 1px solid #ddd; border-radius: 6px; font-size: 0.9rem; }
input:focus { outline: none; border-color: #4361ee; }
.form-group { margin-bottom: 1rem; }
.form-group label { display: block; margin-bottom: 0.3rem; font-weight: 600; font-size: 0.85rem; color: #555; }
.form-group input { width: 100%%; }
.search-bar { margin-bottom: 1rem; display: flex; gap: 0.5rem; }
.search-bar input { flex: 1; }
.counter-display { display: flex; align-items: center; gap: 1rem; }
.counter-display .count { font-size: 3rem; font-weight: 700; min-width: 80px; text-align: center; }
.counter-display a { font-size: 1.5rem; text-decoration: none; padding: 0.5rem 1rem; background: #4361ee; color: #fff; border-radius: 6px; }
.counter-display a:hover { background: #3651d4; }
.island-marker { border: 2px dashed #4361ee; border-radius: 8px; padding: 0.5rem; position: relative; }
.island-marker::before { content: "island"; position: absolute; top: -10px; left: 10px; background: #4361ee; color: #fff; font-size: 0.65rem; padding: 0 6px; border-radius: 3px; }
.badge { display: inline-block; padding: 0.15rem 0.5rem; border-radius: 10px; font-size: 0.75rem; font-weight: 600; }
.badge-active { background: #d4edda; color: #155724; }
.badge-inactive { background: #f8d7da; color: #721c24; }
.footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #eee; color: #999; font-size: 0.8rem; }
</style>
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
			gosx.RawHTML(fmt.Sprintf(`<form method="get" action="/users" style="display:flex;gap:0.5rem;flex:1">
				<input type="text" name="q" placeholder="Search users..." value="%s" style="flex:1" />
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
				gosx.El("p", gosx.Attrs(gosx.Attr("style", "padding: 2rem; text-align: center; color: #999")),
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
				<a href="/users" class="btn" style="margin-left: 0.5rem">Cancel</a>
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
