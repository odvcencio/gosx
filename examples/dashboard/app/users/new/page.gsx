package new

func Page() Node {
	return <>
		<h1>New User</h1>
		<div class="card">
			<form method="post" action="/gosx/action/createUser">
				<input type="hidden" name="csrf_token" value={csrf.token}></input>
				<div class="form-group">
					<label>Name</label>
					<input type="text" name="name" placeholder="Full name" required></input>
				</div>
				<div class="form-group">
					<label>Email</label>
					<input type="email" name="email" placeholder="email@example.com" required></input>
				</div>
				<div class="form-group">
					<label>Role</label>
					<select name="role">
						<option value="viewer">Viewer</option>
						<option value="editor">Editor</option>
						<option value="admin">Admin</option>
					</select>
				</div>
				<button type="submit" class="btn btn-primary">Create User</button>
				<a href="/users" class="btn btn-cancel">Cancel</a>
			</form>
		</div>
	</>
}
