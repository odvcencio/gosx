package new

func Page() Node {
	return <div>
		<h1>New User</h1>
		<div class="card">
			<form method="post" action={actionPath("createUser")}>
				<input type="hidden" name="csrf_token" value={csrf.token}></input>
				<div class="form-group">
					<label>Name</label>
					<input type="text" name="name" placeholder="Full name" value={actions.createUser.values.name}></input>
					<p class="form-error">{actions.createUser.fieldErrors.name}</p>
				</div>
				<div class="form-group">
					<label>Email</label>
					<input type="email" name="email" placeholder="email@example.com" value={actions.createUser.values.email}></input>
					<p class="form-error">{actions.createUser.fieldErrors.email}</p>
				</div>
				<div class="form-group">
					<label>Role</label>
					<select name="role">
						<option value="viewer">Viewer</option>
						<option value="editor">Editor</option>
						<option value="admin">Admin</option>
					</select>
				</div>
				<p class="form-status">{action.message}</p>
				<button type="submit" class="btn btn-primary">Create User</button>
				<a href="/users" data-gosx-link class="btn btn-cancel">Cancel</a>
			</form>
		</div>
	</div>
}
