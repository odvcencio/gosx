// Package command defines page commands: named actions with keyboard chords
// that the opt-in workbench runtime dispatches and mirrors into aria-keyshortcuts.
// Commands are data; the browser matches their chords exactly.
package command

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Command names a page action. Every successful run dispatches gosx:command
// on document with the command ID and timestamp.
type Command struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Keys are exact chords. Mod means Ctrl on Windows/Linux and Cmd on macOS.
	// Unlisted Shift is ignored for single symbol keys such as Plus and ?.
	Keys []string `json:"keys,omitempty"`
	// When requires deep JSON equality for each named shared signal.
	When   map[string]any `json:"when,omitempty"`
	Action Action         `json:"action"`
	// Reserved consumes its chord without running an action.
	Reserved bool `json:"reserved,omitempty"`
	// AllowEditable permits keyboard dispatch from editable elements.
	AllowEditable bool   `json:"allowEditable,omitempty"`
	Group         string `json:"group,omitempty"`
}

// Action writes a shared signal and/or opens or closes a disclosure selector.
// At least one of Signal, Open or Close is required unless the command is reserved.
type Action struct {
	Signal string `json:"signal,omitempty"`
	// Value defaults to {id, at} in the browser when absent or null.
	Value any    `json:"value,omitempty"`
	Open  string `json:"open,omitempty"`
	Close string `json:"close,omitempty"`
}

// Chord is a parsed keyboard chord. Key holds KeyboardEvent.key with single
// characters lowercased and a literal space for Space.
type Chord struct {
	Mod, Ctrl, Alt, Shift, Meta bool
	Key                         string
}

var idPattern = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`)
var functionKeyPattern = regexp.MustCompile(`^f[0-9]+$`)

var namedKeys = map[string]string{
	"space": " ", "plus": "+", "minus": "-", "enter": "Enter",
	"escape": "Escape", "esc": "Escape", "tab": "Tab", "delete": "Delete", "del": "Delete",
	"backspace": "Backspace", "home": "Home", "end": "End", "pageup": "PageUp", "pagedown": "PageDown",
	"insert": "Insert", "arrowup": "ArrowUp", "up": "ArrowUp", "arrowdown": "ArrowDown", "down": "ArrowDown",
	"arrowleft": "ArrowLeft", "left": "ArrowLeft", "arrowright": "ArrowRight", "right": "ArrowRight",
}

// ParseChord parses modifiers followed by one character, a named key or F<n>.
// Use Plus to name the + key. Mod cannot be combined with Ctrl or Meta.
func ParseChord(text string) (Chord, error) {
	var chord Chord
	parts := strings.Split(text, "+")
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "mod":
			chord.Mod = true
		case "ctrl", "control":
			chord.Ctrl = true
		case "alt", "option":
			chord.Alt = true
		case "shift":
			chord.Shift = true
		case "meta", "cmd", "command", "super":
			chord.Meta = true
		default:
			return Chord{}, fmt.Errorf("invalid chord %q: unknown modifier %q", text, part)
		}
	}
	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	switch {
	case key == "":
		return Chord{}, fmt.Errorf("invalid chord %q: missing key", text)
	case utf8.RuneCountInString(key) == 1:
		chord.Key = key
	case namedKeys[key] != "":
		chord.Key = namedKeys[key]
	case functionKeyPattern.MatchString(key):
		chord.Key = strings.ToUpper(key)
	default:
		return Chord{}, fmt.Errorf("invalid chord %q: unknown key %q", text, key)
	}
	if chord.Mod && (chord.Ctrl || chord.Meta) {
		return Chord{}, fmt.Errorf("invalid chord %q: Mod cannot be combined with Ctrl or Meta", text)
	}
	return chord, nil
}

// ARIA renders the chord for aria-keyshortcuts. Mod produces Control and Meta
// alternatives so server-rendered markup works on either platform.
func (c Chord) ARIA() []string {
	if c.Mod {
		c.Mod = false
		control, meta := c, c
		control.Ctrl = true
		meta.Meta = true
		return append(control.ARIA(), meta.ARIA()...)
	}
	var parts []string
	for _, mod := range []struct {
		held bool
		name string
	}{
		{c.Ctrl, "Control"}, {c.Alt, "Alt"}, {c.Meta, "Meta"}, {c.Shift, "Shift"},
	} {
		if mod.held {
			parts = append(parts, mod.name)
		}
	}
	key := c.Key
	switch {
	case key == " ":
		key = "Space"
	case key == "+":
		key = "Plus"
	case utf8.RuneCountInString(key) == 1:
		key = strings.ToUpper(key)
	}
	return []string{strings.Join(append(parts, key), "+")}
}

// KeyShortcuts joins ARIA alternatives, skipping unparseable chords.
func KeyShortcuts(keys ...string) string {
	var shortcuts []string
	for _, key := range keys {
		chord, err := ParseChord(key)
		if err == nil {
			shortcuts = append(shortcuts, chord.ARIA()...)
		}
	}
	return strings.Join(shortcuts, " ")
}

// Validate checks command IDs, titles, chords and actions, including duplicate
// IDs across the entire page registry. Each error names the command.
func Validate(cmds []Command) error {
	seen := make(map[string]bool, len(cmds))
	for _, cmd := range cmds {
		if !idPattern.MatchString(cmd.ID) {
			return fmt.Errorf("command %q: invalid ID", cmd.ID)
		}
		if seen[cmd.ID] {
			return fmt.Errorf("command %q: duplicate ID", cmd.ID)
		}
		seen[cmd.ID] = true
		if strings.TrimSpace(cmd.Title) == "" {
			return fmt.Errorf("command %q: title is empty", cmd.ID)
		}
		for _, key := range cmd.Keys {
			if _, err := ParseChord(key); err != nil {
				return fmt.Errorf("command %q: %w", cmd.ID, err)
			}
		}
		if !cmd.Reserved && cmd.Action.Signal == "" && cmd.Action.Open == "" && cmd.Action.Close == "" {
			return fmt.Errorf("command %q: action is empty", cmd.ID)
		}
	}
	return nil
}
