package docs

func Page() Node {
	return <div>
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
			{CodeBlock("gsx", "<form method=\"post\" action={actionPath(\"subscribe\")} data-gosx-managed>\n\t<input type=\"hidden\" name=\"csrf_token\" value={csrf.token} />\n\t<input name=\"email\" type=\"email\" placeholder=\"you@example.com\" />\n\t<button type=\"submit\">Subscribe</button>\n</form>")}
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
			{CodeBlock("go", "func init() {\n\tif err := route.RegisterFileModuleHere(route.FileModuleOptions{\n\t\tActions: route.FileActions{\n\t\t\t\"subscribe\": func(ctx *action.Context) error {\n\t\t\t\temail := ctx.FormData[\"email\"]\n\t\t\t\tif email == \"\" {\n\t\t\t\t\tctx.ValidationFailure(\"Email is required.\", map[string]string{\n\t\t\t\t\t\t\"email\": \"Please enter an email address.\",\n\t\t\t\t\t})\n\t\t\t\t\treturn nil\n\t\t\t\t}\n\t\t\t\treturn ctx.Success(\"Subscribed!\", nil)\n\t\t\t},\n\t\t},\n\t}); err != nil {\n\t\tlog.Fatal(err)\n\t}\n}")}
			<p>
				The action URL is constructed at render time by
				<span class="inline-code">actionPath("name")</span>
				. It resolves to the page-relative
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
			{CodeBlock("go", "ctx.ValidationFailure(\"Please correct the highlighted fields.\", map[string]string{\n\t\"email\": \"A valid email address is required.\",\n\t\"name\":  \"Name must be at least two characters.\",\n})")}
			<p>
				In the template, read field errors through
				<span class="inline-code">actions.subscribe.fieldErrors.email</span>
				and repopulate inputs from
				<span class="inline-code">actions.subscribe.values.email</span>
				.
			</p>
			{CodeBlock("gsx", "<input name=\"email\" value={actions.subscribe.values.email} />\n<p class=\"form-error\">{actions.subscribe.fieldErrors.email}</p>")}
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
				<span class="inline-code">actionPath(...)</span>
				forms. Static wrappers are fine; GET or native/external forms and fields supplied through dynamic component boundaries remain application-owned.
			</p>
			{CodeBlock("gsx", "<input type=\"hidden\" name=\"csrf_token\" value={csrf.token} />")}
			<p>
				The token is automatically available as
				<span class="inline-code">csrf.token</span>
				in every file-routed page template. No extra wiring is required as long as the session middleware is mounted in
				<span class="inline-code">main.go</span>
				.
			</p>
			{CodeBlock("go", "// main.go\napp.Use(sessions.Middleware)\napp.Use(sessions.Protect)")}
		</section>
		<section id="flash-messages" class="docs-section-block">
			<h2>Flash Messages</h2>
			<p>
				Flash messages survive a redirect. Store a notice in the session from an action handler, then read it back in the template after the browser follows the redirect to the GET page.
			</p>
			{CodeBlock("go", "import \"m31labs.dev/gosx/session\"\n\n// inside an action handler:\nsession.AddFlash(ctx.Request, \"notice\", \"Your changes were saved.\")\nreturn ctx.Success(\"\", nil)")}
			{CodeBlock("gsx", "<p class=\"flash-notice\">{flash.notice}</p>")}
			<p>
				The
				<span class="inline-code">flash</span>
				binding in templates holds the first value for each flash key. Use
				<span class="inline-code">flashes</span>
				to access all values when a key may carry multiple messages.
			</p>
		</section>
		<section id="redirects" class="docs-section-block">
			<h2>Redirects</h2>
			<p>
				Call
				<span class="inline-code">ctx.RedirectWithMessage</span>
				from an action to send the browser to a different URL after a successful post while carrying one human-readable completion message through both native and managed submissions. This preserves the POST-redirect-GET safety model without forcing an enhanced page to reload.
			</p>
			{CodeBlock("go", "// Redirect safely and explain what changed.\nctx.RedirectWithMessage(\"/subscribe/confirmed\", \"Subscription confirmed.\")")}
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
			{CodeBlock("go", "// In the form:\n<input type=\"hidden\" name={action.ReturnTargetField} value=\"/board?tab=all#roster\" />\n\n// In the action:\nctx.RedirectBackWithMessage(\"/account\", \"Profile saved.\")")}
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
			{CodeBlock("go", "if action.WantsJSON(ctx.Request) {\n    // Managed action: return structured feedback.\n}")}
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
			{CodeBlock("gsx", "<div class=\"toast-stack\" data-gosx-toast-host aria-live=\"polite\" aria-relevant=\"additions\"></div>")}
		</section>
		<div class="demo-well" role="region" aria-label="Form demo">
			<p class="demo-well__label">Live demo</p>
			<If cond={actions.subscribe.ok}>
				<p class="form-status form-status--ok">{actions.subscribe.message}</p>
			</If>
			<If cond={!actions.subscribe.ok && actions.subscribe.status != 0}>
				<p class="form-status form-status--error">{actions.subscribe.message}</p>
			</If>
			<form method="post" action={actionPath("subscribe")}>
				<input type="hidden" name="csrf_token" value={csrf.token} />
				<label for="demo-email">Email</label>
				<input
					id="demo-email"
					name="email"
					type="email"
					placeholder="you@example.com"
					value={actions.subscribe.values.email}
				 />
				<p class="form-error">{actions.subscribe.fieldErrors.email}</p>
				<button type="submit">Subscribe</button>
			</form>
		</div>
	</div>
}
