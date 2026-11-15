# Rooms example

A server-authoritative room server and a static browser client for the
`room/roomtest` bodies game: each person steers one body, bots fill the other
seats, and the client predicts its own body while it interpolates the rest.
See [docs/rooms.md](../../docs/rooms.md) for the API.

## Run it on one machine, cross-origin

The client is served from one origin (port 8000) and connects to the server on
another (port 8090), the same shape as a game hosted on a portal or CDN.

```sh
# 1. Build the client.
GOOS=js GOARCH=wasm go build -o examples/rooms/static/main.wasm ./examples/rooms/client
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/rooms/static/

# 2. Start the room server and allow the client's origin.
go run ./examples/rooms/server -addr :8090 -origins http://localhost:8000

# 3. Serve the client from any static host on another origin.
cd examples/rooms/static && python3 -m http.server 8000
```

Open <http://localhost:8000/?server=ws://localhost:8090/play/ws>. Alone, you
play against a bot at once. Open a
second tab to join the same quick-match room; the second person takes over the
bot's seat. Stop the server and the clients keep playing offline.

Add `?server=wss://rooms.example.com/play/ws` to point the page at a deployed
server, `&mode=party` for the six-seat mode, `&private=1` to create a private
room, and `&code=ABCDE` to join one.

To serve the client from the room server itself (same origin), pass
`-static examples/rooms/static` and open <http://localhost:8090/>; without
`?server=` the page connects to its own host.

Every server flag also reads an environment variable (`ROOMS_ADDR`,
`ROOMS_ORIGINS`, `ROOMS_STATIC`, `ROOMS_REQUIRE_ORIGIN`,
`ROOMS_TRUST_FORWARDED_FOR`, `ROOMS_MAX_ROOMS`, `ROOMS_CONNECTS_PER_MINUTE`,
`ROOMS_CREATES_PER_MINUTE`). `GET /healthz` reports that the process is up and
`GET /readyz` that it accepts players. [deploy/](deploy/) runs it on Kubernetes.

## League: persistent groups, invites, scheduled rounds

`league/` is a second, small service built on `room/store`, `room/group`,
`room/identity` and `room/advance`: an organiser creates a group, players join
with a short invite code and get a device token, and a round advances when every
seat has reported or the deadline passes. Run it with
`go run ./examples/rooms/league/cmd` (see the file header for the environment
variables) and read "Persistent groups and identities" in
[docs/rooms.md](../../docs/rooms.md).
