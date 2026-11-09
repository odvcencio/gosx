// Package physicsys connects the physics package's gameplay features to
// game.Runtime: it drives character controllers at the fixed step and
// delivers trigger events to game code.
//
// Both helpers are game.System values for PhaseFixedUpdate. The runtime runs
// every fixed-update system and then steps the physics world, once per fixed
// step, so the order inside one fixed step is:
//
//  1. systems listed before Characters (input, moving platforms)
//  2. Characters: every character controller steps
//  3. systems listed after Characters
//  4. the physics step, which ends by handing its trigger events to the
//     Triggers handlers
//
// Everything runs from the fixed step and the snapshot state alone, never from
// render frame timing, so a server and a predicting client see the same
// characters and the same trigger events tick for tick.
package physicsys
