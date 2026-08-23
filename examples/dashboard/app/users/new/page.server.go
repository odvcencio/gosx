package new

import (
	"log"
	"strings"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Absolute: "New User"}}, nil
		},
		Actions: route.FileActions{
			"createUser": func(ctx *action.Context) error {
				name := strings.TrimSpace(ctx.Form.Value("name"))
				email := strings.TrimSpace(ctx.Form.Value("email"))
				fieldErrors := map[string]string{}
				if name == "" {
					fieldErrors["name"] = "Name is required."
				}
				if email == "" {
					fieldErrors["email"] = "Email is required."
				}
				if len(fieldErrors) > 0 {
					return action.ValidationWithValues("Please correct the highlighted fields.", fieldErrors, map[string]string{
						"name":  ctx.Form.Value("name"),
						"email": ctx.Form.Value("email"),
					})
				}
				ctx.Redirect("/users")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
