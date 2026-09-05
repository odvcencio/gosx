package app

import "m31labs.dev/gosx/signal"
import ui "./ui"

type CounterProps struct {
	Instance string
	Label    string
	Start    int
}

type CounterShellProps struct {
	Instance string
	Label    string
	Count    int
}

component CounterShell(props: CounterShellProps) {
	return <article class="strict-counter-card" data-instance={props.Instance}>
		<header>
			<h2>{props.Label}</h2>
			<p class="strict-counter-meta">{slotMeta}</p>
		</header>
		<div class="strict-counter-body">{children}</div>
		<footer class="strict-counter-total">Current: <span class="strict-counter-value">{props.Count}</span></footer>
	</article>
}

// Counter is a root strict island. It intentionally declares no root
// children or named slots: ir.LowerIsland rejects those because a root
// island call-site is server-rendered outside the per-component .gxi program.
// The same-file CounterShell helper below carries typed props, {children},
// and {slotMeta}; the live count expression and click handler are owned by
// this island caller and projected into that helper.
//
//gosx:island
component Counter(props: CounterProps) {
	count := signal.New(props.Start)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="strict-counter-root" id={"strict-counter-" + props.Instance}>
		<CounterShell Instance={props.Instance} Label={props.Label} Count={count.Get()}>
			<span class="strict-counter-live" id={"strict-counter-" + props.Instance + "-value"}>{count.Get()}</span>
			<button class="strict-counter-button" id={"strict-counter-" + props.Instance + "-button"} onClick={increment}>Increment {props.Instance}</button>
			<strong slot="Meta" class="strict-counter-seed" id={"strict-counter-" + props.Instance + "-seed"}>seed {props.Start}</strong>
		</CounterShell>
	</div>
}

component Page() {
	return <ui.Chrome Title="Strict composition production fixture">
		<Counter Instance="a" Label="Alpha desk" Start={2} />
		<Counter Instance="b" Label="Beta desk" Start={11} />
	</ui.Chrome>
}
