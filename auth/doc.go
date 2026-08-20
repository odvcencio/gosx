// Package auth identifies the current user and guards handlers behind that
// identity.
//
// It owns no user store and no password. A Manager wraps a session.Manager and
// keeps one thing in the session — enough to recover a User on the next
// request. Where that User comes from is the application's choice, made by
// installing a Provider or by using one of the three sign-in flows below.
//
// # The handful that matter
//
//	New(sessions, opts)   build a Manager over an existing session.Manager
//	Manager.Middleware    wrap the handler tree; Current needs it
//	Manager.Require       block anonymous visitors
//	Manager.RequireRole   the same, restricted to one role
//	Current(r)            the User for this request, and whether there is one
//	Manager.SignIn / SignOut  move a User in and out of the session
//
// Require answers in whichever form the caller asked for. A request that wants
// JSON gets 401 and {"error":"authentication required"}; anything else is
// redirected to Options.LoginPath with a canonical same-origin next target for
// GET and HEAD requests. Mutation requests do not preserve their request URI,
// because redirecting back to a non-idempotent operation would replay a flow
// the browser cannot safely resume.
//
// SafeReturnPath is the shared return-target primitive used by Require and all
// built-in sign-in flows. It accepts and canonicalizes only a bounded,
// root-relative request URI (path plus optional query), returning ("", false)
// for unsafe input. Callers must apply any application-specific route policy
// to the canonical returned value and choose their own fallback.
//
// # Three ways to sign in, all optional
//
// Each is built from the Manager and each is independent. An application may
// use one, several, or none.
//
//	Manager.MagicLinks  emailed one-time links. Needs a MagicLinkStore and a
//	                    MagicLinkSender; NewMemoryMagicLinkStore covers
//	                    development and single-process use.
//	Manager.OAuth       third-party sign-in. GitHubProvider and GoogleProvider
//	                    are prebuilt; OAuthProvider describes any other.
//	Manager.WebAuthn    passkeys and security keys. Needs a WebAuthn store;
//	                    NewMemoryWebAuthnStore covers development.
//
// Each exposes both halves: a handler pair to mount directly
// (RequestHandler/CallbackHandler, BeginHandler/CallbackHandler,
// RegisterHandler/LoginHandler) and the underlying calls (Issue/Consume,
// Begin/Callback, BeginRegistration/FinishRegistration) for an application that
// wants to own the routes and the responses.
//
// # Session commit boundary
//
// Built-in BeginHandler, CallbackHandler, magic-link handlers, and WebAuthn
// handlers call session.Commit after their final session mutation and before
// returning JSON or redirecting. A cookie-size or serialization failure is
// therefore terminal: the session middleware emits a generic non-3xx 500 and
// strips Location and stale response metadata instead of claiming success.
//
// When an application calls the lower-level Issue/Consume, Begin/Callback, or
// WebAuthn ceremony methods and writes its own response, it must call
// session.Commit(w, r) after all session mutations and before the first final
// status or body byte. Automatic middleware finalization remains a fallback,
// but custom wrappers should make this boundary explicit on every
// persistence-dependent success path.
//
// OAuth ceremony state is one direct session map at the configured session
// key, keyed by random OAuth state. Each record contains only Provider,
// Verifier, canonical Next, and Unix-millisecond ExpiresAt; the key is not
// duplicated in the record. The map is always capped at two live entries.
// Begin prunes entries whose expiry is at or before now and evicts the oldest
// expiry deterministically, breaking ties lexically by state. Callback looks
// up an exact state, consumes a match before provider, expiry, code exchange,
// or user-info work, and leaves unknown states untouched. Expired and
// provider-mismatched matches are consumed. This is a deliberate pre-v1 clean
// break: no envelope, version field, legacy decoder, mixed writer, or migration
// path is supported, and in-flight ceremonies may be retried after an upgrade.
//
// # The memory stores are for development
//
// NewMemoryMagicLinkStore and NewMemoryWebAuthnStore hold their state in the
// process. Restarting drops every issued link and every registered credential,
// and a second replica does not see the first one's. Implement MagicLinkStore
// or the WebAuthn store interface against real storage before running more than
// one instance.
//
// # Ordering
//
// session.Manager.Middleware must run before auth's, which must run before any
// handler that calls Current, Require or RequireRole. Current reports false
// when the middleware did not run, which is indistinguishable from an anonymous
// visitor.
//
// # Watching what happens
//
// Manager.UseObserver installs an Observer that receives an AuthEvent.
// NewJSONObserver writes them as JSON lines. Nothing is recorded unless an
// observer is installed.
//
// SignIn and SignOut both emit, with AuthEvent.Type "sign_in" or "sign_out". A
// failure is not a separate event type — it is the same event with Success
// false and Error naming the cause, so an observer that filters on Type alone
// still sees the failures.
package auth
