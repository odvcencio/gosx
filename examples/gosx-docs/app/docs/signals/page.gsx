package docs

func Page() Node {
	return <div>
		<section id="signal-basics">
			<h2 class="chrome-text">Signal Basics</h2>
			<p>
				A signal is a reactive cell — a value with a subscriber list. When the value changes, every subscriber is notified and re-evaluates. Signals are the atomic unit of reactivity in GoSX; there is no framework-owned state tree, no context providers, and no prop threading.
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/signal"
	
	// New creates a writable signal with an initial value.
	count := signal.New(0)
	
	// Get reads the current value.
	n := count.Get() // 0
	
	// Set writes a new value and notifies subscribers.
	count.Set(n + 1) // subscribers re-run
	
	// Peek reads without registering a dependency.
	// Safe to call from non-reactive contexts.
	n = count.Peek()`)}
			<p>
				Signals are typed. The type parameter is inferred from the initial value, so
				<span class="inline-code">signal.New(0)</span>
				produces a
				<span class="inline-code">signal.Signal[int]</span>
				and
				<span class="inline-code">signal.New("")</span>
				produces a
				<span class="inline-code">signal.Signal[string]</span>
				. The type is enforced at compile time — no interface boxing on the hot path.
			</p>
			{CodeBlock("go", `// Typed signals.
	active := signal.New(false)       // Signal[bool]
	label  := signal.New("idle")      // Signal[string]
	ratio  := signal.New(0.0)         // Signal[float64]
	items  := signal.New([]string{})  // Signal[[]string]
	
	// ReadOnly wraps a signal to prevent external writes.
	var ro signal.ReadOnly[bool] = active.ReadOnly()`)}
		</section>
		<section id="computed-values">
			<h2 class="chrome-text">Computed Values</h2>
			<p>
				A computed value derives from one or more signals. It re-evaluates lazily when any dependency changes and caches its result until the next change. Computeds are themselves read-only signals, so they can be used anywhere a signal value is expected.
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
	})
	
	// Read the derived value — never stale.
	shown := visible.Get()`)}
			<p>
				Dependencies are tracked automatically. The computed function runs inside a tracking scope. Every
				<span class="inline-code">Get()</span>
				call during that run registers the caller as a subscriber of that signal. No dependency array, no manual
				<span class="inline-code">useMemo</span>
				list.
			</p>
			<div class="signals-graph glass-panel" aria-label="Dependency graph example">
				<div class="signals-graph__row">
					<span class="signals-graph__node signals-graph__node--source">items</span>
					<span class="signals-graph__arrow" aria-hidden="true">&#8594;</span>
					<span class="signals-graph__node signals-graph__node--computed">visible</span>
					<span class="signals-graph__arrow" aria-hidden="true">&#8594;</span>
					<span class="signals-graph__node signals-graph__node--dom">DOM</span>
				</div>
				<div class="signals-graph__row">
					<span class="signals-graph__node signals-graph__node--source">filter</span>
					<span class="signals-graph__arrow" aria-hidden="true">&#8599;</span>
				</div>
			</div>
		</section>
		<section id="effects">
			<h2 class="chrome-text">Effects</h2>
			<p>
				An effect is a side-effecting function that runs whenever its signal dependencies change. Effects are scheduled after all signal writes in a batch have propagated, so they always see a consistent state snapshot.
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/signal"
	
	count := signal.New(0)
	
	// Effect runs immediately once, then again on each change.
	stop := signal.Effect(func() {
	    fmt.Printf("count is now %d\n", count.Get())
	})
	
	count.Set(1) // prints: count is now 1
	count.Set(2) // prints: count is now 2
	
	// Call stop to unsubscribe the effect.
	stop()`)}
			<p>
				Effects are primarily useful in server-side lifecycle code and in engine programs. In island templates, DOM side effects are handled directly by the VM's attribute and text-content opcodes — you rarely need to write an explicit effect for UI updates.
			</p>
			<section class="callout">
				<strong>Avoid circular effects</strong>
				<p>
					An effect that writes to a signal it also reads will loop. The runtime detects and breaks cycles after one iteration, but the pattern is a design smell. Use computed values when you need a derived write — they are evaluated lazily and do not re-trigger themselves.
				</p>
			</section>
		</section>
		<section id="dependency-tracking">
			<h2 class="chrome-text">Dependency Tracking</h2>
			<p>
				GoSX uses push-pull dependency tracking. Signals push change notifications to their subscribers. Computed values and effects pull their inputs when they re-evaluate, rebuilding the dependency edge set on each run. This means conditional dependencies work correctly:
			</p>
			{CodeBlock("go", `showDetails := signal.New(false)
	details     := signal.New("...")
	
	// This computed only depends on details when showDetails is true.
	// If showDetails is false, changes to details do NOT trigger a re-run.
	summary := signal.Computed(func() string {
	    if !showDetails.Get() {
	        return "hidden"
	    }
	    return details.Get()
	})`)}
			<p>
				The dependency graph is rebuilt from scratch on each evaluation. There is no bookkeeping required when you add or remove a branch — the tracking is automatic and correct for any control flow shape.
			</p>
			<div class="feature-grid">
				<div class="card">
					<strong>Automatic</strong>
					<p>
						No dependency arrays. No
						<span class="inline-code">watch()</span>
						calls. Just read signals inside a reactive scope.
					</p>
				</div>
				<div class="card">
					<strong>Precise</strong>
					<p>
						Only computeds and effects that actually read a changed signal are re-run.
					</p>
				</div>
				<div class="card">
					<strong>Conditional</strong>
					<p>
						Branches inside a computed are tracked correctly — unreachable signals are not subscribed.
					</p>
				</div>
				<div class="card">
					<strong>Cycle-safe</strong>
					<p>
						The runtime detects write-then-read cycles and stops them after one iteration.
					</p>
				</div>
			</div>
		</section>
		<section id="batch-updates">
			<h2 class="chrome-text">Batch Updates</h2>
			<p>
				When multiple signals change together — for example, when updating a form with several fields — you want all computeds and effects to re-run once, not once per signal write.
				<span class="inline-code">signal.Batch</span>
				groups writes into a single propagation pass:
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/signal"
	
	firstName := signal.New("")
	lastName  := signal.New("")
	
	// fullName would re-run twice without batching.
	fullName := signal.Computed(func() string {
	    return firstName.Get() + " " + lastName.Get()
	})
	
	// Batch: fullName re-runs exactly once.
	signal.Batch(func() {
	    firstName.Set("Ada")
	    lastName.Set("Lovelace")
	})`)}
			<p>
				Batching is recursive — nested
				<span class="inline-code">signal.Batch</span>
				calls are merged into the outer batch. Propagation fires only when the outermost batch exits.
			</p>
			<p>
				The island VM applies batching automatically for event handlers. When a
				<span class="inline-code">data-on-click</span>
				expression writes multiple signals, the DOM is updated once after the entire expression completes.
			</p>
		</section>
		<section id="signal-store">
			<h2 class="chrome-text">Signal Store</h2>
			<p>
				For pages with many related signals, group them into a store — a plain struct with signal fields. A store is not a framework concept; it is a Go struct. Pass it to the template via the route loader's data map.
			</p>
			{CodeBlock("go", `// Define a store as a plain struct.
	type CartStore struct {
	    Items    signal.Signal[[]CartItem]
	    Discount signal.Signal[float64]
	    Total    signal.Computed[float64]
	}
	
	func NewCartStore() *CartStore {
	    items    := signal.New([]CartItem{})
	    discount := signal.New(0.0)
	    total    := signal.Computed(func() float64 {
	        t := 0.0
	        for _, item := range items.Get() {
	            t += item.Price * float64(item.Qty)
	        }
	        return t * (1 - discount.Get())
	    })
	    return &CartStore{Items: items, Discount: discount, Total: total}
	}
	
	// In the route loader:
	func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    cart := NewCartStore()
	    return map[string]any{"cart": cart}, nil
	}`)}
			<p>
				Island templates access store fields by dotted path:
				<span class="inline-code">{`{cart.Total}`}</span>
				in island markup resolves the computed value and subscribes the island program to its output. Updates to
				<span class="inline-code">cart.Items</span>
				or
				<span class="inline-code">cart.Discount</span>
				propagate through the computed and into the DOM automatically.
			</p>
		</section>
	</div>
}
