# synced-video

A follow-mode synced video: the managed `<video>` engine corrects drift against
a shared clock so several viewers stay on the same frame. Without a sync server
the page shows the baseline `<video>` and plays locally.

## Run it

```sh
go run ./examples/synced-video
SYNC_URL=ws://localhost:9090/sync STREAM_URL=https://example.com/stream.m3u8 \
    go run ./examples/synced-video
```

Open <http://localhost:8080>. Live convergence needs a browser (the drift
correction runs in the runtime WASM) and a `pkg/theatre`-compatible sync server
at `SYNC_URL`.

## What to read

- `main.go` serves the page and passes `SYNC_URL` and `STREAM_URL` to the video.
- `player.gsx` shows the `//gosx:engine video` authoring shape. A `.gsx` engine
  component has no strict form yet, so it keeps the `func Name(props T) Node`
  declaration. Use the programmatic `engine.Config` surface for v1 engine work.

## Docs

[Engines](https://gosx.m31labs.dev/docs/engines) and the Managed Video section
of the [README](../../README.md).
