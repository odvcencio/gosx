package docs

type ContractRowProps struct {
	Label string
	Value string
}

component ContractRow(props: ContractRowProps) {
	return <li className="typed-proof__row">
		<span>{props.Label}</span>
		<strong>{props.Value}</strong>
	</li>
}

component Page() {
	return <article className="typed-proof">
		<p className="typed-proof__kicker">
			Rendered by the v0.39 strict component path
		</p>
		<h2>This page is the example.</h2>
		<p>
			Its source uses the TSX-like component declaration, a real Go props struct, lower-camel attributes, and the production file renderer serving this request.
		</p>
		<ul className="typed-proof__contract">
			<ContractRow label="Declaration" value="component Name(props: GoType)" />
			<ContractRow label="Type authority" value="gosx check + Go" />
			<ContractRow label="Runtime" value="server HTML" />
		</ul>
		<p className="typed-proof__boundary">
			The strict form is deliberately server-only in v0.39. Routes that need loader data, control flow, islands, or engines continue to use the legacy Go-function style.
		</p>
		<a
			className="typed-proof__source"
			href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/typed-live/page.gsx"
			rel="noopener"
		>Read this route's source</a>
	</article>
}
