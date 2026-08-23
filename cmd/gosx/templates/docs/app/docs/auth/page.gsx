package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Auth</span>
			<p class="lede">
				Session-backed auth state now rides on the same request context as file pages, actions, and route middleware.
			</p>
		</div>
		<h1>
			Auth in GoSX is a session concern, not a separate framework bolted on later.
		</h1>
		<p>
			The auth middleware resolves the current user once, stores it on the request context, and exposes it to file-routed
			<span class="inline-code">.gsx</span>
			pages as
			<span class="inline-code">user</span>
			.
		</p>
		<p>
			Built-in auth handlers commit session mutations with
			<span class="inline-code">session.Commit(w, r)</span>
			before JSON success or redirects. When you use low-level ceremony methods in a custom wrapper, commit after all mutations and before writing the response; a failed cookie write must remain a terminal non-3xx error.
		</p>
		<p>
			OAuth uses one direct, pre-v1 map keyed by random state, capped at two live entries. Records contain the provider, verifier, canonical return path, and Unix-millisecond expiry; matched states are consumed before exchange work, while unknown states preserve other browser tabs.
		</p>
		<div class="note-grid">
			<div class="note">
				<strong>Current user</strong>
				<p>{user.name}</p>
			</div>
			<div class="note">
				<strong>Session flash</strong>
				<p>{flash.notice}</p>
			</div>
		</div>
		<form class="docs-form" method="post" action="/gosx/action/signIn">
			<input type="hidden" name="csrf_token" value={csrf.token}></input>
			<label class="field">
				<span>Name</span>
				<input name="name" required></input>
			</label>
			<div class="hero-actions">
				<button class="cta-link primary" type="submit">Sign in to the docs demo</button>
				<button class="cta-link" type="submit" formaction="/gosx/action/signOut">Sign out</button>
			</div>
		</form>
		<section class="callout">
			<strong>Protected route</strong>
			<p>
				Try the guarded lab route:
				<a href="/labs/secret" data-gosx-link class="cta-link">Open the secret page</a>
			</p>
		</section>
	</article>
}
