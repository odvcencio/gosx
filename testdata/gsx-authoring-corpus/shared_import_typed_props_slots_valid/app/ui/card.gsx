package ui

type CardProps struct {
	Tone string
}

component Card(props: CardProps) {
	return <section class={"tone-" + props.Tone}><header>{slotTitle}</header><main>{children}</main></section>
}

