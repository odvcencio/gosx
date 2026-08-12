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
			The server renderer intentionally accepts a fail-closed expression subset. Each expression may be a quoted string,
			<span class="inline-code">true</span>
			or
			<span class="inline-code">false</span>
			, an ungrouped non-negative base-10 integer in the
			<span class="inline-code">int64</span>
			range, a finite ungrouped decimal float, or one direct field on
			<span class="inline-code">props</span>
			whose type is a parity-safe built-in scalar declared in the same file. Nested selectors, indexing, calls, unary and binary operators, ordinary local declarations,
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
				can compose it with attributes that match the strict field contract below. Typed route-loader binding is not part of this release; route data continues to use the legacy page form.
			</p>
		</section>
		<p>
			Same-file calls accept exact exported Go field spelling or an unambiguous TSX-like lower-camel alias, such as
			<span class="inline-code">Label</span>
			/
			<span class="inline-code">label</span>
			,
			<span class="inline-code">HTMLFor</span>
			/
			<span class="inline-code">htmlFor</span>
			, and
			<span class="inline-code">URL</span>
			/
			<span class="inline-code">url</span>
			. Ambiguous aliases must use exact Go spelling. Every field the callee directly renders must be passed explicitly, including
			<span class="inline-code">0</span>
			,
			<span class="inline-code">false</span>
			, and an empty string; omission is rejected so Go and the server renderer observe the same zero values.
		</p>
		<h2 id="legacy-components">Legacy components</h2>
		<p>
			The Go-function spelling remains fully supported and is the compatibility path for existing source. It is also the documented route form when a loader exposes the dynamic
			<span class="inline-code">data</span>
			binding.
		</p>
		<p>
			Both declaration styles may coexist in one file, but v0.39 keeps component calls within the same style. This prevents a dynamic legacy call from bypassing a strict component's typed prop contract.
		</p>
		<CodeBlock lang="gosx" source={data.legacySample} />
		<p>
			Legacy bodies can declare locals and use the established
			<span class="inline-code">Each</span>
			,
			<span class="inline-code">If</span>
			, and
			<span class="inline-code">Slot</span>
			structural builtins. Islands and engines also use this form in v0.39; strict client directives fail closed until their browser and server paths can preserve the typed contract.
		</p>
		<h2 id="attributes">Elements and attributes</h2>
		<p>
			Lowercase tags create HTML elements. Static quoted attributes, boolean attributes, expression attributes, expression children, and fragments are available in both styles within their validation contracts. Strict capitalized tags resolve only to same-file strict components; renderer builtins and element spread attributes remain legacy-only in v0.39.
		</p>
		<CodeBlock lang="gosx" source={data.attributesSample} />
		<p>
			A strict component call is intentionally narrower than an HTML element: pass named attributes that match the props struct. Strict component calls reject spread props and positional child content; use a legacy caller-and-callee chain when component composition needs nested Node content in v0.39.
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
			<span class="inline-code">gosx export</span>
			, and
			<span class="inline-code">gosx render</span>
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
