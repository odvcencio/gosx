package new

func Page() Node {
	return <>
		<h1>New User</h1>
		<div class="card">
			<form method="post" action={actionPath("createUser")}>
				<div class="form-group">
					<label>Name</label>
					<input
						type="text"
						name="name"
						placeholder="Full name"
						required
						value={actions.createUser.values.name}
					></input>
					<If cond={actions.createUser.fieldErrors.name != ""}>
						<p class="form-error">{actions.createUser.fieldErrors.name}</p>
					</If>
				</div>
				<div class="form-group">
					<label>Email</label>
					<input
						type="email"
						name="email"
						placeholder="email@example.com"
						required
						value={actions.createUser.values.email}
					></input>
					<If cond={actions.createUser.fieldErrors.email != ""}>
						<p class="form-error">{actions.createUser.fieldErrors.email}</p>
					</If>
				</div>
				<div class="form-group">
					<label>Role</label>
					<select name="role">
						<option value="viewer">Viewer</option>
						<option value="editor">Editor</option>
						<option value="admin">Admin</option>
					</select>
				</div>
				<If cond={action.message != ""}>
					<p class="form-status">{action.message}</p>
				</If>
				<button type="submit" class="btn btn-primary">Create User</button>
				<a href="/users" class="btn btn-cancel">Cancel</a>
			</form>
		</div>
	</>
}
