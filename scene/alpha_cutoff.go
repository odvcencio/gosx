package scene

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// AlphaCutoff is the bounded tri-state alpha-mask authoring value.
//
// The zero value means "omitted": the author said nothing, and the record
// inherits whatever the runtime default is. CutoffDisabled means the author
// explicitly turned alpha cutoff off, and it is transported as JSON null so
// an explicit disable survives a round-trip. Cutoff(value) carries a finite
// non-negative numeric threshold, including 0 and values above 1.
//
// The state is unexported on purpose: there is no public field, no
// pointer-to-pointer API, and no way to construct an ambiguous half-state.
// Constructors retain exactly what the author wrote, even an invalid numeric
// value, so schema validation and JSON marshaling can diagnose it instead of
// silently degrading it to "disabled" or "omitted".
type AlphaCutoff struct {
	state alphaCutoffState
	value float64
}

type alphaCutoffState uint8

const (
	// alphaCutoffOmitted is the zero state: no authored value at all.
	alphaCutoffOmitted alphaCutoffState = iota
	// alphaCutoffDisabled is an explicit disable, transported as JSON null.
	alphaCutoffDisabled
	// alphaCutoffNumeric carries a finite numeric threshold.
	alphaCutoffNumeric
)

// Cutoff authors an explicit numeric alpha cutoff. The value is retained as
// written, even when it is negative or non-finite, so later validation and
// marshaling can report the authored value rather than a sanitized one.
func Cutoff(value float64) AlphaCutoff {
	return AlphaCutoff{state: alphaCutoffNumeric, value: value}
}

// CutoffDisabled authors an explicit alpha-cutoff disable. On the wire it is
// JSON null, which is distinct from an omitted field.
func CutoffDisabled() AlphaCutoff {
	return AlphaCutoff{state: alphaCutoffDisabled}
}

// IsZero reports whether the value is omitted. It exists so material and
// record fields can use json:",omitzero".
func (a AlphaCutoff) IsZero() bool {
	return a.state == alphaCutoffOmitted
}

// Disabled reports whether the author explicitly disabled alpha cutoff.
func (a AlphaCutoff) Disabled() bool {
	return a.state == alphaCutoffDisabled
}

// Value returns the numeric threshold and whether one was authored. An
// omitted or explicitly disabled cutoff returns false, never an accidental 0.
func (a AlphaCutoff) Value() (float64, bool) {
	if a.state != alphaCutoffNumeric {
		return 0, false
	}
	return a.value, true
}

// validAlphaCutoff reports whether the value is a legal wire number: finite
// and non-negative. Zero and values above 1 are legal.
func validAlphaCutoff(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

// MarshalJSON emits null for an explicit disable and a JSON number for an
// authored threshold. An invalid numeric value is an error, never a silent
// downgrade to disabled. The omitted state is never marshaled because every
// carrying field uses json:",omitzero"; marshaling it directly is an error so
// the tri-state stays honest.
func (a AlphaCutoff) MarshalJSON() ([]byte, error) {
	switch a.state {
	case alphaCutoffDisabled:
		return []byte("null"), nil
	case alphaCutoffNumeric:
		if !validAlphaCutoff(a.value) {
			return nil, fmt.Errorf("scene: alphaCutoff %v is not a finite non-negative number", a.value)
		}
		return []byte(strconv.FormatFloat(a.value, 'f', -1, 64)), nil
	default:
		return nil, fmt.Errorf("scene: omitted alphaCutoff has no wire form; carry it in a json:\",omitzero\" field")
	}
}

// UnmarshalJSON accepts JSON null (explicit disable) or a finite non-negative
// JSON number. Anything else is an error and leaves the receiver unchanged.
func (a *AlphaCutoff) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "null" {
		*a = CutoffDisabled()
		return nil
	}
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if !validAlphaCutoff(value) {
		return fmt.Errorf("scene: alphaCutoff must be null or a finite non-negative number, got %v", value)
	}
	*a = Cutoff(value)
	return nil
}

// setAlphaCutoff writes the tri-state onto a legacy props map: omitted emits
// nothing, explicit disable emits a present nil, and a numeric threshold
// emits its number, including 0.
func setAlphaCutoff(target map[string]any, name string, cutoff AlphaCutoff) {
	if target == nil || name == "" {
		return
	}
	switch {
	case cutoff.IsZero():
		// Omitted: emit nothing.
	case cutoff.Disabled():
		target[name] = nil
	default:
		target[name] = cutoff.value
	}
}

// alphaCutoffFromAny lowers one legacy map entry. present distinguishes an
// explicit nil (disable) from a missing key (omit). A typed AlphaCutoff is
// accepted as-is; numeric values use the existing toFloat64 acceptance set
// and are retained as authored even when invalid, so validation sees them.
// Any other value type omits rather than fabricating a zero mask.
func alphaCutoffFromAny(value any, present bool) AlphaCutoff {
	if !present {
		return AlphaCutoff{}
	}
	if value == nil {
		return CutoffDisabled()
	}
	if cutoff, ok := value.(AlphaCutoff); ok {
		return cutoff
	}
	if f, ok := toFloat64(value); ok {
		return Cutoff(f)
	}
	return AlphaCutoff{}
}
