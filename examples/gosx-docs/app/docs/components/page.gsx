package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Authoring model</span>
			<p class="lede">
				GoSX supports two component spellings in the same
				<span class="inline-code">.gsx</span>
				file: a strict, typed form for deliberately small server components and the established Go-function form for routes, islands, and richer bodies.
			</p>
		</div>
		<h1 id="two-styles">Components</h1>
		<p>
			Both styles lower to the same GoSX component IR. Choosing a style changes the source contract and validation boundary; it does not create a second renderer or runtime.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>Strict and typed</strong>
				<p>
					Use
					<span class="inline-code">component Name(props: Type)</span>
					for a compact, TSX-like declaration whose props are checked as Go types by project-aware CLI commands.
				</p>
			</div>
			<div class="card">
				<strong>Legacy and flexible</strong>
				<p>
					Use
					<span class="inline-code">func Name(...) Node</span>
					for route data bindings, local statements, helpers, structural builtins, and existing applications.
				</p>
			</div>
		</section>
		<h2 id="strict-components">Strict components</h2>
		<p>
			A strict server component has zero or one parameter. When present, the parameter must be named
			<span class="inline-code">props</span>
			and its type must be a Go type visible in the package. Its body is one top-level
			<span class="inline-code">return</span>
			whose value is GSX markup.
		</p>
		<CodeBlock lang="gosx" source={data.strictSample} />
		<p>
			The server renderer intentionally accepts a fail-closed expression subset: props paths, indexes, literals, supported unary and binary operators, and method calls rooted in
			<span class="inline-code">props</span>
			. Ordinary local declarations,
			<span class="inline-code">if</span>
			statements, free helper calls, imported function calls, and cross-file component calls are outside the v0.39 strict contract.
		</p>
		<section class="callout">
			<strong>Keep strict components local</strong>
			<p>
				Define a typed component and call it from the same
				<span class="inline-code">.gsx</span>
				file. A zero-props
				<span class="inline-code">Page()</span>
				can compose it with literal or supported expression attributes. Typed route-loader binding is not part of this release; route data continues to use the legacy page form.
			</p>
		</section>
		<h2 id="legacy-components">Legacy components</h2>
		<p>
			The Go-function spelling remains fully supported and is the compatibility path for existing source. It is also the documented route form when a loader exposes the dynamic
			<span class="inline-code">data</span>
			binding.
		</p>
		<CodeBlock lang="gosx" source={data.legacySample} />
		<p>
			Legacy bodies can declare locals and use the established
			<span class="inline-code">Each</span>
			,
			<span class="inline-code">If</span>
			, and
			<span class="inline-code">Slot</span>
			structural builtins. Islands also use this form when they declare signals and handlers before their single returned tree.
		</p>
		<h2 id="attributes">Elements and attributes</h2>
		<p>
			Lowercase tags create HTML elements; capitalized tags call GoSX components or builtins. Static quoted attributes, boolean attributes, expression attributes, expression children, fragments, and element spread attributes are available in both styles when the target renderer supports the value.
		</p>
		<CodeBlock lang="gosx" source={data.attributesSample} />
		<p>
			A strict component call is intentionally narrower than an HTML element: pass named attributes that match the props struct. Strict component calls reject spread props and positional child content; model child content as a named prop when the component contract needs it.
		</p>
		<p>
			Strict markup also accepts the familiar aliases
			<span class="inline-code">className</span>
			and
			<span class="inline-code">htmlFor</span>
			; rendered HTML uses
			<span class="inline-code">class</span>
			and
			<span class="inline-code">for</span>
			. GoSX escapes ordinary text and attribute values. Use framework primitives for explicitly trusted HTML rather than concatenating markup strings.
		</p>
		<h2 id="tooling">Which checker is authoritative?</h2>
		<p>
			Run project-aware commands from the application module. They can load sibling Go declarations and enforce the strict component contract in package context.
		</p>
		<CodeBlock lang="bash" source={data.commandsSample} />
		<p>
			The current LSP parses and reports context-free GSX structure, but it is not the package type-checking authority. Treat
			<span class="inline-code">gosx check</span>
			,
			<span class="inline-code">gosx build</span>
			,
			<span class="inline-code">gosx dev</span>
			, and
			<span class="inline-code">gosx render</span>
			, and
			<span class="inline-code">gosx export</span>
			as the executable source of truth for strict diagnostics.
		</p>
		<h2 id="choosing">Choosing a style</h2>
		<ul>
			<li>
				Start strict for small, presentational, same-file server components with stable prop types.
			</li>
			<li>
				Use legacy pages for loader data, route params, layouts, structural control flow, and helper-rich bodies.
			</li>
			<li>
				Keep existing legacy components as-is; migration is optional and can happen one local component at a time.
			</li>
			<li>
				Use islands only for browser interaction, not as a workaround for a server component that needs richer Go logic.
			</li>
		</ul>
	</article>
}
