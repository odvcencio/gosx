package docs

func Page() Node {
	return <article class="prose">
		<section class="doc-scene" aria-labelledby={docScene.HeadingID}>
			<div id={docScene.SurfaceID} class="doc-scene__surface">
				<Scene3D class="doc-scene__mount" {...docScene.Scene} respectReducedMotion={true}>
					<div class="doc-scene__fallback">{docScene.Scene.UnsupportedMessage}</div>
				</Scene3D>
			</div>
			<div class="doc-scene__teaching">
				<p class="doc-scene__eyebrow">{docScene.Eyebrow}</p>
				<p id={docScene.HeadingID} class="doc-scene__title" role="heading" aria-level="2">{docScene.Title}</p>
				<p class="doc-scene__summary">{docScene.Summary}</p>
				<dl class="doc-scene__facts">
					<div>
						<dt>Backend contract</dt>
						<dd>{docScene.BackendTruth}</dd>
					</div>
					<div>
						<dt>Interaction</dt>
						<dd>{docScene.InteractionHint}</dd>
					</div>
				</dl>
				<a href={docScene.DemoHref} data-gosx-link="true" class="doc-scene__link">{docScene.DemoLabel}</a>
			</div>
		</section>
		<div class="page-topper">
			<span class="eyebrow">Language pipeline</span>
			<p class="lede">
				A single parser and IR serve both GSX component spellings. Strict declarations add a fail-closed typed boundary and are the only spelling GoSX teaches. The legacy Go-function form still parses for existing code, islands, engines, and loader-bound routes.
			</p>
		</div>
		<h2 id="source-model">GSX source model</h2>
		<p>
			A
			<span class="inline-code">.gsx</span>
			file begins with a Go package declaration and can contain Go type declarations alongside GSX components. The grammar supports both strict
			<span class="inline-code">component</span>
			declarations and legacy functions returning
			<span class="inline-code">Node</span>
			.
		</p>
		<CodeBlock lang="gosx" source={data.strictSample} />
		<CodeBlock lang="gosx" source={data.legacySample} />
		<p>
			The two spellings lower into the same component and node representation. Existing legacy source remains valid, but new code should declare
			<span class="inline-code">component Name(props: Type)</span>
			instead. Calls stay within one declaration style in v0.39, keeping dynamic legacy attributes from bypassing strict Go prop checks.
		</p>
		<h2 id="strict-validation">Strict validation</h2>
		<p>
			Strict server components are intentionally narrower than arbitrary Go. They accept zero props or one typed parameter named
			<span class="inline-code">props</span>
			, then exactly one top-level GSX return. This shape keeps what the checker accepts aligned with what the server renderer can execute.
		</p>
		<p>
			Each expression hole or expression attribute may use a quoted string,
			<span class="inline-code">true</span>
			or
			<span class="inline-code">false</span>
			, an ungrouped non-negative base-10 integer in the
			<span class="inline-code">int64</span>
			range, a finite ungrouped decimal float, or one direct, parity-safe built-in scalar field on props. Nested selectors, indexing, calls, unary and binary operators, local declarations, ordinary
			<span class="inline-code">if</span>
			statements, helper or package calls, and cross-file component calls are rejected for strict server components in v0.39.
		</p>
		<section class="callout">
			<strong>Fail closed by design</strong>
			<p>
				A Go expression may be type-correct yet unavailable to the IR renderer. Strict validation rejects that gap instead of compiling source that can only fail or render differently later.
			</p>
		</section>
		<h2 id="pipeline">Parse, lower, validate</h2>
		<ol>
			<li>
				The embedded pure-Go tree-sitter grammar parses Go plus GSX markup into a concrete syntax tree.
			</li>
			<li>
				The lowerer records package/import information, components, and a flat node array with index-based child references.
			</li>
			<li>
				Validators apply component-shape, renderer-expression, island, and engine constraints for the source surface in use.
			</li>
			<li>
				Project commands add package context where sibling Go types or build inputs matter.
			</li>
		</ol>
		<CodeBlock lang="go" source={data.programSample} />
		<p>
			The public Go API
			<span class="inline-code">gosx.Compile(source)</span>
			returns this IR program. The CLI command
			<span class="inline-code">gosx compile</span>
			is different: it emits transpiled Go source for build workflows.
		</p>
		<h2 id="expressions">Server expressions</h2>
		<p>
			Legacy file routes evaluate the established binding language against values such as
			<span class="inline-code">data</span>
			,
			<span class="inline-code">params</span>
			, and registered template bindings. Strict components instead resolve their typed
			<span class="inline-code">props</span>
			. These are distinct contracts even though both become expression nodes in IR.
		</p>
		<p>
			Markup support is renderer-specific. A syntax node appearing in the grammar does not by itself promise that every Go statement or expression can execute in server IR or in the browser island VM.
		</p>
		<h2 id="islands">Island lowering</h2>
		<p>
			An island is marked by
			<span class="inline-code">//gosx:island</span>
			. In v0.39, island and engine directives use the legacy Go-function component style. Their recognized signal, computed, and handler declarations are lowered with the returned markup into a compact program for the shared browser VM; applying a client directive to a strict declaration fails closed.
		</p>
		<CodeBlock lang="gosx" source={data.islandSample} />
		<p>
			Island expressions use their own constrained instruction set. GoSX validates and encodes the program as a runtime asset; it does not ship arbitrary Go source for evaluation in the browser.
		</p>
		<h2 id="commands">Authoritative commands</h2>
		<CodeBlock lang="bash" source={data.commandsSample} />
		<ul>
			<li>
				<span class="inline-code">check</span>
				parses and validates a GSX file, including its strict component contract.
			</li>
			<li>
				<span class="inline-code">render</span>
				exercises the server IR renderer and prints component HTML.
			</li>
			<li>
				<span class="inline-code">compile</span>
				emits Go source used by compilation workflows.
			</li>
			<li>
				<span class="inline-code">build</span>
				and
				<span class="inline-code">dev</span>
				validate the application in project context.
			</li>
			<li>
				<span class="inline-code">export</span>
				validates strict packages before writing static output.
			</li>
		</ul>
		<p>
			Use
			<span class="inline-code">gosx fmt</span>
			for source formatting. Its
			<span class="inline-code">--check</span>
			mode is suitable for CI.
		</p>
		<h2 id="lsp">LSP boundary</h2>
		<p>
			The GoSX LSP provides context-free syntax diagnostics, document symbols, completion, hover, formatting, and semantic tokens. It does not currently replace package-aware CLI validation for strict prop types or cross-file build context.
		</p>
	</article>
}
