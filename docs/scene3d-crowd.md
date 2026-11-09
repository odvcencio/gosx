# Scene3D crowds

Fill a stadium, an arena or a concert floor with tens of thousands of animated
fans. You describe the stands; the browser builds the seats, and the GPU
animates every fan from a small library of actions. From Go you then drive the
crowd section by section: who cheers, how excited they are, gestures on a
shared beat, a wave travelling round the bowl, raised scarves and lights. Only
those changes cross to the browser, never per-fan data.

This page is about audiences that sit or stand in place. For agents that walk
around, see [Scene3D crowds on a moving parent](scene3d-crowd-motion.md).

## A bowl in twenty lines

```go
home := []scene.CrowdColor{{Color: "#c8102e", Weight: 3}, {Color: "#ffffff", Weight: 1}}
away := []scene.CrowdColor{{Color: "#003087", Weight: 3}, {Color: "#ffc72c", Weight: 1}}

sections := make([]scene.CrowdSection, 24)
for i := range sections {
    sections[i].Colors = home
    if i >= 18 {
        sections[i].Colors = away // the visitors' corner
    }
}

props := scene.Props{
    Environment: scene.Environment{Crowds: []scene.Crowd{{
        ID:   "bowl",
        Seed: 7,
        Stands: []scene.CrowdStand{{
            Shape: scene.CrowdShapeBowl, HalfWidth: 60, HalfDepth: 42, CornerRadius: 26,
            Rows: 21, Sections: sections,
        }},
    }}},
    // ... camera, lights, the pitch and the stadium itself
}
```

That bowl holds 16,084 fans. Its description costs about a kilobyte
on the page. The crowd draws in scenes that already draw something else (the
pitch, the stands), on WebGPU and on WebGL2.

## Describe the stands

A `CrowdStand` is one seating bank: a front edge in plan view, `Rows` that step
back by `RowDepth` and up by `RowRise`, and `Sections` split by aisles. Fans
face the field. Positions are metres in the crowd's own frame; set
`Crowd.Position` and `Crowd.Rotation` to place that frame in the world. Its
origin is the centre of the field or stage.

| Shape | Front edge | Fans face |
|---|---|---|
| `CrowdShapeLine` | `Start` to `End` | `Facing` (default: the origin) |
| `CrowdShapeArc` | a circle of `Radius` round `Center`, from `StartAngle` to `EndAngle` (radians, +X toward +Z) | the centre |
| `CrowdShapeBowl` | a rounded rectangle of `HalfWidth` by `HalfDepth` with `CornerRadius`; `LoopStart` and `LoopEnd` cut a horseshoe | inward |
| `CrowdShapeFloor` | a flat `HalfWidth` by `HalfDepth` area at `Density` fans per square metre | `Facing` (the stage) |

Defaults: 20 rows, 0.85 m treads, 0.42 m rise (about 26 degrees), first row 1 m
up, 0.5 m seats, 1.2 m aisles. A second deck is a second stand with a larger
front edge and a higher `FrontHeight`.

Each `CrowdSection` takes a `Span` (its share of the front edge), shirt
`Colors` and `Accents` (at most four each, chosen per fan by weight), a `Props`
mix, a `Fill` fraction, `Standing` (the section idles on its feet), `NoWave`,
and the `Action` and `Excitement` it starts with. Sections without colours use
the crowd's defaults.

### The same seats everywhere

`Crowd.Seed` decides which seats are filled, each fan's colours, prop, body
height, skin tone and animation phase. Equal descriptions and seeds give equal
seats in Go and in the browser, on every platform: the layout math is
deterministic, and a test checks every seat of a 30,000-seat bowl against the
browser's expansion. Changing `Fill` empties seats without moving anyone else.

`Crowd.Layout()` gives the seats to Go: position, facing, section, row, colour
slot, prop and the azimuth a wave follows. Use it to aim a camera at a section
or to place a sound.

## Drive the crowd

A `CrowdDirector` holds every section's state. Its methods take `t`, your game
clock in seconds; `Frame(t)` returns what changed since the last frame, and
`scene.DispatchCrowdFrame` (in a js/wasm build) sends it.

```go
director, err := scene.NewCrowdDirector(crowd)
if err != nil {
    return err
}
director.Group("home", "lower-0", "lower-1", "lower-2", "lower-3")

// Third down: the home end gets loud and claps on a shared beat.
director.SetExcitement(now, 0.9, 1.5, "home")
director.Sync(now, scene.CrowdClap, 110, "home")

// A big play: everyone up, then a wave round the bowl.
director.SetAction(now, scene.CrowdCheer)
director.StartWave(now+2, scene.CrowdWave{Origin: 0.25, Laps: 2})

if director.Changed() {
    scene.DispatchCrowdFrame("stadium", director.Frame(now))
}
```

| Method | What it does |
|---|---|
| `SetAction(t, action, sel...)` | Blends sections to an action over `Director.Blend` seconds (0.4); fans follow one by one over 0.6 s. |
| `SetExcitement(t, level, ramp, sel...)` | Ramps excitement (0 to 1). It decides how many fans join the action: a quarter at 0, all at 1. The rest idle. Each fan joins or leaves with a short blend. |
| `Sync(t, action, bpm, sel...)` | Puts sections on one beat: one action cycle per beat, all starting at `t`, so claps and jumps land together. |
| `Release(t, sel...)` | Back to free-running, slightly different phases. |
| `RaiseProps(t, raised, sel...)` | Shows or hides the sections' props. |
| `SetPalette(t, colors, accents, sel...)` | Recolours sections: colour i replaces slot i; who wears which slot stays as declared. |
| `StartWave(t, wave)` / `StopWaves(t)` | A wave rises fans as its front passes their azimuth (turns, +X toward +Z). At most four run at once. |
| `FullFrame(t)` | Every section, palette and running wave, for a page that mounts late or reconnects. |

A selector is a section ID, a stand ID (all its sections), a `Group` name, or
`"*"`; none means every section. The browser maps your clock onto its scene
clock from the first frame it receives, so frames apply on time whatever your
clock's zero. A frame that arrives before the crowd has drawn waits for it.

### The action library

| Action | Motion | Cycle (s) | Sound at |
|---|---|---|---|
| `CrowdIdle` | seated; small look-arounds (standing sections stand) | 4.0 | |
| `CrowdStandUp` | standing, swaying | 3.0 | |
| `CrowdCheer` | fists pumping overhead | 0.8 | 0.25 |
| `CrowdClap` | clapping in front of the chest | 0.5 | 0.5 |
| `CrowdWaveArms` | both arms up, swaying together | 1.6 | |
| `CrowdJump` | jumping on the spot | 0.7 | 0.92 |
| `CrowdTwirl` | one hand circling overhead (towels, scarves) | 0.6 | |
| `CrowdHoldUp` | both hands up, holding a prop | 2.4 | |
| `CrowdRideWave` | the wave motion; `StartWave` drives it | 1.0 | 0.5 |

"Sound at" is the fraction of a cycle where the action makes its sound;
`CrowdActions()` lists the same table with each action's loudness.

## Colours and props

Each fan wears one shirt colour and one accent colour from its section. The
weights pick per fan when the layout is built; colours themselves live in a
small GPU table, so `SetPalette` restyles a whole bowl with one frame and no new
layout. A quarter of fans wear a cap in their accent colour.

`CrowdPropMix` gives the share of fans holding each prop: `Lights` (handheld
lights that glow, bright enough to bloom with a post chain), `Scarves` (held in
both hands), `Towels` and `Banners`. Props take the accent colour and show while
the section's props are raised. Lights flicker gently, so a dark stadium full of
them reads as phones.

## Make the crowd heard

The director exposes what the crowd is doing to your mixer. It carries no
audio; you pick the sounds.

```go
noise := scene.CrowdNoise{Bus: "crowd", Quiet: 0.15, Loud: 1}

// Each frame:
levels := director.Audio(now)
noise.Follow(mixer, levels, dt) // *audio.Mixer satisfies scene.CrowdBus

// Schedule claps and stomps on the shared beat.
for _, beat := range director.Beats(now, now+0.5) {
    player.ScheduleAt(clapCue(beat.Strength), audioTime(beat.Time))
}
```

`Audio(t)` returns the fan-weighted `Loudness` (0 to 1), every section's
excitement, action, level and centre for panning, and each wave's front.
`CrowdNoise` rises in about a quarter second and fades over about one and a
half. `Beats(from, to)` lists the sounds of synchronized sections at their exact
times; free-running sections are a texture of sound, not beats. Converting your
game clock to the audio clock is yours.

## Materials

`Crowd.Material.Fan` shades bodies and `Material.Props` shades props. Both draw
with the renderer's standard shading: every scene light, received shadows,
image light, fog, exposure and tone mapping. Each fan's colour multiplies the
material's base colour, so the default white materials show palettes as
written. A `StandardMaterial` sets roughness, sheen, clearcoat and the rest.

Selena: a fan or prop material may be a Selena surface material (one whose
`surface` returns a `Surface`); its `geo.color` is the fan's colour. The crowd
asks the renderer for each material's fragment shader and its uniforms through
its hook, so it shades fans once the renderer's surface-material support is
wired into that hook. Until then a Selena material draws with its standard
fields. Full Selena materials (`surface` returning a colour) are not supported:
they replace the vertex stage the crowd needs.

## Level of detail and cost

Fans are culled and graded in blocks of up to 128 seats by their height on
screen:

| Tier | When | Mesh | Animation |
|---|---|---|---|
| Full | at least `LOD.FullPixels` (64) tall | 13 boxes and a cap: 156 triangles, plus 60 for props | interpolated frames, blended actions |
| Low | at least `LOD.LowPixels` (20) tall | 5 boxes, each arm bent by two joints: 60 triangles, plus 60 for props | nearest frame, blended actions |
| Impostor | smaller | 4 camera-facing cards: 8 triangles, plus 4 for a banner or light | nearest frame |

A fan draws only the prop it holds; the others collapse to nothing in the
vertex shader.

`LOD.MaxFull` (8,000) caps the fans drawn at full detail; the farthest blocks
drop to the low mesh. Neighbouring blocks of one tier merge into one draw, and
bodies and props draw separately (props only for the fans that hold one), so a
whole crowd takes tens of draws. The CPU does per-block work only; the whole
crowd pass takes 0.1 to 0.5 ms of main-thread time a frame in the measurements
below.

Measured with `TestCrowdStadiumOnTheGPU` (e2e): a 29,760-seat two-tier bowl at
1280 by 720 in headless Chrome, a third of the lower tier cheering, a block
clapping on a shared beat, props raised and a wave running. "GPU" is the
renderer's frame time from timestamp queries (WebGPU) or timer queries
(WebGL2); the frame interval stays at the 16.7 ms display refresh in every run.

| View | Fans: full / low / impostor | Draws | Runner | Backend | Crowd GPU ms | Empty field GPU ms | Crowd CPU ms |
|---|---|---|---|---|---|---|---|
| broadcast | 0 / 492 / 28,003 | 18 | NVIDIA RTX 5070 Ti | WebGPU | 0.28 | not sampled | 0.10 |
| broadcast | 0 / 492 / 28,003 | 18 | NVIDIA RTX 5070 Ti | WebGL2 | 0.72 | 0.02 | 0.15 |
| broadcast | 0 / 492 / 28,003 | 18 | Apple M4 Pro | WebGPU | not available | not available | 0.13 |
| broadcast | 0 / 492 / 28,003 | 18 | Apple M4 Pro | WebGL2 | 4.27 | 0.22 | 0.41 |
| close | 256 / 2,524 / 3,221 | 24 | NVIDIA RTX 5070 Ti | WebGPU | 0.13 | not sampled | 0.09 |
| close | 256 / 2,524 / 3,221 | 24 | NVIDIA RTX 5070 Ti | WebGL2 | 0.42 | 0.002 | 0.21 |
| close | 256 / 2,652 / 3,093 | 28 | Apple M4 Pro | WebGPU | not available | not available | 0.16 |
| close | 256 / 2,652 / 3,093 | 28 | Apple M4 Pro | WebGL2 | 2.09 | 0.22 | 0.50 |

"Broadcast" looks across the field from high on the near side; "close" stands
on the field 12 m from a stand. GPU times are medians of 30 samples over 5
seconds. The renderer takes timestamp samples only while it is drawing
something new each frame, so the static empty field has none on WebGPU, and the
Mac's headless Chrome offered no WebGPU timestamp queries at all. Crowd CPU is
the crowd pass's main-thread time per frame (culling, grading, uniform upload
and draw calls).

These are a desktop NVIDIA GPU and an Apple M4 Pro, not a mid-range laptop;
the table is a guide to the crowd's share of a frame, not a laptop guarantee.

## From JavaScript

```js
// Bytes from scene.EncodeCrowdFrame, or built by your own encoder.
await window.__gosx.scene3d.dispatchCrowdFrame("stadium", bytes);

// After the crowd chunk loads: what the crowd is doing now.
window.__gosx_scene3d_crowd_api.levels("stadium", "bowl"); // { loudness, sections }
```

The mount reports `data-gosx-scene3d-crowd` (`none`, `pending`, `ready` or
`failed`), `-crowd-fans`, `-crowd-tiers` (full/low/impostor fans this frame),
`-crowd-draws` and `-crowd-error`. The crowd code ships in its own chunk,
`bootstrap-feature-scene3d-crowd.js` (about 16 KB brotli), which a page fetches
only when its scene declares a crowd.

## Limits

- At most 1,024 sections and 200,000 fans per crowd; four colours per palette;
  four waves at a time.
- Fans do not cast shadows, are not pickable, and are not reflected in water.
  Canvas and headless rendering ignore crowds.
- The fan body is built in. Sculpted glTF fans are not supported yet; the bone
  palette uses the same layout as the glTF crowd atlas, so they can be added
  without changing the shaders' interface.
- An action change and an excitement ramp blend independently, but each section
  has one blend of each kind at a time; a new change starts from where the last
  one had got to.
- A standing section's wave starts from the seated pose.
- The scene clock stops while the scene is paused or the user prefers reduced
  motion, and the crowd stops with it.

## Wire format (GSC1)

Little-endian. Times are float32 offsets from the frame's float64 `now`.

```
"GSC1"
u16 crowd ID length + UTF-8 bytes
f64 now
u16 section count, then per section (42 bytes):
    u16 section, u8 from action, u8 to action,
    f32 blend start - now, f32 blend seconds,
    f32 excitement from, f32 excitement to, f32 rate,
    f32 epoch - now, f32 jitter,
    f32 excitement ramp start - now, f32 excitement ramp seconds,
    u8 flags (1 synchronized, 4 props raised), u8 0
u8 wave count (at most 4), u8 replace waves (0 or 1),
    per wave (20 bytes): f32 start - now, f32 origin, f32 speed, f32 width, f32 laps
u16 palette count, then per palette: u16 section,
    u8 shirt colours (0 keeps them), 3 sRGB bytes each,
    u8 accent colours (0 keeps them), 3 sRGB bytes each
```
