# Sail a model

Add `Props.Vessel` alongside `Props.Walk` and `Environment.Ocean`. The lazy controller owns the named model's transforms. Model geometry uses metres, with its bow toward local -Z, waterline at Y=0 and deck at `DeckHeight`. Leave the model's authored position and rotation at zero; put its mooring in `Vessel.Position` and yaw in `Heading`.

```go
Vessel: &scene.Vessel{
    NodeID: "clipper", Position: scene.Vec3(18, 0, -42), Heading: 0.14,
    Length: 22, Beam: 5, Draft: 1.4, DeckHeight: 2.5,
    Helm: scene.Vec3(.65, 4.2, 8), WindDirection: 8, WindStrength: 8,
    LODs: []scene.VesselLOD{{NodeID: "clipper-low", Distance: 100}},
    WakeTexture: "/models/wake-foam.png",
},
```

Walk within 4 m of the helm eye point, then press E or tap **Take the helm**. A/D steer; W/S raise/lower sail; V switches the following stern camera and wheel view. E leaves on deck, furls the sails and lets momentum decay. You can walk along the moving deck. Home or the scene's reset control returns the walker and ship to their starting positions. Touch uses a left thumb drag for the rudder and separate right-side sail buttons; releasing one pointer leaves the other control active. The controls are focus scoped and clear held input on blur or pointer-lock release.

The stern camera follows 1.8 hull lengths behind the ship, at 0.45 lengths above its waterline, looking toward the lower rig. Its 65° vertical field of view frames the full ship on desktop and portrait phones. Wheel view stays at the configured helm eye point, behind the wheel and offset from the mast, looking forward over the bow with a 0.05 m near plane. Deck and wheel views add at most 3.6 cm of gentle bob. Reduced motion removes this added bob and wheel roll while retaining the hull’s physical position.

Wind direction names the direction wind travels, as Ocean does: 0 toward +Z. Heading 0 points the bow toward -Z, directly into that wind. The no-go zone spans 40° on either side of upwind and supplies no propulsion. Both tacks accelerate, reaching maximum speed on a broad reach and less speed dead downwind. Momentum carries the ship through a tack. Backed canvas supplies enough damped rudder authority to pay off from rest when caught in irons; normal steering resumes outside the no-go zone. `WindStrength` defaults to 8 m/s; `MaxSpeed` defaults to 10 m/s and initial `SailTrim` to 0.55.

Four hull samples use the renderer’s shared Gerstner evaluator to drive damped heave, pitch and roll. Wind pressure adds restrained heel on either tack, even before the ship gathers speed; lowering the sails releases that pressure. Relative bow entry into a rising wave records an impact without triggering spray on the first sample. Movement integrates in fixed 1/60 s steps inside the existing paced scene loop. Three capsule footprint samples block terrain shallower than the draft, walk colliders and bounds, with sliding and damped contact. Grounded hulls slow under keel friction and can turn or retreat over equal/deeper seabed. Vessel bounds fall back to Walk.Bounds. The terrain heightfield remains collision data; the CPU query samples the actual ocean bathymetry texture for visual shoaling and run-up parity, using deep water while it loads, as the GPU does. The controller samples the renderer's wall clock for ocean motion. The same hardware gate selects the GPU's four or six waves. Adaptive quality chooses a smaller model earlier and shortens the wake. No controller RAF loop or per-frame shader compilation is added.

`Walk.Surfaces` adds rectangular decks and sloped ramps above the heightfield. Its Y is the centre height; SizeX/SizeZ are full sizes, RotationY is yaw and SlopeX/SlopeZ are rise/run along local axes. The highest overlapping surface supplies the feet height, while piles and rocks still use feet-height collision. The vessel adds and removes its moving deck automatically.

Sail meshes named `canvas-*` gather into visible canvas bundles below their yards while moored or furled, then fill as trim rises. Their UVs range from 0 to 1 across and down each sail. Wind pressure fills the canvas; pinching within 58° of upwind increases lower-edge luffing and flutter. Triangle normals follow the deformed cloth. Meshes named `rope-rigging` and `timber-spars` share a small elastic sway above their deck anchors. A `wind-flag` mesh points into world wind. These are optional conventions for procedural assets; other named models still sail. LOD NodeIDs refer to alternatives already in the graph, at ascending distances; the controller changes visibility without fetching or recompiling while sailing. Keep the models local and give them stable IDs.

The wake is a bounded transparent ribbon plus two bow-wave strips. Visible foam vertices sample ocean height every rendered frame. Dormant effects skip queries; duplicate rows copy the last live row, limiting reduced-detail foam to 50 queries per frame. An optional alpha foam texture fades the sides and ages the trail over six seconds. Impact spray uses at most 20 ballistic droplets, or 10 at reduced detail. Droplets start at the bow, spread to both sides, and disappear when they reach the sampled ocean surface. Positions and velocities are deterministic; no random generator or extra animation loop is used. Set `Wake` to false to omit both water effects. Disposal removes controls, dynamic deck, wake and spray; asynchronous bathymetry loading checks mount ownership before applying.

Unit and mount tests cover sailing, collision, controls, lazy URL gates, reset, pacing and shader reuse. Browser presentation and real-device touch feel need manual checks; the implementation does not provide authoritative multiplayer physics.

Blackglass Beach moors the clipper at `(18, 0, -42)`, 42 m offshore in about 5 m of water, heading 8° into the wind. The pier runs at X=25 from Z=8 on the beach to Z=-35.2, with a ramp up to the 2.5 m deck and a short gangway west to the stern. Its twelve piles share procedural geometry and collision positions. The ship has a fine bow, sheer and tumblehome, three masts, square sails, a bowsprit, stays/shrouds, a wheel and a wind pennant. Quantized attributes and solid timber, tar, canvas and rope materials keep all LODs below the 300,000-byte target.

The regenerated demo adds these asset bytes; all three LODs are fetched during initial model hydration:

| Asset | Bytes | Vertices | Sails |
| --- | ---: | ---: | ---: |
| `clipper-high.glb` | 108,748 | 6,594 | 9 |
| `clipper-mid.glb` | 70,452 | 3,993 | 9 |
| `clipper-low.glb` | 47,004 | 2,489 | 6 |
| Ship LOD total | 226,204 | | |
| `jetty.glb` | 73,940 | | |
| `wake-foam.png` | 6,840 | | |
| New model/image total | 306,984 | | |

Optional runtime transfer, excluding existing Scene3D, renderer and walk chunks:

| Chunk | Raw bytes | Gzip bytes | Brotli bytes |
| --- | ---: | ---: | ---: |
| Ocean query | 2,451 | 1,293 | 1,152 |
| Vessel, cloth, wake and spray | 21,540 | 8,711 | 7,746 |
| Total | 23,991 | 10,004 | 8,898 |

Relative to the recovered ocean-query commits, Scene3D's lazy gates and advance hook add 609 raw bytes (252 gzip, 232 Brotli); moving walk surfaces and suspension add 654 raw bytes (258 gzip, 224 Brotli). Pages without `Props.Vessel` fetch neither optional chunk.
