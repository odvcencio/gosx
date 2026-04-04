package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Internals</span>
			<p class="lede">
				GoSX compiles
				<span class="inline-code">.gsx</span>
				source through a tree-sitter parse, IR lowering, and validation before render — all in a single Go process with no external toolchain.
			</p>
		</div>
		<h1 id="gsx-syntax">GSX Syntax</h1>
		<p>
			GSX is a superset of Go. Every
			<span class="inline-code">.gsx</span>
			file is a valid Go source file with additional JSX-style element and expression syntax. Functions return
			<span class="inline-code">Node</span>
			, a type alias over the GoSX virtual DOM. Attributes, text children, expression holes, control flow, and component calls all parse in a single pass.
		</p>
		<CodeBlock lang="gosx" source={data.sampleSyntax} />
		<section class="feature-grid">
			<div class="card">
				<strong>JSX-like elements</strong>
				<p>
					Lowercase tags emit HTML elements. Capitalised tags are component calls resolved at compile time through the binding registry.
				</p>
			</div>
			<div class="card">
				<strong>Expression holes</strong>
				<p>
					Curly-brace holes
					<span class="inline-code">{`{expr}`}</span>
					accept any Go expression. The compiler validates the hole against the island subset for reactive pages.
				</p>
			</div>
			<div class="card">
				<strong>Control flow</strong>
				<p>
					<span class="inline-code">Each</span>
					,
					<span class="inline-code">If</span>
					, and
					<span class="inline-code">Slot</span>
					are built-in structural elements handled during IR lowering, not dispatched as regular components.
				</p>
			</div>
			<div class="card">
				<strong>Spread props</strong>
				<p>
					<span class="inline-code">{`{...data.props}`}</span>
					on any element or component expands a map or struct into individual attributes.
				</p>
			</div>
		</section>
		<h2 id="parsing">Parsing</h2>
		<p>
			GoSX uses
			<a href="https://github.com/odvcencio/gotreesitter" class="inline-link">gotreesitter</a>
			— a pure-Go binding to the tree-sitter runtime — to parse
			<span class="inline-code">.gsx</span>
			source into a concrete syntax tree. The GSX grammar extends the Go grammar with JSX element productions. Parsing is incremental: only changed nodes in the CST are reparsed during development.
		</p>
		<p>
			The grammar lives in the GoSX module as a generated Go source file. No C compiler is required; the grammar is compiled offline and shipped as a Go byte slice decoded at startup.
		</p>
		<CodeBlock lang="go" source={data.sampleCompile} />
		<h2 id="ir-lowering">IR Lowering</h2>
		<p>
			The lowering pass walks the CST and emits a flat instruction array — the GoSX IR. Each instruction is one of a small set of opcodes:
			<span class="inline-code">PushElement</span>
			,
			<span class="inline-code">PopElement</span>
			,
			<span class="inline-code">SetAttr</span>
			,
			<span class="inline-code">PushText</span>
			,
			<span class="inline-code">PushExpr</span>
			,
			<span class="inline-code">CallComponent</span>
			, and a handful of control-flow markers. The flat-array layout is cache-friendly and avoids allocating an AST node per HTML element.
		</p>
		<CodeBlock lang="go" source={data.sampleIR} />
		<p>
			Lowering is a single linear scan; there is no recursive descent after parsing. Component calls are resolved against the binding registry at render time, not at lowering time, which keeps the IR portable across different page contexts.
		</p>
		<h2 id="validation">Validation</h2>
		<p>
			After lowering, the validator checks constraints that the grammar cannot express. For normal server pages the validation is minimal: well-formed nesting, required attributes on structural nodes, and no raw
			<span class="inline-code">script</span>
			or
			<span class="inline-code">style</span>
			elements in island scope.
		</p>
		<p>
			Island pages enforce an additional subset rule: every expression hole inside an
			<span class="inline-code">Island</span>
			boundary must be a valid island expression — no arbitrary Go, only the signal-aware expression language understood by the browser VM.
		</p>
		<section class="callout">
			<strong>Island subset enforcement</strong>
			<p>
				The compiler rejects island templates that reference Go constructs outside the expression subset: function calls beyond the allowed builtins, type assertions, goroutines, and channel operations are all parse errors inside island scope.
			</p>
		</section>
		<h2 id="expression-evaluation">Expression Evaluation</h2>
		<p>
			Server-side expressions in curly-brace holes are evaluated against a
			<span class="inline-code">map[string]any</span>
			data context provided by the route loader. Dot-path resolution, method calls, and simple binary operations are handled by a lightweight evaluator — no
			<span class="inline-code">reflect</span>
			hot paths for the common map-key case.
		</p>
		<CodeBlock lang="go" source={data.sampleEval} />
		<h2 id="island-compilation">Island Compilation</h2>
		<p>
			When an
			<span class="inline-code">Island</span>
			element is encountered during lowering, its expression holes are re-parsed by the island compiler into VM opcodes. These opcodes are serialised into a compact binary format embedded in the rendered HTML as a
			<span class="inline-code">data-island</span>
			attribute. The browser VM deserialises the opcode stream and re-evaluates expressions live against signal values — no JavaScript source is shipped, only the opcode bytes.
		</p>
		<CodeBlock lang="gosx" source={data.sampleIslandGSX} />
		<CodeBlock lang="go" source={data.sampleIslandOps} />
		<p>
			Island compilation is idempotent: the same
			<span class="inline-code">.gsx</span>
			source always produces the same opcode bytes for a given signal schema. This makes island output cacheable at the edge without per-request recompilation.
		</p>
		<section class="callout">
			<strong>Contributor note</strong>
			<p>
				The compiler pipeline is intentionally narrow. Adding a new expression form requires changes in three places: the tree-sitter grammar, the IR lowering switch, and the island compiler if the form is island-safe. The validator will catch any mismatch before a test run completes.
			</p>
		</section>
	</article>
}
