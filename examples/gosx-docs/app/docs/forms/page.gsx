package docs

import "m31labs.dev/gosx/route"

type SubscribeProps struct {
	Form route.FormState
}

component SubscribeForm(props: SubscribeProps) {
	return <form method="post" action={props.Form.ActionURL} data-gosx-managed>
		<input type="hidden" name="csrf_token" value={props.Form.CSRFToken} />
		<label>
			<span>Email address</span>
			<input name="email" type="email" value={props.Form.Values["email"]} placeholder="you@example.com" />
		</label>
		<p role="alert">{props.Form.FieldErrors["email"]}</p>
		<p role="status">{props.Form.Message}</p>
		<p>{props.Form.Flash["notice"]}</p>
		<button type="submit">Subscribe</button>
	</form>
}

func Page() Node {
	return <div>
		<section class="docs-live-example" aria-label="Validated server form">
			<p class="eyebrow">Try server validation</p>
			<SubscribeForm {...data.subscribe} />
			<a
				href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/forms/page.server.go"
				rel="noopener"
			>View the validating action</a>
		</section>
		<section id="html-forms" class="docs-section-block">
			<h2>HTML Forms</h2>
			<p>
				GoSX forms are plain HTML forms. Add
				<span class="inline-code">data-gosx-managed</span>
				to progressively enhance a form with no-reload submission, pending state, structured validation, and accessible result announcements. Post to a colocated action endpoint using the standard
				<span class="inline-code">method="post"</span>
				and
				<span class="inline-code">action</span>
				attributes. Without the runtime, the same markup remains an ordinary browser form.
			</p>
			{CodeBlock("gsx", data.sample001)}
		</section>
		<section id="server-actions" class="docs-section-block">
			<h2>Server Actions</h2>
			<p>
				Actions are named handlers registered in
				<span class="inline-code">page.server.go</span>
				alongside the page's
				<span class="inline-code">Load</span>
				function. Each action receives an
				<span class="inline-code">*action.Context</span>
				with the parsed form data and the original HTTP request.
			</p>
			{CodeBlock("go", data.sample002)}
			<p>
				In Load, call
				<span class="inline-code">ctx.FormState("subscribe")</span>
				and return it in a typed Go value matching the page's props fields. The
				<span class="inline-code">route.FormState</span>
				value includes ActionURL, CSRFToken, Values, FieldErrors, Flash, Message, OK, and Status. ActionURL points to the page-relative
				<span class="inline-code">/__actions/name</span>
				endpoint that the router registers automatically when the page module declares that action.
			</p>
		</section>
		<section id="validation" class="docs-section-block">
			<h2>Validation</h2>
			<p>
				Call
				<span class="inline-code">ctx.ValidationFailure</span>
				to return field-level errors. The framework flashes the result through the session on a POST-redirect-GET cycle, so the browser lands back on the form page with errors and submitted values intact.
			</p>
			{CodeBlock("go", data.sample003)}
			<p>
				In a strict component, read field errors through
				<span class="inline-code">props.Form.FieldErrors["email"]</span>
				and repopulate inputs from
				<span class="inline-code">props.Form.Values["email"]</span>
				. String map lookups use literal keys; absent keys return the empty string, including on the first GET. The helper reads ctx.ActionState("subscribe") and its Result.Values and Result.FieldErrors for you.
			</p>
			{CodeBlock("gsx", data.sample004)}
		</section>
		<section id="csrf-protection" class="docs-section-block">
			<h2>CSRF Protection</h2>
			<p>
				The session middleware generates a CSRF token per session. Include it in every form as a hidden field named
				<span class="inline-code">csrf_token</span>
				. The framework validates the token before running the action handler and rejects mismatched requests with a 403.
			</p>
			<p>
				For a mutating file action,
				<span class="inline-code">gosx check</span>
				also catches a statically provable missing token inside
				<span class="inline-code">props.Form.ActionURL</span>
				forms. Static wrappers are fine; GET or native/external forms and fields supplied through dynamic component boundaries remain application-owned.
			</p>
			{CodeBlock("gsx", data.sample005)}
			<p>
				The helper exposes the session token as
				<span class="inline-code">props.Form.CSRFToken</span>
				without creating a session on an anonymous GET. Mount the session middleware in
				<span class="inline-code">main.go</span>
				.
			</p>
			{CodeBlock("go", data.sample006)}
		</section>
		<section id="flash-messages" class="docs-section-block">
			<h2>Flash Messages</h2>
			<p>
				Flash messages survive a redirect. Store a notice in the session from an action handler, then read it back in the template after the browser follows the redirect to the GET page.
			</p>
			{CodeBlock("go", data.sample007)}
			{CodeBlock("gsx", data.sample008)}
			<p>
				FormState.Flash holds the first value for each public flash key as a string. Session middleware consumes the flashes once per request; reading FormState repeatedly in that request preserves them. Legacy pages may still use actionPath(...), csrf.token, actions.subscribe.values, actions.subscribe.fieldErrors, flash, and flashes. Strict components receive these through props.
			</p>
		</section>
		<section id="redirects" class="docs-section-block">
			<h2>Redirects</h2>
			<p>
				Call
				<span class="inline-code">ctx.RedirectWithMessage</span>
				from an action to send the browser to a different URL after a successful post while carrying one human-readable completion message through both native and managed submissions. This preserves the POST-redirect-GET safety model without forcing an enhanced page to reload.
			</p>
			{CodeBlock("go", data.sample009)}
			<p>
				When the action should return to the page that submitted it, opt in with
				<span class="inline-code">ctx.RedirectBackWithMessage</span>
				. It prefers a valid
				<span class="inline-code">__gosx_return_to</span>
				field, then uses the sanitized root-relative fallback. Empty, malformed, absolute, and protocol-relative targets resolve to
				<span class="inline-code">/</span>
				; valid query strings and fragments are preserved. The reserved field is removed before the handler sees
				<span class="inline-code">ctx.FormData</span>
				.
			</p>
			{CodeBlock("gsx", data.sample010)}
			{CodeBlock("go", data.sample011)}
			<p>
				Use
				<span class="inline-code">ctx.RedirectWithMessage</span>
				when the action intentionally changes destination. Explicit non-empty redirect values are sanitized to same-origin root-relative paths before either native
				<span class="inline-code">Location</span>
				or managed JSON is emitted; unsafe values resolve to
				<span class="inline-code">/</span>
				.
			</p>
			<p>
				Most actions never need to inspect their transport mode. When application code genuinely must branch, call
				<span class="inline-code">action.WantsJSON(ctx.Request)</span>
				to share GoSX's authoritative managed-action negotiation instead of parsing request headers again.
			</p>
			{CodeBlock("go", data.sample012)}
			<p>
				Render one
				<span class="inline-code">data-gosx-toast-host</span>
				in your layout to make managed-action messages visibly float. The runtime supplies accessible status and dismiss behavior; style the stable
				<span class="inline-code">gosx-toast</span>
				,
				<span class="inline-code">gosx-toast--success</span>
				, and
				<span class="inline-code">gosx-toast--error</span>
				classes to match your product.
			</p>
			{CodeBlock("gsx", data.sample013)}
		</section>
		<div class="demo-well" role="region" aria-label="Form demo">
			<p class="demo-well__label">Live demo</p>
			<SubscribeForm {...data.subscribe} />
		</div>
	</div>
}
