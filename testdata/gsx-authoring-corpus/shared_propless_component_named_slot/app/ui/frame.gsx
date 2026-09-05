package ui

component Badge() {
	return <span class="badge">Badge</span>
}

component Frame() {
	return <section><header>{slotTitle}</header><main>{children}</main></section>
}

