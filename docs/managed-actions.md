# Managed POST actions

Managed actions are the server-owned mutation boundary for GoSX forms and
browser requests. They have one registration API, one reserved URL namespace,
one bounded parser, and one session/CSRF contract. A file-routed page renders
the form; the owning `route.Router` registers the action.

## Two phases: register, then compose

Registration is a pre-build operation. Composition is a separate operation and
must propagate every error before `BuildChecked`:

```go
package main

import (
	"log"
	"net/http"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func build() (http.Handler, error) {
	// Phase 1: register all managed endpoints on the route router.
	router := route.NewRouter()
	if err := router.RegisterManagedPOST("subscribe", action.Config{}, func(ctx *action.Context) (action.Result, error) {
		if ctx.Form.Value("email") == "" {
			return action.Result{}, action.Validation("Email is required.", map[string]string{
				"email": "Email is required.",
			})
		}
		if err := session.AddFlash(ctx.Request, "notice", "Subscribed."); err != nil {
			return action.Result{}, err
		}
		return action.Result{OK: true, Redirect: "/"}, nil
	}); err != nil {
		return nil, err
	}
	compiled, err := router.BuildChecked()
	if err != nil {
		return nil, err
	}

	// Phase 2: compose the already-checked route tree with the app.
	sessions, err := session.New("replace-this-secret-in-production", session.Options{AllowInsecure: true})
	if err != nil {
		return nil, err
	}
	app := server.New()
	app.Use(func(next http.Handler) http.Handler {
		return sessions.Middleware(sessions.Protect(next))
	})
	app.Mount("/", compiled)
	return app.Build(), nil
}

func main() {
	handler, err := build()
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.ListenAndServe(":8080", handler))
}
```

The direct form and browser examples below assume that the page has obtained a
session token from the same request-scoped session manager. Do not invent a
token in a template or test.

## Action names and paths

Names are one URL path segment and must match this byte-bounded grammar:

```text
[A-Za-z][A-Za-z0-9_-]{0,63}
```

The first character is ASCII alphabetic, the remaining characters are ASCII
alphanumeric, `_`, or `-`, and the maximum is 64 bytes. Unicode, whitespace,
slashes, query delimiters, fragments, and encoded separators are rejected.
`action.ValidateActionName` is the public validator and
`action.ActionPath(name)` is the canonical constructor used by registration
and form renderers. An invalid name returns an error from registration and an
empty path from the renderer, so helpers cannot emit an impossible endpoint.

Every managed action is served at `/gosx/action/{name}` and accepts POST only.
The route router owns the reserved namespace; raw handlers cannot be installed
there.

## Request budgets and formats

`action.Config` resolves zero values to safe defaults. Request bodies, URL form
fields, JSON, multipart metadata, field names, filenames, file counts, and
file bytes are bounded independently. A configured zero `MaxFiles` means file
uploads are disabled; enabling files requires a positive `MaxFileBytes`.
Overflow is checked before arithmetic is committed, including exact-limit,
first-over-limit, and signed overflow cases.

The parser supports:

- `application/json`, with the CSRF token in the configured header;
- `application/x-www-form-urlencoded`, with a header token or the configured
  body field; and
- `multipart/form-data`, with the same header/body token choices and bounded
  uploads.

The managed parser is the sole body parser. A capability-preserving session
middleware chain delegates CSRF ownership to the selected managed handler,
which prevents a second `FormValue`/multipart parse. Opaque or short-circuiting
middleware is not granted that capability and remains responsible for ordinary
CSRF validation.

Multipart metadata is bounded from the raw header stream before the standard
library can materialize a MIME header map. The configured metadata budget is
separate from the total request-body budget; boundary lines, preamble/epilogue,
quoted parameters, continuations, and duplicate headers still follow the
standard multipart grammar. A metadata overflow is a typed 413, even when the
body allowance is larger than the standard library's internal MIME-header
guard.

## CSRF in native and enhanced forms

Native HTML must carry the token in a hidden field using the action's body
field name (the default is `csrf_token`):

```go
token := session.Token(ctx.Request)
gosx.El("form", gosx.Attrs(
	gosx.Attr("method", "post"),
	gosx.Attr("action", action.ActionPath("subscribe")),
),
	gosx.El("input", gosx.Attrs(
	gosx.Attr("type", "hidden"),
	gosx.Attr("name", "csrf_token"),
	gosx.Attr("value", token),
	)),
)
```

The HTML renderer escapes the attribute value. The enhanced runtime may send
the same request token in the configured header (`X-CSRF-Token` by default).
Applications that expose a runtime bridge should emit an escaped
`<meta name="csrf-token" content="...">` from the actual request so browser
code can read the same token. Custom header/body names are configured together
with `action.CSRFConfig`; partial configuration is rejected.

For enhanced managed navigation, CSRF ownership has a stable precedence:
submitter-specific override, associated form, page meta token, then the
default empty-token behavior. The form that owns result projection remains the
projection target even when a submitter owns an action target, event, signal, or
CSRF override. External `form="..."` submitters use their associated form's
fields and lifecycle.

## Build immutability and capability matching

`route.Router.BuildChecked` freezes its managed registration snapshot. A
directly mounted `*action.Router` also implements the explicit
`BuildManagedActionHandler` boundary: `server.App.Build` freezes it and keeps
the exact immutable snapshot as both dispatcher and capability matcher. A
registration attempted after the build fails and cannot alter the built app.

Mount selection is evaluated by the immutable compiled mux, including method,
host, nested, and overlapping patterns. Session CSRF delegation follows only
wrappers that explicitly preserve managed capability; an arbitrary `Unwrap`
method or an opaque middleware short-circuit is never an authorization claim.
Locale middleware applies the same prefix rewrite for capability checks that it
uses for dispatch, so `/fr/gosx/action/subscribe` is one exact endpoint rather
than two divergent interpretations.

## Results and errors

Successful actions return `action.Result{OK: true}`. JSON/browser requests get
the structured `ManagedOutcome` envelope. Native requests may return a safe
same-origin redirect; the framework commits session mutations before writing
the response. Result values are explicitly allowlisted and serialized under a
separate byte budget.

Return `action.Validation` for user-correctable 422 errors and
`action.BadRequest` for a controlled 400. Framework request errors have a
public `RequestErrorKind`, status, and managed code while their causes remain
diagnostic only. A single missing upload or multiple-upload lookup is mapped to
the typed `RequestErrorMissingUpload` or `RequestErrorMultipleUpload` category
and a generic 422 client message. If both sentinel causes are present, the
combination is treated as an internal serialization failure (500), because the
framework cannot choose one validation meaning.

Parse, CSRF, action, and session-commit errors retain response precedence.
Cleanup is still attempted in every path. Cleanup errors are aggregated with
`errors.Join` and logged once by the terminal owner; after a successful
response, a temp-file removal failure is diagnostic only and never rewrites
that response.

## Upload reader lifetime and cleanup

`ctx.Form.File` returns a bounded `action.Upload`. `Open` creates a fresh,
revocable reader while the action is running. Both memory-backed and temp-file
uploads are invalidated after the action returns; memory closures and backing
slices are released, temp paths are never exposed through the public API, and
`Open` fails after invalidation. Reader `Close` is idempotent. Open/invalidate
and terminal cleanup are synchronized, so concurrent callers cannot register a
reader after revocation or remove a temp file while it is being opened.

The terminal cleanup owner attempts invalidation, every reader close, and temp
removal even when one operation fails. Tests should assert `errors.Is` for each
injected cause, the exact response precedence, one diagnostic record, and no
remaining temp path.

## Testing checklist

At minimum, test registration grammar and path construction; exact/over-limit
budgets; header and body CSRF; native and browser responses; method/host/nested
mount selection; post-build mutation; locale prefixes; opaque middleware;
memory/temp/multiple/concurrent upload lifetimes; and create/write/close/open/
remove failure injection. Run `go test ./...`, focused race tests, `go vet`,
the generated runtime parity checks, and the repository's documented browser
harness before freezing a change.
