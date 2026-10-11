# Page commands

Register named actions with `server.PageRuntime.Commands`. This requires the
opt-in workbench chunk, even on a page with no islands, engines, hubs, or
controllers. The selective loader fetches the chunk through the document's
`bootstrapFeatureWorkbenchPath` contract key.

```go
err := runtime.Commands(
    command.Command{
        ID: "edit.undo", Title: "Undo", Group: "Edit",
        Keys: []string{"Mod+Z"},
        Action: command.Action{Signal: "$edit", Value: "undo"},
    },
    command.Command{
        ID: "edit.redo", Title: "Redo", Group: "Edit",
        Keys: []string{"Mod+Shift+Z"},
        Action: command.Action{Signal: "$edit", Value: "redo"},
    },
)
```

Here `runtime` is a `*server.PageRuntime`; import `m31labs.dev/gosx/command`.
Handle the returned error. IDs use lowercase letters and digits separated by
dots or hyphens. Titles are required, and IDs must be unique across the page.
Registration validates the complete list before adding commands.

## Chords and availability

Chords list modifiers followed by one key, separated by `+`. `Mod` means Control
on Windows and Linux, and Meta (Command) on macOS and iOS. Modifier aliases are
`Ctrl`/`Control`, `Alt`/`Option`, `Shift`, and `Meta`/`Cmd`/`Command`/`Super`.
Do not combine `Mod` with Control or Meta.

Keys can be single characters, `F<n>`, or named keys: Space, Plus, Minus, Enter,
Escape/Esc, Tab, Delete/Del, Backspace, Home, End, PageUp, PageDown, Insert, and
ArrowUp/Up, ArrowDown/Down, ArrowLeft/Left, ArrowRight/Right. Use `Plus` for `+`.
Tokens are case-insensitive. `command.ParseChord` parses this grammar in Go.

Matching is exact: `Mod+Shift+Z` fires redo, without firing undo. Unlisted Shift
is ignored for single symbol keys such as `?` and `+`, because layouts use Shift
to type them. Space and Shift+Space remain separate chords. Keyboard dispatch
skips editable targets unless `AllowEditable` is true, and skips events already
handled by the page or an island. The first available matching command wins.

`When` maps shared signal names to expected JSON values. Every entry must equal
the signal's current value for the command to run:

```go
command.Command{
    ID: "transport.play", Title: "Play", Keys: []string{"Space"},
    When: map[string]any{"$view": "arrange"},
    Action: command.Action{Signal: "$transport", Value: "toggle"},
}
```

`Reserved: true` consumes its chord, runs no action, and dispatches
`gosx:command:unavailable` with `{id, at}`. It remains unavailable through the
public API. Use it to reserve an action that the application has not implemented.

## Actions and command elements

An `Action` can write `Signal` with `Value`, open the disclosure selected by
`Open`, close the disclosure selected by `Close`, or combine these operations.
Omitting `Value`, or supplying null, writes `{id, at}`. Use GoSX's disclosure
markup for open/close actions. A nonreserved command needs at least one action.
Every successful run dispatches `gosx:command` on `document` with `{id, at}`;
`at` is the timestamp in milliseconds.

```html
<button data-gosx-command="edit.undo" aria-keyshortcuts="Control+Z Meta+Z">
  Undo
</button>
```

Clicking a command element runs its action. Native button keyboard activation
works too. The workbench narrows `aria-keyshortcuts` to the current platform and
marks reserved command elements with `aria-disabled="true"`. Generate the server
attribute with `command.KeyShortcuts("Mod+Z")`, which includes both Mod
alternatives. Give commands without a chord a button or another activation path.

`window.__gosx.commands` provides `list()`, `run(id)`, `available(id)`,
`platformMod()` (`"ctrl"` or `"meta"`), and `chord(text)` (a parsed chord or null).
`list()` returns ID, title, keys, group, reserved, and current availability.
`run(id)` returns false for missing, reserved, or conditionally unavailable
commands. It applies the same action as a keyboard shortcut or button.

`desktop.MenuItem.Accelerator` is a display label only. It appears after a tab in
the native menu label and installs no native accelerator table. Keep its text
consistent with the page command's chord; the page registry handles keystrokes.
