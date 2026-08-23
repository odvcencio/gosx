package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Forms</span>
			<p class="lede">
				HTML form posts now flow through named managed actions, CSRF protection, native browser validation, and redirect-safe session flashes.
			</p>
		</div>
		<h1>
			GoSX forms can stay boring HTML and still feel like a framework feature.
		</h1>
		<p>
			This page is not using a client router trick or a bespoke fetch layer. It posts to a colocated
			<span class="inline-code">
				/gosx/action/
				{name}
			</span>
			endpoint, validates on the server, stores one explicit flash in the session, and redirects back to this page.
		</p>
		<form class="docs-form" method="post" action="/gosx/action/subscribe">
			<input type="hidden" name="csrf_token" value={csrf.token}></input>
			<label class="field">
				<span>Name</span>
				<input name="name" required></input>
			</label>
			<label class="field">
				<span>Email</span>
				<input name="email" type="email" required></input>
			</label>
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
				, the CSRF token comes from the framework session middleware, and native constraints run before the normal browser redirect. A native redirect does not implicitly rebind submitted values; enhanced requests may opt into the browser projection contract separately.
			</p>
		</section>
	</article>
}
