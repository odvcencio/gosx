package app

func Layout() Node {
	return <div class="layout">
		<aside class="sidebar">
			<h2>GoSX Dashboard</h2>
			<nav>
				<a href="/" data-gosx-link>Home</a>
				<a href="/users" data-gosx-link>Users</a>
				<a href="/users/new" data-gosx-link>New User</a>
				<a href="/counter" data-gosx-link>Counter</a>
				<a href="/kitchen-sink" data-gosx-link>Kitchen Sink</a>
				<a href="/settings" data-gosx-link>Settings</a>
			</nav>
		</aside>
		<main class="main">
			<Slot />
			<div class="footer">GoSX — Server rendered</div>
		</main>
	</div>
}
