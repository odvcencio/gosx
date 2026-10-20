package collections

import (
	"m31labs.dev/gosx/browser"
	"m31labs.dev/gosx/signal"
)

type LiveCollectionProps struct {
	Initial []string
}

//gosx:island
component LiveCollection(props: LiveCollectionProps) {
	items := signal.New(props.Initial)
	draft := signal.New("")
	edit := func() { draft.Set(value) }
	focusDraft := func() { browser.Focus("#island-collection-draft") }
	add := func() {
		browser.PreventDefault()
		if draft.Get() == "" || len(items.Get()) >= 12 {
			return
		}
		items.Set(append(items.Get(), draft.Get()))
		draft.Set("")
		focusDraft()
	}
	clear := func() { items.Set([]string{}); focusDraft() }
	return <section aria-label="Interactive collection">
		<form onSubmit={add}>
			<label for="island-collection-draft">Add a building block</label>
			<input
				id="island-collection-draft"
				name="item"
				value={draft.Get()}
				onInput={edit}
				required
				maxlength="48"
				autocomplete="off"
			 />
			<button type="submit" disabled={draft.Get() == "" || len(items.Get()) >= 12}>Add</button>
			<button type="button" onClick={clear} disabled={len(items.Get()) == 0}>Clear</button>
		</form>
		<ul id="island-collection-items" aria-live="polite">
			<Each of={items.Get()} as="item"><li>{item}</li></Each>
		</ul>
		<p>{len(items.Get())} of 12 building blocks.</p>
	</section>
}


func Page() Node {
	return <article class="prose">
		<p class="lede">Load a typed Go slice from the server, then grow and clear it with reactive signals and ordinary form controls.</p>
		<section id="collection-example" class="docs-live-example" aria-label="Collection island example">
			<h2>Working collection</h2>
			<LiveCollection {...data.collectionProps} />
			<a
				href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/islands/collections/page.gsx"
				rel="noopener"
			>View the collection source</a>
		</section>
		<p>Use append(items.Get(), item) and Set to grow a list. Each renders the updated slice. Several individual elements may be appended in one call, and a call with only the slice preserves its value. The VM creates a fresh slice for additions, keeping earlier signal values independent.</p>
		<p>The loader supplies Initial as []string, and signal.New(props.Initial) keeps its element type through server rendering and hydration. Island props support scalar builtins, slices, string-keyed maps, and same-file records with explicitly named exported fields. The boundary checks types even for empty or nil collections, copies only declared client fields, and uses Go field names consistently. Recursive record types are supported; cyclic values and data deeper than 64 levels fail with an error.</p>
		<p>The handler uses a Go if guard and bare return to reject empty drafts and keep the collection within its limit, even when a submission bypasses the disabled button. Nested if, else if, and else branches use the same shared VM. Handler-local assignments, loops, if initializers, and return values remain outside this subset.</p>
		<p>Share behavior with named func() helpers inside the island. Add and Clear call the same focusDraft helper. Helpers share the current event and signal state; a bare return exits the helper while its caller continues. Calls must be standalone handler statements, and helpers cannot declare parameters or results. Rendering and signal initializers cannot call them.</p>
		<p>This intrinsic supports individual elements. Variadic slice expansion with ... remains outside the island expression subset. The example uses the shared VM and introduces no browser feature module or runtime opcode.</p>
		<h2 id="collection-source">Source</h2>
		{CodeBlock("gosx", data.collectionSample)}
		<a href="/docs/islands/">Island reference</a>
	</article>
}
