package app

func Page() Node {
	return <div>
		<h1>Dashboard</h1>
		<div class="grid">
			<div class="card">
				<h3>Users</h3>
				<div class="stat">{data.users}</div>
			</div>
			<div class="card">
				<h3>Active</h3>
				<div class="stat">{data.active}</div>
			</div>
			<div class="card">
				<h3>Revenue</h3>
				<div class="stat">{data.revenue}</div>
			</div>
			<div class="card">
				<h3>Growth</h3>
				<div class="stat">{data.growth}</div>
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
					<tr><td>Alice</td><td>Created account</td><td>2 min ago</td></tr>
					<tr><td>Bob</td><td>Updated profile</td><td>15 min ago</td></tr>
					<tr><td>Carol</td><td>Uploaded document</td><td>1 hour ago</td></tr>
					<tr><td>Dave</td><td>Changed settings</td><td>3 hours ago</td></tr>
					<tr><td>Eve</td><td>Logged in</td><td>5 hours ago</td></tr>
				</tbody>
			</table>
		</div>
	</div>
}
