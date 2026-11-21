// Package actions maps keyboard, mouse, pointer-lock, gamepad and touch
// input onto named game actions.
//
// A Map holds Contexts (for example "gameplay" and "menu") that stack: the
// top context is asked first, and an Opaque context hides the ones below it.
// Each Context defines actions. An action is a Button or an Axis with one or
// more Bindings, and optional tap, hold and double-tap timing.
//
// Feed raw events into the Map as they arrive, then call Update once per
// frame. A button pressed and released between two Update calls still reads
// as pressed in the next frame (the tap latch), and a held button let go and
// pressed again between two Update calls reads as pressed again, so a fast
// tap or a fast second press is never lost to a slow frame. Two whole taps
// between two Update calls still read as one. Update and the query methods
// (Pressed, Down, Value, Vec2, State) do not allocate.
//
// Bindings can be changed at run time with Rebind, which detects conflicts
// and can reject, swap or unbind them. Capture records the next press for a
// "press a key to bind" screen. Export and Import persist only the bindings
// the player changed, through game/storage or any other store.
//
// For local multiplayer, a Router owns one Map per player and sends each
// device's events to the players it is assigned to.
//
// On a touch screen the page draws its own sticks and buttons, and they are
// input sources like any other: TouchAxis and TouchButton. Touches tracks
// the fingers (one stick or one button per finger, several fingers at once)
// and the js/wasm AttachTouch function reads them from elements the page
// marks with data-gosx-stick and data-gosx-hold.
//
// A press can end without the player letting go: the window loses the focus,
// the browser takes a touch away, or the browser uses the key itself
// (Suppress). The action then reads Released and Canceled together, so logic
// that acts on a release can tell the two apart.
//
// The package does not import package game. Use package game/actions/bridge
// to publish action state to a game.Input, and the js/wasm Attach function to
// read the browser keyboard and mouse.
package actions
