---
title: "Getting Started"
description: "Set up a GoSX project from scratch in under a minute."
mode: "light"
tags: ["quickstart", "init", "setup"]
anchors: {"Overview": "overview", "Install": "install", "Create a Project": "create-a-project", "Project Structure": "project-structure", "Component Syntax": "authoring-styles", "Dev Server": "dev-server", "Next Steps": "next-steps"}
toc_labels: {"overview": "Overview", "install": "Install", "create-a-project": "Create a Project", "project-structure": "Project Structure", "authoring-styles": "Authoring Styles", "dev-server": "Dev Server", "next-steps": "Next Steps"}
---
## Overview

GoSX is a Go-native web platform for server rendering, routing, forms, auth, interactive islands, realtime hubs, and managed graphics. GSX adds HTML-shaped markup to Go packages; the browser runtime is generated and managed by the framework, so an application does not need a JavaScript app toolchain. `gosx build --prod .` stages the server, file-route inputs, public content, hashed browser assets, and deployment metadata together in `dist/`.

## Install

Install the GoSX CLI with a single `go install` command.

```bash
go install m31labs.dev/gosx/cmd/gosx@latest
```

Verify the installation by running `gosx version`.

## Create a Project

The `gosx init` command scaffolds a new project with a runnable app, metadata, 404 and 500 pages, public assets, and the navigation runtime already wired up.

```bash
gosx init my-app
cd my-app
go run .
```

Open `http://localhost:8080` to see the running application.

## Project Structure

A freshly scaffolded project looks like this. Pages live in `app/`, and each page is a pair of files: a `.gsx` template and an optional `page.server.go` for server-side data loading and actions.

```text
my-app/
├── app/
│   ├── layout.gsx          # Root layout shared by all pages
│   ├── page.gsx            # Home page template
│   ├── page.server.go      # Home page server module
│   ├── error.gsx           # 500 error page
│   └── not-found.gsx       # 404 page
├── public/                 # Static assets served at /
├── main.go                 # App entry point
└── go.mod
```

The `page.gsx` file is a Go-flavoured HTML template. The `page.server.go` sibling registers a server module that supplies data to the template through the `data` binding. Loader data is not available to a strict component yet. A route that reads `data` keeps the older `func Page() Node` form below; see "Component Syntax" further down.

```go
// app/page.server.go
package app

import (
	"log"

	"m31labs.dev/gosx/route"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"greeting": "Hello from the server",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
```

```gsx
// app/page.gsx
package app

func Page() Node {
	return <div>
		<h1>{data.greeting}</h1>
	</div>
}
```

## Component Syntax

Declare every component with the strict, typed form: `component Name(props: Type)`. The project-aware CLI checks `props` as an ordinary Go type.

```gsx
package app

type BadgeProps struct {
	Label string
	Count int
}

component Badge(props: BadgeProps) {
	return <span className="badge">
		{props.Label}: {props.Count}
	</span>
}

component Page() {
	return <main><Badge label="Inbox" count={0} /></main>
}
```

A strict server component allows one top-level GSX return and a narrow, renderer-safe expression set. A call uses an exact or unambiguous lower-camel prop name. It passes every field the callee renders explicitly, even a zero value.

The older `func Name(...) Node` form is deprecated for ordinary components; GoSX removes its untyped variant before v1.0. `gosx check` already warns on it. It remains necessary today only for loader-bound routes (as above), islands, and engines. See [Components](/docs/components) for the exact boundary and the migration note.

## Dev Server

Use `gosx dev` to start the development server with hot reload. The server watches your `.gsx` and `.go` files and recompiles the app on change.

```bash
gosx dev
```

The command watches project source, rebuilds when needed, and refreshes connected browser tabs after a successful change. Compiler diagnostics remain in the terminal when a change is invalid.

## Next Steps

Now that the dev server is running, explore what GoSX can do.

- [Components](/docs/components) — Strict component syntax, props, renderer boundaries, and the legacy migration note.
- [Routing](/docs/routing) — File-based routing, dynamic params, and nested layouts.
- [Forms](/docs/forms) — Server-side form handling with validation and CSRF protection.
