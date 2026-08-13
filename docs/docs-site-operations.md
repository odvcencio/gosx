# GoSX docs operations

The production site is [gosx.m31labs.dev](https://gosx.m31labs.dev). It runs
the application in `examples/gosx-docs`; it is not a separate static-docs
stack. The public image therefore exercises the same server components,
actions, islands, hubs, Scene3D runtime, metadata, and deployment bundle that
the guides describe.

## Runtime contract

- `GET /healthz` proves the process is serving requests.
- `GET /readyz` proves configured readiness checks pass.
- `GET /api/site` reports the deployed GoSX version, immutable Git revision,
  build timestamp, Go runtime, and canonical public origin.
- `GET /sitemap.xml` is generated from the documentation and demo catalogs.
- `GET /robots.txt` points crawlers at that sitemap.

Production must set `PUBLIC_URL`, `GOSX_DOCS_REVISION`,
`GOSX_DOCS_BUILT_AT`, and the out-of-band `SESSION_SECRET`. The checked-in
Kubernetes manifest intentionally contains placeholders for the image digest
and build identity; applying it directly is expected to fail image resolution.

## Deploy

From a clean commit on a Linux/amd64 host and Linux/amd64 Docker daemon with
Docker, `kubectl`, `jq`, TinyGo 0.41.1, registry credentials, and the target
Kubernetes context configured:

```sh
TINYGO=/opt/tinygo-0.41.1/bin/tinygo \
GOSX_TINYGO_GOROOT=/opt/go1.25.5 \
  scripts/deploy-gosx-docs.sh
```

`GOSX_TINYGO_GOROOT` must point at the Go 1.25.5 compatibility toolchain used
by TinyGo. The target Deployment and `gosx-docs` Secret must already exist; the
script reads the Secret's `session-secret` without printing it so prerendering
uses the same session configuration as the runtime. The deploy script refuses
a dirty tree, clears and regenerates the entire production `dist/` bundle with
an explicit public origin and build identity, checks the built image platform,
pushes a Git-tagged OCI image, resolves its registry digest, and renders that
immutable digest into the manifest. It then performs a server-side dry run and
diff, applies a zero-unavailable rolling update, and verifies all three
identities:

- the Deployment template names the exact registry digest, revision, and build
  timestamp;
- every ready pod names that exact template image and reports the exact pulled
  digest;
- the public metadata endpoint reports the exact framework version, revision,
  build timestamp, and public origin.

Any failure after the Kubernetes apply arms an automatic Deployment rollback.
The rollback atomically restores the complete captured pod template only while
the failed release is still current; it refuses to overwrite a concurrent
deployment. The script then waits for the restored template and probes
`/healthz` before it exits with the original deployment failure. It will not
apply or push from an uncommitted worktree.

The container root is read-only. An init container copies the image's staged
HTML into a bounded writable `emptyDir` mounted at `/opt/gosx-docs/static`, so
the default ISR store can refresh an artifact without making the rest of the
image writable.

To validate an existing deployment without changing it:

```sh
scripts/smoke-gosx-docs.sh https://gosx.m31labs.dev
```

The smoke command validates health/readiness, metadata and action paths,
search, sitemap coverage, the special `/docs/` canonical redirect, ordinary
prerendered pages served through ISR, and real compiled CSS, JavaScript, WASM,
source-image, and optimized-image responses. It does not trigger a public
state-changing ISR revalidation.

To assert an exact deployment identity outside the deploy script, provide all
of the release fields:

```sh
GOSX_DOCS_EXPECT_FRAMEWORK_VERSION="v0.39.0" \
GOSX_DOCS_EXPECT_REVISION="$(git rev-parse HEAD)" \
GOSX_DOCS_EXPECT_BUILT_AT="2026-08-13T03:30:00Z" \
GOSX_DOCS_EXPECT_PUBLIC_URL="https://gosx.m31labs.dev" \
  scripts/smoke-gosx-docs.sh https://gosx.m31labs.dev
```

## Roll back

The Deployment retains five revisions. Inspect and undo with standard
Kubernetes rollout commands, then run the same public smoke gate:

```sh
kubectl rollout history -n draco-quest deployment/gosx-docs
kubectl rollout undo -n draco-quest deployment/gosx-docs
kubectl rollout status -n draco-quest deployment/gosx-docs --timeout=5m
scripts/smoke-gosx-docs.sh https://gosx.m31labs.dev
```
