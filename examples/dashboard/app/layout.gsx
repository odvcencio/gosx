package app

// Layout matches the sidebar/main shell main.go's Layout builds by hand for
// the two island routes (/counter, /kitchen-sink), which need to inject
// island preload hints and page-head scripts that this file-routed layout
// has no hook for. Keep the two in step by hand when either one changes.
func Layout() Node {
	return <div class="layout">
		<aside class="sidebar">
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
		<main class="main">
			<Slot />
			<div class="footer">{dash.footer}</div>
		</main>
	</div>
}
