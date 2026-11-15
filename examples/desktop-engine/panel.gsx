package main

import (
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/signal"
)

// The island reads the engine's signals and writes its call signal. It does not
// know whether the engine runs in a browser worker or natively in the desktop
// host. The Panel types repeat the field names of the Go types the engine uses,
// because those names are the wire keys. Their type names differ (engine.go
// declares View in the same package), which does not affect the wire.
type PanelArgs struct{ By int }
type PanelSum struct{ Total int }
type PanelCmd struct {
	ID   uint64
	Name string
	Args PanelArgs
}
type PanelRef struct {
	ID    uint64
	Error string
}
type PanelReply struct {
	ID     uint64
	Name   string
	Error  string
	Value  PanelSum
	Recent []PanelRef
}
type PanelView struct {
	Count  int
	Ticks  int
	Source string
}

//gosx:island
component Panel() {
	status := signal.NewShared("gosx.engine.gosx-engine-counter", engine.WorkerStatus{})
	view := signal.NewShared("counter.view", PanelView{})
	cmd := signal.NewShared("counter.cmd", PanelCmd{})
	res := signal.NewShared("counter.cmd.result", PanelReply{})
	inc := func() { cmd.Set(PanelCmd{ID: cmd.Get().ID + 1, Name: "increment", Args: PanelArgs{By: 1}}) }
	refuse := func() { cmd.Set(PanelCmd{ID: cmd.Get().ID + 1, Name: "refuse"}) }
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
