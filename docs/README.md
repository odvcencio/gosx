# GoSX guides

These are the guides, by topic. The
[root README](../README.md) covers the framework itself (components, routing,
islands, the CLI). The [package table](../README.md#packages) lists the main packages.
The hosted docs site is gosx.m31labs.dev.

## Guides by topic

### Browser runtime and engines

- [GoSX in the browser](browser-runtime.md), [Browser controllers](controllers.md), [Island events](island-events.md), [Page commands](commands.md)
- [Browser services for Go/WASM engines](browser-services.md), [Engine workers](engine-worker.md), [Loops in a Go WASM engine](engine-frame-loop.md)
- [Embed GoSX apps in iframes](embedded-apps.md), [Cross-frame shared signals](cross-frame-signals.md)
- [Hub observers](hub-observers.md), [Hub snapshots](hub-snapshots.md), [Coordinated shutdown](shutdown-hooks.md)
- [Expression language](expressions.md), [Expression support matrix](expression-support-matrix.md)
- [Source-owned UI recipes](ui-recipes.md), [Workbench controls](workbench.md), [Immutable asset caching](immutable-asset-caching.md)

### Scene3D

- [v1 support contract](scene3d-v1-support.md), [Selena materials](scene3d-selena.md), [Shadows](scene3d-shadows.md), [Image quality: TAA, auto exposure, depth of field](scene3d-image-quality.md), [Decals](scene3d-decals.md), [Text](scene3d-text.md)
- [Terrain, scatter, wind and shorelines](scene3d-terrain.md), [Crowds](scene3d-crowd.md), [Crowds on a moving parent](scene3d-crowd-motion.md), [Particle bursts](scene3d-particle-bursts.md)
- [Character animation](scene3d-animation.md), [Clip frames](scene3d-clip-frames.md), [Timelines](scene3d-timeline.md), [Hierarchical spin](scene3d-group-spin.md), [Shared motion and shader surfaces](motion-transport.md)
- [Prepare selected models](scene3d-model-preparation.md), [Graphics recovery and adaptive quality](scene3d-graphics-recovery.md)
- [Powered vessels, decks and cutaway](scene3d-powered-vessels.md), [Sail a model](scene3d-vessel.md), [Query the ocean surface](scene3d-ocean-query.md)

### Games and simulation

- [Game kit](game-kit.md), [Static game export](static-game.md), [Game performance](game-performance.md)
- [Deterministic simulation](deterministic-simulation.md), [Netplay](netplay.md), [Rooms](rooms.md)
- [Characters, queries and triggers](physics-character.md), [Team AI](team-ai.md), [Game replay](game-replay.md)
- [Audio](audio.md), [Speech](audio-speech.md)

### Operations and measurement

- [Docs site operations](docs-site-operations.md), [Performance measurement availability](performance-metrics.md), [Signing Windows desktop packages](desktop-code-signing.md)
- Telemetry: [typed activities](telemetry-activities.md), [declarations](telemetry-activity-declarations.md), [events](telemetry-activity-events.md), [lifecycle](telemetry-activity-lifecycle.md), [participants](telemetry-participants.md), [records](telemetry-records.md), [memory sink](telemetry-memory-sink.md), [adapters](telemetry-adapters.md)

Do not add specs, plans, roadmaps or reports here. This folder holds user
guides and references only.
