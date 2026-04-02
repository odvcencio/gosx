package users

func Page() Node {
	return <div>
		<h1>Users</h1>
		<div class="search-bar">
			<form method="get" action="/users" class="search-form">
				<input type="text" name="q" placeholder="Search users..." value={data.query}></input>
				<button type="submit" class="btn btn-primary">Search</button>
			</form>
			<a href="/users/new" class="btn btn-primary">+ New User</a>
		</div>
		<div class="card">
			<table>
				<thead>
					<tr>
						<th>Name</th>
						<th>Email</th>
						<th>Role</th>
						<th>Status</th>
						<th>Actions</th>
					</tr>
				</thead>
				<tbody>
					<tr>
						<td>Alice Johnson</td>
						<td>alice@example.com</td>
						<td>Admin</td>
						<td>
							<span class="badge badge-active">Active</span>
						</td>
						<td>
							<button class="btn btn-danger btn-sm">Delete</button>
						</td>
					</tr>
					<tr>
						<td>Bob Smith</td>
						<td>bob@example.com</td>
						<td>Editor</td>
						<td>
							<span class="badge badge-active">Active</span>
						</td>
						<td>
							<button class="btn btn-danger btn-sm">Delete</button>
						</td>
					</tr>
					<tr>
						<td>Carol Williams</td>
						<td>carol@example.com</td>
						<td>Viewer</td>
						<td>
							<span class="badge badge-inactive">Inactive</span>
						</td>
						<td>
							<button class="btn btn-danger btn-sm">Delete</button>
						</td>
					</tr>
					<tr>
						<td>Dave Brown</td>
						<td>dave@example.com</td>
						<td>Editor</td>
						<td>
							<span class="badge badge-active">Active</span>
						</td>
						<td>
							<button class="btn btn-danger btn-sm">Delete</button>
						</td>
					</tr>
					<tr>
						<td>Eve Davis</td>
						<td>eve@example.com</td>
						<td>Admin</td>
						<td>
							<span class="badge badge-active">Active</span>
						</td>
						<td>
							<button class="btn btn-danger btn-sm">Delete</button>
						</td>
					</tr>
					<tr>
						<td>Frank Miller</td>
						<td>frank@example.com</td>
						<td>Viewer</td>
						<td>
							<span class="badge badge-inactive">Inactive</span>
						</td>
						<td>
							<button class="btn btn-danger btn-sm">Delete</button>
						</td>
					</tr>
				</tbody>
			</table>
		</div>
	</div>
}
