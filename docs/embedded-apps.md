# Embed GoSX apps in iframes and Discord Activities

GoSX can serve an app under a public URL prefix, permit selected iframe parents,
and persist sessions in a third-party iframe. Each option is explicit. A normal
app keeps same-origin framing, root-relative routing, and Secure, HttpOnly,
SameSite=Lax session cookies.

## Configure a Discord Activity

Configure a URL mapping in the Discord Developer Portal for your app server.
Set the base path to the public prefix used by that mapping, such as
`/.proxy/game`. Use the actual prefix your deployment exposes; some mappings
serve the app at `/` and need no base path.

[Discord's networking guide](https://docs.discord.com/developers/activities/development-guides/networking)
describes its proxy, WebSocket support, and cookie requirements. GoSX does not
need SDK URL patching for its own links, bundles, or hub connections.

This setup runs before registering pages and calling `Build`:

```go
app := server.New()

if err := app.EnableSecurityPolicy(server.SecurityPolicy{
    FrameAncestors: []string{
        "'self'",
        "https://discord.com",
        "https://*.discordsays.com",
    },
}); err != nil {
    log.Fatal(err)
}

const basePath = "/.proxy/game"
if err := app.SetBasePath(basePath, server.BasePathOptions{
    ProxyStripsPrefix: true,
}); err != nil {
    log.Fatal(err)
}

// ACTIVITY_ORIGIN is the exact browser origin, for example
// https://12345678.discordsays.com. SESSION_SECRET stays on the server.
activityOrigin := os.Getenv("ACTIVITY_ORIGIN")
activityURL, err := url.Parse(activityOrigin)
if err != nil || activityURL.Scheme != "https" || activityURL.Host == "" {
    log.Fatal("ACTIVITY_ORIGIN must be an HTTPS origin")
}
sessions, err := session.New(os.Getenv("SESSION_SECRET"), session.Options{
    Path:           basePath,
    Domain:         activityURL.Hostname(),
    SameSite:       http.SameSiteNoneMode,
    Partitioned:    true,
    TrustedOrigins: []string{activityOrigin},
})
if err != nil {
    log.Fatal(err)
}
app.Use(sessions.Middleware)

pages := route.NewRouter()
if err := pages.AddDir("app"); err != nil {
    log.Fatal(err)
}
// Protect mutations on the router. A mounted router inherits the app prefix.
app.Mount("/", sessions.Protect(pages.Build()))
log.Fatal(http.ListenAndServe(":8080", app.Build()))
```

The example assumes the mapping strips the prefix before forwarding. When your
proxy preserves it, omit `BasePathOptions` or leave `ProxyStripsPrefix` false.
TLS must terminate at the browser-facing proxy. GoSX still emits Secure cookies
when the connection from that proxy to the server uses HTTP.

Initialize session state when the visitor joins or authenticates, and render
`csrf_token` in forms with the usual GoSX session helpers. The CSRF token lives
inside the signed session cookie; GoSX does not create a separate CSRF cookie.
Existing `session.Token`, file-route `csrfToken`, and `ctx.FormState` helpers
continue to work. No app-specific cookie attributes or URL rewriting are needed.

## Options and defaults

| Option | Default | Behavior when enabled |
| --- | --- | --- |
| `server.SecurityPolicy.FrameAncestors` | Empty; default CSP permits `'self'` | Adds the chosen `frame-ancestors` directive to normal and shared-cache CSPs. |
| `app.SetBasePath(prefix, options...)` | Empty prefix | Maps public request paths and emitted local URLs to the prefix. |
| `router.SetBasePath(prefix, options...)` | Empty prefix | Applies the same behavior to a standalone router. Mounted routers inherit their app's setting. |
| `server.BasePathOptions.ProxyStripsPrefix` | `false` | Keeps incoming paths unchanged when a trusted proxy already removed the prefix. |
| `session.Options.SameSite` | `http.SameSiteLaxMode` | Set `http.SameSiteNoneMode` to permit cookies in cross-site iframe requests. |
| `session.Options.Secure` | `true` | `SameSite=None` cannot be combined with an insecure cookie. |
| `session.Options.Partitioned` | `false` | Adds CHIPS partitioning to writes and deletion; requires `SameSite=None` and Secure. |
| `session.Options.TrustedOrigins` | Empty | Permits exact HTTP(S) mutation origins through the existing CSRF origin guard. |

`EnableSecurityPolicy` returns an error for an invalid framing source or a
conflicting `FrameOptions` setting. It accepts `'self'`, `'none'` alone, and
HTTP(S) origins with an optional wildcard subdomain. It rejects unrestricted
`*`, scheme-only sources, paths, and injected directives. Prefer the exact
Activity domain instead of `https://*.discordsays.com` when the embedding chain
allows it. A report-only CSP still receives an enforced framing policy.

An explicit raw `ContentSecurityPolicy` retains its existing semantics when no
`FrameAncestors` option is supplied. Include a framing directive in a custom CSP
if you want to restrict embedding. The new allowlist replaces framing directives
in both supplied policies while preserving their script and other restrictions.

## Route and runtime URLs

Keep page routes and local URLs root-relative: `/tables`, `/images/tile.png`,
`/ws`, and `/__actions/join`. GoSX prefixes rendered URL attributes, including
links, forms, images, preload hints, scripts, and framework navigation targets.
It prefixes island programs, compute programs, engine bundles, shared WASM,
controller resources, texture variants, and hub WebSocket paths in manifests.
Lazy bootstrap feature loads also use the prefix.

Native HTTP redirects and managed action JSON redirects receive the same prefix.
Query strings and fragments survive, and an already prefixed URL is not prefixed
again. External URLs, protocol-relative URLs, relative paths, and arbitrary
application props or script text retain their values. CSS files should use
relative asset URLs. For an app-owned API response or custom browser code, use
`server.URL(request, "/api/state")` to obtain its public URL.

`SetBasePath` rejects noncanonical prefixes, escaped separators, queries, and
fragments. With the default proxy mode, requests outside the prefix return 404.
The option never reads a client-supplied forwarded-prefix header. Register it on
the app before `Build`; standalone routers use the same API. Framework-rendered
HTML, including deferred fragments, participates in URL rewriting. A mounted
handler that writes raw HTML bytes directly owns that response.

## Origin checks and cookies

Framing permission and mutation trust are separate. A POST from an Activity
normally carries the Activity iframe's own origin, not the parent
`https://discord.com` origin. Trust the exact app-specific origin. Do not add a
wildcard of every Activity to `TrustedOrigins` or trust a parent merely because
it can frame the app. Session-bearing mutations still need a valid CSRF token.
Hub WebSocket connections retain their own origin and authorization checks.

If you need forwarded host/scheme resolution, use the existing
`session.Options.ForwardedTrust` with specific trusted proxy addresses and
`AllowedHosts`. Those proxies must replace client-supplied forwarded headers.
Do not enable broad forwarded-header trust to accommodate embedding.

Partitioned cookies are scoped by the browser to the top-level site. A session
inside Discord may differ from one in a standalone tab. Browsers that do not
support CHIPS may ignore `Partitioned`; browsers that block third-party cookies
can still prevent persistence. See
[the browser cookie attribute reference](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie).
For a generic iframe, keep cookies host-only unless your proxy requires a
specific public domain. `__Host-` cookies still require Path `/` and no Domain;
use an ordinary cookie name when scoping its path to a prefix.

## Build without runtime credentials

Keep authenticated pages request-time rendered. Set `{"prerender":false}` in
`app/route.config.json` when the whole Activity requires runtime state. A
production build with no static routes writes an empty export, stages public
assets, and builds the server and client bundles without starting the server.
It also clears stale exported HTML from a previous build. Pages explicitly
selected for prerendering still use the existing startup and readiness checks.

Install the CLI at the same module version as your app, including when you pin a
pseudo-version. The version guard compares the installed GoSX module version
from the binary's build information; local development binaries retain the
release-version fallback. A genuinely different module version is still rejected.

## Verify embedding

The unit suites cover framing validation and cache policies, prefixed routes,
URL attributes and manifests, streamed fragments, action redirects, cookie
creation/deletion, and trusted versus rejected Discord origins.
`TestEmbeddedAppUnderPrefix` serves a GoSX page inside an iframe on a different
HTTPS site and checks assets, lazy bundles, a hub WebSocket, partitioned session
persistence, CSRF rejection, and a native form redirect:

```sh
GOWORK=off go test -tags e2e ./e2e -run '^TestEmbeddedAppUnderPrefix$' -count=1
```

Set `GOSX_CHROME_BIN` when Chrome is installed outside the usual executable path.
The test uses local TLS fixtures and does not require a Discord account.
