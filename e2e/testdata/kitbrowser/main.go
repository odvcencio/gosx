//go:build js && wasm

// Command kitbrowser is the page program for the game kit browser e2e test.
// It wires the kit packages the way a game would and exposes window.kit so the
// test can drive frames and read state. It holds no game logic.
package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/boot"
	"m31labs.dev/gosx/game/flow"
	"m31labs.dev/gosx/game/host"
	"m31labs.dev/gosx/game/kitui"
	"m31labs.dev/gosx/game/settings"
	"m31labs.dev/gosx/game/storage"
)

type pauser struct {
	paused bool
	calls  int
}

func (p *pauser) SetPaused(v bool) { p.paused, p.calls = v, p.calls+1 }

type pending struct {
	active bool
	action string
	slot   int
}

type state struct {
	Actions    map[string]actions.State
	Stack      []string
	Paused     bool
	PauseCalls int
	Boot       string
	Progress   float64
	Soft       int
	Attempt    int
	Locked     bool
	Capturing  bool
	Bindings   map[string][]string
	Settings   settings.Settings
	Retried    int
}

func main() {
	g := js.Global()
	doc := g.Get("document")
	q, _ := url.ParseQuery(strings.TrimPrefix(g.Get("location").Get("search").String(), "?"))
	stallMS := 1000
	if v := q.Get("stall"); v != "" {
		stallMS = atoi(v)
	}

	opts := settings.NewManager(settings.Config{Store: storage.NewDefault("kit")})
	opts.Load()

	in := actions.NewMap()
	in.Define(actions.Context{Name: "gameplay", Group: "play", Actions: []actions.ActionDef{
		{Name: "jump", Bindings: []actions.Binding{{Source: actions.Key("Space")}, {Source: actions.PadButton(actions.PadA)}}},
		{Name: "moveX", Kind: actions.Axis, Bindings: []actions.Binding{{Source: actions.Key("KeyD")}, {Source: actions.Key("KeyA"), Scale: -1}}},
		{Name: "moveY", Kind: actions.Axis, Bindings: []actions.Binding{{Source: actions.Key("KeyW")}, {Source: actions.Key("KeyS"), Scale: -1}}},
		{Name: "fire", Bindings: []actions.Binding{{Source: actions.MouseButton(0)}}},
		{Name: "lookX", Kind: actions.Axis, Bindings: []actions.Binding{{Source: actions.Motion(actions.CodeMouseDX), Scale: 0.01}}},
		{Name: "zoom", Kind: actions.Axis, Bindings: []actions.Binding{{Source: actions.Motion(actions.CodeWheelY), Scale: 0.01}}},
		{Name: "pause", Bindings: actions.PauseBindings()},
	}})
	in.Define(actions.Context{Name: "menu", Opaque: true, Actions: []actions.ActionDef{{Name: "back", Bindings: actions.BackBindings()}}})
	in.Import(opts.Get().Bindings)

	pz := &pauser{}
	fl := flow.New(flow.Config{Pauser: pz, Input: in},
		flow.State{Name: flow.Play, Input: "gameplay", Pausable: true, PauseOnBlur: true},
		flow.State{Name: flow.Pause, Input: "menu", Pause: true})
	_ = fl.Push(flow.Play)
	flow.AttachBrowser(fl)
	actions.Attach(in, actions.BrowserConfig{PointerLockOnly: true})
	pads := actions.NewPadPoller()

	retried := 0
	marker, key := boot.BrowserMarker()
	gate := boot.New(boot.Config{
		StallTimeout: time.Duration(stallMS) * time.Millisecond, Retries: 1,
		Reload: boot.BrowserReload, Marker: marker, MarkerKey: key,
		ReloadDelay: 100 * time.Millisecond,
		OnChange:    boot.DOMObserver(doc.Call("querySelector", "[data-boot-root]")),
	}, boot.Task{Name: "assets", Retry: func() { retried++ }})

	// Rebinding screen.
	panel := doc.Call("getElementById", "panel")
	var pend pending
	message := ""
	render := func() {
		if !panel.Truthy() || q.Get("panel") != "rebind" {
			return
		}
		kitui.Render(panel, kitui.RebindPanel(kitui.RebindProps{
			Map: in, Context: "gameplay", Message: message,
			Listening: pend.active, ListeningAction: pend.action, ListeningSlot: pend.slot,
		}))
	}
	render()

	push := func(kind, name, value string) {
		g.Get("kitEvents").Call("push", kind+"|"+name+"|"+value)
	}
	kitui.Bind(doc.Get("body"), kitui.Handlers{
		OnAction: func(name, value string) { push("action", name, value) },
		OnSetting: func(k, v string) {
			_, err := kitui.ApplySetting(opts, k, v)
			if err != nil {
				push("error", k, err.Error())
				return
			}
			push("setting", k, v)
		},
		OnRebind: func(ctx, action string, slot int) {
			push("rebind", ctx+"/"+action, itoa(slot))
			pend = pending{true, action, slot}
			message = ""
			in.Capture()
			render()
		},
	})

	snapshot := func() string {
		st := state{
			Actions: map[string]actions.State{}, Stack: fl.Stack(), Paused: pz.paused, PauseCalls: pz.calls,
			Boot: gate.Phase().String(), Progress: gate.Progress(), Soft: gate.Snapshot().SoftRetries,
			Attempt: gate.Snapshot().Attempt, Locked: actions.PointerLocked(), Capturing: in.Capturing(),
			Bindings: map[string][]string{}, Settings: opts.Get(), Retried: retried,
		}
		for _, n := range []string{"jump", "moveX", "moveY", "fire", "lookX", "zoom", "pause", "back"} {
			st.Actions[n] = in.State(n)
		}
		for _, a := range in.Actions("gameplay") {
			for _, b := range in.Bindings("gameplay", a) {
				st.Bindings[a] = append(st.Bindings[a], b.Source.String())
			}
		}
		out, _ := json.Marshal(st)
		return string(out)
	}

	kit := map[string]any{
		"frame": js.FuncOf(func(_ js.Value, args []js.Value) any {
			dt := time.Duration(args[0].Float() * float64(time.Millisecond))
			pads.Poll(host.NavigatorSource{}, in)
			in.Update(dt)
			fl.HandleInput(in, flow.Controls{})
			gate.Update(dt)
			if pend.active {
				if src, ok := in.TakeCaptured(); ok {
					_, err := in.Rebind("gameplay", pend.action, pend.slot, src, actions.ConflictReject)
					switch {
					case err == nil:
						message = ""
						opts.SetBindings(in.Export())
					case err == actions.ErrConflict:
						message = src.String() + " is already used"
					default:
						message = err.Error()
					}
					pend.active = false
					render()
				} else if !in.Capturing() {
					pend.active = false
					message = "cancelled"
					render()
				}
			}
			return snapshot()
		}),
		"state":    js.FuncOf(func(js.Value, []js.Value) any { return snapshot() }),
		"lock":     js.FuncOf(func(_ js.Value, args []js.Value) any { actions.RequestPointerLock(args[0]); return nil }),
		"progress": js.FuncOf(func(_ js.Value, a []js.Value) any { gate.Set("assets", a[0].Float(), a[1].Float()); return nil }),
		"complete": js.FuncOf(func(js.Value, []js.Value) any { gate.Complete("assets"); return nil }),
		"fail": js.FuncOf(func(js.Value, []js.Value) any {
			gate.Fail("assets", errString("load failed"))
			return nil
		}),
	}
	obj := g.Get("Object").New()
	for k, v := range kit {
		obj.Set(k, v)
	}
	g.Set("kit", obj)
	select {}
}

type errString string

func (e errString) Error() string { return string(e) }

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
