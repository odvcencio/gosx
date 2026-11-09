// Package replayui is the replay viewer's controls: a Controller that turns
// keyboard, mouse and gamepad actions (package game/actions) into player and
// camera commands, and a Bar that renders the scrubber, speed picker, camera
// picker and marker list as GoSX nodes in the look of package game/kitui.
//
// The Controller and Bar are separate on purpose. The Controller holds no
// markup and runs every frame; the Bar is a pure function of a View, and a
// game re-renders it when View changes (about 4 times a second is enough for
// the clock, and at once for a button press). Bar controls send the same
// "gosx:kit" events as the other kit screens: wire them with kitui.Bind and
// pass the names to Controller.Handle and Controller.HandleSetting.
//
// Controls are plain buttons, a range input and lists, so they work with the
// keyboard (Tab, Enter, Space, arrows on the scrubber) and the mouse without
// extra script. The "replay" action context adds hotkeys and gamepad
// bindings; see Context.
package replayui
