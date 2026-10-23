package hydrate

import (
	"fmt"
	"strings"
)

// HubCSSVar maps one payload field to one CSS custom property.
type HubCSSVar struct {
	Name   string `json:"name"`             // "--accent-core"; must start with "--"
	Path   string `json:"path"`             // "coreInner" or "palette.core.inner", relative to HubCSSVars.Root
	Unit   string `json:"unit,omitempty"`   // appended to numbers only: "deg", "px"
	Digits int    `json:"digits,omitempty"` // round numbers to this many decimals; 0 keeps the number as sent
	// Kind limits what the browser writes: "" (plain token), "number",
	// "length", "color", "ident" or "string". Every kind rejects control
	// characters and values over 256 characters. The plain token and ident
	// kinds also reject parentheses, backslashes and quotes, so a payload can
	// never build a url(), image-set() or escaped function name. "color"
	// accepts a hex or named color or a rgb, hsl, hwb, lab, lch, oklab or
	// oklch function. "string" writes a quoted CSS string.
	Kind string `json:"kind,omitempty"`
}

// HubCSSVarKinds lists the accepted HubCSSVar.Kind values.
var HubCSSVarKinds = []string{"", "number", "length", "color", "ident", "string"}

// HubCSSVars writes fields of a hub event payload to CSS custom properties.
//
// The browser skips the flash class when prefers-reduced-motion: reduce
// matches, when the tab is hidden, or when the page URL has a
// __gosx_visual_seed query parameter. It skips values that are not a string,
// a finite number or a boolean. It never removes a property.
type HubCSSVars struct {
	Target     string      `json:"target,omitempty"`     // CSS selector; empty means the document element (":root")
	Root       string      `json:"root,omitempty"`       // payload path the Vars paths are relative to; empty means the payload itself
	Vars       []HubCSSVar `json:"vars"`                 // at least one
	FlashClass string      `json:"flashClass,omitempty"` // added to Target after an apply that changed a value
	FlashMS    int         `json:"flashMs,omitempty"`    // how long FlashClass stays; 0 means 1000; range 16..60000
	FlashWhen  string      `json:"flashWhen,omitempty"`  // payload path; flash only when this value differs from the last one seen
	LiveClass  string      `json:"liveClass,omitempty"`  // added to Target once, after the first apply
}

// Validate reports the first problem with the mapping. Every message starts
// with "cssVars" so an author can find the binding.
func (c HubCSSVars) Validate() error {
	target := strings.TrimSpace(c.Target)
	if len(target) > 256 || hasControlChar(target) {
		return fmt.Errorf("cssVars target must be at most 256 characters with no control characters")
	}
	root := strings.TrimSpace(c.Root)
	if root != "" && !validHubPayloadPath(root) {
		return fmt.Errorf("cssVars root %q is not a payload path", root)
	}
	flashWhen := strings.TrimSpace(c.FlashWhen)
	if flashWhen != "" && !validHubPayloadPath(flashWhen) {
		return fmt.Errorf("cssVars flashWhen %q is not a payload path", flashWhen)
	}
	for field, token := range map[string]string{"flashClass": c.FlashClass, "liveClass": c.LiveClass} {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if len(token) > 64 || strings.IndexFunc(token, isSpaceOrControl) >= 0 {
			return fmt.Errorf("cssVars %s %q must be one class name of at most 64 characters", field, token)
		}
	}
	if c.FlashMS != 0 && (c.FlashMS < 16 || c.FlashMS > 60000) {
		return fmt.Errorf("cssVars flashMs %d is outside 16..60000", c.FlashMS)
	}
	if len(c.Vars) == 0 {
		return fmt.Errorf("cssVars needs at least one var")
	}
	seen := make(map[string]bool, len(c.Vars))
	for i, v := range c.Vars {
		if !validCSSCustomPropertyName(v.Name) {
			return fmt.Errorf("cssVars vars[%d] name %q must start with -- and use letters, digits, - and _", i, v.Name)
		}
		if seen[v.Name] {
			return fmt.Errorf("cssVars vars[%d] name %q appears twice", i, v.Name)
		}
		seen[v.Name] = true
		if !validHubPayloadPath(v.Path) {
			return fmt.Errorf("cssVars vars[%d] path %q is not a payload path", i, v.Path)
		}
		if len(v.Unit) > 16 || strings.IndexFunc(v.Unit, func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '%')
		}) >= 0 {
			return fmt.Errorf("cssVars vars[%d] unit %q must be at most 16 letters or %%", i, v.Unit)
		}
		kindOK := false
		for _, kind := range HubCSSVarKinds {
			if v.Kind == kind {
				kindOK = true
			}
		}
		if !kindOK {
			return fmt.Errorf("cssVars vars[%d] kind %q is not one of number, length, color, ident, string", i, v.Kind)
		}
		if v.Digits < 0 || v.Digits > 10 {
			return fmt.Errorf("cssVars vars[%d] digits %d is outside 0..10", i, v.Digits)
		}
	}
	return nil
}

func isSpaceOrControl(r rune) bool {
	return r <= ' ' || r == 0x7f
}

func hasControlChar(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0
}

// validHubPayloadPath accepts 1..128 characters of [A-Za-z0-9_.] with no
// empty segment.
func validHubPayloadPath(path string) bool {
	if path == "" || len(path) > 128 {
		return false
	}
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return false
		}
	}
	for _, r := range path {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// validCSSCustomPropertyName copies the rule of motion.validCSSVariable
// (motion/authoring.go) so hydrate does not import motion.
func validCSSCustomPropertyName(property string) bool {
	if !strings.HasPrefix(property, "--") || len(property) < 3 || len(property) > 128 {
		return false
	}
	for i, r := range property[2:] {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && ((r >= '0' && r <= '9') || r == '-')) {
			continue
		}
		return false
	}
	return true
}
