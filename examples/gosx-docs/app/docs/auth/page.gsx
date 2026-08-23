package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Session-backed identity</span>
			<p class="lede">
				GoSX provides signed cookie sessions, authentication middleware, one-time magic links, WebAuthn server ceremonies, and OAuth 2.0 helpers with state and PKCE.
			</p>
		</div>
		<h2 id="sessions">Sessions</h2>
		<CodeBlock lang="go" source={data.sessionSample} />
		<p>
			<span class="inline-code">session.New</span>
			requires a secret of at least 16 bytes and returns configuration errors. Cookies are signed; enable
			<span class="inline-code">Encrypt</span>
			when values must also be confidential.
			<span class="inline-code">PreviousSecrets</span>
			supports key rotation.
		</p>
		<p>
			Secure, HTTP-only, SameSite=Lax cookies are the defaults.
			<span class="inline-code">AllowInsecure</span>
			exists only for deliberate local plain-HTTP development. A
			<span class="inline-code">__Host-</span>
			cookie must stay Secure, use path
			<span class="inline-code">/</span>
			, and omit Domain.
		</p>
		<p>
			Register session middleware before auth middleware. Both sign-in state and pending auth ceremonies are stored in the request session.
		</p>
		<p>
			Built-in auth handlers explicitly call
			<span class="inline-code">session.Commit(w, r)</span>
			before JSON success or redirects. If you call
			<span class="inline-code">Begin</span>
			,
			<span class="inline-code">Callback</span>
			, or another low-level ceremony method yourself, commit after all mutations and before the response; a cookie-size failure becomes a terminal non-3xx 500.
		</p>
		<section id="session-demo" class="demo-well" aria-labelledby="session-demo-title">
			<p class="demo-well__label">Live session-backed action</p>
			<h3 id="session-demo-title">Sign in to this documentation route</h3>
			<If cond={flash.notice != nil}>
				<p class="form-status">{flash.notice}</p>
			</If>
			<If cond={!data.currentUser.signedIn}>
				<p>
					This form posts through a named GoSX action, validates on the server, rotates the signed session cookie, and carries the success notice through the session redirect.
				</p>
				<form method="post" action="/gosx/action/signIn">
					<input type="hidden" name="csrf_token" value={csrf.token} />
					<label class="field" for="docs-auth-name">
						<span>Name</span>
						<input id="docs-auth-name" name="name" type="text" value="" autocomplete="name" required />
					</label>
					<button class="cta-link primary" type="submit">Create demo session</button>
				</form>
			</If>
			<If cond={data.currentUser.signedIn}>
				<p>
					Current request identity:
					<strong>{data.currentUser.name}</strong>
					. Refresh this route to verify that the signed session survives another request.
				</p>
				<form method="post" action="/gosx/action/signOut">
					<input type="hidden" name="csrf_token" value={csrf.token} />
					<button class="cta-link" type="submit">Clear demo session</button>
				</form>
			</If>
			<p class="docs-demo-limit">
				Demo boundary: identity is intentionally local to this process and carries only the
				<span class="inline-code">docs</span>
				role. It is not a production account system.
			</p>
		</section>
		<h2 id="magic-links">Magic links</h2>
		<CodeBlock lang="go" source={data.magicSample} />
		<p>
			Magic-link tokens are random, single-use records stored until consumption or expiry. Configure a trusted absolute
			<span class="inline-code">BaseURL</span>
			, or an
			<span class="inline-code">AllowedHosts</span>
			policy for a multi-tenant deployment. GoSX does not build token-bearing links from untrusted request headers.
		</p>
		<p>
			The default memory store is process-local. Use the Redis adapter or another
			<span class="inline-code">MagicLinkStore</span>
			for multiple instances and durable flow continuity. When no sender is configured, the request handler exposes the URL in JSON or flash output; configure a sender before production.
		</p>
		<If cond={data.authFlows.magicLinkEnabled}>
			<form class="docs-form" method="post" action={data.authFlows.magicLinkRequestPath}>
				<input type="hidden" name="csrf_token" value={csrf.token} />
				<input type="hidden" name="next" value="/docs/auth" />
				<label class="field">
					<span>Email</span>
					<input type="email" name="email" value={user.email} placeholder="you@example.com" />
				</label>
				<button class="cta-link primary" type="submit">Issue docs demo link</button>
			</form>
			<If cond={flash.magicLink != nil}>
				<div class="callout">
					<strong>Magic-link status</strong>
					<p>
						{flash.magicLink.status}
						:
						{flash.magicLink.email}
					</p>
					<If cond={flash.magicLink.url != nil}>
						<a class="cta-link" href={flash.magicLink.url}>Open this local demo link</a>
					</If>
				</div>
			</If>
		</If>
		<If cond={!data.authFlows.magicLinkEnabled}>
			<div class="callout">
				<strong>Public deployment boundary</strong>
				<p>
					This production docs site disables token issuance and passkey mutation endpoints. Run the example locally to exercise those in-memory demos; production applications should bind durable stores, delivery, and abuse controls.
				</p>
			</div>
		</If>
		<h2 id="passkeys">WebAuthn and passkeys</h2>
		<CodeBlock lang="go" source={data.passkeySample} />
		<p>
			An explicit absolute
			<span class="inline-code">Origin</span>
			is required; ceremonies fail closed when it is missing or does not match client data. The four handlers exchange JSON options and responses for
			<span class="inline-code">navigator.credentials.create</span>
			and
			<span class="inline-code">navigator.credentials.get</span>
			. Your browser code performs those Web Credential API calls and serializes the returned credential fields.
		</p>
		<p>
			The memory credential store is suitable for a single process. Use
			<span class="inline-code">auth/redis</span>
			or your own
			<span class="inline-code">WebAuthnStore</span>
			when credentials must survive restarts or be shared.
		</p>
		<h2 id="oauth">OAuth 2.0</h2>
		<CodeBlock lang="go" source={data.oauthSample} />
		<p>
			Google and GitHub provider constructors fill their standard endpoints. Custom
			<span class="inline-code">OAuthProvider</span>
			values and user resolvers are supported. GoSX stores an expiring state record in the session, validates the callback, and uses PKCE S256 for the authorization-code exchange.
		</p>
		<p>
			OAuth ceremony state is a direct, pre-v1 map keyed by random state, capped at two live entries. Each record stores the provider, verifier, canonical return path, and Unix-millisecond expiry; there is no legacy envelope or mixed-version decoder. Unknown callbacks leave other live tabs intact, while a matched state is consumed before callback checks and exchange work.
		</p>
		<h2 id="protected-routes">Protected routes</h2>
		<CodeBlock lang="go" source={data.guardSample} />
		<p>
			<span class="inline-code">Require</span>
			accepts an
			<span class="inline-code">http.Handler</span>
			and blocks unauthenticated requests.
			<span class="inline-code">RequireRole</span>
			returns middleware for one exact role. HTML requests redirect to the configured login path; JSON requests receive a 401 JSON response.
		</p>
		<p>
			Read context-bound identity with
			<span class="inline-code">auth.Current(r)</span>
			after auth middleware, or use
			<span class="inline-code">authn.Current(r)</span>
			when provider fallback is desired.
		</p>
		<h2 id="csrf">CSRF</h2>
		<CodeBlock lang="gosx" source={data.csrfSample} />
		<p>
			<span class="inline-code">sessions.Protect</span>
			checks unsafe methods. Submit
			<span class="inline-code">csrf_token</span>
			as a form field or send
			<span class="inline-code">X-CSRF-Token</span>
			for JSON requests. Read the request token through
			<span class="inline-code">session.Token(r)</span>
			or the file-template
			<span class="inline-code">csrf.token</span>
			binding.
		</p>
	</article>
}
