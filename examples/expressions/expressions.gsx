package expressions

// Showcase of the Phase 4 expression-language forms.
//
// This island is illustrative — it is not wired to a server.
// The forms below are accepted by the IR expression parser
// (see ir/exprparse_test.go and ir/closure_eval_test.go for
// the executable proofs).

type Item struct {
	ID     int
	Name   string
	Tag    string
	Active bool
}

type Catalog struct {
	Items []Item
	Query string
}

//gosx:island
func Expressions(props Catalog) Node {
	return <section class="expressions">
		// Length / array methods using closures
		<p>
			Total:
			{items.length}
		</p>
		<p>
			Active:
			{items.filter(func(i){ return i.active }).length}
		</p>
		// Chained filter + map with closures
		<ul>
			<Each of={items.filter(func(i){ return i.active }).map(func(i){ return i.name })} as="name">
				<li>{name}</li>
			</Each>
		</ul>
		// String methods on a search query
		<p>
			Normalized query:
			{query.trim().toLower()}
		</p>
		// Slice + append composition
		<p>
			First three:
			{items.slice(0, 3).map(func(i){ return i.name })}
		</p>
		// Predicate find — single match
		<p>
			Top tag:
			{items.find(func(i){ return i.tag.startsWith("hero") })}
		</p>
	</section>
}
