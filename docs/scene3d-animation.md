# Scene3D character animation

Drive skinned characters from Go with a layered animation graph: a state
machine with crossfades, 1D and 2D blend spaces, bone-masked and additive
layers, footstep events, two-bone IK with foot placement on uneven ground, and
head or aim look-at. It works with characters rigged in Blender and with
auto-rigged humanoids (Mixamo, Meshy), and it retargets clips between
humanoid rigs that share a skeleton shape. For team games it adds paired
two-character moves (tackles, grapples, handoffs, contested catches) that
meet at a point and time your simulation decides, level of detail for
dozens of characters on screen, and stylized mascot layers.

You declare the graph in Go (package `m31labs.dev/gosx/motion/animgraph`).
Each frame your game sets parameters such as speed and aim, calls `Step`,
reacts to the events that fired, and sends the result to the renderer with
`Dispatch`.

## Where each part runs, and why

| Runs in Go (`motion/animgraph`) | Runs in the browser (animation graph chunk) |
|---|---|
| States, transitions, exit times, triggers, crossfade weights | Clip keyframe sampling |
| Blend-space weights and synced clip times | Layer blending inside bone masks, additive layers |
| Clip events (footsteps) and float curves | Root-motion stripping |
| IK inputs: targets, poles, ground heights, look-at targets | IK solving, joint matrices for skinning |

The renderer already holds the clip keyframes and the skinning, so it samples
and poses there. Doing that in Go would mean copying every keyframe into Go
and sending back sixty 4x4 matrices per character per frame. The graph logic
stays in Go because it is small, deterministic, testable without a browser,
and usable on an authoritative server; events fire inside `Step`, so game code
can play a footstep sound in the same frame with no browser round trip.

Measured costs:

| What | Cost | How it was measured |
|---|---|---|
| `Step` plus frame encoding, one character | 215 ns, 0 allocations | `go test -run x -bench BenchmarkStep ./motion/animgraph` on an Apple M4 Pro |
| Pose evaluation, one 60-joint character | 7.0 to 7.4 µs (Apple M4 Pro), 12.9 to 14.8 µs (NVIDIA RTX 5070 Ti host) | in-page loop in `TestAnimationGraphCharacterOnTheGPU`, Chrome, WebGPU and WebGL2 |
| Frame size, one character | about 145 bytes | GSA1 frame with 3 clip samples, 2 IK chains, 1 look-at |

The pose case is a walk/run blend plus an additive upper-body layer (3 clip
samples in 2 layers), two foot IK chains with ground and pelvis drop, and a
four-bone look-at. The figures are CPU time; GPU skinning is separate.

## Worked example: a third-person hero

This builds a hero who idles, walks and runs on one blend, jumps, aims with
the upper body while the legs keep running, plants both feet on uneven ground,
and turns the head toward a point of interest. The same graph is compiled and
tested as `Example` in `motion/animgraph/example_test.go`.

### 1. Declare the model

The model needs an ID, and `ClipSampled` turns off the browser's own clip
clock so the graph alone poses it.

```go
scene.Model{
    ID:          "hero",
    Src:         "/models/hero.glb",
    Position:    scene.Vector3{X: 0, Y: 0, Z: 0},
    ClipSampled: true,
}
```

### 2. Load the animation runtime on the page

The browser half ships in its own chunk. Require it on pages that use it:

```go
ctx.Runtime().RequireFeature(animgraph.Feature) // "scene3d-animation-graph"
```

### 3. Declare the graph

Clip durations must match the asset. `gltfclip.Read` reads the names and
durations from the `.glb` (see [Clip names and durations](#clip-names-and-durations)).

```go
ik, look := animgraph.Humanoid(true) // foot, hand and head chains on canonical bones

graph := animgraph.Graph{
    Params: []animgraph.Param{
        {Name: "speed", Kind: animgraph.Float},
        {Name: "grounded", Kind: animgraph.Bool, Default: 1},
        {Name: "jump", Kind: animgraph.Trigger},
        {Name: "aim", Kind: animgraph.Bool},
    },
    Clips: []animgraph.Clip{
        {Name: "Idle", Duration: 2, Loop: true,
            Curves: []animgraph.Curve{
                {Name: "leftFootIK", Keys: []animgraph.CurveKey{{T: 0, V: 1}}},
                {Name: "rightFootIK", Keys: []animgraph.CurveKey{{T: 0, V: 1}}},
            }},
        {Name: "Walk", Duration: 1, Loop: true,
            Events: []animgraph.Event{{Name: "footstep", Time: 0.25}, {Name: "footstep", Time: 0.75}}},
        {Name: "Run", Duration: 0.6, Loop: true,
            Events: []animgraph.Event{{Name: "footstep", Time: 0.15}, {Name: "footstep", Time: 0.45}}},
        {Name: "Jump", Duration: 0.8, Events: []animgraph.Event{{Name: "land", Time: 0.7}}},
        {Name: "Aim", Duration: 1, Loop: true},
    },
    Masks: []animgraph.Mask{{Name: "upperBody", Include: []string{"upperChest"}, Exclude: []string{"neck"}}},
    Layers: []animgraph.Layer{
        {Name: "locomotion", Machine: animgraph.StateMachine{
            Entry: "Idle",
            States: []animgraph.State{
                {Name: "Idle", Clip: "Idle", Transitions: []animgraph.Transition{
                    {To: "Move", When: []animgraph.Condition{animgraph.When("speed", animgraph.Greater, 0.1)}, Duration: 0.2},
                }},
                {Name: "Move",
                    Blend1D: &animgraph.BlendSpace1D{Param: "speed", ScaleSpeed: true, Points: []animgraph.BlendPoint1D{
                        {Clip: "Walk", At: 1.4}, {Clip: "Run", At: 4},
                    }},
                    Transitions: []animgraph.Transition{
                        {To: "Idle", When: []animgraph.Condition{animgraph.When("speed", animgraph.Less, 0.1)}, Duration: 0.25},
                    }},
                {Name: "Jump", Clip: "Jump", Transitions: []animgraph.Transition{
                    {To: "Idle", When: []animgraph.Condition{animgraph.WhenTrue("grounded")}, ExitTime: 0.9, Duration: 0.15},
                }},
            },
            Any: []animgraph.Transition{
                {To: "Jump", When: []animgraph.Condition{animgraph.WhenTriggered("jump")}, Duration: 0.1},
            },
        }},
        {Name: "aim", Mask: "upperBody", Additive: true, Machine: animgraph.StateMachine{States: []animgraph.State{
            {Name: "Off", Transitions: []animgraph.Transition{{To: "Aim", When: []animgraph.Condition{animgraph.WhenTrue("aim")}, Duration: 0.2}}},
            {Name: "Aim", Clip: "Aim", Transitions: []animgraph.Transition{{To: "Off", When: []animgraph.Condition{animgraph.WhenFalse("aim")}, Duration: 0.2}}},
        }}},
    },
    IK:     ik,
    LookAt: look,
    Pelvis: animgraph.Pelvis{MaxDrop: 0.3},
}
prog := graph.MustCompile()      // once, at startup; safe to share
player := prog.New("hero")       // one Instance per character, keyed by model ID
```

What the graph does:

- **Locomotion** blends Walk at 1.4 m/s and Run at 4 m/s. Both clips play in
  sync by normalized time, so the feet stay in phase through the blend.
  `ScaleSpeed` speeds playback up above 4 m/s so the feet do not slide.
- **Jump** can start from any state when the `jump` trigger is set; the
  trigger resets when the transition fires. It leaves at 90% of the clip once
  the character is grounded again.
- **Aim** is an additive layer limited to the upper body: the arms and chest
  aim while the legs keep walking or running.
- **Foot IK** weights come from the `leftFootIK` and `rightFootIK` curves:
  1 while idle, 0 while walking, running or jumping, blended across
  crossfades.

### 4. Drive it every frame

Run this once per rendered frame, before the scene draws (for example in a
`game.System` in the render phase). `character`, `input`, `audio`, `world`
and `pointOfInterest` stand for your own game objects.

```go
leftFoot, rightFoot := prog.IKID("leftFoot"), prog.IKID("rightFoot")
head := prog.LookAtID("head")
player.SetIKWeight(leftFoot, 1)
player.SetIKWeight(rightFoot, 1)

func animate(dt float64) {
    player.SetFloat("speed", character.Speed())
    player.SetBool("grounded", character.OnGround())
    player.SetBool("aim", input.Held("aim"))
    if input.Pressed("jump") {
        player.SetTrigger("jump")
    }

    player.Step(dt)
    for _, e := range player.Events() {
        if e.Kind == animgraph.Notify && e.Name == "footstep" {
            audio.Play("footstep", e.Weight) // e.Weight fades with crossfades
        }
    }

    // Raycast below where each foot was animated last frame.
    for _, chain := range []int{leftFoot, rightFoot} {
        if pos, ok := player.Feedback().AnimatedEnd(chain); ok {
            ray := physics.Ray{Origin: physics.Vec3{X: pos[0], Y: pos[1] + 0.5, Z: pos[2]}, Direction: physics.Vec3{Y: -1}}
            if hit, ok := world.Raycast(ray, 1.5); ok {
                player.SetGround(chain, hit.Point.Y, [3]float64{hit.Normal.X, hit.Normal.Y, hit.Normal.Z})
            } else {
                player.ClearGround(chain)
            }
        }
    }
    player.SetLookAt(head, pointOfInterest, 0.8)

    _ = animgraph.Dispatch("scene", player) // ErrNotReady while the page loads; send the next frame as usual
}
```

`Dispatch` sends the rig description once per page, then one small binary
frame per call, and reads back each model's `Feedback`: the pre-IK and solved
foot positions, the look-at pivot and direction, the root bone position, and
the CPU time the renderer spent.

## Reference

### Parameters and conditions

| Kind | Stores | Set with |
|---|---|---|
| `Float` | any finite number | `SetFloat`, `SetFloatID` |
| `Bool` | 0 or 1 | `SetBool`, `SetBoolID` |
| `Trigger` | 0 or 1; a transition that tests it resets it | `SetTrigger`, `ResetTrigger` |

Conditions compare one parameter: `Greater`, `Less`, `GreaterOrEqual`,
`LessOrEqual`, `Equal`, `NotEqual`, `IsTrue`, `IsFalse`, `Triggered`. A
transition fires when all of its conditions hold. Use `prog.ParamID(name)`
with the `...ID` setters in hot loops; the name setters do a map lookup.

### States and transitions

A state plays one motion: `Clip`, `Blend1D`, or `Blend2D`. A state with no
motion contributes nothing; in an upper layer, crossfading into it fades the
layer out. `Speed` and `SpeedParam` scale playback; negative speeds count as
zero.

Each `Step(dt)`, for every layer in order:

1. Advance the running crossfade; when it completes, the states it faded out
   fire `Exit`.
2. Advance every active state's normalized time by `dt × speed / cycle`, where
   cycle is the weight-averaged duration of the state's clips.
3. Fire clip events crossed during this step.
4. Check transitions on the newest state: any-state transitions first, then
   the state's own, in declaration order. The first match fires. At most one
   transition fires per layer per step.

| Transition field | Meaning |
|---|---|
| `To` | destination state |
| `When` | conditions that must all hold |
| `ExitTime` | normalized-time gate; 0 means no gate |
| `Duration` | crossfade length in seconds; 0 switches at once |
| `Offset` | destination start, in normalized time |
| `CanSelf` | lets an any-state transition restart its own target (use with triggers) |
| `Interruptible` | lets the destination's transitions fire before the crossfade ends |

Exit time: for a looping state and `ExitTime <= 1`, the gate opens at that
fraction of every loop and stays open until the loop ends. Otherwise it opens
once the total normalized time reaches `ExitTime` (2.5 means halfway through
the third loop).

Interruptions: while a crossfade runs, any-state transitions can always start
a new one; the destination's own transitions wait unless the running
transition is `Interruptible`. A new crossfade keeps every playing state at
its current weight and fades them out together, so an interrupted blend never
pops. A layer holds at most four playing states; the faintest drops first.

`Enter` fires when a transition starts toward a state, and `Exit` when a state
stops contributing. Both carry the layer and state name.

### Blend spaces

`BlendSpace1D` places clips along one parameter. Between two points the
weights are linear; outside the range the nearest point plays alone.
`ScaleSpeed` raises the playback rate in proportion above the last point.

`BlendSpace2D` places clips on a plane, for example strafe velocity (X) and
forward velocity (Y):

```go
Blend2D: &animgraph.BlendSpace2D{ParamX: "vx", ParamY: "vz", Points: []animgraph.BlendPoint2D{
    {Clip: "Idle", X: 0, Y: 0},
    {Clip: "WalkFwd", X: 0, Y: 1.4}, {Clip: "WalkBack", X: 0, Y: -1.4},
    {Clip: "StrafeL", X: -1.4, Y: 0}, {Clip: "StrafeR", X: 1.4, Y: 0},
    {Clip: "RunFwd", X: 0, Y: 4},
}},
```

Weights use gradient band interpolation (Johansen 2009, cartesian form): each
point gets weight 1 when the parameters sit exactly on it, weights change
smoothly between points, and any point layout works. Every clip in a blend
space shares one normalized time, so walk and run cycles of different lengths
stay in step.

### Events and curves

`Clip.Events` fire when playback crosses their time, including the time-zero
event on a state's first step. By default (`EventsDominant`) only the
highest-weighted state of each layer fires, and within it the
highest-weighted clip, so a synced walk/run blend fires one footstep per
step, not two. `EventsAll` with `EventMinWeight` fires from every clip at or
above that weight. `Event.Weight` reports the clip's blend weight, useful for
volume.

`Clip.Curves` are piecewise-linear float tracks in clip seconds. Each layer
averages them by sample weight (a clip without the curve counts as 0); later
layers override within their weight, or add if additive. Read them with
`Curve(name)`, or name one in `TwoBoneIK.WeightCurve` to drive an IK weight.

### Layers, masks and additive layers

Layer 0 is the base pose. Each later layer blends its pose over the layers
below it, weighted by its own weight (`SetLayerWeight` times `WeightParam`
times the share of states that play a motion) and by its mask.

A `Mask` selects bones: `Include` adds a bone and its descendants at weight 1,
`Exclude` removes a bone and its descendants, and `Weights` sets single bones
afterwards (for example `{"spine": 0.3, "chest": 0.6}` for a gradual blend).
Bone names are canonical humanoid names or raw node names.

An `Additive` layer adds its clips as offsets from a reference pose: each
clip's own first frame, or `RefClip` at `RefTime`. Rotation offsets multiply,
translation offsets add, and scale offsets multiply.

`SyncLayer` names an earlier layer whose playing state sets this layer's
normalized time, so a style clip stays in step with the locomotion under it
(see [Stylized characters](#stylized-characters-mascots)).

### Actions

`PlayAction` plays one clip over a layer without touching its state
machine: a hit reaction, an emote, a celebration, or one side of a paired
move. The machine keeps running underneath, so when the action ends the
layer returns to whatever the machine plays by then.

```go
player.PlayAction("Celebrate", animgraph.ActionOptions{Layer: 0, BlendIn: 0.15, BlendOut: 0.3})
player.SetActionRate(0, 1.2) // change the rate at any frame
player.StopAction(0, 0.2)    // or let a non-looping clip end by itself
```

The action's clip events fire like a state's. `Action(layer)` reports its
clip, time and weight.

### IK

All positions are world space. Feedback reports the same frame's results.

**Two-bone chains** (`TwoBoneIK`) solve analytically: the end bone reaches
the target and both bone lengths stay fixed. The knee or elbow bends toward
`SetIKPole`'s point, or, without a pole, keeps the side it bends to in the
animation. A target out of reach straightens the chain toward it. The chain
weight (`SetIKWeight` times its weight curve) blends the end position between
the animated one and the target.

**Foot placement** (`Foot: true`): the animation assumes flat ground at the
model's origin height. `SetGround(chain, height, normal)` gives the ground
under the foot; the solver moves the foot by the height difference, keeping
the animated clearance, so a planted foot stays planted and a swinging foot
still lifts. With `Pelvis.MaxDrop`, the hips lower by the lowest foot's drop
(at most `MaxDrop`) so a leg can reach ground below the other foot.
`AlignToGround` tilts the foot to the ground normal. Offsets ease in with a
60 ms time constant, so stepping onto a ledge does not pop; the first frame
snaps. Raycast below `Feedback().AnimatedEnd(chain)`: it is the foot's
position before IK in the frame just drawn.

**Look-at and aim** (`LookAt`) turn a bone chain so the pivot's forward axis
points at a target. Each bone takes its weight's share of the remaining turn
and the last bone finishes it, so a spine, chest, neck and head chain turns
the whole upper body a little and the head the most. `Axis` is the forward
direction in model space at the bind pose (+Z by default, the glTF forward).
`Pivot` aims another bone, for example a hand holding a weapon, with `Axis`
set to the weapon's bind-pose direction. `MaxAngle` (1.2 rad by default)
limits the turn away from the animated direction.

### Humanoid rig mapping

Masks, IK chains, look-at chains and `Rig.Bones` accept canonical bone names
(`animgraph.HumanoidBones`, the VRM 1.0 humanoid set): `hips`, `spine`,
`chest`, `upperChest`, `neck`, `head`, `leftShoulder`, `leftUpperArm`,
`leftLowerArm`, `leftHand`, `leftUpperLeg`, `leftLowerLeg`, `leftFoot`,
`leftToes`, the right-side equivalents, eyes, jaw, and three bones per finger.
The renderer maps them to the model's node names:

| Convention | Example names it reads |
|---|---|
| Mixamo | `mixamorig:Hips`, `mixamorig:Spine2`, `mixamorig:LeftUpLeg`, `mixamorig:LeftHandIndex1` |
| Meshy auto-rig | Mixamo names without the prefix: `Hips`, `Spine01`, `LeftUpLeg`, `neck` |
| Blender Rigify (generated) | deform bones only: `DEF-spine` (hips) to `DEF-spine.006` (head), `DEF-thigh.L`, `DEF-f_index.01.L` |
| Blender Rigify metarig | `spine.001`, `upper_arm.L`, `shin.R` |
| Unreal | `pelvis`, `spine_01`, `clavicle_l`, `upperarm_l`, `calf_r`, `ball_l` |
| 3ds Max Biped | `Bip01 Pelvis`, `Bip01 L Thigh`, `Bip01 R Calf` |
| Blender `.L`/`.R` and short names | `thigh.L`, `forearm.R`, `lArm`, `rShin` |

Detection ignores twist and roll bones, end bones (`_end`, `HeadTop_End`),
Rigify segments after the first (`DEF-thigh.L.001`), and control bones
(`MCH-`, `IK`, `ctrl`). Spine chains of any length map to spine, chest and
upper chest from the bottom up. Set `Rig.Bones` to name any bone the matcher
misses, and `Rig.Humanoid: "none"` to use only those overrides. Bones a rig
lacks are reported once in the console and skipped.

**Retargeting.** `Rig.Sources` lists extra glTF files whose clips play on this
model, matched by canonical bone:

```go
Rig: animgraph.Rig{Sources: []animgraph.ClipSource{{Src: "/anim/mixamo-locomotion.glb", Prefix: "mx_"}}},
```

Each mapped bone receives its rotation relative to its rest pose, converted
between the two rigs' bone axes, so a Mixamo clip drives a Rigify rig whose
bones point along different local axes. The hips translation transfers as an
offset from rest, scaled by hip height. Other translations, scale, and morph
weights do not transfer.

Limits of retargeting:

- Both rigs must share the same rest-pose shape. A clip made for a T-pose
  plays wrongly on an A-pose rig; re-export one of them in the other's pose.
- Topology must match where it matters: a bone between two mapped bones must
  stay at rest in the source clip (twist bones are fine; an extra animated
  spine bone loses its motion).
- Fingers transfer only when both rigs have the same finger segments.
- Rigs must face the same way (+Z for glTF exports).

### Paired moves

A paired move is one clip per character, authored together in Blender (one
scene, both characters, one shared origin: the alignment root). The
simulation decides the outcome (who wins the contest), where the two meet and
when; `PairedMove` makes the animation show exactly that:

- **Time warp.** Both clips play at a rate that lands the authored contact
  frame on the decided contact time (bounded by `MinRate` and `MaxRate`,
  0.5 and 2 by default), then at rate 1.
- **Root warp.** Each character's root follows its authored path, carried by
  a frame that moves (with an ease in and out) from where the character
  actually is to the decided alignment root, arriving exactly at contact.
- **Contact IK.** A hand (or any two-bone chain) reaches a bone of the other
  character, resolved from that character's pose in the same frame, with a
  weight that ramps in and out over clip times.

```go
// At load: the authored root paths, from the GLB.
attackerPath, _ := gltfclip.RootPath(glb, "TackleA", "", 30)
defenderPath, _ := gltfclip.RootPath(glb, "TackleB", "", 30)
library := animgraph.PairedLibrary{Clips: []animgraph.PairedClip{{
    Name: "frontStop", Outcome: "stop", Approach: 0, Contact: 0.5, BlendIn: 0.1, BlendOut: 0.2,
    Roles: [2]animgraph.PairRole{
        // Where each character stood in the authoring scene, relative to the alignment root.
        animgraph.PairRole{Name: "attacker", Clip: "TackleA", Path: attackerPath}.Placed(animgraph.Pose{Z: -1.3}),
        animgraph.PairRole{Name: "defender", Clip: "TackleB", Path: defenderPath}.Placed(animgraph.Pose{Z: 0.4, Yaw: math.Pi}),
    },
    IK: []animgraph.ContactIK{
        {Role: 0, Chain: "leftHand", Bone: "rightShoulder", In: 0.3, Full: 0.45, Out: 0.7, End: 0.9},
        {Role: 0, Chain: "rightHand", Bone: "leftShoulder", In: 0.3, Full: 0.45, Out: 0.7, End: 0.9},
    },
}}}

// When the simulation resolves a contest:
clip, ok := library.Pick(result.Outcome, result.Approach, result.Seed)
move, err := animgraph.StartPaired(clip, attacker, defender, animgraph.PairStart{
    Poses: [2]animgraph.Pose{attackerNow, defenderNow}, // world root poses now
    Align: result.ContactPose,                          // alignment root at contact
    In:    result.SecondsToContact,
})

// Every frame until move.Done():
move.Step(dt) // before the instances' Step: sets rates and contact IK
attacker.Step(dt)
defender.Step(dt)
setModelPose("attacker", move.Root(0)) // your scene update: position and yaw
setModelPose("defender", move.Root(1))
_ = animgraph.Dispatch("scene", attacker, defender) // together, for contact IK
```

`Pick` chooses among the clips for an outcome the one whose authored
`Approach` (the direction role 1 comes from, relative to role 0's facing) is
closest to the actual one, and breaks ties within 0.1 radians by the seed, so
every client picks the same clip from the same simulation state. Keep
`scene.Model.RootMotion` off on these models: the renderer removes the
clip's horizontal root motion and `Root` carries the character instead.
`Contacted` reports that the contact frame was reached, and the clip's own
events (a "contact" event, for example) fire as usual. `Cancel` blends both
characters out early.

The renderer resolves a bone target from the other character's pose in the
same frame, so dispatch both partners together, and set `Pinned` on both
partners' `LODTarget` during the move so they update every frame at full
detail.

### Level of detail

`LODScheduler` keeps dozens of skinned characters within a frame budget. It
projects each character's height onto the screen and picks a level:

| Level (`DefaultLOD`) | Projected height | Pose updates | Joints | IK | Layers |
|---|---|---|---|---|---|
| 0 | 220 px and up | every frame | all | yes | all |
| 1 | 90 px and up | 30 Hz | no fingers, eyes or jaw | yes | all |
| 2 | 35 px and up | 20 Hz | also no toes or unmapped bones | no | base only |
| 3 | below 35 px | 10 Hz | as level 2 | no | base only |

```go
sched := &animgraph.LODScheduler{FullBudget: 6} // at most 6 characters at level 0
// every frame, after stepping every instance:
due := sched.Update(dt, animgraph.LODCamera{Position: camPos, FOVY: fovY, ViewportHeight: canvasHeight}, targets)
_ = animgraph.Dispatch("scene", due...)
```

Nothing pops: a level changes only once the character is 15% past the
threshold (`Hysteresis`); IK, upper layers and joint detail fade over 0.25 s
(`FadeSeconds`); characters that share a reduced rate update on staggered
frames; and the renderer re-skins a skipped character when it moves, with
one matrix multiply per joint. Graph logic (`Step`, events) still runs every
frame for every character, at about 0.2 µs each.

### Stylized characters (mascots)

Three pieces cover mascot-style animation over a shared animation set:

- **Style layers.** An additive layer playing a waddle (side-to-side hip
  roll) or a bounce, with `SyncLayer` naming the locomotion layer so the
  style cycle follows the steps. Switch styles with the layer's state
  machine, scale them with `WeightParam`.
- **Proportions.** Retargeted clips (`Rig.Sources`) keep the target rig's
  bone lengths and scale the hips' motion by hip height, so a short-legged
  mascot walks with its feet on the ground. For a mascot rig that shares the
  animation set's bone names, `Rig.KeepProportions` drops the clips'
  non-root translation keys instead.
- **Exaggeration.** `Rig.BoneScale` enlarges single bones after animation,
  for example `{"head": 1.4}` for an oversized head; the scale carries to
  the bone's children.

```go
graph.Layers = append(graph.Layers, animgraph.Layer{
    Name: "style", Additive: true, SyncLayer: "locomotion", WeightParam: "styleWeight",
    Machine: animgraph.StateMachine{States: []animgraph.State{
        {Name: "Waddle", Clip: "Waddle"},
        {Name: "Bounce", Clip: "Bounce"},
    }},
})
graph.Rig = animgraph.Rig{
    Sources:   []animgraph.ClipSource{{Src: "/anim/shared-set.glb"}},
    BoneScale: map[string]float64{"head": 1.4},
}
```

### Clip names and durations

`gltfclip.Read(glb)` returns each animation's name and duration from the
glTF JSON (sampler input accessors must carry `max`, as the glTF 2.0
specification requires; for a GLB without it, the reader falls back to the
last key in the binary chunk). `gltfclip.Clips(infos, loop)` turns them into
`animgraph.Clip` values and strips a Blender `Armature|` prefix. The renderer
matches either form: a clip named `Walk` plays the glTF animation
`Armature|Walk`.

### Browser API and wire format

`Dispatch` calls three functions the chunk installs on
`window.__gosx.scene3d`:

| Function | Returns |
|---|---|
| `defineAnimationRig(hash, json)` | `true` when the rig parses |
| `dispatchAnimationFrame(target, bytes)` | `{applied, pending}`, or `{applied: false, reason}`; `"not-ready"` means send the next frame |
| `animationFeedback(target, modelID, float64Array)` | the number of values written, or 0 |

For tools and tests, `window.__gosx_scene3d_animation_graph_api.measure(target, id, n, mode)`
times `n` full evaluations of a posed model (`id` `"*"` for all of them), or
with mode `"reskin"` the re-skin a skipped, moving model pays.

`target` is the Scene3D engine ID or mount element ID. Frames apply
synchronously and re-skin the named models; a model the frame does not name
keeps its pose. Frames for models that are still loading are held and applied
when the model hydrates.

The frame layout is documented on `animgraph.FrameEncoder`. A golden fixture
(`motion/animgraph/testdata/frame.golden.hex`) pins the encoder, and the
browser decoder test reads the same bytes.

### Limits

- **Root motion stays in the renderer.** By default the hips' horizontal clip
  motion is removed and your simulation moves the character; set
  `scene.Model.RootMotion` to keep it. The graph does not report root-motion
  deltas to Go.
- **Morph targets.** Clip channels that animate morph weights (facial
  shapes) do not play through the graph; skeletal channels do. A model with
  morph targets keeps its default weights.
- **Hand orientation.** Two-bone IK places the hand or foot; it does not
  rotate the hand to match a target orientation (feet do align to the ground
  normal).
- **Curves come from Go.** glTF has no standard float tracks, so declare
  `Clip.Curves` in the graph (for example IK weights keyed at the frames a
  foot is planted).
- **Paired root paths are ground-plane only.** `RootPath` keeps the root's
  X and Z; vertical root motion stays in the clip. The root warp assumes the
  character's rest hips sit over its origin.
- **Skipped frames keep the last pose.** At reduced update rates a distant
  character's pose steps at that rate; its feedback updates only when its
  pose does.
- **Same-thread game loop.** `Dispatch` and `Feedback` call the page directly,
  so the Go code must run on the page's main thread. A game hosted in a Web
  Worker can still encode frames, but it has no synchronous feedback.
- **Per-frame timing.** `Feedback().EvalMicros` reads the browser clock, which
  Chrome coarsens to 100 µs on pages that are not cross-origin isolated; use
  it only as a long average. `window.__gosx_scene3d_animation_graph_api.measure(target, id, n)`
  times `n` evaluations of a posed model.
- **Capacity.** 8 layers, 16 clip samples per layer, 4 states per layer's
  crossfade stack, 8 two-bone chains, 4 look-at chains of up to 8 bones. Skins
  follow the renderer's joint limit (256).
- **Meshy names.** The Meshy row above comes from the Mixamo-style names
  Meshy exports are reported to use; no Meshy asset was available to test.
