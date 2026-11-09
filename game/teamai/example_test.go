package teamai_test

import (
	"fmt"
	"log"

	"m31labs.dev/gosx/game/teamai"
)

// drillJSON is a toy capture-the-flag drill: three red runners raid a flag
// that a blue keeper guards while two blue chasers run them down.
const drillJSON = `{
  "format": "gosx.teamai",
  "version": 1,
  "roles": ["runner", "chaser", "keeper"],
  "objects": ["flag"],
  "signals": ["go"],
  "landmarks": [
    {"name": "flag_home", "host": true},
    {"name": "safe", "at": [0, -2]}
  ],
  "formations": [
    {"name": "spread", "slots": [
      {"name": "mid",  "role": "runner", "at": [0, -1]},
      {"name": "wide", "role": "runner", "at": [7, 0], "side": "strong"},
      {"name": "far",  "role": "runner", "at": [7, 0], "side": "weak"}
    ]},
    {"name": "shell", "slots": [
      {"name": "keeper", "role": "keeper", "at": [0, 2]},
      {"name": "left",   "role": "chaser", "at": [6, 12], "side": "strong"},
      {"name": "right",  "role": "chaser", "at": [6, 12], "side": "weak"}
    ]}
  ],
  "paths": [
    {"name": "post", "segments": [
      {"kind": "line", "by": [0, 12], "mark": "break"},
      {"kind": "decide", "window": 0.3,
       "branches": [{"when": "lane_open", "goto": "deep"}], "otherwise": "cut"},
      {"label": "cut", "kind": "curve", "by": [-5, 5], "control": [0, 5], "speed": 0.8},
      {"label": "deep", "kind": "line", "landmark": "flag_home", "track": true}
    ]}
  ],
  "rules": [
    {"name": "go", "fact": "signal.go", "op": ">=", "value": 1},
    {"name": "lane_open", "fact": "nearest_opponent.dist", "op": ">", "value": 6}
  ],
  "assignments": [
    {"name": "raid", "steps": [
      {"do": "slot", "until": "go"},
      {"do": "path", "path": "post"},
      {"do": "pursue", "target": "object:flag", "radius": 0.6, "mark": "grab"},
      {"do": "move", "landmark": "safe"}
    ]},
    {"name": "keep", "steps": [
      {"do": "guard", "landmark": "flag_home", "radius": 8, "target": "nearest_opponent", "gap": 1.5}
    ]},
    {"name": "chase", "steps": [
      {"do": "slot", "until": "go"},
      {"label": "pick", "do": "decide", "mode": "weighted",
       "branches": [{"goto": "cut", "weight": 1}, {"goto": "run", "weight": 2}]},
      {"label": "cut", "do": "cutoff", "target": "nearest_opponent", "margin": 0.2, "radius": 1, "next": "tag"},
      {"label": "run", "do": "pursue", "target": "nearest_opponent", "radius": 1},
      {"label": "tag", "do": "engage", "target": "nearest_opponent", "radius": 0.8, "duration": 1},
      {"do": "hold", "duration": 0.5, "next": "pick"}
    ]}
  ],
  "plans": [
    {"name": "red_raid", "formation": "spread",
     "assign": [{"role": "runner", "assignment": "raid"}]},
    {"name": "blue_shell", "formation": "shell",
     "assign": [{"slot": "keeper", "assignment": "keep"}, {"role": "chaser", "assignment": "chase"}]}
  ]
}`

// Example runs the drill for ten seconds at 30 ticks per second. The host
// owns the bodies and the rules of the drill (who holds the flag, whether a
// tag sticks); the executor only steers.
func Example() {
	book, err := teamai.LoadJSON([]byte(drillJSON), nil)
	if err != nil {
		log.Fatal(err)
	}
	const dt = 1.0 / 30
	ex, err := teamai.NewExecutor(book, teamai.Options{Step: dt, Separation: 1.5})
	if err != nil {
		log.Fatal(err)
	}

	runner, _ := book.Role("runner")
	chaser, _ := book.Role("chaser")
	keeper, _ := book.Role("keeper")
	names := []string{"red0", "red1", "red2", "keeper", "blue1", "blue2"}
	world := teamai.World{
		Seed:  7,
		Field: teamai.Rect{Min: teamai.V2(-20, 0), Max: teamai.V2(20, 60)},
		Teams: []teamai.Team{
			{Frame: teamai.Frame{Origin: teamai.V2(0, 10), Band: -1}},                            // red faces +Y
			{Frame: teamai.Frame{Origin: teamai.V2(0, 50), Forward: teamai.V2(0, -1), Band: -1}}, // blue faces -Y
		},
		Agents: []teamai.Body{
			{Pos: teamai.V2(-3, 4), Team: 0, Role: runner, MaxSpeed: 7.5, MaxAccel: 12},
			{Pos: teamai.V2(2, 5), Team: 0, Role: runner, MaxSpeed: 7, MaxAccel: 12},
			{Pos: teamai.V2(6, 3), Team: 0, Role: runner, MaxSpeed: 7.2, MaxAccel: 12},
			{Pos: teamai.V2(0, 55), Team: 1, Role: keeper, MaxSpeed: 6.5, MaxAccel: 10, Reaction: 2},
			{Pos: teamai.V2(-8, 56), Team: 1, Role: chaser, MaxSpeed: 7, MaxAccel: 10, Reaction: 3},
			{Pos: teamai.V2(8, 56), Team: 1, Role: chaser, MaxSpeed: 7, MaxAccel: 10, Reaction: 3},
		},
		Objects:   []teamai.Body{{Pos: teamai.V2(0, 50)}},
		Landmarks: []teamai.Vec2{teamai.V2(0, 50), {}},
		Signals:   []float64{0},
	}
	ex.Reset(len(world.Agents), len(world.Teams))
	red, _ := book.PlanID("red_raid")
	blue, _ := book.PlanID("blue_shell")
	ex.StartPlan(&world, 0, red)
	ex.StartPlan(&world, 1, blue)

	grab, _ := book.Mark("grab")
	tackle := teamai.ContestModel{Draw: 0.1}
	carrier := -1
	for tick := 0; tick < 300; tick++ {
		if tick == 15 {
			world.Signals[0] = 1 // the drill starts after half a second
		}
		cmds := ex.Step(&world)
		for _, ev := range ex.Events() {
			at := float64(tick) * dt
			switch ev.Kind {
			case teamai.EventMark:
				switch {
				case ev.Value != grab:
					fmt.Printf("%.2fs %s reached %s\n", at, names[ev.Agent], book.MarkName(ev.Value))
				case carrier < 0:
					fmt.Printf("%.2fs %s takes the flag\n", at, names[ev.Agent])
					carrier = ev.Agent
				}
			case teamai.EventContact:
				if int(ev.Target.Index) != carrier {
					continue
				}
				a, d := &world.Agents[ev.Agent], &world.Agents[carrier]
				out := tackle.Resolve(teamai.Contest{
					Attacker: 70, Defender: 66,
					Angle:    teamai.ApproachAngle(a.Vel, d.Pos.Sub(a.Pos)),
					RelSpeed: a.Vel.Len() - d.Vel.Len(),
				}, world.Seed, world.Tick, uint64(ev.Agent), uint64(carrier))
				fmt.Printf("%.2fs %s tags %s: %s (p=%.2f)\n", at, names[ev.Agent], names[carrier], out.Result, out.P)
				if out.Result == teamai.Win {
					carrier = -1
					world.Objects[0].Pos = world.Landmarks[0]
				}
			}
		}
		for i := range world.Agents {
			teamai.Integrate(&world.Agents[i], cmds[i].Vel, dt)
		}
		if carrier >= 0 {
			world.Objects[0].Pos = world.Agents[carrier].Pos
			world.Objects[0].Vel = world.Agents[carrier].Vel
		}
		world.Tick++
	}
	fmt.Printf("flag at (%.1f, %.1f), state hash %016x\n", world.Objects[0].Pos.X, world.Objects[0].Pos.Y, ex.Hash())
	// Output:
	// 2.17s red0 reached break
	// 2.40s red1 reached break
	// 2.60s red2 reached break
	// 6.53s red0 takes the flag
	// 8.17s blue2 tags red0: win (p=0.86)
	// flag at (0.0, 50.0), state hash 3b4e5944bed2cf5a
}
