package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Authoring model</span>
			<p class="lede">
				GoSX teaches one component spelling: a strict, typed form. Declare a
				<span class="inline-code">.gsx</span>
				component with
				<span class="inline-code">component Name(props: Type)</span>
				, and the project-aware CLI checks its props as Go types.
			</p>
		</div>
		<h2 id="two-styles">One authoring style</h2>
		<p>
			Every new component uses
			<span class="inline-code">component Name(props: Type)</span>
			. GoSX still runs the older Go-function spelling,
			<span class="inline-code">func Name(...) Node</span>
			, for existing applications, engines, and loader-bound routes. Strict components can hydrate as islands when they stay within the supported island VM subset. See "Legacy components (deprecated)" below for the older spelling. This page does not teach it to a new author.
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
				<strong>Legacy (deprecated)</strong>
				<p>
					<span class="inline-code">func Name(...) Node</span>
					still runs, but it is deprecated for ordinary server components. Engines and loader-bound routes still need it; supported strict islands do not. See below.
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
			whose type is a parity-safe built-in scalar declared in the same file. Nested selectors, indexing, calls, unary operators, ordinary local declarations,
			<span class="inline-code">if</span>
			statements, free helper calls, imported function calls, and cross-file component calls are outside the v0.39 strict contract. v0.42 widens this contract in two narrow, typed ways; the next section covers both.
		</p>
		<h2 id="strict-expressions">Concatenation and conditional rendering</h2>
		<p>
			A hole may join a
			<span class="inline-code">+</span>
			chain of string literals and
			<span class="inline-code">props</span>
			field selectors. The chain needs at least one literal and at least one field. Each field must have the exact declared type
			<span class="inline-code">string</span>
			, not a named string type. Use this to build a class name or a label from a typed prop.
		</p>
		<CodeBlock lang="gosx" source={data.concatSample} />
		<p>
			A strict body may also render
			<span class="inline-code">
				&lt;If cond=
				{"{"}
				...
				{"}"}
				&gt;
			</span>
			. The
			<span class="inline-code">cond</span>
			attribute takes exactly one bool
			<span class="inline-code">props</span>
			field, bare or compared with
			<span class="inline-code">== false</span>
			. GoSX renders the children when
			<span class="inline-code">cond</span>
			is true and renders nothing otherwise. Write a second
			<span class="inline-code">&lt;If&gt;</span>
			with
			<span class="inline-code">== false</span>
			for the opposite branch;
			<span class="inline-code">fallback</span>
			and
			<span class="inline-code">else</span>
			attributes are not part of this form.
		</p>
		<CodeBlock lang="gosx" source={data.conditionalSample} />
		<h2 id="strict-loops-and-spread">Loops and spread props</h2>
		<p>
			A strict body may also loop with
			<span class="inline-code">
				&lt;Each of=
				{"{"}
				...
				{"}"}
				as="name"&gt;
			</span>
			. The
			<span class="inline-code">of</span>
			source must be a
			<span class="inline-code">props</span>
			field declared
			<span class="inline-code">[]T</span>
			, where
			<span class="inline-code">T</span>
			is a value struct declared in the same file. Inside the loop, the binding resolves selectors exactly like
			<span class="inline-code">props</span>
			does, against
			<span class="inline-code">T</span>
			's fields. An optional
			<span class="inline-code">index="i"</span>
			attribute binds the loop index as a plain
			<span class="inline-code">int</span>
			; it renders bare but admits no selector. An empty or nil slice renders nothing — write an
			<span class="inline-code">&lt;If&gt;</span>
			sibling for the empty state.
		</p>
		<CodeBlock lang="gosx" source={data.eachSample} />
		<p>
			A strict call site may also spread exactly one value and nothing else:
			<span class="inline-code">
				&lt;Callee
				{"{"}
				...source
				{"}"}
				/&gt;
			</span>
			. A strict caller's spread source must have exactly the callee's declared props type — the Go compiler proves this like any other typed call. A legacy caller's single spread is proved instead at the file-renderer boundary: the source must be a struct with every field the callee renders, of the exact declared type; a
			<span class="inline-code">map[string]any</span>
			source is always rejected, because a map can omit a key where the typed call would supply a zero value, and map keys have no declared type to check ahead of time. Spread plus named attributes, and more than one spread, stay rejected at every call site.
		</p>
		<CodeBlock lang="gosx" source={data.spreadSample} />
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
		<h2 id="legacy-components">Legacy components (deprecated)</h2>
		<p>
			This section is a migration note, not something to write in new code. The Go-function spelling still compiles, checks, and renders, so existing source keeps working. It is also today's only route form when a loader exposes the dynamic
			<span class="inline-code">data</span>
			binding.
		</p>
		<p>
			<span class="inline-code">gosx check</span>
			now warns on an
			<span class="inline-code">untyped legacy</span>
			component:
			<span class="inline-code">func Name(props any) Node</span>
			. This form is deprecated, and GoSX removes it before v1.0. Declare its props as a same-file struct, then switch to
			<span class="inline-code">component Name(props: Type)</span>
			.
		</p>
		<p>
			Both declaration styles may coexist in one file. A component call stays within its declaration style, with one exception: a
			<span class="inline-code">typed legacy</span>
			component —
			<span class="inline-code">func Name(props T) Node</span>
			whose
			<span class="inline-code">T</span>
			is a struct declared in the same
			<span class="inline-code">.gsx</span>
			file — declares the same prop contract a strict component does, so it takes part in strict calls in both directions. A component that declares no such type (
			<span class="inline-code">props any</span>
			, an attribute list, or a type from another file) is an
			<span class="inline-code">untyped legacy</span>
			component and keeps the cross-style ban, because nothing about its props can be checked.
		</p>
		<p>
			A typed legacy component keeps the legacy render frame's flattened map binding, so its body observes its props exactly as it always has: children arrive under
			<span class="inline-code">props.Children</span>
			, an attribute the struct does not name still arrives, and a caller may spread a map into it. Writing the props type down widens what the component may compose with; it does not change what the component sees.
		</p>
		<CodeBlock lang="gosx" source={data.legacySample} />
		<p>
			Legacy bodies can declare locals and use the established
			<span class="inline-code">Each</span>
			,
			<span class="inline-code">If</span>
			, and
			<span class="inline-code">Slot</span>
			structural builtins. Islands may instead use the strict component form when they stay within the supported island VM subset. Strict engine declarations and automatic
			<span class="inline-code">.gsx //gosx:engine</span>
			discovery remain preview/post-v1; programmatic
			<span class="inline-code">engine.Config</span>
			is the v1 engine surface.
		</p>
		<h2 id="attributes">Elements and attributes</h2>
		<p>
			Lowercase tags create HTML elements. Static quoted attributes, boolean attributes, expression attributes, expression children, and fragments are available in both styles within their validation contracts. Strict capitalized tags resolve only to same-file strict components, plus the
			<span class="inline-code">If</span>
			and
			<span class="inline-code">Each</span>
			builtins covered above; every other renderer builtin and element spread attributes remain legacy-only.
		</p>
		<CodeBlock lang="gosx" source={data.attributesSample} />
		<p>
			A strict component call is intentionally narrower than an HTML element: pass named attributes that match the props struct, or the single proven spread described above.
		</p>
		<h2 id="children">Children</h2>
		<p>
			A strict component places the markup its caller wrote. Write
			<span class="inline-code">{"{children}"}</span>
			in the body, and pass child content at the call site. All three call shapes accept children: a strict caller's named attributes, a strict caller's single spread, and a legacy caller's single spread.
		</p>
		<p>
			A typed legacy callee answers the same rule, because a strict body may call one. Its body places children through the same
			<span class="inline-code">{"{children}"}</span>
			hole. That hole is separate from the older
			<span class="inline-code">props.Children</span>
			channel a legacy body may also read: the channel carries the children whether or not the props struct declares the field, and it is unchanged.
		</p>
		<p>
			Children are not a prop. They never enter the props schema, no boundary proof reads them, and the explicit-supply rule does not cover them. They arrive as one already-rendered node: the caller rendered them, in the caller's scope, so every element and every props read inside them already passed the caller's own compile, check, and render proofs. Nothing is deferred across the call.
		</p>
		<p>
			The one operation on children is emission. A body may write
			<span class="inline-code">{"{children}"}</span>
			more than once, and each hole emits the markup again. Every other use stays rejected: a field read, a concatenation operand, an
			<span class="inline-code">If</span>
			condition, an
			<span class="inline-code">Each</span>
			source, an
			<span class="inline-code">Each</span>
			binding named children, and an attribute value. An attribute value is written inside quotes, so rendered markup there would produce broken HTML.
		</p>
		<p>
			A component that renders no children rejects child content with a diagnostic that names both remedies, rather than dropping the content silently.
		</p>
		<h2 id="island-composition">Composing inside an island</h2>
		<p>
			An island may call a same-file strict pure-view component. GoSX expands each call into the parent island program at compile time, so the browser still receives one hydration root and one VM—not a client component runtime. Direct typed scalar props, caller children, and named slots are supported. Projected child markup remains in the caller's scope, so it may read the parent island's signals and bind its handlers.
		</p>
		<p>
			Keep state and behavior in the parent island. A v1 pure-view callee cannot be another island, an engine, a legacy or imported component; own signals, computed values, handlers, or effects; accept handler-valued props; read nested or slice props; or use spreads. Recursive composition fails with its cycle path, depth is capped at 32 components, and the expanded program keeps the VM's 65,535-node and expression limits.
		</p>
		<p>
			Do not declare
			<span class="inline-code">{"{children}"}</span>
			or a named-slot hole on the root island itself. A root island program is compiled per component, before a server call site can supply content. Put a slot-bearing pure-view component inside the island instead.
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
		<h2 id="choosing">When legacy is still necessary</h2>
		<p>
			Declare every new server component with
			<span class="inline-code">component Name(props: Type)</span>
			. The legacy Go-function form remains necessary in two cases only, both structural limits of the current release, not a style choice.
		</p>
		<ul>
			<li>
				A route reads loader data, route params, structural control flow, or a helper-rich body that the strict expression subset does not cover yet.
			</li>
			<li>
				A component runs as an engine (canvas, WebGL, WebGPU, or a background worker). Strict engines are not supported yet.
			</li>
		</ul>
		<p>
			A strict component may hydrate client-side as an island when its body stays within the supported island VM subset. Project-aware checks fail closed when an island uses an unsupported expression or client operation.
		</p>
	</article>
}
