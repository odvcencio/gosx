# basic

The smallest file-routed GoSX app: two server-rendered `.gsx` pages, a data
loader beside each page, and a counter that works with plain links and no
JavaScript. It is the same `app/page.gsx` plus `page.server.go` shape that
`gosx init` scaffolds.

## Run it

```sh
go run ./examples/basic
```

Open <http://localhost:8080>. `/counter?count=3` shows the link-driven counter.

## What to read

- `main.go` wires the file router to an HTTP server (`route.Router.AddDir`).
- `app/page.gsx` and `app/counter/page.gsx` are the pages. `app/**/page.server.go`
  holds each page's `Load`.
- A page reads its loader's result as `data`.

## Docs

[Routing](https://gosx.m31labs.dev/docs/routing) and
[Getting started](https://gosx.m31labs.dev/docs/getting-started).
