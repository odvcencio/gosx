package docs

func Page() Node {
	return <div>
		<section id="html-forms" class="docs-section-block">
			<h2>HTML Forms</h2>
			<p>
				GoSX forms are plain HTML forms. Post to a colocated action endpoint using the standard
				<span class="inline-code">method="post"</span>
				and
				<span class="inline-code">action</span>
				attributes. No JavaScript fetch layer, no custom hooks — just a browser form post that flows through the server and redirects back with the result flashed into the session.
			</p>
			{CodeBlock("gsx", "<form method=\"post\" action={actionPath(\"subscribe\")}>\n\t<input type=\"hidden\" name=\"csrf_token\" value={csrf.token} />\n\t<input name=\"email\" type=\"email\" placeholder=\"you@example.com\" />\n\t<button type=\"submit\">Subscribe</button>\n</form>")}
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
			{CodeBlock("go", "route.MustRegisterFileModuleHere(route.FileModuleOptions{\n\tActions: route.FileActions{\n\t\t\"subscribe\": func(ctx *action.Context) error {\n\t\t\temail := ctx.FormData[\"email\"]\n\t\t\tif email == \"\" {\n\t\t\t\tctx.ValidationFailure(\"Email is required.\", map[string]string{\n\t\t\t\t\t\"email\": \"Please enter an email address.\",\n\t\t\t\t})\n\t\t\t\treturn nil\n\t\t\t}\n\t\t\treturn ctx.Success(\"Subscribed!\", nil)\n\t\t},\n\t},\n})")}
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
			{CodeBlock("go", "import \"github.com/odvcencio/gosx/session\"\n\n// inside an action handler:\nsession.AddFlash(ctx.Request, \"notice\", \"Your changes were saved.\")\nreturn ctx.Success(\"\", nil)")}
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
				<span class="inline-code">ctx.Redirect</span>
				from an action to send the browser to a different URL after a successful post. This is the standard POST-redirect-GET pattern and prevents double-submission on browser refresh.
			</p>
			{CodeBlock("go", "// Redirect to a confirmation page after success.\nctx.Redirect(\"/subscribe/confirmed\")")}
			<p>
				For redirect-backed flows where you want the action state to survive the redirect, the framework flashes the result automatically through the session when
				<span class="inline-code">sessions.Protect</span>
				middleware is active.
			</p>
		</section>
		<div class="demo-well" role="region" aria-label="Form demo">
			<p class="demo-well__label">Live demo</p>
			<If cond={actions.subscribe.result.ok}>
				<p class="form-status form-status--ok">{actions.subscribe.result.message}</p>
			</If>
			<If cond={!actions.subscribe.result.ok && actions.subscribe.submitted}>
				<p class="form-status form-status--error">{actions.subscribe.result.message}</p>
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
