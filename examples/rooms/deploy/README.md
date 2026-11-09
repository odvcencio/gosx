# Deploying the rooms example

These files run the generic room server from `examples/rooms` (the
`room/roomtest` moving-bodies game with bots) on the k3s cluster at
<https://rooms.draco.quest>. They follow the Dominoes deployment
(`caribbean-dominoes/deploy`): the same image flow, ingress class and
annotations, TLS issuer and DNS.

| Setting | Value |
|---|---|
| Public site | https://rooms.draco.quest (client page at `/`, WebSocket at `/play/ws`, lobby at `/play/rooms`) |
| Health | `GET /healthz` (process up), `GET /readyz` (accepting players) |
| Kubernetes context / namespace | `default` / `gosx-rooms` |
| Deployment / container | `gosx-rooms` / `app` |
| Image repository | `harbor.draco.quest/orchard/gosx-rooms`, tag `git-<commit>`, deployed by digest |
| Ingress | class `traefik`, entrypoint `websecure`, issuer `cloudflare-issuer`, secret `gosx-rooms-tls` |
| DNS | the `*.draco.quest` wildcard already points at the ingress; no record to add |

## What runs

One replica with `Recreate`: rooms live in process memory, so one process
owns them. A restart drops every room. Connected clients keep playing locally
(netsync's offline fallback) and quick-match again on their next start.

The pod runs as UID 65532 with a read-only root, no service-account token and
no outbound network beyond cluster DNS. It requests 50m CPU and 64 MiB and is
limited to 500m and 256 MiB (`GOMEMLIMIT` 200 MiB). It has no secrets and no
volumes. The `orchard` Harbor project is public, so no pull secret is needed.

Configuration is environment variables in `k8s.yaml`:

| Variable | Value | Why |
|---|---|---|
| `ROOMS_ORIGINS` | see below | browser origins that may connect |
| `ROOMS_REQUIRE_ORIGIN` | `true` | browsers always send Origin; refuse clients that omit it |
| `ROOMS_TRUST_FORWARDED_FOR` | `true` | rate limits per client: Traefik appends the real peer address (`externalTrafficPolicy: Local`) |
| `ROOMS_CONNECTS_PER_MINUTE` | `30` | WebSocket connections per client address |
| `ROOMS_CREATES_PER_MINUTE` | `10` | new rooms per client address |
| `ROOMS_MAX_ROOMS` | `64` | live rooms; more joins get close code 4006 and play offline |

Allowed origins, besides `https://rooms.draco.quest` itself (always allowed):

| Origin | Why |
|---|---|
| `https://opengame.io` | the jam portal's pages |
| `https://*.opengame.io` | portal subdomains that host game iframes |
| `https://*.draco.quest` | our own test pages on other draco.quest hosts |
| `http://127.0.0.1:47831`, `http://localhost:47831` | the live browser e2e test page; browsers send these only from a page on the visitor's own machine |

A sandboxed iframe without `allow-same-origin` sends `Origin: null`. Add
`null` only if the portal needs it: every such iframe on every site sends it.

Bots fill every empty seat, so a visitor alone plays at once.

## Release

Build on your workstation, never on the cluster host (`buildbox` is a
production machine; see its disk incident of 2026-09-24). `release.sh` builds
`git-<commit>`, pushes it, and applies the manifests rendered to the digest
Harbor acknowledged:

```sh
bash examples/rooms/deploy/release.sh      # build, push, apply, wait for rollout
bash examples/rooms/deploy/release.sh -n   # build only
```

It needs Docker with Buildx logged in to Harbor, and `kubectl` for context
`default`. Commit first: the script refuses a dirty tree, because the tag
names the commit.

To render without applying:

```sh
bash examples/rooms/deploy/render.sh harbor.draco.quest/orchard/gosx-rooms@sha256:DIGEST
```

## Check it

```sh
curl -fsS https://rooms.draco.quest/healthz
curl -fsS https://rooms.draco.quest/readyz
curl -fsS https://rooms.draco.quest/play/rooms
kubectl --context default -n gosx-rooms get pods,ingress,certificate
kubectl --context default -n gosx-rooms logs deployment/gosx-rooms --tail=50
```

The browser e2e tests run against the live server with
`GOSX_ROOMS_E2E_SERVER=wss://rooms.draco.quest/play/ws`:

```sh
mac-gpu-run "$PWD" env GOSX_ROOMS_E2E_SERVER=wss://rooms.draco.quest/play/ws \
  go test -tags e2e -run 'TestRoomsNetsync' -v ./e2e/
```

## Roll back or remove

```sh
# Back to the previous revision (after a second release exists):
kubectl --context default -n gosx-rooms rollout undo deployment/gosx-rooms
kubectl --context default -n gosx-rooms rollout status deployment/gosx-rooms --timeout=3m

# Or pin an earlier image digest:
bash examples/rooms/deploy/render.sh harbor.draco.quest/orchard/gosx-rooms@sha256:OLD_DIGEST \
  | kubectl --context default apply -f -

# Remove everything (namespace, Deployment, Service, Ingress, certificate):
kubectl --context default delete namespace gosx-rooms
```

Nothing outside the `gosx-rooms` namespace is created, so deleting it removes
the deployment completely. The image stays in Harbor.
