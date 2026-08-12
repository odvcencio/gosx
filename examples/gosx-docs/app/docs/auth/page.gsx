package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Session-backed identity</span>
			<p class="lede">
				GoSX provides signed cookie sessions, authentication middleware, one-time magic links, WebAuthn server ceremonies, and OAuth 2.0 helpers with state and PKCE.
			</p>
		</div>
		<h1 id="sessions">Sessions</h1>
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
