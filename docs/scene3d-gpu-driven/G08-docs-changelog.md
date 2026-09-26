# G08 — Docs page section and changelog (repo: gosx)

Depends on: G06, G07 (the text names the bench workload and the attributes).

## Step 1 — `examples/gosx-docs/app/docs/scene3d/page.gsx`

Anchor (exactly once, in the "Instancing and Level of Detail" section):

```html
			<h3>Instanced glTF</h3>
```

Insert directly BEFORE it:

```html
			<h3>GPU-driven instancing</h3>
			<p>
				Set
				<span class="inline-code">Props.GPUDriven</span>
				and the WebGPU renderer takes over every opaque
				<span class="inline-code">InstancedMesh</span>
				without an authored cull kernel. One compute dispatch culls every instance of those meshes against the camera, and one more per shadow light culls the casters. Each mesh then draws with one indirect draw that reads its instance records from a storage buffer, so per-instance colors survive the cull.
			</p>
			{CodeBlock("go", `scene.Props{
	    GPUDriven: &scene.GPUDriven{Occlusion: true},
	    Graph:     scene.NewGraph(city...),
	}`)}
			<p>
				<span class="inline-code">Occlusion</span>
				adds two-phase hierarchical-Z occlusion culling. The main pass first draws what was visible last frame, builds a depth pyramid from that depth, then culls again and draws only what just became visible. It never drops a visible instance, and it costs one extra compute pass and one extra render pass per frame. It pays off when large occluders hide many instances.
				<span class="inline-code">ShadowCulling</span>
				defaults to true. The mode changes no pixels, so WebGL, Canvas, and the headless renderer ignore it. The mount publishes
				<span class="inline-code">data-gosx-scene3d-webgpu-gpu-driven-*</span>
				attributes: whether the path is active and why, owned meshes and instances, and the survivors of the last frame. The GPU-driven city workload on the Scene3D Bench compares it with the classic path. Transforms are detected by array identity, so replace the transforms array, or stream them with the instance stream, rather than editing one in place.
			</p>
```

`CodeBlock("go", ...)` is the page's existing helper (see the
`scene.InstancedMesh` example just above the anchor); keep the four-space
continuation indent inside the backtick string, as that example does.

## Step 2 — `CHANGELOG.md`

Insert directly after the first line `# Changelog` and its blank line (so the
new section comes before `## v0.57.1 (2026-09-24)`):

```markdown
## Unreleased

### Added: GPU-driven instancing for the WebGPU renderer

- `scene.Props.GPUDriven` hands every opaque `InstancedMesh` without an
  authored cull kernel to one GPU-driven host. One compute dispatch per view
  culls every instance (camera, and each shadow light), and each mesh draws
  with one indirect draw that pulls its instance record from a storage
  buffer. Per-instance colors now survive GPU culling.
- `GPUDriven.Occlusion` adds two-phase hierarchical-Z occlusion culling.
  The cull and Hi-Z kernels are authored once in Elio and embedded as WGSL.
- The mode changes no pixels. WebGL, Canvas and headless rendering ignore it.
  The mount publishes `data-gosx-scene3d-webgpu-gpu-driven-*` telemetry, and
  the Scene3D Bench gains `gpu-driven` and `instanced-classic` workloads.

### Fixed: instanced meshes no longer allocate GPU state every frame

- The render bundle hands the WebGPU renderer a fresh copy of each instanced
  mesh every frame. The renderer cached its uniform buffer and bind group on
  that copy, so it created both again every frame and never replayed its
  render bundle. The cache is now keyed by mesh id.
```

If the file already has an `## Unreleased` section when you get here, add
the two `###` subsections to it instead of creating a second one.

## Verify

```sh
go test ./examples/gosx-docs/... -count=1
git diff --check
```

## Commit

`document(scene3d): document gpu-driven instancing`

- explain the opt-in, occlusion, telemetry and the identity rule for transforms
- record the feature and the instanced cache fix in the changelog
