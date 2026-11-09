# Scene3D particle bursts

Start a finite impact or celebration with typed Go emitters. The browser adds an
event-owned system to the mounted scene, then removes it after its longest
particle lifetime. It reuses GoSX's existing GPU particles and CPU/WebGL fallback.

Set `scene.Props{ParticleBursts: scene.Bool(true)}` on a scene that will receive
event effects, or use `<Scene3D particleBursts={true}>`. This advertises the
versioned burst and compute URLs without loading them.
Scenes with existing compute particles already advertise that prerequisite.
Burst playback requires a WebGL or WebGPU mount; other backends reject the request.

```go
burst := scene.ParticleBurst{
    ID: "impact-42", Count: 16, Delay: 190 * time.Millisecond,
    Emitter: scene.ParticleEmitter{
        Kind: "disc", Position: scene.Vec3(2, 0.05, 1),
        Radius: 0.5, Lifetime: 0.4, Scatter: 0.3,
    },
    Forces: []scene.ParticleForce{{Kind: "gravity", Strength: 1}},
    Material: scene.ParticleMaterial{
        Color: "#cfbd89", Size: 0.08, SizeEnd: 0,
        Opacity: 0.65, OpacityEnd: 0, Style: scene.PointStyleGlow,
    },
}
payload, err := json.Marshal(burst)
```

Return the plan with your authenticated action or hub response. Apply any
prerequisite scene commands before starting the effect. Delay is relative to
receipt, so a tabletop game can align a dust burst with a piece's contact frame.

```js
const effect = await window.__gosx.scene3d.burstParticles(mount, response.burst);
const result = await effect.finished;
// Or stop early: effect.cancel();
```

The target may be a mount element, its stable ID, or a mounted scene handle.
Different IDs play concurrently; repeating an ID replaces its previous effect
and creates fresh renderer state. A mount accepts at most 16 active bursts and
4,096 particles across them. Validation completes before replacing an effect.
Existing point layers, compute emitters, and water systems retain their values.
Authoritative particle replacement commands supersede active bursts.

Completion resolves `{ finished: true, suppressed: false }`. Reduced motion
suppresses decorative bursts and resolves with `suppressed: true`, including
when the preference changes during an effect. Cancellation, replacement,
authoritative commands, and disposal resolve `{ finished: false, reason }`, with
reasons `cancelled`, `replaced`, `commands`, and `disposed`. Rendering application
errors reject `finished`. Cleanup runs in the shared write phase; pending writes
cannot resurrect an effect after disposal or authoritative replacement.

The existing `ParticleEmitter` controls shape, radius, scatter, and lifetime;
`ParticleForce` and `ParticleMaterial` control motion and sprite appearance.

## Soft edges where sprites meet the scene

Set `ParticleMaterial.SoftDistance` to fade each sprite as it nears the opaque
surface behind it. The value is in world units: dust that settles on a floor
with `SoftDistance: 0.3` fades over the last 0.3 units instead of cutting a hard
rectangle into the ground. It applies to bursts and to `ComputeParticles`.

```go
Material: scene.ParticleMaterial{
    Color: "#d8e4ee", Size: 0.4, Opacity: 0.5, OpacityEnd: 0,
    BlendMode: scene.BlendAlpha, SoftDistance: 0.3,
}
```

Only blended builtin sprites soften (`BlendAlpha` or `BlendAdditive`, no depth
writes, no authored render shader). WebGPU always reads the scene depth. WebGL2
softens when the scene renders through the post chain (any post effect), and
draws hard sprites when it renders straight to the canvas. Zero, the default,
keeps hard sprites. Bursts reject negative or non-finite values.
`Once` is always enabled. Lifetimes must be positive and at most 10 seconds;
delay is at most 60 seconds. The deadline includes the existing emitter's
emission jitter and randomized lifetime. Background gaps advance at most
100 milliseconds per scheduler frame. Bursts use receipt time independently of
the scene's authored animation clock.

Playback loads `bootstrap-feature-scene3d-particle-burst.js` and the existing
particle compute chunk only on first use, waiting for both before starting.
Its advertised versioned URL supports production manifests and embedded asset
prefixes. `Renderer.SetBootstrapFeatureScene3DParticleBurstPath` overrides the
URL. Scenes without the `particleBursts` opt-in omit the burst URL and never
load the burst chunk, even when they already use compute particles.

Native hosts can call `burst.Sample(currentSceneIR, elapsedSeconds)` to obtain
a `SetParticlesCommand` preserving their other particle layers. Apply it when
the burst starts and at its deadline, using the current authoritative scene.
Use unique event IDs and suppress decorative effects for reduced motion. Hosts
own the clock, command application, and their renderer's particle support.
