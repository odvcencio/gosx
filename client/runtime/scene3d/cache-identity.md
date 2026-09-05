# Depth and shadow cache identity

Shared bounds-depth results retain one fixed snapshot of six normalized bounds
coordinates and six normalized camera eye/rotation values. Equality is exact;
coordinate sums are not an identity. Near/far clipping is evaluated separately
on every culling call.

WebGL shadow resources retain only the last completed pass's inputs. Reuse
requires exact light/model matrix values, cascade settings, framebuffer/depth
texture/program identity, binding locations, and immutable retained geometry
identity plus revision. Position/index stream replacement and draw-count edits
invalidate reuse. In-place geometry-content edits must advance the geometry
revision, as required by the existing retained-buffer upload contract; claiming
immutable data while changing it without revision is not supported.

Mutable, unversioned, and world-space soup casters rerender their shadow pass.
Their bounds/count cannot prove an unchanged silhouette, even with identical
AABBs. This deliberately gives up whole-pass reuse for those cases; no measured
performance improvement is claimed. The cache has no frame-history map: one
scratch list per renderer and one snapshot per active shadow resource are
proportional to current caster count. Missing program/resources, incomplete
framebuffers, and failed caster binding do not publish reusable results.

Caster eligibility is unchanged here, including the existing camera-view-cull
filter. Camera versus light visibility, alpha-mask silhouettes, deformation and
instancing coverage remain separate correctness work. This fix does not close
the WebGL/WebGPU visual parity or hardware-performance signoff.

`client/js/scene3d-cache-identity.test.js` checks consumed culling and production
depth-pass submissions. `TestScene3DCacheIdentityPixels` serves the generated
bootstrap and exact production shadow-pass/shader source slices into real
WebGL2. Its cached/static and fresh-resource oracles compare readback bytes
exactly; movement counts use a grayscale threshold of eight. It is not a full
mounted-scene test, and its forced SwiftShader run is software graphics.
