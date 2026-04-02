package settings

func Page() Node {
	return <div>
		<h1>Settings</h1>
		<div class="card">
			<h3>Application Settings</h3>
			<form method="post" action={actionPath("saveSettings")}>
				<input type="hidden" name="csrf_token" value={csrf.token}></input>
				<div class="form-group">
					<label>Site Name</label>
					<input type="text" name="siteName" value="GoSX Dashboard"></input>
				</div>
				<div class="form-group">
					<label>Theme</label>
					<select name="theme">
						<option value="light">Light</option>
						<option value="dark">Dark</option>
					</select>
				</div>
				<div class="form-group">
					<label>Items per page</label>
					<input type="number" name="pageSize" value="25" min="10" max="100"></input>
				</div>
				<p class="form-status">{action.message}</p>
				<button type="submit" class="btn btn-primary">Save Settings</button>
			</form>
		</div>
	</div>
}
