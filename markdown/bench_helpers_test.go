package markdown

const benchShortMarkdown = `# Hello

This is a **short** markdown document with a [link](https://example.com)
and some inline ` + "`code`" + `. It covers the most common nodes a typical
docs page uses.

- First item
- Second item
- Third item
`

const benchLongMarkdown = `# Getting Started

GoSX is a Go-native web framework that renders every component on the
server by default. No client runtime is required unless you explicitly
opt into interactivity.

## Installation

Install the CLI with:

` + "```bash\ngo install github.com/odvcencio/gosx/cmd/gosx@latest\n```" + `

Then create a new project:

` + "```bash\ngosx new my-app\ncd my-app\ngosx dev\n```" + `

## Core concepts

GoSX provides **five** runtime primitives:

1. **Server** — request/response, routes, SSR
2. **Action** — mutations and form submissions
3. **Island** — constrained DOM interactivity
4. **Engine** — heavy browser compute/render (WebGL, WASM)
5. **Hub** — long-lived realtime server state

Each primitive is opt-in. A page that just serves HTML pays none
of the runtime cost of islands or engines.

### Example

Here's a minimal component:

` + "```go\nfunc Hello(props Props) Node {\n  return <div>Hello, {props.Name}!</div>\n}\n```" + `

The component compiles to pure Go and renders on the server. No
virtual DOM on the client, no hydration cost for static content.

## Features

GoSX supports a wide range of common patterns:

- File-based routing with layouts
- Automatic code splitting for islands
- Built-in caching with automatic ETag
- Metadata conventions for SEO
- WASM engine bundles for Scene3D, editor, markdown

See the [documentation](https://gosx.dev) for the full feature matrix.

## Why not React?

React re-renders the entire component tree on every state change,
then diffs against a virtual DOM. For most pages that's wasted work —
the server can render HTML once and the browser can apply minimal
patches in response to user events.

GoSX flips the model: *server renders everything* and *the client
only runs code where you explicitly declare an island*. This is
faster for the vast majority of pages.

> The fastest code is the code that never runs on the client.

## What's next?

Read the [tutorial](https://gosx.dev/tutorial) or jump into the
[API reference](https://gosx.dev/api) to start building.
`
