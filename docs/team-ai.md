# Team AI

Package `m31labs.dev/gosx/game/teamai` runs the computer-controlled players of a
team or squad game: where each agent lines up, the timed path it runs, how it
chases, cuts off, shadows or guards, and which way it goes when a read comes up.
You describe formations, paths, rules and assignments as data. The executor runs
them at your fixed step, and it gives the same answer on the server and in every
predicting client.

The toolkit knows nothing about any sport. It works with frames, slots, roles,
paths, facts and contests, and your game decides what they mean.

| Piece | Does |
|---|---|
| `Frame`, `Rect` | A movable reference: origin spot, facing, strength side, mirror, lateral band. The field bounds. |
| `Formation` | Alignment slots relative to the frame, another slot or the field edge, filled by role. |
| `Path` | Timed segments (line, curve, hold, decide) with speed profiles, landmarks, marks and rule-chosen branches. |
| `Seek`, `Arrive`, `Intercept`, `Cutoff`, `Separation`, ... | Steering and interception math. Pure functions, no allocation. |
| `Predicate`, `Chooser`, `Registry` | The rule interface over a read-only per-agent `Query`. |
| `Executor` | Runs one assignment per agent, returns velocity commands and events, and saves its state in snapshots. |
| `ContestModel` | Seeded, rating-weighted resolution of a one-on-one contest. |
| `game/teamai/arbiterrules` | Optional adapter that lets Arbiter rules answer decisions (a separate module). |

## Example

`game/teamai/example_test.go` runs a toy capture-the-flag drill: three red
runners raid a flag, a blue keeper guards it, and two blue chasers run the
raiders down. The data (shortened here) is JSON:

```json
{
  "format": "gosx.teamai",
  "version": 1,
  "roles": ["runner", "chaser", "keeper"],
  "objects": ["flag"],
  "signals": ["go"],
  "landmarks": [{"name": "flag_home", "host": true}, {"name": "safe", "at": [0, -2]}],
  "formations": [
    {"name": "spread", "slots": [
      {"name": "mid",  "role": "runner", "at": [0, -1]},
      {"name": "wide", "role": "runner", "at": [7, 0], "side": "strong"},
      {"name": "far",  "role": "runner", "at": [7, 0], "side": "weak"}
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
    ]}
  ],
  "plans": [
    {"name": "red_raid", "formation": "spread", "assign": [{"role": "runner", "assignment": "raid"}]}
  ]
}
```

The host owns the bodies and the rules of its game. Each tick it fills a
`World`, calls `Step`, moves the agents, and reacts to events:

```go
book, err := teamai.LoadJSON(data, nil)
ex, err := teamai.NewExecutor(book, teamai.Options{Step: 1.0 / 30, Separation: 1.5})
ex.Reset(len(world.Agents), len(world.Teams))
red, _ := book.PlanID("red_raid")
ex.StartPlan(&world, 0, red) // slots by role, then each slot's assignment

for tick := 0; ; tick++ {
	cmds := ex.Step(&world) // one Command per agent: Vel and Face
	for _, ev := range ex.Events() {
		if ev.Kind == teamai.EventContact {
			out := tackle.Resolve(contest, world.Seed, world.Tick, uint64(ev.Agent), uint64(ev.Target.Index))
			_ = out.Result // Win, Draw or Loss; the host applies it
		}
	}
	for i := range world.Agents {
		teamai.Integrate(&world.Agents[i], cmds[i].Vel, 1.0/30) // or your character controller
	}
	world.Tick++
}
```

Its output, the same on every platform:

```text
2.17s red0 reached break
2.40s red1 reached break
2.60s red2 reached break
6.53s red0 takes the flag
8.17s blue2 tags red0: win (p=0.86)
flag at (0.0, 50.0), state hash 3b4e5944bed2cf5a
```

## Running it in a game

Run the executor from a `PhaseFixedUpdate` system on `game.Runtime`, so it
steps at the fixed tick on the server and on clients alike. Under
`game.CatchUpLag` (C1) the runtime never drops a tick, so a client that falls
behind still runs every tick the server ran.

1. Before `Step`, copy your agents into `World.Agents` (position, velocity,
   team, role, top speed, acceleration, reaction ticks), your tracked objects
   into `World.Objects`, and your host landmarks and signals into their
   slices. Every slice is indexed by an ID from the `Book`
   (`book.Object("flag")`, `book.Landmark("goal")`, `book.Signal("clock")`).
2. Call `ex.Step(&world)`. It reads the world and returns one `Command` per
   agent. It does not change the world.
3. Move each agent with its command. With GoSX physics, list the executor's
   system before `physicsys.Characters`, and have the characters' input
   function return the command as the desired horizontal velocity. teamai's
   Y is the world's Z:

   ```go
   physicsys.Characters(func(ctx *game.Context, c *physics.Character) physics.CharacterInput {
   	v := cmds[agentOf(c)].Vel
   	return physics.CharacterInput{Move: physics.Vec3{X: v.X, Z: v.Y}}
   })
   ```

   The capsule controller then slides along walls and climbs steps; read the
   agents' next positions from `c.Position()` (X and Z). Without physics, use
   `teamai.Integrate`.
4. Handle `ex.Events()`: marks reached, decisions taken, interrupts, contacts,
   finished assignments. Contacts are where contests start.
5. Put `ex.AppendState(dst)` in your simulation snapshot and call
   `ex.LoadState` in restore. `game/netsync` restores snapshots several times a
   second during reconciliation, and both calls are allocation-free.

The executor is deterministic: agents are stepped in index order, each reads
only the world given for the tick, and every random choice is a hash of
`World.Seed`, `World.Tick`, the agent and the decision. Keep your own host code
just as strict: use `det` for float math and iterate in a fixed order. See
`docs/deterministic-simulation.md`.

## Frames and formations

A `Frame` is a reference that moves with play: an `Origin` spot, a `Forward`
direction, a `Strength` side (+1 right, -1 left), a `Mirror` flag and a lateral
`Band` index. Local coordinates are `[lateral, depth]`: lateral runs along the
frame's right, depth along its forward.

A slot's `at` is local. Its `side` says how it reacts to the frame:

| `side` | Lateral sign |
|---|---|
| `fixed` (default) | as written; `Mirror` flips it |
| `strong` | on the strength side |
| `weak` | away from the strength side |

A slot can be placed `from` another slot of the formation (depth and lateral
offsets from it), or with `"lateral": "edge"`, `at[0]` units in from the field
edge on its side. `bands` adds an offset when the frame is in that band, and
`clamp` keeps the slot inside `World.Field`.

`StartPlan` fills the slots with `AssignByRole`: slots in order, each taking the
lowest-index free active agent of its role, then empty slots try their
`alternates`. `AssignNearest` takes the nearest eligible agent instead, for
re-forming a squad on the move. An agent's slot side becomes its sign, and its
paths and local landmarks mirror with it.

## Paths

A path is a list of segments. Points are local to the agent's frame when the
path starts, signed by the agent's slot side, and relative to where the agent
stood when the path started (`to`) or to the end of the previous segment
(`by`). The planned points do not drift when an agent is pushed off line; the
agent steers back to the line through a point a few ticks ahead.

| `kind` | Fields | Ends |
|---|---|---|
| `line` | `to`, `by`, or `landmark` + `offset` (+ `track` to follow a moving landmark) | within the arrival radius of its end |
| `curve` | `by` and `control`: a quadratic Bezier from the segment start | at its end |
| `hold` | `duration` | after `duration` seconds |
| `decide` | `branches`, `window`, `mode`, `chooser`, `otherwise` | when a branch is taken or the window ends |

Every segment takes `speed` and `end_speed` (fractions of the agent's top
speed, changing linearly with distance), or `duration` (the time to run it),
`not_before` (seconds after the path started; the agent waits), `mark` (set and
reported when the segment completes), `next` (a label to jump to) and `label`.

## Decisions

A decision has branches, each a rule (`when`) and a label (`goto`). In `first`
mode the first branch whose rule holds wins. In `weighted` mode one of the
branches whose rules hold is drawn in proportion to its `weight`, from a hash of
the seed, tick, agent and decision. A registered `chooser` replaces both.

The agent first tests a decision `Body.Reaction` ticks after reaching it, then
every tick for `window` seconds, and takes `otherwise` (default: the next entry)
when nothing fires. A decision with no window is tested once. Raise
`Reaction` to make an agent read slower; a slow reader with a short window
falls back to the default more often.

## Assignments and steps

An assignment is a small state machine of steps. A step ends when its `until`
rule holds, when `duration` seconds pass, or, when there is no `until`, at its
natural end.

| `do` | What the agent does | Natural end |
|---|---|---|
| `slot` | Goes to its formation slot (+ `offset`), live from the team frame | arrived |
| `move` | Goes to another `slot`, a `landmark`, or by `offset` from where the step started | arrived |
| `path` | Follows `path` | path finished |
| `hold` | Stands still (needs `duration` or `until`) | none |
| `mirror` | Holds `offset` from `target`, matching its velocity | none |
| `guard` | Guards the zone of `radius` around `landmark`; a `target` inside it is met `gap` units on the way to `protect` | none |
| `pursue` | Runs down `target`, leading it | contact within `radius`, if set |
| `cutoff` | Takes the cutoff angle on `target` against the field edge, `margin` seconds early | contact within `radius`, if set |
| `engage` | Pursues `target` to `radius`, reports `EventContact` once, then stays in contact | none |
| `decide` | Picks the next step (see Decisions) | a branch or the default |

Targets are `nearest_opponent`, `nearest_teammate`, `opponent:<role>`,
`teammate:<role>`, `object:<name>`, `landmark:<name>` and `assigned` (set by
the host with `ex.SetTarget`). They are picked every tick unless the step sets
`latch`.

`interrupts` jump to a step when a rule starts to hold. Each fires once per
false-to-true change, after the agent's `Reaction` ticks.

## Rules and facts

A rule is a yes-or-no question about the world from one agent. Write simple
ones in the data:

```json
{"name": "lane_open", "fact": "nearest_opponent.dist", "op": ">", "value": 6}
{"name": "beaten",    "fact": "target.y", "op": ">", "than": "self.y"}
{"name": "press",     "all": ["go", "rested"]}
{"name": "calm",      "not": "threat_close"}
```

Write the rest in Go and register them:

```go
reg := teamai.NewRegistry()
reg.Predicate("open_lane", teamai.PredicateFunc(func(q *teamai.Query) bool {
	return countDefendersAhead(q.World(), q.Self()) < 2
}))
reg.Chooser("utility", myScorer) // picks a branch index by score
book, err := teamai.LoadJSON(data, reg)
```

A `Predicate` must answer the same for the same world on every platform: no
clock, no global random source, no map iteration. A predicate or chooser that
needs the `Book` (to compile fact names with `book.ParseFact`) implements
`Binder`.

Facts are numbers a rule reads by name:

| Fact | Value |
|---|---|
| `tick` | the world tick |
| `signal.<name>`, `team.<name>` | a host signal; a team signal of the agent's team |
| `mark.<name>` | 1 once the agent reached the mark in this assignment |
| `mem.0` to `mem.3` | agent memory, set with `ex.SetMemory` |
| `<subject>.x`, `.y` | position in the agent's team frame; lateral signed by the agent's side |
| `<subject>.dx`, `.dy` | the same, relative to the agent |
| `<subject>.vx`, `.vy`, `.speed` | velocity in the frame, and speed |
| `<subject>.dist`, `.closing` | distance from the agent; speed at which the two approach |
| `<subject>.exists` | 1 or 0 |
| `<subject>.<agent signal>` | a per-agent host signal, for agent subjects |
| `self.step_time`, `.assign_time`, `.path_time` | seconds in the step, assignment, path |
| `self.contact` | 1 after an engage step made contact |

Subjects are `self`, `target`, `nearest_opponent`, `nearest_teammate`,
`object.<name>` and `landmark.<name>`. A missing subject reads as NaN, and every
comparison with NaN is false.

## Contests

`ContestModel` turns a one-on-one contest into a win chance with a logistic
curve over the rating difference, the approach angle (`ApproachAngle`), the
speed advantage and the timing error:

```go
m := teamai.ContestModel{Draw: 0.1} // zero fields take documented defaults
c := teamai.Contest{Attacker: 72, Defender: 65, Angle: 0.4, RelSpeed: 1.5, Timing: 0.05}
win, draw, loss := m.Chances(c)
out := m.Resolve(c, world.Seed, world.Tick, uint64(a), uint64(b))
// out.Result is Win, Draw or Loss; out.Margin says how decisive it was
```

The same contest and keys give the same outcome everywhere, and over many keys
the results follow `Chances`. The tests pin exact probability bits and the
counts over 20,000 keys.

## Arbiter rules

`game/teamai/arbiterrules` is its own module, so GoSX does not depend on
Arbiter. Each Arbiter rule becomes a predicate; its variable paths are fact
names:

```arb
rule LaneOpen {
    when { nearest_opponent.dist > 6 and self.y < 30 }
    then Read { branch: "deep" }
}
```

```go
rules, err := arbiterrules.Compile(src)
rules.Register(reg, "")                          // predicates named after the rules
ch, _ := rules.Chooser("Late", "LaneOpen")       // first rule that holds picks its "branch"
reg.Chooser("reads", ch)
book, err := teamai.LoadJSON(data, reg)          // unknown fact names fail here
```

The adapter reads facts straight from the `Query`, with no maps or JSON per
tick, and a predicate call does not allocate. It refuses rules with active
windows, rollouts, segments, prerequisites, excludes or kill switches, because
those depend on more than the facts. The module needs Go 1.26.10 (Arbiter
v1.13.0 does). Today it links Arbiter's parser; a runtime-only evaluator for
precompiled bundles is in progress in Arbiter, and `arbiterrules.New` already
takes a compiled ruleset.

## Data format reference (version 1)

A data file is one JSON object. `LoadJSON` reports every problem at once, each
with the path of its entry:

```text
teamai: formations[0](wedge).slots[2](wing).rol: unknown field "rol" (did you mean "role"?);
assignments[1](chase).steps[3].until: unknown rule "presss": not in rules and not registered
```

Syntax errors carry a line and column. A file with a newer `version` is refused
with "version 2 is newer than this build reads (1)". Names are 1 to 64
characters from `A-Z a-z 0-9 _ -`.

| Top-level field | Type | Notes |
|---|---|---|
| `format` | string | must be `"gosx.teamai"` |
| `version` | integer | `1` |
| `roles` | names | `Body.Role` indexes this list |
| `bands` | names | `Frame.Band` indexes this list |
| `objects` | names | `World.Objects` order |
| `signals`, `team_signals`, `agent_signals` | names | `World.Signals`, `World.TeamSignals`, `World.AgentSignals` |
| `landmarks` | `{name, host, at, side}` | `host` takes the position from `World.Landmarks`; otherwise `at` is local to the agent's team frame |
| `formations` | `{name, slots}` | slot: `{name, role, alternates, at, side, from, lateral, bands, clamp}`; band offset: `{band, offset}` |
| `paths` | `{name, segments}` | see Paths |
| `rules` | `{name, fact, op, value \| than}` or `{name, all \| any \| not}` | `op` is one of `< <= > >= == !=` |
| `assignments` | `{name, steps, interrupts}` | step fields in Assignments and steps; interrupt: `{when, goto}` |
| `plans` | `{name, formation, assign, default, extra}` | assign: `{slot \| role, assignment}`; `extra` is for team agents left without a slot |

Points are `[x, y]` arrays. Times are seconds, speeds are fractions of the
agent's top speed from 0 to 1, distances are world units. Limits: 4,096 entries
per table, 64 slots per formation, 64 segments per path or steps per
assignment, 16 branches per decision, 16 interrupts per assignment, 32 distinct
marks.

`encoding/json` writes a `Spec` back out in the same format, so tools can
generate data files.

## Determinism

The package is marked `//gosx:deterministic` and passes `make lint-deterministic`.
`determinism_test.go` runs the 22-agent drill for 1,000 ticks and pins the state
hash every 100 ticks. It also pins a hash over 20,000 seeded steering,
interception and contest queries. The same values were reproduced on:

| Platform | Fuses multiply-add | Result |
|---|---|---|
| linux/amd64, `GOAMD64=v1` | no | pinned here |
| linux/amd64, `GOAMD64=v3` | yes | identical |
| darwin/arm64 (Apple M4 Pro, oscar-mbp) | yes | identical |
| js/wasm (Node) | no | identical |

## Costs

Measured with `go test -bench 22Agents ./game/teamai` on the drill in
`fixture_test.go`: 11 agents a side, every step kind in use, teammate separation
on. Every row allocates nothing.

| Host | `Step`, 22 agents | Per agent | Drill tick | Snapshot save + restore |
|---|---|---|---|---|
| Apple M4 Pro, darwin/arm64, Go 1.26.1 | 1.40 µs | 64 ns | 2.84 µs | 4.41 µs |
| WSL2 workstation, linux/amd64, Go 1.26.0 | 1.34 µs | 61 ns | 3.0 µs | 2.63 µs |
| shared build host, linux/amd64, Go 1.26.4 | 5.10 µs | 232 ns | 7.98 µs | 7.34 µs |
| js/wasm in Node 26 on the WSL2 workstation | 5.05 µs | 230 ns | 7.8 µs | 8.4 µs |

At 30 ticks per second a tick lasts 33 ms, so the slowest row spends about
0.02% of it on AI.

`Step` is the executor alone. A drill tick adds the host's event handling,
contests and integration. A snapshot is `AppendState` plus `LoadState`. A
Go-registered predicate costs a function call. A decision answered through
Arbiter costs about 190 ns on arm64 (measured with `arbiterrules`'
`BenchmarkArbiterPredicate`, which also re-arms the assignment).

## Limits

- The toolkit plans in the ground plane. Height, jumping and collision response
  belong to the host and its physics.
- Commands are desired velocities. The executor does not know about obstacles;
  path segments steer straight at their points.
- `Separation` pushes teammates apart only. Opponents meet through engage
  steps and the contests your game runs.
- Intercepts assume the target keeps its velocity and the pursuer can turn
  freely. They search a fixed horizon (`Options.Horizon`, 4 s by default).
- `World.Agents` must keep the same order between ticks: agent indices are
  identities.
