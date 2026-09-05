package main

component Shell() {
	return <section><header>{slotTitle}</header><main>{children}</main></section>
}

component Page() {
	return <Shell>{<h1 slot={"Title"}>Roster</h1>}</Shell>
}

