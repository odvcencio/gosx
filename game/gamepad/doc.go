// Package gamepad polls the browser Gamepad API and feeds the result into a
// game.Input's existing bindings.
//
// game.Input already understands gamepad-shaped events: game.Button and
// game.Axis bind an action to a "buttonN"/"axisN" code, and
// game.FightingProfile and game.FirstPersonProfile already ship such
// bindings. What was missing was anything that reads navigator.getGamepads()
// and calls Input.Apply — that is this package's whole job.
//
// Poller is the pure translation: standard-mapping button/axis values in,
// game.InputEvent calls out, with a radial deadzone on each stick and edge
// detection so a held button or a resting stick does not spam Input.Apply
// every frame. Poller only depends on the Source interface, so its behavior
// (deadzone shape, edge detection, reconnect handling) is unit-tested
// natively with a fake Source. NavigatorSource, built only under
// GOOS=js/GOARCH=wasm, is the real navigator.getGamepads() adapter.
package gamepad
