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
				attributes. Native forms and the managed JSON runtime both use the framework-owned bounded endpoint and an explicit session commit.
			</p>
			{CodeBlock("gsx", "<form method=\"post\" action=\"/gosx/action/subscribe\">\n\t<input type=\"hidden\" name=\"csrf_token\" value={csrf.token} />\n\t<input name=\"email\" type=\"email\" required placeholder=\"you@example.com\" />\n\t<button type=\"submit\">Subscribe</button>\n</form>")}
		</section>
		<section id="server-actions" class="docs-section-block">
			<h2>Server Actions</h2>
			<p>
				Managed actions are named handlers registered explicitly with
				<span class="inline-code">route.Router.RegisterManagedPOST</span>
				before the router is built. Each action receives an
				<span class="inline-code">*action.Context</span>
				with the parsed form data and the original HTTP request.
			</p>
			{CodeBlock("go", "router.RegisterManagedPOST(\"subscribe\", action.Config{}, func(ctx *action.Context) (action.Result, error) {\n\tif ctx.Form.Value(\"email\") == \"\" {\n\t\treturn action.Result{}, action.Validation(\"Email is required.\", map[string]string{\"email\": \"Please enter an email address.\"})\n\t}\n\tif err := session.AddFlash(ctx.Request, \"notice\", \"Subscribed!\"); err != nil {\n\t\treturn action.Result{}, err\n\t}\n\treturn action.Result{OK: true, Redirect: \"/docs/forms\"}, nil\n})")}
			<p>
				The action URL is a stable named endpoint
				<span class="inline-code">/gosx/action/name</span>
				. It resolves to the framework-owned
				<span class="inline-code">/gosx/action/name</span>
				endpoint registered explicitly before the owning route.Router is built.
			</p>
		</section>
		<section id="validation" class="docs-section-block">
			<h2>Validation</h2>
			<p>
				Use native HTML constraints such as
				<span class="inline-code">required</span>
				and
				<span class="inline-code">type=\"email\"</span>
				for the first validation pass. The server still calls
				<span class="inline-code">action.Validation</span>
				to reject malformed or replayed requests with a typed protocol error. On successful requests, the handler explicitly stores a flash before returning a redirect; templates read that flash rather than an implicit action binding.
			</p>
			{CodeBlock("go", "return action.Result{}, action.Validation(\"Please correct the highlighted fields.\", map[string]string{\n\t\"email\": \"A valid email address is required.\",\n\t\"name\":  \"Name must be at least two characters.\",\n})")}
			{CodeBlock("gsx", "<input name=\"email\" type=\"email\" required />\n<p class=\"flash-notice\">{flash.notice}</p>")}
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
			{CodeBlock("go", "import \"m31labs.dev/gosx/session\"\n\n// inside a managed action:\nsession.AddFlash(ctx.Request, \"notice\", \"Your changes were saved.\")\nreturn action.Result{OK: true, Redirect: \"/docs/forms\"}, nil")}
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
				<span class="inline-code">Result.Redirect</span>
				from an action to send the browser to a different URL after a successful post. This is the standard POST-redirect-GET pattern and prevents double-submission on browser refresh.
			</p>
			{CodeBlock("go", "// Redirect to a confirmation page after success.\nreturn action.Result{OK: true, Redirect: \"/subscribe/confirmed\"}, nil")}
			<p>
				For redirect-backed flows, store the user-facing notice explicitly with
				<span class="inline-code">session.AddFlash</span>
				before returning the result. The session middleware then carries that notice across the browser redirect.
			</p>
		</section>
		<div class="demo-well" role="region" aria-label="Form demo">
			<p class="demo-well__label">Live demo</p>
			<p class="form-status">
				Submit through the managed action endpoint.
			</p>
			<form method="post" action="/gosx/action/subscribe">
				<input type="hidden" name="csrf_token" value={csrf.token} />
				<label for="demo-email">Email</label>
				<input id="demo-email" name="email" type="email" required placeholder="you@example.com" value="" />
				<p class="form-error">{flash.notice}</p>
				<button type="submit">Subscribe</button>
			</form>
		</div>
	</div>
}
