package main

type BreakdownRow struct {
	Scored bool
	Label string
}

type RowProps struct {
	Breakdown []BreakdownRow
}

component Page(props: RowProps) {
	return <div>
		<Each of={props.Breakdown} as="row" index="i">
			<div class={"tone-" + row.Label} data-scored={row.Scored}>
				<span>{row.Label}</span>
				<span>{i}</span>
				<If cond={row.Scored}>scored</If>
			</div>
		</Each>
	</div>
}

