package collections

import (
	"m31labs.dev/gosx/browser"
	"m31labs.dev/gosx/signal"
)

type LiveCollectionProps struct {
	Initial string
}

//gosx:island
component LiveCollection(props: LiveCollectionProps) {
	items := signal.New([]string{props.Initial})
	draft := signal.New("")
	edit := func() { draft.Set(value) }
	add := func() {
		browser.PreventDefault()
		items.Set(append(items.Get(), draft.Get()))
		draft.Set("")
		browser.Focus("#island-collection-draft")
	}
	clear := func() { items.Set([]string{}); browser.Focus("#island-collection-draft") }
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
		<p class="lede">Grow and clear a collection with Go slice values, reactive signals, and ordinary form controls.</p>
		<section id="collection-example" class="docs-live-example" aria-label="Collection island example">
			<h2>Working collection</h2>
			<LiveCollection {...data.collectionProps} />
			<a
				href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/islands/collections/page.gsx"
				rel="noopener"
			>View the collection source</a>
		</section>
		<p>Use append(items.Get(), item) and Set to grow a list. Each renders the updated slice. Several individual elements may be appended in one call, and a call with only the slice preserves its value. The VM creates a fresh slice for additions, keeping earlier signal values independent.</p>
		<p>This intrinsic supports individual elements. Variadic slice expansion with ... remains outside the island expression subset. The example uses the shared VM and introduces no browser feature module or runtime opcode.</p>
		<h2 id="collection-source">Source</h2>
		{CodeBlock("gosx", data.collectionSample)}
		<a href="/docs/islands/">Island reference</a>
	</article>
}
