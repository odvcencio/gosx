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
		<form class="docs-form" method="post" action={actionPath("signIn")}>
			<input type="hidden" name="csrf_token" value={csrf.token}></input>
			<label class="field">
				<span>Name</span>
				<input name="name"></input>
			</label>
			<p class="form-error">{actions.signIn.fieldErrors.name}</p>
			<div class="hero-actions">
				<button class="cta-link primary" type="submit">Sign in to the docs demo</button>
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
