package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Auth</span>
			<p class="lede">
				GoSX ships passwordless auth primitives by default: session-backed magic links, passkeys, and provider OAuth on the same routed request pipeline.
			</p>
		</div>
		<h1>Auth in GoSX is a session concern, not a bolt-on password stack.</h1>
		<p>
			The auth middleware resolves the current user once, stores it on the request context, and exposes it to file-routed
			<span class="inline-code">.gsx</span>
			pages as
			<span class="inline-code">user</span>
			.
		</p>
		<div class="note-grid">
			<div class="note">
				<strong>Current user</strong>
				<p>{user.name}</p>
			</div>
			<div class="note">
				<strong>Current provider</strong>
				<p>{user.meta.provider}</p>
			</div>
			<div class="note">
				<strong>Session flash</strong>
				<p>{flash.notice}</p>
			</div>
		</div>
		<section class="callout">
			<strong>Default stance</strong>
			<p>Password flows are not shipped by default. If an app wants username-and-password auth, it owns that logic.</p>
		</section>
		<If when={authFlows.magicLinkEnabled}>
			<section class="callout">
				<strong>Magic link</strong>
				<p>Post an email address to the built-in handler and GoSX will issue a signed-in session callback without requiring a separate auth subsystem.</p>
				<form class="docs-form" method="post" action={authFlows.magicLinkRequestPath}>
					<input type="hidden" name="csrf_token" value={csrf.token}></input>
					<input type="hidden" name="next" value="/docs/auth"></input>
					<label class="field">
						<span>Email</span>
						<input name="email" value={user.email}></input>
					</label>
					<div class="hero-actions">
						<button class="cta-link primary" type="submit">Send demo magic link</button>
					</div>
				</form>
				<If when={flash.magicLink}>
					<div class="note-grid">
						<div class="note">
							<strong>Delivery</strong>
							<p>{flash.magicLink.email}</p>
						</div>
						<div class="note">
							<strong>Preview</strong>
							<a class="cta-link" href={flash.magicLink.url}>Open the issued link</a>
						</div>
					</div>
				</If>
			</section>
		</If>
		<If when={authFlows.webauthnEnabled}>
			<section class="callout">
				<strong>Passkeys / WebAuthn</strong>
				<p>
					GoSX ships begin/finish handlers plus a browser helper so apps do not have to hand-roll base64 decoding, credential serialization, or signature verification.
				</p>
				{DocsCodeBlock("javascript", `await GoSXWebAuthn.register(
  "/auth/webauthn/register/options",
  "/auth/webauthn/register",
  { csrfToken: "${csrf.token}" }
)

await GoSXWebAuthn.authenticate(
  "/auth/webauthn/login/options",
  "/auth/webauthn/login",
  { csrfToken: "${csrf.token}", next: "/docs/auth" }
)`)}
				<If when={flash.passkey}>
					<p class="form-status">{flash.passkey.status}</p>
				</If>
			</section>
		</If>
		<If when={len(authFlows.oauthProviders) > 0}>
			<section class="callout">
				<strong>OAuth providers</strong>
				<p>Provider OAuth rides the same session-backed callback path. When provider credentials are configured, the docs app exposes direct sign-in links.</p>
				<div class="hero-actions">
					<Each as="provider" of={authFlows.oauthProviders}>
						<a class="cta-link" href={provider.Href}>{provider.Label}</a>
					</Each>
				</div>
				<If when={flash.oauth}>
					<p class="form-status">{flash.oauth.provider}</p>
				</If>
			</section>
		</If>
		<section class="callout">
			<strong>Custom app auth logic</strong>
			<p>The framework defaults are passwordless, but apps can still write their own session actions when they need a custom identity model.</p>
		</section>
		<form class="docs-form" method="post" action={actionPath("signIn")}>
			<input type="hidden" name="csrf_token" value={csrf.token}></input>
			<label class="field">
				<span>Name</span>
				<input name="name"></input>
			</label>
			<p class="form-error">{actions.signIn.fieldErrors.name}</p>
			<div class="hero-actions">
				<button class="cta-link primary" type="submit">Run custom sign-in action</button>
				<button class="cta-link" type="submit" formaction={actionPath("signOut")}>Sign out</button>
			</div>
		</form>
		<section class="callout">
			<strong>Protected route</strong>
			<p>
				Try the guarded lab route:
				<a href="/labs/secret" data-gosx-link class="cta-link">Open the secret page</a>
			</p>
		</section>
		<p class="form-status">{action.message}</p>
	</article>
}
