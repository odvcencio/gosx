package docs

func Page() Node {
	return <div>
		<section id="sessions">
			<h2>Sessions</h2>
			<p>
				Sessions are the base layer. Every auth primitive in GoSX operates through a session store, so the current user and flash state are available to any file-routed page without extra plumbing.
			</p>
			{CodeBlock("go", `sessions := session.MustNew(os.Getenv("SESSION_SECRET"), session.Options{
	    Encrypt:         true,
	    PreviousSecrets: strings.Fields(os.Getenv("SESSION_PREVIOUS_SECRETS")),
	})
	app.Use(sessions.Middleware)
	app.Use(sessions.Protect)`)}
			<p>
				<span class="inline-code">session.MustNew</span>
				creates a cookie-backed session store. By default it signs cookies for integrity; with
				<span class="inline-code">Encrypt: true</span>
				it also encrypts them for confidentiality, and
				<span class="inline-code">PreviousSecrets</span>
				allows zero-downtime key rotation.
				<span class="inline-code">sessions.Protect</span>
				attaches CSRF enforcement to every mutating request.
			</p>
		</section>
		<section id="magic-links">
			<h2>Magic Links</h2>
			<p>
				Magic links let users authenticate by clicking a signed URL delivered to their email address. GoSX ships the request and callback handlers; the app supplies the resolver and the mailer.
			</p>
			{CodeBlock("go", `authn := auth.New(sessions, auth.Options{LoginPath: "/docs/auth"})
	magicLinks := authn.MagicLinks(auth.MagicLinkOptions{
	    Path:        "/auth/magic-link",
	    SuccessPath: "/dashboard",
	    FailurePath: "/login",
	    FlashKey:    "magicLink",
	    Store: authredis.NewMagicLinkStore(redisClient, authredis.Options{
	        Prefix: "myapp:prod",
	    }),
	    Resolver: auth.MagicLinkResolverFunc(func(_ context.Context, email string) (auth.User, error) {
	        return lookupOrCreateUser(email)
	    }),
	})
	app.Mount("/auth/magic-link/request", magicLinks.RequestHandler())
	app.Mount("/auth/magic-link", magicLinks.CallbackHandler())`)}
			<p>
				The resolver receives the submitted email address and must return a populated
				<span class="inline-code">auth.User</span>
				. GoSX handles token issuance, expiry, and signature verification. The issued link is exposed in the flash payload during development so apps can wire their own delivery layer without blocking on SMTP setup.
			</p>
			<p>
				For multi-node deployments, swap the default in-memory token store for the Redis-backed
				<span class="inline-code">auth/redis</span>
				adapter shown above.
			</p>
			<If cond={data.authFlows.magicLinkEnabled}>
				<form class="docs-form" method="post" action={data.authFlows.magicLinkRequestPath}>
					<input type="hidden" name="csrf_token" value={csrf.token}></input>
					<input type="hidden" name="next" value="/docs/auth"></input>
					<label class="field">
						<span>Email</span>
						<input type="email" name="email" value={user.email} placeholder="you@example.com"></input>
					</label>
					<div class="docs-form__actions">
						<button class="cta-link primary" type="submit">Send demo magic link</button>
					</div>
				</form>
				<If cond={flash.magicLink != nil}>
					<div class="callout">
						<strong>Link issued</strong>
						<p>
							Delivery target:
							{flash.magicLink.email}
						</p>
						<a class="cta-link" href={flash.magicLink.url}>Open the issued link</a>
					</div>
				</If>
			</If>
		</section>
		<section id="passkeys">
			<h2>WebAuthn / Passkeys</h2>
			<p>
				GoSX ships begin/finish handlers for WebAuthn registration and authentication. The browser helper handles credential serialization, base64 encoding, and signature verification so apps do not have to hand-roll those steps.
			</p>
			{CodeBlock("go", `webauthn := authn.WebAuthn(auth.WebAuthnOptions{
	    RPName:      "My App",
	    Origin:      publicBase,
	    SuccessPath: "/dashboard",
	    FailurePath: "/login",
	    FlashKey:    "passkey",
	    Store: authredis.NewWebAuthnStore(redisClient, authredis.Options{
	        Prefix: "myapp:prod",
	    }),
	    Resolver: auth.WebAuthnResolverFunc(func(_ context.Context, login string) (auth.User, error) {
	        return lookupUser(login)
	    }),
	})
	app.Mount("/auth/webauthn/register/options", webauthn.RegisterOptionsHandler())
	app.Mount("/auth/webauthn/register", webauthn.RegisterHandler())
	app.Mount("/auth/webauthn/login/options", webauthn.LoginOptionsHandler())
	app.Mount("/auth/webauthn/login", webauthn.LoginHandler())`)}
			{CodeBlock("javascript", `// Registration
	await GoSXWebAuthn.register(
	    "/auth/webauthn/register/options",
	    "/auth/webauthn/register",
	    { csrfToken: document.querySelector("[name=csrf_token]").value }
	)
	
	// Authentication
	await GoSXWebAuthn.authenticate(
	    "/auth/webauthn/login/options",
	    "/auth/webauthn/login",
	    { csrfToken: document.querySelector("[name=csrf_token]").value, next: "/dashboard" }
	)`)}
			<If cond={flash.passkey != nil}>
				<p class="form-status">{flash.passkey.status}</p>
			</If>
			<div class="callout">
				<strong>Note</strong>
				<p>
					OAuth provider login is deferred to v2. The same session and callback infrastructure will carry it when it lands.
				</p>
			</div>
		</section>
		<section id="protected-routes">
			<h2>Protected Routes</h2>
			<p>
				The
				<span class="inline-code">authn.Require</span>
				middleware guard redirects unauthenticated requests to the configured login path. Mount it on a subtree or individual route via
				<span class="inline-code">app.Use</span>
				or inline in an action handler.
			</p>
			{CodeBlock("go", `authn := auth.New(sessions, auth.Options{LoginPath: "/login"})
	app.Use(authn.Middleware)
	
	// Guard a specific subtree
	router.AddDir("./app/admin", route.FileRoutesOptions{
	    Middleware: []route.Middleware{authn.Require("admin")},
	})`)}
			<p>
				<span class="inline-code">authn.Require</span>
				accepts optional role names. When roles are given, users without a matching role are redirected even if they are authenticated.
			</p>
		</section>
		<section id="csrf">
			<h2>CSRF</h2>
			<p>
				CSRF protection is on by default. Every session initialized with
				<span class="inline-code">sessions.Protect</span>
				will reject mutating requests that do not include a valid token. File-routed pages expose the token as
				<span class="inline-code">csrf.token</span>
				.
			</p>
			{CodeBlock("gosx", `<form method="post" action={actionPath("submit")}>
	    <input type="hidden" name="csrf_token" value={csrf.token}></input>
	    <button type="submit">Submit</button>
	</form>`)}
			<p>
				No separate package or middleware registration is needed. The session store and
				<span class="inline-code">sessions.Protect</span>
				handle the full token lifecycle.
			</p>
		</section>
		<p class="form-status">{action.message}</p>
	</div>
}
