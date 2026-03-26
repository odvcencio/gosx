package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Forms</span>
			<p class="lede">
				HTML form posts now flow through relative actions, CSRF protection, session-backed validation state, and redirect-safe success messages.
			</p>
		</div>
		<h1>GoSX forms can stay boring HTML and still feel like a framework feature.</h1>
		<p>
			This page is not using a client router trick or a bespoke fetch layer. It posts to a colocated
			<span class="inline-code">__actions</span>
			endpoint, validates on the server, flashes the result through the session, and re-renders the page with the submitted values intact.
		</p>
		<form class="docs-form" method="post" action={actionPath("subscribe")}>
			<input type="hidden" name="csrf_token" value={csrf.token}></input>
			<label class="field">
				<span>Name</span>
				<input name="name" value={actions.subscribe.values.name}></input>
			</label>
			<label class="field">
				<span>Email</span>
				<input name="email" value={actions.subscribe.values.email}></input>
			</label>
			<p class="form-error">{actions.subscribe.fieldErrors.email}</p>
			<p class="form-status">{action.message}</p>
			<p class="flash-note">{flash.notice}</p>
			<div class="hero-actions">
				<button class="cta-link primary" type="submit">Submit the example form</button>
				<a href="/docs/auth" data-gosx-link class="cta-link">Continue to auth</a>
			</div>
		</form>
		<section class="callout">
			<strong>What this page is proving</strong>
			<p>
				The page body is
				<span class="inline-code">.gsx</span>
				, the action lives beside it in
				<span class="inline-code">page.server.go</span>
				, the CSRF token comes from the framework session middleware, and the submitted values round-trip through a normal browser redirect.
			</p>
		</section>
	</article>
}
