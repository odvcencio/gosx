// Package game provides a first-class runtime foundation for interactive
// simulations, games, and scientific visualizations in GoSX.
//
// It deliberately sits above the lower-level primitives already in the
// repository: engine owns browser/native surfaces, scene owns Scene3D authoring,
// physics owns rigid-body simulation, hub owns realtime transport, and sim owns
// server-authoritative ticking. Package game ties those pieces together with a
// deterministic fixed-step loop, ECS-style world storage, input action mapping,
// asset manifests, and Scene3D/physics bridge helpers.
package game
