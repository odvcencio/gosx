package main

// chrome.gsx holds the static prose and card headings for the two island
// routes (/counter, /kitchen-sink). Those routes stay hand-written Go — see
// the comment on CounterPage and KitchenSinkPage in main.go for why — but
// everything in this file is ordinary markup with no per-request data, so it
// renders through route.RenderProgramComponent (gosx#226) instead of El.
//
// Card is the exception: its content — a header from this file, plus a
// Go-computed island node and, for the counter card, a no-JS fallback — is
// only known at request time. Before gosx#246 gave strict components a
// {children} hole, and RenderProgramComponentNode a way to splice a
// Go-computed node into it, the card div itself had to be a gosx.El call in
// main.go (see chromeCard in chrome.go). Card itself is left exactly as it
// was: adding a second hole to it and letting gosx fmt reformat its body
// onto separate lines would put a stray whitespace-only text node around
// every existing chromeCard call's content (ir/lower.go's lowerText
// collapses one to a space) — a real behavior change for markup this
// conversion has no reason to touch.
//
// HeaderCard is CounterHowItWorksCard's own shell instead (gosx#249): a
// caller-side slot="Header" attribute on a direct child, filled through a
// same-file, same-program nested <HeaderCard> call — no
// RenderProgramComponent anywhere in that path — because
// CounterHowItWorksCard's whole heading-plus-steps body is static prose
// with no per-request data. See the CHANGELOG entry for this change for
// the rendered-bytes proof this conversion is checked against.

component CounterIntro() {
	return <h1>Counter (Island Demo)</h1>
}

component Card() {
	return <div class="card">{children}</div>
}

component HeaderCard() {
	return <div class="card">
		{slotHeader}
		{children}
	</div>
}

component CounterCardHeader() {
	return <>
		<h3>Interactive Island</h3>
		<p>
			This counter is compiled from counter.gsx and hydrated via WASM.
		</p>
		<br />
	</>
}

component CounterHowItWorksCard() {
	return <HeaderCard>
		<h3 slot="Header">How It Works</h3>
		<p>
			1. counter.gsx is compiled to an IslandProgram at server startup
		</p>
		<p>
			2. Server renders the counter HTML with data-gosx-handler attributes
		</p>
		<p>
			3. Bootstrap loads the shared WASM runtime and fetches the IslandProgram
		</p>
		<p>
			4. Event delegation catches clicks and dispatches to the VM
		</p>
		<p>
			5. Signal updates trigger reconciliation and DOM patching
		</p>
	</HeaderCard>
}

component KitchenSinkIntro() {
	return <>
		<h1>Kitchen Sink — SPA Patterns</h1>
		<p>
			Every pattern below is a GoSX island: server-rendered HTML hydrated with a shared WASM runtime. Click to interact — no page reloads.
		</p>
	</>
}

component KSCounterHeader() {
	return <>
		<h2>Counter</h2>
		<p>Signal-driven increment/decrement.</p>
	</>
}

component KSTabsHeader() {
	return <>
		<h2>Tabs</h2>
		<p>
			Conditional rendering via OpCond with dynamic CSS class toggling on active tab.
		</p>
	</>
}

component KSToggleHeader() {
	return <>
		<h2>Toggle</h2>
		<p>
			Boolean signal with show/hide. Click or press Enter to toggle (keyboard handler).
		</p>
	</>
}

component KSTodoHeader() {
	return <>
		<h2>Todo List</h2>
		<p>String concatenation for list items.</p>
	</>
}

component KSFormHeader() {
	return <>
		<h2>Form Validation</h2>
		<p>
			Two-way input binding via OpEventGet. Type in the input to see live updates.
		</p>
	</>
}

component KSPriceHeader() {
	return <>
		<h2>Price Calculator</h2>
		<p>
			Derived values: total = price x qty - discount.
		</p>
	</>
}

component KSListHeader() {
	return <>
		<h2>Dynamic List</h2>
		<p>
			Add/remove items from a list. Items stored as comma-separated string, count tracked separately.
		</p>
	</>
}

component KSEditorHeader() {
	return <>
		<h2>Code Editor</h2>
		<p>
			Overlay editor with WASM-powered Go syntax highlighting, line numbers, and live char count.
		</p>
	</>
}
