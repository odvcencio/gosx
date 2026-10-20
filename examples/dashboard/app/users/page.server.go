package users

import (
	"log"
	"strings"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// user is a display row for the users table. The loader below filters and
// types this list; the page template only renders it.
type user struct {
	Name   string
	Email  string
	Role   string
	Active bool
}

var allUsers = []user{
	{"Alice Johnson", "alice@example.com", "Admin", true},
	{"Bob Smith", "bob@example.com", "Editor", true},
	{"Carol Williams", "carol@example.com", "Viewer", false},
	{"Dave Brown", "dave@example.com", "Editor", true},
	{"Eve Davis", "eve@example.com", "Admin", true},
	{"Frank Miller", "frank@example.com", "Viewer", false},
}

type usersPageData struct {
	Query string
	Users []user
	Empty bool
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			query := ctx.Query("q")
			rows := allUsers
			if query != "" {
				q := strings.ToLower(query)
				filtered := make([]user, 0, len(allUsers))
				for _, u := range allUsers {
					if strings.Contains(strings.ToLower(u.Name), q) || strings.Contains(strings.ToLower(u.Email), q) {
						filtered = append(filtered, u)
					}
				}
				rows = filtered
			}
			return usersPageData{Query: query, Users: rows, Empty: len(rows) == 0}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Absolute: "Users"}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
