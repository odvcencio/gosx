// Package session stores per-visitor state in a signed cookie.
//
// The cookie carries the whole session. There is no server-side store to run
// or scale, and the trade is size: a session that outgrows the browser's cookie
// limit fails to write. That failure surfaces through Store.Err and
// Options.OnError rather than through the handler, because by then the response
// is already going out.
//
// # The handful that matter
//
// Of everything this package exports, an application normally touches these:
//
//	New / MustNew   build a Manager from a secret and Options
//	Manager.Middleware  wrap the handler tree; nothing below works without it
//	Manager.Protect     add CSRF checking to unsafe methods
//	Current(r)      the *Store for this request
//	Token(r)        the CSRF token to embed when a session exists
//
// On the Store itself: Set, Value, String, Decode, Delete, AddFlash, Flashes,
// Destroy, and Err. The package-level AddFlash, Values, FlashValues and Destroy
// are conveniences that look the Store up from the request first.
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
// An anonymous Token read returns empty and does not create a cookie. Set or
// AddFlash creates session state; every persisted session includes a random
// CSRF token. Responses carrying session state are private and vary by Cookie.
//
// Protect rejects unsafe browser requests from foreign origins. A fresh
// same-origin browser form needs no session token. Once a session exists, both
// the origin check and the token check must pass. Requests without Origin and
// Sec-Fetch-Site still need a session token, as before. Explicit TrustedOrigins
// can permit other browser origins. Forwarded headers are ignored unless both
// the proxy addresses and the public AllowedHosts are configured.
//
// Protect reads tokens from X-CSRF-Token. For a request that does not want JSON
// it also reads the csrf_token field in urlencoded or multipart submissions.
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
