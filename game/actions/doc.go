// Package actions maps keyboard, mouse, pointer-lock and gamepad input onto
// named game actions.
//
// A Map holds Contexts (for example "gameplay" and "menu") that stack: the
// top context is asked first, and an Opaque context hides the ones below it.
// Each Context defines actions. An action is a Button or an Axis with one or
// more Bindings, and optional tap, hold and double-tap timing.
//
// Feed raw events into the Map as they arrive, then call Update once per
// frame. A button pressed and released between two Update calls still reads
// as pressed in the next frame (the tap latch), so a fast tap is never lost
// to a slow frame. Update and the query methods (Pressed, Down, Value, Vec2,
// State) do not allocate.
//
// Bindings can be changed at run time with Rebind, which detects conflicts
// and can reject, swap or unbind them. Capture records the next press for a
// "press a key to bind" screen. Export and Import persist only the bindings
// the player changed, through game/storage or any other store.
//
// For local multiplayer, a Router owns one Map per player and sends each
// device's events to the players it is assigned to.
//
// The package does not import package game. Use package game/actions/bridge
// to publish action state to a game.Input, and the js/wasm Attach function to
// read the browser keyboard and mouse.
package actions
