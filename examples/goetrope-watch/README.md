# goetrope-watch

A watch-room viewer reimagined as a server-driven GoSX app. The server renders
the room, the queue and the subtitle state; the browser only has to carry a
narrow playback transport. It is a prototype and does not touch production
playback.

## Run it

```sh
go run ./examples/goetrope-watch
go test ./examples/goetrope-watch   # renders both routes through the file router
```

Open <http://localhost:8080>, then <http://localhost:8080/watch/alpha>.

## What to read

- `app/layout.gsx` is the shared shell.
- `app/page.server.go` and `app/page.gsx` render the landing page.
- `app/watch/page.server.go` loads the room snapshot for
  `app/watch/[code]/page.gsx`, which renders it.
- The landing page's cards are strict `component`s with typed props structs. A
  strict component accepts a spread only from a struct, so `app/page.server.go`
  builds structs (`highlightCard`, `roomPreview`) whose fields match.
- The room page writes its queue, transport shell and moments inline. `gosx
  check` cannot type-check a strict component inside a `[code]` directory yet:
  it fails with `malformed import path ... invalid char '['`.
- `public/watch.css` carries the visual system.

## Docs

[Components](https://gosx.m31labs.dev/docs/components) and
[Routing](https://gosx.m31labs.dev/docs/routing).
