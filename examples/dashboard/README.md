# dashboard

A multi-page admin app: shared layout, server-rendered tables, forms with
server actions, and a set of small islands (counter, tabs, toggle, todo, form,
derived values, editor, list) on a "kitchen sink" page.

## Run it

The islands need the browser runtime in `examples/dashboard/build/`. Build it
once, then start the server:

```sh
cd examples/dashboard
go run ../../cmd/gosx build-runtime build
go run .
```

Open <http://localhost:3000>. `go test ./examples/dashboard` renders the home
page through the file router and compares it with a golden.

## What to read

- `app/` holds the file-routed pages; `app/layout.gsx` and `chrome.gsx` are the
  shared chrome as strict components.
- `main.go` serves the island programs from `program.*Program()` reference
  builders rather than compiling `.gsx` islands; see `../hotswap` and
  `../counter` for islands compiled from `.gsx`.

## Docs

[Islands](https://gosx.m31labs.dev/docs/islands) and
[Forms](https://gosx.m31labs.dev/docs/forms).
