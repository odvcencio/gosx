# Imported animation inventory

`gosx assets plan` includes the following glTF/GLB inspection data:

- `animationClips`: source index, authored name, channel count, target paths,
  interpolation modes, and optional time bounds.
- `morphTargets`: total target records across mesh primitives, plus
  `morphPrimitives` for the number of primitives carrying targets.
- `skinManifest`: includes both skeleton-skinned and morph-only assets.
  `skinned` and `morphTargets` are independent flags.

Clip indices distinguish unnamed clips and duplicate names. Timing is supplied
only when every referenced sampler has valid scalar float accessor bounds.
`timingSource: "accessor-bounds"` identifies that provenance. These bounds are
authored metadata: this cheap inventory does not decode or validate animation
samples. Missing or partial bounds remain unknown, rather than becoming a
fabricated zero-duration clip.

This is an additive asset-plan contract. `AnimationClipInfo` is inventory, not
the runtime animation mixer API or a certification of an imported character.
