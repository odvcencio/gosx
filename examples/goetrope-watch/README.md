# Goetrope Watch Surface Prototype

This example reimagines the Goetrope viewer as a server-driven GoSX app.

The prototype keeps the page shape on the server:

- the watch room shell is file-routed through `.gsx` pages
- queue state, subtitle state, and the next item are precomputed in `page.server.go`
- playback is represented as a narrow surface, not a full client-managed page
- queue titles are normalized before they ever reach the render tree

The example is intentionally isolated. It does not wire into production Goetrope and it does not touch live playback control code.

## Layout

- `app/layout.gsx` defines the shared shell
- `app/page.server.go` and `app/page.gsx` render the landing surface
- `app/watch/[code]/page.server.go` and `app/watch/[code]/page.gsx` render a room snapshot
- `app/watch/[code]/player.gsx` contains the transport shell placeholder
- `public/watch.css` carries the visual system

## Run

From this directory:

```bash
go run .
```

The app is meant as a prototype and reference point for a later Goetrope migration, not as a production entrypoint.
