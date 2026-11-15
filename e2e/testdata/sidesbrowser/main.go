//go:build js && wasm

// Command sidesbrowser is the page program for the side-select browser e2e
// test. It wires kitui.SideSelect to the keyboard (actions.Attach) and to
// navigator.getGamepads (actions.PadPoller), and redraws on every change.
package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"syscall/js"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
	"m31labs.dev/gosx/game/kitui"
)

type view struct {
	Seats     []kitui.Seat
	Fields    map[string]string
	Starts    int
	Backs     int
	Startable bool
	Changes   []string
	Players   map[string][]int
}

func main() {
	g := js.Global()
	doc := g.Get("document")
	q, _ := url.ParseQuery(strings.TrimPrefix(g.Get("location").Get("search").String(), "?"))
	online := q.Get("mode") == "online"
	root := doc.Call("getElementById", "root")

	var (
		starts, backs int
		changes       []string
		model         *kitui.SideSelect
	)
	routerMaps := []*actions.Map{actions.NewMap(), actions.NewMap(), actions.NewMap(), actions.NewMap()}
	router := actions.NewRouter(routerMaps...)

	model = kitui.NewSideSelect(kitui.SideConfig{
		SideNames: [2]string{"Home", "Away"},
		Fields: []kitui.Field{
			{ID: "quarter", Label: "Quarter length", Options: []kitui.Option{{Value: "2", Label: "2 min"}, {Value: "5", Label: "5 min"}, {Value: "10", Label: "10 min"}}, Index: 1},
			{ID: "level", Label: "Difficulty", Options: []kitui.Option{{Value: "easy", Label: "Easy"}, {Value: "hard", Label: "Hard"}}},
		},
		Router: router, PlayerSlots: [2][]int{{0, 2}, {1, 3}},
		OnStart:  func(kitui.Result) { starts++ },
		OnBack:   func() { backs++ },
		OnChange: func(c kitui.Change) { changes = append(changes, c.Seat.ID+"|"+c.Field+"|"+c.Value) },
		OnRender: func() {
			kitui.Render(root, kitui.SideSelectPanel(kitui.SideSelectProps{Model: model}))
		},
	})
	model.ConnectKeyboard()
	if online {
		model.SetRemote([]kitui.Seat{{ID: "net:1", Name: "Ana", Slot: kitui.SlotB, Host: true}})
		model.SetFieldsReadOnly(true)
	}
	actions.Attach(model, actions.BrowserConfig{})
	kitui.Bind(doc.Get("body"), kitui.Handlers{OnAction: func(name, value string) { model.HandleAction(name, value) }})
	pads := actions.NewPadPoller()

	state := func() string {
		v := view{Seats: model.Seats(), Fields: model.Result().Fields, Starts: starts, Backs: backs, Startable: model.Startable(), Changes: changes, Players: map[string][]int{}}
		for _, s := range v.Seats {
			v.Players[s.ID] = router.PlayersFor(s.ID)
		}
		out, _ := json.Marshal(v)
		return string(out)
	}
	obj := g.Get("Object").New()
	obj.Set("frame", js.FuncOf(func(js.Value, []js.Value) any {
		src := host.NavigatorSource{}
		model.SyncPads(src)
		pads.Poll(src, model)
		return state()
	}))
	obj.Set("state", js.FuncOf(func(js.Value, []js.Value) any { return state() }))
	obj.Set("remote", js.FuncOf(func(_ js.Value, a []js.Value) any {
		var seats []kitui.Seat
		if err := json.Unmarshal([]byte(a[0].String()), &seats); err == nil {
			model.SetRemote(seats)
		}
		return nil
	}))
	g.Set("kit", obj)
	model.Connect("pad:9", "tmp", kitui.SeatGamepad) // forces a first render
	model.Disconnect("pad:9")
	select {}
}
