package docs

func Page() Node {
	return <div>
		<section id="what-are-islands">
			<h2 class="chrome-text">What Are Islands</h2>
			<p>
				An island is a reactive DOM region. The server renders the full HTML document — including the island's initial markup — and ships it as static HTML. When the page boots, the shared WASM runtime discovers each island by its
				<span class="inline-code">data-island</span>
				attribute and hydrates it with a compiled expression program.
			</p>
			<p>
				Islands are opt-in. Pages with no reactive requirements ship as pure HTML with zero JavaScript. Only pages that declare islands pull in the shared runtime, and that runtime is loaded once and reused across every navigation.
			</p>
			<div class="islands-arch glass-panel">
				<div class="islands-arch__col">
					<strong>Server</strong>
					<p>
						Renders HTML, compiles island programs to opcodes, embeds both in the document.
					</p>
				</div>
				<div class="islands-arch__divider" aria-hidden="true">&#8594;</div>
				<div class="islands-arch__col">
					<strong>Browser</strong>
					<p>
						Boots the shared WASM VM, deserialises opcode programs, binds signals to DOM nodes.
					</p>
				</div>
			</div>
			<p>
				The architecture ensures that the page is always readable — the server HTML is the real content, not a placeholder. Islands add reactivity without owning the render.
			</p>
		</section>
		<section id="island-programs">
			<h2 class="chrome-text">Island Programs</h2>
			<p>
				Each island boundary in a
				<span class="inline-code">.gsx</span>
				template is compiled into a program — a compact array of opcodes that the browser VM executes to keep the DOM in sync with signal values. The opcodes are serialised into a binary format and embedded as a
				<span class="inline-code">data-island</span>
				attribute on the island root element.
			</p>
			{CodeBlock("gosx", `func Counter() Node {
	    return <Island>
	        <button
	            class={if data.active { "btn btn--active" } else { "btn" }}
	            data-on-click="count++"
	        >
	            {data.count}
	        </button>
	    </Island>
	}`)}
			<p>
				The compiler validates that every expression inside an
				<span class="inline-code">Island</span>
				boundary uses only the island expression subset — no arbitrary Go, only signal-aware paths, comparisons, string concatenation, and conditional forms. Violations are caught at compile time, not at runtime.
			</p>
			{CodeBlock("go", `// Island VM opcodes (simplified).
	const (
	    IslandOpPushSignal   = 0x01 // push named signal value onto stack
	    IslandOpPushLiteral  = 0x02 // push string literal onto stack
	    IslandOpCondStr      = 0x10 // conditional: pop cond, push one of two strings
	    IslandOpConcat       = 0x11 // pop two strings, push concatenated result
	    IslandOpEq           = 0x30 // pop two values, push bool
	    IslandOpSetAttr      = 0x40 // pop value, set attribute on bound DOM node
	    IslandOpSetText      = 0x41 // pop value, set text content of bound DOM node
	)`)}
		</section>
		<section id="expression-vm">
			<h2 class="chrome-text">Expression VM</h2>
			<p>
				The browser VM is a stack machine implemented in WASM and compiled from Go. It holds a single evaluation stack, a signal table, and a DOM binding map. On each signal change the VM re-evaluates only the programs that reference the changed signal — there is no virtual DOM diffing, no framework reconciler, and no JavaScript source shipped to the browser.
			</p>
			<div class="feature-grid">
				<div class="card">
					<strong>Stack machine</strong>
					<p>
						Simple push/pop opcodes — no heap allocations per update cycle.
					</p>
				</div>
				<div class="card">
					<strong>Signal-scoped re-evaluation</strong>
					<p>
						Only programs with a dependency on the changed signal are re-run.
					</p>
				</div>
				<div class="card">
					<strong>Direct DOM writes</strong>
					<p>
						Opcode results are written directly to DOM nodes — no intermediate VDOM.
					</p>
				</div>
				<div class="card">
					<strong>No JS source in the wire</strong>
					<p>
						Opcodes are binary. The browser receives data, not executable JavaScript.
					</p>
				</div>
			</div>
			<p>
				The VM is shared across all islands on a page. A single WASM instance handles every island program; there is no per-island JS bundle. The signal table is also shared, which is what makes cross-island synchronisation possible without any additional wiring.
			</p>
		</section>
		<section id="shared-signals">
			<h2 class="chrome-text">Shared Signals</h2>
			<p>
				Signals are reactive state cells. Each signal has a typed value and a subscriber list. When the value changes, all subscribers are notified and re-evaluate their programs. In Go, the
				<span class="inline-code">signal</span>
				package provides the signal types:
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/signal"
	
	// A writable signal with an initial value.
	count := signal.New(0)
	
	// Read the current value.
	n := count.Get()
	
	// Write a new value — notifies all subscribers.
	count.Set(n + 1)
	
	// A read-only view of a signal.
	var ro signal.ReadOnly[int] = count.ReadOnly()`)}
			<p>
				Signals declared with a
				<span class="inline-code">$</span>
				prefix in island markup are promoted to the shared signal table. Any island on the same page that references the same name reads from the same cell, so a write in one island is immediately visible to all others.
			</p>
			{CodeBlock("gosx", `// Both islands reference the same shared signal "$theme".
	func ThemeToggle() Node {
	    return <Island>
	        <button data-on-click="$theme = $theme == 'dark' ? 'light' : 'dark'">
	            Toggle theme
	        </button>
	    </Island>
	}
	
	func ThemeLabel() Node {
	    return <Island>
	        <span class={if $theme == "dark" { "label label--dark" } else { "label" }}>
	            {$theme}
	        </span>
	    </Island>
	}`)}
		</section>
		<section id="cross-island-sync">
			<h2 class="chrome-text">Cross-Island Sync</h2>
			<p>
				Because all island programs run in the same WASM VM instance with the same shared signal table, cross-island reactivity is automatic. No event bus, no global store, no prop drilling. A signal write in one island propagates to every other island that reads the same signal within the same microtask.
			</p>
			<p>
				Computed signals derive their value from one or more source signals. They are re-evaluated lazily when any dependency changes:
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/signal"
	
	items  := signal.New([]string{"apple", "banana", "cherry"})
	filter := signal.New("")
	
	// Computed re-runs whenever items or filter changes.
	visible := signal.Computed(func() []string {
	    f := filter.Get()
	    if f == "" {
	        return items.Get()
	    }
	    var out []string
	    for _, item := range items.Get() {
	        if strings.Contains(item, f) {
	            out = append(out, item)
	        }
	    }
	    return out
	})`)}
			<p>
				Computed signals are also shareable across islands. Declare the computed in the route loader, pass it to the template data, and reference it by name in island markup. The VM will subscribe to the computed's output, not to its inputs directly.
			</p>
		</section>
		<section id="hydration">
			<h2 class="chrome-text">Hydration</h2>
			<p>
				Island hydration is the process of attaching the WASM VM to a server-rendered DOM. It does not re-render. The server has already produced the correct initial HTML. Hydration only:
			</p>
			<ul>
				<li>
					Deserialises the opcode program from the
					<span class="inline-code">data-island</span>
					attribute.
				</li>
				<li>
					Walks the island's DOM to locate nodes that programs write to.
				</li>
				<li>
					Registers signal subscriptions so future writes trigger re-evaluation.
				</li>
				<li>
					Registers event handlers declared with
					<span class="inline-code">data-on-*</span>
					attributes.
				</li>
			</ul>
			<p>
				Hydration is batched into the shared bootstrap hook that runs once after the page shell is ready. Islands that are below the fold are hydrated progressively as the user scrolls, using
				<span class="inline-code">IntersectionObserver</span>
				to defer work until needed.
			</p>
			{CodeBlock("go", `// Route loader: attach signal values to template data.
	func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    count := signal.New(0)
	    theme := signal.New("dark")
	
	    return map[string]any{
	        "count": count,
	        "theme": theme,
	    }, nil
	}`)}
			<section class="callout">
				<strong>No hydration mismatch</strong>
				<p>
					Because the server and VM use the same expression compiler, the initial HTML always matches what the VM would produce for the same signal values. There is no hydration mismatch class of bug in GoSX islands.
				</p>
			</section>
		</section>
	</div>
}
