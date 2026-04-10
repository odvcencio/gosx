# field

Package `field` provides a 3D vector field type with trilinear sampling, standard operators, per-component scalar quantization, and hub streaming. It is the foundation for volumetric rendering, particle advection, fluid simulation, and any consumer that needs structured 3D data — independent of any renderer.

## Concepts

- **`Field`** — a 3D grid of N-component vectors (1, 2, 3, or 4 components) with world-space `AABB` bounds.
- **Sampling** — trilinear interpolation with edge clamping. `Sample`, `SampleScalar`, `SampleVec3`.
- **Operators** — `Advect`, `Curl`, `Divergence`, `Gradient`, `Blur`, `Resample`.
- **Codec** — per-component min/max scalar quantization at 4–8 bits, with optional delta encoding against a previous field for streaming.
- **Streaming** — `PublishField` / `SubscribeField` integrate with `gosx/hub` for live broadcast across WebSocket connections.

## Quick start

```go
import "github.com/odvcencio/gosx/field"

// Build a velocity field analytically.
v := field.FromFunc([3]int{32, 32, 32}, 3,
    field.AABB{Min: [3]float32{-1, -1, -1}, Max: [3]float32{1, 1, 1}},
    func(x, y, z float32) []float32 {
        return []float32{-y, x, 0} // tangential rotation around Z
    },
)

// Quantize for the wire.
q := v.Quantize(field.QuantizeOptions{BitWidth: 6})
fmt.Println("wire bytes:", q.WireSize())

// Decompress on the receiver.
decoded := q.Decompress()

// Advect particles for one tick.
particles := []float32{0.5, 0, 0, 0, 0.5, 0}
field.Advect(decoded, particles, 0.016)
```

## Wire format

| Section | Bytes | Notes |
|---|---|---|
| Header | ~64 | Resolution, Components, Bounds, BitWidth |
| Mins | 4 × Components | per-component min |
| Maxs | 4 × Components | per-component max |
| Packed | (voxels × components × bitWidth + 7) / 8 | deinterleaved by component |
| Preview | optional, smaller bitWidth | progressive loading |

A 64³ scalar field at 6 bits packs to ~200 KB. A 64³ vec3 field at 6 bits packs to ~600 KB. Delta encoding against a previous field reduces this significantly when the field is temporally coherent.

## Operators

| Function | Input | Output | Use |
|---|---|---|---|
| `Advect(v, particles, dt)` | vec3 field | mutates particles | particle simulation, fluid markers |
| `Curl(v)` | vec3 field | vec3 field | swirly motion from a smooth field |
| `Divergence(v)` | vec3 field | scalar field | source/sink detection, fluid pressure projection |
| `Gradient(s)` | scalar field | vec3 field | normals from SDFs, drift from densities |
| `Blur(f, radius)` | any field | same shape | smoothing, removing noise |
| `Resample(f, newRes)` | any field | new resolution | LOD streaming |

## Streaming

```go
// Producer
field.PublishField(hub, "wind", currentWind, field.QuantizeOptions{BitWidth: 6})

// Subscriber (any goroutine)
ch := field.SubscribeField(hub, "wind")
for f := range ch {
    // ... use f, decoded and delta-applied automatically
}
```

Successive `PublishField` calls automatically delta-encode against the previous publish for that topic. Subscribers must treat received `*Field` values as read-only. Subscriptions are permanent for the lifetime of the process — there is no unsubscribe API yet.

## Status

Phase 1 of the gosx Scene3D + Fields upgrade.
