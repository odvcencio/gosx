package panel

import (
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/signal"
)

// The island reads the engine's signals and writes its call signal. It does not
// know whether the engine runs in a browser worker or natively in the desktop
// host. Cmd, Reply, Ref and View repeat the field names of the Go types the
// engine uses, because those names are the wire keys.
type Args struct{ By int }
type Sum struct{ Total int }
type Cmd struct {
	ID   uint64
	Name string
	Args Args
}
type Ref struct {
	ID    uint64
	Error string
}
type Reply struct {
	ID     uint64
	Name   string
	Error  string
	Value  Sum
	Recent []Ref
}
type View struct {
	Count  int
	Ticks  int
	Source string
}

//gosx:island
func Panel(props struct{}) Node {
	status := signal.NewShared("gosx.engine.gosx-engine-counter", engine.WorkerStatus{})
	view := signal.NewShared("counter.view", View{})
	cmd := signal.NewShared("counter.cmd", Cmd{})
	res := signal.NewShared("counter.cmd.result", Reply{})
	inc := func() { cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "increment", Args: Args{By: 1}}) }
	refuse := func() { cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "refuse"}) }
	return <section class="panel">
		<h1>Native engine</h1>
		<p>Engine: <b id="state">{status.Get().State}/{status.Get().Thread}</b> running {view.Get().Source}</p>
		<p>Clock ticks: <b id="ticks">{view.Get().Ticks}</b></p>
		<p>Count: <b id="count">{view.Get().Count}</b></p>
		<p>Last call: <b id="result">{res.Get().ID}:{res.Get().Value.Total}:{res.Get().Error}</b></p>
		<button id="inc" onClick={inc} disabled={status.Get().State != "running"}>Increment</button>
		<button id="refuse" onClick={refuse} disabled={status.Get().State != "running"}>Refused call</button>
	</section>
}
