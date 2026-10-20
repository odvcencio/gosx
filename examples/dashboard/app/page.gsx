package app

// PageProps is the dashboard root page's typed root props (gosx#248). This
// type only ever compiles through the gosx toolchain, never through `go
// build` — page.server.go's Load hook, real Go, cannot spell this name, so
// it returns its own same-shaped dashboardStats instead. The strict
// boundary proves field coverage structurally, not type identity (see
// ProgramRenderEnv.Props), so a same-shaped struct under a different Go
// type name satisfies this declaration.
type PageProps struct {
	Users   string
	Active  string
	Revenue string
	Growth  string
}

component Page(props: PageProps) {
	return <>
		<h1>Dashboard</h1>
		<div class="grid">
			<div class="card">
				<h3>Users</h3>
				<div class="stat">{props.Users}</div>
			</div>
			<div class="card">
				<h3>Active</h3>
				<div class="stat">{props.Active}</div>
			</div>
			<div class="card">
				<h3>Revenue</h3>
				<div class="stat">{props.Revenue}</div>
			</div>
			<div class="card">
				<h3>Growth</h3>
				<div class="stat">{props.Growth}</div>
			</div>
		</div>
		<div class="card">
			<h3>Recent Activity</h3>
			<table>
				<thead>
					<tr>
						<th>User</th>
						<th>Action</th>
						<th>When</th>
					</tr>
				</thead>
				<tbody>
					<tr>
						<td>Alice</td>
						<td>Created account</td>
						<td>2 min ago</td>
					</tr>
					<tr>
						<td>Bob</td>
						<td>Updated profile</td>
						<td>15 min ago</td>
					</tr>
					<tr>
						<td>Carol</td>
						<td>Uploaded document</td>
						<td>1 hour ago</td>
					</tr>
					<tr>
						<td>Dave</td>
						<td>Changed settings</td>
						<td>3 hours ago</td>
					</tr>
					<tr>
						<td>Eve</td>
						<td>Logged in</td>
						<td>5 hours ago</td>
					</tr>
				</tbody>
			</table>
		</div>
	</>
}
