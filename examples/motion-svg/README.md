# motion-svg

A Go-authored SVG diagram that morphs between two states with `motion`: the
server renders semantic SVG and the motion transport animates the marked parts.
No JavaScript is written by the app.

## Run it

```sh
PORT=8082 go run ./examples/motion-svg
```

Open <http://127.0.0.1:8082>.

## What to read

- `main.go` builds both diagram states and marks the morphing parts with
  `data-morph-id`.
- The motion transport contract is in [docs/motion-transport.md](../../docs/motion-transport.md).

## Docs

[Motion](https://gosx.m31labs.dev/docs/motion).
