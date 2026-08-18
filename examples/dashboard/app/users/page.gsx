package users

func Page() Node {
	return <>
		<h1>Users</h1>
		<div class="search-bar">
			<form method="get" action="/users" class="search-form">
				<input type="text" name="q" placeholder="Search users..." value={data.Query} />
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
					<Each of={data.Users} as="u">
						<tr>
							<td>{u.Name}</td>
							<td>{u.Email}</td>
							<td>{u.Role}</td>
							<td>
								<If cond={u.Active}>
									<span class="badge badge-active">Active</span>
								</If>
								<If cond={!u.Active}>
									<span class="badge badge-inactive">Inactive</span>
								</If>
							</td>
							<td>
								<button class="btn btn-danger btn-sm">Delete</button>
							</td>
						</tr>
					</Each>
				</tbody>
			</table>
			<If cond={data.Empty}>
				<p class="empty-state">No users found matching your search.</p>
			</If>
		</div>
	</>
}
