// Package session stores per-visitor state in a signed cookie.
//
// The cookie carries the whole session. There is no server-side store to run
// or scale, and the trade is size: a session that outgrows the browser's cookie
// limit fails to write. Manager.Middleware preflights the cookie before the
// first final status or body byte; failure replaces the pending response with a
// fixed 500 rather than forwarding success without durable session state.
//
// # The handful that matter
//
// Of everything this package exports, an application normally touches these:
//
//	New / MustNew   build a Manager from a secret and Options
//	Manager.Middleware  wrap the handler tree; nothing below works without it
//	Manager.Protect     add CSRF checking to unsafe methods
//	Current(r)      the *Store for this request
//	Commit(w, r)    prove and seal persistence before claiming success
//	Token(r)        the CSRF token to embed in a form
//
// On the Store itself: Set, Value, String, Decode, Delete, AddFlash, Flashes,
// Destroy, and Err. The package-level AddFlash, Values, FlashValues and Destroy
// are conveniences that look the Store up from the request first.
//
// # Persistence-dependent success
//
// Automatic completion is a safe fallback, but a handler whose redirect or JSON
// result means "the session mutation succeeded" should make the transaction
// boundary visible:
//
//	store := Current(r)
//	if err := store.Set("signed_in", true); err != nil { /* answer 500 */ }
//	if err := Commit(w, r); err != nil { /* answer 500 */ }
//	http.Redirect(w, r, "/", http.StatusSeeOther)
//
// Commit runs after every mutation and before WriteHeader, Write, Flush, or a
// redirect. It builds and checks the complete serialized cookie synchronously,
// stages exactly one session Set-Cookie for dirty state, and never writes a
// response status. Success seals the Store: repeated commit is harmless, while
// Set, Delete, AddFlash, and Destroy return ErrSessionCommitted afterward.
//
// A failed explicit attempt has not touched the response. Repeating it without a
// mutation returns the cached error and does not report Options.OnError twice. A
// mutation that shrinks or otherwise repairs the session permits another try.
// Store.Err exposes that cached failure and clears after a successful retry.
//
// If a handler ignores the error and starts a response, automatic completion
// still fails closed: it removes redirects, cookies, encoding, and stale entity
// metadata; marks the failure no-store; writes one generic 500; and discards
// later handler bytes. The package does not buffer response bodies, so
// streaming remains streaming.
//
// # Order
//
// Manager.Middleware must wrap any handler that reads a session. Current
// returns nil when it did not, and every Store method tolerates a nil receiver,
// so a missing middleware does not panic — it reads exactly like a visitor with
// no session, and nothing persists.
//
// Protect goes inside Middleware, not outside it, because it reads the token
// from the session Middleware loaded. Getting that order wrong is the one case
// this package refuses to absorb quietly: Protect answers 500 with "session
// middleware required before csrf protection" rather than letting an unchecked
// request through.
//
// Protect reads the token from the X-CSRF-Token header. For a request that does
// not want JSON it also falls back to the csrf_token form field, which covers
// both urlencoded and multipart submissions.
//
// # Streaming and optional response capabilities
//
// Write, WriteHeader, Flush, and normal handler fallthrough all pass through the
// same cookie preflight. Informational 1xx responses other than 101 do not seal
// the Store; Push does not commit the parent response. A dirty session must call
// Commit before Hijack, because HTTP can no longer add a cookie afterward. The
// middleware exposes Hijacker and Pusher only when the underlying writer
// supports the corresponding operation. It normalizes either supported flush
// form (Flusher or FlushError) into both forms so handlers and
// http.ResponseController cross the same session boundary. Its Unwrap method
// remains available to http.ResponseController. Deliberately bypassing the
// middleware through Unwrap also bypasses these guarantees.
//
// A raw upgrade library may hijack the connection and construct its own 101
// header map instead of serializing w.Header. In that case Commit proves the
// session cookie fits and stages Set-Cookie, but the upgrader must copy those
// staged Set-Cookie values into its upgrade response headers. This applies to
// Gorilla WebSocket's Upgrader.Upgrade responseHeader argument; Commit cannot
// safely write the 101 on the library's behalf.
//
// # Secrets and rotation
//
// New derives the signing key, and the sealing key when Options.Encrypt is set,
// from the secret. Signing proves the browser did not edit the payload;
// encryption also hides it, which matters because the visitor can always read
// their own cookie.
//
// Options.PreviousSecrets lets a secret rotate without signing everyone out:
// the manager accepts a cookie sealed with an older secret and rewrites it with
// the current one on the next response.
//
// # Defaults that are deliberately strict
//
// New sets Secure unless Options.AllowInsecure is true, so the cookie does not
// travel over plain HTTP by accident. AllowInsecure exists for local
// development on http://localhost and should not reach production.
//
// A cookie with no issuance timestamp predates expiry enforcement. The manager
// accepts one for Options.LegacyCookieGrace and then stops. Give a negative
// grace to reject every such cookie immediately.
package session
