package main

// layout.gsx holds the document shell for /counter and /kitchen-sink, the
// two routes whose own page content stays hand-written Go (see
// CounterPage/KitchenSinkPage in main.go — they need real signals,
// handlers, and expression opcodes the island VM executes, which a
// strict .gsx body cannot express).
//
// Before named slots (gosx#249) this shell had to be a gosx.El call in
// main.go: a single {children} hole cannot address the shell's four
// distinct injection points — a per-route title and a per-request
// island-preload-hints node inside <head>, the page content inside <body>,
// and an island hydration manifest at the very end of <body>, after
// content the file renderer has not finished writing when <head> closes.
// Named slots give each one its own bare identifier: {slotTitle},
// {slotPreloadHints}, {children}, and {slotPageHead}. main.go's
// chromeLayout supplies all four in one RenderProgramComponent call, the
// same "content built first, then the layout wraps it" order
// LayoutFunc's own contract already guarantees for route.Route.Layout
// (route/filesystem.go's applyLayoutFuncs) — see chromeLayout's own doc
// comment for why that ordering makes slotPreloadHints and slotPageHead
// correct without a strict expression ever computing a value that depends
// on a sibling's render order.
//
// gosx fmt reformats this component's markup onto one line per element,
// which inserts a single collapsed space at some tag boundaries the
// pre-conversion hand-written HTML string did not have (ir/lower.go's
// lowerText collapses any whitespace-only text between tags to one space).
// The CHANGELOG entry for this change documents the exact, normalized
// (structure-only) diff against the pre-conversion output; this is a
// formatting-driven, semantically inert whitespace difference, not a
// document-structure change.
component Layout() {
	return <html>
		<head>
			<meta charset="utf-8" />
			<meta name="viewport" content="width=device-width, initial-scale=1" />
			<title>{slotTitle}</title>
			<link rel="stylesheet" href="/static/styles.css" />
			{slotPreloadHints}
		</head>
		<body>
			<div class="layout">
				<Sidebar />
				<main class="main">{children}</main>
			</div>
			{slotPageHead}
		</body>
	</html>
}

// Sidebar renders the navigation sidebar. It carries no per-request data,
// so Layout places it with a plain nested call — no slot needed.
component Sidebar() {
	return <aside class="sidebar">
		<h2>GoSX Dashboard</h2>
		<nav>
			<a href="/">Home</a>
			<a href="/users">Users</a>
			<a href="/users/new">New User</a>
			<a href="/counter">Counter</a>
			<a href="/kitchen-sink">Kitchen Sink</a>
			<a href="/settings">Settings</a>
		</nav>
	</aside>
}

// FooterProps carries the one per-request value the footer renders: the
// version-and-timestamp line main.go's chromeFooter computes with
// time.Now(), which a strict .gsx expression cannot call. This is a proved
// prop, not a slot — the value is scalar data, not markup a slot's
// element-content-only contract could carry (see IsSlotBindingName's doc
// comment on why a slot rejects attribute position). Footer is rendered
// directly from Go (chromeFooter) and its result folds into the children
// Fragment chromeLayout passes to Layout, the same way today's Footer()
// nests inside <main> beside content — Layout's own body never calls
// <Footer/>.
type FooterProps struct {
	Message string
}

component Footer(props: FooterProps) {
	return <div class="footer">{props.Message}</div>
}
